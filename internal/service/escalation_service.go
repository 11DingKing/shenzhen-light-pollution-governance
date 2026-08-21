package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"lightgovernance/internal/auditlog"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/store"
)

type EscalationService struct {
	store             store.Store
	clock             domain.Clock
	deadlineExtension time.Duration
	maxLevel          int
}

func NewEscalationService(s store.Store, clock domain.Clock, ext time.Duration, maxLevel int) *EscalationService {
	return &EscalationService{store: s, clock: clock, deadlineExtension: ext, maxLevel: maxLevel}
}

func (s *EscalationService) CheckAndEscalate(ctx context.Context) ([]*domain.Escalation, error) {
	now := s.clock.Now()
	overdue, err := s.store.FindOverdueItems(ctx, now, s.maxLevel)
	if err != nil {
		return nil, fmt.Errorf("find overdue items: %w", err)
	}
	var escalations []*domain.Escalation
	for _, item := range overdue {
		esc, err := s.escalateItem(ctx, item, now)
		if err != nil {
			if errors.Is(err, domain.ErrMaxEscalationReached) {
				if ferr := s.recordPermanentFailure(ctx, item, now, err); ferr != nil {
					return escalations, ferr
				}
				continue
			}
			return escalations, fmt.Errorf("escalate item %s: %w", item.ID, err)
		}
		escalations = append(escalations, esc)
	}
	return escalations, nil
}

func (s *EscalationService) escalateItem(ctx context.Context, item *domain.ComplianceCase, now time.Time) (*domain.Escalation, error) {
	newLevel := item.EscalationLevel + 1
	if newLevel > s.maxLevel {
		return nil, fmt.Errorf("item %s at level %d: %w", item.ID, item.EscalationLevel, domain.ErrMaxEscalationReached)
	}
	newLead := domain.EscalationDepartmentName(item.LeadDepartment, newLevel)
	newDeadline := now.Add(s.deadlineExtension)

	esc := &domain.Escalation{
		ID:          uuid.NewString(),
		ItemID:      item.ID,
		FromLevel:   item.EscalationLevel,
		ToLevel:     newLevel,
		OldLeadDept: item.LeadDepartment,
		NewLeadDept: newLead,
		EscalatedAt: now,
		Reason:      "deadline_exceeded",
		NewDeadline: newDeadline,
		DataVersion: domain.DataVersion,
	}
	referral := &domain.Referral{
		ID:             uuid.NewString(),
		ItemID:         item.ID,
		LeadDepartment: newLead,
		CoDepartments:  append([]string(nil), item.CoDepartments...),
		RuleVersion:    item.RuleVersion,
		AdjudicatedAt:  now,
		AdjudicatedBy:  "system_escalation",
		IsCurrent:      true,
		DataVersion:    domain.DataVersion,
	}

	item.LeadDepartment = newLead
	item.EscalationLevel = newLevel
	item.Deadline = newDeadline
	if err := item.TransitionTo(domain.StatusEscalated); err != nil {
		return nil, err
	}
	item.UpdatedAt = now

	err := s.store.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.SaveEscalation(ctx, esc); err != nil {
			return fmt.Errorf("save escalation: %w", err)
		}
		if err := tx.MarkAssignmentSuperseded(ctx, item.ID); err != nil {
			return fmt.Errorf("mark referral superseded: %w", err)
		}
		if err := tx.SaveAssignment(ctx, referral); err != nil {
			return fmt.Errorf("save escalation referral: %w", err)
		}
		if err := tx.UpdateItem(ctx, item); err != nil {
			return fmt.Errorf("update item: %w", err)
		}
		audit := auditlog.NewEntry(item.ID, "item", auditlog.ActionEscalate, "system", now,
			fmt.Sprintf("level %d->%d lead %s->%s", esc.FromLevel, esc.ToLevel, esc.OldLeadDept, esc.NewLeadDept))
		return tx.SaveAudit(ctx, audit)
	})
	if err != nil {
		return nil, err
	}
	return esc, nil
}

func (s *EscalationService) recordPermanentFailure(ctx context.Context, item *domain.ComplianceCase, now time.Time, cause error) error {
	failure := &domain.PermanentFailure{
		ID:            uuid.NewString(),
		EntityType:    "item",
		EntityID:      item.ID,
		TaskType:      "escalation",
		LastError:     cause.Error(),
		Attempts:      s.maxLevel,
		FirstFailedAt: item.RegisteredAt,
		LastFailedAt:  now,
		Status:        domain.FailureStatusActive,
		DataVersion:   domain.DataVersion,
	}
	return s.store.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.SaveFailure(ctx, failure); err != nil {
			return fmt.Errorf("save failure: %w", err)
		}
		audit := auditlog.NewEntry(item.ID, "item", "max_escalation_reached", "system", now, cause.Error())
		return tx.SaveAudit(ctx, audit)
	})
}
