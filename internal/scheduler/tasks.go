package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"lightgovernance/internal/domain"
	"lightgovernance/internal/store"
)

func (s *Scheduler) runWithRetry(ctx context.Context, taskName string, fn func(context.Context) error) {
	attempts := 0
	for {
		err := fn(ctx)
		if err == nil {
			return
		}
		attempts++
		s.logger.Error().Err(err).Str("task", taskName).Int("attempt", attempts).Msg("task failed")
		if attempts >= s.config.MaxRetries {
			if ferr := s.recordTaskFailure(ctx, taskName, err, attempts); ferr != nil {
				s.logger.Error().Err(ferr).Str("task", taskName).Msg("failed to record permanent failure")
			}
			return
		}
		backoff := s.calculateBackoff(attempts)
		select {
		case <-ctx.Done():
			return
		case <-s.clock.After(backoff):
		}
	}
}

func (s *Scheduler) runEscalationOnce(ctx context.Context) error {
	s.escalationRuns.Add(1)
	escs, err := s.escalationSvc.CheckAndEscalate(ctx)
	if err != nil {
		return fmt.Errorf("escalation check: %w", err)
	}
	if len(escs) > 0 {
		s.logger.Info().Int("escalated", len(escs)).Msg("escalation task completed")
	}
	return nil
}

func (s *Scheduler) runReconciliationOnce(ctx context.Context) error {
	s.reconRuns.Add(1)
	report, err := s.store.Reconcile(ctx)
	if err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}
	if len(report.Details) > 0 {
		s.logger.Warn().
			Int("orphaned", report.OrphanedInShard).
			Int("missing", report.MissingInShard).
			Int("checksum_mismatches", report.ChecksumMismatches).
			Msg("reconciliation found discrepancies")
	}
	return nil
}

func (s *Scheduler) recordTaskFailure(ctx context.Context, taskName string, cause error, attempts int) error {
	now := s.clock.Now()
	failure := &domain.PermanentFailure{
		ID:            uuid.NewString(),
		EntityType:    "scheduler_task",
		EntityID:      taskName,
		TaskType:      taskName,
		LastError:     cause.Error(),
		Attempts:      attempts,
		FirstFailedAt: now.Add(-s.config.BaseBackoff * time.Duration(attempts)),
		LastFailedAt:  now,
		Status:        domain.FailureStatusActive,
		BackoffState:  fmt.Sprintf("exhausted after %d attempts", attempts),
		DataVersion:   domain.DataVersion,
	}
	return s.store.WithTx(ctx, func(tx store.Tx) error {
		return tx.SaveFailure(ctx, failure)
	})
}
