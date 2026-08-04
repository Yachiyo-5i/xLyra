package auth

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestCSRFTokenForSessionIsStableAndSessionBound(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "test-master-key")
	first := service.CSRFTokenForSession("xlyra_session_one")
	second := service.CSRFTokenForSession("xlyra_session_one")
	other := service.CSRFTokenForSession("xlyra_session_two")

	if first == "" {
		t.Fatal("expected csrf token")
	}
	if first != second {
		t.Fatal("expected stable csrf token for same session")
	}
	if first == other {
		t.Fatal("expected csrf token to be bound to session token")
	}
	if !service.ValidateCSRFToken("xlyra_session_one", first) {
		t.Fatal("expected csrf token to validate")
	}
	if service.ValidateCSRFToken("xlyra_session_one", other) {
		t.Fatal("expected token from another session to be rejected")
	}
}

func TestCSRFTokenForSessionRejectsBlankSessionAndUsesDefaultSecret(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "")
	if got := service.CSRFTokenForSession(" \t\n "); got != "" {
		t.Fatalf("blank session csrf token = %q, want empty", got)
	}

	sessionToken := "xlyra_session_cookie"
	defaultToken := service.CSRFTokenForSession(sessionToken)
	explicitToken := NewService(nil, "test-master-key").CSRFTokenForSession(sessionToken)
	if defaultToken == "" {
		t.Fatal("expected csrf token with default secret")
	}
	if defaultToken == explicitToken {
		t.Fatal("expected default secret token to differ from explicit master key token")
	}
	if !service.ValidateCSRFToken(sessionToken, defaultToken) {
		t.Fatal("expected token signed with default secret to validate")
	}
	if service.ValidateCSRFToken("", defaultToken) {
		t.Fatal("expected blank session to reject csrf token")
	}
}

func TestRequireAdminCSRFRejectsSessionMutationWithoutToken(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "test-master-key")
	handler := service.RequireAdminCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := newSessionRequest(http.MethodPost, "/api/v1/profile/account", "xlyra_session_cookie")
	rec := authPerform(handler, req)

	assertAuthErrorCode(t, rec, http.StatusForbidden, "csrf_token_required")
}

func TestRequireAdminCSRFRejectsSessionMutationWithInvalidToken(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "test-master-key")
	handler := service.RequireAdminCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := newSessionRequest(http.MethodPost, "/api/v1/profile/account", "xlyra_session_cookie")
	req.Header.Set(CSRFHeaderName, service.CSRFTokenForSession("xlyra_session_other"))
	rec := authPerform(handler, req)

	assertAuthErrorCode(t, rec, http.StatusForbidden, "csrf_token_invalid")
}

func TestRequireAdminCSRFAcceptsSessionMutationWithToken(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "test-master-key")
	handler := service.RequireAdminCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	sessionToken := "xlyra_session_cookie"
	req := newSessionRequest(http.MethodPut, "/api/v1/profile/account", sessionToken)
	req.Header.Set(CSRFHeaderName, service.CSRFTokenForSession(sessionToken))
	rec := authPerform(handler, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestRequireAdminCSRFPassesThroughSessionActorWithoutCookie(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "test-master-key")
	handler := service.RequireAdminCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := authTestRequest(http.MethodPost, "/api/v1/profile/account")
	req = req.WithContext(WithAdminActor(req.Context(), AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()}))
	rec := authPerform(handler, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestRequireAdminCSRFSkipsSafeMethodsAndAccessTokens(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "test-master-key")
	handler := service.RequireAdminCSRF(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		name   string
		method string
		cookie string
		header string
	}{
		{name: "get with session cookie", method: http.MethodGet, cookie: "xlyra_session_cookie"},
		{name: "head with session cookie", method: http.MethodHead, cookie: "xlyra_session_cookie"},
		{name: "options with session cookie", method: http.MethodOptions, cookie: "xlyra_session_cookie"},
		{name: "post with access token", method: http.MethodPost, header: "xlyra-admin-token"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := authTestRequest(tc.method, "/api/v1/profile")
			if tc.cookie != "" {
				req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: tc.cookie})
				req = req.WithContext(WithAdminActor(req.Context(), AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()}))
			}
			if tc.header != "" {
				req.Header.Set("X-Access-Token", tc.header)
				req = req.WithContext(WithAdminActor(req.Context(), AdminActor{Type: "access_token", AdminID: uuid.New(), AccessTokenID: uuid.New()}))
			}
			rec := authPerform(handler, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
			}
		})
	}
}

func newSessionRequest(method string, target string, sessionToken string) *http.Request {
	req := authTestRequest(method, target)
	req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: sessionToken})
	return req.WithContext(WithAdminActor(req.Context(), AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()}))
}
