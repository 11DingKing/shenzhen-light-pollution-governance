package domain

import "time"

type FailureStatus string

const (
	FailureStatusActive   FailureStatus = "permanent_failure"
	FailureStatusRetried  FailureStatus = "retried"
	FailureStatusResolved FailureStatus = "resolved"
)

type PermanentFailure struct {
	ID            string        `json:"id"`
	EntityType    string        `json:"entity_type"`
	EntityID      string        `json:"entity_id"`
	TaskType      string        `json:"task_type"`
	LastError     string        `json:"last_error"`
	Attempts      int           `json:"attempts"`
	FirstFailedAt time.Time     `json:"first_failed_at"`
	LastFailedAt  time.Time     `json:"last_failed_at"`
	Status        FailureStatus `json:"status"`
	ResolvedAt    *time.Time    `json:"resolved_at,omitempty"`
	NextRetryAt   *time.Time    `json:"next_retry_at,omitempty"`
	BackoffState  string        `json:"backoff_state,omitempty"`
	DataVersion   int           `json:"data_version"`
}
