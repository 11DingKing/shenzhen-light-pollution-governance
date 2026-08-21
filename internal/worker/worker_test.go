package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lightgovernance/internal/applog"
	"lightgovernance/internal/auditlog"
	"lightgovernance/internal/config"
	"lightgovernance/internal/dispatch"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/repo"
	"lightgovernance/internal/service"
)

type fakeTicker struct{ ch chan time.Time }

func (f *fakeTicker) C() <-chan time.Time { return f.ch }
func (f *fakeTicker) Stop()               {}

func testWorkerCfg() config.SchedulerConfig {
	return config.SchedulerConfig{
		ReevalInterval:         1 * time.Second,
		EscalationInterval:     1 * time.Second,
		ReconciliationInterval: 5 * time.Second,
		MaxRetries:             3,
		BaseBackoff:            10 * time.Millisecond,
		TaskTimeout:            5 * time.Second,
	}
}

func setupWorkerTest(t *testing.T) (*ReevalWorker, *repo.Store, *clock.Mock, context.Context) {
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

	w := New(clk, dispatch.NewAdjudicator(clk), st, testWorkerCfg(), applog.New("error", "json"))
	return w, st, clk, ctx
}

func TestWorker_RuleVersionEvolutionReeval(t *testing.T) {
	w, st, clk, ctx := setupWorkerTest(t)
	itemSvc := service.NewItemService(st, dispatch.NewAdjudicator(clk), clk, 72*time.Hour)
	ruleSvc := service.NewRuleService(st, clk)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-REEVAL-001",
		Title:        "Reeval ComplianceCase",
		RegisteredBy: "u1",
	})
	require.NoError(t, err)
	require.Equal(t, 1, item.RuleVersion)
	require.Equal(t, "general-bureau", item.LeadDepartment)

	rule2, err := ruleSvc.CreateRule(ctx, service.CreateRuleRequest{
		Name:             "default-rule-v2",
		LeadDepartment:   "special-bureau",
		IsDefault:        true,
		CreatedBy:        "admin",
		SupersedeVersion: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 2, rule2.Version)

	require.NoError(t, w.RunOnce(ctx))

	loaded, err := st.GetItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, loaded.RuleVersion)
	assert.Equal(t, "special-bureau", loaded.LeadDepartment)
	assert.Equal(t, domain.StatusAdjudicated, loaded.Status)

	assigns, err := st.GetAssignments(ctx, item.ID)
	require.NoError(t, err)
	require.Len(t, assigns, 2)
	current, err := st.GetCurrentAssignment(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, current.RuleVersion)
	assert.True(t, current.IsCurrent)

	audits, _, err := st.ListAudit(ctx, domain.AuditFilter{EntityID: item.ID, PageSize: 50})
	require.NoError(t, err)
	found := false
	for _, a := range audits {
		if a.Action == auditlog.ActionRuleReevaluated {
			found = true
		}
	}
	assert.True(t, found, "expected rule_reevaluated audit entry")
}

func TestWorker_RestartRecoverReplayPersist(t *testing.T) {
	clk := clock.NewMock()
	ctx := context.Background()
	dir := t.TempDir()

	st, err := repo.New(ctx, dir, clk, 1024*1024, true)
	require.NoError(t, err)
	ruleSvc := service.NewRuleService(st, clk)
	_, err = ruleSvc.CreateRule(ctx, service.CreateRuleRequest{
		Name: "default", LeadDepartment: "general-bureau", IsDefault: true, CreatedBy: "test",
	})
	require.NoError(t, err)
	itemSvc := service.NewItemService(st, dispatch.NewAdjudicator(clk), clk, 72*time.Hour)
	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef: "REF-RESTART", Title: "Restart ComplianceCase", RegisteredBy: "u1",
	})
	require.NoError(t, err)
	_, err = ruleSvc.CreateRule(ctx, service.CreateRuleRequest{
		Name: "default-v2", LeadDepartment: "restart-bureau", IsDefault: true,
		CreatedBy: "admin", SupersedeVersion: 1,
	})
	require.NoError(t, err)
	require.NoError(t, st.Close())

	st2, err := repo.New(ctx, dir, clk, 1024*1024, true)
	require.NoError(t, err)
	t.Cleanup(func() { st2.Close() })

	w := New(clk, dispatch.NewAdjudicator(clk), st2, testWorkerCfg(), applog.New("error", "json"))
	require.NoError(t, w.RunOnce(ctx))

	loaded, err := st2.GetItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, loaded.RuleVersion)
	assert.Equal(t, "restart-bureau", loaded.LeadDepartment)
	assert.Equal(t, int64(1), w.Reevaluated())
}

func TestWorker_IdempotentDuplicateRetry(t *testing.T) {
	w, st, clk, ctx := setupWorkerTest(t)
	itemSvc := service.NewItemService(st, dispatch.NewAdjudicator(clk), clk, 72*time.Hour)
	ruleSvc := service.NewRuleService(st, clk)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef: "REF-IDEMP", Title: "Idempotent", RegisteredBy: "u1",
	})
	require.NoError(t, err)
	_, err = ruleSvc.CreateRule(ctx, service.CreateRuleRequest{
		Name: "default-v2", LeadDepartment: "idemp-bureau", IsDefault: true,
		CreatedBy: "admin", SupersedeVersion: 1,
	})
	require.NoError(t, err)

	require.NoError(t, w.RunOnce(ctx))
	require.NoError(t, w.RunOnce(ctx))
	require.NoError(t, w.RunOnce(ctx))

	loaded, err := st.GetItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, loaded.RuleVersion)
	assigns, err := st.GetAssignments(ctx, item.ID)
	require.NoError(t, err)
	assert.Len(t, assigns, 2, "no duplicate assignments from repeated sweeps")
	assert.Equal(t, int64(1), w.Reevaluated())
}

