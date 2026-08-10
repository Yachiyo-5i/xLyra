package gateway

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/httpx"
	"xlyra/server/internal/ratelimit"
	routeengine "xlyra/server/internal/router"
)

func TestAttemptMetadataIncludesNormalizedGatewayFields(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	siteID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	siteModelID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	canonicalModelID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	credentialID := uuid.MustParse("55555555-5555-5555-5555-555555555555")

	metadata := attemptMetadata(
		context.Background(),
		"req-attempt",
		"req-parent",
		apiKeyID,
		canonicalModelID,
		routeengine.Candidate{
			Site: routeengine.CandidateSite{
				ID:       siteID,
				Name:     "demo",
				SiteType: "newapi",
			},
			Model: routeengine.CandidateModel{
				SiteModelID:  siteModelID,
				UpstreamName: "gpt-4o-mini",
				DisplayName:  "GPT-4o Mini",
			},
		},
		gatewayAttemptResult{
			attempt:            1,
			statusCode:         429,
			upstreamStatusCode: 429,
			errorType:          "upstream_http_error",
			currency:           "USD",
			stream:             true,
			responseStarted:    true,
			streamEndReason:    "upstream_stream_incomplete",
			upstreamProtocol:   string(canonicalProtocolOpenAIResponses),
			downstreamPath:     gatewayEndpointChatCompletions,
			credentialID:       credentialID,
			credentialAttempt:  1,
			credentialTotal:    2,
		},
	)

	if got := metadata["scope"]; got != "gateway" {
		t.Fatalf("expected scope gateway, got %#v", got)
	}
	if got := metadata["endpoint"]; got != gatewayEndpointChatCompletions {
		t.Fatalf("expected endpoint %q, got %#v", gatewayEndpointChatCompletions, got)
	}
	if got := metadata["request_id"]; got != "req-attempt" {
		t.Fatalf("expected attempt request id, got %#v", got)
	}
	if got := metadata["parent_request_id"]; got != "req-parent" {
		t.Fatalf("expected parent request id, got %#v", got)
	}
	if got := metadata["error_code"]; got != "upstream_http_error" {
		t.Fatalf("expected error_code upstream_http_error, got %#v", got)
	}
	if got := metadata["stream_started"]; got != true {
		t.Fatalf("expected stream_started true, got %#v", got)
	}
	if got := metadata["downstream_transport"]; got != "http" {
		t.Fatalf("expected downstream_transport http, got %#v", got)
	}
	if got := metadata["upstream_transport"]; got != "http_sse" {
		t.Fatalf("expected upstream_transport http_sse, got %#v", got)
	}
	if got := metadata["stream_incomplete"]; got != true {
		t.Fatalf("expected stream_incomplete true, got %#v", got)
	}
	if got := metadata["stream_failure_scope"]; got != "upstream" {
		t.Fatalf("expected upstream stream failure scope, got %#v", got)
	}
	conversion, ok := metadata["protocol_conversion"].(map[string]any)
	if !ok {
		t.Fatalf("expected protocol_conversion metadata, got %T", metadata["protocol_conversion"])
	}
	if got := conversion["mode"]; got != "canonical" {
		t.Fatalf("expected canonical conversion mode, got %#v", got)
	}
}

func TestStreamMetadataClassifiesReadFailureAsIncomplete(t *testing.T) {
	t.Parallel()

	result := gatewayAttemptResult{
		stream:          true,
		responseStarted: true,
		streamEndReason: "upstream_stream_read_failed",
	}

	if got := streamIncomplete(result); got != true {
		t.Fatalf("expected upstream read failure to be stream incomplete")
	}
	if got := streamFailureScope(result.streamEndReason); got != "upstream" {
		t.Fatalf("expected upstream failure scope, got %#v", got)
	}
}

