package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"lightgovernance/internal/domain"
)

func (s *Server) getBacklog(w http.ResponseWriter, r *http.Request) {
	stats, err := s.querySvc.GetBacklog(r.Context())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	filter := domain.AuditFilter{
		EntityID:   r.URL.Query().Get("entity_id"),
		EntityType: r.URL.Query().Get("entity_type"),
		Actor:      r.URL.Query().Get("actor"),
		PageSize:   parsePageSize(r),
		PageOffset: parsePageOffset(r),
	}
	if from := r.URL.Query().Get("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			filter.From = t
		}
	}
	if to := r.URL.Query().Get("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			filter.To = t
		}
	}
	entries, total, err := s.querySvc.ListAudit(r.Context(), filter)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, PaginatedResponse{
		Items: entries, Total: total,
		PageSize: filter.PageSize, PageOffset: filter.PageOffset,
	})
}

func (s *Server) listFailures(w http.ResponseWriter, r *http.Request) {
	failures, err := s.querySvc.ListFailures(r.Context())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"failures": failures, "total": len(failures)})
}

func (s *Server) retryFailure(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeBadRequest(w, r, "failure id required")
		return
	}
	actor := r.URL.Query().Get("actor")
	if actor == "" {
		actor = "admin"
	}
	if err := s.querySvc.RetryFailure(r.Context(), id, actor); err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "retried"})
}
