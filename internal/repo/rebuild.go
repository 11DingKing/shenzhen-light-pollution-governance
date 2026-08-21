package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lightgovernance/internal/domain"
	"lightgovernance/internal/index"
	"lightgovernance/internal/shard"
	"lightgovernance/internal/store"
)

type recordCollector struct {
	itemsByID       map[string]*domain.ComplianceCase
	rulesByVersion  map[int]*domain.Rule
	assignmentsByID map[string]*domain.Referral
	escalationsByID map[string]*domain.Escalation
	auditByID       map[string]*domain.AuditEntry
	failuresByID    map[string]*domain.PermanentFailure
	batchesByID     map[string]*domain.ImportBatch
}

func newCollector() *recordCollector {
	return &recordCollector{
		itemsByID:       map[string]*domain.ComplianceCase{},
		rulesByVersion:  map[int]*domain.Rule{},
		assignmentsByID: map[string]*domain.Referral{},
		escalationsByID: map[string]*domain.Escalation{},
		auditByID:       map[string]*domain.AuditEntry{},
		failuresByID:    map[string]*domain.PermanentFailure{},
		batchesByID:     map[string]*domain.ImportBatch{},
	}
}

func (c *recordCollector) add(entityType string, data []byte) error {
	switch entityType {
	case "items":
		var item domain.ComplianceCase
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		c.itemsByID[item.ID] = &item
	case "rules":
		var rule domain.Rule
		if err := json.Unmarshal(data, &rule); err != nil {
			return err
		}
		c.rulesByVersion[rule.Version] = &rule
	case "assignments":
		var a domain.Referral
		if err := json.Unmarshal(data, &a); err != nil {
			return err
		}
		c.assignmentsByID[a.ID] = &a
	case "escalations":
		var e domain.Escalation
		if err := json.Unmarshal(data, &e); err != nil {
			return err
		}
		c.escalationsByID[e.ID] = &e
	case "audit":
		var a domain.AuditEntry
		if err := json.Unmarshal(data, &a); err != nil {
			return err
		}
		c.auditByID[a.ID] = &a
	case "failures":
		var f domain.PermanentFailure
		if err := json.Unmarshal(data, &f); err != nil {
			return err
		}
		c.failuresByID[f.ID] = &f
	case "batches":
		var b domain.ImportBatch
		if err := json.Unmarshal(data, &b); err != nil {
			return err
		}
		c.batchesByID[b.ID] = &b
	default:
		return fmt.Errorf("unknown entity type: %s", entityType)
	}
	return nil
}