func TestStreamMetadataKeepsResponseIncompleteOutOfFailureScope(t *testing.T) {
	t.Parallel()

	result := gatewayAttemptResult{
		stream:          true,
		responseStarted: true,
		streamEndReason: "response_incomplete",
	}

	if got := streamIncomplete(result); got != false {
		t.Fatalf("expected response_incomplete to avoid transport stream incomplete, got %#v", got)
	}
	if got := streamFailureScope(result.streamEndReason); got != nil {
		t.Fatalf("expected no stream failure scope for response_incomplete, got %#v", got)
	}
}

func TestAttemptMetadataMarksReadFailedStartedStreamIncomplete(t *testing.T) {
	t.Parallel()

	metadata := attemptMetadata(
		context.Background(),
		"req-attempt",
		"req-parent",
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		routeengine.Candidate{
			Site:  routeengine.CandidateSite{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333")},
			Model: routeengine.CandidateModel{SiteModelID: uuid.MustParse("44444444-4444-4444-4444-444444444444")},
		},
		gatewayAttemptResult{
			stream:          true,
			responseStarted: true,
			streamEndReason: "upstream_stream_read_failed",
		},
	)

	if got := metadata["stream_incomplete"]; got != true {
		t.Fatalf("expected read-failed started stream to be incomplete, got %#v", got)
	}
	if got := metadata["stream_failure_scope"]; got != "upstream" {
		t.Fatalf("expected upstream stream failure scope, got %#v", got)
	}
}

func TestAttemptMetadataDoesNotMarkResponsesIncompleteAsTransportFailure(t *testing.T) {
	t.Parallel()

	metadata := attemptMetadata(
		context.Background(),
		"req-attempt",
		"req-parent",
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		routeengine.Candidate{
			Site:  routeengine.CandidateSite{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333")},
			Model: routeengine.CandidateModel{SiteModelID: uuid.MustParse("44444444-4444-4444-4444-444444444444")},
		},
		gatewayAttemptResult{
			stream:          true,
			responseStarted: true,
			streamEndReason: "response_incomplete",
		},
	)

	if got := metadata["stream_incomplete"]; got != false {
		t.Fatalf("expected response_incomplete to avoid transport incomplete flag, got %#v", got)
	}
	if got := metadata["stream_failure_scope"]; got != nil {
		t.Fatalf("expected no stream failure scope for response_incomplete, got %#v", got)
	}
}

func TestAttemptMetadataIncludesFastBillingCalculation(t *testing.T) {
	t.Parallel()

	inputValue := 1.0
	outputValue := 2.0
	baseCost := 0.5
	finalCost := 1.25

	metadata := attemptMetadata(
		context.Background(),
		"req-attempt",
		"req-parent",
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		routeengine.Candidate{
			Site:  routeengine.CandidateSite{ID: uuid.MustParse("33333333-3333-3333-3333-333333333333"), SiteType: "newapi"},
			Model: routeengine.CandidateModel{SiteModelID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), UpstreamName: "gpt-5.5-codex"},
		},
		gatewayAttemptResult{
			promptTokens:             100,
			completionTokens:         50,
			cachedPromptTokens:       20,
			estimatedCost:            &finalCost,
			baseEstimatedCost:        &baseCost,
			serviceTier:              "fast",
			billingMode:              "fast",
			costMultiplier:           2.5,
			credentialCostMultiplier: 1.2,
			multiplierReason:         "codex_fast_mode",
			pricing: selectedPricing{
				Currency:    "USD",
				InputValue:  &inputValue,
				OutputValue: &outputValue,
			},
		},
	)

	if got := metadata["billing_mode"]; got != "fast" {
		t.Fatalf("billing_mode = %#v, want fast", got)
	}
	calculation, ok := metadata["cost_calculation"].(map[string]any)
	if !ok {
		t.Fatalf("expected cost_calculation metadata, got %T", metadata["cost_calculation"])
	}
	if got := calculation["service_tier"]; got != "fast" {
		t.Fatalf("service_tier = %#v, want fast", got)
	}
	if got := calculation["billing_mode"]; got != "fast" {
		t.Fatalf("billing_mode = %#v, want fast", got)
	}
	if got := calculation["base_estimated_cost"]; got != baseCost {
		t.Fatalf("base_estimated_cost = %#v, want %v", got, baseCost)
	}
	if got := calculation["estimated_cost"]; got != finalCost {
		t.Fatalf("estimated_cost = %#v, want %v", got, finalCost)
	}
	if got := calculation["cost_multiplier"]; got != 2.5 {
		t.Fatalf("cost_multiplier = %#v, want 2.5", got)
	}
	if got := calculation["cost_multiplier_reason"]; got != "codex_fast_mode" {
		t.Fatalf("cost_multiplier_reason = %#v, want codex_fast_mode", got)
	}
	if got := calculation["credential_upstream_cost_multiplier"]; got != 1.2 {
		t.Fatalf("credential_upstream_cost_multiplier = %#v, want 1.2", got)
	}
	if got := calculation["service_tier_multiplier"]; got != 2.5 {
		t.Fatalf("service_tier_multiplier = %#v, want 2.5", got)
	}
}

func TestAttemptMetadataMarksDiagnosticAndModelMapping(t *testing.T) {
	t.Parallel()

	ctx := withModelMapping(context.Background(), "gpt-original", "gpt-mapped", "hard")
	metadata := attemptMetadata(
		ctx,
		"req-attempt",
		"req-parent",
		uuid.Nil,
		uuid.Nil,
		routeengine.Candidate{},
		gatewayAttemptResult{
			diagnostic:     true,
			downstreamPath: gatewayEndpointResponses,
		},
	)

	if metadata["scope"] != "site_model_test" || metadata["test"] != true {
		t.Fatalf("expected diagnostic metadata, got %#v", metadata)
	}
	if metadata["downstream_api_key"] != nil {
		t.Fatalf("diagnostic metadata should hide downstream api key, got %#v", metadata["downstream_api_key"])
	}
	if metadata["api_key_id"] != nil || metadata["site_id"] != nil || metadata["site_model_id"] != nil || metadata["canonical_model_id"] != nil {
		t.Fatalf("nil IDs should remain nil in metadata: %#v", metadata)
	}
	if metadata["original_model"] != "gpt-original" || metadata["mapped_model"] != "gpt-mapped" {
		t.Fatalf("model mapping metadata = %#v", metadata)
	}
}

func TestAttemptMetadataUsesRateLimitMetadataFromContext(t *testing.T) {
	t.Parallel()

	ctx := withRateLimitMetadata(context.Background(), map[string]any{
		"scope":      "api_key",
		"limit_type": "rpm",
	})
	metadata := attemptMetadata(
		ctx,
		"req-attempt",
		"req-parent",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{},
		gatewayAttemptResult{},
	)

	rateLimit, ok := metadata["rate_limit"].(map[string]any)
	if !ok {
		t.Fatalf("expected rate_limit metadata, got %T", metadata["rate_limit"])
	}
	if rateLimit["scope"] != "api_key" || rateLimit["limit_type"] != "rpm" {
		t.Fatalf("unexpected rate limit metadata: %#v", rateLimit)
	}
}

func TestRateLimitTokenCountIncludesAudioOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result gatewayAttemptResult
		want   int64
	}{
		{
			name: "text only",
			result: gatewayAttemptResult{
				promptTokens:     100,
				completionTokens: 50,
			},
			want: 150,
		},
		{
			name: "audio output",
			result: gatewayAttemptResult{
				promptTokens:      100,
				completionTokens:  50,
				audioOutputTokens: 25,
			},
			want: 175,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := rateLimitTokenCount(tt.result); got != tt.want {
				t.Fatalf("rate limit tokens = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAttemptMetadataRateLimitIncludesAudioOutput(t *testing.T) {
	t.Parallel()

	metadata := attemptMetadata(
		context.Background(),
		"req-attempt",
		"req-parent",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{},
		gatewayAttemptResult{
			promptTokens:      100,
			completionTokens:  50,
			audioOutputTokens: 25,
			rateLimit:         &ratelimit.Reservation{},
		},
	)

	rateLimit, ok := metadata["rate_limit"].(map[string]any)
	if !ok {
		t.Fatalf("expected rate_limit metadata, got %T", metadata["rate_limit"])
	}
	if got := rateLimit["actual_tokens"]; got != int64(175) {
		t.Fatalf("rate limit actual tokens = %#v, want 175", got)
	}
}

func TestProtocolConversionMetadataClassifiesPassthroughAndUnknown(t *testing.T) {
	t.Parallel()

	passthrough := protocolConversionMetadata(gatewayAttemptResult{
		stream:           true,
		downstreamPath:   gatewayEndpointResponses,
		upstreamProtocol: string(canonicalProtocolOpenAIResponses),
	})
	passthroughMap, ok := passthrough.(map[string]any)
	if !ok {
		t.Fatalf("expected passthrough conversion metadata, got %T", passthrough)
	}
	if passthroughMap["mode"] != "passthrough" || passthroughMap["downstream_protocol"] != string(canonicalProtocolOpenAIResponses) {
		t.Fatalf("unexpected passthrough conversion metadata: %#v", passthroughMap)
	}

	unknown := protocolConversionMetadata(gatewayAttemptResult{
		stream:           true,
		downstreamPath:   "/v1/unknown",
		upstreamProtocol: "",
	})
	unknownMap, ok := unknown.(map[string]any)
	if !ok {
		t.Fatalf("expected unknown conversion metadata, got %T", unknown)
	}
	if unknownMap["mode"] != "unknown" || unknownMap["downstream_protocol"] != nil || unknownMap["upstream_protocol"] != nil {
		t.Fatalf("unexpected unknown conversion metadata: %#v", unknownMap)
	}
	if got := protocolConversionMetadata(gatewayAttemptResult{stream: false}); got != nil {
		t.Fatalf("non-stream protocol conversion metadata = %#v, want nil", got)
	}
}

func TestStreamFailureScopeClassifiesDownstreamAndUnknownReasons(t *testing.T) {
	t.Parallel()

	if got := streamFailureScope("downstream_client_cancelled"); got != "downstream" {
		t.Fatalf("downstream client cancel scope = %#v, want downstream", got)
	}
	if got := streamFailureScope("upstream_stream_error"); got != "upstream" {
		t.Fatalf("upstream stream error scope = %#v, want upstream", got)
	}
	if got := streamFailureScope("unexpected_reason"); got != nil {
		t.Fatalf("unexpected stream end reason scope = %#v, want nil", got)
	}
	if got := streamEndedIncomplete(false, "done"); got {
		t.Fatal("done stream should not be marked incomplete")
	}
	if got := streamEndedIncomplete(true, "upstream_stream_read_failed"); got {
		t.Fatal("completed stream should not be marked incomplete")
	}
}

func TestRequestFailureMetadataUsesUnifiedErrorEnvelope(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	metadata := requestFailureMetadata(
		context.Background(),
		"req-parent",
		apiKeyID,
		503,
		"gateway_unavailable",
		"gateway service is not available",
		"gpt-4o-mini",
		false,
		"gateway",
		gatewayEndpointChatCompletions,
	)

	if got := metadata["scope"]; got != "gateway" {
		t.Fatalf("expected scope gateway, got %#v", got)
	}
	if got := metadata["status_code"]; got != 503 {
		t.Fatalf("expected status_code 503, got %#v", got)
	}
	errorResponse, ok := metadata["error_response"].(httpx.ErrorEnvelope)
	if !ok {
		t.Fatalf("expected error_response to be httpx.ErrorEnvelope, got %T", metadata["error_response"])
	}
	if errorResponse.Error.Code != "gateway_unavailable" {
		t.Fatalf("expected gateway_unavailable, got %q", errorResponse.Error.Code)
	}
}

func TestRequestFailureMetadataIncludesRetryAfter(t *testing.T) {
	t.Parallel()

	metadata := requestFailureMetadata(
		withRetryAfter(context.Background(), 30),
		"req-parent",
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		503,
		"no_route_candidates",
		"no available upstream route candidates",
		"gpt-4o-mini",
		false,
		"route_plan",
		gatewayEndpointChatCompletions,
	)

	if got := metadata["retry_after_seconds"]; got != int64(30) {
		t.Fatalf("expected retry_after_seconds 30, got %#v", got)
	}
}

func TestRequestFailureMetadataDefaultsEndpointAndIncludesContextMetadata(t *testing.T) {
	t.Parallel()

	ctx := withModelMapping(context.Background(), "gpt-original", "gpt-mapped", "hard")
	ctx = withRetryAfter(ctx, 0)
	ctx = withRateLimitMetadata(ctx, map[string]any{"scope": "global", "limit_type": "tpm"})
	metadata := requestFailureMetadata(
		ctx,
		"req-parent",
		uuid.Nil,
		400,
		"bad_request",
		"bad request",
		" ",
		true,
		"decode",
		" ",
	)

	if metadata["endpoint"] != gatewayEndpointChatCompletions || metadata["downstream_path"] != gatewayEndpointChatCompletions {
		t.Fatalf("blank endpoint should default to chat completions, got %#v", metadata)
	}
	if metadata["api_key_id"] != nil || metadata["requested_model"] != nil {
		t.Fatalf("blank api key/model should be nil, got %#v", metadata)
	}
	if metadata["response_mode"] != "stream" || metadata["stream"] != true {
		t.Fatalf("expected stream failure metadata, got %#v", metadata)
	}
	if metadata["original_model"] != "gpt-original" || metadata["mapped_model"] != "gpt-mapped" {
		t.Fatalf("model mapping metadata = %#v", metadata)
	}
	if _, ok := metadata["retry_after_seconds"]; ok {
		t.Fatalf("non-positive retry-after should be omitted, got %#v", metadata["retry_after_seconds"])
	}
	rateLimit, ok := metadata["rate_limit"].(map[string]any)
	if !ok || rateLimit["scope"] != "global" || rateLimit["limit_type"] != "tpm" {
		t.Fatalf("unexpected rate limit metadata: %#v", metadata["rate_limit"])
	}
}

func TestReasoningEffortFromPayloadAcceptsOnlyExplicitScalar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{name: "nested effort", payload: map[string]any{"reasoning": map[string]any{"effort": " high "}}, want: "high"},
		{name: "scalar fallback", payload: map[string]any{"reasoning_effort": "medium"}, want: "medium"},
		{name: "nested wins", payload: map[string]any{"reasoning": map[string]any{"effort": "high"}, "reasoning_effort": "low"}, want: "high"},
		{name: "blank nested falls back", payload: map[string]any{"reasoning": map[string]any{"effort": "  "}, "reasoning_effort": "low"}, want: "low"},
		{name: "invalid nested and scalar", payload: map[string]any{"reasoning": map[string]any{"effort": 3}, "reasoning_effort": true}},
		{name: "missing", payload: map[string]any{"model": "gpt-5"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := reasoningEffortFromPayload(tt.payload); got != tt.want {
				t.Fatalf("reasoning effort = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttemptMetadataPersistsReasoningEffortWithoutPayload(t *testing.T) {
	t.Parallel()

	ctx := withReasoningEffort(context.Background(), " high ")
	metadata := attemptMetadata(
		ctx,
		"req-attempt",
		"req-parent",
		uuid.Nil,
		uuid.Nil,
		routeengine.Candidate{},
		gatewayAttemptResult{},
	)
	if got := metadata["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort metadata = %#v, want high", got)
	}
	if _, ok := metadata["request_payload"]; ok {
		t.Fatalf("request payload must not be recorded: %#v", metadata)
	}
}

func TestRequestFailureMetadataPersistsReasoningEffort(t *testing.T) {
	t.Parallel()

	metadata := requestFailureMetadata(
		withReasoningEffort(context.Background(), "medium"),
		"req-parent",
		uuid.Nil,
		400,
		"bad_request",
		"bad request",
		"gpt-5",
		false,
		"validate",
		gatewayEndpointChatCompletions,
	)
	if got := metadata["reasoning_effort"]; got != "medium" {
		t.Fatalf("reasoning_effort metadata = %#v, want medium", got)
	}
}
