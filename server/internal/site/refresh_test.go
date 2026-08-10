package site

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/modelcapabilities"
	"xlyra/server/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestMaskedKeysCompatibleAcceptsDifferentMaskStylesWithSameSuffix(t *testing.T) {
	t.Parallel()

	if !maskedKeysCompatible("sk-7...v464", "7127**********v464") {
		t.Fatal("expected completed sk-prefixed key mask to match upstream suffix mask")
	}
}

func TestMaskedKeysCompatibleRejectsDifferentVisibleSuffix(t *testing.T) {
	t.Parallel()

	if maskedKeysCompatible("sk-7...v464", "7127**********zz99") {
		t.Fatal("expected different visible suffix to be incompatible")
	}
}

func TestMaskedKeysCompatibleAcceptsEmptyOrNonComparableUpstreamMasks(t *testing.T) {
	t.Parallel()

	if !maskedKeysCompatible("sk-local", "") {
		t.Fatal("empty upstream mask should be compatible")
	}
	if !maskedKeysCompatible("sk-local", "**ab**") {
		t.Fatal("short visible mask fragments should not invalidate existing key")
	}
	if !maskedKeysCompatible("sk-prefix-abcdef", "abc****") {
		t.Fatal("visible prefix contained in local mask should be compatible")
	}
}

func TestMergeAPIKeyRefreshMetaPreservesLocalSwitches(t *testing.T) {
	t.Parallel()

	merged := mergeAPIKeyRefreshMeta(
		map[string]any{
			"enabled":                           false,
			credentialMetaAutoDisabledByRefresh: true,
			"disabled_models":                   []any{"gpt-test"},
			"manually_completed":                true,
		},
		map[string]any{
			"name":            "upstream",
			"raw_key_missing": false,
		},
	)

	if merged["enabled"] != false {
		t.Fatalf("expected enabled switch to be preserved, got %#v", merged)
	}
	if merged[credentialMetaAutoDisabledByRefresh] != true {
		t.Fatalf("expected automatic disable marker to be preserved, got %#v", merged)
	}
	if merged["manually_completed"] != true {
		t.Fatalf("expected manual completion marker to be preserved, got %#v", merged)
	}
	if merged["disabled_models"] == nil {
		t.Fatalf("expected disabled models to be preserved, got %#v", merged)
	}
}

func TestMergeAPIKeyRefreshMetaHandlesManualCompletionAndInvalidationPrecedence(t *testing.T) {
	t.Parallel()

	invalidated := mergeAPIKeyRefreshMeta(
		map[string]any{"manually_completed": true},
		map[string]any{"manual_secret_invalidated": true, "manual_secret_invalidated_at": "2026-06-22T01:02:03Z"},
	)
	if invalidated["manual_secret_invalidated"] != true || invalidated["manual_secret_invalidated_at"] != "2026-06-22T01:02:03Z" {
		t.Fatalf("expected upstream invalidation to win, got %#v", invalidated)
	}
	if _, ok := invalidated["manually_completed"]; ok {
		t.Fatalf("manual completion marker should be cleared after invalidation: %#v", invalidated)
	}

	completed := mergeAPIKeyRefreshMeta(
		map[string]any{"manual_secret_invalidated": true, "manual_secret_invalidated_at": "old"},
		map[string]any{"manually_completed": true},
	)
	if completed["manually_completed"] != true {
		t.Fatalf("expected upstream manual completion marker, got %#v", completed)
	}
	if _, ok := completed["manual_secret_invalidated"]; ok {
		t.Fatalf("manual invalidation marker should be cleared after completion: %#v", completed)
	}
	if _, ok := completed["manual_secret_invalidated_at"]; ok {
		t.Fatalf("manual invalidation timestamp should be cleared after completion: %#v", completed)
	}
}

func TestShouldPreserveExistingAPIKeySecretKeepsUpstreamFetchedRawKeys(t *testing.T) {
	t.Parallel()

	if !shouldPreserveExistingAPIKeySecret(map[string]any{"raw_key_missing": false}, "sk-existing") {
		t.Fatal("expected existing raw key to be preserved when upstream only returns masked key")
	}
	if shouldPreserveExistingAPIKeySecret(map[string]any{"raw_key_missing": true}, "sk-existing") {
		t.Fatal("missing raw key marker must not preserve encrypted placeholder")
	}
	if shouldPreserveExistingAPIKeySecret(map[string]any{"raw_key_missing": false}, "missing-api-key:7") {
		t.Fatal("placeholder secrets must not be treated as raw api keys")
	}
}

