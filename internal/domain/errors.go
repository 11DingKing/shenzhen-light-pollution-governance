package domain

import "errors"

var (
	ErrNotFound             = errors.New("entity not found")
	ErrDuplicate            = errors.New("duplicate entity")
	ErrInvalidTransition    = errors.New("invalid status transition")
	ErrNoMatchingRule       = errors.New("no matching dispatch rule")
	ErrMaxEscalationReached = errors.New("maximum escalation level reached")
	ErrValidation           = errors.New("validation error")
	ErrAlreadyCompleted     = errors.New("item already completed")
	ErrAlreadyCancelled     = errors.New("item already cancelled")
	ErrShardCorrupted       = errors.New("shard file corrupted")
	ErrConcurrentConflict   = errors.New("concurrent modification conflict")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func (e ValidationError) Unwrap() error {
	return ErrValidation
}
