package gateway

import (
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

func TestOpenAIProtocolResolverEndpointBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		request   gatewayRequest
		candidate routeengine.Candidate
		want      string
	}{
		{
			name:    "embeddings endpoint",
			request: typedGatewayRequest(gatewayEndpointEmbeddings, map[string]any{"model": "text-embedding-3-large", "input": []any{"hello"}}),
			want:    "openai_embeddings",
		},
		{
			name:    "openai images endpoint",
			request: typedGatewayRequest(gatewayEndpointImagesGenerations, map[string]any{"model": "gpt-image-2", "prompt": "draw"}),
			want:    "openai_images_generations",
		},
		{
			name:      "codex images endpoint",
			request:   typedGatewayRequest(gatewayEndpointImagesGenerations, map[string]any{"model": "gpt-5-codex", "prompt": "draw"}),
			candidate: resolverCooldownCandidate("codex"),
			want:      "codex_responses",
		},
		{
			name:      "antigravity images endpoint",
			request:   typedGatewayRequest(gatewayEndpointImagesGenerations, map[string]any{"model": "gemini-3-pro-image", "prompt": "draw"}),
			candidate: resolverCooldownCandidate("antigravity"),
			want:      "antigravity_image_generate_content",
		},
		{
			name:      "google images endpoint",
			request:   typedGatewayRequest(gatewayEndpointImagesGenerations, map[string]any{"model": "gemini-image", "prompt": "draw"}),
			candidate: resolverCooldownCandidate("google_gemini"),
			want:      "google_generate_content",
		},
		{
			name:      "codex chat endpoint",
			request:   typedGatewayRequest(gatewayEndpointChatCompletions, map[string]any{"model": "gpt-5-codex", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}),
			candidate: resolverCooldownCandidate("codex"),
			want:      "codex_responses",
		},
		{
			name:      "antigravity chat endpoint",
			request:   typedGatewayRequest(gatewayEndpointChatCompletions, map[string]any{"model": "gemini-3-pro", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}),
			candidate: resolverCooldownCandidate("antigravity"),
			want:      "antigravity_generate_content",
		},
		{
			name:      "google chat endpoint",
			request:   typedGatewayRequest(gatewayEndpointChatCompletions, map[string]any{"model": "gemini-2.5-pro", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}),
			candidate: resolverCooldownCandidate("google_gemini"),
			want:      "google_generate_content",
		},
		{
			name:      "anthropic chat endpoint",
			request:   typedGatewayRequest(gatewayEndpointChatCompletions, map[string]any{"model": "claude-sonnet-4", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}),
			candidate: resolverCooldownCandidate("anthropic"),
			want:      "anthropic_messages_to_chat_completions",
		},
		{
			name:      "nil db endpoint fallback",
			request:   typedGatewayRequest(gatewayEndpointResponses, map[string]any{"model": "gpt-5.4", "input": "hi"}),
			candidate: resolverCooldownCandidate("openai"),
			want:      "openai_chat_completions_to_responses",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter, err := (openAIProtocolResolver{}).Resolve(t.Context(), tt.request, tt.candidate)
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			if got := adapter.ProtocolName(); got != tt.want {
				t.Fatalf("ProtocolName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolverEndpointTypeHelpersNilDBAndUUID(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		siteModelID uuid.UUID
	}{
		{name: "nil db", siteModelID: uuid.MustParse("22222222-2222-2222-2222-222222222222")},
		{name: "nil uuid", siteModelID: uuid.Nil},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := (openAIProtocolResolver{}).supportedEndpointTypes(t.Context(), tc.siteModelID)
			if err != nil {
				t.Fatalf("supportedEndpointTypes returned error: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("supportedEndpointTypes = %#v, want empty", got)
			}
		})
	}
}

func TestEndpointTypeHelpersMatchResponsesRouting(t *testing.T) {
	t.Parallel()

	if !containsEndpointType([]string{" OpenAI-Response ", "google-gemini"}, "openai-response") {
		t.Fatal("expected endpoint type match to be case and whitespace insensitive")
	}
	if containsEndpointType(nil, "openai-response") {
		t.Fatal("expected nil endpoint types not to match")
	}
	if containsEndpointType([]string{"openai"}, "openai-response") {
		t.Fatal("expected different endpoint type not to match")
	}

	for _, tc := range []struct {
		name          string
		request       gatewayRequest
		candidate     routeengine.Candidate
		endpointTypes []string
		want          bool
	}{
		{
			name:          "responses unsupported",
			request:       gatewayRequest{DownstreamPath: gatewayEndpointResponses},
			endpointTypes: []string{"openai"},
			want:          false,
		},
		{
			name:          "responses only",
			request:       gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions},
			endpointTypes: []string{" openai-response "},
			want:          true,
		},
		{
			name:          "dual stack codex model",
			request:       gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions},
			candidate:     routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5-codex"}},
			endpointTypes: []string{"openai", "openai-response"},
			want:          true,
		},
		{
			name:          "dual stack responses downstream",
			request:       gatewayRequest{DownstreamPath: gatewayEndpointResponses},
			endpointTypes: []string{"openai", "openai-response"},
			want:          true,
		},
		{
			name:          "dual stack messages downstream",
			request:       gatewayRequest{DownstreamPath: gatewayEndpointMessages},
			endpointTypes: []string{"openai", "openai-response"},
			want:          true,
		},
		{
			name:          "dual stack chat non codex",
			request:       gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions},
			candidate:     routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4"}},
			endpointTypes: []string{"openai", "openai-response"},
			want:          false,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := shouldUseOpenAIResponses(tc.request, tc.candidate, tc.endpointTypes); got != tc.want {
				t.Fatalf("shouldUseOpenAIResponses() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolverDownstreamCanonicalProtocolBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want canonicalProtocol
	}{
		{path: " " + gatewayEndpointResponses + " ", want: canonicalProtocolOpenAIResponses},
		{path: gatewayEndpointMessages, want: canonicalProtocolAnthropicMessages},
		{path: gatewayEndpointImagesGenerations, want: canonicalProtocolOpenAIImages},
		{path: gatewayEndpointChatCompletions, want: canonicalProtocolOpenAIChat},
		{path: "/v1/unknown", want: canonicalProtocolOpenAIChat},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.want)+"_"+tt.path, func(t *testing.T) {
			t.Parallel()

			if got := downstreamCanonicalProtocol(tt.path); got != tt.want {
				t.Fatalf("downstreamCanonicalProtocol(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestCooldownInputForFailureClassifiesRetryableFailures(t *testing.T) {
	t.Parallel()

	credentialID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	tests := []struct {
		name             string
		candidate        routeengine.Candidate
		result           gatewayAttemptResult
		wantScope        string
		wantReason       string
		wantDuration     time.Duration
		wantModelID      bool
		wantCredentialID bool
		wantNoResponse   bool
	}{
		{
			name:      "credential unauthorized",
			candidate: resolverCooldownCandidate("openai"),
			result: gatewayAttemptResult{
				statusCode:        http.StatusUnauthorized,
				errorType:         "upstream_http_error",
				credentialID:      credentialID,
				credentialMasked:  "sk-...abcd",
				credentialAttempt: 1,
				credentialTotal:   2,
			},
			wantScope:        "credential",
			wantReason:       "upstream_credential_unauthorized",
			wantDuration:     credentialUnauthorizedCooldownDuration,
			wantCredentialID: true,
		},
		{
			name:      "credential forbidden",
			candidate: resolverCooldownCandidate("openai"),
			result: gatewayAttemptResult{
				statusCode:   http.StatusForbidden,
				errorType:    "upstream_http_error",
				credentialID: credentialID,
			},
			wantScope:        "credential",
			wantReason:       "upstream_credential_forbidden",
			wantDuration:     30 * time.Minute,
			wantModelID:      true,
			wantCredentialID: true,
		},
		{
			name:      "credential rate limited",
			candidate: resolverCooldownCandidate("openai"),
			result: gatewayAttemptResult{
				statusCode:        http.StatusTooManyRequests,
				errorType:         "upstream_http_error",
				credentialID:      credentialID,
				retryAfterSeconds: 23,
			},
			wantScope:        "credential",
			wantReason:       "upstream_credential_rate_limited",
			wantDuration:     23 * time.Second,
			wantCredentialID: true,
		},
		{
			name:      "credential decrypt failed",
			candidate: resolverCooldownCandidate("openai"),
			result: gatewayAttemptResult{
				errorType:    "upstream_credential_decrypt_failed",
				credentialID: credentialID,
			},
			wantScope:        "credential",
			wantReason:       "upstream_credential_decrypt_failed",
			wantDuration:     30 * time.Minute,
			wantCredentialID: true,
		},
		{
			name:      "model not found",
			candidate: resolverCooldownCandidate("openai"),
			result: gatewayAttemptResult{
				statusCode: http.StatusNotFound,
				errorType:  "upstream_http_error",
			},
			wantScope:    "model",
			wantReason:   "upstream_model_not_found",
			wantDuration: 30 * time.Minute,
			wantModelID:  true,
		},
		{
			name:      "model server failure",
			candidate: resolverCooldownCandidate("openai"),
			result: gatewayAttemptResult{
				statusCode:         http.StatusBadGateway,
				upstreamStatusCode: http.StatusInternalServerError,
				errorType:          "upstream_http_error",
			},
			wantScope:    "model",
			wantReason:   "upstream_http_error",
			wantDuration: transientCooldownBaseDuration,
			wantModelID:  true,
		},
		{
			name:      "no response timeout",
			candidate: resolverCooldownCandidate("openai"),
			result: gatewayAttemptResult{
				statusCode: http.StatusBadGateway,
				errorType:  "upstream_timeout",
			},
			wantScope:      "model",
			wantReason:     "upstream_no_response",
			wantDuration:   upstreamNoResponseCooldownDuration,
			wantModelID:    true,
			wantNoResponse: true,
		},
		{
			name:      "classified codex model",
			candidate: resolverCooldownCandidate("codex"),
			result: gatewayAttemptResult{
				statusCode:       http.StatusTooManyRequests,
				errorType:        "codex_model_capacity",
				cooldownReason:   "codex_model_capacity",
				cooldownScope:    "model",
				cooldownDuration: 2 * time.Minute,
			},
			wantScope:    "model",
			wantReason:   "codex_model_capacity",
			wantDuration: 2 * time.Minute,
			wantModelID:  true,
		},
		{
			name:      "classified antigravity credential default duration",
			candidate: resolverCooldownCandidate("antigravity"),
			result: gatewayAttemptResult{
				statusCode:    http.StatusTooManyRequests,
				errorType:     "antigravity_oauth_rate_limited",
				credentialID:  credentialID,
				cooldownScope: "credential",
			},
			wantScope:        "credential",
			wantReason:       "antigravity_oauth_rate_limited",
			wantDuration:     5 * time.Minute,
			wantCredentialID: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := cooldownInputForFailure(tt.candidate, tt.result)
			if !ok {
				t.Fatal("cooldownInputForFailure returned ok=false")
			}
			if got.Scope != tt.wantScope {
				t.Fatalf("Scope = %q, want %q", got.Scope, tt.wantScope)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if got.Duration != tt.wantDuration {
				t.Fatalf("Duration = %s, want %s", got.Duration, tt.wantDuration)
			}
			if (got.SiteModelID != nil) != tt.wantModelID {
				t.Fatalf("SiteModelID present = %v, want %v", got.SiteModelID != nil, tt.wantModelID)
			}
			if got.SiteModelID != nil && *got.SiteModelID != tt.candidate.Model.SiteModelID {
				t.Fatalf("SiteModelID = %s, want %s", *got.SiteModelID, tt.candidate.Model.SiteModelID)
			}
			if (got.SiteCredentialID != nil) != tt.wantCredentialID {
				t.Fatalf("SiteCredentialID present = %v, want %v", got.SiteCredentialID != nil, tt.wantCredentialID)
			}
			if got.SiteCredentialID != nil && *got.SiteCredentialID != credentialID {
				t.Fatalf("SiteCredentialID = %s, want %s", *got.SiteCredentialID, credentialID)
			}
			if got.Metadata["no_upstream_response"] != tt.wantNoResponse && tt.wantNoResponse {
				t.Fatalf("metadata no_upstream_response = %#v, want true", got.Metadata["no_upstream_response"])
			}
			if tt.result.retryAfterSeconds > 0 && got.Metadata["retry_after_seconds"] != tt.result.retryAfterSeconds {
				t.Fatalf("metadata retry_after_seconds = %#v, want %d", got.Metadata["retry_after_seconds"], tt.result.retryAfterSeconds)
			}
			if tt.result.credentialMasked != "" && got.Metadata["credential_masked"] != tt.result.credentialMasked {
				t.Fatalf("metadata credential_masked = %#v, want %q", got.Metadata["credential_masked"], tt.result.credentialMasked)
			}
		})
	}
}

func TestCooldownInputForFailureSkipsNonCooldownBranches(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		result gatewayAttemptResult
	}{
		{name: "bad request", result: gatewayAttemptResult{statusCode: http.StatusBadRequest, errorType: "upstream_http_error"}},
		{name: "downstream cancel", result: gatewayAttemptResult{statusCode: 499, errorType: "downstream_client_cancelled"}},
		{name: "concurrency limited", result: gatewayAttemptResult{statusCode: http.StatusTooManyRequests, errorType: "site_concurrency_limited"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got, ok := cooldownInputForFailure(resolverCooldownCandidate("openai"), tc.result); ok {
				t.Fatalf("cooldownInputForFailure returned ok=true with input %#v", got)
			}
		})
	}
}

func TestClassifyGatewayUpstreamErrorCodexAndAntigravityBranches(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)

	tests := []struct {
		name              string
		candidate         routeengine.Candidate
		result            gatewayAttemptResult
		body              []byte
		wantStatus        int
		wantType          string
		wantScope         string
		wantReason        string
		wantDuration      time.Duration
		wantRetryAfterSec int64
	}{
		{
			name:         "codex model capacity",
			candidate:    resolverCooldownCandidate("codex"),
			result:       gatewayAttemptResult{statusCode: http.StatusBadGateway, upstreamStatusCode: http.StatusBadRequest, errorType: "upstream_http_error"},
			body:         []byte(`{"error":{"message":"Selected model is at capacity. Please try a different model."}}`),
			wantStatus:   http.StatusTooManyRequests,
			wantType:     "codex_model_capacity",
			wantScope:    "model",
			wantReason:   "codex_model_capacity",
			wantDuration: 2 * time.Minute,
		},
		{
			name:              "codex usage limit",
			candidate:         resolverCooldownCandidate("codex"),
			result:            gatewayAttemptResult{statusCode: http.StatusBadGateway, upstreamStatusCode: http.StatusTooManyRequests, errorType: "upstream_http_error"},
			body:              []byte(`{"error":{"type":"usage_limit_reached","message":"usage limit reached","resets_in_seconds":45}}`),
			wantStatus:        http.StatusTooManyRequests,
			wantType:          "codex_usage_limit_reached",
			wantScope:         "credential",
			wantReason:        "codex_usage_limit_reached",
			wantDuration:      45 * time.Second,
			wantRetryAfterSec: 45,
		},
		{
			name:      "antigravity rate limited",
			candidate: resolverCooldownCandidate("antigravity"),
			result:    gatewayAttemptResult{statusCode: http.StatusBadGateway, upstreamStatusCode: http.StatusTooManyRequests, errorType: "upstream_http_error"},
			body: []byte(`{"error":{"message":"quota exhausted","details":[` +
				`{"reason":"RATE_LIMIT_EXCEEDED","retryDelay":"7s"}` +
				`]}}`),
			wantStatus:        http.StatusTooManyRequests,
			wantType:          "antigravity_model_rate_limited",
			wantScope:         "model",
			wantReason:        "antigravity_model_rate_limited",
			wantDuration:      7 * time.Second,
			wantRetryAfterSec: 7,
		},
		{
			name:              "xlyra daily api key quota",
			candidate:         resolverCooldownCandidate("xlyra"),
			result:            gatewayAttemptResult{statusCode: http.StatusUnauthorized, upstreamStatusCode: http.StatusUnauthorized, errorType: "upstream_http_error"},
			body:              []byte(`{"error":{"type":"authentication_error","code":"api_key_daily_quota_exhausted","message":"API key daily quota has been exhausted.","scope":"daily","reset_at":"2023-11-14T22:14:50Z"}}`),
			wantStatus:        http.StatusUnauthorized,
			wantType:          "upstream_credential_limited",
			wantScope:         "credential",
			wantReason:        store.CooldownReasonUpstreamCredentialLimited,
			wantDuration:      90 * time.Second,
			wantRetryAfterSec: 90,
		},
		{
			name:         "sub2api api key quota",
			candidate:    resolverCooldownCandidate("openai"),
			result:       gatewayAttemptResult{statusCode: http.StatusTooManyRequests, upstreamStatusCode: http.StatusTooManyRequests, errorType: "upstream_http_error"},
			body:         []byte(`{"code":"API_KEY_QUOTA_EXHAUSTED","message":"API key 额度已用完"}`),
			wantStatus:   http.StatusTooManyRequests,
			wantType:     "upstream_credential_limited",
			wantScope:    "credential",
			wantReason:   store.CooldownReasonUpstreamCredentialLimited,
			wantDuration: 0,
		},
		{
			name:         "sub2api insufficient balance",
			candidate:    resolverCooldownCandidate("openai"),
			result:       gatewayAttemptResult{statusCode: http.StatusForbidden, upstreamStatusCode: http.StatusForbidden, errorType: "upstream_http_error"},
			body:         []byte(`{"code":"INSUFFICIENT_BALANCE","message":"Insufficient account balance"}`),
			wantStatus:   http.StatusForbidden,
			wantType:     "upstream_credential_limited",
			wantScope:    "credential",
			wantReason:   store.CooldownReasonUpstreamCredentialLimited,
			wantDuration: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := classifyGatewayUpstreamError(tt.candidate, tt.result, tt.body, now)
			if got.statusCode != tt.wantStatus {
				t.Fatalf("statusCode = %d, want %d", got.statusCode, tt.wantStatus)
			}
			if got.errorType != tt.wantType {
				t.Fatalf("errorType = %q, want %q", got.errorType, tt.wantType)
			}
			if got.cooldownScope != tt.wantScope {
				t.Fatalf("cooldownScope = %q, want %q", got.cooldownScope, tt.wantScope)
			}
			if got.cooldownReason != tt.wantReason {
				t.Fatalf("cooldownReason = %q, want %q", got.cooldownReason, tt.wantReason)
			}
			if got.cooldownDuration != tt.wantDuration {
				t.Fatalf("cooldownDuration = %s, want %s", got.cooldownDuration, tt.wantDuration)
			}
			if got.retryAfterSeconds != tt.wantRetryAfterSec {
				t.Fatalf("retryAfterSeconds = %d, want %d", got.retryAfterSeconds, tt.wantRetryAfterSec)
			}
		})
	}
}

func TestClassifyGatewayUpstreamErrorRecognizesSubscriptionUsageLimit(t *testing.T) {
	t.Parallel()

	input := gatewayAttemptResult{
		statusCode:         http.StatusBadGateway,
		upstreamStatusCode: http.StatusTooManyRequests,
		errorType:          "upstream_http_error",
		errorMessage:       "unchanged",
	}

	got := classifyGatewayUpstreamErrorWithTimeZone(
		resolverCooldownCandidate("openai"),
		input,
		[]byte(`{"error":{"type":"usage_limit_reached","resets_in_seconds":30}}`),
		time.Unix(1_700_000_000, 0),
		config.LoadTimeZone("Asia/Shanghai"),
	)
	if got.statusCode != input.statusCode || got.errorType != "upstream_credential_limited" || got.errorMessage != input.errorMessage {
		t.Fatalf("classifyGatewayUpstreamError result = %#v, want generic credential limit", got)
	}
	if got.cooldownScope != "credential" || got.cooldownReason != store.CooldownReasonUpstreamCredentialLimited || got.cooldownDuration != 30*time.Second || got.retryAfterSeconds != 30 {
		t.Fatalf("classifyGatewayUpstreamError cooldown = %#v, want credential limit for 30s", got)
	}
}

func TestClassifyGatewayUpstreamErrorBuildsSubscriptionCooldown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 6, 14, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	result := gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusTooManyRequests,
		errorType:          "upstream_http_error",
		errorMessage:       "upstream returned HTTP 429",
	}
	body := []byte(`{"code":"USAGE_LIMIT_EXCEEDED","message":"error: code=429 reason=\"DAILY_LIMIT_EXCEEDED\" message=\"daily usage limit exceeded\" metadata=map[]"}`)
	got := classifyGatewayUpstreamErrorWithTimeZone(resolverCooldownCandidate("openai"), result, body, now, config.LoadTimeZone("Asia/Shanghai"))
	wantReset := time.Date(2026, 8, 7, 0, 0, 0, 0, now.Location())
	if got.errorType != "upstream_subscription_limit_exceeded" || got.cooldownReason != store.CooldownReasonUpstreamSubscriptionLimitExceeded || got.cooldownScope != "credential" {
		t.Fatalf("classification = %#v, want dedicated credential subscription cooldown", got)
	}
	if got.cooldownDuration != wantReset.Sub(now) || got.retryAfterSeconds != int64(wantReset.Sub(now).Seconds()) {
		t.Fatalf("cooldown = %s retry=%d, want %s", got.cooldownDuration, got.retryAfterSeconds, wantReset.Sub(now))
	}
	if got.cooldownMetadata["limit_window"] != "daily" || got.cooldownMetadata["upstream_code"] != "USAGE_LIMIT_EXCEEDED" || got.cooldownMetadata["upstream_reason"] != "DAILY_LIMIT_EXCEEDED" || got.cooldownMetadata["reset_at"] != wantReset.UTC().Format(time.RFC3339) {
		t.Fatalf("metadata = %#v, want daily upstream details and reset", got.cooldownMetadata)
	}
}

