package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestAPIKeyHandlersRequireAuthService(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	apiKeyID := uuid.New().String()
	for _, tc := range []struct {
		name string
		req  *http.Request
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "create", req: adminTestRequest(http.MethodPost, "/api/v1/api-keys", ""), call: handler.CreateAPIKey},
		{name: "list", req: adminTestRequest(http.MethodGet, "/api/v1/api-keys", ""), call: handler.ListAPIKeys},
		{name: "get", req: adminRequestWithRouteParam(http.MethodGet, "/api/v1/api-keys/"+apiKeyID, "", "apiKeyID", apiKeyID), call: handler.GetAPIKey},
		{name: "update", req: adminRequestWithRouteParam(http.MethodPatch, "/api/v1/api-keys/"+apiKeyID, `{}`, "apiKeyID", apiKeyID), call: handler.UpdateAPIKey},
		{name: "increase quota", req: adminRequestWithRouteParam(http.MethodPost, "/api/v1/api-keys/"+apiKeyID+"/quota/increase", `{}`, "apiKeyID", apiKeyID), call: handler.IncreaseAPIKeyQuota},
		{name: "reset quota", req: adminRequestWithRouteParam(http.MethodPost, "/api/v1/api-keys/"+apiKeyID+"/quota/reset", `{}`, "apiKeyID", apiKeyID), call: handler.ResetAPIKeyQuota},
		{name: "delete", req: adminRequestWithRouteParam(http.MethodDelete, "/api/v1/api-keys/"+apiKeyID, "", "apiKeyID", apiKeyID), call: handler.DeleteAPIKey},
		{name: "site_models", req: adminRequestWithRouteParam(http.MethodGet, "/api/v1/api-keys/"+apiKeyID+"/site-models", "", "apiKeyID", apiKeyID), call: handler.ListAPIKeySiteModels},
		{name: "sites", req: adminRequestWithRouteParam(http.MethodGet, "/api/v1/api-keys/"+apiKeyID+"/sites", "", "apiKeyID", apiKeyID), call: handler.ListAPIKeySites},
		{name: "site_groups", req: adminRequestWithRouteParam(http.MethodGet, "/api/v1/api-keys/"+apiKeyID+"/site-groups", "", "apiKeyID", apiKeyID), call: handler.ListAPIKeySiteGroups},
		{name: "update_site_models", req: adminRequestWithRouteParam(http.MethodPut, "/api/v1/api-keys/"+apiKeyID+"/site-models", `{}`, "apiKeyID", apiKeyID), call: handler.UpdateAPIKeySiteModels},
		{name: "update_site_groups", req: adminRequestWithRouteParam(http.MethodPut, "/api/v1/api-keys/"+apiKeyID+"/site-groups", `{}`, "apiKeyID", apiKeyID), call: handler.UpdateAPIKeySiteGroups},
		{name: "update_sites", req: adminRequestWithRouteParam(http.MethodPut, "/api/v1/api-keys/"+apiKeyID+"/sites", `{}`, "apiKeyID", apiKeyID), call: handler.UpdateAPIKeySites},
		{name: "check_model", req: adminRequestWithRouteParam(http.MethodPost, "/api/v1/api-keys/"+apiKeyID+"/check-model", `{}`, "apiKeyID", apiKeyID), call: handler.CheckAPIKeyModel},
		{name: "update_model_mappings", req: adminRequestWithRouteParam(http.MethodPut, "/api/v1/api-keys/"+apiKeyID+"/model-mappings", `{}`, "apiKeyID", apiKeyID), call: handler.UpdateAPIKeyModelMappings},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminPerform(tc.call, tc.req)

			assertAdminErrorCode(t, rec, http.StatusServiceUnavailable, "auth_unavailable")
		})
	}
}

func TestCreateAPIKeyWithAuthServiceRequiresAdminSession(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithAuthService()
	rec := adminPerform(handler.CreateAPIKey, adminTestRequest(http.MethodPost, "/api/v1/api-keys", ""))

	assertAdminErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

func TestCreateAPIKeyRejectsInvalidBodyAndBlankNameBeforeRepository(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithAuthService()
	adminID := uuid.New()

	for _, tc := range []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{name: "invalid json", body: `{"name":`, status: http.StatusBadRequest, code: "invalid_json"},
		{name: "blank name", body: `{"name":" \t\n "}`, status: http.StatusBadRequest, code: "invalid_api_key_name"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", tc.body)
			req = req.WithContext(auth.WithAdminID(req.Context(), adminID))
			rec := adminPerform(handler.CreateAPIKey, req)

			assertAdminErrorCode(t, rec, tc.status, tc.code)
		})
	}
}

