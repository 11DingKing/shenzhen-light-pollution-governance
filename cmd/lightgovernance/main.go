package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/benbjohnson/clock"

	"lightgovernance/internal/applog"
	"lightgovernance/internal/config"
	"lightgovernance/internal/httpapi"
	"lightgovernance/internal/repo"
	"lightgovernance/internal/scheduler"
)

func main() {
	configPath := os.Getenv("SHENZHEN_LIGHT_GOVERNANCE_CONFIG")
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger := applog.New(cfg.Logging.Level, cfg.Logging.Format)
	clk := clock.New()

	ctx := context.Background()
	st, err := repo.New(ctx, cfg.Storage.DataDir, clk, cfg.Storage.ShardMaxSize, cfg.Storage.SyncOnWrite)
	if err != nil {
		logger.Error().Err(err).Msg("failed to open store")
		os.Exit(1)
	}

	httpSrv := httpapi.New(cfg, st, clk, logger, nil)

	sched := scheduler.New(clk, httpSrv.EscSvc(), st, cfg.Scheduler, logger)
	if err := sched.Start(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to start scheduler")
		os.Exit(1)
	}
	httpSrv.SetScheduler(sched)

	reevalWorker := httpSrv.ReevalWorker()
	if err := reevalWorker.Start(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to start reeval worker")
		os.Exit(1)
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info().Msg("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
		sched.Stop()
		reevalWorker.Stop()
		_ = st.Close()
	}()

	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error().Err(err).Msg("server stopped")
	}
	logger.Info().Msg("service exited")
}
