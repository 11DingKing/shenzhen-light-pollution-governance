package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"lightgovernance/internal/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func bearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) > 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, r, "invalid JSON: "+err.Error())
		return
	}
	user, token, expires, err := s.authStore.Login(req.Username, req.Password, s.authTTL, time.Now().UTC())
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, "", http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码错误")
		return
	}
	if err != nil {
		writeError(w, "", http.StatusInternalServerError, "AUTH_STORE_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": expires, "user": user})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, "", http.StatusUnauthorized, "UNAUTHORIZED", "缺少会话令牌")
		return
	}
	if err := s.authStore.Logout(token, time.Now().UTC()); err != nil {
		writeError(w, "", http.StatusUnauthorized, "INVALID_SESSION", "会话无效")
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, "", http.StatusUnauthorized, "UNAUTHORIZED", "缺少会话令牌")
		return
	}
	user, err := s.authStore.Resolve(token, time.Now().UTC())
	if errors.Is(err, auth.ErrSessionExpired) {
		writeError(w, "", http.StatusUnauthorized, "SESSION_EXPIRED", "会话已过期")
		return
	}
	if errors.Is(err, auth.ErrSessionRevoked) {
		writeError(w, "", http.StatusUnauthorized, "SESSION_REVOKED", "会话已退出")
		return
	}
	if err != nil {
		writeError(w, "", http.StatusUnauthorized, "INVALID_SESSION", "会话无效")
		return
	}
	writeJSON(w, http.StatusOK, user)
}
