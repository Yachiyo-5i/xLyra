package gateway

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func openCodeGoTestChatProtocol(candidate routeengine.Candidate) gatewayProtocolAdapter {
	return newOpenAIChatProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, candidate)
}

func TestApplyOpenCodeGoDefaultToolsAddsReadWhenMissing(t *testing.T) {
	t.Parallel()

	candidate := resolverCooldownCandidate("opencode_go")
	candidate.Site.BaseURL = "https://opencode.ai/zen"
	payload := map[string]any{"model": "deepseek-v4.1-flash-free"}
	applyOpenCodeGoDefaultTools(payload, candidate, openCodeGoTestChatProtocol(candidate))

	want := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "read",
				"description": "读取文件",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "文件路径",
						},
					},
					"required": []any{"path"},
				},
			},
		},
	}
	if !reflect.DeepEqual(payload["tools"], want) {
		t.Fatalf("tools = %#v, want %#v", payload["tools"], want)
	}
}

func TestApplyOpenCodeGoDefaultToolsPreservesDownstreamTools(t *testing.T) {
	t.Parallel()

	downstreamTools := []any{map[string]any{"type": "function", "function": map[string]any{"name": "custom"}}}
	payload := map[string]any{"tools": downstreamTools}
	candidate := resolverCooldownCandidate("opencode_go")
	applyOpenCodeGoDefaultTools(payload, candidate, openCodeGoTestChatProtocol(candidate))
	if !reflect.DeepEqual(payload["tools"], downstreamTools) {
		t.Fatalf("tools = %#v, want downstream tools %#v", payload["tools"], downstreamTools)
	}
}

func TestApplyOpenCodeGoDefaultToolsPreservesPresentEmptyTools(t *testing.T) {
	t.Parallel()

	candidate := resolverCooldownCandidate("opencode_go")
	payload := map[string]any{"tools": []any{}}
	applyOpenCodeGoDefaultTools(payload, candidate, openCodeGoTestChatProtocol(candidate))
	if tools, ok := payload["tools"].([]any); !ok || len(tools) != 0 {
		t.Fatalf("tools = %#v, want the downstream empty array", payload["tools"])
	}
}

func TestBuildOpenCodeUpstreamPayloadPreservesToolsAfterProtocolConversion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		tools []any
	}{
		{name: "explicit empty tools", tools: []any{}},
		{name: "custom tools", tools: []any{map[string]any{
			"type": "function",
			"name": "custom",
			"parameters": map[string]any{
				"type": "object",
			},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{
				"model": "deepseek-v4.1-flash-free",
				"input": "test",
				"tools": tc.tools,
			}
			canonical, err := canonicalRequestFromOpenAIResponsesPayload(payload, "deepseek-v4.1-flash-free")
			if err != nil {
				t.Fatalf("decode request: %v", err)
			}
			request := gatewayRequest{DownstreamPath: gatewayEndpointResponses, Payload: payload, Canonical: &canonical}
			candidate := resolverCooldownCandidate("opencode_go")
			protocol := newOpenAIChatProtocolAdapter(request, candidate)
			upstream, err := buildGatewayUpstreamPayload(protocol, request, candidate)
			if err != nil {
				t.Fatalf("build upstream payload: %v", err)
			}
			tools, _ := upstream["tools"].([]any)
			if len(tools) != len(tc.tools) {
				t.Fatalf("upstream tools = %#v, want only the %d downstream tools", upstream["tools"], len(tc.tools))
			}
			if len(tools) > 0 {
				tool, _ := tools[0].(map[string]any)
				function, _ := tool["function"].(map[string]any)
				if function["name"] != "custom" {
					t.Fatalf("upstream tool = %#v, want converted custom tool", tool)
				}
			}
		})
	}
}