func TestAPIKeySecretForStoragePrefersRawThenMaskedThenStablePlaceholder(t *testing.T) {
	t.Parallel()

	if got := apiKeySecretForStorage(adapter.APIKey{Key: " sk-raw ", MaskedKey: "masked", ID: 7, Name: "name"}); got != "sk-raw" {
		t.Fatalf("raw key secret = %q, want sk-raw", got)
	}
	if got := apiKeySecretForStorage(adapter.APIKey{MaskedKey: " masked-key ", ID: 7, Name: "name"}); got != "masked-key" {
		t.Fatalf("masked key secret = %q, want masked-key", got)
	}
	if got := apiKeySecretForStorage(adapter.APIKey{ID: 7, Name: "name"}); got != "missing-api-key:7" {
		t.Fatalf("numeric placeholder = %q, want missing-api-key:7", got)
	}
	if got := apiKeySecretForStorage(adapter.APIKey{Name: " named-key "}); got != "missing-api-key:named-key" {
		t.Fatalf("name placeholder = %q, want missing-api-key:named-key", got)
	}
	if got := apiKeySecretForStorage(adapter.APIKey{}); got != "missing-api-key" {
		t.Fatalf("default placeholder = %q, want missing-api-key", got)
	}
}

func TestXlyraRefreshCanFallbackToAPIKeyOnlyForCredentialFailures(t *testing.T) {
	t.Parallel()

	if xlyraRefreshCanFallbackToAPIKey(nil) {
		t.Fatal("nil error should not fallback")
	}
	if !xlyraRefreshCanFallbackToAPIKey(errors.New("xLyra access token is required")) {
		t.Fatal("missing xLyra access token should fallback to API key refresh")
	}
	if !xlyraRefreshCanFallbackToAPIKey(errors.New("get site credential: not found")) {
		t.Fatal("credential lookup errors should fallback to API key refresh")
	}
	if xlyraRefreshCanFallbackToAPIKey(errors.New("pricing fetch timed out")) {
		t.Fatal("unrelated refresh errors should not fallback")
	}
}

func TestShouldDeleteAPIKeyCredentialsPreservesNewAPITokenOnlyUpdates(t *testing.T) {
	t.Parallel()

	deleteKeys := shouldDeleteAPIKeyCredentials("newapi", "newapi", []CredentialInput{
		{
			Type:   "newapi_access_token",
			Secret: "access-token",
			Meta:   map[string]any{"user_id": 7},
		},
	})

	if deleteKeys {
		t.Fatal("expected NewAPI access-token-only update to preserve completed api keys")
	}
}

func TestShouldDeleteAPIKeyCredentialsDeletesOnSiteTypeChange(t *testing.T) {
	t.Parallel()

	deleteKeys := shouldDeleteAPIKeyCredentials("openai", "newapi", []CredentialInput{
		{
			Type:   "newapi_access_token",
			Secret: "access-token",
			Meta:   map[string]any{"user_id": 7},
		},
	})

	if !deleteKeys {
		t.Fatal("expected site type change to clear old api key credentials")
	}
}

func TestOAuthPermanentAuthRefreshErrorDetectsOAuth4xx(t *testing.T) {
	t.Parallel()

	message := `codex token refresh returned 401: {"error":{"code":"refresh_token_reused"}}`
	if !OAuthPermanentAuthRefreshError("codex", message) {
		t.Fatal("expected codex 401 refresh error to require manual refresh")
	}
	if !OAuthPermanentAuthRefreshError("antigravity", "google token refresh returned 400: invalid_grant") {
		t.Fatal("expected antigravity 400 refresh error to require manual refresh")
	}
}

func TestOAuthPermanentAuthRefreshErrorIgnoresNonOAuthSites(t *testing.T) {
	t.Parallel()

	if OAuthPermanentAuthRefreshError("openai_compatible", "upstream returned 401") {
		t.Fatal("non-oauth sites must not be classified by oauth refresh rules")
	}
	if OAuthPermanentAuthRefreshError("codex", "context window is 400000 tokens") {
		t.Fatal("large non-status numbers must not be treated as HTTP 4xx")
	}
}

func TestOAuthPermanentAuthRefreshErrorIgnoresCodexTransientHTML403(t *testing.T) {
	t.Parallel()

	message := `codex upstream returned 403: <html><head><meta http-equiv="refresh" content="360"></head><body></body></html>`
	if OAuthPermanentAuthRefreshError("codex", message) {
		t.Fatal("transient codex html refresh challenge should not require manual refresh")
	}
	if !OAuthPermanentAuthRefreshError("codex", `codex token refresh returned 403: {"error":"invalid_token"}`) {
		t.Fatal("token refresh 403 should still require manual refresh")
	}
}

