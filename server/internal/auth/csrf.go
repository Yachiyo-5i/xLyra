package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"

	"xlyra/server/internal/httpx"
)

// csrfFallbackSecret is a process-random secret used only if the master key is
// somehow empty (a misconfiguration). A random per-process value avoids the
// previous predictable constant, which would let an attacker forge CSRF tokens.
var csrfFallbackSecret = sync.OnceValue(func() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic("generate csrf fallback secret: " + err.Error())
	}
	return base64.RawStdEncoding.EncodeToString(buf)
})

const CSRFHeaderName = "X-CSRF-Token"

func (s *Service) CSRFTokenForSession(sessionToken string) string {
	sessionToken = strings.TrimSpace(sessionToken)
	if sessionToken == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(s.csrfSecret()))
	_, _ = mac.Write([]byte("xlyra-admin-csrf\x00"))
	_, _ = mac.Write([]byte(sessionToken))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) ValidateCSRFToken(sessionToken string, csrfToken string) bool {
	expected := s.CSRFTokenForSession(sessionToken)
	csrfToken = strings.TrimSpace(csrfToken)
	if expected == "" || csrfToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(csrfToken)) == 1
}

func (s *Service) RequireAdminCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requiresCSRF(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		actor, ok := AdminActorFromContext(r.Context())
		if !ok || actor.Type != "session" {
			next.ServeHTTP(w, r)
			return
		}

		sessionToken := AdminSessionTokenFromRequest(r)
		if sessionToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		csrfToken := strings.TrimSpace(r.Header.Get(CSRFHeaderName))
		if csrfToken == "" {
			httpx.Error(w, r, http.StatusForbidden, "csrf_token_required", "CSRF token is required")
			return
		}
		if !s.ValidateCSRFToken(sessionToken, csrfToken) {
			httpx.Error(w, r, http.StatusForbidden, "csrf_token_invalid", "CSRF token is invalid")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func requiresCSRF(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func (s *Service) csrfSecret() string {
	if s == nil || s.masterKey == "" {
		return csrfFallbackSecret()
	}
	return s.masterKey
}
