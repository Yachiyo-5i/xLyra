package gateway

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/ratelimit"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestGatewayCooldownEarlyReturnsAndNilDBThreshold(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.New()},
		Model: routeengine.CandidateModel{SiteModelID: uuid.New(), UpstreamName: "upstream-model"},
	}
	handler := Handler{logger: slog.Default(), modelsCache: newModelsCache()}

	handler.cooldownAfterFailure(ctx, candidate, gatewayAttemptResult{success: true})
	handler.cooldownAfterFailure(ctx, candidate, gatewayAttemptResult{statusCode: http.StatusBadRequest})

	if !handler.upstreamNoResponseCooldownThresholdReached(ctx, candidate) {
		t.Fatal("nil db should allow no-response cooldown instead of blocking the failure path")
	}
}

func TestGatewayCooldownInputForNoResponseAndCredentialFailures(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.New(), SiteType: "codex"},
		Model: routeengine.CandidateModel{SiteModelID: uuid.New(), UpstreamName: "gpt-5-codex"},
	}

	noResponse, ok := cooldownInputForFailure(candidate, gatewayAttemptResult{
		statusCode:        http.StatusBadGateway,
		errorType:         "upstream_transport_error",
		errorMessage:      "dial tcp timeout",
		credentialAttempt: 2,
		credentialTotal:   3,
	})
	if !ok {
		t.Fatal("expected no-response transport failure to produce a cooldown input")
	}
	if noResponse.Scope != "model" || noResponse.Reason != "upstream_no_response" || noResponse.Duration != upstreamNoResponseCooldownDuration {
		t.Fatalf("unexpected no-response cooldown input: %#v", noResponse)
	}
	if noResponse.SiteModelID == nil || *noResponse.SiteModelID != candidate.Model.SiteModelID {
		t.Fatalf("no-response SiteModelID = %#v, want candidate model", noResponse.SiteModelID)
	}
	if noResponse.Metadata["no_upstream_response"] != true || noResponse.Metadata["credential_attempt"] != 2 {
		t.Fatalf("unexpected no-response metadata: %#v", noResponse.Metadata)
	}

	credential, ok := cooldownInputForFailure(candidate, gatewayAttemptResult{
		statusCode:        http.StatusTooManyRequests,
		credentialID:      credentialID,
		retryAfterSeconds: 17,
	})
	if !ok {
		t.Fatal("expected credential rate limit to produce a cooldown input")
	}
	if credential.Scope != "credential" || credential.Reason != "upstream_credential_rate_limited" || credential.Duration != 17*time.Second {
		t.Fatalf("unexpected credential cooldown input: %#v", credential)
	}
	if credential.SiteCredentialID == nil || *credential.SiteCredentialID != credentialID {
		t.Fatalf("credential SiteCredentialID = %#v, want %s", credential.SiteCredentialID, credentialID)
	}
	if credential.Metadata["retry_after_seconds"] != int64(17) {
		t.Fatalf("retry-after metadata = %#v, want 17", credential.Metadata["retry_after_seconds"])
	}
}