func TestAPIKeyRouteHandlersRejectInvalidRouteIDBeforeRepository(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithAuthService()
	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
		call   func(http.ResponseWriter, *http.Request)
	}{
		{name: "get", method: http.MethodGet, target: "/api/v1/api-keys/bad-id", call: handler.GetAPIKey},
		{name: "update", method: http.MethodPatch, target: "/api/v1/api-keys/bad-id", body: `{}`, call: handler.UpdateAPIKey},
		{name: "increase quota", method: http.MethodPost, target: "/api/v1/api-keys/bad-id/quota/increase", body: `{}`, call: handler.IncreaseAPIKeyQuota},
		{name: "reset quota", method: http.MethodPost, target: "/api/v1/api-keys/bad-id/quota/reset", body: `{}`, call: handler.ResetAPIKeyQuota},
		{name: "delete", method: http.MethodDelete, target: "/api/v1/api-keys/bad-id", call: handler.DeleteAPIKey},
		{name: "site models", method: http.MethodGet, target: "/api/v1/api-keys/bad-id/site-models", call: handler.ListAPIKeySiteModels},
		{name: "sites", method: http.MethodGet, target: "/api/v1/api-keys/bad-id/sites", call: handler.ListAPIKeySites},
		{name: "site groups", method: http.MethodGet, target: "/api/v1/api-keys/bad-id/site-groups", call: handler.ListAPIKeySiteGroups},
		{name: "update site models", method: http.MethodPut, target: "/api/v1/api-keys/bad-id/site-models", body: `{}`, call: handler.UpdateAPIKeySiteModels},
		{name: "update site groups", method: http.MethodPut, target: "/api/v1/api-keys/bad-id/site-groups", body: `{}`, call: handler.UpdateAPIKeySiteGroups},
		{name: "update sites", method: http.MethodPut, target: "/api/v1/api-keys/bad-id/sites", body: `{}`, call: handler.UpdateAPIKeySites},
		{name: "check model", method: http.MethodPost, target: "/api/v1/api-keys/bad-id/check-model", body: `{}`, call: handler.CheckAPIKeyModel},
		{name: "update model mappings", method: http.MethodPut, target: "/api/v1/api-keys/bad-id/model-mappings", body: `{}`, call: handler.UpdateAPIKeyModelMappings},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := adminRequestWithRouteParam(tc.method, tc.target, tc.body, "apiKeyID", "bad-id")
			rec := adminPerform(tc.call, req)

			assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_api_key_id")
		})
	}
}

func TestResetAPIKeyQuotaRequestDoesNotRequireReason(t *testing.T) {
	t.Parallel()

	var payload resetAPIKeyQuotaRequest
	if err := json.Unmarshal([]byte(`{"scopes":["daily"]}`), &payload); err != nil {
		t.Fatalf("decode reset quota request: %v", err)
	}
	if len(payload.Scopes) != 1 || payload.Scopes[0] != "daily" {
		t.Fatalf("reset quota scopes = %#v, want daily", payload.Scopes)
	}
}

