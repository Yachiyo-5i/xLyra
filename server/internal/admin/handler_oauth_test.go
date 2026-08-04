package admin

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/store"
)

func TestCodexSiteNameFromEmail(t *testing.T) {
	t.Parallel()

	got, err := oauthsvc.CodexSiteNameFromEmail("abcde+zxcvb@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^Codex_abcd_[a-z]{4}$`).MatchString(got) {
		t.Fatalf("expected Codex_abcd_[a-z]{4}, got %q", got)
	}
}

func TestCodexSiteNameFromEmailUsesRandomSuffix(t *testing.T) {
	t.Parallel()

	got, err := oauthsvc.CodexSiteNameFromEmail("abcdefg@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^Codex_abcd_[a-z]{4}$`).MatchString(got) {
		t.Fatalf("expected Codex_abcd_[a-z]{4}, got %q", got)
	}
}

func TestCodexSiteNameFromEmailFallsBackToProviderName(t *testing.T) {
	t.Parallel()

	got, err := oauthsvc.CodexSiteNameFromEmail("not-an-email")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Codex" {
		t.Fatalf("expected Codex, got %q", got)
	}
}

func TestShouldAutoRenameCodexSite(t *testing.T) {
	t.Parallel()

	if !shouldAutoRenameCodexSite("Codex", "Codex_abcd_xcvb") {
		t.Fatal("expected default Codex name to be auto-renamed")
	}
	if shouldAutoRenameCodexSite("My Codex Prod", "Codex_abcd_xcvb") {
		t.Fatal("expected custom site name to be preserved")
	}
}

func TestOAuthSiteModelPayloadIncludesDisplayName(t *testing.T) {
	t.Parallel()

	model := store.SiteModel{
		ID:           uuid.New(),
		UpstreamName: "gpt-5.5",
		DisplayName:  "GPT 5.5",
		Status:       "active",
	}

	payload := oauthSiteModelPayload(model)
	if payload["display_name"] != "GPT 5.5" {
		t.Fatalf("display_name = %#v, want GPT 5.5", payload["display_name"])
	}
	if payload["display"] != "GPT 5.5" {
		t.Fatalf("display = %#v, want GPT 5.5", payload["display"])
	}
	if payload["name"] != "gpt-5.5" {
		t.Fatalf("name should remain stable model id, got %#v", payload["name"])
	}
}

func TestOAuthAuthorizeRequiresServices(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.StartCodexOAuth, adminTestRequest(http.MethodPost, "/api/v1/oauth/providers/codex/authorize", `{}`))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "oauth_service_unavailable")
}

func TestListOAuthConnectionsRequiresService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.ListOAuthConnections, adminTestRequest(http.MethodGet, "/api/v1/oauth/connections", ""))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "oauth_service_unavailable")
}

func TestOAuthCallbacksRequireState(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteAndOAuthServices()
	for _, tc := range []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "codex", call: handler.CodexOAuthCallback},
		{name: "antigravity", call: handler.AntigravityOAuthCallback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, adminTestRequest(http.MethodGet, "/api/v1/oauth/providers/"+tc.name+"/callback?code=abc", ""))

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_oauth_state")
		})
	}
}

func TestCompleteOAuthCallbackURLRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteAndOAuthServices()
	for _, tc := range []struct {
		name     string
		provider string
		body     string
		code     string
	}{
		{
			name:     "unsupported provider",
			provider: "unknown",
			body:     `{"callback_url":"https://xlyra.example.com/callback?state=s&code=c"}`,
			code:     "unsupported_oauth_provider",
		},
		{
			name:     "invalid callback url",
			provider: "codex",
			body:     `{"callback_url":"%"}`,
			code:     "invalid_callback_url",
		},
		{
			name:     "missing state",
			provider: "codex",
			body:     `{"callback_url":"https://xlyra.example.com/callback?code=c"}`,
			code:     "invalid_oauth_state",
		},
		{
			name:     "missing code and error",
			provider: "codex",
			body:     `{"callback_url":"https://xlyra.example.com/callback?state=s"}`,
			code:     "invalid_callback_url",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(http.MethodPost, "/api/v1/oauth/providers/"+tc.provider+"/callback-url", tc.body, "provider", tc.provider)
			rec := adminPerform(handler.CompleteOAuthCallbackURL, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, tc.code)
		})
	}
}

func TestOAuthModelUpdatesValidateRequestBeforeLookup(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteAndOAuthServices()
	connectionID := uuid.New().String()

	req := adminRequestWithRouteParam(http.MethodPut, "/api/v1/oauth/connections/"+connectionID+"/models", `{"model":"gpt-5"}`, "connectionID", connectionID)
	rec := adminPerform(handler.UpdateOAuthConnectionModel, req)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_enabled")

	req = adminRequestWithRouteParam(http.MethodPut, "/api/v1/oauth/connections/"+connectionID+"/models/status", `{"enabled":true,"site_model_ids":[" "]}`, "connectionID", connectionID)
	rec = adminPerform(handler.UpdateOAuthConnectionModelsStatus, req)
	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_site_model_ids")
}