func TestGatewayModelsCachePrewarmGuardsAndRemainingHelpers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	apiKey := store.APIKey{ID: uuid.New(), Status: "active", QuotaUnlimited: true}
	Handler{}.PrewarmModelsCache(ctx)
	Handler{}.PrewarmModelsCacheForAPIKey(ctx, apiKey)
	Handler{modelsCache: newModelsCache()}.PrewarmModelsCacheForAPIKey(ctx, apiKey)

	if got := gatewayJSONBoolDefault(store.JSON([]byte(`{"enabled":"false","other":1}`)), "enabled", true); got {
		t.Fatal("string false metadata should override fallback")
	}
	if got := gatewayJSONBoolDefault(store.JSON([]byte(`{`)), "enabled", false); got {
		t.Fatal("invalid JSON should return fallback false")
	}

	credentialID := uuid.New()
	siteID := uuid.New()
	modelID := uuid.New()
	cooldowns := []store.RouteCooldown{
		{SiteID: siteID, SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true}},
		{SiteID: siteID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}},
		{SiteID: uuid.New()},
	}
	siteCooldowns, modelCooldowns := gatewayCooldownSets(cooldowns)
	if _, ok := siteCooldowns[cooldowns[2].SiteID]; !ok {
		t.Fatalf("site cooldowns = %#v, want site-wide cooldown included", siteCooldowns)
	}
	if _, ok := modelCooldowns[modelID]; !ok {
		t.Fatalf("model cooldowns = %#v, want model cooldown included", modelCooldowns)
	}
	if gatewayCredentialCooling(cooldowns, siteID, uuid.Nil, credentialID) != true {
		t.Fatal("site credential cooldown should apply when no model id is supplied")
	}
	if gatewayCredentialCooling(cooldowns, siteID, modelID, credentialID) != true {
		t.Fatal("site credential cooldown should apply to a matching model")
	}
	if gatewayCredentialCooling(cooldowns, siteID, uuid.New(), credentialID) != true {
		t.Fatal("site-wide credential cooldown should apply to any model")
	}
	if gatewayCredentialCooling(cooldowns, uuid.New(), modelID, credentialID) {
		t.Fatal("credential cooldown should not apply to another site")
	}

	if gatewayCredentialUsable(store.SiteCredential{Meta: store.JSON([]byte(`{"raw_key_missing":true}`))}) {
		t.Fatal("credential with raw_key_missing metadata should be unusable")
	}
	if gatewayCredentialStateUsable(store.SiteAPIKeyState{
		SiteCredentialID: uuid.New(),
		Enabled:          true,
		SyncStatus:       "failed",
		SyncMessage:      sql.NullString{String: "oauth invalid_grant", Valid: true},
	}) {
		t.Fatal("permanent auth failure state should be unusable")
	}
}

func TestGatewayNoRouteRetryAfterDefaultsAndCacheClamps(t *testing.T) {
	t.Parallel()

	if got := (Handler{}).noRouteRetryAfter(context.Background(), routeAccessForNoRoute{}, time.Unix(100, 0)); got != noRouteSuppressionDefaultTTL {
		t.Fatalf("nil router retry-after = %s, want default %s", got, noRouteSuppressionDefaultTTL)
	}

	now := time.Unix(200, 0)
	key := noRouteSuppressionKey{
		APIKeyID: uuid.New(),
		Endpoint: " " + gatewayEndpointResponses + " ",
		ModelKey: " gpt-5 ",
		Code:     " no_route_candidates ",
	}
	cache := newNoRouteSuppressionCache()
	cache.remember(key, 5*time.Minute, 99, now)
	if retryAfter, ok := cache.retryAfter(key, now.Add(59*time.Second)); !ok || retryAfter != 99 {
		t.Fatalf("max-clamped cache hit = retryAfter %d ok %v, want 99 true", retryAfter, ok)
	}
	if retryAfter, ok := cache.retryAfter(key, now.Add(61*time.Second)); ok || retryAfter != 0 {
		t.Fatalf("cache should expire after max ttl, retryAfter %d ok %v", retryAfter, ok)
	}
	if joinSortedUUIDs(nil) != "" {
		t.Fatal("nil UUID list should join to an empty key segment")
	}
}

func TestGatewayOpenAIResponsesBuildPayloadCanonicalAndConvertedBranches(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "upstream-gpt"},
	}
	directPayload := map[string]any{"model": "alias", "input": "hello", "temperature": 0.3}
	direct, err := (openAIResponsesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}).BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        directPayload,
		Canonical:      &canonicalRequest{},
	}, candidate)
	if err != nil {
		t.Fatalf("direct responses BuildUpstreamPayload returned error: %v", err)
	}
	if direct["model"] != "upstream-gpt" || direct["temperature"] != 0.3 {
		t.Fatalf("direct responses payload = %#v", direct)
	}
	if directPayload["model"] != "alias" {
		t.Fatalf("BuildUpstreamPayload mutated caller payload: %#v", directPayload)
	}

	converted, err := (openAIResponsesProtocolAdapter{}).BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Payload: map[string]any{
			"model":    "alias",
			"messages": []any{map[string]any{"role": "user", "content": "hello"}},
			"stream":   true,
		},
	}, candidate)
	if err != nil {
		t.Fatalf("converted responses BuildUpstreamPayload returned error: %v", err)
	}
	if converted["model"] != "upstream-gpt" || converted["stream"] != true {
		t.Fatalf("converted responses payload = %#v", converted)
	}
	if input, ok := converted["input"].([]any); !ok || len(input) != 1 {
		t.Fatalf("converted responses input = %#v, want one input item", converted["input"])
	}
}