func TestAPIKeyPayloadIncludesQuotaRateLimitAndAccessDetails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(24 * time.Hour)
	lastUsedAt := now.Add(-time.Hour)
	apiKeyID := uuid.New()
	siteID := uuid.New()
	siteModelID := uuid.New()
	groupID := uuid.New()
	payload := (Handler{}).apiKeyPayload(context.Background(), store.APIKey{
		ID:             apiKeyID,
		Name:           "gateway key",
		KeyPrefix:      "xly",
		MaskedKey:      "xly_****",
		KeyKind:        "generated",
		Scope:          "gateway",
		Status:         "active",
		ModelPolicy:    "allow_list",
		SitePolicy:     "allow_list",
		ModelMappings:  store.JSON(`{"gpt-5":"upstream-gpt-5"}`),
		QuotaLimit:     sql.NullFloat64{Float64: 100, Valid: true},
		QuotaUsed:      125.5,
		QuotaTotalUsed: 25.5,
		QuotaUnlimited: false,
		ExpiresAt:      &expiresAt,
		LastUsedAt:     &lastUsedAt,
		CreatedAt:      now.Add(-2 * time.Hour),
		UpdatedAt:      now,
	}, []store.APIKeySiteModelPermissionDetail{{
		ID:                uuid.New(),
		APIKeyID:          apiKeyID,
		SiteModelID:       siteModelID,
		SiteID:            siteID,
		SiteName:          "Codex",
		SiteSlug:          "codex",
		SiteType:          "codex",
		UpstreamModelName: "gpt-5",
		DisplayName:       "GPT-5",
		Enabled:           true,
	}}, []store.APIKeySitePermission{{
		ID:       uuid.New(),
		APIKeyID: apiKeyID,
		SiteID:   siteID,
		Enabled:  true,
	}}, []store.APIKeySiteGroupPermission{{
		ID:       uuid.New(),
		APIKeyID: apiKeyID,
		GroupID:  groupID,
		Enabled:  true,
	}}, false)

	if payload["id"] != apiKeyID.String() || payload["name"] != "gateway key" || payload["key"] != nil {
		t.Fatalf("unexpected api key identity payload: %#v", payload)
	}
	if payload["quota_limit"] != 100.0 || payload["quota_available"] != 74.5 || payload["quota_unlimited"] != false {
		t.Fatalf("unexpected quota payload: %#v", payload)
	}
	if payload["quota_used"] != 125.5 || payload["quota_total_used"] != 25.5 || payload["quota_total_available"] != 74.5 {
		t.Fatalf("unexpected total quota counters: %#v", payload)
	}
	mappingRules := payload["model_mappings"].([]store.APIKeyModelRule)
	if len(mappingRules) != 1 || mappingRules[0].Pattern != "gpt-5" || mappingRules[0].Target != "upstream-gpt-5" {
		t.Fatalf("unexpected model mappings payload: %#v", payload["model_mappings"])
	}
	rateLimit := payload["rate_limit"].(map[string]any)
	if rateLimit["status"] != store.RateLimitStatusDisabled || rateLimit["rpm_limit"] != nil || rateLimit["tpm_limit"] != nil {
		t.Fatalf("unexpected default rate limit payload: %#v", rateLimit)
	}
	if len(payload["site_models"].([]map[string]any)) != 1 || len(payload["sites"].([]map[string]any)) != 1 || len(payload["site_groups"].([]map[string]any)) != 1 {
		t.Fatalf("expected access detail payloads, got %#v", payload)
	}
	if payload["expires_at"] == nil || payload["last_used_at"] == nil || payload["created_at"] == "" || payload["updated_at"] == "" {
		t.Fatalf("expected timestamp fields, got %#v", payload)
	}
}

func TestAPIKeySyncPayloadOmitsUnavailablePlaintextAndKeepsQuota(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	payload := (Handler{auth: adminAuthService()}).apiKeySyncPayload(store.APIKey{
		ID:             apiKeyID,
		Name:           "sync key",
		KeyPrefix:      "xly",
		MaskedKey:      "xly_****",
		KeyKind:        "custom",
		Scope:          "gateway",
		Status:         "active",
		QuotaLimit:     sql.NullFloat64{Float64: 10, Valid: true},
		QuotaUsed:      12,
		QuotaTotalUsed: 8,
		QuotaUnlimited: false,
	})

	if payload["id"] != apiKeyID.String() || payload["name"] != "sync key" || payload["key"] != nil {
		t.Fatalf("unexpected sync identity payload: %#v", payload)
	}
	if payload["quota_available"] != 2.0 || payload["quota_total_used"] != 8.0 || payload["quota_limit"] != 10.0 || payload["quota_unlimited"] != false {
		t.Fatalf("unexpected sync quota payload: %#v", payload)
	}
}

