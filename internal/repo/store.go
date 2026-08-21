package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"lightgovernance/internal/domain"
	"lightgovernance/internal/index"
	"lightgovernance/internal/shard"
	"lightgovernance/internal/store"
)

type Store struct {
	writer  *shard.Writer
	reader  *shard.Reader
	scanner *shard.Scanner
	index   *index.Index
	clock   domain.Clock
	dataDir string
}

func New(ctx context.Context, dataDir string, clock domain.Clock, maxShardSize int64, syncOnWrite bool) (*Store, error) {
	writer := shard.NewWriter(dataDir, clock, maxShardSize, syncOnWrite)
	if err := writer.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("ensure shard dirs: %w", err)
	}
	dbPath := filepath.Join(dataDir, "index.db")
	idx, err := index.Open(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	return &Store{
		writer:  writer,
		reader:  shard.NewReader(),
		scanner: shard.NewScanner(dataDir),
		index:   idx,
		clock:   clock,
		dataDir: dataDir,
	}, nil
}

func (s *Store) WithTx(ctx context.Context, fn func(store.Tx) error) (err error) {
	idxTx, txErr := s.index.BeginTx(ctx)
	if txErr != nil {
		return fmt.Errorf("begin index tx: %w", txErr)
	}
	tx := &storeTx{tx: idxTx, writer: s.writer, clock: s.clock}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return s.index.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.index.Ping(ctx) }

func (s *Store) GetItem(ctx context.Context, id string) (*domain.ComplianceCase, error) {
	return s.index.GetItemByID(ctx, id)
}

func (s *Store) GetItemByExternalRef(ctx context.Context, ref string) (*domain.ComplianceCase, error) {
	return s.index.GetItemByExternalRef(ctx, ref)
}

func (s *Store) ListItems(ctx context.Context, filter domain.ItemFilter) ([]*domain.ComplianceCase, int, error) {
	return s.index.ListItems(ctx, filter)
}

func (s *Store) FindOverdueItems(ctx context.Context, now time.Time, maxLevel int) ([]*domain.ComplianceCase, error) {
	return s.index.FindOverdueItems(ctx, now, maxLevel)
}

func (s *Store) CountByStatus(ctx context.Context) (map[domain.ItemStatus]int, error) {
	return s.index.CountByStatus(ctx)
}

func (s *Store) GetRule(ctx context.Context, version int) (*domain.Rule, error) {
	return s.index.GetRuleByVersion(ctx, version)
}

func (s *Store) GetActiveRules(ctx context.Context, at time.Time) ([]*domain.Rule, error) {
	return s.index.GetActiveRules(ctx, at)
}

func (s *Store) ListRules(ctx context.Context) ([]*domain.Rule, error) {
	return s.index.ListRules(ctx)
}

func (s *Store) GetCurrentRuleVersion(ctx context.Context) (int, error) {
	return s.index.GetCurrentRuleVersion(ctx)
}

func (s *Store) GetAssignments(ctx context.Context, itemID string) ([]*domain.Referral, error) {
	return s.index.GetAssignmentsByItemID(ctx, itemID)
}

func (s *Store) GetCurrentAssignment(ctx context.Context, itemID string) (*domain.Referral, error) {
	return s.index.GetCurrentAssignment(ctx, itemID)
}

func (s *Store) GetEscalations(ctx context.Context, itemID string) ([]*domain.Escalation, error) {
	return s.index.GetEscalationsByItemID(ctx, itemID)
}

func (s *Store) ListAudit(ctx context.Context, filter domain.AuditFilter) ([]*domain.AuditEntry, int, error) {
	return s.index.ListAudit(ctx, filter)
}

func (s *Store) ListFailures(ctx context.Context) ([]*domain.PermanentFailure, error) {
	return s.index.ListFailures(ctx)
}

func (s *Store) GetFailure(ctx context.Context, id string) (*domain.PermanentFailure, error) {
	return s.index.GetFailureByID(ctx, id)
}

func (s *Store) ListBatches(ctx context.Context, filter domain.BatchFilter) ([]*domain.ImportBatch, int, error) {
	return s.index.ListBatches(ctx, filter)
}

