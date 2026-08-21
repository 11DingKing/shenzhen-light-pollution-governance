package worker

import (
	"context"
	"errors"
	"fmt"

	"lightgovernance/internal/auditlog"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/store"
)

// ErrRuleReeval is a sentinel wrapped into every worker-originated error so
// callers can detect them with errors.Is(err, worker.ErrRuleReeval).
var ErrRuleReeval = errors.New("rule re-evaluation failure")

// reevaluableStatuses are the item states that may be re-routed when a newer
// rule becomes active. Items already in_progress or beyond are left alone so
// in-flight work is never disrupted.
var reevaluableStatuses = []domain.ItemStatus{
	domain.StatusRegistered,
	domain.StatusAdjudicated,
}

const sweepPageSize = 200

// sweep reads the current rule version, collects items still routed under an
// older version, and re-adjudicates each one under the active rules.
func (w *ReevalWorker) sweep(ctx context.Context) error {
	current, err := w.store.GetCurrentRuleVersion(ctx)
	if err != nil {
		return fmt.Errorf("get current rule version: %w: %w", err, ErrRuleReeval)
	}
	if current == 0 {
		return nil
	}

	rules, err := w.store.GetActiveRules(ctx, w.clock.Now())
	if err != nil {
		return fmt.Errorf("get active rules: %w: %w", err, ErrRuleReeval)
	}

	stale, err := w.collectStaleItems(ctx, current)
	if err != nil {
		return fmt.Errorf("collect stale items: %w: %w", err, ErrRuleReeval)
	}
	if len(stale) == 0 {
		return nil
	}

	for _, item := range stale {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sweep canceled: %w: %w", err, ErrRuleReeval)
		}
		if err := w.reevaluateItem(ctx, item, rules, current); err != nil {
			w.failed.Add(1)
			w.logger.Error().Err(err).Str("item_id", item.ID).Msg("re-evaluate item failed")
		}
	}
	return nil
}

// collectStaleItems pages through re-evaluable items and keeps those whose
// rule version is older than the current one.
func (w *ReevalWorker) collectStaleItems(ctx context.Context, currentVersion int) ([]*domain.ComplianceCase, error) {
	var stale []*domain.ComplianceCase
	for _, status := range reevaluableStatuses {
		offset := 0
		for {
			items, _, err := w.store.ListItems(ctx, domain.ItemFilter{
				Status:     status,
				PageSize:   sweepPageSize,
				PageOffset: offset,
			})
			if err != nil {
				return nil, fmt.Errorf("list %s items offset %d: %w", status, offset, err)
			}
			for _, it := range items {
				if it.RuleVersion < currentVersion {
					stale = append(stale, it)
				}
			}
			if len(items) < sweepPageSize {
				break
			}
			offset += sweepPageSize
		}
	}
	return stale, nil
}

// reevaluateItem re-routes a single item under the active rules inside one
// transaction. It is idempotent: if the item has already been moved to the
// current version (by a prior cycle or another path) it is a no-op.
func (w *ReevalWorker) reevaluateItem(ctx context.Context, item *domain.ComplianceCase, rules []*domain.Rule, currentVersion int) error {
	fresh, err := w.store.GetItem(ctx, item.ID)
	if err != nil {
		return fmt.Errorf("reload item %s: %w", item.ID, err)
	}
	if fresh.RuleVersion >= currentVersion {
		w.skipped.Add(1)
		return nil
	}
	if fresh.Status.IsTerminal() {
		w.skipped.Add(1)
		return nil
	}
	if !isReevaluable(fresh.Status) {
		w.skipped.Add(1)
		return nil
	}

	referral, err := w.adjudicator.Adjudicate(ctx, fresh, rules)
	if err != nil {
		if errors.Is(err, domain.ErrNoMatchingRule) {
			w.skipped.Add(1)
			w.logger.Warn().
				Str("item_id", fresh.ID).
				Msg("no matching rule during re-eval; keeping current routing")
			return nil
		}
		return fmt.Errorf("adjudicate item %s: %w", fresh.ID, err)
	}

	now := w.clock.Now()
	if fresh.Status == domain.StatusRegistered {
		if err := fresh.TransitionTo(domain.StatusAdjudicated); err != nil {
			return fmt.Errorf("transition item %s: %w", fresh.ID, err)
		}
	}
	oldVersion := fresh.RuleVersion
	fresh.LeadDepartment = referral.LeadDepartment
	fresh.CoDepartments = referral.CoDepartments
	fresh.RuleVersion = referral.RuleVersion
	fresh.UpdatedAt = now

	err = w.store.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.MarkAssignmentSuperseded(ctx, fresh.ID); err != nil {
			return fmt.Errorf("supersede old referral: %w", err)
		}
		if err := tx.SaveAssignment(ctx, referral); err != nil {
			return fmt.Errorf("save new referral: %w", err)
		}
		if err := tx.UpdateItem(ctx, fresh); err != nil {
			return fmt.Errorf("update item: %w", err)
		}
		audit := auditlog.NewEntry(
			fresh.ID, "item", auditlog.ActionRuleReevaluated, "system", now,
			fmt.Sprintf("rule v%d->v%d lead %s", oldVersion, referral.RuleVersion, referral.LeadDepartment))
		return tx.SaveAudit(ctx, audit)
	})
	if err != nil {
		return fmt.Errorf("reeval tx for %s: %w", fresh.ID, err)
	}
	w.reevaled.Add(1)
	return nil
}

func isReevaluable(s domain.ItemStatus) bool {
	for _, r := range reevaluableStatuses {
		if s == r {
			return true
		}
	}
	return false
}