func TestApplyOpenCodeGoDefaultToolsUsesUpstreamToolSchema(t *testing.T) {
	t.Parallel()

	candidate := resolverCooldownCandidate("opencode_go")
	for _, test := range []struct {
		path string
		name string
	}{
		{path: "/v1/responses", name: "openAI Responses"},
		{path: "/v1/messages", name: "Anthropic Messages"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate.Site.BaseURL = "https://opencode.ai/zen/go"
			payload := map[string]any{}
			var protocol gatewayProtocolAdapter
			switch test.path {
			case "/v1/responses":
				protocol = openAIResponsesProtocolAdapter{path: test.path}
			case "/v1/messages":
				protocol = anthropicMessagesProtocolAdapter{}
			}
			applyOpenCodeGoDefaultTools(payload, candidate, protocol)
			tools, ok := payload["tools"].([]any)
			if !ok || len(tools) != 1 {
				t.Fatalf("tools = %#v, want one default tool", payload["tools"])
			}
			tool, ok := tools[0].(map[string]any)
			if !ok {
				t.Fatalf("tool = %#v, want an object", tools[0])
			}
			if _, hasChatFunction := tool["function"]; hasChatFunction {
				t.Fatalf("tool = %#v, should use the target protocol's schema", tool)
			}
		})
	}
}

func TestApplyOpenCodeGoDefaultToolsDoesNotChangeOtherSites(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"model": "test"}
	candidate := resolverCooldownCandidate("openai")
	applyOpenCodeGoDefaultTools(payload, candidate, openCodeGoTestChatProtocol(candidate))
	if _, exists := payload["tools"]; exists {
		t.Fatalf("added tools for non-OpenCode site: %#v", payload["tools"])
	}
}

func TestApplyOpenCodeGoGatewayHeadersKeepsOnlyRequiredHeaders(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamHeaders: http.Header{
			"X-OpenCode-Session-Id": {"invalid"},
			"X-Session-Affinity":    {"invalid"},
		},
	}
	req := gatewayHeaderRequest(t, http.MethodPost, "https://opencode.ai/zen/go/v1/chat/completions")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Other", "drop-me")
	req.Header.Set("x-opencode-client", "old-client")
	applyOpenCodeGoGatewayHeaders(req, request, "test-api-key")

	want := map[string]string{
		"Authorization":          "Bearer test-api-key",
		"Content-Type":           "application/json",
		"User-Agent":             "opencode/latest/2.0.29/cli",
		openCodeGoSessionHeader:  req.Header.Get(openCodeGoSessionHeader),
		openCodeGoAffinityHeader: req.Header.Get(openCodeGoAffinityHeader),
	}
	if len(req.Header) != len(want) {
		t.Fatalf("headers = %#v, want only %d headers", req.Header, len(want))
	}
	for key, value := range want {
		if got := req.Header.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	if !openCodeGoHeaderIDIsValid(req.Header.Get(openCodeGoSessionHeader)) {
		t.Errorf("%s = %q, want ses_ + 12 lowercase hex timestamp digits + 14 random characters", openCodeGoSessionHeader, req.Header.Get(openCodeGoSessionHeader))
	}
	if !openCodeGoHeaderIDIsValid(req.Header.Get(openCodeGoAffinityHeader)) {
		t.Errorf("%s = %q, want ses_ + 12 lowercase hex timestamp digits + 14 random characters", openCodeGoAffinityHeader, req.Header.Get(openCodeGoAffinityHeader))
	}
}

func TestApplyOpenCodeGoGatewayHeadersPreservesValidSessionAndAffinityIDs(t *testing.T) {
	t.Parallel()

	sessionID := "ses_0123456789abABCDEFGHIJKLMN"
	affinityID := "ses_abcdef012345zyxwvutsrqponm"
	downstream := http.Header{}
	downstream.Set(openCodeGoSessionHeader, sessionID)
	downstream.Set(openCodeGoAffinityHeader, affinityID)
	request := gatewayRequest{DownstreamHeaders: downstream}
	req := gatewayHeaderRequest(t, http.MethodPost, "https://opencode.ai/zen/go/v1/chat/completions")
	applyOpenCodeGoGatewayHeaders(req, request, "test-api-key")
	assertGatewayHeader(t, req, openCodeGoSessionHeader, sessionID)
	assertGatewayHeader(t, req, openCodeGoAffinityHeader, affinityID)
}