func TestRefreshValidationForSyncMarksOAuth401Failed(t *testing.T) {
	t.Parallel()

	message := `codex upstream returned 401: {"error":{"code":"token_invalidated"}}`
	status, validationOK, validationMessage, authFailure := refreshValidationForSync("codex", "partial", message)
	if status != "failed" {
		t.Fatalf("expected failed status, got %q", status)
	}
	if validationOK != false {
		t.Fatalf("expected validation false, got %#v", validationOK)
	}
	if validationMessage != message {
		t.Fatal("expected validation message to be preserved")
	}
	if authFailure != message {
		t.Fatal("expected auth failure message to be preserved")
	}
}

func TestRefreshValidationForSyncKeepsNonAuthPartial(t *testing.T) {
	t.Parallel()

	status, validationOK, validationMessage, authFailure := refreshValidationForSync("codex", "partial", "pricing fetch timed out")
	if status != "partial" {
		t.Fatalf("expected partial status, got %q", status)
	}
	if validationOK != true {
		t.Fatalf("expected validation true, got %#v", validationOK)
	}
	if validationMessage != nil {
		t.Fatalf("expected no validation message, got %#v", validationMessage)
	}
	if authFailure != "" {
		t.Fatalf("expected no auth failure, got %q", authFailure)
	}
}

func TestSummarizePreparedRefreshKeysHandlesEmptyInput(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	summaries := service.summarizePreparedRefreshKeys(t.Context(), store.Site{}, nil, nil)
	if len(summaries) != 0 {
		t.Fatalf("expected empty prepared key summaries, got %#v", summaries)
	}
}

func TestSummarizePreparedRefreshKeysMarksMissingRawKeysWithoutFetcher(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	keys := []preparedRefreshKey{
		{hasRawKey: false, usage: map[string]any{"index": 0}},
		{hasRawKey: false, usage: map[string]any{"index": 1}},
		{hasRawKey: false, usage: map[string]any{"index": 2}},
		{hasRawKey: false, usage: map[string]any{"index": 3}},
		{hasRawKey: false, usage: map[string]any{"index": 4}},
	}

	summaries := service.summarizePreparedRefreshKeys(t.Context(), store.Site{}, nil, keys)
	if len(summaries) != len(keys) {
		t.Fatalf("summary count = %d, want %d", len(summaries), len(keys))
	}
	for index, summary := range summaries {
		if summary.syncStatus != "partial" {
			t.Fatalf("summary[%d] status = %q, want partial", index, summary.syncStatus)
		}
		if summary.syncMessage == nil {
			t.Fatalf("summary[%d] missing sync message", index)
		}
		usage, _ := summary.usage.(map[string]any)
		if usage["index"] != index {
			t.Fatalf("summary[%d] usage = %#v, want original usage", index, summary.usage)
		}
		if len(summary.models) != 0 || summary.keyError {
			t.Fatalf("summary[%d] should not have models or key error: %#v", index, summary)
		}
	}
}

func TestSummarizePreparedRefreshKeyMarksMissingRawKeyPartial(t *testing.T) {
	t.Parallel()

	service := Service{}
	summary := service.summarizePreparedRefreshKey(t.Context(), store.Site{}, nil, preparedRefreshKey{
		hasRawKey: false,
		usage:     map[string]any{"preserved": true},
	})

	if summary.syncStatus != "partial" {
		t.Fatalf("expected partial sync status, got %q", summary.syncStatus)
	}
	if summary.syncMessage == nil {
		t.Fatal("expected missing raw key sync message")
	}
	if usage, _ := summary.usage.(map[string]any); usage["preserved"] != true {
		t.Fatalf("expected existing usage to be preserved, got %#v", summary.usage)
	}
	if len(summary.models) != 0 || summary.keyError {
		t.Fatalf("missing raw key should not produce models or key error, got %#v", summary)
	}
}

func TestSummarizePreparedRefreshKeyUsesFetcherResult(t *testing.T) {
	t.Parallel()

	service := Service{}
	fetcher := &fakeAPIKeySummaryFetcher{
		summary: adapter.APIKeySummary{
			Usage: map[string]any{
				"data": map[string]any{
					"total_available": float64(100),
				},
				"success": true,
			},
			Models: []adapter.Model{{
				UpstreamName: "gpt-5",
				DisplayName:  "GPT-5",
			}},
		},
	}

	summary := service.summarizePreparedRefreshKey(t.Context(), store.Site{BaseURL: "https://example.com", SiteType: "openai_compatible"}, fetcher, preparedRefreshKey{
		hasRawKey:      true,
		resolvedAPIKey: "sk-test",
		usage:          map[string]any{"old": true},
	})

	if fetcher.seenAPIKey != "sk-test" {
		t.Fatalf("fetcher api key = %q, want sk-test", fetcher.seenAPIKey)
	}
	if usage, _ := summary.usage.(map[string]any); usage["data"].(map[string]any)["total_available"] != float64(100) || usage["success"] != true {
		t.Fatalf("expected usage to be replaced by quota data, got %#v", summary.usage)
	}
	if summary.syncStatus != "synced" || summary.syncMessage != nil || summary.keyError {
		t.Fatalf("unexpected successful key summary status: %#v", summary)
	}
	if len(summary.models) != 1 || summary.models[0].model.UpstreamName != "gpt-5" {
		t.Fatalf("expected fetched models to be appended, got %#v", summary.models)
	}
}

