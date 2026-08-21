package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lightgovernance/internal/dispatch"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/repo"
	"lightgovernance/internal/service"
)

func setupService(t *testing.T) (*service.ItemService, *service.EscalationService,
	*service.BatchService, *service.RuleService, *repo.Store, *clock.Mock, context.Context) {
	t.Helper()
	clk := clock.NewMock()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := repo.New(ctx, dir, clk, 1024*1024, true)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })

	ruleSvc := service.NewRuleService(st, clk)
	_, err = ruleSvc.CreateRule(ctx, service.CreateRuleRequest{
		Name:           "default-rule",
		LeadDepartment: "general-bureau",
		IsDefault:      true,
		CreatedBy:      "test",
	})
	require.NoError(t, err)

	adj := dispatch.NewAdjudicator(clk)
	itemSvc := service.NewItemService(st, adj, clk, 72*time.Hour)
	escSvc := service.NewEscalationService(st, clk, 48*time.Hour, 3)
	batchSvc := service.NewBatchService(st, itemSvc, clk)
	return itemSvc, escSvc, batchSvc, ruleSvc, st, clk, ctx
}

func TestItemService_Register(t *testing.T) {
	itemSvc, _, _, _, _, _, ctx := setupService(t)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-001",
		Title:        "Test ComplianceCase",
		RegisteredBy: "user1",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.StatusAdjudicated, item.Status)
	assert.Equal(t, "general-bureau", item.LeadDepartment)
	assert.Equal(t, 1, item.RuleVersion)
	assert.False(t, item.Deadline.IsZero())
}

func TestItemService_IdempotentDuplicateRegister(t *testing.T) {
	itemSvc, _, _, _, _, _, ctx := setupService(t)

	req := service.RegisterItemRequest{
		ExternalRef:  "REF-DUP-001",
		Title:        "Duplicate Test",
		RegisteredBy: "user1",
	}

	item1, err := itemSvc.Register(ctx, req)
	require.NoError(t, err)

	item2, err := itemSvc.Register(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, item1.ID, item2.ID)
}

func TestItemService_CompleteLifecycle(t *testing.T) {
	itemSvc, _, _, _, _, _, ctx := setupService(t)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-LIFE-001",
		Title:        "Lifecycle Test",
		RegisteredBy: "user1",
	})
	require.NoError(t, err)

	_, err = itemSvc.StartProcessing(ctx, item.ID, "user1")
	require.NoError(t, err)

	completed, err := itemSvc.Complete(ctx, item.ID, "user1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCompleted, completed.Status)
	assert.NotNil(t, completed.CompletedAt)
}

func TestItemService_Cancel(t *testing.T) {
	itemSvc, _, _, _, _, _, ctx := setupService(t)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-CANCEL-001",
		Title:        "Cancel Test",
		RegisteredBy: "user1",
	})
	require.NoError(t, err)

	cancelled, err := itemSvc.Cancel(ctx, item.ID, "wrong item", "user1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCancelled, cancelled.Status)
	assert.NotNil(t, cancelled.CancelledAt)
	assert.Equal(t, "wrong item", cancelled.CancelReason)
}

func TestItemService_ReturnAndResubmit(t *testing.T) {
	itemSvc, _, _, _, _, _, ctx := setupService(t)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-RETURN-001",
		Title:        "Return Test",
		RegisteredBy: "user1",
	})
	require.NoError(t, err)

	returned, err := itemSvc.ReturnForCorrection(ctx, item.ID, "needs docs", "user1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusReturned, returned.Status)

	resubmitted, err := itemSvc.Resubmit(ctx, item.ID, "user1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusAdjudicated, resubmitted.Status)
}

func TestItemService_IllegalTransitionReject(t *testing.T) {
	itemSvc, _, _, _, _, _, ctx := setupService(t)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-TRANS-001",
		Title:        "Transition Test",
		RegisteredBy: "user1",
	})
	require.NoError(t, err)

	_, err = itemSvc.StartProcessing(ctx, item.ID, "user1")
	require.NoError(t, err)

	_, err = itemSvc.Complete(ctx, item.ID, "user1")
	require.NoError(t, err)

	_, err = itemSvc.StartProcessing(ctx, item.ID, "user1")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrInvalidTransition)

	_, err = itemSvc.Cancel(ctx, item.ID, "late cancel", "user1")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrInvalidTransition)
}

func TestItemService_TransactionRollbackOnFailure(t *testing.T) {
	itemSvc, _, _, _, _, _, ctx := setupService(t)

	_, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-ROLLBACK-001",
		Title:        "Rollback Test",
		RegisteredBy: "user1",
	})
	require.NoError(t, err)

	_, err = itemSvc.Cancel(ctx, "nonexistent-id", "reason", "user1")
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestEscalationService_OverdueUpgradePersist(t *testing.T) {
	itemSvc, escSvc, _, _, st, clk, ctx := setupService(t)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-ESC-001",
		Title:        "Escalation Test",
		RegisteredBy: "user1",
	})
	require.NoError(t, err)

	_, err = itemSvc.StartProcessing(ctx, item.ID, "user1")
	require.NoError(t, err)

	clk.Add(73 * time.Hour)

	escs, err := escSvc.CheckAndEscalate(ctx)
	require.NoError(t, err)
	require.Len(t, escs, 1)

	loaded, err := st.GetItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusEscalated, loaded.Status)
	assert.Equal(t, 1, loaded.EscalationLevel)
	assert.Contains(t, loaded.LeadDepartment, "supervisor-l1")

	escList, err := st.GetEscalations(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, escList, 1)
	assert.Equal(t, 1, escList[0].ToLevel)
	current, err := st.GetCurrentAssignment(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, loaded.LeadDepartment, current.LeadDepartment)
	assert.True(t, current.IsCurrent)
}

func TestEscalationService_MultiLevelEscalation(t *testing.T) {
	itemSvc, escSvc, _, _, st, clk, ctx := setupService(t)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-ESC-MULTI",
		Title:        "Multi Escalation",
		RegisteredBy: "user1",
	})
	require.NoError(t, err)
	_, err = itemSvc.StartProcessing(ctx, item.ID, "user1")
	require.NoError(t, err)

	clk.Add(73 * time.Hour)
	_, err = escSvc.CheckAndEscalate(ctx)
	require.NoError(t, err)

	clk.Add(49 * time.Hour)
	_, err = escSvc.CheckAndEscalate(ctx)
	require.NoError(t, err)

	loaded, err := st.GetItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, loaded.EscalationLevel)
	assert.Contains(t, loaded.LeadDepartment, "supervisor-l2")
}