func TestOAuthPendingSitePayloadValidatesNewAndExistingSites(t *testing.T) {
	t.Parallel()

	if _, err := oauthPendingSitePayload(oauthAuthorizeRequest{}); err == nil {
		t.Fatal("expected missing site payload to fail")
	}
	if _, err := oauthPendingSitePayload(oauthAuthorizeRequest{Site: &oauthAuthorizeSite{Name: "Codex"}}); err == nil {
		t.Fatal("expected new oauth site without slug to fail")
	}

	siteID := uuid.New()
	payload, err := oauthPendingSitePayload(oauthAuthorizeRequest{Site: &oauthAuthorizeSite{
		SiteID: siteID.String(),
	}})
	if err != nil {
		t.Fatalf("existing site payload should be accepted: %v", err)
	}
	if payload.SiteID != siteID || !payload.Enabled {
		t.Fatalf("unexpected existing site payload: %#v", payload)
	}

	enabled := false
	payload, err = oauthPendingSitePayload(oauthAuthorizeRequest{Site: &oauthAuthorizeSite{
		Name:    " Codex Team ",
		Slug:    " codex-team ",
		Enabled: &enabled,
	}})
	if err != nil {
		t.Fatalf("new site payload should be accepted: %v", err)
	}
	if payload.Name != "Codex Team" || payload.Slug != "codex-team" || payload.Enabled {
		t.Fatalf("unexpected new site payload: %#v", payload)
	}
}

func TestOAuthCallbackAndExportHelpers(t *testing.T) {
	t.Parallel()

	if got := appendQuery("https://xlyra.example.com/callback?existing=1", map[string]string{"status": "success", "empty": " "}); got != "https://xlyra.example.com/callback?existing=1&status=success" {
		t.Fatalf("appendQuery = %q", got)
	}
	if got := appendQuery("://bad", map[string]string{"status": "success"}); got != "" {
		t.Fatalf("invalid appendQuery target = %q, want empty", got)
	}
	if got := successOrPartial(nil); got != "success" {
		t.Fatalf("successOrPartial(nil) = %q, want success", got)
	}
	if got := successOrPartial(assertionError("refresh failed")); got != "partial" {
		t.Fatalf("successOrPartial(error) = %q, want partial", got)
	}

	redirect := oauthRedirectTarget(store.OAuthSession{
		SuccessRedirectURL: "https://xlyra.example.com/success",
	}, map[string]string{"connection_id": "conn-1"})
	if result := oauthCallbackURLResult(redirect); result["connection_id"] != "conn-1" || result["status"] != "success" {
		t.Fatalf("unexpected callback URL result: %#v from %s", result, redirect)
	}

	connectionID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	if got := codexOAuthExportFilename(store.OAuthConnection{ID: connectionID, Email: "alice@example.com"}); got != "codex-alice.json" {
		t.Fatalf("email export filename = %q, want codex-alice.json", got)
	}
	if got := codexOAuthExportFilename(store.OAuthConnection{ID: connectionID, AccountID: "account-abcdef"}); got != "codex-account-.json" {
		t.Fatalf("account export filename = %q, want codex-account-.json", got)
	}
}

func TestOAuthRequestBaseURLAndErrorHelpers(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodGet, "/api/v1/oauth/providers/codex/callback", "")
	req.Host = "internal.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "xlyra.example.com")

	if got := requestPublicBaseURL(req); got != "https://xlyra.example.com" {
		t.Fatalf("requestPublicBaseURL = %q, want forwarded public base URL", got)
	}
	if got := errorMessage(nil); got != "" {
		t.Fatalf("nil error message = %q, want empty", got)
	}
	if got := errorMessage(assertionError("refresh failed")); got != "refresh failed" {
		t.Fatalf("error message = %q, want refresh failed", got)
	}
	if !strings.Contains(codexSuccessPage(), "Codex OAuth complete") || !strings.Contains(antigravitySuccessPage(), "Antigravity OAuth complete") {
		t.Fatal("success pages should include provider-specific completion text")
	}
}

