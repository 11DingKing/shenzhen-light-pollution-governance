package index

import (
	"context"
	"fmt"
	"time"

	"lightgovernance/internal/domain"
)

func (t *Tx) InsertItem(ctx context.Context, item *domain.ComplianceCase) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO items (
		id, external_ref, title, description, operator_name, operator_contact,
		materials, category, keywords, status, lead_department, co_departments, rule_version,
		registered_at, reported_by, deadline, escalation_level, store_id,
		completed_at, cancelled_at, cancel_reason, shard_path, shard_offset, data_version,
		created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.ExternalRef, item.Title, item.Description,
		item.OperatorName, item.OperatorContact, encodeSlice(item.Materials),
		item.Category, encodeSlice(item.Keywords), item.Status, item.LeadDepartment,
		encodeSlice(item.CoDepartments), item.RuleVersion, formatTime(item.RegisteredAt),
		item.RegisteredBy, formatTime(item.Deadline), item.EscalationLevel, item.StoreID,
		formatNullableTime(item.CompletedAt), formatNullableTime(item.CancelledAt),
		item.CancelReason, item.ShardPath, item.ShardOffset, item.DataVersion,
		formatTime(item.CreatedAt), formatTime(item.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert item: %w", err)
	}
	return nil
}

func (t *Tx) UpdateItem(ctx context.Context, item *domain.ComplianceCase) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE items SET
		title=?, description=?, operator_name=?, operator_contact=?,
		materials=?, category=?, keywords=?, status=?,
		lead_department=?, co_departments=?, rule_version=?,
		deadline=?, escalation_level=?, store_id=?,
		completed_at=?, cancelled_at=?, cancel_reason=?,
		data_version=?, updated_at=? WHERE id=?`,
		item.Title, item.Description, item.OperatorName, item.OperatorContact,
		encodeSlice(item.Materials), item.Category, encodeSlice(item.Keywords),
		item.Status, item.LeadDepartment, encodeSlice(item.CoDepartments),
		item.RuleVersion, formatTime(item.Deadline), item.EscalationLevel,
		item.StoreID, formatNullableTime(item.CompletedAt),
		formatNullableTime(item.CancelledAt), item.CancelReason,
		item.DataVersion, formatTime(item.UpdatedAt), item.ID,
	)
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (t *Tx) InsertAssignment(ctx context.Context, a *domain.Referral) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO assignments (
		id, item_id, lead_department, co_departments, rule_version,
		adjudicated_at, adjudicated_by, is_current, shard_path, data_version
	) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.ItemID, a.LeadDepartment, encodeSlice(a.CoDepartments),
		a.RuleVersion, formatTime(a.AdjudicatedAt), a.AdjudicatedBy,
		boolToInt(a.IsCurrent), a.ShardPath, a.DataVersion,
	)
	if err != nil {
		return fmt.Errorf("insert referral: %w", err)
	}
	return nil
}

func (t *Tx) MarkAssignmentSuperseded(ctx context.Context, itemID string) error {
	_, err := t.tx.ExecContext(ctx, "UPDATE assignments SET is_current=0 WHERE item_id=? AND is_current=1", itemID)
	if err != nil {
		return fmt.Errorf("mark referral superseded: %w", err)
	}
	return nil
}

func (t *Tx) InsertEscalation(ctx context.Context, e *domain.Escalation) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO escalations (
		id, item_id, from_level, to_level, old_lead_dept, new_lead_dept,
		escalated_at, reason, new_deadline, shard_path, data_version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.ItemID, e.FromLevel, e.ToLevel, e.OldLeadDept,
		e.NewLeadDept, formatTime(e.EscalatedAt), e.Reason,
		formatTime(e.NewDeadline), e.ShardPath, e.DataVersion,
	)
	if err != nil {
		return fmt.Errorf("insert escalation: %w", err)
	}
	return nil
}

func (t *Tx) UpdateItemForEscalation(ctx context.Context, itemID string, newLead string, newLevel int, newDeadline time.Time, newStatus domain.ItemStatus) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE items SET
		lead_department=?, escalation_level=?, deadline=?, status=?, updated_at=?
		WHERE id=?`,
		newLead, newLevel, formatTime(newDeadline), newStatus, formatTime(time.Now()), itemID)
	if err != nil {
		return fmt.Errorf("update item for escalation: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (t *Tx) InsertAudit(ctx context.Context, a *domain.AuditEntry) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO audit_entries (
		id, entity_id, entity_type, action, actor, timestamp, details, shard_path, data_version
	) VALUES (?,?,?,?,?,?,?,?,?)`,
		a.ID, a.EntityID, a.EntityType, a.Action, a.Actor,
		formatTime(a.Timestamp), a.Details, a.ShardPath, a.DataVersion,
	)
	if err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}

func (t *Tx) InsertFailure(ctx context.Context, f *domain.PermanentFailure) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO permanent_failures (
		id, entity_type, entity_id, task_type, last_error, attempts,
		first_failed_at, last_failed_at, status, resolved_at, next_retry_at,
		backoff_state, data_version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		f.ID, f.EntityType, f.EntityID, f.TaskType, f.LastError, f.Attempts,
		formatTime(f.FirstFailedAt), formatTime(f.LastFailedAt), f.Status,
		formatNullableTime(f.ResolvedAt), formatNullableTime(f.NextRetryAt),
		f.BackoffState, f.DataVersion,
	)
	if err != nil {
		return fmt.Errorf("insert failure: %w", err)
	}
	return nil
}

func (t *Tx) UpdateFailure(ctx context.Context, f *domain.PermanentFailure) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE permanent_failures SET
		last_error=?, attempts=?, last_failed_at=?, status=?, next_retry_at=?,
		backoff_state=?, resolved_at=? WHERE id=?`,
		f.LastError, f.Attempts, formatTime(f.LastFailedAt), f.Status,
		formatNullableTime(f.NextRetryAt), f.BackoffState,
		formatNullableTime(f.ResolvedAt), f.ID,
	)
	if err != nil {
		return fmt.Errorf("update failure: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (t *Tx) MarkFailureResolved(ctx context.Context, id string, at time.Time) error {
	result, err := t.tx.ExecContext(ctx, `UPDATE permanent_failures SET
		status='resolved', resolved_at=? WHERE id=?`, formatTime(at), id)
	if err != nil {
		return fmt.Errorf("mark failure resolved: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (t *Tx) InsertBatch(ctx context.Context, b *domain.ImportBatch) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO import_batches (
		id, store_id, batch_date, total_rows, success_count, failure_count,
		imported_at, shard_path, data_version
	) VALUES (?,?,?,?,?,?,?,?,?)`,
		b.ID, b.StoreID, formatTime(b.BatchDate), b.TotalRows,
		b.SuccessCount, b.FailureCount, formatTime(b.ImportedAt),
		b.ShardPath, b.DataVersion,
	)
	if err != nil {
		return fmt.Errorf("insert batch: %w", err)
	}
	return nil
}

func (t *Tx) InsertRule(ctx context.Context, r *domain.Rule) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO rules (
		version, name, description, match_keywords, match_category, lead_department,
		co_departments, priority, is_default, effective_from, effective_to, status,
		created_at, created_by, shard_path, data_version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.Version, r.Name, r.Description, encodeSlice(r.MatchKeywords),
		r.MatchCategory, r.LeadDepartment, encodeSlice(r.CoDepartments),
		r.Priority, boolToInt(r.IsDefault), formatTime(r.EffectiveFrom),
		formatTimeVal(r.EffectiveTo), r.Status, formatTime(r.CreatedAt),
		r.CreatedBy, r.ShardPath, r.DataVersion,
	)
	if err != nil {
		return fmt.Errorf("insert rule: %w", err)
	}
	return nil
}

func (t *Tx) SupersedeRule(ctx context.Context, version int) error {
	_, err := t.tx.ExecContext(ctx, "UPDATE rules SET status='superseded', effective_to=? WHERE version=?",
		formatTime(time.Now()), version)
	if err != nil {
		return fmt.Errorf("supersede rule: %w", err)
	}
	return nil
}

func (t *Tx) DeleteAllData(ctx context.Context) error {
	tables := []string{"items", "assignments", "escalations", "audit_entries",
		"permanent_failures", "import_batches", "shard_manifest"}
	for _, table := range tables {
		if _, err := t.tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}
	return nil
}

type ManifestEntry struct {
	ShardID     string
	EntityType  string
	ShardPath   string
	Checksum    string
	RecordCount int
	MinKey      string
	MaxKey      string
	DateKey     string
	Status      string
	CreatedAt   string
	UpdatedAt   string
}

func (t *Tx) UpsertManifest(ctx context.Context, entry ManifestEntry) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO shard_manifest (
		shard_id, entity_type, shard_path, checksum, record_count,
		min_key, max_key, date_key, status, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)
	ON CONFLICT(shard_id) DO UPDATE SET
		checksum=excluded.checksum, record_count=excluded.record_count,
		min_key=excluded.min_key, max_key=excluded.max_key,
		status=excluded.status, updated_at=excluded.updated_at`,
		entry.ShardID, entry.EntityType, entry.ShardPath, entry.Checksum,
		entry.RecordCount, entry.MinKey, entry.MaxKey, entry.DateKey,
		entry.Status, entry.CreatedAt, entry.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert manifest: %w", err)
	}
	return nil
}

func (i *Index) ListManifest(ctx context.Context) ([]ManifestEntry, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT shard_id, entity_type, shard_path,
		checksum, record_count, min_key, max_key, date_key, status, created_at, updated_at
		FROM shard_manifest ORDER BY entity_type, date_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ManifestEntry
	for rows.Next() {
		var e ManifestEntry
		if err := rows.Scan(&e.ShardID, &e.EntityType, &e.ShardPath, &e.Checksum,
			&e.RecordCount, &e.MinKey, &e.MaxKey, &e.DateKey, &e.Status,
			&e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
