package domain

import "time"

type AuditEntry struct {
	ID          string    `json:"id"`
	EntityID    string    `json:"entity_id"`
	EntityType  string    `json:"entity_type"`
	Action      string    `json:"action"`
	Actor       string    `json:"actor"`
	Timestamp   time.Time `json:"timestamp"`
	Details     string    `json:"details,omitempty"`
	ShardPath   string    `json:"-"`
	DataVersion int       `json:"data_version"`
}

type AuditFilter struct {
	EntityID   string
	EntityType string
	Actor      string
	From       time.Time
	To         time.Time
	PageSize   int
	PageOffset int
}