func TestClassifyGatewayUpstreamErrorUsesCredentialQuotaProbeReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 4, 3, 37, 0, time.UTC)
	wantReset := time.Date(2026, 8, 21, 14, 29, 8, 997742000, time.UTC)
	result := gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusTooManyRequests,
		errorType:          "upstream_http_error",
		errorMessage:       "upstream returned HTTP 429",
		credentialMeta: store.JSON(`{"quota_probe":{"status":"ok","entries":[
			{"label":"daily","remaining":100,"reset_at":"2026-08-21T00:00:00+08:00"},
			{"label":"weekly","remaining":0,"reset_at":"2026-08-21T22:29:08.997742+08:00"},
			{"label":"monthly","remaining":33.8,"reset_at":"2026-09-07T22:29:08.997742+08:00"}
		],"fetched_at":"2026-08-20T00:00:14Z"}}`),
	}
	body := []byte(`{"code":"USAGE_LIMIT_EXCEEDED","message":"error: code=429 reason=\"WEEKLY_LIMIT_EXCEEDED\" message=\"weekly usage limit exceeded\" metadata=map[]"}`)
	got := classifyGatewayUpstreamErrorWithTimeZone(resolverCooldownCandidate("openai"), result, body, now, config.LoadTimeZone("Asia/Shanghai"))
	if got.cooldownDuration != wantReset.Sub(now) || got.retryAfterSeconds != int64(math.Ceil(wantReset.Sub(now).Seconds())) {
		t.Fatalf("cooldown = %s retry=%d, want reset %s", got.cooldownDuration, got.retryAfterSeconds, wantReset)
	}
	if got.cooldownMetadata["reset_at"] != wantReset.Format(time.RFC3339) {
		t.Fatalf("reset_at = %#v, want %s", got.cooldownMetadata["reset_at"], wantReset.Format(time.RFC3339))
	}
	calendarReset := subscriptionLimitCalendarResetAt(now, "weekly", config.LoadTimeZone("Asia/Shanghai"))
	if wantReset.Equal(calendarReset) {
		t.Fatalf("test setup must differ from calendar fallback %s", calendarReset)
	}
}

