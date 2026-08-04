package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestListSitesRequiresSiteService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.ListSites, adminTestRequest(http.MethodGet, "/api/v1/sites", ""))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "site_service_unavailable")
}

func TestListSitesRejectsInvalidDeletedFilter(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	rec := adminPerform(handler.ListSites, adminTestRequest(http.MethodGet, "/api/v1/sites?deleted=invalid", ""))

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_deleted_filter")
}

func TestCreateSiteRejectsInvalidJSONBeforeSiteMutation(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	rec := adminPerform(handler.CreateSite, adminTestRequest(http.MethodPost, "/api/v1/sites", `{"name":`))

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_json")
}

func TestSiteRouteHandlersRejectInvalidRouteIDBeforeSiteMutation(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "update", method: http.MethodPut, target: "/api/v1/sites/bad-id", body: `{}`, call: handler.UpdateSite},
		{name: "delete", method: http.MethodDelete, target: "/api/v1/sites/bad-id", call: handler.DeleteSite},
		{name: "enabled", method: http.MethodPatch, target: "/api/v1/sites/bad-id/enabled", body: `{"enabled":true}`, call: handler.UpdateSiteEnabled},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(tc.method, tc.target, tc.body, "siteID", "bad-id")
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_site_id")
		})
	}
}

func TestUpdateSiteEnabledRequiresEnabled(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	req := adminRequestWithRouteParam(http.MethodPatch, "/api/v1/sites/site-id/enabled", `{"name":"ignored"}`, "siteID", uuid.New().String())
	rec := adminPerform(handler.UpdateSiteEnabled, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_enabled")
}

func TestCreateSiteModelRejectsInvalidCanonicalModelID(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	req := adminRequestWithRouteParam(http.MethodPost, "/api/v1/sites/site-id/models", `{"canonical_model_id":"bad-id"}`, "siteID", uuid.New().String())
	rec := adminPerform(handler.CreateSiteModel, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_canonical_model_id")
}

func TestUpdateSiteModelsStatusRejectsEmptyModelIDs(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	req := adminRequestWithRouteParam(http.MethodPatch, "/api/v1/sites/site-id/models/status", `{"enabled":true,"model_ids":[" "]}`, "siteID", uuid.New().String())
	rec := adminPerform(handler.UpdateSiteModelsStatus, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_model_ids")
}

func TestGetSiteHealthHistoryRejectsInvalidLimit(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminRequestWithRouteParam(http.MethodGet, "/api/v1/sites/site-id/health/history?limit=0", "", "siteID", uuid.New().String())
	rec := adminPerform(handler.GetSiteHealthHistory, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_limit")
}

func TestGetSiteHealthHourlyRejectsInvalidHours(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminRequestWithRouteParam(http.MethodGet, "/api/v1/sites/site-id/health/hourly?hours=-1", "", "siteID", uuid.New().String())
	rec := adminPerform(handler.GetSiteHealthHourly, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_hours")
}

func TestTestSiteModelRequiresGateway(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.TestSiteModel, adminTestRequest(http.MethodPost, "/api/v1/sites/site-id/models/model-id/test", ""))

	assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "gateway_unavailable")
}

func TestListSiteTypesSmokeIncludesGatewayAndOAuthTypes(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminPerform(handler.ListSiteTypes, adminTestRequest(http.MethodGet, "/api/v1/site-types", ""))

	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[struct {
		Items []siteTypeInfo `json:"items"`
		Meta  map[string]any `json:"meta"`
	}](t, rec)
	if len(body.Items) == 0 || int(body.Meta["count"].(float64)) != len(body.Items) {
		t.Fatalf("unexpected site type response: %#v", body)
	}
	seen := map[string]siteTypeInfo{}
	for _, item := range body.Items {
		seen[item.SiteType] = item
	}
	if !seen["openai"].ShowInCreateDialog || seen["openai"].CredentialType != "api_key" {
		t.Fatalf("openai site type missing gateway setup fields: %#v", seen["openai"])
	}
	if seen["codex"].CredentialType != "oauth" || seen["codex"].ShowInCreateDialog {
		t.Fatalf("codex site type should be OAuth-only in create dialog: %#v", seen["codex"])
	}
	if seen["newapi"].CredentialType != "system_token" || !seen["newapi"].RequiresBaseURL {
		t.Fatalf("newapi site type missing system-token/base-url fields: %#v", seen["newapi"])
	}
}

func TestNormalizedRequestSiteType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		siteType string
		want     string
	}{
		{name: "empty defaults to openai", siteType: "", want: "openai"},
		{name: "whitespace defaults to openai", siteType: " \t\n ", want: "openai"},
		{name: "trims explicit type", siteType: " newapi ", want: "newapi"},
		{name: "preserves explicit type", siteType: "xlyra", want: "xlyra"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizedRequestSiteType(tt.siteType); got != tt.want {
				t.Fatalf("normalizedRequestSiteType(%q) = %q, want %q", tt.siteType, got, tt.want)
			}
		})
	}
}

func TestSiteTypeCredentialClassifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		siteType         string
		wantNewAPI       bool
		wantOpenAICompat bool
	}{
		{name: "newapi uses system token", siteType: " newapi ", wantNewAPI: true, wantOpenAICompat: false},
		{name: "openai uses api key", siteType: "openai", wantNewAPI: false, wantOpenAICompat: true},
		{name: "unknown defaults to api key", siteType: "custom_provider", wantNewAPI: false, wantOpenAICompat: true},
		{name: "codex oauth is not gateway api key", siteType: " codex ", wantNewAPI: false, wantOpenAICompat: false},
		{name: "antigravity oauth is not gateway api key", siteType: "antigravity", wantNewAPI: false, wantOpenAICompat: false},
		{name: "xlyra has dedicated credential flow", siteType: " xlyra ", wantNewAPI: false, wantOpenAICompat: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isNewAPISiteType(tt.siteType); got != tt.wantNewAPI {
				t.Fatalf("isNewAPISiteType(%q) = %v, want %v", tt.siteType, got, tt.wantNewAPI)
			}
			if got := isOpenAICompatibleSiteType(tt.siteType); got != tt.wantOpenAICompat {
				t.Fatalf("isOpenAICompatibleSiteType(%q) = %v, want %v", tt.siteType, got, tt.wantOpenAICompat)
			}
		})
	}
}

func TestResolveSiteCredentialsNewAPI(t *testing.T) {
	t.Parallel()

	credentials := resolveSiteCredentialsOK(t, siteUpsertRequest{
		SiteType: " newapi ",
		NewAPI:   &siteNewAPIRequest{AccessToken: "system-token", UserID: 42},
	}, true)
	assertSingleCredential(t, credentials, "newapi_access_token", "system-token", map[string]any{"user_id": 42})

	tests := []struct {
		name    string
		payload siteUpsertRequest
	}{
		{
			name:    "missing newapi config",
			payload: siteUpsertRequest{SiteType: "newapi"},
		},
		{
			name:    "blank access token",
			payload: siteUpsertRequest{SiteType: "newapi", NewAPI: &siteNewAPIRequest{AccessToken: " \t ", UserID: 42}},
		},
		{
			name:    "non-positive user id",
			payload: siteUpsertRequest{SiteType: "newapi", NewAPI: &siteNewAPIRequest{AccessToken: "system-token", UserID: 0}},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertResolveSiteCredentialsError(t, tt.payload, true, http.StatusBadRequest, "invalid_newapi_auth")
		})
	}
}