func TestAPIKeyPayloadIncludesCurrentPeriodicQuotaState(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("UTC")
	now := time.Now()
	dailyStart := timeZone.StartOfDay(now)
	weeklyStart := timeZone.StartOfWeek(now)
	payload := (Handler{timeZone: timeZone}).apiKeyPayload(context.Background(), store.APIKey{
		ID:                     uuid.New(),
		QuotaUnlimited:         true,
		QuotaDailyLimit:        sql.NullFloat64{Float64: 20, Valid: true},
		QuotaDailyUsed:         7.5,
		QuotaDailyWindowStart:  &dailyStart,
		QuotaWeeklyLimit:       sql.NullFloat64{Float64: 100, Valid: true},
		QuotaWeeklyUsed:        40,
		QuotaWeeklyWindowStart: &weeklyStart,
	}, nil, nil, nil, false)

	if payload["quota_daily_used"] != 7.5 || payload["quota_daily_available"] != 12.5 || payload["quota_daily_reset_at"] == nil {
		t.Fatalf("unexpected daily quota payload: %#v", payload)
	}
	if payload["quota_weekly_used"] != 40.0 || payload["quota_weekly_available"] != 60.0 || payload["quota_weekly_reset_at"] == nil {
		t.Fatalf("unexpected weekly quota payload: %#v", payload)
	}
}

func TestAPIKeyRateLimitPayloadDefaultsAndPointerValues(t *testing.T) {
	t.Parallel()

	payload := (Handler{}).apiKeyRateLimitPayload(context.Background(), uuid.New())

	if payload["status"] != store.RateLimitStatusDisabled || payload["rpm_limit"] != nil || payload["tpm_limit"] != nil {
		t.Fatalf("unexpected default rate limit payload: %#v", payload)
	}
	if got := int64PtrValue(nil); got != nil {
		t.Fatalf("nil int64 pointer value = %#v, want nil", got)
	}
	value := int64(60)
	if got := int64PtrValue(&value); got != value {
		t.Fatalf("int64 pointer value = %#v, want %d", got, value)
	}
}

func TestParseOptionalTimeHandlesOmittedNullAndFutureValue(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", "")

	value, provided, ok := parseOptionalTime(adminRecorder(), req, nil)
	if !ok || provided || value != nil {
		t.Fatalf("omitted expires_at = value=%v provided=%v ok=%v, want nil false true", value, provided, ok)
	}

	value, provided, ok = parseOptionalTime(adminRecorder(), req, json.RawMessage(`null`))
	if !ok || !provided || value != nil {
		t.Fatalf("null expires_at = value=%v provided=%v ok=%v, want nil true true", value, provided, ok)
	}

	future := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	raw, err := json.Marshal(future.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("marshal future time: %v", err)
	}
	value, provided, ok = parseOptionalTime(adminRecorder(), req, raw)
	if !ok || !provided || value == nil || !value.Equal(future) {
		t.Fatalf("future expires_at = value=%v provided=%v ok=%v, want %v true true", value, provided, ok, future)
	}
}

