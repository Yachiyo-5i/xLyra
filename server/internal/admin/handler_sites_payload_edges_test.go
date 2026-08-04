package admin

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestUpdateSiteModelRequiresEnabled(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	req := adminRequestWithRouteParam(http.MethodPatch, "/api/v1/sites/site-id/models/model-id", `{"enabled":null}`, "siteID", uuid.New().String())
	req = withRouteParam(req, "modelID", uuid.New().String())
	rec := adminPerform(handler.UpdateSiteModel, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_enabled")
}

func TestUpdateSiteModelsStatusRequiresEnabledBeforeModelIDs(t *testing.T) {
	t.Parallel()

	handler := adminHandlerWithSiteService()
	req := adminRequestWithRouteParam(http.MethodPatch, "/api/v1/sites/site-id/models/status", `{"model_ids":["bad-id"]}`, "siteID", uuid.New().String())
	rec := adminPerform(handler.UpdateSiteModelsStatus, req)

	assertAdminErrorCode(t, rec, http.StatusBadRequest, "invalid_enabled")
}

func TestSitePayloadWithStateUsesPendingDefaultsForMissingState(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	siteID := uuid.New()
	payload := handler.sitePayloadWithState(store.Site{
		ID:       siteID,
		Name:     "Gateway",
		Slug:     "gateway",
		SiteType: "openai",
		BaseURL:  "https://api.example.test",
		Enabled:  true,
	}, store.SiteState{})

	if payload["model_count"] != 0 || payload["api_key_count"] != 0 {
		t.Fatalf("missing state counts = %#v/%#v, want zero", payload["model_count"], payload["api_key_count"])
	}
	syncState, ok := payload["sync_state"].(map[string]any)
	if !ok || syncState["status"] != "pending" {
		t.Fatalf("sync_state = %#v, want pending map", payload["sync_state"])
	}
	if _, ok := payload["validation"]; ok {
		t.Fatalf("missing state should not include validation payload: %#v", payload["validation"])
	}
}

func TestSitePayloadWithStateIncludesValidationWhenKnown(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	siteID := uuid.New()
	updatedAt := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	payload := handler.sitePayloadWithState(store.Site{
		ID:       siteID,
		SiteType: "openai",
	}, store.SiteState{
		SiteID:            siteID,
		ValidationOK:      sql.NullBool{Bool: false, Valid: true},
		ValidationMessage: sql.NullString{String: "invalid token", Valid: true},
		SyncStatus:        "failed",
		APIKeyCount:       2,
		ModelCount:        3,
		UpdatedAt:         updatedAt,
	})

	if payload["model_count"] != 3 || payload["api_key_count"] != 2 {
		t.Fatalf("state counts = %#v/%#v, want 3/2", payload["model_count"], payload["api_key_count"])
	}
	validation, ok := payload["validation"].(map[string]any)
	if !ok || validation["ok"] != false || validation["message"] != "invalid token" {
		t.Fatalf("validation payload = %#v", payload["validation"])
	}
	syncState, ok := payload["sync_state"].(map[string]any)
	if !ok || syncState["status"] != "failed" || syncState["model_count"] != 3 {
		t.Fatalf("sync_state = %#v", payload["sync_state"])
	}
}

func TestSiteAPIKeyPayloadFromStatePrefersStateAndSkipsUnavailableModels(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	credentialID := uuid.New()
	siteID := uuid.New()
	payload := handler.siteAPIKeyPayloadFromState(
		store.Site{ID: siteID, SiteType: "openai"},
		sitepkg.APIKeyCredential{
			Credential: store.SiteCredential{ID: credentialID},
			Name:       "Default Key",
			UpstreamID: 10,
			Enabled:    false,
			Meta: map[string]any{
				"group":        "meta-group",
				"remain_quota": float64(5),
				"used_quota":   float64(2),
			},
			MaskedSecret: "sk-***",
			Secret:       "sk-copy",
		},
		store.SiteAPIKeyState{
			SiteCredentialID: credentialID,
			UpstreamID:       sql.NullInt64{Int64: 99, Valid: true},
			Name:             "Primary",
			Enabled:          true,
			UpstreamStatus:   []byte(`"disabled"`),
			GroupName:        sql.NullString{String: "state-group", Valid: true},
			Usage:            []byte(`{"success":true}`),
			SyncMessage:      sql.NullString{String: "synced", Valid: true},
		},
		[]store.SiteAPIKeyModel{
			{UpstreamModelName: "gpt-5", Enabled: true, Available: true},
			{UpstreamModelName: "old-model", Enabled: true, Available: false},
			{UpstreamModelName: "gpt-4.1", Enabled: false, Available: true},
		},
		"Apikey 1",
	)

	if payload["upstream_id"] != 99 || payload["name"] != "Primary" || payload["enabled"] != true {
		t.Fatalf("state fields were not preferred: %#v", payload)
	}
	if payload["status"] != "disabled" || payload["group"] != "state-group" || payload["message"] != "synced" {
		t.Fatalf("state status/group/message missing: %#v", payload)
	}
	if _, hasCopyKey := payload["copy_key"]; hasCopyKey || payload["key"] != "sk-***" {
		t.Fatalf("secret fields = %#v", payload)
	}
	models := payload["models"].([]string)
	if len(models) != 2 || models[0] != "gpt-5" || models[1] != "gpt-4.1" {
		t.Fatalf("models = %#v, want available models only", models)
	}
	modelItems := payload["model_items"].([]map[string]any)
	if len(modelItems) != 2 || modelItems[1]["enabled"] != false {
		t.Fatalf("model_items = %#v", modelItems)
	}
	usage, ok := payload["usage"].(map[string]any)
	if !ok || usage["success"] != true {
		t.Fatalf("usage = %#v, want state usage", payload["usage"])
	}
}

func TestSiteAPIKeyPayloadExposesStaleSyncState(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	payload := (Handler{}).siteAPIKeyPayloadFromState(
		store.Site{ID: uuid.New(), SiteType: "newapi"},
		sitepkg.APIKeyCredential{
			Credential:   store.SiteCredential{ID: credentialID},
			Enabled:      true,
			MaskedSecret: "sk-***",
			Meta:         map[string]any{"status": "active"},
		},
		store.SiteAPIKeyState{
			SiteCredentialID: credentialID,
			Enabled:          true,
			SyncStatus:       "stale",
			SyncMessage:      sql.NullString{String: "removed upstream", Valid: true},
		},
		nil,
		"Apikey 1",
	)

	if payload["status"] != "stale" || payload["sync_status"] != "stale" || payload["enabled"] != true {
		t.Fatalf("stale payload = %#v", payload)
	}
}

func TestSiteModelPricingPayloadsIncludesSiteAndDerivedValues(t *testing.T) {
	t.Parallel()

	pricingID := uuid.New()
	siteID := uuid.New()
	modelID := uuid.New()
	items := siteModelPricingPayloads([]store.SiteModelPricing{{
		ID:                   pricingID,
		SiteID:               siteID,
		SiteModelID:          uuid.NullUUID{UUID: modelID, Valid: true},
		ModelName:            "gpt-5",
		GroupName:            "default",
		Currency:             "USD",
		GroupRatio:           2,
		InputValue:           sql.NullFloat64{Float64: 3, Valid: true},
		OutputValue:          sql.NullFloat64{Float64: 6, Valid: true},
		CacheRatio:           sql.NullFloat64{Float64: 0.5, Valid: true},
		AudioRatio:           sql.NullFloat64{Float64: 4, Valid: true},
		AudioCompletionRatio: sql.NullFloat64{Float64: 5, Valid: true},
		Raw:                  []byte(`{"source":"test"}`),
		Available:            true,
	}}, store.Site{
		ID:       siteID,
		Name:     "OpenAI",
		Slug:     "openai",
		SiteType: "openai",
		Enabled:  true,
	})

	if len(items) != 1 {
		t.Fatalf("payload count = %d, want 1", len(items))
	}
	payload := items[0]
	if payload["id"] != pricingID.String() || payload["site_name"] != "OpenAI" || payload["site_enabled"] != true {
		t.Fatalf("site/model fields missing: %#v", payload)
	}
	if payload["cache_input_value"] != float64(1.5) || payload["audio_input_value"] != float64(12) || payload["audio_output_value"] != float64(60) {
		t.Fatalf("derived values = cache %#v audio %#v audio_output %#v", payload["cache_input_value"], payload["audio_input_value"], payload["audio_output_value"])
	}
	if raw, ok := payload["raw"].(map[string]any); !ok || raw["source"] != "test" {
		t.Fatalf("raw payload = %#v", payload["raw"])
	}
	calculation := payload["calculation"].(map[string]any)
	encoded, err := json.Marshal(calculation)
	if err != nil || !json.Valid(encoded) {
		t.Fatalf("calculation payload should be JSON-serializable: %v %s", err, encoded)
	}
}

func TestCredentialModelPricingPayloadsUseKeyRatioAsGroupRatio(t *testing.T) {
	t.Parallel()

	items := credentialModelPricingPayloads([]sitepkg.CredentialModelPricing{{
		SiteCredentialID:  uuid.New(),
		CredentialName:    "Primary",
		RoutingPriority:   5,
		GroupRatio:        1.2,
		CredentialEnabled: true,
		CredentialUsable:  true,
		ModelEnabled:      true,
		ModelAvailable:    true,
	}}, &store.SiteModelPricing{
		BillingType:      "tokens",
		Currency:         "USD",
		InputValue:       sql.NullFloat64{Float64: 2, Valid: true},
		OutputValue:      sql.NullFloat64{Float64: 6, Valid: true},
		CacheRatio:       sql.NullFloat64{Float64: 0.5, Valid: true},
		CreateCacheRatio: sql.NullFloat64{Float64: 2, Valid: true},
	})

	if len(items) != 1 {
		t.Fatalf("credential pricing payload = %#v", items)
	}
	output, _ := items[0]["output_value"].(float64)
	if items[0]["group_ratio"] != 1.2 || items[0]["input_value"] != 2.4 || math.Abs(output-7.2) > 1e-9 {
		t.Fatalf("credential pricing payload = %#v", items)
	}
	if items[0]["cache_input_value"] != 1.2 || items[0]["create_cache_input_value"] != 4.8 {
		t.Fatalf("credential derived pricing payload = %#v", items[0])
	}
}
