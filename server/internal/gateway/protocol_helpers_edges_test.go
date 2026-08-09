package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestGatewayModelsCacheRefreshAsyncStoresFreshClone(t *testing.T) {
	cache := newModelsCache()
	apiKey := store.APIKey{ID: uuid.New()}
	buildStarted := make(chan struct{})
	releaseBuild := make(chan struct{})
	buildCalls := 0

	build := func(ctx context.Context, got store.APIKey) (map[string]any, error) {
		if got.ID != apiKey.ID {
			t.Fatalf("api key ID = %s, want %s", got.ID, apiKey.ID)
		}
		buildCalls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("refreshAsync build context has no timeout deadline")
		}
		close(buildStarted)
		<-releaseBuild
		return map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "model-a"}},
		}, nil
	}

	cache.refreshAsync(apiKey, build)
	<-buildStarted
	cache.refreshAsync(apiKey, build)
	close(releaseBuild)
	waitForGatewayModelsCacheEntry(t, cache, apiKey.ID)

	if buildCalls != 1 {
		t.Fatalf("build calls = %d, want 1 while inflight", buildCalls)
	}
	got, ok := cache.get(apiKey.ID)
	if !ok {
		t.Fatal("expected refreshed payload to be cached")
	}
	rows := got["data"].([]map[string]any)
	rows[0]["id"] = "mutated"
	again, ok := cache.get(apiKey.ID)
	if !ok {
		t.Fatal("expected cached payload to survive caller mutation")
	}
	if again["data"].([]map[string]any)[0]["id"] != "model-a" {
		t.Fatalf("cached payload was not cloned: %#v", again)
	}
}

func TestGatewayBuildModelsPayloadHelpersFilterCredentialsAndClonePayloads(t *testing.T) {
	credentialID := uuid.New()
	siteID := uuid.New()
	modelID := uuid.New()

	cooldowns := []store.RouteCooldown{
		{
			SiteID: siteID,
			SiteCredentialID: uuid.NullUUID{
				UUID:  credentialID,
				Valid: true,
			},
		},
	}
	credentials := []store.SiteCredential{
		{ID: credentialID, SiteID: siteID, CredentialType: "api_key", Meta: store.JSON([]byte(`{"enabled":true}`))},
		{ID: uuid.New(), SiteID: siteID, CredentialType: "api_key:oauth", Meta: store.JSON([]byte(`{"raw_key_missing":false}`))},
		{ID: uuid.New(), SiteID: siteID, CredentialType: "oauth"},
	}
	counts := gatewaySiteCredentialCounts(credentials, map[uuid.UUID]store.SiteAPIKeyState{}, cooldowns)
	if counts[siteID] != 1 {
		t.Fatalf("site credential count = %d, want only non-cooling api key", counts[siteID])
	}

	modelCounts := gatewayModelCredentialCounts([]store.SiteAPIKeyModel{
		{SiteID: siteID, SiteCredentialID: credentials[1].ID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
		{SiteID: siteID, SiteCredentialID: credentials[1].ID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
	}, credentials, map[uuid.UUID]store.SiteAPIKeyState{}, nil)
	if modelCounts[modelID] != 1 {
		t.Fatalf("model credential count = %d, want deduplicated credential", modelCounts[modelID])
	}

	payload := canonicalModelPayload(store.CanonicalModel{ID: uuid.New(), ModelKey: "gpt-test", Status: "active", CreatedAt: time.Unix(123, 0)})
	if payload["owned_by"] != "xlyra" {
		t.Fatalf("owned_by = %#v, want xlyra fallback", payload["owned_by"])
	}
	empty := emptyModelsPayload()
	if empty["object"] != "list" || len(empty["data"].([]map[string]any)) != 0 {
		t.Fatalf("empty payload = %#v", empty)
	}
}

func TestGatewayNoRouteRetryAfterFromCooldownsSkipsIneligibleEntries(t *testing.T) {
	now := time.Unix(1000, 0)
	siteID := uuid.New()
	modelID := uuid.New()
	otherSiteID := uuid.New()

	got := noRouteRetryAfterFromCooldowns([]store.RouteCooldown{
		{SiteID: siteID, Source: "site_health", ActiveUntil: now.Add(time.Second)},
		{SiteID: otherSiteID, ActiveUntil: now.Add(5 * time.Second)},
		{SiteID: siteID, SiteCredentialID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, ActiveUntil: now.Add(2 * time.Second)},
		{SiteID: siteID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, ActiveUntil: now.Add(7 * time.Second)},
		{SiteID: siteID, ActiveUntil: now.Add(9 * time.Second)},
	}, routeAccessForNoRoute{
		AllowedSiteIDs:      []uuid.UUID{siteID},
		AllowedSiteModelIDs: []uuid.UUID{modelID},
	}, now)
	if got != 7*time.Second {
		t.Fatalf("retry after = %s, want nearest eligible model cooldown", got)
	}
}

func TestGatewayOAuthAuthFailureMessageAndPredicates(t *testing.T) {
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: " antigravity "},
		Model: routeengine.CandidateModel{UpstreamName: "gemini-pro"},
	}
	result := gatewayAttemptResult{
		statusCode:         http.StatusForbidden,
		upstreamStatusCode: http.StatusUnauthorized,
		upstreamPath:       " /v1internal:generateContent ",
		errorMessage:       " token expired ",
	}
	if !oauthModelRequestAuthFailure(candidate.Site.SiteType, result) {
		t.Fatal("expected antigravity upstream 401 to be an OAuth auth failure")
	}
	if oauthModelRequestAuthFailure("openai", result) {
		t.Fatal("expected non OAuth site type to be ignored")
	}
	message := oauthModelRequestAuthFailureMessage(candidate, result)
	for _, want := range []string{"antigravity model request returned HTTP 401", "path /v1internal:generateContent", "model gemini-pro", "token expired"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q missing %q", message, want)
		}
	}

	semanticResult := gatewayAttemptResult{
		statusCode:         http.StatusBadGateway,
		upstreamStatusCode: http.StatusOK,
		errorType:          "upstream_credential_invalid",
	}
	if !oauthModelRequestAuthFailure(candidate.Site.SiteType, semanticResult) {
		t.Fatal("expected semantic credential failure to be an OAuth auth failure")
	}

	Handler{}.markOAuthConnectionUnavailableOnAuthFailure(context.Background(), candidate, result)
}