func TestSummarizePreparedRefreshKeyMarksFetcherError(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	summary := service.summarizePreparedRefreshKey(t.Context(), store.Site{}, &fakeAPIKeySummaryFetcher{err: errors.New("upstream quota failed")}, preparedRefreshKey{
		hasRawKey:      true,
		resolvedAPIKey: "sk-test",
		usage:          map[string]any{"preserved": true},
	})

	if summary.syncStatus != "failed" || summary.syncMessage != "upstream quota failed" || !summary.keyError {
		t.Fatalf("unexpected failed key summary status: %#v", summary)
	}
	if usage, _ := summary.usage.(map[string]any); usage["preserved"] != true {
		t.Fatalf("expected existing usage to be preserved on failure, got %#v", summary.usage)
	}
	if len(summary.models) != 0 {
		t.Fatalf("failed summary should not append models, got %#v", summary.models)
	}
}

func TestAuthFailureErrorOnlyWrapsNonBlankMessages(t *testing.T) {
	t.Parallel()

	if err := authFailureError(" \t\n "); err != nil {
		t.Fatalf("blank auth failure should not become an error, got %v", err)
	}
	err := authFailureError("token invalidated")
	if err == nil || err.Error() != "token invalidated" {
		t.Fatalf("unexpected auth failure error: %v", err)
	}
}

func TestQuotaAndOAuthMetadataFromUserSummary(t *testing.T) {
	t.Parallel()

	summary := adapter.UserSummary{
		User: map[string]any{
			"quota":             map[string]any{"available": true},
			"project_id":        "proj_123",
			"subscription_tier": " plus ",
			"plan_type":         " ",
		},
	}

	quota := quotaFromUserSummary(summary)
	if quota == nil || quota["available"] != true {
		t.Fatalf("unexpected quota: %#v", quota)
	}

	meta := oauthMetadataFromUserSummary(summary)
	if meta["project_id"] != "proj_123" || meta["subscription_tier"] != " plus " {
		t.Fatalf("unexpected oauth metadata: %#v", meta)
	}
	if _, ok := meta["plan_type"]; ok {
		t.Fatalf("blank plan_type should be omitted: %#v", meta)
	}

	meta = oauthMetadataFromUserSummary(adapter.UserSummary{User: map[string]any{
		"project_id":        " project-1 ",
		"subscription_tier": " ",
		"plan_type":         "team",
	}})
	if meta["project_id"] != " project-1 " || meta["plan_type"] != "team" {
		t.Fatalf("metadata with plan_type = %#v, want project_id and plan_type", meta)
	}
	if _, ok := meta["subscription_tier"]; ok {
		t.Fatalf("blank subscription tier should be omitted: %#v", meta)
	}

	if quota := quotaFromUserSummary(adapter.UserSummary{User: "not a map"}); quota != nil {
		t.Fatalf("non-map quota = %#v, want nil", quota)
	}
	if meta := oauthMetadataFromUserSummary(adapter.UserSummary{User: "not a map"}); meta != nil {
		t.Fatalf("non-map metadata = %#v, want nil", meta)
	}
}

func TestModelSnapshotsIncludeCapabilitiesAndQuota(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	items := modelSnapshots([]store.SiteModel{
		{
			ID:           modelID,
			UpstreamName: "gpt-test",
			DisplayName:  "GPT Test",
			Status:       "active",
			Capabilities: siteJSONMeta(t, map[string]any{
				"source": "upstream",
				"quota":  map[string]any{"available": true},
			}),
		},
	})

	if len(items) != 1 {
		t.Fatalf("snapshots = %d, want 1", len(items))
	}
	item := items[0]
	if item["id"] != "gpt-test" || item["display_name"] != "GPT Test" || item["site_model_id"] != modelID.String() {
		t.Fatalf("unexpected snapshot identifiers: %#v", item)
	}
	if item["quota"] == nil || item["capabilities"] == nil {
		t.Fatalf("expected quota and capabilities in snapshot: %#v", item)
	}
}

