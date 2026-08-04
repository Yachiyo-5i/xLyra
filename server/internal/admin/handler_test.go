package admin

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/newapi"
	"xlyra/server/internal/store"
	"xlyra/server/internal/usage"
)

func TestUsageFromCredentialMetaDoesNotInventZeroQuota(t *testing.T) {
	t.Parallel()

	usage := usageFromCredentialMeta(map[string]any{"name": "legacy"}, nil)
	if usage["success"] != false {
		t.Fatalf("expected missing quota data to be marked unavailable, got %#v", usage)
	}
	if _, ok := usage["data"]; ok {
		t.Fatalf("expected missing quota data to omit data payload, got %#v", usage)
	}
}

func TestGatewayModelCacheHelpersNoopWithoutGatewayOrAPIKeyID(t *testing.T) {
	t.Parallel()

	handler := Handler{}

	handler.invalidateGatewayModelsCache()
	handler.invalidateGatewayModelsCacheForAPIKey(uuid.New())
	handler.prewarmGatewayModelsCacheForAPIKey(store.APIKey{})
}

func TestUsageFromCredentialMetaUsesTokenQuotaSnapshot(t *testing.T) {
	t.Parallel()

	usage := usageFromCredentialMeta(map[string]any{
		"name":         "chat",
		"remain_quota": float64(1500000),
		"used_quota":   float64(500000),
	}, nil)
	data, ok := usage["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected usage data, got %#v", usage)
	}
	if got := data["total_available"]; got != 1500000 {
		t.Fatalf("expected available quota 1500000, got %#v", got)
	}
	if got := data["total_granted"]; got != 2000000 {
		t.Fatalf("expected granted quota 2000000, got %#v", got)
	}
}

func TestFreshAPIKeyMetaPatchAddsQuotaSnapshotWithoutNumericChurn(t *testing.T) {
	t.Parallel()

	current := map[string]any{
		"upstream_id": float64(7),
		"name":        "chat",
		"status":      float64(1),
	}
	freshKey := newapi.UserAPIKey{
		ID:     7,
		Name:   "chat",
		Status: 1,
		Raw: map[string]any{
			"remain_quota": float64(1500000),
			"used_quota":   float64(500000),
		},
	}

	patch := freshAPIKeyMetaPatch(current, freshKey)
	if _, ok := patch["upstream_id"]; ok {
		t.Fatalf("expected numeric-equivalent upstream_id to be ignored, got %#v", patch)
	}
	if _, ok := patch["status"]; ok {
		t.Fatalf("expected numeric-equivalent status to be ignored, got %#v", patch)
	}
	if got := patch["remain_quota"]; got != float64(1500000) {
		t.Fatalf("expected quota snapshot in patch, got %#v", patch)
	}
}

func TestBootstrapStatusUsesStandardUnavailableError(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.BootstrapStatus, adminTestRequest(http.MethodGet, "/api/v1/bootstrap/status", ""))
	adminAssertStatus(t, rec, http.StatusServiceUnavailable)
	body := adminDecodeJSON[struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}](t, rec)
	if body.Error.Code != "auth_unavailable" {
		t.Fatalf("expected auth_unavailable, got %q", body.Error.Code)
	}
}

func TestListRequestLogsRejectsInvalidPage(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, nil, nil, nil, usage.NewService(nil), nil, nil, nil, nil, nil)
	rec := adminPerform(handler.ListRequestLogs, adminTestRequest(http.MethodGet, "/api/v1/requests?page=0", ""))
	adminAssertStatus(t, rec, http.StatusBadRequest)
	body := adminDecodeJSON[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)
	if body.Error.Code != "invalid_page" {
		t.Fatalf("expected invalid_page, got %q", body.Error.Code)
	}
}

func TestCreateAPIKeyRequiresAdminSession(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.CreateAPIKey, adminTestRequest(http.MethodPost, "/api/v1/api-keys", `{"name":"demo"}`))

	adminAssertStatus(t, rec, http.StatusServiceUnavailable)
}

func TestAuthHandlersRequireAuthService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	for _, tc := range []struct {
		name string
		req  *http.Request
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "bootstrap_register", req: adminTestRequest(http.MethodPost, "/api/v1/bootstrap/register", `{}`), call: handler.BootstrapRegister},
		{name: "create_session", req: adminTestRequest(http.MethodPost, "/api/v1/auth/session", `{}`), call: handler.CreateSession},
		{name: "delete_session", req: adminTestRequest(http.MethodDelete, "/api/v1/auth/session", ""), call: handler.DeleteSession},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, tc.req)

			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "auth_unavailable")
		})
	}
}