type storeTx struct {
	tx     *index.Tx
	writer *shard.Writer
	clock  domain.Clock
}

func (t *storeTx) Commit() error { return t.tx.Commit() }

func (t *storeTx) Rollback() error { return t.tx.Rollback() }

func (t *storeTx) SaveItem(ctx context.Context, item *domain.ComplianceCase) error {
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshal item: %w", err)
	}
	loc, err := t.writer.Append(ctx, "items", item.RegisteredAt, data)
	if err != nil {
		return fmt.Errorf("write item shard: %w", err)
	}
	item.ShardPath = loc.Path
	item.ShardOffset = loc.Offset
	return t.tx.InsertItem(ctx, item)
}

func (t *storeTx) UpdateItem(ctx context.Context, item *domain.ComplianceCase) error {
	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("marshal item: %w", err)
	}
	loc, err := t.writer.Append(ctx, "items", t.clock.Now(), data)
	if err != nil {
		return fmt.Errorf("write item update shard: %w", err)
	}
	item.ShardPath = loc.Path
	item.ShardOffset = loc.Offset
	return t.tx.UpdateItem(ctx, item)
}

func (t *storeTx) SaveAssignment(ctx context.Context, a *domain.Referral) error {
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("marshal referral: %w", err)
	}
	loc, err := t.writer.Append(ctx, "assignments", a.AdjudicatedAt, data)
	if err != nil {
		return fmt.Errorf("write referral shard: %w", err)
	}
	a.ShardPath = loc.Path
	return t.tx.InsertAssignment(ctx, a)
}

func (t *storeTx) MarkAssignmentSuperseded(ctx context.Context, itemID string) error {
	return t.tx.MarkAssignmentSuperseded(ctx, itemID)
}

func (t *storeTx) SaveEscalation(ctx context.Context, e *domain.Escalation) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal escalation: %w", err)
	}
	loc, err := t.writer.Append(ctx, "escalations", e.EscalatedAt, data)
	if err != nil {
		return fmt.Errorf("write escalation shard: %w", err)
	}
	e.ShardPath = loc.Path
	return t.tx.InsertEscalation(ctx, e)
}

func (t *storeTx) SaveAudit(ctx context.Context, a *domain.AuditEntry) error {
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("marshal audit: %w", err)
	}
	loc, err := t.writer.Append(ctx, "audit", a.Timestamp, data)
	if err != nil {
		return fmt.Errorf("write audit shard: %w", err)
	}
	a.ShardPath = loc.Path
	return t.tx.InsertAudit(ctx, a)
}

func (t *storeTx) SaveFailure(ctx context.Context, f *domain.PermanentFailure) error {
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal failure: %w", err)
	}
	_, err = t.writer.Append(ctx, "failures", f.FirstFailedAt, data)
	if err != nil {
		return fmt.Errorf("write failure shard: %w", err)
	}
	return t.tx.InsertFailure(ctx, f)
}

func (t *storeTx) UpdateFailure(ctx context.Context, f *domain.PermanentFailure) error {
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshal failure: %w", err)
	}
	_, err = t.writer.Append(ctx, "failures", t.clock.Now(), data)
	if err != nil {
		return fmt.Errorf("write failure update shard: %w", err)
	}
	return t.tx.UpdateFailure(ctx, f)
}

func (t *storeTx) SaveBatch(ctx context.Context, b *domain.ImportBatch) error {
	data, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}
	loc, err := t.writer.Append(ctx, "batches", b.ImportedAt, data)
	if err != nil {
		return fmt.Errorf("write batch shard: %w", err)
	}
	b.ShardPath = loc.Path
	return t.tx.InsertBatch(ctx, b)
}

func (t *storeTx) SaveRule(ctx context.Context, r *domain.Rule) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal rule: %w", err)
	}
	loc, err := t.writer.Append(ctx, "rules", r.CreatedAt, data)
	if err != nil {
		return fmt.Errorf("write rule shard: %w", err)
	}
	r.ShardPath = loc.Path
	return t.tx.InsertRule(ctx, r)
}

func (t *storeTx) SupersedeRule(ctx context.Context, version int) error {
	return t.tx.SupersedeRule(ctx, version)
}