func TestModelsFromUserSummaryPayloadVariants(t *testing.T) {
	t.Parallel()

	fromStrings := modelsFromUserSummaryPayload([]string{" gpt-string ", "", "claude-string"})
	if len(fromStrings) != 2 {
		t.Fatalf("string models = %#v, want 2", fromStrings)
	}
	if fromStrings[0].UpstreamName != "gpt-string" || fromStrings[1].DisplayName != "claude-string" {
		t.Fatalf("unexpected string models: %#v", fromStrings)
	}
	if fromStrings[0].Capabilities["source"] != "newapi_user_models" {
		t.Fatalf("string model capabilities = %#v, want newapi source", fromStrings[0].Capabilities)
	}

	models := modelsFromUserSummaryPayload([]any{
		" gpt-string ",
		map[string]any{"id": "gpt-map", "display_name": "GPT Map"},
		map[string]any{"model_name": "gpt-fallback"},
		"",
	})
	if len(models) != 3 {
		t.Fatalf("models = %#v, want 3", models)
	}
	if models[0].UpstreamName != "gpt-string" || models[1].DisplayName != "GPT Map" || models[2].UpstreamName != "gpt-fallback" {
		t.Fatalf("unexpected models: %#v", models)
	}
	if models[1].Capabilities["raw"] == nil {
		t.Fatalf("map payload should preserve raw capabilities: %#v", models[1].Capabilities)
	}

	nested := modelsFromUserSummaryPayload(map[string]any{
		"success": true,
		"message": "ok",
		"models":  []any{"nested-model"},
	})
	if len(nested) != 1 || nested[0].UpstreamName != "nested-model" {
		t.Fatalf("unexpected nested models: %#v", nested)
	}

	dataNested := modelsFromUserSummaryPayload(map[string]any{
		"data": []string{"data-model"},
	})
	if len(dataNested) != 1 || dataNested[0].UpstreamName != "data-model" {
		t.Fatalf("unexpected data models: %#v", dataNested)
	}

	direct := modelsFromUserSummaryPayload(map[string]any{
		"success":    true,
		"message":    "ok",
		"gpt-direct": map[string]any{"enabled": true},
	})
	if len(direct) != 1 || direct[0].UpstreamName != "gpt-direct" {
		t.Fatalf("unexpected direct map models: %#v", direct)
	}

	if got := modelsFromUserSummaryPayload("not models"); got != nil {
		t.Fatalf("unsupported payload models = %#v, want nil", got)
	}
}

func TestPricingModelHelpersUseDisplayFallbackAndEndpointTypes(t *testing.T) {
	t.Parallel()

	pricing := adapter.ModelPricing{
		ModelName: "gpt-priced",
		Raw: map[string]any{
			"display_name":             "GPT Priced",
			"supported_endpoint_types": []any{" openai ", "", "responses"},
		},
	}

	if got := pricingModelDisplayName(pricing); got != "GPT Priced" {
		t.Fatalf("display name = %q, want GPT Priced", got)
	}
	capabilities := pricingModelCapabilities(pricing)
	endpoints, ok := capabilities["supported_endpoint_types"].([]string)
	if !ok || len(endpoints) != 2 || endpoints[0] != "openai" || endpoints[1] != "responses" {
		t.Fatalf("unexpected endpoint capabilities: %#v", capabilities)
	}

	pricing.DisplayName = "Display Override"
	if got := pricingModelDisplayName(pricing); got != "Display Override" {
		t.Fatalf("display override = %q, want Display Override", got)
	}

	pricing.DisplayName = ""
	pricing.Raw = map[string]any{"name": "Raw Name"}
	if got := pricingModelDisplayName(pricing); got != "Raw Name" {
		t.Fatalf("raw name fallback = %q, want Raw Name", got)
	}
	pricing.Raw = nil
	if got := pricingModelDisplayName(pricing); got != "gpt-priced" {
		t.Fatalf("model name fallback = %q, want gpt-priced", got)
	}

	capabilities = pricingModelCapabilities(adapter.ModelPricing{
		Raw: map[string]any{"supported_endpoint_types": []string{" openai-image ", ""}},
	})
	endpoints, ok = capabilities["supported_endpoint_types"].([]string)
	if !ok || len(endpoints) != 1 || endpoints[0] != "openai-image" {
		t.Fatalf("unexpected string endpoint capabilities: %#v", capabilities)
	}
	if capabilities := pricingModelCapabilities(adapter.ModelPricing{}); capabilities["source"] != "newapi_pricing" || capabilities["supported_endpoint_types"] != nil {
		t.Fatalf("nil raw capabilities = %#v", capabilities)
	}

	if got := stringSliceFromPricingRaw([]string{" chat ", ""}); len(got) != 1 || got[0] != "chat" {
		t.Fatalf("string slice endpoints = %#v", got)
	}
	if got := stringSliceFromPricingRaw(123); got != nil {
		t.Fatalf("unsupported endpoint raw = %#v, want nil", got)
	}
}