func TestResolveSiteCredentialsAPIKey(t *testing.T) {
	t.Parallel()

	credentials := resolveSiteCredentialsOK(t, siteUpsertRequest{
		SiteType: "openai",
		APIKey:   " sk-test ",
	}, true)
	assertSingleCredential(t, credentials, "api_key", "sk-test", nil)

	credentials = resolveSiteCredentialsOK(t, siteUpsertRequest{
		SiteType: "custom_provider",
		APIKey:   "custom-key",
	}, true)
	assertSingleCredential(t, credentials, "api_key", "custom-key", nil)

	priority := 4.5
	multiplier := 1.25
	credentials = resolveSiteCredentialsOK(t, siteUpsertRequest{
		SiteType: "openai",
		APIKeys: []siteAPIKeyRequest{
			{APIKey: " first ", Name: "Primary", RoutingPriority: &priority, UpstreamCostMultiplier: &multiplier},
			{APIKey: "second", Name: "Backup"},
		},
	}, true)
	if len(credentials) != 2 || credentials[0].Secret != "first" || credentials[1].Secret != "second" {
		t.Fatalf("multi api key credentials = %#v", credentials)
	}
	if credentials[0].DisplayName == nil || *credentials[0].DisplayName != "Primary" || credentials[0].RoutingPriority != &priority || credentials[0].UpstreamCostMultiplier != &multiplier {
		t.Fatalf("first api key config = %#v", credentials[0])
	}

	credentials = resolveSiteCredentialsOK(t, siteUpsertRequest{
		SiteType: "openai",
		APIKey:   " \t ",
	}, false)
	if len(credentials) != 0 {
		t.Fatalf("optional blank api key credentials = %#v, want none", credentials)
	}

	assertResolveSiteCredentialsError(t, siteUpsertRequest{
		SiteType: "openai",
		APIKey:   " \t ",
	}, true, http.StatusBadRequest, "invalid_api_key")
	assertResolveSiteCredentialsError(t, siteUpsertRequest{
		SiteType: "openai",
		APIKeys:  []siteAPIKeyRequest{{Name: "Missing"}},
	}, true, http.StatusBadRequest, "invalid_api_key")
}

func TestResolveSiteCredentialsXLyra(t *testing.T) {
	t.Parallel()

	credentials := resolveSiteCredentialsOK(t, siteUpsertRequest{
		SiteType: "xlyra",
		XLyra:    &siteXLyraRequest{AuthMode: " access_token ", AccessToken: " access-token "},
	}, true)
	assertSingleCredential(t, credentials, "xlyra_access_token", "access-token", map[string]any{"auth_mode": "access_token"})

	credentials = resolveSiteCredentialsOK(t, siteUpsertRequest{
		SiteType: "xlyra",
		XLyra:    &siteXLyraRequest{AuthMode: "api_key", APIKey: " xlyra-key "},
	}, true)
	assertSingleCredential(t, credentials, "api_key", "xlyra-key", map[string]any{"auth_mode": "api_key"})

	credentials = resolveSiteCredentialsOK(t, siteUpsertRequest{
		SiteType: "xlyra",
		APIKey:   " fallback-key ",
		XLyra:    &siteXLyraRequest{AuthMode: "api_key"},
	}, true)
	assertSingleCredential(t, credentials, "api_key", "fallback-key", map[string]any{"auth_mode": "api_key"})

	credentials = resolveSiteCredentialsOK(t, siteUpsertRequest{
		SiteType: "xlyra",
		XLyra:    &siteXLyraRequest{AuthMode: "api_key", APIKey: " \t "},
	}, false)
	if len(credentials) != 0 {
		t.Fatalf("optional blank xlyra api key credentials = %#v, want none", credentials)
	}

	tests := []struct {
		name    string
		payload siteUpsertRequest
	}{
		{
			name:    "missing xlyra config",
			payload: siteUpsertRequest{SiteType: "xlyra"},
		},
		{
			name:    "blank access token",
			payload: siteUpsertRequest{SiteType: "xlyra", XLyra: &siteXLyraRequest{AuthMode: "access_token", AccessToken: " \t "}},
		},
		{
			name:    "blank required api key",
			payload: siteUpsertRequest{SiteType: "xlyra", XLyra: &siteXLyraRequest{AuthMode: "api_key", APIKey: " \t "}},
		},
		{
			name:    "unsupported auth mode",
			payload: siteUpsertRequest{SiteType: "xlyra", XLyra: &siteXLyraRequest{AuthMode: "oauth"}},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertResolveSiteCredentialsError(t, tt.payload, true, http.StatusBadRequest, "invalid_xlyra_auth")
		})
	}
}

func TestResolveSiteCredentialsOAuthAndDefault(t *testing.T) {
	t.Parallel()

	for _, siteType := range []string{"codex", " antigravity "} {
		siteType := siteType
		t.Run(siteType, func(t *testing.T) {
			t.Parallel()

			credentials := resolveSiteCredentialsOK(t, siteUpsertRequest{SiteType: siteType}, true)
			if len(credentials) != 0 {
				t.Fatalf("oauth credentials for %q = %#v, want none", siteType, credentials)
			}
		})
	}

	credentials := resolveSiteCredentialsOK(t, siteUpsertRequest{
		APIKey: "default-key",
	}, true)
	assertSingleCredential(t, credentials, "api_key", "default-key", nil)
}

