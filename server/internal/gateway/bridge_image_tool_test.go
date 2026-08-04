package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func bridgeTestResponsesRequest(payload map[string]any) gatewayRequest {
	return gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		RequestedModel: "gpt-5.5",
		Stream:         true,
		Payload:        payload,
	}
}

func TestRewriteImageToolForBridgeReplacesHostedTool(t *testing.T) {
	request := bridgeTestResponsesRequest(map[string]any{
		"model": "gpt-5.5",
		"input": "draw a cat",
		"tools": []any{
			map[string]any{"type": "image_generation", "size": "1024x1024", "quality": "medium"},
			map[string]any{"type": "function", "name": "get_weather"},
		},
	})

	rewritten, spec, ok := rewriteImageToolForBridge(request)
	if !ok {
		t.Fatal("expected rewrite to apply")
	}
	if spec.Size != "1024x1024" || spec.Quality != "medium" {
		t.Fatalf("expected spec to capture hosted tool params, got %+v", spec)
	}
	if spec.Forced {
		t.Fatal("expected non-forced spec without tool_choice")
	}
	tools, _ := rewritten.Payload["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	first, _ := tools[0].(map[string]any)
	if first["type"] != "function" || first["name"] != bridgeImageFunctionName {
		t.Fatalf("expected bridge function tool first, got %+v", first)
	}
	second, _ := tools[1].(map[string]any)
	if second["name"] != "get_weather" {
		t.Fatalf("expected user tool preserved, got %+v", second)
	}
	if rewritten.Canonical == nil {
		t.Fatal("expected canonical request rebuilt")
	}
	originalTools, _ := request.Payload["tools"].([]any)
	originalFirst, _ := originalTools[0].(map[string]any)
	if originalFirst["type"] != "image_generation" {
		t.Fatal("expected original payload unmodified")
	}
}

func TestRewriteImageToolForBridgeForcesFunctionChoice(t *testing.T) {
	request := bridgeTestResponsesRequest(map[string]any{
		"model":       "gpt-5.5",
		"input":       "draw a cat",
		"tools":       []any{map[string]any{"type": "image_generation"}},
		"tool_choice": map[string]any{"type": "image_generation"},
	})

	rewritten, spec, ok := rewriteImageToolForBridge(request)
	if !ok {
		t.Fatal("expected rewrite to apply")
	}
	if !spec.Forced {
		t.Fatal("expected forced spec")
	}
	choice, _ := rewritten.Payload["tool_choice"].(map[string]any)
	if choice["type"] != "function" || choice["name"] != bridgeImageFunctionName {
		t.Fatalf("expected forced function tool_choice, got %+v", choice)
	}
}

func TestRewriteImageToolForBridgeSkipsWithoutTool(t *testing.T) {
	request := bridgeTestResponsesRequest(map[string]any{
		"model": "gpt-5.5",
		"input": "hello",
	})
	if _, _, ok := rewriteImageToolForBridge(request); ok {
		t.Fatal("expected rewrite to skip without image tool")
	}
}

func TestRewriteBridgeImageHistoryItems(t *testing.T) {
	input := []any{
		map[string]any{"type": "message", "role": "user", "content": "draw a cat"},
		map[string]any{
			"type":           "image_generation_call",
			"id":             "ig_abc",
			"status":         "completed",
			"result":         strings.Repeat("A", 1024),
			"revised_prompt": "a fluffy cat",
		},
		map[string]any{"type": "message", "role": "user", "content": "now a dog"},
	}

	result := rewriteBridgeImageHistoryItems(input)
	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %d", len(result))
	}
	call, _ := result[1].(map[string]any)
	if call["type"] != "function_call" || call["name"] != bridgeImageFunctionName || call["call_id"] != "ig_abc" {
		t.Fatalf("expected function_call replacement, got %+v", call)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(anyString(call["arguments"])), &args); err != nil {
		t.Fatalf("expected valid arguments json: %v", err)
	}
	if args["prompt"] != "a fluffy cat" {
		t.Fatalf("expected revised prompt in arguments, got %+v", args)
	}
	output, _ := result[2].(map[string]any)
	if output["type"] != "function_call_output" || output["call_id"] != "ig_abc" {
		t.Fatalf("expected function_call_output, got %+v", output)
	}
	if strings.Contains(anyString(output["output"]), strings.Repeat("A", 32)) {
		t.Fatal("expected base64 result dropped from replayed history")
	}
}

