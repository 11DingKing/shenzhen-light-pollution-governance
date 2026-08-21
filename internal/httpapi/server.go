package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"lightgovernance/internal/applog"
	"lightgovernance/internal/auth"
	"lightgovernance/internal/config"
	"lightgovernance/internal/dispatch"
	"lightgovernance/internal/scheduler"
	"lightgovernance/internal/service"
	"lightgovernance/internal/store"
	"lightgovernance/internal/worker"
)

type Server struct {
	config       *config.Config
	store        store.Store
	itemSvc      *service.ItemService
	ruleSvc      *service.RuleService
	batchSvc     *service.BatchService
	querySvc     *service.QueryService
	escSvc       *service.EscalationService
	scheduler    *scheduler.Scheduler
	reevalWorker *worker.ReevalWorker
	logger       *applog.Logger
	authStore    *auth.Store
	authTTL      time.Duration
	router       chi.Router
	httpSrv      *http.Server
}

func New(cfg *config.Config, st store.Store, clk clock.Clock, logger *applog.Logger, sched *scheduler.Scheduler) *Server {
	adj := dispatch.NewAdjudicator(clk)
	srv := &Server{
		config:       cfg,
		store:        st,
		itemSvc:      service.NewItemService(st, adj, clk, cfg.Business.DefaultDeadline),
		escSvc:       service.NewEscalationService(st, clk, cfg.Business.EscalationDeadlineExtension, cfg.Business.MaxEscalationLevel),
		ruleSvc:      service.NewRuleService(st, clk),
		querySvc:     service.NewQueryService(st, clk),
		scheduler:    sched,
		reevalWorker: worker.New(clk, adj, st, cfg.Scheduler, logger),
		logger:       logger,
		authTTL:      cfg.Auth.SessionTTL,
	}
	authPath := cfg.Auth.StorePath
	if !filepath.IsAbs(authPath) && cfg.Storage.DataDir != "" {
		authPath = filepath.Join(cfg.Storage.DataDir, filepath.Base(authPath))
	}
	bootstrap := make([]auth.BootstrapUser, 0, len(cfg.Auth.BootstrapUsers))
	for _, user := range cfg.Auth.BootstrapUsers {
		bootstrap = append(bootstrap, auth.BootstrapUser{ID: user.ID, Username: user.Username, Password: user.Password, Role: auth.Role(user.Role)})
	}
	authStore, err := auth.Open(authPath, bootstrap...)
	if err != nil {
		panic(fmt.Errorf("open auth store: %w", err))
	}
	srv.authStore = authStore
	srv.batchSvc = service.NewBatchService(st, srv.itemSvc, clk)
	srv.setupRouter()
	return srv
}

func (s *Server) setupRouter() {
	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(loggingMiddleware(s.logger))
	r.Use(recoveryMiddleware(s.logger))

	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)
	r.Post("/api/auth/login", s.login)
	r.Post("/api/auth/logout", s.logout)
	r.Get("/api/auth/me", s.me)

	r.Route("/api", func(r chi.Router) {
		if s.config.Auth.Required {
			r.Use(s.authenticationMiddleware)
		}
		r.Route("/items", func(r chi.Router) {
			r.Post("/", s.registerItem)
			r.Get("/", s.listItems)
			r.Get("/{id}", s.getItemDetail)
			r.Patch("/{id}", s.modifyItem)
			r.Post("/{id}/start", s.startProcessing)
			r.Post("/{id}/return", s.returnForCorrection)
			r.Post("/{id}/resubmit", s.resubmitItem)
			r.Post("/{id}/cancel", s.cancelItem)
			r.Post("/{id}/complete", s.completeItem)
		})
		r.Route("/rules", func(r chi.Router) {
			r.With(s.requireRoles(auth.RoleAdmin, auth.RoleLightingAssessor)).Post("/", s.createRule)
			r.Get("/", s.listRules)
			r.Get("/{version}", s.getRule)
		})
		r.Route("/batches", func(r chi.Router) {
			r.With(s.requireRoles(auth.RoleAdmin, auth.RoleLightingAssessor)).Post("/import", s.batchImport)
			r.Get("/export", s.batchExport)
			r.Get("/", s.listBatches)
		})
		r.Get("/stats/backlog", s.getBacklog)
		r.Get("/audit", s.listAudit)
		r.Route("/failures", func(r chi.Router) {
			r.Get("/", s.listFailures)
			r.With(s.requireRoles(auth.RoleAdmin)).Post("/{id}/retry", s.retryFailure)
		})
	})
	s.router = r
	s.httpSrv = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:      s.router,
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
	}
}

func (s *Server) ListenAndServe() error {
	s.logger.Info().Str("addr", s.httpSrv.Addr).Msg("http server starting")
	return s.httpSrv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("http server shutting down")
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	checks := map[string]string{}
	ready := true

	if err := s.store.Ping(ctx); err != nil {
		checks["store"] = "unavailable: " + err.Error()
		ready = false
	} else {
		checks["store"] = "ok"
	}

	f, err := os.CreateTemp(s.config.Storage.DataDir, ".readyz-*")
	if err != nil {
		checks["data_dir"] = "not writable: " + err.Error()
		ready = false
	} else {
		f.Close()
		os.Remove(f.Name())
		checks["data_dir"] = "ok"
	}

	if s.scheduler == nil || !s.scheduler.Started() {
		checks["scheduler"] = "not started"
		ready = false
	} else {
		checks["scheduler"] = "ok"
	}

	if s.reevalWorker == nil || !s.reevalWorker.Started() {
		checks["reeval_worker"] = "not started"
		ready = false
	} else {
		checks["reeval_worker"] = "ok"
	}

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{
		"status": map[bool]string{true: "ready", false: "not_ready"}[ready],
		"checks": checks,
	})
}

func (s *Server) EscSvc() *service.EscalationService {
	return s.escSvc
}

func (s *Server) ReevalWorker() *worker.ReevalWorker {
	return s.reevalWorker
}

func (s *Server) SetScheduler(sched *scheduler.Scheduler) {
	s.scheduler = sched
}

func (s *Server) Handler() http.Handler {
	return s.router
}