func TestParseOptionalTimeRejectsInvalidAndPastValues(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", "")

	for _, tc := range []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "non_string", raw: json.RawMessage(`123`)},
		{name: "bad_format", raw: json.RawMessage(`"tomorrow"`)},
		{name: "past", raw: json.RawMessage(`"2000-01-01T00:00:00Z"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminRecorder()
			_, _, ok := parseOptionalTime(rec, req, tc.raw)

			adminAssertParserError(t, rec, ok, "invalid_expires_at")
		})
	}
}

func TestParseOptionalTimeHandlesBlankRawAndBlankStringAsProvidedNull(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", "")

	for _, raw := range []json.RawMessage{
		json.RawMessage(" \t\n "),
		json.RawMessage(`"  \t "`),
	} {
		value, provided, ok := parseOptionalTime(adminRecorder(), req, raw)
		if !ok || !provided || value != nil {
			t.Fatalf("blank expires_at = value=%v provided=%v ok=%v, want nil true true", value, provided, ok)
		}
	}
}

func TestParseOptionalTimeRejectsCurrentInstant(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", "")
	raw, err := json.Marshal(time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("marshal current time: %v", err)
	}
	rec := adminRecorder()

	_, _, ok := parseOptionalTime(rec, req, raw)

	adminAssertParserError(t, rec, ok, "invalid_expires_at")
}

func TestAPIKeyInputFromRequestParsesScopesQuotaAndRateLimit(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", "")
	rec := adminRecorder()
	quotaLimit := 100.5
	rpmLimit := int64(60)
	tpmLimit := int64(1000)
	siteID := uuid.New()
	siteModelID := uuid.New()
	groupID := uuid.New()
	payload := apiKeyRequest{
		Name:           "gateway key",
		ModelPolicy:    "allow",
		SiteModelIDs:   []string{" " + siteModelID.String() + " "},
		SitePolicy:     "allow",
		SiteIDs:        []string{siteID.String(), ""},
		SiteGroupIDs:   []string{groupID.String()},
		ModelMappings:  []modelRuleRequest{{Pattern: "gpt-4.1", Target: "upstream-gpt-4.1"}},
		QuotaLimit:     &quotaLimit,
		QuotaUnlimited: nil,
		RateLimit: &rateLimitRequest{
			Status:   "enabled",
			RPMLimit: &rpmLimit,
			TPMLimit: &tpmLimit,
		},
	}

	input, ok := apiKeyInputFromRequest(rec, req, payload)

	adminRequireParserOK(t, rec, ok, "api key input")
	if input.Name != payload.Name || input.ModelPolicy != payload.ModelPolicy || input.SitePolicy != payload.SitePolicy {
		t.Fatalf("unexpected basic input fields: %#v", input)
	}
	if input.QuotaLimit == nil || *input.QuotaLimit != quotaLimit {
		t.Fatalf("quota limit = %#v, want %v", input.QuotaLimit, quotaLimit)
	}
	if input.QuotaUnlimited {
		t.Fatal("quota_unlimited should default to false when quota_limit is provided")
	}
	if len(input.SiteIDs) != 1 || input.SiteIDs[0] != siteID {
		t.Fatalf("site ids = %#v, want %s", input.SiteIDs, siteID)
	}
	if len(input.SiteModelIDs) != 1 || input.SiteModelIDs[0] != siteModelID {
		t.Fatalf("site model ids = %#v, want %s", input.SiteModelIDs, siteModelID)
	}
	if len(input.SiteGroupIDs) != 1 || input.SiteGroupIDs[0] != groupID {
		t.Fatalf("site group ids = %#v, want %s", input.SiteGroupIDs, groupID)
	}
	if len(input.ModelRules) != 1 || input.ModelRules[0].Pattern != "gpt-4.1" || input.ModelRules[0].Target != "upstream-gpt-4.1" {
		t.Fatalf("model rules = %#v", input.ModelRules)
	}
	if input.RateLimit == nil || input.RateLimit.Status != "enabled" || input.RateLimit.RPMLimit == nil || *input.RateLimit.RPMLimit != rpmLimit || input.RateLimit.TPMLimit == nil || *input.RateLimit.TPMLimit != tpmLimit {
		t.Fatalf("unexpected rate limit input: %#v", input.RateLimit)
	}
}

func TestAPIKeyInputFromRequestDefaultsQuotaToUnlimited(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", "")
	rec := adminRecorder()

	input, ok := apiKeyInputFromRequest(rec, req, apiKeyRequest{Name: "gateway key"})

	adminRequireParserOK(t, rec, ok, "api key input")
	if !input.QuotaUnlimited {
		t.Fatal("quota_unlimited should default to true when no quota limit is provided")
	}
	if input.QuotaDailyUnlimited == nil || !*input.QuotaDailyUnlimited {
		t.Fatal("quota_daily_unlimited should default to true when no daily quota limit is provided")
	}
	if input.QuotaWeeklyUnlimited == nil || !*input.QuotaWeeklyUnlimited {
		t.Fatal("quota_weekly_unlimited should default to true when no weekly quota limit is provided")
	}
	if input.RateLimit != nil {
		t.Fatalf("rate limit = %#v, want nil", input.RateLimit)
	}
}

func TestAPIKeyInputFromRequestRejectsInvalidScopeID(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPost, "/api/v1/api-keys", "")
	rec := adminRecorder()

	_, ok := apiKeyInputFromRequest(rec, req, apiKeyRequest{SiteIDs: []string{"not-a-uuid"}})

	adminAssertParserError(t, rec, ok, "invalid_site_id")
}

func TestAPIKeyUpdateInputPreservesCurrentFieldsWhenOmitted(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPatch, "/api/v1/api-keys/key-id", "")
	rec := adminRecorder()
	currentExpiresAt := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	current := store.APIKey{
		ExpiresAt:      &currentExpiresAt,
		QuotaLimit:     sql.NullFloat64{Float64: 42.5, Valid: true},
		QuotaUnlimited: false,
	}

	input, ok := apiKeyUpdateInputFromRequest(rec, req, apiKeyRequest{}, current)

	adminRequireParserOK(t, rec, ok, "api key update input")
	if input.ExpiresAt == nil || !input.ExpiresAt.Equal(currentExpiresAt) {
		t.Fatalf("expires_at = %v, want %v", input.ExpiresAt, currentExpiresAt)
	}
	if input.QuotaLimit == nil || *input.QuotaLimit != current.QuotaLimit.Float64 {
		t.Fatalf("quota limit = %#v, want %v", input.QuotaLimit, current.QuotaLimit.Float64)
	}
	if input.QuotaUnlimited != current.QuotaUnlimited {
		t.Fatalf("quota_unlimited = %v, want %v", input.QuotaUnlimited, current.QuotaUnlimited)
	}
}

func TestAPIKeyUpdateInputAllowsExplicitNullExpiresAt(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPatch, "/api/v1/api-keys/key-id", "")
	rec := adminRecorder()
	currentExpiresAt := time.Now().UTC().Add(48 * time.Hour)
	current := store.APIKey{ExpiresAt: &currentExpiresAt}

	input, ok := apiKeyUpdateInputFromRequest(rec, req, apiKeyRequest{ExpiresAt: json.RawMessage(`null`)}, current)

	adminRequireParserOK(t, rec, ok, "api key update input")
	if input.ExpiresAt != nil {
		t.Fatalf("expires_at = %v, want nil", input.ExpiresAt)
	}
}

func TestAPIKeyUpdateInputParsesAccessSets(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPatch, "/api/v1/api-keys/key-id", "")
	rec := adminRecorder()
	siteID := uuid.New()
	siteModelID := uuid.New()
	groupID := uuid.New()

	input, ok := apiKeyUpdateInputFromRequest(rec, req, apiKeyRequest{
		SiteIDs:      []string{siteID.String()},
		SiteModelIDs: []string{siteModelID.String()},
		SiteGroupIDs: []string{groupID.String()},
	}, store.APIKey{})

	adminRequireParserOK(t, rec, ok, "api key update access sets")
	if len(input.SiteIDs) != 1 || input.SiteIDs[0] != siteID {
		t.Fatalf("site ids = %#v, want %s", input.SiteIDs, siteID)
	}
	if len(input.SiteModelIDs) != 1 || input.SiteModelIDs[0] != siteModelID {
		t.Fatalf("site model ids = %#v, want %s", input.SiteModelIDs, siteModelID)
	}
	if len(input.SiteGroupIDs) != 1 || input.SiteGroupIDs[0] != groupID {
		t.Fatalf("site group ids = %#v, want %s", input.SiteGroupIDs, groupID)
	}
}

func TestAPIKeyUpdateInputPreservesOmittedAccessSets(t *testing.T) {
	t.Parallel()

	req := adminTestRequest(http.MethodPatch, "/api/v1/api-keys/key-id", "")
	rec := adminRecorder()

	input, ok := apiKeyUpdateInputFromRequest(rec, req, apiKeyRequest{}, store.APIKey{})

	adminRequireParserOK(t, rec, ok, "api key update omitted access sets")
	if input.SiteIDs != nil || input.SiteModelIDs != nil || input.SiteGroupIDs != nil {
		t.Fatalf("access sets = %#v %#v %#v, want nil", input.SiteIDs, input.SiteModelIDs, input.SiteGroupIDs)
	}
}

func TestAPIKeyQuotaAvailable(t *testing.T) {
	t.Parallel()

	if got := apiKeyQuotaAvailable(store.APIKey{QuotaUnlimited: true, QuotaLimit: sql.NullFloat64{Float64: 10, Valid: true}}); got != nil {
		t.Fatalf("unlimited quota available = %#v, want nil", got)
	}
	if got := apiKeyQuotaAvailable(store.APIKey{}); got != nil {
		t.Fatalf("missing quota limit available = %#v, want nil", got)
	}
	if got := apiKeyQuotaAvailable(store.APIKey{QuotaLimit: sql.NullFloat64{Float64: 10, Valid: true}, QuotaUsed: 12}); got != 0 {
		t.Fatalf("overused quota available = %#v, want 0", got)
	}
	if got := apiKeyQuotaAvailable(store.APIKey{QuotaLimit: sql.NullFloat64{Float64: 10, Valid: true}, QuotaUsed: 3.5}); got != 6.5 {
		t.Fatalf("remaining quota available = %#v, want 6.5", got)
	}
}

func TestModelMappingsPayloadReturnsOnlyNonEmptyRules(t *testing.T) {
	t.Parallel()

	if got := modelMappingsPayload(nil); got != nil {
		t.Fatalf("nil mapping payload = %#v, want nil", got)
	}
	if got := modelMappingsPayload(store.JSON(`[]`)); got != nil {
		t.Fatalf("empty rules payload = %#v, want nil", got)
	}
	if got := modelMappingsPayload(store.JSON(`{}`)); got != nil {
		t.Fatalf("empty legacy payload = %#v, want nil", got)
	}
	legacy := modelMappingsPayload(store.JSON(`{"gpt-4.1":"upstream-gpt-4.1"}`))
	if len(legacy) != 1 || legacy[0].Pattern != "gpt-4.1" || legacy[0].Target != "upstream-gpt-4.1" || legacy[0].Mode != store.APIKeyModelRuleModeHard {
		t.Fatalf("legacy mapping payload = %#v, want single hard rule", legacy)
	}
	rules := modelMappingsPayload(store.JSON(`[{"pattern":"gpt-4*","target":"gpt-5.5","mode":"soft"}]`))
	if len(rules) != 1 || rules[0].Pattern != "gpt-4*" || rules[0].Target != "gpt-5.5" || rules[0].Mode != store.APIKeyModelRuleModeSoft {
		t.Fatalf("rules payload = %#v, want single soft rule", rules)
	}
}

func TestAPIKeyPermissionPayloadsExposeExpectedFields(t *testing.T) {
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

	apiKeyID := uuid.New()
	siteID := uuid.New()
	siteModelID := uuid.New()
	canonicalID := uuid.New()
	modelPayloads := apiKeySiteModelPayloads([]store.APIKeySiteModelPermissionDetail{{
		ID:                uuid.New(),
		APIKeyID:          apiKeyID,
		SiteModelID:       siteModelID,
		SiteID:            siteID,
		SiteName:          "Codex",
		SiteSlug:          "codex",
		SiteType:          "codex",
		UpstreamModelName: "gpt-4.1",
		DisplayName:       "GPT 4.1",
		CanonicalModelID:  uuid.NullUUID{UUID: canonicalID, Valid: true},
		CanonicalModelKey: sql.NullString{String: "gpt-4.1", Valid: true},
		Enabled:           true,
	}})
	if len(modelPayloads) != 1 {
		t.Fatalf("site model payload count = %d, want 1", len(modelPayloads))
	}
	if modelPayloads[0]["api_key_id"] != apiKeyID.String() || modelPayloads[0]["site_model_id"] != siteModelID.String() || modelPayloads[0]["canonical_model_key"] != "gpt-4.1" {
		t.Fatalf("unexpected site model payload: %#v", modelPayloads[0])
	}

	sitePayloads := apiKeySitePayloads([]store.APIKeySitePermission{{
		ID:       uuid.New(),
		APIKeyID: apiKeyID,
		SiteID:   siteID,
		Enabled:  true,
	}})
	if len(sitePayloads) != 1 || sitePayloads[0]["api_key_id"] != apiKeyID.String() || sitePayloads[0]["site_id"] != siteID.String() || sitePayloads[0]["enabled"] != true {
		t.Fatalf("unexpected site payloads: %#v", sitePayloads)
	}

	groupID := uuid.New()
	groupPayloads := apiKeySiteGroupPayloads([]store.APIKeySiteGroupPermission{{
		ID:       uuid.New(),
		APIKeyID: apiKeyID,
		GroupID:  groupID,
		Enabled:  true,
	}})
	if len(groupPayloads) != 1 || groupPayloads[0]["api_key_id"] != apiKeyID.String() || groupPayloads[0]["group_id"] != groupID.String() || groupPayloads[0]["enabled"] != true {
		t.Fatalf("unexpected site group payloads: %#v", groupPayloads)
	}
}
