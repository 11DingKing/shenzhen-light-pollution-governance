package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"lightgovernance/internal/domain"
	"lightgovernance/internal/service"
)

func (s *Server) batchImport(w http.ResponseWriter, r *http.Request) {
	var req service.BatchImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, r, "invalid JSON: "+err.Error())
		return
	}
	result, err := s.batchSvc.Import(r.Context(), req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) batchExport(w http.ResponseWriter, r *http.Request) {
	var req service.BatchExportRequest
	if from := r.URL.Query().Get("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			req.From = t
		}
	}
	if to := r.URL.Query().Get("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			req.To = t
		}
	}
	items, total, err := s.batchSvc.Export(r.Context(), req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, PaginatedResponse{
		Items: items, Total: total,
		PageSize: len(items), PageOffset: 0,
	})
}

func (s *Server) listBatches(w http.ResponseWriter, r *http.Request) {
	filter := domain.BatchFilter{
		StoreID:    r.URL.Query().Get("store_id"),
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
	batches, total, err := s.querySvc.ListBatches(r.Context(), filter)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, PaginatedResponse{
		Items: batches, Total: total,
		PageSize: filter.PageSize, PageOffset: filter.PageOffset,
	})
}