func TestWorker_ConcurrentParallelRace(t *testing.T) {
	w, st, clk, ctx := setupWorkerTest(t)
	itemSvc := service.NewItemService(st, dispatch.NewAdjudicator(clk), clk, 72*time.Hour)
	ruleSvc := service.NewRuleService(st, clk)

	const n = 10
	itemIDs := make([]string, n)
	for i := 0; i < n; i++ {
		it, err := itemSvc.Register(ctx, service.RegisterItemRequest{
			ExternalRef: fmt.Sprintf("REF-RACE-%d", i), Title: "Race", RegisteredBy: "u1",
		})
		require.NoError(t, err)
		itemIDs[i] = it.ID
	}
	_, err := ruleSvc.CreateRule(ctx, service.CreateRuleRequest{
		Name: "default-v2", LeadDepartment: "race-bureau", IsDefault: true,
		CreatedBy: "admin", SupersedeVersion: 1,
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.RunOnce(ctx)
		}()
	}
	wg.Wait()

	for _, id := range itemIDs {
		loaded, err := st.GetItem(ctx, id)
		require.NoError(t, err)
		assert.Equalf(t, 2, loaded.RuleVersion, "item %s not re-evaluated", id)
		assert.Equal(t, "race-bureau", loaded.LeadDepartment)
		current, err := st.GetCurrentAssignment(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, 2, current.RuleVersion)
		assigns, err := st.GetAssignments(ctx, id)
		require.NoError(t, err)
		currentCount := 0
		for _, a := range assigns {
			if a.IsCurrent {
				currentCount++
			}
		}
		assert.Equalf(t, 1, currentCount, "item %s has %d current assignments", id, currentCount)
	}
	assert.GreaterOrEqual(t, w.Reevaluated(), int64(n))
}

func TestWorker_TerminalItemNotReevaluated(t *testing.T) {
	w, st, clk, ctx := setupWorkerTest(t)
	itemSvc := service.NewItemService(st, dispatch.NewAdjudicator(clk), clk, 72*time.Hour)
	ruleSvc := service.NewRuleService(st, clk)

	active, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef: "REF-ACTIVE", Title: "Active Reeval", RegisteredBy: "u1",
	})
	require.NoError(t, err)
	terminal, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef: "REF-TERMINAL", Title: "Terminal Reeval", RegisteredBy: "u1",
	})
	require.NoError(t, err)
	_, err = itemSvc.StartProcessing(ctx, terminal.ID, "u1")
	require.NoError(t, err)
	_, err = itemSvc.Complete(ctx, terminal.ID, "u1")
	require.NoError(t, err)

	_, err = ruleSvc.CreateRule(ctx, service.CreateRuleRequest{
		Name: "default-v2", LeadDepartment: "v2-bureau", IsDefault: true,
		CreatedBy: "admin", SupersedeVersion: 1,
	})
	require.NoError(t, err)

	require.NoError(t, w.RunOnce(ctx))

	loadedActive, err := st.GetItem(ctx, active.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, loadedActive.RuleVersion)
	assert.Equal(t, "v2-bureau", loadedActive.LeadDepartment)

	loadedTerm, err := st.GetItem(ctx, terminal.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCompleted, loadedTerm.Status)
	assert.Equal(t, 1, loadedTerm.RuleVersion, "terminal item must not be re-routed")
	assert.Equal(t, int64(1), w.Reevaluated())
}

func TestWorker_NoMatchingRuleSkipsItem(t *testing.T) {
	w, st, clk, ctx := setupWorkerTest(t)
	itemSvc := service.NewItemService(st, dispatch.NewAdjudicator(clk), clk, 72*time.Hour)
	ruleSvc := service.NewRuleService(st, clk)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef: "REF-NOMATCH", Title: "No Match", Category: "category-y", RegisteredBy: "u1",
	})
	require.NoError(t, err)
	_, err = ruleSvc.CreateRule(ctx, service.CreateRuleRequest{
		Name: "cat-x-rule", LeadDepartment: "cat-x-bureau", MatchCategory: "category-x",
		CreatedBy: "admin", SupersedeVersion: 1,
	})
	require.NoError(t, err)

	require.NoError(t, w.RunOnce(ctx))

	loaded, err := st.GetItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, loaded.RuleVersion, "item with no matching rule keeps old routing")
	assert.Equal(t, "general-bureau", loaded.LeadDepartment)
	assert.Equal(t, int64(0), w.Reevaluated())
	assert.GreaterOrEqual(t, w.Skipped(), int64(1))
}

func TestWorker_ErrorChainWrapping(t *testing.T) {
	clk := clock.NewMock()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := repo.New(ctx, dir, clk, 1024*1024, true)
	require.NoError(t, err)
	require.NoError(t, st.Close())

	w := New(clk, dispatch.NewAdjudicator(clk), st, testWorkerCfg(), applog.New("error", "json"))
	err = w.RunOnce(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRuleReeval), "error should wrap ErrRuleReeval")
}

func TestWorker_StartStopGraceful(t *testing.T) {
	w, _, _, _ := setupWorkerTest(t)
	w.newTicker = func(time.Duration) Ticker {
		return &fakeTicker{ch: make(chan time.Time, 1)}
	}
	require.NoError(t, w.Start(context.Background()))
	assert.True(t, w.Started())
	w.Stop()
	assert.False(t, w.Started())
}
