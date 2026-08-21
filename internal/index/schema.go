package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"lightgovernance/migrations"

	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 2

type Index struct {
	db *sql.DB
}

type Tx struct {
	tx *sql.Tx
}

func Open(ctx context.Context, dbPath string) (*Index, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("exec pragma %q: %w", pragma, err)
		}
	}
	idx := &Index{db: db}
	if err := idx.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return idx, nil
}

func (i *Index) Close() error {
	if i.db == nil {
		return nil
	}
	return i.db.Close()
}

func (i *Index) Ping(ctx context.Context) error {
	return i.db.PingContext(ctx)
}

func (i *Index) BeginTx(ctx context.Context) (*Tx, error) {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return &Tx{tx: tx}, nil
}

func (t *Tx) Commit() error {
	return t.tx.Commit()
}

func (t *Tx) Rollback() error {
	return t.tx.Rollback()
}

func (i *Index) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := i.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

func (i *Index) migrate(ctx context.Context) error {
	if _, err := i.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY, description TEXT NOT NULL, applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	applied, err := i.appliedMigrations(ctx)
	if err != nil {
		return err
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read migration dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		if applied[version] {
			continue
		}
		if err := i.applyMigration(ctx, version, name); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

func (i *Index) appliedMigrations(ctx context.Context) (map[int]bool, error) {
	rows, err := i.db.QueryContext(ctx, "SELECT version FROM schema_version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func (i *Index) applyMigration(ctx context.Context, version int, name string) error {
	content, err := migrations.FS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("exec migration sql: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_version (version, description, applied_at) VALUES (?, ?, ?)",
		version, name, time.Now().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func migrationVersion(name string) (int, error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid migration filename: %s", name)
	}
	var v int
	if _, err := fmt.Sscanf(parts[0], "%d", &v); err != nil {
		return 0, fmt.Errorf("parse version from %s: %w", name, err)
	}
	return v, nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func formatNullableTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}

func scanNullableTime(col sql.NullString) (*time.Time, error) {
	if !col.Valid || col.String == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, col.String)
	if err != nil {
		return nil, fmt.Errorf("parse nullable time %q: %w", col.String, err)
	}
	return &t, nil
}

func encodeSlice(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	data, _ := json.Marshal(s)
	return string(data)
}

func decodeSlice(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil
	}
	return result
}

func (t *Tx) Tx() *sql.Tx {
	return t.tx
}

func formatTimeVal(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}
