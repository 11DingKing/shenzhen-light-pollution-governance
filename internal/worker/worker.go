package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/benbjohnson/clock"

	"lightgovernance/internal/applog"
	"lightgovernance/internal/config"
	"lightgovernance/internal/dispatch"
	"lightgovernance/internal/store"
)

// Ticker is the periodic tick source that drives the worker loop. The production
// implementation wraps a real *time.Ticker so the loop is driven by the standard
// library; tests supply a fake that feeds a controlled channel so no real
// waiting is required.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct{ t *time.Ticker }

func (r *realTicker) C() <-chan time.Time { return r.t.C }
func (r *realTicker) Stop()               { r.t.Stop() }

func realTickerFactory(d time.Duration) Ticker {
	return &realTicker{t: time.NewTicker(d)}
}

// ReevalWorker is a real background processing loop. When the active dispatch
// rule version advances, it re-adjudicates items that are still in a
// re-evaluable state (registered or adjudicated) so they are routed under the
// current rules instead of a superseded one. This is the background half of
// rule-version evolution: old records keep their original semantics, while
// pending work is migrated forward.
//
// Progress is fully persisted on disk: on process restart the worker reads the
// stale-rule items from the store and resumes. Each re-evaluation is a
// multi-step transaction (supersede old referral, persist new referral,
// update item, write audit) that commits atomically or rolls back.
type ReevalWorker struct {
	store       store.Store
	adjudicator *dispatch.Adjudicator
	clock       clock.Clock
	config      config.SchedulerConfig
	logger      *applog.Logger

	newTicker func(time.Duration) Ticker

	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	runMu    sync.Mutex
	started  atomic.Bool
	cycles   atomic.Int64
	reevaled atomic.Int64
	skipped  atomic.Int64
	failed   atomic.Int64
}

// New constructs a ReevalWorker. The tick source defaults to a real
// time.NewTicker; tests may replace newTicker to drive ticks deterministically.
func New(clk clock.Clock, adj *dispatch.Adjudicator, st store.Store,
	cfg config.SchedulerConfig, logger *applog.Logger) *ReevalWorker {
	return &ReevalWorker{
		store:       st,
		adjudicator: adj,
		clock:       clk,
		config:      cfg,
		logger:      logger,
		newTicker:   realTickerFactory,
	}
}

// Start launches the background loop. It returns immediately; the loop runs
// until ctx is canceled or Stop is called.
func (w *ReevalWorker) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.wg.Add(1)
	go w.loop()
	w.started.Store(true)
	w.logger.Info().Dur("interval", w.config.ReevalInterval).Msg("reeval worker started")
	return nil
}

// Stop signals the background loop to exit and waits for it to drain.
func (w *ReevalWorker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	w.started.Store(false)
	w.logger.Info().Msg("reeval worker stopped")
}

func (w *ReevalWorker) loop() {
	defer w.wg.Done()
	ticker := w.newTicker(w.config.ReevalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C():
			w.cycles.Add(1)
			taskCtx, cancel := context.WithTimeout(w.ctx, w.config.TaskTimeout)
			if err := w.RunOnce(taskCtx); err != nil {
				w.failed.Add(1)
				w.logger.Error().Err(err).Msg("reeval sweep failed")
			}
			cancel()
		}
	}
}

// RunOnce performs a single re-evaluation sweep. It is safe for concurrent
// invocation: an overlapping call (a previous sweep still running, or a tick
// firing during a manual run) is skipped rather than queued, so ticks never
// pile up and items are never processed by two sweeps at once.
func (w *ReevalWorker) RunOnce(ctx context.Context) error {
	if !w.runMu.TryLock() {
		w.skipped.Add(1)
		return nil
	}
	defer w.runMu.Unlock()
	return w.sweep(ctx)
}

// Started reports whether the background loop is running.
func (w *ReevalWorker) Started() bool { return w.started.Load() }

// Cycles returns the number of tick-driven sweeps attempted.
func (w *ReevalWorker) Cycles() int64 { return w.cycles.Load() }

// Reevaluated returns the number of items re-routed under a newer rule.
func (w *ReevalWorker) Reevaluated() int64 { return w.reevaled.Load() }

// Skipped returns the count of items intentionally left unchanged (already
// current, terminal, or with no matching rule) plus skipped overlapping sweeps.
func (w *ReevalWorker) Skipped() int64 { return w.skipped.Load() }

// Failed returns the number of sweeps that ended with an error.
func (w *ReevalWorker) Failed() int64 { return w.failed.Load() }
