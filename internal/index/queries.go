package index

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"lightgovernance/internal/domain"
)

type scanner interface {
	Scan(dest ...interface{}) error
}

const itemCols = `id, external_ref, title, description, operator_name, operator_contact,
	materials, category, keywords, status, lead_department, co_departments, rule_version,
	registered_at, reported_by, deadline, escalation_level, store_id,
	completed_at, cancelled_at, cancel_reason, shard_path, shard_offset, data_version,
	created_at, updated_at`

func scanItem(row scanner) (*domain.ComplianceCase, error) {
	var (
		item        domain.ComplianceCase
		matJSON     string
		kwJSON      string
		coJSON      string
		regAt       string
		deadline    string
		createdAt   string
		updatedAt   string
		completedAt sql.NullString
		cancelledAt sql.NullString
	)
	err := row.Scan(
		&item.ID, &item.ExternalRef, &item.Title, &item.Description,
		&item.OperatorName, &item.OperatorContact, &matJSON,
		&item.Category, &kwJSON, &item.Status, &item.LeadDepartment,
		&coJSON, &item.RuleVersion, &regAt, &item.RegisteredBy,
		&deadline, &item.EscalationLevel, &item.StoreID,
		&completedAt, &cancelledAt, &item.CancelReason,
		&item.ShardPath, &item.ShardOffset, &item.DataVersion,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	item.Materials = decodeSlice(matJSON)
	item.Keywords = decodeSlice(kwJSON)
	item.CoDepartments = decodeSlice(coJSON)
	if item.RegisteredAt, err = parseTime(regAt); err != nil {
		return nil, err
	}
	if item.Deadline, err = parseTime(deadline); err != nil {
		return nil, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if item.CompletedAt, err = scanNullableTime(completedAt); err != nil {
		return nil, err
	}
	if item.CancelledAt, err = scanNullableTime(cancelledAt); err != nil {
		return nil, err
	}
	return &item, nil
}

const ruleCols = `version, name, description, match_keywords, match_category, lead_department,
	co_departments, priority, is_default, effective_from, effective_to, status,
	created_at, created_by, shard_path, data_version`

func scanRule(row scanner) (*domain.Rule, error) {
	var (
		rule      domain.Rule
		mkJSON    string
		coJSON    string
		effFrom   string
		effTo     sql.NullString
		createdAt string
		isDefault int
	)
	err := row.Scan(
		&rule.Version, &rule.Name, &rule.Description, &mkJSON,
		&rule.MatchCategory, &rule.LeadDepartment, &coJSON, &rule.Priority,
		&isDefault, &effFrom, &effTo, &rule.Status, &createdAt,
		&rule.CreatedBy, &rule.ShardPath, &rule.DataVersion,
	)
	if err != nil {
		return nil, err
	}
	rule.MatchKeywords = decodeSlice(mkJSON)
	rule.CoDepartments = decodeSlice(coJSON)
	rule.IsDefault = isDefault != 0
	if rule.EffectiveFrom, err = parseTime(effFrom); err != nil {
		return nil, err
	}
	if effTo.Valid {
		if rule.EffectiveTo, err = parseTime(effTo.String); err != nil {
			return nil, err
		}
	}
	if rule.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &rule, nil
}

const asgnCols = `id, item_id, lead_department, co_departments, rule_version,
	adjudicated_at, adjudicated_by, is_current, shard_path, data_version`

func scanAssignment(row scanner) (*domain.Referral, error) {
	var (
		a         domain.Referral
		coJSON    string
		adjAt     string
		isCurrent int
	)
	err := row.Scan(
		&a.ID, &a.ItemID, &a.LeadDepartment, &coJSON, &a.RuleVersion,
		&adjAt, &a.AdjudicatedBy, &isCurrent, &a.ShardPath, &a.DataVersion,
	)
	if err != nil {
		return nil, err
	}
	a.CoDepartments = decodeSlice(coJSON)
	a.IsCurrent = isCurrent != 0
	if a.AdjudicatedAt, err = parseTime(adjAt); err != nil {
		return nil, err
	}
	return &a, nil
}

const escCols = `id, item_id, from_level, to_level, old_lead_dept, new_lead_dept,
	escalated_at, reason, new_deadline, shard_path, data_version`

func scanEscalation(row scanner) (*domain.Escalation, error) {
	var (
		e     domain.Escalation
		escAt string
		newDL string
	)
	err := row.Scan(
		&e.ID, &e.ItemID, &e.FromLevel, &e.ToLevel, &e.OldLeadDept,
		&e.NewLeadDept, &escAt, &e.Reason, &newDL, &e.ShardPath, &e.DataVersion,
	)
	if err != nil {
		return nil, err
	}
	if e.EscalatedAt, err = parseTime(escAt); err != nil {
		return nil, err
	}
	if e.NewDeadline, err = parseTime(newDL); err != nil {
		return nil, err
	}
	return &e, nil
}

const auditCols = `id, entity_id, entity_type, action, actor, timestamp, details,
	shard_path, data_version`

func scanAudit(row scanner) (*domain.AuditEntry, error) {
	var (
		a  domain.AuditEntry
		ts string
	)
	err := row.Scan(
		&a.ID, &a.EntityID, &a.EntityType, &a.Action, &a.Actor, &ts,
		&a.Details, &a.ShardPath, &a.DataVersion,
	)
	if err != nil {
		return nil, err
	}
	if a.Timestamp, err = parseTime(ts); err != nil {
		return nil, err
	}
	return &a, nil
}

const failCols = `id, entity_type, entity_id, task_type, last_error, attempts,
	first_failed_at, last_failed_at, status, resolved_at, next_retry_at,
	backoff_state, data_version`

func scanFailure(row scanner) (*domain.PermanentFailure, error) {
	var (
		f          domain.PermanentFailure
		firstAt    string
		lastAt     string
		resolvedAt sql.NullString
		nextRetry  sql.NullString
	)
	err := row.Scan(
		&f.ID, &f.EntityType, &f.EntityID, &f.TaskType, &f.LastError,
		&f.Attempts, &firstAt, &lastAt, &f.Status, &resolvedAt, &nextRetry,
		&f.BackoffState, &f.DataVersion,
	)
	if err != nil {
		return nil, err
	}
	if f.FirstFailedAt, err = parseTime(firstAt); err != nil {
		return nil, err
	}
	if f.LastFailedAt, err = parseTime(lastAt); err != nil {
		return nil, err
	}
	if f.ResolvedAt, err = scanNullableTime(resolvedAt); err != nil {
		return nil, err
	}
	if f.NextRetryAt, err = scanNullableTime(nextRetry); err != nil {
		return nil, err
	}
	return &f, nil
}

const batchCols = `id, store_id, batch_date, total_rows, success_count, failure_count,
	imported_at, shard_path, data_version`

func scanBatch(row scanner) (*domain.ImportBatch, error) {
	var (
		b     domain.ImportBatch
		bd    string
		impAt string
	)
	err := row.Scan(
		&b.ID, &b.StoreID, &bd, &b.TotalRows, &b.SuccessCount,
		&b.FailureCount, &impAt, &b.ShardPath, &b.DataVersion,
	)
	if err != nil {
		return nil, err
	}
	if b.BatchDate, err = parseTime(bd); err != nil {
		return nil, err
	}
	if b.ImportedAt, err = parseTime(impAt); err != nil {
		return nil, err
	}
	return &b, nil
}

func (i *Index) GetItemByID(ctx context.Context, id string) (*domain.ComplianceCase, error) {
	row := i.db.QueryRowContext(ctx, "SELECT "+itemCols+" FROM items WHERE id = ?", id)
	item, err := scanItem(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return item, nil
}

func (i *Index) GetItemByExternalRef(ctx context.Context, ref string) (*domain.ComplianceCase, error) {
	row := i.db.QueryRowContext(ctx, "SELECT "+itemCols+" FROM items WHERE external_ref = ?", ref)
	item, err := scanItem(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return item, nil
}

func (i *Index) ListItems(ctx context.Context, filter domain.ItemFilter) ([]*domain.ComplianceCase, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(filter.Status))
	}
	if filter.LeadDepartment != "" {
		where = append(where, "lead_department = ?")
		args = append(args, filter.LeadDepartment)
	}
	if filter.StoreID != "" {
		where = append(where, "store_id = ?")
		args = append(args, filter.StoreID)
	}
	if filter.RegisteredBy != "" {
		where = append(where, "reported_by = ?")
		args = append(args, filter.RegisteredBy)
	}
	if !filter.From.IsZero() {
		where = append(where, "registered_at >= ?")
		args = append(args, formatTime(filter.From))
	}
	if !filter.To.IsZero() {
		where = append(where, "registered_at <= ?")
		args = append(args, formatTime(filter.To))
	}
	if filter.OverdueOnly {
		where = append(where, "status NOT IN ('completed','cancelled')")
		where = append(where, "deadline < ?")
		args = append(args, formatTime(time.Now()))
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	err := i.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM items WHERE "+whereClause, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count items: %w", err)
	}

	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := filter.PageOffset
	if offset < 0 {
		offset = 0
	}

	query := "SELECT " + itemCols + " FROM items WHERE " + whereClause +
		" ORDER BY registered_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset)

	rows, err := i.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	var items []*domain.ComplianceCase
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (i *Index) FindOverdueItems(ctx context.Context, now time.Time, maxLevel int) ([]*domain.ComplianceCase, error) {
	query := "SELECT " + itemCols + " FROM items WHERE status NOT IN ('completed','cancelled') AND deadline < ? AND escalation_level < ? ORDER BY deadline ASC"
	rows, err := i.db.QueryContext(ctx, query, formatTime(now), maxLevel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*domain.ComplianceCase
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (i *Index) CountByStatus(ctx context.Context) (map[domain.ItemStatus]int, error) {
	rows, err := i.db.QueryContext(ctx, "SELECT status, COUNT(*) FROM items GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[domain.ItemStatus]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		result[domain.ItemStatus(status)] = count
	}
	return result, rows.Err()
}

func (i *Index) GetRuleByVersion(ctx context.Context, version int) (*domain.Rule, error) {
	row := i.db.QueryRowContext(ctx, "SELECT "+ruleCols+" FROM rules WHERE version = ?", version)
	return scanRule(row)
}

func (i *Index) GetActiveRules(ctx context.Context, at time.Time) ([]*domain.Rule, error) {
	atStr := formatTime(at)
	query := "SELECT " + ruleCols + " FROM rules WHERE status = 'active' AND effective_from <= ? AND (effective_to IS NULL OR effective_to = '' OR effective_to > ?) ORDER BY priority DESC, version DESC"
	rows, err := i.db.QueryContext(ctx, query, atStr, atStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []*domain.Rule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (i *Index) GetCurrentRuleVersion(ctx context.Context) (int, error) {
	var version int
	err := i.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM rules").Scan(&version)
	return version, err
}

func (i *Index) ListRules(ctx context.Context) ([]*domain.Rule, error) {
	rows, err := i.db.QueryContext(ctx, "SELECT "+ruleCols+" FROM rules ORDER BY version DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []*domain.Rule
	for rows.Next() {
		rule, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (i *Index) GetAssignmentsByItemID(ctx context.Context, itemID string) ([]*domain.Referral, error) {
	rows, err := i.db.QueryContext(ctx, "SELECT "+asgnCols+" FROM assignments WHERE item_id = ? ORDER BY adjudicated_at", itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*domain.Referral
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

func (i *Index) GetCurrentAssignment(ctx context.Context, itemID string) (*domain.Referral, error) {
	row := i.db.QueryRowContext(ctx, "SELECT "+asgnCols+" FROM assignments WHERE item_id = ? AND is_current = 1", itemID)
	a, err := scanAssignment(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return a, nil
}

func (i *Index) GetEscalationsByItemID(ctx context.Context, itemID string) ([]*domain.Escalation, error) {
	rows, err := i.db.QueryContext(ctx, "SELECT "+escCols+" FROM escalations WHERE item_id = ? ORDER BY escalated_at", itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*domain.Escalation
	for rows.Next() {
		e, err := scanEscalation(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func (i *Index) ListAudit(ctx context.Context, filter domain.AuditFilter) ([]*domain.AuditEntry, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if filter.EntityID != "" {
		where = append(where, "entity_id = ?")
		args = append(args, filter.EntityID)
	}
	if filter.EntityType != "" {
		where = append(where, "entity_type = ?")
		args = append(args, filter.EntityType)
	}
	if filter.Actor != "" {
		where = append(where, "actor = ?")
		args = append(args, filter.Actor)
	}
	if !filter.From.IsZero() {
		where = append(where, "timestamp >= ?")
		args = append(args, formatTime(filter.From))
	}
	if !filter.To.IsZero() {
		where = append(where, "timestamp <= ?")
		args = append(args, formatTime(filter.To))
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	if err := i.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_entries WHERE "+whereClause, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	offset := filter.PageOffset
	if offset < 0 {
		offset = 0
	}
	query := "SELECT " + auditCols + " FROM audit_entries WHERE " + whereClause + " ORDER BY timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset)
	rows, err := i.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*domain.AuditEntry
	for rows.Next() {
		a, err := scanAudit(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, a)
	}
	return list, total, rows.Err()
}

func (i *Index) ListFailures(ctx context.Context) ([]*domain.PermanentFailure, error) {
	rows, err := i.db.QueryContext(ctx, "SELECT "+failCols+" FROM permanent_failures WHERE status = 'permanent_failure' ORDER BY last_failed_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*domain.PermanentFailure
	for rows.Next() {
		f, err := scanFailure(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

func (i *Index) GetFailureByID(ctx context.Context, id string) (*domain.PermanentFailure, error) {
	row := i.db.QueryRowContext(ctx, "SELECT "+failCols+" FROM permanent_failures WHERE id = ?", id)
	f, err := scanFailure(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func (i *Index) ListBatches(ctx context.Context, filter domain.BatchFilter) ([]*domain.ImportBatch, int, error) {
	where := []string{"1=1"}
	args := []interface{}{}
	if filter.StoreID != "" {
		where = append(where, "store_id = ?")
		args = append(args, filter.StoreID)
	}
	if !filter.From.IsZero() {
		where = append(where, "batch_date >= ?")
		args = append(args, formatTime(filter.From))
	}
	if !filter.To.IsZero() {
		where = append(where, "batch_date <= ?")
		args = append(args, formatTime(filter.To))
	}
	whereClause := strings.Join(where, " AND ")

	var total int
	countArgs := make([]interface{}, len(args))
	copy(countArgs, args)
	if err := i.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM import_batches WHERE "+whereClause, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	offset := filter.PageOffset
	if offset < 0 {
		offset = 0
	}
	query := "SELECT " + batchCols + " FROM import_batches WHERE " + whereClause + " ORDER BY batch_date DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, offset)
	rows, err := i.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*domain.ImportBatch
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, b)
	}
	return list, total, rows.Err()
}