func TestUsageAndNullableRefreshHelpers(t *testing.T) {
	t.Parallel()

	if !usageHasQuotaData(map[string]any{"five_hour": map[string]any{}}) {
		t.Fatal("expected five_hour quota data to be detected")
	}
	if !usageHasQuotaData(map[string]any{"data": map[string]any{"total_used": float64(3)}}) {
		t.Fatal("expected total_used quota data to be detected")
	}
	if usageHasQuotaData(map[string]any{"data": map[string]any{"object": "empty"}}) {
		t.Fatal("expected empty data payload not to count as quota data")
	}
	if usageHasQuotaData(map[string]any{"data": []any{"not a quota map"}}) {
		t.Fatal("expected non-map data payload not to count as quota data")
	}
	if !usageHasQuotaData(map[string]any{"weekly": map[string]any{}}) {
		t.Fatal("expected weekly quota data to be detected")
	}
	if !usageHasQuotaData(map[string]any{"data": map[string]any{"total_granted": float64(10)}}) {
		t.Fatal("expected total_granted quota data to be detected")
	}
	if !usageHasQuotaData(map[string]any{"data": map[string]any{"total_available": float64(7)}}) {
		t.Fatal("expected total_available quota data to be detected")
	}
	if usageHasQuotaData("not a map") {
		t.Fatal("non-map usage should not count as quota data")
	}

	usage := usageFromTokenRaw(map[string]any{
		"name":                 "token-a",
		"remain_quota":         json.Number("8"),
		"used_quota":           int64(2),
		"unlimited_quota":      false,
		"model_limits":         []any{"gpt-5"},
		"model_limits_enabled": true,
		"expired_time":         "2026-07-01",
	})
	data, _ := usage["data"].(map[string]any)
	if data["total_granted"] != int64(10) || data["total_available"] != int64(8) {
		t.Fatalf("unexpected token usage data: %#v", data)
	}

	if got := string(modelLimitsJSON(" gpt-4, ,gpt-5 ")); got != `["gpt-4","gpt-5"]` {
		t.Fatalf("model limits json = %s", got)
	}
	if got := string(modelLimitsJSON([]any{"gpt-4", "gpt-5"})); got != `["gpt-4","gpt-5"]` {
		t.Fatalf("slice model limits json = %s", got)
	}
	if got := string(modelLimitsJSON(123)); got != `[]` {
		t.Fatalf("unsupported model limits json = %s", got)
	}

	if got := syncedPricingSource("newapi", nil); got != "newapi" {
		t.Fatalf("newapi pricing source = %q", got)
	}
	if got := syncedPricingSource("openai", map[string]any{"source": " upstream "}); got != "upstream" {
		t.Fatalf("raw pricing source = %q", got)
	}
	if got := syncedPricingSource("openai", nil); got != "unknown" {
		t.Fatalf("default pricing source = %q", got)
	}

	if got := nullableFloat64(1.5, true); got != 1.5 {
		t.Fatalf("nullable float = %#v", got)
	}
	if got := nullableFloat64(1.5, false); got != nil {
		t.Fatalf("missing nullable float = %#v", got)
	}
	if got := nullableInt(0); got != nil {
		t.Fatalf("zero nullable int = %#v", got)
	}
	if got := nullableInt(7); got != 7 {
		t.Fatalf("nullable int = %#v", got)
	}
	if got := nullableInt64(9, true); got != int64(9) {
		t.Fatalf("nullable int64 = %#v", got)
	}
	if got := nullableInt64(9, false); got != nil {
		t.Fatalf("missing nullable int64 = %#v", got)
	}
	if got := nullableString(" value "); got != "value" {
		t.Fatalf("nullable string = %#v", got)
	}
	if got := nullableString(" "); got != nil {
		t.Fatalf("blank nullable string = %#v", got)
	}
	if got := defaultString(" ", "fallback"); got != "fallback" {
		t.Fatalf("default string = %q", got)
	}
	if !boolFromAny(true) || boolFromAny("true") {
		t.Fatal("unexpected boolFromAny behavior")
	}
	if got, ok := int64FromAny(json.Number("12")); !ok || got != 12 {
		t.Fatalf("json number int64 = %d, %v", got, ok)
	}
	if got, ok := int64FromAny(12); !ok || got != 12 {
		t.Fatalf("int int64 = %d, %v", got, ok)
	}
	if got, ok := int64FromAny(float64(12)); !ok || got != 12 {
		t.Fatalf("float int64 = %d, %v", got, ok)
	}
	if _, ok := int64FromAny(json.Number("12.5")); ok {
		t.Fatal("fractional json number should not convert")
	}
	if _, ok := int64FromAny("12"); ok {
		t.Fatal("string should not convert")
	}
	if jsonBytes(nil) != nil {
		t.Fatal("nil jsonBytes should stay nil")
	}
	if jsonBytes(func() {}) != nil {
		t.Fatal("unmarshalable jsonBytes input should return nil")
	}
}

