package admin

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestSitePayloadHandlesEmptyAndInvalidMeta(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		meta store.JSON
	}{
		{name: "empty", meta: nil},
		{name: "invalid", meta: store.JSON(`{invalid`)},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			payload := sitePayload(store.Site{
				ID:       uuid.New(),
				Name:     "Edge Site",
				SiteType: "unknown",
				Meta:     tc.meta,
			})

			meta, ok := payload["meta"].(map[string]any)
			if !ok || len(meta) != 0 {
				t.Fatalf("meta = %#v, want empty object", payload["meta"])
			}
			if _, ok := payload["proxy_id"]; ok {
				t.Fatalf("proxy_id should be omitted for %s meta: %#v", tc.name, payload)
			}
			if _, ok := payload["request_headers"]; ok {
				t.Fatalf("request_headers should be omitted for %s meta: %#v", tc.name, payload)
			}
			if _, ok := payload["oauth_account"]; ok {
				t.Fatalf("oauth_account should be omitted for %s meta: %#v", tc.name, payload)
			}
			if payload["icon_url"] != "" {
				t.Fatalf("unknown site type icon = %#v, want empty", payload["icon_url"])
			}
		})
	}
}

func TestSitePayloadMetadataBranchesSkipBlankOAuthAndKeepHeaders(t *testing.T) {
	t.Parallel()

	payload := sitePayload(store.Site{
		ID:       uuid.New(),
		SiteType: "codex",
		Meta: store.JSON(`{
			"proxy_id": 12,
			"request_headers": {"X-Test": "yes"},
			"oauth_provider": " ",
			"oauth_connection_id": "",
			"oauth_account_id": " ",
			"oauth_email": ""
		}`),
	})

	if _, ok := payload["proxy_id"]; ok {
		t.Fatalf("non-string proxy_id should be omitted: %#v", payload["proxy_id"])
	}
	headers, ok := payload["request_headers"].(map[string]any)
	if !ok || headers["X-Test"] != "yes" {
		t.Fatalf("request headers = %#v, want decoded object", payload["request_headers"])
	}
	if _, ok := payload["oauth_account"]; ok {
		t.Fatalf("blank oauth metadata should not create oauth_account: %#v", payload["oauth_account"])
	}
}

func TestSiteAPIKeyPayloadFromStateFallsBackWithoutState(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	payload := (Handler{}).siteAPIKeyPayloadFromState(
		store.Site{ID: siteID, SiteType: "openai"},
		sitepkg.APIKeyCredential{
			Credential: store.SiteCredential{ID: credentialID, SiteID: siteID},
			Name:       "默认 Key",
			UpstreamID: 11,
			Enabled:    true,
			Meta: map[string]any{
				"group":        " ",
				"remain_quota": float64(8),
				"used_quota":   json.Number("2"),
			},
		},
		store.SiteAPIKeyState{},
		[]store.SiteAPIKeyModel{
			{UpstreamModelName: "gpt-visible", Available: true, Enabled: true},
			{UpstreamModelName: "gpt-hidden", Available: false, Enabled: true},
		},
		"Apikey 4",
	)

	if payload["upstream_id"] != 11 || payload["name"] != "Apikey 4" || payload["enabled"] != true {
		t.Fatalf("credential fallback fields unexpected: %#v", payload)
	}
	if payload["status"] != "active" {
		t.Fatalf("missing status fallback = %#v, want active", payload["status"])
	}
	if _, ok := payload["group"]; ok {
		t.Fatalf("blank metadata group should be omitted: %#v", payload["group"])
	}
	models := payload["models"].([]string)
	if len(models) != 1 || models[0] != "gpt-visible" {
		t.Fatalf("models = %#v, want only available model", models)
	}
	usage := payload["usage"].(map[string]any)
	if usage["success"] != true {
		t.Fatalf("usage should be built from credential meta: %#v", usage)
	}
	data := usage["data"].(map[string]any)
	if data["total_granted"] != 10 || data["total_used"] != 2 || data["total_available"] != 8 {
		t.Fatalf("usage data = %#v, want quota totals from meta", data)
	}
}

