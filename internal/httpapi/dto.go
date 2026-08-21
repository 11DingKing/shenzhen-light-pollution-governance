package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5/middleware"

	"lightgovernance/internal/domain"
)

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type PaginatedResponse struct {
	Items      any `json:"items"`
	Total      int `json:"total"`
	PageSize   int `json:"page_size"`
	PageOffset int `json:"page_offset"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

func writeError(w http.ResponseWriter, requestID string, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{
		Error: ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	})
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	requestID := middleware.GetReqID(r.Context())

	var valErr domain.ValidationError
	if errors.As(err, &valErr) {
		writeError(w, requestID, http.StatusBadRequest, "VALIDATION_ERROR", valErr.Field+": "+valErr.Message)
		return
	}
	switch {
	case errors.Is(err, domain.ErrValidation):
		writeError(w, requestID, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, requestID, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, domain.ErrInvalidTransition):
		writeError(w, requestID, http.StatusConflict, "INVALID_TRANSITION", err.Error())
	case errors.Is(err, domain.ErrAlreadyCompleted):
		writeError(w, requestID, http.StatusConflict, "ALREADY_COMPLETED", err.Error())
	case errors.Is(err, domain.ErrAlreadyCancelled):
		writeError(w, requestID, http.StatusConflict, "ALREADY_CANCELLED", err.Error())
	case errors.Is(err, domain.ErrNoMatchingRule):
		writeError(w, requestID, http.StatusUnprocessableEntity, "NO_MATCHING_RULE", err.Error())
	case errors.Is(err, domain.ErrMaxEscalationReached):
		writeError(w, requestID, http.StatusConflict, "MAX_ESCALATION", err.Error())
	case errors.Is(err, domain.ErrDuplicate):
		writeError(w, requestID, http.StatusConflict, "DUPLICATE", err.Error())
	default:
		writeError(w, requestID, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
	}
}

func writeBadRequest(w http.ResponseWriter, r *http.Request, message string) {
	requestID := middleware.GetReqID(r.Context())
	writeError(w, requestID, http.StatusBadRequest, "BAD_REQUEST", message)
}

func parsePageSize(r *http.Request) int {
	s := r.URL.Query().Get("page_size")
	if s == "" {
		return 20
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 20
	}
	if n > 200 {
		return 200
	}
	return n
}

func parsePageOffset(r *http.Request) int {
	s := r.URL.Query().Get("page_offset")
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