func TestCurrentSessionRequiresAuthenticatedActor(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.CurrentSession, adminTestRequest(http.MethodGet, "/api/v1/auth/session", ""))

	assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestSessionPayloadIncludesCSRFToken(t *testing.T) {
	t.Parallel()

	payload := sessionPayload(auth.LoginResult{
		CSRFToken: "csrf-token",
		Admin: store.Admin{
			ID:       uuid.New(),
			Username: "alice",
			Role:     "owner",
			Status:   "active",
		},
	})

	if payload["csrf_token"] != "csrf-token" {
		t.Fatalf("csrf_token = %#v, want csrf-token", payload["csrf_token"])
	}
}

func TestCurrentSessionPayloadCanIncludeCSRFToken(t *testing.T) {
	t.Parallel()

	service := auth.NewService(nil, adminTestMasterKey)
	sessionToken := "xlyra_session_cookie"
	req := adminTestRequest(http.MethodGet, "/api/v1/auth/session", "")
	req.AddCookie(&http.Cookie{Name: "xlyra_admin_session", Value: sessionToken})
	req = req.WithContext(auth.WithAdminSession(req.Context(), store.AdminSession{ID: uuid.New(), AdminID: uuid.New()}))

	payload := map[string]any{}
	if _, ok := auth.AdminSessionFromContext(req.Context()); ok {
		payload["csrf_token"] = service.CSRFTokenForSession(auth.AdminSessionTokenFromRequest(req))
	}

	want := service.CSRFTokenForSession(sessionToken)
	if payload["csrf_token"] != want {
		t.Fatalf("csrf_token = %#v, want %q", payload["csrf_token"], want)
	}
}

func TestSessionCookieSecureHonorsTLSAndForwardedHeaders(t *testing.T) {
	t.Parallel()

	if sessionCookieSecure(nil) {
		t.Fatal("nil request should not be secure")
	}
	req := adminTestRequest(http.MethodGet, "/api/v1/auth/session", "")
	if sessionCookieSecure(req) {
		t.Fatal("plain request should not be secure")
	}
	req.TLS = &tls.ConnectionState{}
	if !sessionCookieSecure(req) {
		t.Fatal("TLS request should be secure")
	}

	req = adminTestRequest(http.MethodGet, "/api/v1/auth/session", "")
	req.Header.Set("X-Forwarded-Proto", " https ")
	if !sessionCookieSecure(req) {
		t.Fatal("X-Forwarded-Proto=https should be secure")
	}

	req = adminTestRequest(http.MethodGet, "/api/v1/auth/session", "")
	req.Header.Set("X-Forwarded-Ssl", "ON")
	if !sessionCookieSecure(req) {
		t.Fatal("X-Forwarded-Ssl=on should be secure")
	}
}

func TestWriteSessionCookieUsesSecurityDefaultsAndExpiry(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	req := adminTestRequest(http.MethodPost, "/api/v1/auth/session", "")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := adminRecorder()

	writeSessionCookie(rec, req, auth.LoginResult{Token: "session-token", ExpiresAt: &expiresAt})

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != "xlyra_admin_session" || cookie.Value != "session-token" || cookie.Path != "/" {
		t.Fatalf("unexpected session cookie identity: %#v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unexpected session cookie security flags: %#v", cookie)
	}
	if !cookie.Expires.Equal(expiresAt) {
		t.Fatalf("cookie expiry = %s, want %s", cookie.Expires, expiresAt)
	}
}

func TestAuditAdminMutationHelpers(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if !shouldAuditAdminMutation(method) {
			t.Fatalf("%s should be audited", method)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		if shouldAuditAdminMutation(method) {
			t.Fatalf("%s should not be audited", method)
		}
	}

	rec := adminRecorder()
	writer := &auditResponseWriter{ResponseWriter: rec}
	if _, err := writer.Write([]byte("ok")); err != nil {
		t.Fatalf("write audit response: %v", err)
	}
	if writer.status != http.StatusOK {
		t.Fatalf("implicit write status = %d, want 200", writer.status)
	}
	writer.WriteHeader(http.StatusCreated)
	if writer.status != http.StatusCreated {
		t.Fatalf("explicit write status = %d, want 201", writer.status)
	}
}

func TestAuditAdminMutationPassesThroughWithoutAuthService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	nextCalled := false
	wrapped := handler.AuditAdminMutation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusAccepted)
	}))
	req := adminTestRequest(http.MethodPost, "/api/v1/sites?debug=1", "")
	rec := adminRecorder()

	wrapped.ServeHTTP(rec, req)

	if !nextCalled || rec.Code != http.StatusAccepted {
		t.Fatalf("expected middleware to pass through, nextCalled=%v status=%d", nextCalled, rec.Code)
	}
}