func TestGatewayAntigravityProtocolNamesPathsAndMissingProject(t *testing.T) {
	t.Parallel()

	if got := (antigravityProtocolAdapter{stream: true}).ProtocolName(); got != "antigravity_stream_generate_content" {
		t.Fatalf("stream protocol name = %q", got)
	}
	if got := (antigravityProtocolAdapter{downstreamImages: true}).ProtocolName(); got != "antigravity_image_generate_content" {
		t.Fatalf("image protocol name = %q", got)
	}
	if got := (antigravityProtocolAdapter{stream: true}).UpstreamPath("https://example.test/v1internal"); got != "https://example.test/v1internal:streamGenerateContent?alt=sse" {
		t.Fatalf("stream v1internal path = %q", got)
	}
	if got := (antigravityProtocolAdapter{}).UpstreamPath(" https://example.test/root/ "); got != "https://example.test/root/v1internal:generateContent" {
		t.Fatalf("buffered path = %q", got)
	}

	_, err := (antigravityProtocolAdapter{downstreamImages: true}).BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointImagesGenerations,
		Payload:        map[string]any{"model": "alias", "prompt": "draw"},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.New()},
		Model: routeengine.CandidateModel{UpstreamName: "gemini-3-pro-image"},
	})
	if err == nil {
		t.Fatal("expected missing antigravity project_id error for image payload without OAuth metadata")
	}
}

func TestGatewayRateLimitAcquireAndSettleNilBackends(t *testing.T) {
	t.Parallel()

	reservation, limitErr, err := (Handler{}).acquireRateLimit(
		context.Background(),
		uuid.New(),
		testGatewayEndpointStub{path: gatewayEndpointResponses, routeEndpointType: "openai-response"},
		gatewayRequest{Payload: map[string]any{"input": "hello"}},
		time.Unix(300, 0),
	)
	if err != nil || limitErr != nil || reservation != nil {
		t.Fatalf("nil service acquire = reservation %v limit %v err %v, want all nil", reservation, limitErr, err)
	}

	handler := Handler{logger: slog.Default(), rateLimits: ratelimit.NewService(nil)}
	reservation, limitErr, err = handler.acquireRateLimit(
		context.Background(),
		uuid.New(),
		testGatewayEndpointStub{path: gatewayEndpointResponses, routeEndpointType: "openai-response"},
		gatewayRequest{Payload: map[string]any{"input": "hello"}},
		time.Unix(300, 0),
	)
	if err != nil || limitErr != nil || reservation != nil {
		t.Fatalf("nil-db service acquire = reservation %v limit %v err %v, want all nil", reservation, limitErr, err)
	}
	handler.settleRateLimit(context.Background(), nil, 12)
	handler.settleRateLimit(context.Background(), &ratelimit.Reservation{}, 12)
}
func TestGatewayUUIDSetFiltersNilAndDeduplicates(t *testing.T) {
	t.Parallel()

	first := uuid.New()
	second := uuid.New()
	got := gatewayUUIDSet([]uuid.UUID{uuid.Nil, first, second, first})

	if len(got) != 2 {
		t.Fatalf("set size = %d, want 2: %#v", len(got), got)
	}
	if _, ok := got[first]; !ok {
		t.Fatalf("first UUID missing from set: %#v", got)
	}
	if _, ok := got[second]; !ok {
		t.Fatalf("second UUID missing from set: %#v", got)
	}
	if _, ok := got[uuid.Nil]; ok {
		t.Fatalf("nil UUID should be filtered out: %#v", got)
	}
}

