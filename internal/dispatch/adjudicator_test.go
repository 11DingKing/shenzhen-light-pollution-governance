package dispatch

import (
	"context"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lightgovernance/internal/domain"
)

func newTestClock() clock.Clock {
	return clock.NewMock()
}

func TestAdjudicator_UniqueLeadDepartment(t *testing.T) {
	clk := newTestClock()
	now := clk.Now()
	adj := NewAdjudicator(clk)

	item := &domain.ComplianceCase{
		ID:           "item-1",
		Category:     "tax",
		Keywords:     []string{"refund"},
		RegisteredAt: now,
	}

	rules := []*domain.Rule{
		{
			Version:        1,
			Name:           "tax-refund-rule",
			MatchCategory:  "tax",
			MatchKeywords:  []string{"refund"},
			LeadDepartment: "tax-bureau",
			Priority:       10,
			Status:         domain.RuleStatusActive,
			EffectiveFrom:  now.Add(-1 * time.Hour),
		},
	}

	asg, err := adj.Adjudicate(context.Background(), item, rules)
	require.NoError(t, err)
	assert.Equal(t, "tax-bureau", asg.LeadDepartment)
	assert.Equal(t, 1, asg.RuleVersion)
	assert.True(t, asg.IsCurrent)
	assert.Equal(t, "system", asg.AdjudicatedBy)
}

func TestAdjudicator_RuleVersionSelection(t *testing.T) {
	clk := newTestClock()
	now := clk.Now()
	adj := NewAdjudicator(clk)

	item := &domain.ComplianceCase{
		ID:           "item-v2",
		Category:     "tax",
		RegisteredAt: now,
	}

	rules := []*domain.Rule{
		{
			Version:        1,
			Name:           "old-rule",
			MatchCategory:  "tax",
			LeadDepartment: "old-tax-dept",
			Priority:       5,
			Status:         domain.RuleStatusSuperseded,
			EffectiveFrom:  now.Add(-48 * time.Hour),
			EffectiveTo:    now.Add(-1 * time.Hour),
		},
		{
			Version:        2,
			Name:           "new-rule",
			MatchCategory:  "tax",
			LeadDepartment: "new-tax-dept",
			Priority:       5,
			Status:         domain.RuleStatusActive,
			EffectiveFrom:  now.Add(-1 * time.Hour),
		},
	}

	asg, err := adj.Adjudicate(context.Background(), item, rules)
	require.NoError(t, err)
	assert.Equal(t, "new-tax-dept", asg.LeadDepartment)
	assert.Equal(t, 2, asg.RuleVersion)
}

func TestAdjudicator_PriorityOrdering(t *testing.T) {
	clk := newTestClock()
	now := clk.Now()
	adj := NewAdjudicator(clk)

	item := &domain.ComplianceCase{
		ID:           "item-prio",
		Category:     "tax",
		Keywords:     []string{"refund"},
		RegisteredAt: now,
	}

	rules := []*domain.Rule{
		{
			Version:        1,
			Name:           "low-priority",
			MatchCategory:  "tax",
			MatchKeywords:  []string{"refund"},
			LeadDepartment: "low-dept",
			Priority:       1,
			Status:         domain.RuleStatusActive,
			EffectiveFrom:  now.Add(-1 * time.Hour),
		},
		{
			Version:        2,
			Name:           "high-priority",
			MatchCategory:  "tax",
			MatchKeywords:  []string{"refund"},
			LeadDepartment: "high-dept",
			Priority:       100,
			Status:         domain.RuleStatusActive,
			EffectiveFrom:  now.Add(-1 * time.Hour),
		},
	}

	asg, err := adj.Adjudicate(context.Background(), item, rules)
	require.NoError(t, err)
	assert.Equal(t, "high-dept", asg.LeadDepartment)
}

func TestAdjudicator_NoMatchingRule(t *testing.T) {
	clk := newTestClock()
	now := clk.Now()
	adj := NewAdjudicator(clk)

	item := &domain.ComplianceCase{
		ID:           "item-nomatch",
		Category:     "unknown",
		RegisteredAt: now,
	}

	rules := []*domain.Rule{
		{
			Version:        1,
			Name:           "tax-only",
			MatchCategory:  "tax",
			LeadDepartment: "tax-bureau",
			Priority:       10,
			Status:         domain.RuleStatusActive,
			EffectiveFrom:  now.Add(-1 * time.Hour),
		},
	}

	_, err := adj.Adjudicate(context.Background(), item, rules)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrNoMatchingRule)
}

func TestAdjudicator_DefaultRuleFallback(t *testing.T) {
	clk := newTestClock()
	now := clk.Now()
	adj := NewAdjudicator(clk)

	item := &domain.ComplianceCase{
		ID:           "item-default",
		Category:     "unknown",
		RegisteredAt: now,
	}

	rules := []*domain.Rule{
		{
			Version:        1,
			Name:           "specific-rule",
			MatchCategory:  "tax",
			LeadDepartment: "tax-bureau",
			Priority:       10,
			Status:         domain.RuleStatusActive,
			EffectiveFrom:  now.Add(-1 * time.Hour),
		},
		{
			Version:        2,
			Name:           "default-rule",
			LeadDepartment: "general-bureau",
			IsDefault:      true,
			Priority:       0,
			Status:         domain.RuleStatusActive,
			EffectiveFrom:  now.Add(-1 * time.Hour),
		},
	}

	asg, err := adj.Adjudicate(context.Background(), item, rules)
	require.NoError(t, err)
	assert.Equal(t, "general-bureau", asg.LeadDepartment)
	assert.Equal(t, 2, asg.RuleVersion)
}