func TestSubscriptionLimitCalendarResetAt(t *testing.T) {
	t.Parallel()

	shanghai := config.LoadTimeZone("Asia/Shanghai")
	newYork := config.LoadTimeZone("America/New_York")
	for _, test := range []struct {
		name     string
		now      time.Time
		window   string
		timeZone config.TimeZone
		want     string
	}{
		{name: "daily configured zone", now: time.Date(2026, 8, 6, 23, 30, 0, 0, time.UTC), window: "daily", timeZone: shanghai, want: "2026-08-08T00:00:00+08:00"},
		{name: "weekly monday boundary", now: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC), window: "weekly", timeZone: shanghai, want: "2026-08-10T00:00:00+08:00"},
		{name: "monthly boundary", now: time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC), window: "monthly", timeZone: shanghai, want: "2027-01-01T00:00:00+08:00"},
		{name: "usage fallback", now: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC), window: "usage", timeZone: shanghai, want: "2026-08-07T00:00:00+08:00"},
		{name: "daily dst safe", now: time.Date(2026, 3, 8, 1, 30, 0, 0, newYork.Location), window: "daily", timeZone: newYork, want: "2026-03-09T00:00:00-04:00"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := subscriptionLimitCalendarResetAt(test.now, test.window, test.timeZone)
			if got.Format(time.RFC3339) != test.want {
				t.Fatalf("reset = %s, want %s", got.Format(time.RFC3339), test.want)
			}
		})
	}
}