func TestGatewayCredentialMetaStringTrimsStringValuesOnly(t *testing.T) {
	t.Parallel()

	credential := store.SiteCredential{
		Meta: store.JSON(`{"account_id":"  acct-123  ","numeric":123}`),
	}
	if got := gatewayCredentialMetaString(credential, "account_id"); got != "acct-123" {
		t.Fatalf("account_id = %q, want acct-123", got)
	}
	if got := gatewayCredentialMetaString(credential, "numeric"); got != "" {
		t.Fatalf("numeric metadata = %q, want empty string", got)
	}
	if got := gatewayCredentialMetaString(store.SiteCredential{Meta: store.JSON(`{invalid`)}, "account_id"); got != "" {
		t.Fatalf("invalid JSON metadata = %q, want empty string", got)
	}
}

func TestPricingMetadataIncludesDerivedAndNilValues(t *testing.T) {
	t.Parallel()

	inputValue := 2.5
	cacheRatio := 0.2
	groupRatio := 1.4
	quotaType := int64(1)

	meta := pricingMetadata(selectedPricing{
		GroupName:  "premium",
		Currency:   "USD",
		InputValue: &inputValue,
		CacheRatio: &cacheRatio,
		GroupRatio: &groupRatio,
		QuotaType:  &quotaType,
	})

	if meta["group_name"] != "premium" || meta["currency"] != "USD" {
		t.Fatalf("unexpected string metadata: %#v", meta)
	}
	if meta["input_value"] != inputValue || meta["cache_ratio"] != cacheRatio || meta["cache_input_value"] != 0.5 {
		t.Fatalf("unexpected input/cache metadata: %#v", meta)
	}
	if meta["group_ratio"] != groupRatio || meta["quota_type"] != quotaType {
		t.Fatalf("unexpected ratio/quota metadata: %#v", meta)
	}
	if meta["output_value"] != nil || meta["billing_type"] != nil || meta["per_request_value"] != nil {
		t.Fatalf("unset pricing fields should be nil in metadata: %#v", meta)
	}
}

func TestCostCalculationMetadataIncludesBillingAdjustment(t *testing.T) {
	t.Parallel()

	inputValue := 1.0
	outputValue := 3.0
	cacheRatio := 0.5
	total := 0.0065
	baseTotal := 0.0026

	meta := costCalculationMetadata(
		gatewayUsage{
			PromptTokens:       4_000,
			CachedPromptTokens: 1_000,
			CompletionTokens:   1_000,
		},
		selectedPricing{
			Currency:    "USD",
			InputValue:  &inputValue,
			OutputValue: &outputValue,
			CacheRatio:  &cacheRatio,
		},
		&total,
		billingAdjustment{
			ServiceTier: "fast",
			Mode:        "fast",
			Multiplier:  2.5,
			Reason:      "codex_fast_mode",
		},
		&baseTotal,
	)

	if meta["formula"] == "" || meta["prompt_tokens"] != 3_000 || meta["cache_tokens"] != 1_000 {
		t.Fatalf("unexpected token formula metadata: %#v", meta)
	}
	if meta["cache_input_value"] != 0.5 || meta["prompt_cost"] != 0.003 || meta["completion_cost"] != 0.003 || meta["cache_cost"] != 0.0005 {
		t.Fatalf("unexpected cost breakdown metadata: %#v", meta)
	}
	if meta["service_tier"] != "fast" || meta["billing_mode"] != "fast" || meta["cost_multiplier"] != 2.5 {
		t.Fatalf("missing billing adjustment metadata: %#v", meta)
	}
	if meta["base_estimated_cost"] != baseTotal || meta["cost_multiplier_reason"] != "codex_fast_mode" {
		t.Fatalf("unexpected billing adjustment detail: %#v", meta)
	}
}