func TestGatewayPricingHelpersCoverPerRequestImagesAndFastMultiplier(t *testing.T) {
	perRequest := 0.25
	if got := estimateCost(gatewayUsage{PromptTokens: 100, CompletionTokens: 100}, selectedPricing{BillingType: "per_request", PerRequestValue: &perRequest}); got == nil || *got != perRequest {
		t.Fatalf("per-request cost = %v, want %v", got, perRequest)
	}

	inputValue := 1.0
	imageRatio := 0.02
	if got := estimateCost(gatewayUsage{ImageCount: 3}, selectedPricing{InputValue: &inputValue, ImageRatio: &imageRatio}); got == nil || *got != 0.06 {
		t.Fatalf("image unit cost = %v, want 0.06", got)
	}

	result := applyEstimatedCostBillingAdjustment(gatewayAttemptResult{
		estimatedCost:      gatewayFloat64Ptr(2),
		billingMode:        "fast",
		costMultiplier:     2.5,
		multiplierReason:   "codex_fast_mode",
		upstreamProtocol:   "openai_responses",
		downstreamPath:     gatewayEndpointResponses,
		stream:             true,
		streamEndReason:    "upstream_stream_parse_failed",
		responseStarted:    true,
		streamCompleted:    false,
		streamReceivedDone: false,
	})
	if result.baseEstimatedCost == nil || *result.baseEstimatedCost != 2 || result.estimatedCost == nil || *result.estimatedCost != 5 {
		t.Fatalf("adjusted result = %+v", result)
	}
}

func TestGatewayAntigravityAndOpenAIResponsesBuildPayloadBranches(t *testing.T) {
	candidate := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "upstream-model"}}
	ag := antigravityProtocolAdapter{}
	if _, err := ag.BuildUpstreamPayload(gatewayRequest{Payload: map[string]any{"messages": []any{}}}, candidate); err == nil {
		t.Fatal("expected missing antigravity project_id error")
	}
	if got := ag.projectID(gatewayRequest{}, routeengine.Candidate{}); got != "" {
		t.Fatalf("projectID without db/site = %q, want empty", got)
	}

	responses := openAIResponsesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}
	payload, err := responses.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        map[string]any{"model": "requested", "temperature": 0.5},
	}, candidate)
	if err != nil {
		t.Fatalf("responses BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "upstream-model" || payload["temperature"] != 0.5 {
		t.Fatalf("responses payload = %#v", payload)
	}
}

