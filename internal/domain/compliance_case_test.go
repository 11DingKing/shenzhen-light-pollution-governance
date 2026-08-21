package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateMachine_ValidTransitions(t *testing.T) {
	valid := []struct {
		from, to ItemStatus
	}{
		{StatusRegistered, StatusAdjudicated},
		{StatusRegistered, StatusCancelled},
		{StatusRegistered, StatusReturned},
		{StatusAdjudicated, StatusInProgress},
		{StatusAdjudicated, StatusCancelled},
		{StatusAdjudicated, StatusReturned},
		{StatusAdjudicated, StatusEscalated},
		{StatusInProgress, StatusCompleted},
		{StatusInProgress, StatusCancelled},
		{StatusInProgress, StatusEscalated},
		{StatusReturned, StatusRegistered},
		{StatusReturned, StatusCancelled},
		{StatusEscalated, StatusInProgress},
		{StatusEscalated, StatusCompleted},
		{StatusEscalated, StatusCancelled},
	}
	for _, tc := range valid {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			assert.True(t, CanTransition(tc.from, tc.to))
			assert.NoError(t, ValidateTransition(tc.from, tc.to))
		})
	}
}

func TestStateMachine_InvalidTransitionReject(t *testing.T) {
	invalid := []struct {
		from, to ItemStatus
	}{
		{StatusCompleted, StatusInProgress},
		{StatusCompleted, StatusAdjudicated},
		{StatusCompleted, StatusCancelled},
		{StatusCancelled, StatusInProgress},
		{StatusCancelled, StatusCompleted},
		{StatusRegistered, StatusCompleted},
		{StatusRegistered, StatusInProgress},
		{StatusInProgress, StatusRegistered},
		{StatusReturned, StatusInProgress},
		{StatusEscalated, StatusReturned},
		{StatusEscalated, StatusRegistered},
	}
	for _, tc := range invalid {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			assert.False(t, CanTransition(tc.from, tc.to))
			err := ValidateTransition(tc.from, tc.to)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrInvalidTransition)
		})
	}
}

func TestStateMachine_TerminalStatesReject(t *testing.T) {
	for _, terminal := range []ItemStatus{StatusCompleted, StatusCancelled} {
		assert.True(t, terminal.IsTerminal())
		for _, target := range []ItemStatus{StatusRegistered, StatusAdjudicated, StatusInProgress, StatusEscalated, StatusReturned, StatusCompleted, StatusCancelled} {
			if target == terminal {
				continue
			}
			assert.False(t, CanTransition(terminal, target))
		}
	}
}

func TestStateMachine_SameStatusAllowed(t *testing.T) {
	for _, status := range []ItemStatus{StatusRegistered, StatusAdjudicated, StatusInProgress, StatusEscalated, StatusReturned, StatusCompleted, StatusCancelled} {
		assert.NoError(t, ValidateTransition(status, status))
	}
}

func TestItem_IsOverdue(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	item := &ComplianceCase{Deadline: now.Add(-1 * time.Hour), Status: StatusInProgress}
	assert.True(t, item.IsOverdue(now))
	assert.False(t, item.IsOverdue(now.Add(-2*time.Hour)))

	completed := &ComplianceCase{Deadline: now.Add(-1 * time.Hour), Status: StatusCompleted}
	assert.False(t, completed.IsOverdue(now))
}

func TestItem_Validate(t *testing.T) {
	item := &ComplianceCase{ExternalRef: "REF-001", Title: "Test", RegisteredBy: "user1", Deadline: time.Now()}
	assert.NoError(t, item.Validate())

	bad := &ComplianceCase{ExternalRef: "", Title: "Test", RegisteredBy: "user1", Deadline: time.Now()}
	err := bad.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrValidation)
}

func TestRule_Matches(t *testing.T) {
	item := &ComplianceCase{Category: "tax", Keywords: []string{"property", "refund"}}
	rule := &Rule{
		MatchCategory: "tax",
		MatchKeywords: []string{"refund"},
		Status:        RuleStatusActive,
		EffectiveFrom: time.Now().Add(-1 * time.Hour),
	}
	assert.True(t, rule.Matches(item))

	noMatch := &Rule{
		MatchCategory: "education",
		Status:        RuleStatusActive,
		EffectiveFrom: time.Now().Add(-1 * time.Hour),
	}
	assert.False(t, noMatch.Matches(item))

	defaultRule := &Rule{IsDefault: true, Status: RuleStatusActive, EffectiveFrom: time.Now().Add(-1 * time.Hour)}
	assert.True(t, defaultRule.Matches(item))
}

func TestRule_IsActiveAt(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	rule := &Rule{
		Status:        RuleStatusActive,
		EffectiveFrom: now.Add(-1 * time.Hour),
		EffectiveTo:   now.Add(1 * time.Hour),
	}
	assert.True(t, rule.IsActiveAt(now))
	assert.False(t, rule.IsActiveAt(now.Add(-2*time.Hour)))
	assert.False(t, rule.IsActiveAt(now.Add(2*time.Hour)))

	superseded := &Rule{Status: RuleStatusSuperseded, EffectiveFrom: now.Add(-2 * time.Hour)}
	assert.False(t, superseded.IsActiveAt(now))
}

func TestEscalationDepartmentName(t *testing.T) {
	assert.Equal(t, "tax-bureau", EscalationDepartmentName("tax-bureau", 0))
	assert.Equal(t, "tax-bureau-supervisor-l1", EscalationDepartmentName("tax-bureau", 1))
	assert.Equal(t, "tax-bureau-supervisor-l3", EscalationDepartmentName("tax-bureau", 3))
}