func TestCooldownInputForFailureMarksNoResponseAsModelCooldown(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	input, ok := cooldownInputForFailure(routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: siteID},
		Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "gpt-5.4"},
	}, gatewayAttemptResult{
		statusCode:         http.StatusBadGateway,
		errorType:          "upstream_transport_error",
		errorMessage:       "connection reset",
		credentialMasked:   "sk-...123",
		credentialAttempt:  2,
		credentialTotal:    3,
		retryAfterSeconds:  9,
		upstreamStatusCode: 0,
	})

	if !ok {
		t.Fatal("expected no-response failure to produce cooldown input")
	}
	if input.SiteID != siteID || input.SiteModelID == nil || *input.SiteModelID != siteModelID || input.Scope != "model" {
		t.Fatalf("unexpected cooldown target: %#v", input)
	}
	if input.Reason != "upstream_no_response" || input.Duration != upstreamNoResponseCooldownDuration {
		t.Fatalf("unexpected no-response cooldown policy: %#v", input)
	}
	if input.Metadata["no_upstream_response"] != true || input.Metadata["retry_after_seconds"] != int64(9) {
		t.Fatalf("missing no-response metadata: %#v", input.Metadata)
	}
	if input.Metadata["credential_masked"] != "sk-...123" || input.Metadata["credential_attempt"] != 2 || input.Metadata["credential_total"] != 3 {
		t.Fatalf("missing credential attempt metadata: %#v", input.Metadata)
	}
}

func TestUpstreamNoResponseFailureRequiresNoStartedResponse(t *testing.T) {
	t.Parallel()

	base := gatewayAttemptResult{
		statusCode: http.StatusBadGateway,
		errorType:  "upstream_timeout",
	}
	if !upstreamNoResponseFailure(base) {
		t.Fatal("timeout without upstream response should count as no-response failure")
	}

	for _, result := range []gatewayAttemptResult{
		{statusCode: http.StatusBadGateway, errorType: "upstream_timeout", success: true},
		{statusCode: http.StatusBadGateway, errorType: "upstream_timeout", upstreamStatusCode: http.StatusBadGateway},
		{statusCode: http.StatusBadGateway, errorType: "upstream_timeout", responseStarted: true},
		{statusCode: http.StatusBadRequest, errorType: "upstream_timeout"},
		{statusCode: http.StatusBadGateway, errorType: "upstream_auth_failed"},
	} {
		if upstreamNoResponseFailure(result) {
			t.Fatalf("result should not count as no-response failure: %#v", result)
		}
	}
}

func TestRateLimitMetadataContextStoresOnlyNonNilMetadata(t *testing.T) {
	t.Parallel()

	base := context.Background()
	if got := withRateLimitMetadata(base, nil); got != base {
		t.Fatal("nil metadata should return original context")
	}
	if _, ok := rateLimitMetadataFromContext(base); ok {
		t.Fatal("base context should not contain rate limit metadata")
	}

	metadata := map[string]any{"limit_type": "tpm", "retry_after_seconds": int64(12)}
	ctx := withRateLimitMetadata(base, metadata)
	got, ok := rateLimitMetadataFromContext(ctx)
	if !ok {
		t.Fatal("expected rate limit metadata in context")
	}
	if got["limit_type"] != "tpm" || got["retry_after_seconds"] != int64(12) {
		t.Fatalf("unexpected rate limit metadata: %#v", got)
	}
}

func TestModelCapabilityTrimsNamesAndRejectsMissingCapabilities(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-4.1"},
	}
	if !modelCapability(canonicalProtocolOpenAIResponses, candidate, " text_format ") {
		t.Fatal("expected trimmed text_format capability to be enabled")
	}
	if modelCapability(canonicalProtocolOpenAIResponses, candidate, "") {
		t.Fatal("empty capability name should be disabled")
	}
	if modelCapability(canonicalProtocolOpenAIResponses, candidate, "missing_capability") {
		t.Fatal("unknown capability should be disabled")
	}
}

func TestCooldownConstantsDocumentNoResponseThresholdPolicy(t *testing.T) {
	t.Parallel()

	if upstreamNoResponseCooldownThreshold != 3 {
		t.Fatalf("upstreamNoResponseCooldownThreshold = %d, want 3", upstreamNoResponseCooldownThreshold)
	}
	if upstreamNoResponseCooldownWindow != 10*time.Minute {
		t.Fatalf("upstreamNoResponseCooldownWindow = %s, want 10m", upstreamNoResponseCooldownWindow)
	}
	if upstreamNoResponseCooldownAttemptLimit != 20 {
		t.Fatalf("upstreamNoResponseCooldownAttemptLimit = %d, want 20", upstreamNoResponseCooldownAttemptLimit)
	}
}