func TestGatewayCanonicalChatEncodingCoversFunctionThinkingAndImages(t *testing.T) {
	messages := encodeCanonicalMessagesAsChatMessages([]canonicalMessage{
		{Type: "function_call", ToolCallID: "call_1", Name: "lookup", Arguments: `{"q":"x"}`, Metadata: map[string]any{"xlyra": map[string]any{"origin": "responses"}}},
		{Type: "function_call_output", ToolCallID: "call_1", Output: map[string]any{"ok": true}},
		{Role: "developer", Content: []canonicalContentPart{{Type: "text", Text: "rules"}}},
		{Role: "assistant", Content: []canonicalContentPart{{Type: "text", Text: "answer"}}, Thinking: []canonicalThinkingBlock{{Thinking: "hidden", Signature: "sig"}}},
	})
	if messages[0].(map[string]any)["role"] != "assistant" {
		t.Fatalf("function call message = %#v", messages[0])
	}
	if messages[1].(map[string]any)["role"] != "tool" {
		t.Fatalf("function call output = %#v", messages[1])
	}
	if messages[2].(map[string]any)["role"] != "system" {
		t.Fatalf("developer role = %#v", messages[2])
	}
	assistant := messages[3].(map[string]any)
	if assistant["reasoning_content"] != "hidden" || assistant["thinking_signature"] != "sig" {
		t.Fatalf("assistant thinking fields = %#v", assistant)
	}

	content := encodeCanonicalContentAsChat([]canonicalContentPart{
		{Type: "text", Text: "look"},
		{Type: "input_image", ImageURL: "data:image/png;base64,abc"},
	}).([]any)
	if content[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("encoded image content = %#v", content)
	}
}