func TestAvailableSiteModelsFiltersUnavailableOnly(t *testing.T) {
	t.Parallel()

	activeID := uuid.New()
	disabledID := uuid.New()
	got := availableSiteModels([]store.SiteModel{
		{ID: activeID, Status: "active", UpstreamName: "active"},
		{ID: uuid.New(), Status: "unavailable", UpstreamName: "gone"},
		{ID: disabledID, Status: "disabled", UpstreamName: "disabled"},
	})
	if len(got) != 2 || got[0].ID != activeID || got[1].ID != disabledID {
		t.Fatalf("available models = %#v", got)
	}
}

func TestOAuthConnectionEnableBlockedReasonDetectsReconnectRequired(t *testing.T) {
	t.Parallel()

	reason := oauthConnectionEnableBlockedReason(store.OAuthConnection{Status: "reconnect_required"})
	if reason == "" {
		t.Fatal("expected reconnect_required connection to block enabling")
	}

	reason = oauthConnectionEnableBlockedReason(store.OAuthConnection{
		Provider: "codex",
		Status:   "connected",
		Metadata: siteJSONMeta(t, map[string]any{
			"last_error": `codex token refresh returned 401: {"error":"invalid_grant"}`,
		}),
	})
	if reason == "" {
		t.Fatal("expected permanent oauth auth error to block enabling")
	}

	reason = oauthConnectionEnableBlockedReason(store.OAuthConnection{
		Provider: "codex",
		Status:   "connected",
		Metadata: siteJSONMeta(t, map[string]any{
			"quota": map[string]any{
				"available": false,
				"error":     `codex upstream returned 401: token_invalidated`,
			},
		}),
	})
	if reason == "" {
		t.Fatal("expected quota auth error to block enabling")
	}
}

func TestOAuthConnectionEnableBlockedReasonAllowsConnectedTransient403(t *testing.T) {
	t.Parallel()

	reason := oauthConnectionEnableBlockedReason(store.OAuthConnection{
		Provider: "codex",
		Status:   "connected",
		Metadata: siteJSONMeta(t, map[string]any{
			"last_error": `codex upstream returned 403: <html><head><meta http-equiv="refresh" content="360"></head></html>`,
		}),
	})
	if reason != "" {
		t.Fatalf("expected transient codex 403 refresh challenge to remain enableable, got %q", reason)
	}
}

func TestShouldSkipBulkRefreshSkipsManuallyDisabledSites(t *testing.T) {
	t.Parallel()

	service := Service{}
	skip, reason := service.shouldSkipBulkRefresh(t.Context(), store.Site{Enabled: false, SiteType: "openai_compatible"})
	if !skip {
		t.Fatal("expected manually disabled site to be skipped")
	}
	if reason == "" {
		t.Fatal("expected skip reason")
	}
}

func TestShouldSkipBulkRefreshAllowsAutoDisabledSites(t *testing.T) {
	t.Parallel()

	service := Service{}
	site := store.Site{
		Enabled:  false,
		SiteType: "openai_compatible",
		Meta:     siteJSONMeta(t, map[string]any{siteMetaAutoDisabledByRefresh: true}),
	}
	skip, reason := service.shouldSkipBulkRefresh(t.Context(), site)
	if skip {
		t.Fatalf("expected auto-disabled site to be eligible for recovery refresh, reason=%q", reason)
	}
}

func TestManualCredentialEnabledPrefersSyncedStateOverMeta(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	credential := store.SiteCredential{
		ID:   credentialID,
		Meta: siteJSONMeta(t, map[string]any{"enabled": true}),
	}

	if !manualCredentialEnabled(credential, store.SiteAPIKeyState{}) {
		t.Fatal("credential meta enabled=true should default to enabled")
	}
	if manualCredentialEnabled(credential, store.SiteAPIKeyState{SiteCredentialID: credentialID, Enabled: false}) {
		t.Fatal("synced disabled state should override enabled meta")
	}

	credential.Meta = siteJSONMeta(t, map[string]any{"enabled": false})
	if manualCredentialEnabled(credential, store.SiteAPIKeyState{}) {
		t.Fatal("credential meta enabled=false should disable credential")
	}
	if !manualCredentialEnabled(credential, store.SiteAPIKeyState{SiteCredentialID: credentialID, Enabled: true}) {
		t.Fatal("synced enabled state should override disabled meta")
	}
}