func openCodeGoHeaderIDIsValid(value string) bool {
	return openCodeGoValidHeaderID(value) == value
}

func TestOpenCodeGoHeadersAreScopedToOpenCodeSites(t *testing.T) {
	t.Parallel()

	if isOpenCodeGoSite("openai") || isOpenCodeGoSite("") || !isOpenCodeGoSite(" OpenCode_Go ") {
		t.Fatal("OpenCode Go site detection is not scoped correctly")
	}
}

func TestClassifyOpenCodeGoUsageLimitPreservesOfficialReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			ID:       uuid.New(),
			SiteType: "opencode_go",
		},
		Model: routeengine.CandidateModel{
			SiteModelID:  uuid.New(),
			UpstreamName: "kimi-k3",
		},
	}
	credentialID := uuid.New()
	result := gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusTooManyRequests,
		errorType:          "upstream_http_error",
		retryAfterSeconds:  7 * 24 * 60 * 60,
		credentialID:       credentialID,
	}
	body := []byte(`{"type":"error","error":{"type":"GoUsageLimitError","message":"Weekly Go usage limit reached"},"metadata":{"workspace":"wrk_test","limitName":"weekly"}}`)

	classified := classifyGatewayUpstreamError(candidate, result, body, now)
	if classified.errorType != "opencode_go_usage_limit" || classified.cooldownScope != "credential" {
		t.Fatalf("classified result = %#v", classified)
	}
	if classified.cooldownDuration != 7*24*time.Hour || classified.retryAfterSeconds != 7*24*60*60 {
		t.Fatalf("classified duration = %s, retryAfter = %d", classified.cooldownDuration, classified.retryAfterSeconds)
	}
	if classified.cooldownMetadata["workspace"] != "wrk_test" || classified.cooldownMetadata["limit_name"] != "weekly" {
		t.Fatalf("classified metadata = %#v", classified.cooldownMetadata)
	}
	if classified.cooldownMetadata["reset_at"] != now.Add(7*24*time.Hour).Format(time.RFC3339) {
		t.Fatalf("reset_at = %#v", classified.cooldownMetadata["reset_at"])
	}

	input, ok := cooldownInputForFailure(candidate, classified)
	if !ok {
		t.Fatal("OpenCode Go usage limit did not create a cooldown")
	}
	if input.Scope != "credential" || input.Reason != store.CooldownReasonOpenCodeGoUsageLimitReached || input.Duration != 7*24*time.Hour {
		t.Fatalf("cooldown input = %#v", input)
	}
	if input.Metadata["workspace"] != "wrk_test" || input.Metadata["retry_after_seconds"] != int64(7*24*60*60) {
		t.Fatalf("cooldown metadata = %#v", input.Metadata)
	}
}

func TestClassifyOpenCodeGoLeavesOrdinaryRateLimitGeneric(t *testing.T) {
	t.Parallel()

	result := gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusTooManyRequests,
		errorType:          "upstream_http_error",
		retryAfterSeconds:  30,
	}
	got := classifyGatewayUpstreamError(
		resolverCooldownCandidate("opencode_go"),
		result,
		[]byte(`{"error":{"type":"rate_limit_exceeded","message":"slow down"}}`),
		time.Now(),
	)
	if got.errorType != "upstream_credential_limited" {
		t.Fatalf("ordinary rate limit errorType = %q", got.errorType)
	}
}

func TestWriteUpstreamFailureForwardsRetryAfter(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	writeUpstreamFailure(recorder, gatewayAttemptResult{
		statusCode:        http.StatusTooManyRequests,
		contentType:       "application/json",
		body:              []byte(`{"error":{"type":"GoUsageLimitError"}}`),
		retryAfterSeconds: 86400,
	}, "request-test")
	if got := recorder.Header().Get("Retry-After"); got != "86400" {
		t.Fatalf("Retry-After = %q, want 86400", got)
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
}
