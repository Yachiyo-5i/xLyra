package auth

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"

	"xlyra/server/internal/httpx"
)

func (s *Service) RequireAdminSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := AdminSessionTokenFromRequest(r)
		if token != "" {
			session, err := s.ValidateAdminSession(r.Context(), token)
			if err == nil {
				s.TouchAdminSession(r.Context(), session.ID)
				next.ServeHTTP(w, r.WithContext(WithAdminSession(r.Context(), session)))
				return
			}
		}

		accessTokenValue := strings.TrimSpace(r.Header.Get("X-Access-Token"))
		if accessTokenValue != "" {
			accessToken, err := s.ValidateAdminAccessToken(r.Context(), accessTokenValue, r.UserAgent(), r.RemoteAddr)
			if err == nil {
				actor := AdminActor{Type: "access_token", AdminID: accessToken.AdminID, AccessTokenID: accessToken.ID}
				s.RecordAudit(r.Context(), AuditInput{
					Actor:      actor,
					Action:     "admin_api.access",
					RemoteAddr: r.RemoteAddr,
					UserAgent:  r.UserAgent(),
					RequestID:  middleware.GetReqID(r.Context()),
					Success:    true,
					Metadata: map[string]any{
						"method": r.Method,
						"path":   r.URL.Path,
					},
				})
				next.ServeHTTP(w, r.WithContext(WithAdminActor(r.Context(), actor)))
				return
			}
		}

		httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "admin session is required")
	})
}

func (s *Service) RequireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := apiKeyFromRequest(r)
		apiKey, err := s.ValidateAPIKey(r.Context(), key)
		if err != nil {
			if failure, ok := APIKeyQuotaFailureFromError(err); ok {
				httpx.JSON(w, http.StatusUnauthorized, map[string]any{
					"error": failure.Payload(middleware.GetReqID(r.Context())),
				})
				return
			}
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "valid api key is required")
			return
		}

		next.ServeHTTP(w, r.WithContext(WithAPIKey(r.Context(), apiKey)))
	})
}

func (s *Service) RequireAPIKeyIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := apiKeyFromRequest(r)
		apiKey, err := s.ValidateAPIKeyIdentity(r.Context(), key)
		if err != nil {
			httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "valid api key is required")
			return
		}

		next.ServeHTTP(w, r.WithContext(WithAPIKey(r.Context(), apiKey)))
	})
}

func AdminSessionTokenFromRequest(r *http.Request) string {
	if cookie, err := r.Cookie("xlyra_admin_session"); err == nil {
		return strings.TrimSpace(cookie.Value)
	}

	return ""
}

func apiKeyFromRequest(r *http.Request) string {
	if token := bearerToken(r.Header.Get("Authorization")); token != "" {
		return token
	}

	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func bearerToken(value string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(value, prefix))
}
