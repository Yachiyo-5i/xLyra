package admin

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/newapi"
	oauthsvc "xlyra/server/internal/oauth"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestOAuthSiteStartRejectsMalformedOrMissingSitePayloads(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteAndOAuthServices()
	for _, tc := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
		body string
		code string
	}{
		{
			name: "codex invalid json",
			call: handler.StartCodexOAuth,
			body: `{"site":`,
			code: "invalid_json",
		},
		{
			name: "codex missing site",
			call: handler.StartCodexOAuth,
			body: `{}`,
			code: "invalid_oauth_site",
		},
		{
			name: "antigravity invalid json",
			call: handler.StartAntigravityOAuth,
			body: `{"site":`,
			code: "invalid_json",
		},
		{
			name: "antigravity missing site slug",
			call: handler.StartAntigravityOAuth,
			body: `{"site":{"name":"Team"}}`,
			code: "invalid_oauth_site",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, adminTestRequest(http.MethodPost, "/api/v1/oauth/providers/codex/authorize", tc.body))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestOAuthSiteCallbacksReturnOrRedirectProviderErrors(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteAndOAuthServices()
	for _, tc := range []struct {
		name         string
		call         func(http.ResponseWriter, *http.Request)
		target       string
		wantStatus   int
		wantLocation bool
	}{
		{
			name:       "codex error without redirect",
			call:       handler.CodexOAuthCallback,
			target:     "/api/v1/oauth/providers/codex/callback?state=s&error=access_denied",
			wantStatus: http.StatusBadRequest,
		},
		{
			// F19: an attacker-supplied redirect_url with an unresolvable state must
			// not trigger an open redirect; it falls through to a bad request.
			name:       "codex error ignores attacker redirect_url",
			call:       handler.CodexOAuthCallback,
			target:     "/api/v1/oauth/providers/codex/callback?state=s&error=access_denied&redirect_url=https%3A%2F%2Fevil.example%2Fdone",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "antigravity error without redirect",
			call:       handler.AntigravityOAuthCallback,
			target:     "/api/v1/oauth/providers/antigravity/callback?state=s&error=access_denied",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "antigravity error ignores attacker redirect_url",
			call:       handler.AntigravityOAuthCallback,
			target:     "/api/v1/oauth/providers/antigravity/callback?state=s&error=access_denied&redirect_url=https%3A%2F%2Fevil.example%2Fdone",
			wantStatus: http.StatusBadRequest,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, adminTestRequest(http.MethodGet, tc.target, ""))

			adminAssertStatus(t, rec, tc.wantStatus)
			if tc.wantLocation && !strings.Contains(rec.Header().Get("Location"), "status=error") {
				t.Fatalf("redirect location = %q, want status=error", rec.Header().Get("Location"))
			}
		})
	}
}