func TestBridgeContextForRequestGating(t *testing.T) {
	enabledKey := store.APIKey{ImageToolBridge: store.JSON(`{"enabled":true,"model":"gpt-image-2"}`)}
	basePayload := func() map[string]any {
		return map[string]any{
			"model": "gpt-5.5",
			"input": "draw a cat",
			"tools": []any{map[string]any{"type": "image_generation"}},
		}
	}

	base := bridgeTestResponsesRequest(basePayload())
	if bridge, reason := bridgeContextForRequest(enabledKey, base, codexImageIntentToolAdvertised); bridge == nil {
		t.Fatalf("expected bridge context for eligible request, got reason %q", reason)
	}

	nonStream := base
	nonStream.Stream = false
	if bridge, reason := bridgeContextForRequest(enabledKey, nonStream, codexImageIntentToolAdvertised); bridge != nil || reason != "non_streaming_request" {
		t.Fatalf("expected non_streaming_request skip, got %v %q", bridge, reason)
	}

	chat := base
	chat.DownstreamPath = gatewayEndpointChatCompletions
	if bridge, reason := bridgeContextForRequest(enabledKey, chat, codexImageIntentToolAdvertised); bridge != nil || reason != "non_responses_endpoint" {
		t.Fatalf("expected non_responses_endpoint skip, got %v %q", bridge, reason)
	}

	stateful := bridgeTestResponsesRequest(basePayload())
	stateful.Payload["previous_response_id"] = "resp_123"
	if bridge, reason := bridgeContextForRequest(enabledKey, stateful, codexImageIntentToolAdvertised); bridge != nil || reason != "stateful_previous_response_id" {
		t.Fatalf("expected stateful skip, got %v %q", bridge, reason)
	}

	disabledKey := store.APIKey{ImageToolBridge: store.JSON(`{}`)}
	if bridge, reason := bridgeContextForRequest(disabledKey, base, codexImageIntentToolAdvertised); bridge != nil || reason != "key_bridge_not_configured" {
		t.Fatalf("expected key config skip, got %v %q", bridge, reason)
	}

	if bridge, reason := bridgeContextForRequest(enabledKey, base, codexImageIntentNone); bridge != nil || reason != "no_image_intent" {
		t.Fatalf("expected no_image_intent skip, got %v %q", bridge, reason)
	}
}

func TestCandidateRequiresImageBridge(t *testing.T) {
	stripSite := routeengine.Candidate{}
	stripSite.Site.ResponsesToolPolicy = "compatibility"
	stripSite.Site.DisabledResponsesTools = []string{"image_generation"}
	plainSite := routeengine.Candidate{}

	if candidateRequiresImageBridge(plainSite, codexProtocolAdapter{}) {
		t.Fatal("codex upstream without strip config must use the native path")
	}
	if !candidateRequiresImageBridge(stripSite, codexProtocolAdapter{}) {
		t.Fatal("codex upstream with strip config must bridge")
	}
	if !candidateRequiresImageBridge(plainSite, anthropicMessagesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}) {
		t.Fatal("canonical conversion upstream must bridge")
	}
}

func TestBridgeImageOutcomeFromBody(t *testing.T) {
	good := []byte(`{"created":1,"data":[{"b64_json":"aGk=","revised_prompt":"a cat"}]}`)
	outcome := bridgeImageOutcomeFromBody(good)
	if !outcome.OK || outcome.B64 != "aGk=" || outcome.RevisedPrompt != "a cat" {
		t.Fatalf("expected parsed outcome, got %+v", outcome)
	}

	empty := bridgeImageOutcomeFromBody([]byte(`{"data":[]}`))
	if empty.OK {
		t.Fatal("expected failure for empty data")
	}

	invalid := bridgeImageOutcomeFromBody([]byte(`nope`))
	if invalid.OK {
		t.Fatal("expected failure for invalid json")
	}
}

func TestBridgeReplayHasUserFunctionCall(t *testing.T) {
	bridged := []any{map[string]any{"type": "function_call", "name": bridgeImageFunctionName}}
	if bridgeReplayHasUserFunctionCall(bridged) {
		t.Fatal("bridge call alone is not a user call")
	}
	mixed := append(bridged, map[string]any{"type": "function_call", "name": "get_weather"})
	if !bridgeReplayHasUserFunctionCall(mixed) {
		t.Fatal("expected user function call detected")
	}
}

func TestBridgeRescueEligible(t *testing.T) {
	sub2apiGroup := gatewayAttemptResult{
		statusCode: 403,
		body:       []byte(`{"error":{"type":"permission_error","message":"Image generation is not enabled for this group"}}`),
	}
	if !bridgeRescueEligible(sub2apiGroup) {
		t.Fatal("expected sub2api group rejection to be rescue-eligible")
	}

	sub2apiPlatform := gatewayAttemptResult{
		statusCode: 404,
		body:       []byte(`{"error":{"type":"not_found_error","message":"Images API is not supported for this platform"}}`),
	}
	if !bridgeRescueEligible(sub2apiPlatform) {
		t.Fatal("expected sub2api platform rejection to be rescue-eligible")
	}

	started := sub2apiGroup
	started.responseStarted = true
	if bridgeRescueEligible(started) {
		t.Fatal("must not rescue after content reached the client")
	}

	generic := gatewayAttemptResult{statusCode: 500, body: []byte(`{"error":"internal"}`)}
	if bridgeRescueEligible(generic) {
		t.Fatal("generic failures must not trigger a rescue")
	}

	succeeded := gatewayAttemptResult{success: true, body: []byte(`image generation is not enabled`)}
	if bridgeRescueEligible(succeeded) {
		t.Fatal("successful results must not trigger a rescue")
	}
}
