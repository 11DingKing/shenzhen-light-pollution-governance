package httpapi

import (
	"context"
	"errors"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"lightgovernance/internal/applog"
	"lightgovernance/internal/auth"
)

type authContextKey struct{}

func (s *Server) authenticationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeError(w, middleware.GetReqID(r.Context()), http.StatusUnauthorized, "UNAUTHORIZED", "缺少会话令牌")
			return
		}
		user, err := s.authStore.Resolve(token, time.Now().UTC())
		if err != nil {
			code := "INVALID_SESSION"
			if errors.Is(err, auth.ErrSessionExpired) {
				code = "SESSION_EXPIRED"
			}
			if errors.Is(err, auth.ErrSessionRevoked) {
				code = "SESSION_REVOKED"
			}
			writeError(w, middleware.GetReqID(r.Context()), http.StatusUnauthorized, code, "会话无效或已失效")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authContextKey{}, user)))
	})
}

func (s *Server) requireRoles(roles ...auth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := r.Context().Value(authContextKey{}).(auth.User)
			if !ok || auth.RequireRole(user, roles...) != nil {
				writeError(w, middleware.GetReqID(r.Context()), http.StatusForbidden, "FORBIDDEN", "当前角色无权执行此操作")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func loggingMiddleware(logger *applog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			duration := time.Since(start)
			logger.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", ww.Status()).
				Dur("duration", duration).
				Str("request_id", middleware.GetReqID(r.Context())).
				Msg("request")
		})
	}
}

func recoveryMiddleware(logger *applog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error().
						Interface("panic", rec).
						Bytes("stack", debug.Stack()).
						Str("request_id", middleware.GetReqID(r.Context())).
						Msg("panic recovered")
					requestID := middleware.GetReqID(r.Context())
					writeError(w, requestID, http.StatusInternalServerError, "PANIC", "internal panic")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
