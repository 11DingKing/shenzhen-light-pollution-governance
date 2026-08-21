package repo

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"lightgovernance/internal/domain"
	"lightgovernance/internal/store"
)

func newTestStore(t *testing.T, dir string, clk clock.Clock) *Store {
	t.Helper()
	ctx := context.Background()
	st, err := New(ctx, dir, clk, 1024*1024, true)
	require.NoError(t, err)
	return st
}

func makeItem(id, ref string, now time.Time) *domain.ComplianceCase {
	return &domain.ComplianceCase{
		ID:             id,
		ExternalRef:    ref,
		Title:          "ComplianceCase " + id,
		Status:         domain.StatusAdjudicated,
		LeadDepartment: "test-dept",
		RegisteredAt:   now,
		RegisteredBy:   "user1",
		Deadline:       now.Add(72 * time.Hour),
		DataVersion:    domain.DataVersion,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func saveItemWithAssignment(t *testing.T, ctx context.Context, st *Store, item *domain.ComplianceCase) {
	t.Helper()
	now := item.RegisteredAt
	asg := &domain.Referral{
		ID:             uuid.NewString(),
		ItemID:         item.ID,
		LeadDepartment: item.LeadDepartment,
		RuleVersion:    1,
		AdjudicatedAt:  now,
		IsCurrent:      true,
		DataVersion:    domain.DataVersion,
	}
	audit := &domain.AuditEntry{
		ID:          uuid.NewString(),
		EntityID:    item.ID,
		EntityType:  "item",
		Action:      "register",
		Actor:       "user1",
		Timestamp:   now,
		DataVersion: domain.DataVersion,
	}
	err := st.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.SaveItem(ctx, item); err != nil {
			return err
		}
		if err := tx.SaveAssignment(ctx, asg); err != nil {
			return err
		}
		return tx.SaveAudit(ctx, audit)
	})
	require.NoError(t, err)
}

func TestRepo_PersistAndRecoverRestart(t *testing.T) {
	clk := clock.NewMock()
	ctx := context.Background()
	dir := t.TempDir()
	now := clk.Now()

	st1 := newTestStore(t, dir, clk)
	item := makeItem("restart-1", "REF-RESTART-1", now)
	saveItemWithAssignment(t, ctx, st1, item)
	st1.Close()

	st2 := newTestStore(t, dir, clk)
	defer st2.Close()

	loaded, err := st2.GetItem(ctx, "restart-1")
	require.NoError(t, err)
	assert.Equal(t, "ComplianceCase restart-1", loaded.Title)
	assert.Equal(t, domain.StatusAdjudicated, loaded.Status)

	asgs, err := st2.GetAssignments(ctx, "restart-1")
	require.NoError(t, err)
	require.Len(t, asgs, 1)
	assert.True(t, asgs[0].IsCurrent)
}

func TestRepo_TransactionCommitRollback(t *testing.T) {
	clk := clock.NewMock()
	ctx := context.Background()
	st := newTestStore(t, t.TempDir(), clk)
	defer st.Close()
	now := clk.Now()

	committed := makeItem("commit-1", "REF-COMMIT-1", now)
	err := st.WithTx(ctx, func(tx store.Tx) error {
		return tx.SaveItem(ctx, committed)
	})
	require.NoError(t, err)

	loaded, err := st.GetItem(ctx, "commit-1")
	require.NoError(t, err)
	assert.Equal(t, "ComplianceCase commit-1", loaded.Title)

	rolledBack := makeItem("rollback-1", "REF-ROLLBACK-1", now)
	err = st.WithTx(ctx, func(tx store.Tx) error {
		if err := tx.SaveItem(ctx, rolledBack); err != nil {
			return err
		}
		return fmt.Errorf("simulated failure for rollback test")
	})
	require.Error(t, err)

	_, err = st.GetItem(ctx, "rollback-1")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestRepo_ConcurrentRaceParallel(t *testing.T) {
	clk := clock.NewMock()
	ctx := context.Background()
	st := newTestStore(t, t.TempDir(), clk)
	defer st.Close()
	now := clk.Now()

	var wg sync.WaitGroup
	const n = 30
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			item := makeItem(fmt.Sprintf("race-%d", idx), fmt.Sprintf("REF-RACE-%d", idx), now)
			_ = st.WithTx(ctx, func(tx store.Tx) error {
				return tx.SaveItem(ctx, item)
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		item, err := st.GetItem(ctx, fmt.Sprintf("race-%d", i))
		require.NoError(t, err, "item %d should exist", i)
		assert.Equal(t, fmt.Sprintf("ComplianceCase race-%d", i), item.Title)
	}

	_, count, _ := st.ListItems(ctx, domain.ItemFilter{PageSize: 200})
	assert.Equal(t, n, count)
}

func TestRepo_RebuildIndexReplay(t *testing.T) {
	clk := clock.NewMock()
	ctx := context.Background()
	dir := t.TempDir()
	now := clk.Now()

	st1 := newTestStore(t, dir, clk)
	for i := 0; i < 5; i++ {
		item := makeItem(fmt.Sprintf("rebuild-%d", i), fmt.Sprintf("REF-REBUILD-%d", i), now)
		saveItemWithAssignment(t, ctx, st1, item)
	}
	st1.Close()

	st2 := newTestStore(t, dir, clk)
	defer st2.Close()

	report, err := st2.RebuildIndex(ctx)
	require.NoError(t, err)
	assert.Equal(t, 15, report.TotalRecords)

	for i := 0; i < 5; i++ {
		item, err := st2.GetItem(ctx, fmt.Sprintf("rebuild-%d", i))
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("ComplianceCase rebuild-%d", i), item.Title)
	}
}

func TestRepo_ShardCorruptionSkip(t *testing.T) {
	clk := clock.NewMock()
	ctx := context.Background()
	dir := t.TempDir()
	now := clk.Now()

	st := newTestStore(t, dir, clk)
	defer st.Close()

	item := makeItem("corrupt-1", "REF-CORRUPT-1", now)
	saveItemWithAssignment(t, ctx, st, item)

	files, err := st.scanner.ScanEntityType(ctx, "items")
	require.NoError(t, err)
	require.NotEmpty(t, files)

	err = os.WriteFile(files[0].Path, []byte("{{{corrupted json}}}\n"), 0o644)
	require.NoError(t, err)

	report, err := st.RebuildIndex(ctx)
	require.NoError(t, err)
	assert.Greater(t, report.TotalShards, 0)

	_, err = st.GetItem(ctx, "corrupt-1")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestRepo_ReconcileReport(t *testing.T) {
	clk := clock.NewMock()
	ctx := context.Background()
	dir := t.TempDir()
	now := clk.Now()

	st := newTestStore(t, dir, clk)
	defer st.Close()

	item := makeItem("recon-1", "REF-RECON-1", now)
	saveItemWithAssignment(t, ctx, st, item)

	report, err := st.Reconcile(ctx)
	require.NoError(t, err)
	assert.Greater(t, report.ShardCount, 0)
	assert.Equal(t, 0, report.ChecksumMismatches)
}

func TestRepo_Diagnose(t *testing.T) {
	clk := clock.NewMock()
	ctx := context.Background()
	dir := t.TempDir()
	now := clk.Now()

	st := newTestStore(t, dir, clk)
	defer st.Close()

	item := makeItem("diag-1", "REF-DIAG-1", now)
	saveItemWithAssignment(t, ctx, st, item)

	report, err := st.Diagnose(ctx)
	require.NoError(t, err)
	assert.True(t, report.DataDirWritable)
	assert.Equal(t, 1, report.ItemCount)
	assert.True(t, report.Ready)
}
