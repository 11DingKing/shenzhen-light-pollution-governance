package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"lightgovernance/internal/applog"
	"lightgovernance/internal/config"
	"lightgovernance/internal/dispatch"
	"lightgovernance/internal/domain"
	"lightgovernance/internal/repo"
	"lightgovernance/internal/service"
)

func setupSchedulerTest(t *testing.T) (*Scheduler, *repo.Store, *clock.Mock, context.Context) {
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

	cfg := config.SchedulerConfig{
		EscalationInterval:     1 * time.Second,
		ReconciliationInterval: 5 * time.Second,
		MaxRetries:             3,
		BaseBackoff:            10 * time.Millisecond,
		TaskTimeout:            5 * time.Second,
	}
	logger := applog.New("error", "json")
	sched := New(clk, escSvc, st, cfg, logger)
	_ = itemSvc
	return sched, st, clk, ctx
}

func TestScheduler_EscalationOverdueUpgrade(t *testing.T) {
	sched, st, clk, ctx := setupSchedulerTest(t)

	adj := dispatch.NewAdjudicator(clk)
	itemSvc := service.NewItemService(st, adj, clk, 72*time.Hour)

	item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
		ExternalRef:  "REF-SCHED-001",
		Title:        "Scheduler Escalation",
		RegisteredBy: "user1",
	})
	require.NoError(t, err)

	_, err = itemSvc.StartProcessing(ctx, item.ID, "user1")
	require.NoError(t, err)

	clk.Add(73 * time.Hour)

	err = sched.RunEscalationOnce(ctx)
	require.NoError(t, err)

	loaded, err := st.GetItem(ctx, item.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusEscalated, loaded.Status)
	assert.Equal(t, 1, loaded.EscalationLevel)
}

func TestScheduler_ConcurrentEscalation(t *testing.T) {
	sched, st, clk, ctx := setupSchedulerTest(t)

	adj := dispatch.NewAdjudicator(clk)
	itemSvc := service.NewItemService(st, adj, clk, 72*time.Hour)

	for i := 0; i < 5; i++ {
		item, err := itemSvc.Register(ctx, service.RegisterItemRequest{
			ExternalRef:  "REF-SCHED-CONCURRENT-" + string(rune('A'+i)),
			Title:        "Concurrent Esc " + string(rune('A'+i)),
			RegisteredBy: "user1",
		})
		require.NoError(t, err)
		_, err = itemSvc.StartProcessing(ctx, item.ID, "user1")
		require.NoError(t, err)
	}

	clk.Add(73 * time.Hour)

	err := sched.RunEscalationOnce(ctx)
	require.NoError(t, err)

	items, err := st.FindOverdueItems(ctx, clk.Now(), 3)
	require.NoError(t, err)
	for _, item := range items {
		assert.Equal(t, domain.StatusEscalated, item.Status)
		assert.Equal(t, 1, item.EscalationLevel)
	}
}

func TestScheduler_BackoffCalculation(t *testing.T) {
	sched := &Scheduler{
		config: config.SchedulerConfig{BaseBackoff: 100 * time.Millisecond},
	}
	assert.Equal(t, 100*time.Millisecond, sched.calculateBackoff(1))
	assert.Equal(t, 200*time.Millisecond, sched.calculateBackoff(2))
	assert.Equal(t, 400*time.Millisecond, sched.calculateBackoff(3))
	assert.Equal(t, 800*time.Millisecond, sched.calculateBackoff(4))
}

func TestScheduler_StartAndStop(t *testing.T) {
	sched, _, _, _ := setupSchedulerTest(t)

	err := sched.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, sched.Started())

	time.Sleep(50 * time.Millisecond)

	sched.Stop()
	assert.False(t, sched.Started())
}