func TestOAuthSiteCompleteCallbackURLRejectsBadInputAndProviderErrors(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteAndOAuthServices()
	for _, tc := range []struct {
		name     string
		provider string
		body     string
		code     string
	}{
		{
			name:     "invalid json",
			provider: "codex",
			body:     `{"callback_url":`,
			code:     "invalid_json",
		},
		{
			name:     "callback error redirect becomes bad request",
			provider: "antigravity",
			body:     `{"callback_url":"https://xlyra.example/callback?state=s&error=access_denied&redirect_url=https%3A%2F%2Fclient.example%2Fdone","proxy_id":" direct "}`,
			code:     "oauth_callback_failed",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(http.MethodPost, "/api/v1/oauth/providers/"+tc.provider+"/callback/complete", tc.body, "provider", tc.provider)
			rec := adminPerform(handler.CompleteOAuthCallbackURL, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestOAuthConnectionHandlersValidateInputsBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteAndOAuthServices()
	for _, tc := range []struct {
		name   string
		method string
		target string
		connID string
		body   string
		call   func(http.ResponseWriter, *http.Request)
		code   string
	}{
		{
			name:   "get invalid id",
			method: http.MethodGet,
			target: "/api/v1/oauth/connections/bad-id",
			call:   handler.GetOAuthConnection,
			code:   "invalid_connection_id",
		},
		{
			name:   "refresh invalid id",
			method: http.MethodPost,
			target: "/api/v1/oauth/connections/bad-id/refresh",
			call:   handler.RefreshOAuthConnection,
			code:   "invalid_connection_id",
		},
		{
			name:   "export unavailable before id parse",
			method: http.MethodPost,
			target: "/api/v1/oauth/connections/bad-id/export",
			call:   handler.ExportOAuthConnection,
			code:   "oauth_export_unavailable",
		},
		{
			name:   "bulk model empty ids",
			method: http.MethodPatch,
			connID: uuid.NewString(),
			target: "/api/v1/oauth/connections/conn-id/models/status",
			body:   `{"enabled":true,"site_model_ids":[]}`,
			call:   handler.UpdateOAuthConnectionModelsStatus,
			code:   "invalid_site_model_ids",
		},
		{
			name:   "bulk model invalid json",
			method: http.MethodPut,
			connID: uuid.NewString(),
			target: "/api/v1/oauth/connections/conn-id/models/status",
			body:   `{"enabled":`,
			call:   handler.UpdateOAuthConnectionModelsStatus,
			code:   "invalid_json",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			connID := tc.connID
			if connID == "" {
				connID = oauthConnectionIDFromTarget(tc.target)
			}
			req := adminRequestWithRouteParam(tc.method, tc.target, tc.body, "connectionID", connID)
			rec := adminPerform(tc.call, req)

			status := http.StatusBadRequest
			if tc.code == "oauth_export_unavailable" {
				status = http.StatusServiceUnavailable
			}
			assertAdminErrorCode(t, rec, status, tc.code)
		})
	}
}

func TestImportOAuthAccountsRejectsEmptyJSONAndInvalidMultipart(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteAndOAuthServices()
	for _, tc := range []struct {
		name        string
		contentType string
		body        string
		code        string
	}{
		{
			name:        "empty json body",
			contentType: "application/json; charset=utf-8",
			body:        "",
			code:        "empty_body",
		},
		{
			name:        "invalid multipart form",
			contentType: "multipart/form-data; boundary=oauth-import-boundary",
			body:        "--not-the-boundary\r\n",
			code:        "invalid_form",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(http.MethodPost, "/api/v1/oauth/import?proxy_id=direct", tc.body)
			req.Header.Set("Content-Type", tc.contentType)
			rec := adminPerform(handler.ImportOAuthAccounts, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestImportedOAuthSyncSkipsUnavailableServicesAndUnsafeItems(t *testing.T) {
	t.Parallel()

	Handler{}.enqueueImportedOAuthSync(oauthsvc.ImportResult{Items: []oauthsvc.ImportAccountResult{
		{Status: "queued", SiteID: uuid.NewString()},
	}})

	adminHandlerWithSiteAndOAuthServices().enqueueImportedOAuthSync(oauthsvc.ImportResult{Items: []oauthsvc.ImportAccountResult{
		{Status: "failed", SiteID: uuid.NewString()},
		{Status: "queued"},
	}})

	adminHandlerWithSiteAndOAuthServices().refreshImportedOAuthSites(context.Background(), []oauthsvc.ImportAccountResult{
		{Status: "failed", SiteID: uuid.NewString()},
		{Status: "queued", SiteID: "not-a-uuid"},
	})
}

func TestOAuthSiteHandlersRejectInvalidIDsAndMissingServices(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		req    *http.Request
		call   func(Handler, http.ResponseWriter, *http.Request)
		status int
		code   string
	}{
		{
			name: "refresh site invalid id",
			req:  adminRequestWithRouteParam(http.MethodPost, "/api/v1/sites/bad-id/refresh", "", "siteID", "bad-id"),
			call: func(h Handler, w http.ResponseWriter, r *http.Request) { h.RefreshSite(w, r) },
			code: "invalid_site_id",
		},
		{
			name: "refresh all requires site service",
			req:  adminTestRequest(http.MethodPost, "/api/v1/sites/refresh", ""),
			call: func(h Handler, w http.ResponseWriter, r *http.Request) { h.RefreshAllSites(w, r) },
			code: "site_service_unavailable",
		},
		{
			name: "test site model requires gateway",
			req:  adminRequestWithRouteParam(http.MethodPost, "/api/v1/sites/bad-id/models/bad-model/test", `{`, "siteID", "bad-id"),
			call: func(h Handler, w http.ResponseWriter, r *http.Request) { h.TestSiteModel(w, r) },
			code: "gateway_unavailable",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(func(w http.ResponseWriter, r *http.Request) {
				tc.call(Handler{}, w, r)
			}, tc.req)

			status := tc.status
			if status == 0 {
				status = http.StatusBadRequest
			}
			if tc.code == "site_service_unavailable" || tc.code == "gateway_unavailable" {
				status = http.StatusServiceUnavailable
			}
			assertAdminErrorCode(t, rec, status, tc.code)
		})
	}
}

func TestOAuthSitePayloadHelpersReturnBasePayloadsWithoutServices(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/sites/site-id", "")
	item := store.Site{
		ID:              uuid.New(),
		Name:            "OpenAI Main",
		Slug:            "openai-main",
		SiteType:        "openai",
		BaseURL:         "https://api.openai.com/v1",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 7,
	}

	stats := (Handler{}).sitePayloadWithStats(req, item)
	if stats["id"] != item.ID.String() || stats["model_count"] != nil || stats["api_key_count"] != nil {
		t.Fatalf("unexpected stats payload without site service: %#v", stats)
	}

	edit := (Handler{}).sitePayloadWithEditConfig(req, item)
	if edit["id"] != item.ID.String() || edit["auth_config"] != nil {
		t.Fatalf("unexpected edit payload without site service: %#v", edit)
	}

	noPatchKey := sitepkg.APIKeyCredential{
		Credential: store.SiteCredential{ID: uuid.New()},
		Meta:       map[string]any{"upstream_id": float64(123), "name": "fresh"},
	}
	freshKey := newapi.UserAPIKey{ID: 123, Name: "fresh"}
	gotKey := (Handler{}).persistFreshAPIKeyMeta(req, item.ID, noPatchKey, freshKey)
	if gotKey.Credential.ID != noPatchKey.Credential.ID {
		t.Fatalf("persistFreshAPIKeyMeta without patch changed key: %#v", gotKey)
	}
}

func TestFreshNewAPIKeyByIDSkipsUnsupportedOrUnavailableLookups(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/sites/site-id/api-keys", "")
	handler := adminHandlerWithSiteService()
	newAPISite := store.Site{ID: uuid.New(), SiteType: "newapi", BaseURL: "https://newapi.example"}
	openAISite := store.Site{ID: uuid.New(), SiteType: "openai", BaseURL: "https://api.openai.com/v1"}

	if got := handler.freshNewAPIKeyByID(req, newAPISite, newAPISite.ID, 0); got.ID != 0 {
		t.Fatalf("upstream id <= 0 should return empty key, got %#v", got)
	}
	if got := handler.freshNewAPIKeyByID(req, openAISite, openAISite.ID, 123); got.ID != 0 {
		t.Fatalf("non-newapi site should return empty key, got %#v", got)
	}
	if got := handler.freshNewAPIKeyByID(req, newAPISite, newAPISite.ID, 123); got.ID != 0 {
		t.Fatalf("missing newapi service should return empty key, got %#v", got)
	}
}

func oauthConnectionIDFromTarget(target string) string {
	parts := strings.Split(strings.Trim(target, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if last == "models" || last == "status" || last == "refresh" || last == "export" {
		return parts[len(parts)-2]
	}
	return last
}
