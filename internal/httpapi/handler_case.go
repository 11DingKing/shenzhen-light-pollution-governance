package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"lightgovernance/internal/domain"
	"lightgovernance/internal/service"
)

func (s *Server) registerItem(w http.ResponseWriter, r *http.Request) {
	var req service.RegisterItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, r, "invalid JSON: "+err.Error())
		return
	}
	item, err := s.itemSvc.Register(r.Context(), req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	filter := parseItemFilter(r)
	items, total, err := s.querySvc.ListItems(r.Context(), filter)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, PaginatedResponse{
		Items: items, Total: total,
		PageSize: filter.PageSize, PageOffset: filter.PageOffset,
	})
}

func (s *Server) getItemDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeBadRequest(w, r, "item id required")
		return
	}
	detail, err := s.querySvc.GetItemDetail(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) modifyItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeBadRequest(w, r, "item id required")
		return
	}
	var req service.ModifyItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, r, "invalid JSON: "+err.Error())
		return
	}
	item, err := s.itemSvc.Modify(r.Context(), id, req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) startProcessing(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor := r.URL.Query().Get("actor")
	if actor == "" {
		actor = "unknown"
	}
	item, err := s.itemSvc.StartProcessing(r.Context(), id, actor)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) returnForCorrection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Reason string `json:"reason"`
		Actor  string `json:"actor"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Actor == "" {
		req.Actor = "unknown"
	}
	item, err := s.itemSvc.ReturnForCorrection(r.Context(), id, req.Reason, req.Actor)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) resubmitItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor := r.URL.Query().Get("actor")
	if actor == "" {
		actor = "unknown"
	}
	item, err := s.itemSvc.Resubmit(r.Context(), id, actor)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) cancelItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Reason string `json:"reason"`
		Actor  string `json:"actor"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Actor == "" {
		req.Actor = "unknown"
	}
	item, err := s.itemSvc.Cancel(r.Context(), id, req.Reason, req.Actor)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) completeItem(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor := r.URL.Query().Get("actor")
	if actor == "" {
		actor = "unknown"
	}
	item, err := s.itemSvc.Complete(r.Context(), id, actor)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func parseItemFilter(r *http.Request) domain.ItemFilter {
	filter := domain.ItemFilter{
		Status:         domain.ItemStatus(r.URL.Query().Get("status")),
		LeadDepartment: r.URL.Query().Get("lead_department"),
		StoreID:        r.URL.Query().Get("store_id"),
		RegisteredBy:   r.URL.Query().Get("reported_by"),
		PageSize:       parsePageSize(r),
		PageOffset:     parsePageOffset(r),
		OverdueOnly:    r.URL.Query().Get("overdue") == "true",
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
	return filter
}
