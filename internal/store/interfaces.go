package store

import (
	"context"
	"time"

	"lightgovernance/internal/domain"
)

type Tx interface {
	SaveItem(ctx context.Context, item *domain.ComplianceCase) error
	UpdateItem(ctx context.Context, item *domain.ComplianceCase) error
	SaveAssignment(ctx context.Context, a *domain.Referral) error
	MarkAssignmentSuperseded(ctx context.Context, itemID string) error
	SaveEscalation(ctx context.Context, e *domain.Escalation) error
	SaveAudit(ctx context.Context, e *domain.AuditEntry) error
	SaveFailure(ctx context.Context, f *domain.PermanentFailure) error
	UpdateFailure(ctx context.Context, f *domain.PermanentFailure) error
	SaveBatch(ctx context.Context, b *domain.ImportBatch) error
	SaveRule(ctx context.Context, r *domain.Rule) error
	SupersedeRule(ctx context.Context, version int) error
}

type Store interface {
	WithTx(ctx context.Context, fn func(Tx) error) error

	GetItem(ctx context.Context, id string) (*domain.ComplianceCase, error)
	GetItemByExternalRef(ctx context.Context, ref string) (*domain.ComplianceCase, error)
	ListItems(ctx context.Context, filter domain.ItemFilter) ([]*domain.ComplianceCase, int, error)
	FindOverdueItems(ctx context.Context, now time.Time, maxLevel int) ([]*domain.ComplianceCase, error)
	CountByStatus(ctx context.Context) (map[domain.ItemStatus]int, error)

	GetRule(ctx context.Context, version int) (*domain.Rule, error)
	GetActiveRules(ctx context.Context, at time.Time) ([]*domain.Rule, error)
	GetCurrentRuleVersion(ctx context.Context) (int, error)
	ListRules(ctx context.Context) ([]*domain.Rule, error)

	GetAssignments(ctx context.Context, itemID string) ([]*domain.Referral, error)
	GetCurrentAssignment(ctx context.Context, itemID string) (*domain.Referral, error)

	GetEscalations(ctx context.Context, itemID string) ([]*domain.Escalation, error)

	ListAudit(ctx context.Context, filter domain.AuditFilter) ([]*domain.AuditEntry, int, error)

	ListFailures(ctx context.Context) ([]*domain.PermanentFailure, error)
	GetFailure(ctx context.Context, id string) (*domain.PermanentFailure, error)

	ListBatches(ctx context.Context, filter domain.BatchFilter) ([]*domain.ImportBatch, int, error)

	RebuildIndex(ctx context.Context) (RebuildReport, error)
	Reconcile(ctx context.Context) (ReconcileReport, error)
	Diagnose(ctx context.Context) (DiagnoseReport, error)

	Ping(ctx context.Context) error
	Close() error
}

type RebuildReport struct {
	TotalShards     int
	IndexedShards   int
	SkippedShards   int
	CorruptedShards []CorruptedShard
	TotalRecords    int
}

type CorruptedShard struct {
	Path   string
	Reason string
}

type ReconcileReport struct {
	ShardCount         int
	IndexCount         int
	OrphanedInShard    int
	MissingInShard     int
	ChecksumMismatches int
	Details            []string
}

type DiagnoseReport struct {
	DataDirWritable bool
	ShardManifest   []ShardManifestEntry
	ItemCount       int
	RuleCount       int
	OverdueCount    int
	CorruptedShards int
	SchemaVersion   int
	Ready           bool
	Issues          []string
}

type ShardManifestEntry struct {
	ShardID     string
	EntityType  string
	ShardPath   string
	RecordCount int
	Status      string
	Checksum    string
}
