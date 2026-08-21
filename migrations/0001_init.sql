-- Initial schema for lead responsibility adjudication service.
-- All tables carry data_version for forward-compatible migration.

CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER PRIMARY KEY,
    description TEXT    NOT NULL,
    applied_at  TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS shard_manifest (
    shard_id     TEXT PRIMARY KEY,
    entity_type  TEXT NOT NULL,
    shard_path   TEXT NOT NULL,
    checksum     TEXT NOT NULL,
    record_count INTEGER NOT NULL DEFAULT 0,
    min_key      TEXT,
    max_key      TEXT,
    date_key     TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'ok',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_manifest_type ON shard_manifest(entity_type);
CREATE INDEX IF NOT EXISTS idx_manifest_date ON shard_manifest(date_key);

CREATE TABLE IF NOT EXISTS items (
    id               TEXT PRIMARY KEY,
    external_ref     TEXT UNIQUE NOT NULL,
    title            TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    operator_name   TEXT NOT NULL DEFAULT '',
    operator_contact TEXT NOT NULL DEFAULT '',
    materials        TEXT NOT NULL DEFAULT '[]',
    category         TEXT NOT NULL DEFAULT '',
    keywords         TEXT NOT NULL DEFAULT '[]',
    status           TEXT NOT NULL,
    lead_department  TEXT NOT NULL DEFAULT '',
    co_departments   TEXT NOT NULL DEFAULT '[]',
    rule_version     INTEGER NOT NULL DEFAULT 0,
    registered_at    TEXT NOT NULL,
    reported_by    TEXT NOT NULL DEFAULT '',
    deadline         TEXT NOT NULL,
    escalation_level INTEGER NOT NULL DEFAULT 0,
    store_id        TEXT NOT NULL DEFAULT '',
    completed_at     TEXT,
    cancelled_at     TEXT,
    cancel_reason    TEXT NOT NULL DEFAULT '',
    shard_path       TEXT NOT NULL DEFAULT '',
    shard_offset     INTEGER NOT NULL DEFAULT 0,
    data_version     INTEGER NOT NULL DEFAULT 1,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_items_status ON items(status);
CREATE INDEX IF NOT EXISTS idx_items_registered ON items(registered_at);
CREATE INDEX IF NOT EXISTS idx_items_lead ON items(lead_department);
CREATE INDEX IF NOT EXISTS idx_items_deadline ON items(deadline);

CREATE TABLE IF NOT EXISTS rules (
    version         INTEGER PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    match_keywords  TEXT NOT NULL DEFAULT '[]',
    match_category  TEXT NOT NULL DEFAULT '',
    lead_department TEXT NOT NULL,
    co_departments  TEXT NOT NULL DEFAULT '[]',
    priority        INTEGER NOT NULL DEFAULT 0,
    is_default      INTEGER NOT NULL DEFAULT 0,
    effective_from  TEXT NOT NULL,
    effective_to    TEXT,
    status          TEXT NOT NULL,
    created_at      TEXT NOT NULL,
    created_by      TEXT NOT NULL DEFAULT '',
    shard_path      TEXT NOT NULL DEFAULT '',
    data_version    INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS assignments (
    id              TEXT PRIMARY KEY,
    item_id         TEXT NOT NULL,
    lead_department TEXT NOT NULL,
    co_departments  TEXT NOT NULL DEFAULT '[]',
    rule_version    INTEGER NOT NULL,
    adjudicated_at  TEXT NOT NULL,
    adjudicated_by  TEXT NOT NULL DEFAULT 'system',
    is_current      INTEGER NOT NULL DEFAULT 1,
    shard_path      TEXT NOT NULL DEFAULT '',
    data_version    INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_assign_item ON assignments(item_id);

CREATE TABLE IF NOT EXISTS escalations (
    id            TEXT PRIMARY KEY,
    item_id       TEXT NOT NULL,
    from_level    INTEGER NOT NULL,
    to_level      INTEGER NOT NULL,
    old_lead_dept TEXT NOT NULL DEFAULT '',
    new_lead_dept TEXT NOT NULL,
    escalated_at  TEXT NOT NULL,
    reason        TEXT NOT NULL,
    new_deadline  TEXT NOT NULL,
    shard_path    TEXT NOT NULL DEFAULT '',
    data_version  INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_esc_item ON escalations(item_id);

CREATE TABLE IF NOT EXISTS audit_entries (
    id          TEXT PRIMARY KEY,
    entity_id   TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    action      TEXT NOT NULL,
    actor       TEXT NOT NULL,
    timestamp   TEXT NOT NULL,
    details     TEXT NOT NULL DEFAULT '',
    shard_path  TEXT NOT NULL DEFAULT '',
    data_version INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_entries(entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_entries(timestamp);

CREATE TABLE IF NOT EXISTS permanent_failures (
    id             TEXT PRIMARY KEY,
    entity_type    TEXT NOT NULL,
    entity_id      TEXT NOT NULL,
    task_type      TEXT NOT NULL,
    last_error     TEXT NOT NULL DEFAULT '',
    attempts       INTEGER NOT NULL DEFAULT 0,
    first_failed_at TEXT NOT NULL,
    last_failed_at TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'permanent_failure',
    resolved_at    TEXT,
    data_version   INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS import_batches (
    id            TEXT PRIMARY KEY,
    store_id     TEXT NOT NULL,
    batch_date    TEXT NOT NULL,
    total_rows    INTEGER NOT NULL,
    success_count INTEGER NOT NULL,
    failure_count INTEGER NOT NULL,
    imported_at   TEXT NOT NULL,
    shard_path    TEXT NOT NULL DEFAULT '',
    data_version  INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_batch_date ON import_batches(batch_date);
