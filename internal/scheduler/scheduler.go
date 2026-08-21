package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/benbjohnson/clock"

	"lightgovernance/internal/applog"
	"lightgovernance/internal/config"
	"lightgovernance/internal/service"
	"lightgovernance/internal/store"
)

type Scheduler struct {
	clock          clock.Clock
	escalationSvc  *service.EscalationService
	store          store.Store
	config         config.SchedulerConfig
	logger         *applog.Logger
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	started        atomic.Bool
	escalationRuns atomic.Int64
	reconRuns      atomic.Int64
}

func New(clk clock.Clock, escSvc *service.EscalationService, st store.Store,
	cfg config.SchedulerConfig, logger *applog.Logger) *Scheduler {
	return &Scheduler{
		clock:         clk,
		escalationSvc: escSvc,
		store:         st,
		config:        cfg,
		logger:        logger,
	}
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.started.Store(true)
	s.wg.Add(2)
	go s.runEscalationLoop()
	go s.runReconciliationLoop()
	s.logger.Info().Msg("scheduler started")
	return nil
}

func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	s.started.Store(false)
	s.logger.Info().Msg("scheduler stopped")
}

func (s *Scheduler) Started() bool {
	return s.started.Load()
}

func (s *Scheduler) EscalationRuns() int64 {
	return s.escalationRuns.Load()
}

func (s *Scheduler) RunEscalationOnce(ctx context.Context) error {
	return s.runEscalationOnce(ctx)
}

func (s *Scheduler) RunReconciliationOnce(ctx context.Context) error {
	return s.runReconciliationOnce(ctx)
}

func (s *Scheduler) runEscalationLoop() {
	defer s.wg.Done()
	ticker := s.clock.Ticker(s.config.EscalationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			taskCtx, cancel := context.WithTimeout(s.ctx, s.config.TaskTimeout)
			s.runWithRetry(taskCtx, "escalation", s.runEscalationOnce)
			cancel()
		}
	}
}

func (s *Scheduler) runReconciliationLoop() {
	defer s.wg.Done()
	ticker := s.clock.Ticker(s.config.ReconciliationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			taskCtx, cancel := context.WithTimeout(s.ctx, s.config.TaskTimeout)
			s.runWithRetry(taskCtx, "reconciliation", s.runReconciliationOnce)
			cancel()
		}
	}
}

func (s *Scheduler) calculateBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	shift := uint(attempt - 1)
	if shift > 10 {
		shift = 10
	}
	return s.config.BaseBackoff * time.Duration(1<<shift)
}