func TestEnrichModelCapabilitiesUsesStandardEndpointTypesForOpenAISiteClaudeModel(t *testing.T) {
	t.Parallel()

	service := Service{
		modelCaps: modelcapabilities.NewWithConfig(modelcapabilities.Config{
			HTTPClient: &http.Client{
				Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{}`)),
						Header:     make(http.Header),
					}, nil
				}),
			},
		}),
	}

	model := service.enrichModelCapabilities(t.Context(), store.Site{SiteType: "openai"}, adapter.Model{
		UpstreamName: "claude-3-5-sonnet",
		DisplayName:  "claude-3-5-sonnet",
		Capabilities: map[string]any{
			"source":                   "upstream",
			"supported_endpoint_types": []string{"openai"},
		},
	})

	endpoints, _ := model.Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 1 || endpoints[0] != "anthropic-messages" {
		t.Fatalf("unexpected supported_endpoint_types: %#v", model.Capabilities["supported_endpoint_types"])
	}
	if got, _ := model.Capabilities["supports_anthropic_messages"].(bool); !got {
		t.Fatalf("expected supports_anthropic_messages=true, got %#v", model.Capabilities)
	}
	if got, _ := model.Capabilities["supports_chat_completions"].(bool); got {
		t.Fatalf("expected supports_chat_completions=false, got %#v", model.Capabilities)
	}
}

func TestOpenCodeGoSpecEndpointTypesRemainAuthoritativeDuringSiteEnrichment(t *testing.T) {
	t.Parallel()

	model := (&Service{}).enrichModelCapabilities(t.Context(), store.Site{SiteType: "opencode_go"}, adapter.Model{
		UpstreamName: "gpt-5.6-luna",
		DisplayName:  "gpt-5.6-luna",
		Capabilities: map[string]any{
			"source":                   "opencode_go_spec",
			"supported_endpoint_types": []string{"openai-response"},
		},
	})

	endpoints, _ := model.Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 1 || endpoints[0] != "openai-response" {
		t.Fatalf("OpenCode Go endpoints = %#v, want Spec value", model.Capabilities["supported_endpoint_types"])
	}
}

func TestOpenCodeGoSiteModelCapabilitiesPreserveSpecMapping(t *testing.T) {
	t.Parallel()

	encoded, err := openCodeGoSiteModelCapabilities(store.JSON(`{"source":"upstream","supported_endpoint_types":["openai"]}`), "minimax-m3")
	if err != nil {
		t.Fatalf("openCodeGoSiteModelCapabilities returned error: %v", err)
	}
	capabilities := map[string]any{}
	if err := json.Unmarshal(encoded, &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	endpoints := stringSliceFromPricingRaw(capabilities["supported_endpoint_types"])
	if len(endpoints) != 1 || endpoints[0] != "anthropic-messages" {
		t.Fatalf("stored endpoint types = %#v", capabilities["supported_endpoint_types"])
	}
	if capabilities["source"] != "opencode_go_spec" || capabilities["protocol_spec_version"] != float64(1) || capabilities["protocol_mapping_status"] != "mapped" {
		t.Fatalf("stored capabilities = %#v", capabilities)
	}
}

func TestOpenCodeGoSiteModelCapabilitiesFallbackToChatCompletions(t *testing.T) {
	t.Parallel()

	encoded, err := openCodeGoSiteModelCapabilities(store.JSON(`{"source":"upstream","supported_endpoint_types":["anthropic-messages"]}`), "longcat-2.0-free")
	if err != nil {
		t.Fatalf("openCodeGoSiteModelCapabilities returned error: %v", err)
	}
	capabilities := map[string]any{}
	if err := json.Unmarshal(encoded, &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	endpoints := stringSliceFromPricingRaw(capabilities["supported_endpoint_types"])
	if len(endpoints) != 1 || endpoints[0] != "openai" {
		t.Fatalf("stored endpoint types = %#v", capabilities["supported_endpoint_types"])
	}
	if capabilities["source"] != "opencode_go_spec" || capabilities["protocol_mapping_status"] != "fallback" {
		t.Fatalf("stored capabilities = %#v", capabilities)
	}
}

type fakeAPIKeySummaryFetcher struct {
	summary    adapter.APIKeySummary
	err        error
	seenSite   adapter.SiteConfig
	seenAPIKey string
}

func (f *fakeAPIKeySummaryFetcher) SummarizeAPIKey(_ context.Context, site adapter.SiteConfig, apiKey string) (adapter.APIKeySummary, error) {
	f.seenSite = site
	f.seenAPIKey = apiKey
	return f.summary, f.err
}