func TestSiteModelPayloadNullAndInvalidJSONFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	payload := siteModelPayload(store.SiteModel{
		ID:           uuid.New(),
		SiteID:       uuid.New(),
		Capabilities: store.JSON(`{bad`),
		Status:       "unavailable",
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	if payload["canonical_model_id"] != nil {
		t.Fatalf("canonical_model_id = %#v, want nil", payload["canonical_model_id"])
	}
	if payload["canonical_matched_at"] != nil {
		t.Fatalf("canonical_matched_at = %#v, want nil", payload["canonical_matched_at"])
	}
	if payload["enabled"] != false {
		t.Fatalf("enabled = %#v, want false for unavailable status", payload["enabled"])
	}
	capabilities, ok := payload["capabilities"].(map[string]any)
	if !ok || len(capabilities) != 0 {
		t.Fatalf("capabilities = %#v, want empty object for invalid JSON", payload["capabilities"])
	}
}

func TestCanonicalModelPayloadProviderIconsAndJSONEdges(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 9, 15, 0, 0, time.UTC)
	payload := canonicalModelPayload(store.CanonicalModel{
		ID:           uuid.New(),
		ModelKey:     "kimi-k2",
		DisplayName:  "Kimi K2",
		Provider:     "kimi_code",
		Category:     "chat",
		Capabilities: store.JSON(`[]`),
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	if payload["icon_url"] != "/brand-icons/moonshot.png" {
		t.Fatalf("kimi_code icon = %#v, want moonshot icon", payload["icon_url"])
	}
	capabilities, ok := payload["capabilities"].([]any)
	if !ok || len(capabilities) != 0 {
		t.Fatalf("capabilities = %#v, want decoded empty array", payload["capabilities"])
	}

	invalid := canonicalModelPayload(store.CanonicalModel{
		ID:           uuid.New(),
		Provider:     "no-such-provider",
		Capabilities: store.JSON(`{bad`),
	})
	if invalid["icon_url"] != "" {
		t.Fatalf("unknown provider icon = %#v, want empty", invalid["icon_url"])
	}
	emptyCapabilities, ok := invalid["capabilities"].(map[string]any)
	if !ok || len(emptyCapabilities) != 0 {
		t.Fatalf("invalid capabilities = %#v, want empty object", invalid["capabilities"])
	}
}

func TestModelMappingsPayloadRejectsInvalidAndDropsBlankLegacyEntries(t *testing.T) {
	t.Parallel()

	for _, raw := range []store.JSON{
		store.JSON(`{bad`),
		store.JSON(`{"gpt-4.1": 42}`),
		store.JSON(`{"gpt-4.1": null}`),
	} {
		if got := modelMappingsPayload(raw); got != nil {
			t.Fatalf("mapping payload for %s = %#v, want nil", raw, got)
		}
	}
	got := modelMappingsPayload(store.JSON(`{"gpt-4.1":"upstream","gpt-4o":""}`))
	if len(got) != 1 || got[0].Pattern != "gpt-4.1" || got[0].Target != "upstream" {
		t.Fatalf("legacy payload = %#v, want blank entry dropped", got)
	}
}

func TestAPIKeyQuotaAvailableAllowsZeroLimitAndExactUsage(t *testing.T) {
	t.Parallel()

	if got := apiKeyQuotaAvailable(store.APIKey{
		QuotaLimit: sql.NullFloat64{Float64: 0, Valid: true},
	}); got != float64(0) {
		t.Fatalf("zero quota available = %#v, want 0", got)
	}
	if got := apiKeyQuotaAvailable(store.APIKey{
		QuotaLimit: sql.NullFloat64{Float64: 7.25, Valid: true},
		QuotaUsed:  7.25,
	}); got != float64(0) {
		t.Fatalf("exactly used quota available = %#v, want 0", got)
	}
}

func TestXLyraAuthConfigDefaultsToAPIKeyWithoutSiteService(t *testing.T) {
	t.Parallel()

	got := (Handler{}).xlyraAuthConfig(t.Context(), uuid.New())
	want := map[string]any{"xlyra": map[string]any{"auth_mode": "api_key"}}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("xlyra auth config = %#v, want %#v", got, want)
	}
}

func TestSiteUsagePayloadBuildsScalarUsageShape(t *testing.T) {
	t.Parallel()

	first := time.Date(2026, 6, 22, 1, 2, 3, 0, time.UTC)
	last := time.Date(2026, 6, 22, 4, 5, 6, 0, time.UTC)
	row := store.SiteUsageSummaryRow{
		RequestCount:     10,
		SuccessCount:     8,
		FailedCount:      2,
		PromptTokens:     100,
		CompletionTokens: 200,
		TotalTokens:      300,
		EstimatedCost:    0.42,
		Currency:         "USD",
		FirstRequestAt:   first,
		LastRequestAt:    last,
	}

	got := siteUsagePayload(row)
	want := map[string]any{
		"request_count":     int64(10),
		"success_count":     int64(8),
		"failed_count":      int64(2),
		"prompt_tokens":     int64(100),
		"completion_tokens": int64(200),
		"total_tokens":      int64(300),
		"estimated_cost":    0.42,
		"currency":          "USD",
		"first_request_at":  timeString(first),
		"last_request_at":   timeString(last),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage payload = %#v, want %#v", got, want)
	}
}