func TestSitePayloadIncludesGatewayProxyHeadersAndOAuthAccount(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	connectionID := uuid.New()
	now := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	payload := sitePayload(store.Site{
		ID:              siteID,
		Name:            "Codex Primary",
		Slug:            "codex-primary",
		SiteType:        "codex",
		BaseURL:         "https://chatgpt.example.com/backend-api",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 12.5,
		Meta: store.JSON(`{
			"proxy_id":" proxy-a ",
			"request_headers":[{"key":"X-Team","value":"core"}],
			"gateway":{"request_timeout_ms":30000,"responses_image_generation_policy":"disabled"},
			"oauth_provider":" codex ",
			"oauth_connection_id":"` + connectionID.String() + `",
			"oauth_account_id":" acct_123 ",
			"oauth_email":" user@example.com ",
			"oauth_plan_type":" plus "
		}`),
		CreatedAt: now,
		UpdatedAt: now,
	})

	if payload["id"] != siteID.String() || payload["icon_url"] != "/oauth-icons/codex.svg" {
		t.Fatalf("unexpected basic site payload: %#v", payload)
	}
	if payload["proxy_id"] != "proxy-a" {
		t.Fatalf("proxy id should be trimmed, got %#v", payload["proxy_id"])
	}
	if headers, ok := payload["request_headers"].([]any); !ok || len(headers) != 1 {
		t.Fatalf("request headers were not exposed: %#v", payload["request_headers"])
	}
	gatewayConfig, ok := payload["gateway_config"].(*sitepkg.GatewayConfig)
	if !ok || gatewayConfig == nil || gatewayConfig.RequestTimeoutMS == nil || *gatewayConfig.RequestTimeoutMS != 30000 {
		t.Fatalf("gateway config not decoded: %#v", payload["gateway_config"])
	}
	if gatewayConfig.ResponsesToolPolicy != sitepkg.ResponsesToolPolicyCompatibility || len(gatewayConfig.DisabledResponsesTools) != 1 {
		t.Fatalf("legacy responses image policy was not normalized: %#v", gatewayConfig)
	}
	account, ok := payload["oauth_account"].(map[string]any)
	if !ok {
		t.Fatalf("oauth account missing from payload: %#v", payload)
	}
	if account["provider"] != "codex" || account["connection_id"] != connectionID.String() || account["email"] != "user@example.com" || account["plan_type"] != "plus" {
		t.Fatalf("unexpected oauth account payload: %#v", account)
	}
}

func TestSiteAPIKeyPayloadFromStatePrefersStateAndAvailableModels(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	payload := (Handler{}).siteAPIKeyPayloadFromState(
		store.Site{ID: siteID, SiteType: "openai"},
		sitepkg.APIKeyCredential{
			Credential:    store.SiteCredential{ID: credentialID, SiteID: siteID},
			Secret:        "raw-secret",
			Name:          "Default Key",
			UpstreamID:    7,
			Enabled:       true,
			MaskedSecret:  "sk-...1234",
			SecretMissing: true,
			Meta: map[string]any{
				"status":          "stale",
				"group":           "meta-group",
				"remain_quota":    float64(20),
				"used_quota":      json.Number("5"),
				"disabled_models": []string{"gpt-disabled"},
			},
		},
		store.SiteAPIKeyState{
			SiteCredentialID: credentialID,
			SiteID:           siteID,
			UpstreamID:       sql.NullInt64{Int64: 42, Valid: true},
			Name:             "State Key",
			UpstreamStatus:   store.JSON(`{"label":"healthy"}`),
			Enabled:          false,
			GroupName:        sql.NullString{String: "state-group", Valid: true},
			Usage:            store.JSON(`{"source":"state"}`),
			SyncMessage:      sql.NullString{String: "synced with warning", Valid: true},
		},
		[]store.SiteAPIKeyModel{
			{UpstreamModelName: "gpt-active", Available: true, Enabled: true},
			{UpstreamModelName: "gpt-disabled", Available: true, Enabled: false},
			{UpstreamModelName: "gpt-missing", Available: false, Enabled: true},
		},
		"Apikey 1",
	)

	if payload["upstream_id"] != 42 || payload["name"] != "State Key" || payload["enabled"] != false {
		t.Fatalf("state fields did not override credential defaults: %#v", payload)
	}
	status, ok := payload["status"].(map[string]any)
	if !ok || status["label"] != "healthy" {
		t.Fatalf("state upstream status was not decoded: %#v", payload["status"])
	}
	if payload["group"] != "state-group" || payload["message"] != "synced with warning" {
		t.Fatalf("state group/message missing: %#v", payload)
	}
	if _, hasCopyKey := payload["copy_key"]; payload["secret_missing"] != true || payload["can_complete"] != true || hasCopyKey {
		t.Fatalf("secret completion fields unexpected: %#v", payload)
	}
	models := payload["models"].([]string)
	if len(models) != 2 || models[0] != "gpt-active" || models[1] != "gpt-disabled" {
		t.Fatalf("available model list = %#v", models)
	}
	modelItems := payload["model_items"].([]map[string]any)
	if len(modelItems) != 2 || modelItems[0]["enabled"] != true || modelItems[1]["enabled"] != false {
		t.Fatalf("model item enabled flags = %#v", modelItems)
	}
	usage := payload["usage"].(map[string]any)
	if usage["source"] != "state" {
		t.Fatalf("state usage should win over credential metadata: %#v", usage)
	}
}