func TestOAuthConnectionPayloadSurfacesMetadataAndQuotaAuthFailure(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	now := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	payload := (Handler{}).oauthConnectionPayload(adminTestRequest(http.MethodGet, "/api/v1/oauth/connections", ""), store.OAuthConnection{
		ID:            uuid.New(),
		Provider:      "codex",
		SiteID:        &siteID,
		Status:        "connected",
		AccountID:     "acct_123",
		Email:         "user@example.com",
		ExpiresAt:     sql.NullTime{Time: now.Add(time.Hour), Valid: true},
		LastRefreshAt: sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
		LastSyncAt:    sql.NullTime{Time: now, Valid: true},
		Metadata: store.JSON(`{
			"quota":{"available":false,"error":"invalid_grant"},
			"refreshable":false,
			"token_mode":"access_token_only",
			"refresh_warning":"refresh token missing",
			"last_error_at":"2026-06-22T10:00:00Z"
		}`),
		CreatedAt: now.Add(-2 * time.Hour),
		UpdatedAt: now,
	})

	if payload["status"] != "reconnect_required" {
		t.Fatalf("expected quota auth failure to require reconnect, got %#v", payload["status"])
	}
	if payload["last_error"] != "invalid_grant" || payload["last_error_at"] != "2026-06-22T10:00:00Z" {
		t.Fatalf("expected quota auth error metadata to surface, got %#v", payload)
	}
	if payload["site_id"] != siteID.String() || payload["refreshable"] != false || payload["token_mode"] != "access_token_only" || payload["refresh_warning"] != "refresh token missing" {
		t.Fatalf("unexpected oauth connection metadata payload: %#v", payload)
	}
	if meta, ok := payload["meta"].(map[string]any); !ok || meta["quota"] == nil {
		t.Fatalf("expected raw metadata to be included, got %#v", payload["meta"])
	}
}

func TestOAuthConnectionPayloadPreservesExplicitLastError(t *testing.T) {
	t.Parallel()

	payload := (Handler{}).oauthConnectionPayload(adminTestRequest(http.MethodGet, "/api/v1/oauth/connections", ""), store.OAuthConnection{
		ID:       uuid.New(),
		Provider: "codex",
		Status:   "connected",
		Metadata: store.JSON(`{
			"last_error":"operator noted reconnect required",
			"quota":{"available":false,"message":"invalid_grant"}
		}`),
	})

	if payload["status"] != "reconnect_required" {
		t.Fatalf("quota auth failure should still update status, got %#v", payload["status"])
	}
	if payload["last_error"] != "operator noted reconnect required" {
		t.Fatalf("explicit last_error should be preserved, got %#v", payload["last_error"])
	}
	if got := oauthConnectionMetadataAuthError("codex", store.JSON(`{"quota":{"available":false,"detail":"invalid_grant"}}`)); got != "invalid_grant" {
		t.Fatalf("oauthConnectionMetadataAuthError = %q, want invalid_grant", got)
	}
	if got := oauthQuotaAuthError("codex", map[string]any{"quota": map[string]any{"available": true, "error": "invalid_grant"}}); got != "" {
		t.Fatalf("available quota should not produce auth error, got %q", got)
	}
}

func TestApplyOAuthCallbackProxyOverrideAndSiteMeta(t *testing.T) {
	t.Parallel()

	pending := oauthsvc.PendingSite{}
	req := adminTestRequest(http.MethodGet, "/api/v1/oauth/providers/codex/callback", "")
	applyOAuthCallbackProxyOverride(req, &pending)
	if pending.ProxyID != nil {
		t.Fatalf("proxy id without context = %#v, want nil", *pending.ProxyID)
	}

	req = req.WithContext(context.WithValue(req.Context(), oauthCallbackProxyIDContextKey{}, " proxy-main "))
	applyOAuthCallbackProxyOverride(req, &pending)
	if pending.ProxyID == nil || *pending.ProxyID != "proxy-main" {
		t.Fatalf("trimmed proxy id = %#v, want proxy-main", pending.ProxyID)
	}

	applyOAuthCallbackProxyOverride(req, nil)
	req = req.WithContext(context.WithValue(req.Context(), oauthCallbackProxyIDContextKey{}, " \t "))
	applyOAuthCallbackProxyOverride(req, &pending)
	if pending.ProxyID == nil || *pending.ProxyID != "" {
		t.Fatalf("blank proxy id should be applied as empty string, got %#v", pending.ProxyID)
	}

	meta := oauthSiteMeta(store.OAuthConnection{
		AccountID: "acct_123",
		Email:     "alice@example.com",
		Metadata: store.JSON(`{
			"plan_type":" plus ",
			"subscription_tier":" team ",
			"project_id":" proj_123 "
		}`),
	})
	if meta["oauth_account_id"] != "acct_123" || meta["oauth_email"] != "alice@example.com" {
		t.Fatalf("account/email meta missing: %#v", meta)
	}
	if meta["oauth_plan_type"] != "team" || meta["oauth_subscription_tier"] != "team" || meta["oauth_project_id"] != "proj_123" {
		t.Fatalf("oauth metadata should be trimmed with subscription tier overriding plan type, got %#v", meta)
	}
}