func TestSubscriptionLimitResetPriority(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)
	failure := upstream.ClassifyResponseAt(http.StatusTooManyRequests, nil, []byte(`{"code":"USAGE_LIMIT_EXCEEDED","reset_at":"2026-08-07T00:00:00Z"}`), now)
	resetAt, seconds := subscriptionLimitResetAt(gatewayAttemptResult{retryAfterSeconds: 90}, failure, now, config.LoadTimeZone("Asia/Shanghai"))
	if !resetAt.Equal(now.Add(90*time.Second)) || seconds != 90 {
		t.Fatalf("Retry-After reset = %s/%d, want 90 seconds", resetAt, seconds)
	}

	resetAt, seconds = subscriptionLimitResetAt(gatewayAttemptResult{}, failure, now, config.LoadTimeZone("Asia/Shanghai"))
	want := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	if !resetAt.Equal(want) || seconds != int64(want.Sub(now).Seconds()) {
		t.Fatalf("upstream reset_at = %s/%d, want %s", resetAt, seconds, want)
	}
}

func typedGatewayRequest(path string, payload map[string]any) gatewayRequest {
	return gatewayRequest{
		DownstreamPath: path,
		Payload:        payload,
	}
}

func resolverCooldownCandidate(siteType string) routeengine.Candidate {
	return routeengine.Candidate{
		Site: routeengine.CandidateSite{
			ID:       uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			SiteType: siteType,
		},
		Model: routeengine.CandidateModel{
			SiteModelID:  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			UpstreamName: "upstream-model",
		},
	}
}
