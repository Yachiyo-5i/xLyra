package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/dashboard"
	"xlyra/server/internal/store"
	"xlyra/server/internal/systemstats"
)

func TestCreateAPIKeyRejectsMalformedPermissionFieldsBeforeRepository(t *testing.T) {
	t.Parallel()

	handler := adminAuthHandlerWithoutStore()
	adminID := uuid.New()
	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{
			name: "expires_at_non_string",
			body: `{"name":"malformed-permissions","expires_at":123}`,
			code: "invalid_expires_at",
		},
		{
			name: "site_model_id",
			body: `{"name":"malformed-permissions","site_model_ids":["not-a-uuid"]}`,
			code: "invalid_site_model_id",
		},
		{
			name: "site_group_id",
			body: `{"name":"malformed-permissions","site_group_ids":["not-a-uuid"]}`,
			code: "invalid_group_id",
		},
		{
			name: "site_id",
			body: `{"name":"malformed-permissions","site_ids":["not-a-uuid"]}`,
			code: "invalid_site_id",
		},
		{
			name: "bad_rate_limit_shape",
			body: `{"name":"malformed-permissions","rate_limit":{"rpm_limit":"fast"}}`,
			code: "invalid_json",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", tc.body)
			req = req.WithContext(auth.WithAdminID(req.Context(), adminID))
			rec := adminPerform(handler.CreateAPIKey, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestAPIKeyPermissionPayloadHelpersReturnNilForMissingOrInvalidMappings(t *testing.T) {
	t.Parallel()

	if got := apiKeySiteModelPayloads(nil); got != nil {
		t.Fatalf("nil site model payloads = %#v, want nil", got)
	}
	if got := apiKeySitePayloads(nil); got != nil {
		t.Fatalf("nil site payloads = %#v, want nil", got)
	}
	if got := apiKeySiteGroupPayloads(nil); got != nil {
		t.Fatalf("nil site group payloads = %#v, want nil", got)
	}
	if got := modelMappingsPayload(store.JSON(`{"downstream":}`)); got != nil {
		t.Fatalf("invalid model mappings payload = %#v, want nil", got)
	}
	if got := modelMappingsPayload(store.JSON(`{}`)); got != nil {
		t.Fatalf("empty model mappings payload = %#v, want nil", got)
	}
}

func TestBootstrapRegisterRejectsInvalidCredentialsBeforeRepository(t *testing.T) {
	t.Parallel()

	handler := adminAuthHandlerWithoutStore()
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing_username", body: `{"password":"ValidPass123!"}`},
		{name: "short_password", body: `{"username":"admin","password":"short"}`},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertAdminHandlerError(
				t,
				handler.BootstrapRegister,
				http.MethodPost,
				"/api/v1/bootstrap/register",
				tc.body,
				http.StatusBadRequest,
				"invalid_registration",
			)
		})
	}
}

func TestCurrentSessionRejectsUnavailableAuthServiceWithActor(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/auth/session", "")
	req = req.WithContext(auth.WithAdminActor(req.Context(), auth.AdminActor{Type: "session", AdminID: uuid.New()}))
	rec := adminPerform((Handler{}).CurrentSession, req)

	assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestDashboardEndpointsReturnServiceErrorsWhenStoreUnavailable(t *testing.T) {
	t.Parallel()

	handler := Handler{dashboard: dashboard.NewService(nil)}
	for _, tc := range []struct {
		name   string
		call   func(http.ResponseWriter, *http.Request)
		status int
		code   string
	}{
		{name: "usage", call: handler.DashboardUsage, status: http.StatusInternalServerError, code: "dashboard_usage_failed"},
		{name: "cooldowns", call: handler.DashboardCooldowns, status: http.StatusInternalServerError, code: "dashboard_cooldowns_failed"},
		{name: "health", call: handler.DashboardHealth, status: http.StatusInternalServerError, code: "dashboard_health_failed"},
		{name: "insights", call: handler.DashboardInsights, status: http.StatusInternalServerError, code: "dashboard_insights_failed"},
		{name: "epaper", call: handler.DashboardEpaperSummary, status: http.StatusInternalServerError, code: "dashboard_epaper_summary_failed"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertAdminHandlerError(
				t,
				tc.call,
				http.MethodGet,
				"/api/v1/dashboard/"+tc.name,
				"",
				tc.status,
				tc.code,
			)
		})
	}
}

func TestDashboardResourceStreamWritesInitialSnapshotBeforeExit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := adminTestRequest(http.MethodGet, "/api/v1/dashboard/resources/stream", "").WithContext(ctx)
	rec := adminPerform((Handler{system: systemstats.NewService()}).DashboardResourceStream, req)

	adminAssertStatus(t, rec, http.StatusOK)
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("content type = %q, want text/event-stream; charset=utf-8", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "retry: 3000\n\n") || !strings.Contains(body, "event: resource\n") {
		t.Fatalf("stream body missing retry or resource event: %q", body)
	}
}

func adminAuthHandlerWithoutStore() Handler {
	return Handler{auth: adminAuthService()}
}

func assertAdminHandlerError(t *testing.T, call func(http.ResponseWriter, *http.Request), method string, target string, body string, status int, code string) {
	t.Helper()

	rec := adminPerform(call, adminTestRequest(method, target, body))
	assertAdminErrorCode(t, rec, status, code)
}
