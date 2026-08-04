package site

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestSummarizePreparedRefreshKeyKeepsExistingUsageWhenFetchedQuotaIsEmpty(t *testing.T) {
	t.Parallel()

	existingUsage := map[string]any{
		"source": "token_list",
		"data":   map[string]any{"object": "token_usage"},
	}
	fetcher := &fakeAPIKeySummaryFetcher{
		summary: adapter.APIKeySummary{
			Usage: map[string]any{"success": true, "data": map[string]any{"object": "empty"}},
			Models: []adapter.Model{{
				UpstreamName: "gpt-preserved-usage",
				DisplayName:  "GPT Preserved Usage",
			}},
		},
	}

	summary := (&Service{}).summarizePreparedRefreshKey(t.Context(), store.Site{}, fetcher, preparedRefreshKey{
		hasRawKey:      true,
		resolvedAPIKey: "sk-preserved-usage",
		usage:          existingUsage,
	})

	if fetcher.seenAPIKey != "sk-preserved-usage" {
		t.Fatalf("fetcher api key = %q, want sk-preserved-usage", fetcher.seenAPIKey)
	}
	gotUsage, _ := summary.usage.(map[string]any)
	if gotUsage["source"] != "token_list" {
		t.Fatalf("summary usage = %#v, want existing token-list usage to be preserved", summary.usage)
	}
	if summary.syncStatus != "synced" || summary.syncMessage != nil || summary.keyError {
		t.Fatalf("unexpected summary status: %#v", summary)
	}
	if len(summary.models) != 1 || summary.models[0].model.UpstreamName != "gpt-preserved-usage" {
		t.Fatalf("summary models = %#v, want fetched model", summary.models)
	}
}

func TestOAuthConnectionRequiresManualRefreshFromMetadataAuthFailures(t *testing.T) {
	t.Parallel()

	connection := store.OAuthConnection{
		Provider: "codex",
		Status:   " active ",
		Metadata: siteJSONMeta(t, map[string]any{
			"last_error": "refresh_token_reused by upstream",
		}),
	}
	if !oauthConnectionRequiresManualRefresh(connection) {
		t.Fatal("last_error refresh_token_reused should require manual OAuth reconnect")
	}

	connection.Metadata = siteJSONMeta(t, map[string]any{
		"quota": map[string]any{
			"available": false,
			"detail":    "token refresh returned HTTP 401",
		},
	})
	if !oauthConnectionRequiresManualRefresh(connection) {
		t.Fatal("quota auth detail should require manual OAuth reconnect")
	}

	connection.Metadata = siteJSONMeta(t, map[string]any{
		"quota": map[string]any{
			"available": true,
			"detail":    "invalid_grant",
		},
	})
	if oauthConnectionRequiresManualRefresh(connection) {
		t.Fatal("available quota should not require manual OAuth reconnect")
	}
}

func TestModelsFromUserSummaryPayloadFallsBackToDirectKeysWhenNestedListsAreEmpty(t *testing.T) {
	t.Parallel()

	models := modelsFromUserSummaryPayload(map[string]any{
		"data":       []any{},
		"models":     []string{},
		"success":    true,
		"message":    "ok",
		" ":          map[string]any{"ignored": true},
		"gpt-direct": map[string]any{"enabled": true},
	})

	if len(models) != 3 {
		t.Fatalf("models = %#v, want direct fallback models after empty nested payloads", models)
	}
	byName := map[string]adapter.Model{}
	for _, model := range models {
		byName[model.UpstreamName] = model
	}
	model, ok := byName["gpt-direct"]
	if !ok || model.DisplayName != "gpt-direct" {
		t.Fatalf("model identity = %#v, want direct model fallback", models)
	}
	raw, ok := model.Capabilities["raw"].(map[string]any)
	if !ok || raw["enabled"] != true {
		t.Fatalf("model capabilities = %#v, want raw direct map preserved", model.Capabilities)
	}
	if _, ok := byName["data"]; !ok {
		t.Fatalf("models = %#v, want empty data key to fall through as direct model name", models)
	}
	if _, ok := byName["models"]; !ok {
		t.Fatalf("models = %#v, want empty models key to fall through as direct model name", models)
	}
}

func TestPricingModelCapabilitiesDropsNonStringEndpointItems(t *testing.T) {
	t.Parallel()

	capabilities := pricingModelCapabilities(adapter.ModelPricing{
		Raw: map[string]any{
			"supported_endpoint_types": []any{" openai ", 123, nil, " anthropic-messages "},
		},
	})

	endpoints, ok := capabilities["supported_endpoint_types"].([]string)
	if !ok {
		t.Fatalf("endpoint capabilities = %#v, want []string", capabilities["supported_endpoint_types"])
	}
	if len(endpoints) != 2 || endpoints[0] != "openai" || endpoints[1] != "anthropic-messages" {
		t.Fatalf("endpoint capabilities = %#v, want trimmed string-only endpoints", endpoints)
	}
}

func TestNormalizeModelPriceInputRejectsNegativeOutputValue(t *testing.T) {
	t.Parallel()

	output := -0.01
	_, err := normalizeModelPriceInput(ModelPriceInput{
		BillingType: "tokens",
		OutputValue: &output,
	})
	if !errors.Is(err, ErrModelPriceInvalid) || !strings.Contains(err.Error(), "output_value must not be negative") {
		t.Fatalf("negative output value error = %v, want ErrModelPriceInvalid output_value branch", err)
	}
}

func TestModelPriceCompleteRejectsNegativeOrInvalidPerRequestValue(t *testing.T) {
	t.Parallel()

	pricing := store.SiteModelPricing{
		Available:       true,
		BillingType:     "per_request",
		PerRequestValue: sql.NullFloat64{Float64: -0.01, Valid: true},
	}
	if modelPriceComplete(&pricing) {
		t.Fatal("negative per-request pricing should not be complete")
	}

	pricing.PerRequestValue = sql.NullFloat64{Float64: 0.01, Valid: false}
	if modelPriceComplete(&pricing) {
		t.Fatal("invalid per-request nullable value should not be complete")
	}
}

func TestBulkPriceAndManualModelValidationReturnBeforeStoreUse(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	price := 0.1
	_, err := service.BulkUpsertModelPrices(t.Context(), BulkModelPriceInput{
		CanonicalModelID: uuid.New(),
		SiteModelIDs:     []uuid.UUID{uuid.New()},
		ModelPriceInput: ModelPriceInput{
			BillingType: "tokens",
			InputValue:  &price,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("bulk upsert error = %v, want store initialization guard after input validation", err)
	}

	_, err = service.CreateManualSiteModel(t.Context(), CreateManualSiteModelParams{
		SiteID:                 uuid.New(),
		UpstreamModelName:      "gpt-manual-valid",
		CanonicalModelID:       uuid.New(),
		SupportedEndpointTypes: []string{"openai"},
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("manual model error = %v, want store initialization guard after input validation", err)
	}
}