func TestGatewayAnthropicStreamEncoderThinkingTextAndTerminalEvents(t *testing.T) {
	rec := httptest.NewRecorder()
	capture := &streamCaptureState{usage: completionUsage{PromptTokens: 4, CompletionTokens: 2}}
	encoder := newAnthropicMessagesStreamEncoder(canonicalStreamOptions{Candidate: routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4.5"}}}, rec, capture)

	if err := encoder.EncodeEvent(canonicalStreamEvent{Type: canonicalStreamEventCreated, ID: "msg_test", Model: "claude", CreatedAt: 123}); err != nil {
		t.Fatalf("created event: %v", err)
	}
	if err := encoder.EncodeEvent(canonicalStreamEvent{Type: canonicalStreamEventReasoningDelta, ReasoningKind: "thinking", Delta: "plan"}); err != nil {
		t.Fatalf("thinking event: %v", err)
	}
	if err := encoder.EncodeEvent(canonicalStreamEvent{Type: canonicalStreamEventReasoningDelta, ReasoningKind: "thinking_signature", Delta: "sig"}); err != nil {
		t.Fatalf("signature event: %v", err)
	}
	if err := encoder.EncodeEvent(canonicalStreamEvent{Type: canonicalStreamEventTextDelta, Delta: "hello"}); err != nil {
		t.Fatalf("text event: %v", err)
	}
	if err := encoder.EncodeEvent(canonicalStreamEvent{Type: canonicalStreamEventCompleted}); err != nil {
		t.Fatalf("completed event: %v", err)
	}

	body := rec.Body.String()
	for _, want := range []string{"message_start", "thinking_delta", "signature_delta", "text_delta", "message_stop"} {
		if !strings.Contains(body, want) {
			t.Fatalf("anthropic stream body missing %q: %s", want, body)
		}
	}
	if !capture.streamCompleted || capture.endReason != "done" {
		t.Fatalf("capture = %+v, want completed done", capture)
	}
}

func TestGatewayProtocolSpecValidationAndCloneHelpers(t *testing.T) {
	for _, value := range []any{float64(1.5), float32(2.5), int(3), int64(4), json.Number("5.5")} {
		if _, ok := toFloat64ForValidation(value); !ok {
			t.Fatalf("expected %T to convert", value)
		}
	}
	if _, ok := toFloat64ForValidation(json.Number("bad")); ok {
		t.Fatal("invalid json.Number converted")
	}
	if _, ok := toFloat64ForValidation("7"); ok {
		t.Fatal("string should not convert")
	}

	original := map[string]any{"items": []any{"a", "b"}, "nested": map[string]any{"n": []any{map[string]any{"x": "y"}}}}
	cloned := cloneSpecValue(original).(map[string]any)
	cloned["items"].([]string)[0] = "changed"
	cloned["nested"].(map[string]any)["n"].([]any)[0].(map[string]any)["x"] = "z"
	if original["items"].([]any)[0] != "a" {
		t.Fatalf("string slice clone mutated original: %#v", original)
	}
	if original["nested"].(map[string]any)["n"].([]any)[0].(map[string]any)["x"] != "y" {
		t.Fatalf("nested clone mutated original: %#v", original)
	}
}

func TestGatewayProviderThinkingCachePrunesOverflowAndModeFlags(t *testing.T) {
	cache := &providerAnthropicThinkingCache{entries: map[string]providerThinkingCacheEntry{}}
	for i := 0; i < providerThinkingCacheMaxEntries+3; i++ {
		cache.entries[uuid.NewString()] = providerThinkingCacheEntry{expiresAt: time.Now().Add(time.Hour)}
	}
	cache.pruneOverflowLocked()
	if len(cache.entries) != providerThinkingCacheMaxEntries {
		t.Fatalf("entries after prune = %d, want %d", len(cache.entries), providerThinkingCacheMaxEntries)
	}

	if !providerThinkingModeEnabled(nil) {
		t.Fatal("nil payload should default thinking mode to enabled")
	}
	if providerThinkingModeEnabled(map[string]any{"thinking": map[string]any{"type": "disabled"}}) {
		t.Fatal("disabled thinking map should disable mode")
	}
	if providerThinkingModeEnabled(map[string]any{"thinking": false}) {
		t.Fatal("false thinking bool should disable mode")
	}
	if !providerThinkingModeEnabled(map[string]any{"thinking": "unknown"}) {
		t.Fatal("unknown thinking value should default enabled")
	}
}

func TestGatewayRateLimitAndRecordingNilDependencyBranches(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	handler := Handler{logger: logger}

	reservation, limitErr, err := handler.acquireRateLimit(context.Background(), uuid.New(), testGatewayEndpointStub{path: gatewayEndpointResponses}, gatewayRequest{}, time.Now())
	if reservation != nil || limitErr != nil || err != nil {
		t.Fatalf("nil rate limit acquire = reservation=%v limit=%v err=%v", reservation, limitErr, err)
	}
	handler.settleRateLimit(context.Background(), nil, 42)

	id := handler.recordAttempt(context.Background(), "req", uuid.New(), uuid.New(), routeengine.Candidate{}, gatewayAttemptResult{attempt: 1, statusCode: http.StatusBadGateway}, nil)
	if id != uuid.Nil {
		t.Fatalf("recordAttempt with nil recorder store = %s, want nil", id)
	}
	handler.recordRequestFailure(context.Background(), "req", uuid.New(), time.Now(), http.StatusBadRequest, "bad_request", "bad", "model", false, "decode", gatewayEndpointChatCompletions)
}

func TestGatewaySiteModelDiagnosticProtocolHelpersAndRecorder(t *testing.T) {
	for input, want := range map[string]string{
		" /chat/completions ": siteModelTestProtocolChatCompletions,
		"openai-responses":    siteModelTestProtocolResponses,
		"anthropic_messages":  siteModelTestProtocolMessages,
		"":                    siteModelTestProtocolAuto,
	} {
		got, err := normalizeSiteModelTestProtocol(input)
		if err != nil {
			t.Fatalf("normalize %q returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalize %q = %q, want %q", input, got, want)
		}
	}
	if _, err := normalizeSiteModelTestProtocol("bad"); err == nil {
		t.Fatal("expected invalid protocol error")
	}

	path, err := siteModelTestDownstreamPathForProtocol([]string{upstreamEndpointTypeOpenAI}, siteModelTestProtocolMessages)
	if err != nil || path != gatewayEndpointMessages {
		t.Fatalf("messages protocol path = %q err=%v", path, err)
	}
	if _, err := siteModelTestDownstreamPathForProtocol(nil, "bad"); err == nil {
		t.Fatal("expected invalid downstream protocol error")
	}

	rec := &discardResponseWriter{}
	rec.WriteHeader(http.StatusAccepted)
}

func waitForGatewayModelsCacheEntry(t *testing.T, cache *modelsCache, apiKeyID uuid.UUID) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, ok := cache.get(apiKeyID); ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for cache refresh")
}

func gatewayFloat64Ptr(value float64) *float64 {
	return &value
}

type testGatewayEndpointStub struct {
	path              string
	routeEndpointType string
}

func (e testGatewayEndpointStub) DownstreamPath() string {
	return e.path
}

func (e testGatewayEndpointStub) RouteEndpointType() string {
	return e.routeEndpointType
}

func (e testGatewayEndpointStub) DecodeRequest(*http.Request) (gatewayRequest, *chatFailure) {
	return gatewayRequest{}, nil
}