func (s *Store) RebuildIndex(ctx context.Context) (store.RebuildReport, error) {
	report := store.RebuildReport{}

	files, err := s.scanner.ScanAll(ctx)
	if err != nil {
		return report, fmt.Errorf("scan shards: %w", err)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	report.TotalShards = len(files)

	collector := newCollector()
	for _, sf := range files {
		records, readErr := s.reader.ReadAll(sf.Path)
		if readErr != nil {
			report.CorruptedShards = append(report.CorruptedShards, store.CorruptedShard{
				Path:   sf.Path,
				Reason: readErr.Error(),
			})
			report.SkippedShards++
			continue
		}
		for _, rec := range records {
			_ = collector.add(sf.EntityType, rec)
		}
		report.IndexedShards++
	}

	idxTx, err := s.index.BeginTx(ctx)
	if err != nil {
		return report, fmt.Errorf("begin tx: %w", err)
	}
	defer idxTx.Rollback()

	if err := idxTx.DeleteAllData(ctx); err != nil {
		return report, fmt.Errorf("delete all data: %w", err)
	}

	for _, item := range collector.itemsByID {
		if err := idxTx.InsertItem(ctx, item); err != nil {
			return report, fmt.Errorf("insert item %s: %w", item.ID, err)
		}
		report.TotalRecords++
	}
	for _, rule := range collector.rulesByVersion {
		if err := idxTx.InsertRule(ctx, rule); err != nil {
			return report, fmt.Errorf("insert rule v%d: %w", rule.Version, err)
		}
		report.TotalRecords++
	}
	for _, a := range collector.assignmentsByID {
		if err := idxTx.InsertAssignment(ctx, a); err != nil {
			return report, fmt.Errorf("insert referral %s: %w", a.ID, err)
		}
		report.TotalRecords++
	}
	for _, e := range collector.escalationsByID {
		if err := idxTx.InsertEscalation(ctx, e); err != nil {
			return report, fmt.Errorf("insert escalation %s: %w", e.ID, err)
		}
		report.TotalRecords++
	}
	for _, a := range collector.auditByID {
		if err := idxTx.InsertAudit(ctx, a); err != nil {
			return report, fmt.Errorf("insert audit %s: %w", a.ID, err)
		}
		report.TotalRecords++
	}
	for _, f := range collector.failuresByID {
		if err := idxTx.InsertFailure(ctx, f); err != nil {
			return report, fmt.Errorf("insert failure %s: %w", f.ID, err)
		}
		report.TotalRecords++
	}
	for _, b := range collector.batchesByID {
		if err := idxTx.InsertBatch(ctx, b); err != nil {
			return report, fmt.Errorf("insert batch %s: %w", b.ID, err)
		}
		report.TotalRecords++
	}

	if err := s.postProcessRebuild(ctx, idxTx); err != nil {
		return report, fmt.Errorf("post-process: %w", err)
	}

	if err := s.rebuildManifests(ctx, idxTx, files); err != nil {
		return report, fmt.Errorf("rebuild manifests: %w", err)
	}

	if err := idxTx.Commit(); err != nil {
		return report, fmt.Errorf("commit rebuild: %w", err)
	}
	return report, nil
}

func (s *Store) postProcessRebuild(ctx context.Context, tx *index.Tx) error {
	if _, err := tx.Tx().ExecContext(ctx, `UPDATE assignments SET is_current=0
		WHERE item_id IN (SELECT DISTINCT item_id FROM escalations)`); err != nil {
		return fmt.Errorf("mark superseded assignments: %w", err)
	}
	if _, err := tx.Tx().ExecContext(ctx, `UPDATE rules SET status='superseded'
		WHERE version IN (
			SELECT CAST(entity_id AS INTEGER) FROM audit_entries
			WHERE action='supersede_rule' AND entity_type='rule'
		)`); err != nil {
		return fmt.Errorf("mark superseded rules: %w", err)
	}
	return nil
}

func (s *Store) rebuildManifests(ctx context.Context, tx *index.Tx, files []shard.ShardFile) error {
	now := time.Now().Format(time.RFC3339Nano)
	for _, sf := range files {
		records, err := s.reader.ReadAll(sf.Path)
		status := "ok"
		count := 0
		if err != nil {
			status = "corrupted"
		} else {
			count = len(records)
		}
		checksum := ""
		if status == "ok" {
			checksum, _ = s.reader.Checksum(sf.Path)
		}
		entry := index.ManifestEntry{
			ShardID:     sf.EntityType + ":" + filepath.Base(sf.Path),
			EntityType:  sf.EntityType,
			ShardPath:   sf.Path,
			Checksum:    checksum,
			RecordCount: count,
			DateKey:     extractDateKey(sf.Path),
			Status:      status,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := tx.UpsertManifest(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func extractDateKey(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, ".jsonl")
	return base
}

func (s *Store) Reconcile(ctx context.Context) (store.ReconcileReport, error) {
	report := store.ReconcileReport{}

	files, err := s.scanner.ScanAll(ctx)
	if err != nil {
		return report, err
	}
	report.ShardCount = len(files)

	manifests, err := s.index.ListManifest(ctx)
	if err != nil {
		return report, err
	}
	manifestByPath := map[string]index.ManifestEntry{}
	for _, m := range manifests {
		manifestByPath[m.ShardPath] = m
	}

	for _, sf := range files {
		records, readErr := s.reader.ReadAll(sf.Path)
		if readErr != nil {
			report.Details = append(report.Details,
				fmt.Sprintf("corrupted shard %s: %s", sf.Path, readErr.Error()))
			continue
		}
		shardCount := len(records)
		m, ok := manifestByPath[sf.Path]
		if !ok {
			report.OrphanedInShard += shardCount
			report.Details = append(report.Details,
				fmt.Sprintf("shard %s not in manifest (records: %d)", sf.Path, shardCount))
			continue
		}
		if m.RecordCount != shardCount {
			diff := shardCount - m.RecordCount
			if diff > 0 {
				report.OrphanedInShard += diff
			} else {
				report.MissingInShard += -diff
			}
			report.Details = append(report.Details,
				fmt.Sprintf("count mismatch %s: shard=%d manifest=%d",
					sf.Path, shardCount, m.RecordCount))
		}
		checksum, _ := s.reader.Checksum(sf.Path)
		if m.Checksum != "" && m.Checksum != checksum {
			report.ChecksumMismatches++
			report.Details = append(report.Details,
				fmt.Sprintf("checksum mismatch %s", sf.Path))
		}
		report.IndexCount += m.RecordCount
	}
	return report, nil
}

func (s *Store) Diagnose(ctx context.Context) (store.DiagnoseReport, error) {
	report := store.DiagnoseReport{}

	f, err := os.CreateTemp(s.dataDir, ".diagnose-*")
	if err != nil {
		report.DataDirWritable = false
		report.Issues = append(report.Issues, "data directory not writable")
	} else {
		f.Close()
		os.Remove(f.Name())
		report.DataDirWritable = true
	}

	manifests, err := s.index.ListManifest(ctx)
	if err != nil {
		report.Issues = append(report.Issues, "failed to list manifest: "+err.Error())
	}
	for _, m := range manifests {
		report.ShardManifest = append(report.ShardManifest, store.ShardManifestEntry{
			ShardID:     m.ShardID,
			EntityType:  m.EntityType,
			ShardPath:   m.ShardPath,
			RecordCount: m.RecordCount,
			Status:      m.Status,
			Checksum:    m.Checksum,
		})
		if m.Status == "corrupted" {
			report.CorruptedShards++
		}
	}

	statusCounts, err := s.index.CountByStatus(ctx)
	if err != nil {
		report.Issues = append(report.Issues, "failed to count by status: "+err.Error())
	}
	for _, count := range statusCounts {
		report.ItemCount += count
	}

	rules, err := s.index.ListRules(ctx)
	if err != nil {
		report.Issues = append(report.Issues, "failed to list rules: "+err.Error())
	}
	report.RuleCount = len(rules)

	overdue, err := s.index.FindOverdueItems(ctx, s.clock.Now(), 999)
	if err != nil {
		report.Issues = append(report.Issues, "failed to find overdue: "+err.Error())
	}
	report.OverdueCount = len(overdue)

	sv, err := s.index.SchemaVersion(ctx)
	if err != nil {
		report.Issues = append(report.Issues, "failed to get schema version: "+err.Error())
	}
	report.SchemaVersion = sv

	report.Ready = report.DataDirWritable && report.CorruptedShards == 0 && len(report.Issues) == 0
	return report, nil
}
