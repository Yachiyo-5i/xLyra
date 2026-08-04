package admin

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/usage"
)

func TestProfileHandlersRequireAdminSessionBeforeBodyOrServiceWork(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	sessionID := uuid.NewString()
	cases := []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "update account", method: http.MethodPatch, target: "/api/v1/profile/account", body: `{"username":"alice"}`, call: handler.UpdateProfileAccount},
		{name: "update password", method: http.MethodPatch, target: "/api/v1/profile/password", body: `{"current_password":"old","new_password":"new"}`, call: handler.UpdateProfilePassword},
		{name: "list sessions", method: http.MethodGet, target: "/api/v1/profile/sessions", call: handler.ListProfileSessions},
		{name: "delete other sessions", method: http.MethodDelete, target: "/api/v1/profile/sessions", call: handler.DeleteOtherProfileSessions},
		{name: "create access token", method: http.MethodPost, target: "/api/v1/profile/access-token", call: handler.CreateProfileAccessToken},
		{name: "setup totp", method: http.MethodPost, target: "/api/v1/profile/totp/setup", call: handler.SetupProfileTOTP},
		{name: "enable totp", method: http.MethodPost, target: "/api/v1/profile/totp/enable", body: `{"code":"123456"}`, call: handler.EnableProfileTOTP},
		{name: "disable totp", method: http.MethodPost, target: "/api/v1/profile/totp/disable", body: `{"current_password":"pw","code":"123456"}`, call: handler.DisableProfileTOTP},
		{name: "delete session", method: http.MethodDelete, target: "/api/v1/profile/sessions/" + sessionID, call: handler.DeleteProfileSession},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(tc.method, tc.target, tc.body)
			if tc.name == "delete session" {
				req = adminRequestWithRouteParam(tc.method, tc.target, tc.body, "sessionID", sessionID)
			}
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
		})
	}
}

func TestRequestHandlersRequireUsageService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	requestLogID := uuid.NewString()
	cases := []struct {
		name   string
		method string
		target string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "list", method: http.MethodGet, target: "/api/v1/requests", call: handler.ListRequestLogs},
		{name: "summary", method: http.MethodGet, target: "/api/v1/requests/summary", call: handler.RequestLogSummary},
		{name: "channel split", method: http.MethodGet, target: "/api/v1/requests/channel-split", call: handler.RequestChannelSplit},
		{name: "get", method: http.MethodGet, target: "/api/v1/requests/" + requestLogID, call: handler.GetRequestLog},
		{name: "route traces", method: http.MethodGet, target: "/api/v1/routes/traces", call: handler.ListRouteTraces},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(tc.method, tc.target, "")
			if tc.name == "get" {
				req = adminRequestWithRouteParam(tc.method, tc.target, "", "requestLogID", requestLogID)
			}
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "request_log_service_unavailable")
		})
	}
}

func TestListRequestLogsRejectsInvalidPageSizeBeforeQueryingUsage(t *testing.T) {
	t.Parallel()

	handler := Handler{usage: usage.NewService(nil)}
	for _, raw := range []string{"abc", "0", "-1"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(handler.ListRequestLogs, adminTestRequest(http.MethodGet, "/api/v1/requests?page_size="+raw, ""))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_page_size")
		})
	}
}

func TestListRequestLogsRejectsInvalidPageBeforeQueryingUsage(t *testing.T) {
	t.Parallel()

	handler := Handler{usage: usage.NewService(nil)}
	for _, raw := range []string{"abc", "0", "-1"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(handler.ListRequestLogs, adminTestRequest(http.MethodGet, "/api/v1/requests?page="+raw, ""))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_page")
		})
	}
}

func TestListRequestLogsRejectsInvalidFilterValuesBeforeQueryingUsage(t *testing.T) {
	t.Parallel()

	handler := Handler{usage: usage.NewService(nil)}
	for _, tc := range []struct {
		name   string
		target string
		code   string
	}{
		{name: "site id", target: "/api/v1/requests?site_id=bad-id", code: "invalid_site_id"},
		{name: "api key id", target: "/api/v1/requests?api_key_id=bad-id", code: "invalid_api_key_id"},
		{name: "created from", target: "/api/v1/requests?created_from=tomorrow", code: "invalid_created_from"},
		{name: "created to", target: "/api/v1/requests?created_to=tomorrow", code: "invalid_created_to"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(handler.ListRequestLogs, adminTestRequest(http.MethodGet, tc.target, ""))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestRequestChannelSplitRejectsInvalidUUIDQueryBeforeQueryingUsage(t *testing.T) {
	t.Parallel()

	handler := Handler{usage: usage.NewService(nil)}
	for _, tc := range []struct {
		name   string
		target string
		code   string
	}{
		{name: "site id", target: "/api/v1/requests/channel-split?site_id=bad-id", code: "invalid_site_id"},
		{name: "oauth connection id", target: "/api/v1/requests/channel-split?oauth_connection_id=bad-id", code: "invalid_oauth_connection_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(handler.RequestChannelSplit, adminTestRequest(http.MethodGet, tc.target, ""))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestGetRequestLogRejectsInvalidRouteIDBeforeQueryingUsage(t *testing.T) {
	t.Parallel()

	handler := Handler{usage: usage.NewService(nil)}
	req := adminRequestWithRouteParam(http.MethodGet, "/api/v1/requests/not-a-uuid", "", "requestLogID", "not-a-uuid")
	rec := adminPerform(handler.GetRequestLog, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_request_log_id")
}

func TestListRouteTracesRejectsInvalidLimitBeforeQueryingUsage(t *testing.T) {
	t.Parallel()

	handler := Handler{usage: usage.NewService(nil)}
	for _, raw := range []string{"abc", "0", "-1"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(handler.ListRouteTraces, adminTestRequest(http.MethodGet, "/api/v1/routes/traces?limit="+raw, ""))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_limit")
		})
	}
}

func TestClearRouteCooldownRejectsInvalidPayloadBeforeClearingRouterCooldown(t *testing.T) {
	t.Parallel()

	handler := Handler{router: routeengine.NewService(nil)}
	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "invalid json", body: `{`, code: "invalid_json"},
		{name: "missing site id", body: `{}`, code: "invalid_site_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(handler.ClearRouteCooldown, adminTestRequest(http.MethodPost, "/api/v1/routes/cooldowns/clear", tc.body))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}
