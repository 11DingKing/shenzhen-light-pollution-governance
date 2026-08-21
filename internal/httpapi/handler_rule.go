package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"lightgovernance/internal/service"
)

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	var req service.CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, r, "invalid JSON: "+err.Error())
		return
	}
	rule, err := s.ruleSvc.CreateRule(r.Context(), req)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.ruleSvc.ListRules(r.Context())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules, "total": len(rules)})
}

func (s *Server) getRule(w http.ResponseWriter, r *http.Request) {
	versionStr := chi.URLParam(r, "version")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		writeBadRequest(w, r, "invalid rule version")
		return
	}
	rule, err := s.ruleSvc.GetRule(r.Context(), version)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}