func TestCodexOAuthExportLastRefresh(t *testing.T) {
	t.Parallel()

	lastRefresh := time.Date(2026, 6, 22, 8, 9, 10, 123456789, time.FixedZone("CST", 8*60*60))
	got := codexOAuthExportLastRefresh(oauthsvc.CodexConnection{
		Connection: store.OAuthConnection{
			LastRefreshAt: sql.NullTime{Time: lastRefresh, Valid: true},
		},
		Metadata: map[string]any{"last_refresh": "metadata-refresh"},
	})
	if got != "2026-06-22T00:09:10.123456789Z" {
		t.Fatalf("last refresh sql timestamp = %q, want UTC RFC3339Nano", got)
	}

	got = codexOAuthExportLastRefresh(oauthsvc.CodexConnection{
		Metadata: map[string]any{"last_refresh": " metadata-refresh "},
	})
	if got != "metadata-refresh" {
		t.Fatalf("last refresh metadata fallback = %q, want metadata-refresh", got)
	}

	got = codexOAuthExportLastRefresh(oauthsvc.CodexConnection{
		Metadata: map[string]any{"last_refresh": 123},
	})
	if got != "" {
		t.Fatalf("non-string last refresh metadata = %q, want empty", got)
	}
}

func TestOAuthConnectionSiteModelIDUsesExplicitUUID(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	got, err := (Handler{}).oauthConnectionSiteModelID(context.Background(), uuid.New(), " "+modelID.String()+" ", "ignored-model")
	if err != nil {
		t.Fatalf("explicit site model id should be accepted: %v", err)
	}
	if got != modelID {
		t.Fatalf("site model id = %s, want %s", got, modelID)
	}
}

func TestOAuthConnectionSiteModelIDRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := (Handler{}).oauthConnectionSiteModelID(context.Background(), uuid.New(), "not-a-uuid", "gpt-5"); err == nil || !strings.Contains(err.Error(), "valid UUID") {
		t.Fatalf("invalid site_model_id error = %v, want valid UUID error", err)
	}
	if _, err := (Handler{}).oauthConnectionSiteModelID(context.Background(), uuid.New(), " \t ", " \n "); err == nil || !strings.Contains(err.Error(), "site_model_id or model is required") {
		t.Fatalf("missing model error = %v, want required error", err)
	}
}

func TestOAuthConnectionDetailsPayloadFallsBackToSnapshotModels(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	details := oauthsvc.CodexConnection{
		Connection: store.OAuthConnection{
			ID:        connectionID,
			Provider:  "codex",
			Status:    "connected",
			AccountID: "acct_123",
			Email:     "user@example.com",
		},
		PlanType: "plus",
		Quota:    map[string]any{"available": true},
		Claims:   map[string]any{"sub": "user_123"},
		Models: []map[string]any{
			{"id": "gpt-5-codex", "display_name": "GPT-5 Codex", "status": "active"},
			{"id": "gpt-disabled", "status": "disabled"},
			{"id": "gpt-explicit", "enabled": false},
		},
	}

	payload := (Handler{}).oauthConnectionDetailsPayload(adminTestRequest(http.MethodGet, "/api/v1/oauth/connections/"+connectionID.String(), ""), details, nil)

	if payload["id"] != connectionID.String() || payload["plan_type"] != "plus" {
		t.Fatalf("unexpected connection identity payload: %#v", payload)
	}
	if payload["quota"].(map[string]any)["available"] != true || payload["claims"].(map[string]any)["sub"] != "user_123" {
		t.Fatalf("quota/claims missing from payload: %#v", payload)
	}
	models := payload["models"].([]map[string]any)
	if len(models) != 3 {
		t.Fatalf("expected three snapshot models, got %#v", models)
	}
	if models[0]["enabled"] != true || models[1]["enabled"] != false || models[2]["enabled"] != false {
		t.Fatalf("unexpected model enabled defaults: %#v", models)
	}
	if models[0]["id"] != "gpt-5-codex" || models[0]["display_name"] != "GPT-5 Codex" {
		t.Fatalf("unexpected first model payload: %#v", models[0])
	}
}

func TestOAuthSmallValueHelpers(t *testing.T) {
	t.Parallel()

	if got := firstNonEmptyOAuthString(" ", "\t", " connected "); got != "connected" {
		t.Fatalf("firstNonEmptyOAuthString = %q, want connected", got)
	}
	if got := oauthStringValue(" value "); got != "value" {
		t.Fatalf("oauthStringValue = %q, want value", got)
	}
	if got := oauthStringValue(123); got != "" {
		t.Fatalf("non-string oauthStringValue = %q, want empty", got)
	}
}

type assertionError string

func (e assertionError) Error() string {
	return string(e)
}
