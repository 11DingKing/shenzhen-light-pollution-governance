package dispatch

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"lightgovernance/internal/domain"
)

type Adjudicator struct {
	clock domain.Clock
}

func NewAdjudicator(clock domain.Clock) *Adjudicator {
	return &Adjudicator{clock: clock}
}

func (a *Adjudicator) Adjudicate(ctx context.Context, item *domain.ComplianceCase, rules []*domain.Rule) (*domain.Referral, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var matched []*domain.Rule
	for _, rule := range rules {
		if !rule.IsActiveAt(item.RegisteredAt) {
			continue
		}
		if rule.Matches(item) {
			matched = append(matched, rule)
		}
	}

	if len(matched) == 0 {
		for _, rule := range rules {
			if rule.IsActiveAt(item.RegisteredAt) && rule.IsDefault {
				matched = append(matched, rule)
				break
			}
		}
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("no matching dispatch rule for item %s: %w", item.ID, domain.ErrNoMatchingRule)
	}

	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Priority != matched[j].Priority {
			return matched[i].Priority > matched[j].Priority
		}
		return matched[i].Version > matched[j].Version
	})

	chosen := matched[0]
	now := a.clock.Now()

	return &domain.Referral{
		ID:             uuid.NewString(),
		ItemID:         item.ID,
		LeadDepartment: chosen.LeadDepartment,
		CoDepartments:  chosen.CoDepartments,
		RuleVersion:    chosen.Version,
		AdjudicatedAt:  now,
		AdjudicatedBy:  "system",
		IsCurrent:      true,
		DataVersion:    domain.DataVersion,
	}, nil
}

func (a *Adjudicator) SelectEscalationLead(currentLead string, level int) string {
	return domain.EscalationDepartmentName(currentLead, level)
}