func TestSiteStateHealthRefreshModelAndPricingPayloads(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	canonicalID := uuid.New()
	snapshotID := uuid.New()
	now := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	site := store.Site{ID: siteID, Name: "OpenAI", Slug: "openai", SiteType: "openai", Status: "active", Enabled: true, CreatedAt: now, UpdatedAt: now}
	state := store.SiteState{
		SiteID:            siteID,
		ValidationOK:      sql.NullBool{Bool: true, Valid: true},
		ValidationMessage: sql.NullString{String: "ok", Valid: true},
		SyncStatus:        "synced",
		SyncMessage:       sql.NullString{String: "fresh", Valid: true},
		LastSyncedAt:      sql.NullTime{Time: now, Valid: true},
		APIKeyCount:       2,
		ModelCount:        1,
		RawStatus:         store.JSON(`{"upstream":"ready"}`),
		UserSummary:       store.JSON(`{"balance":10}`),
		Pricing:           store.JSON(`{"groups":1}`),
		Checkin:           store.JSON(`{"ok":true}`),
		UpdatedAt:         now,
	}

	statePayload := siteStatePayload(state)
	if statePayload["status"] != "synced" || statePayload["validation_ok"] != true || statePayload["api_key_count"] != 2 {
		t.Fatalf("unexpected site state payload: %#v", statePayload)
	}
	if rawStatus := statePayload["raw_status"].(map[string]any); rawStatus["upstream"] != "ready" {
		t.Fatalf("raw status not decoded: %#v", statePayload["raw_status"])
	}

	healthState := store.SiteHealthState{
		SiteID:              siteID,
		Status:              "degraded",
		LastSnapshotID:      uuid.NullUUID{UUID: snapshotID, Valid: true},
		LastFailureAt:       sql.NullTime{Time: now, Valid: true},
		ConsecutiveFailures: 2,
		RecentSuccessRate:   sql.NullFloat64{Float64: 0.5, Valid: true},
		RecentAvgLatencyMS:  sql.NullInt64{Int64: 250, Valid: true},
		CheckedAt:           sql.NullTime{Time: now, Valid: true},
		Message:             sql.NullString{String: "slow", Valid: true},
		Metadata:            store.JSON(`{"reason":"latency"}`),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	snapshot := store.HealthSnapshot{
		ID:         snapshotID,
		SiteID:     siteID,
		Scope:      "site",
		Source:     "manual",
		Endpoint:   "/v1/models",
		Method:     http.MethodGet,
		Success:    false,
		StatusCode: sql.NullInt64{Int64: 503, Valid: true},
		LatencyMS:  sql.NullInt64{Int64: 300, Valid: true},
		ErrorType:  sql.NullString{String: "upstream_unavailable", Valid: true},
		CheckedAt:  now,
		Metadata:   store.JSON(`{"attempt":1}`),
	}
	healthPayload := siteHealthResultPayload(sitepkg.HealthResult{Site: site, State: healthState, Snapshot: snapshot, Recent: []store.HealthSnapshot{snapshot}})
	if healthPayload["ok"] != false || healthPayload["snapshot"].(map[string]any)["status_code"] != int64(503) {
		t.Fatalf("unexpected health result payload: %#v", healthPayload)
	}
	if healthPayload["meta"].(map[string]any)["recent_count"] != 1 {
		t.Fatalf("recent count missing: %#v", healthPayload["meta"])
	}

	model := store.SiteModel{
		ID:              siteModelID,
		SiteID:          siteID,
		CanonicalID:     uuid.NullUUID{UUID: canonicalID, Valid: true},
		UpstreamName:    "gpt-5",
		DisplayName:     "GPT-5",
		Capabilities:    store.JSON(`{"supports_reasoning":true}`),
		Status:          "active",
		MatchSource:     "alias",
		MatchConfidence: 90,
		MatchedAt:       sql.NullTime{Time: now, Valid: true},
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	modelPayload := siteModelPayload(model)
	if modelPayload["enabled"] != true || modelPayload["canonical_model_id"] != canonicalID.String() {
		t.Fatalf("unexpected site model payload: %#v", modelPayload)
	}
	if capabilities := modelPayload["capabilities"].(map[string]any); capabilities["supports_reasoning"] != true {
		t.Fatalf("capabilities not decoded: %#v", modelPayload["capabilities"])
	}

	refreshPayload := (Handler{}).refreshResultPayload(context.Background(), sitepkg.RefreshResult{
		Site:         site,
		State:        state,
		Models:       []store.SiteModel{model},
		APIKeyStates: []store.SiteAPIKeyState{{SiteCredentialID: uuid.New()}},
	})
	if refreshPayload["model_sync"].(map[string]any)["count"] != 1 || refreshPayload["api_key_sync"].(map[string]any)["count"] != 1 {
		t.Fatalf("unexpected refresh payload: %#v", refreshPayload)
	}

	groupPayloads := sitePricingGroupPayloads([]store.SitePricingGroup{{
		ID:           uuid.New(),
		SiteID:       siteID,
		GroupName:    "default",
		DisplayName:  sql.NullString{String: "Default", Valid: true},
		Ratio:        1.2,
		IsAuto:       true,
		Available:    true,
		Raw:          store.JSON(`{"source":"sync"}`),
		LastSyncedAt: sql.NullTime{Time: now, Valid: true},
		CreatedAt:    now,
		UpdatedAt:    now,
	}})
	if len(groupPayloads) != 1 || groupPayloads[0]["display_name"] != "Default" {
		t.Fatalf("unexpected pricing group payloads: %#v", groupPayloads)
	}

	pricingPayload := siteModelPricingPayload(store.SiteModelPricing{
		ID:                   uuid.New(),
		SiteID:               siteID,
		SiteModelID:          uuid.NullUUID{UUID: siteModelID, Valid: true},
		ModelName:            "gpt-5",
		GroupName:            "default",
		QuotaType:            1,
		BillingType:          "tokens",
		Currency:             "USD",
		GroupRatio:           1,
		InputValue:           sql.NullFloat64{Float64: 2, Valid: true},
		OutputValue:          sql.NullFloat64{Float64: 4, Valid: true},
		CacheRatio:           sql.NullFloat64{Float64: 0.5, Valid: true},
		CreateCacheRatio:     sql.NullFloat64{Float64: 0.25, Valid: true},
		ImageRatio:           sql.NullFloat64{Float64: 3, Valid: true},
		AudioRatio:           sql.NullFloat64{Float64: 4, Valid: true},
		AudioCompletionRatio: sql.NullFloat64{Float64: 0.5, Valid: true},
		Available:            true,
		Raw:                  store.JSON(`{"vendor":"manual"}`),
		CreatedAt:            now,
		UpdatedAt:            now,
	}, site)
	if pricingPayload["site_name"] != "OpenAI" || pricingPayload["cache_input_value"] != float64(1) || pricingPayload["audio_output_value"] != float64(4) {
		t.Fatalf("unexpected pricing payload: %#v", pricingPayload)
	}
	if pricingPayload["calculation"].(map[string]any)["input"] == nil {
		t.Fatalf("pricing calculation missing: %#v", pricingPayload["calculation"])
	}
}

func TestSiteAPIKeyModelAndQuotaHelpers(t *testing.T) {
	t.Parallel()

	modelNames := siteModelNames([]store.SiteModel{
		{DisplayName: "GPT-5", UpstreamName: "gpt-5"},
		{UpstreamName: "gpt-4.1"},
		{DisplayName: " \t\n "},
	})
	if len(modelNames) != 2 || modelNames[0] != "GPT-5" || modelNames[1] != "gpt-4.1" {
		t.Fatalf("site model names = %#v", modelNames)
	}

	ids := apiKeySummaryModelIDs(map[string]any{"data": []any{
		map[string]any{"id": "gpt-5"},
		map[string]any{"id": " "},
		"invalid",
	}})
	if len(ids) != 1 || ids[0] != "gpt-5" {
		t.Fatalf("summary model ids = %#v", ids)
	}

	items := apiKeyModelItems([]string{"gpt-5", "gpt-disabled", " "}, map[string]any{"disabled_models": []any{"gpt-disabled"}})
	if len(items) != 2 || items[0]["enabled"] != true || items[1]["enabled"] != false {
		t.Fatalf("api key model items = %#v", items)
	}

	if !usageHasQuotaData(map[string]any{"five_hour": map[string]any{}}) {
		t.Fatal("expected five_hour usage to count as quota data")
	}
	if !usageHasQuotaData(map[string]any{"data": map[string]any{"total_available": float64(10)}}) {
		t.Fatal("expected nested total_available to count as quota data")
	}
	if usageHasQuotaData(map[string]any{"data": map[string]any{"other": float64(10)}}) {
		t.Fatal("unexpected quota data from unrelated usage fields")
	}

	usage := usageFromCredentialMeta(map[string]any{
		"name":            "key-a",
		"remain_quota":    json.Number("20"),
		"used_quota":      int64(5),
		"unlimited_quota": false,
	}, nil)
	data := usage["data"].(map[string]any)
	if usage["success"] != true || data["total_granted"] != 25 || data["total_available"] != 20 {
		t.Fatalf("usage from credential meta = %#v", usage)
	}
}

func resolveSiteCredentialsOK(t *testing.T, payload siteUpsertRequest, required bool) []sitepkg.CredentialInput {
	t.Helper()

	req := adminTestRequest(http.MethodPost, "/api/v1/sites", "")
	rec := adminRecorder()
	credentials, ok := (Handler{}).resolveSiteCredentials(rec, req, payload, required)
	adminRequireParserOK(t, rec, ok, "resolve site credentials")
	if rec.Body.Len() != 0 {
		t.Fatalf("resolveSiteCredentials wrote unexpected response: %s", rec.Body.String())
	}
	return credentials
}

func assertResolveSiteCredentialsError(t *testing.T, payload siteUpsertRequest, required bool, status int, code string) {
	t.Helper()

	req := adminTestRequest(http.MethodPost, "/api/v1/sites", "")
	rec := adminRecorder()
	credentials, ok := (Handler{}).resolveSiteCredentials(rec, req, payload, required)
	if ok {
		t.Fatalf("resolveSiteCredentials returned ok with credentials %#v, want error", credentials)
	}
	if credentials != nil {
		t.Fatalf("resolveSiteCredentials credentials = %#v, want nil on error", credentials)
	}
	assertAdminErrorCode(t, rec, status, code)
}

func assertSingleCredential(t *testing.T, credentials []sitepkg.CredentialInput, credentialType string, secret string, meta map[string]any) {
	t.Helper()

	if len(credentials) != 1 {
		t.Fatalf("credentials = %#v, want exactly one", credentials)
	}
	credential := credentials[0]
	if credential.Type != credentialType || credential.Secret != secret {
		t.Fatalf("credential = %#v, want type %q secret %q", credential, credentialType, secret)
	}
	if !reflect.DeepEqual(credential.Meta, meta) {
		t.Fatalf("credential meta = %#v, want %#v", credential.Meta, meta)
	}
}
