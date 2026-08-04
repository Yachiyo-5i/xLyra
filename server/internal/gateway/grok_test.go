package gateway

import (
	"net/http"
	"strings"
	"testing"

	"xlyra/server/internal/adapter"
	routeengine "xlyra/server/internal/router"
)

func TestApplyGrokGatewayHeadersUsesOfficialHostAndModelOverride(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, adapter.GrokChatBaseURL+"/v1/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyGrokGatewayHeaders(req, " oauth-token ", " grok-4.5 ")
	if req.Header.Get("Authorization") != "Bearer oauth-token" {
		t.Fatalf("authorization header = %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-XAI-Token-Auth") != adapter.GrokTokenAuthHeader {
		t.Fatalf("token auth header = %q", req.Header.Get("X-XAI-Token-Auth"))
	}
	if req.Header.Get(adapter.GrokModelHeader) != "grok-4.5" {
		t.Fatalf("model override header = %q", req.Header.Get(adapter.GrokModelHeader))
	}

	custom, err := http.NewRequest(http.MethodPost, "https://relay.example/v1/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyGrokGatewayHeaders(custom, "oauth-token", "grok-4.5")
	if custom.Header.Get("X-XAI-Token-Auth") != "" || custom.Header.Get(adapter.GrokModelHeader) != "" {
		t.Fatalf("custom host headers = %#v", custom.Header)
	}
}

func TestApplyGrokGatewayHeadersUsesBearerWithoutCLIIdentityForAPIHost(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, adapter.GrokAPIBaseURL+gatewayEndpointImagesGenerations, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer overridden")
	applyGrokGatewayHeaders(req, "oauth-token", "grok-imagine-image-quality")
	if req.Header.Get("Authorization") != "Bearer oauth-token" {
		t.Fatalf("authorization header = %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-XAI-Token-Auth") != "" || req.Header.Get(adapter.GrokModelHeader) != "" {
		t.Fatalf("api host received CLI headers: %#v", req.Header)
	}
}

func TestGrokImagesProtocolUsesOfficialAPIHost(t *testing.T) {
	generation := newGrokImagesProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations}, routeengine.Candidate{})
	if got := generation.UpstreamPath(adapter.GrokChatBaseURL); got != adapter.GrokAPIBaseURL+gatewayEndpointImagesGenerations {
		t.Fatalf("generation path = %q", got)
	}
	edit := newGrokImagesProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesEdits}, routeengine.Candidate{})
	if got := edit.UpstreamPath(adapter.GrokChatBaseURL); got != adapter.GrokAPIBaseURL+gatewayEndpointImagesEdits {
		t.Fatalf("edit path = %q", got)
	}
}

func TestGrokResponsesProtocolUsesOfficialCLIHost(t *testing.T) {
	protocol := newGrokResponsesProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointResponses}, false)
	if got := protocol.UpstreamPath("https://untrusted.example"); got != adapter.GrokChatBaseURL+gatewayEndpointResponses {
		t.Fatalf("responses path = %q", got)
	}
}

func TestNormalizeGrokResponsesPayloadRewritesContentTypes(t *testing.T) {
	payload := map[string]any{
		"model": "grok-4.5",
		"input": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hi"},
				map[string]any{"type": "input_image", "image_url": "x"},
			}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "text", "text": "hello"},
			}},
			map[string]any{"role": "user", "content": "already a string"},
		},
	}
	normalizeGrokResponsesPayload(payload)

	input := payload["input"].([]any)
	userPart := input[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if userPart["type"] != "input_text" {
		t.Fatalf("user text part type = %v, want input_text", userPart["type"])
	}
	imagePart := input[0].(map[string]any)["content"].([]any)[1].(map[string]any)
	if imagePart["type"] != "input_image" {
		t.Fatalf("non-text part changed: %v", imagePart["type"])
	}
	assistantPart := input[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if assistantPart["type"] != "output_text" {
		t.Fatalf("assistant text part type = %v, want output_text", assistantPart["type"])
	}
	if input[2].(map[string]any)["content"] != "already a string" {
		t.Fatal("string content must be left untouched")
	}
}

func TestNormalizeGrokResponsesPayloadIgnoresNonResponses(t *testing.T) {
	payload := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	normalizeGrokResponsesPayload(payload)
	if _, ok := payload["input"]; ok {
		t.Fatal("unexpected input key")
	}
}

func TestNormalizeGrokResponsesPayloadHoistsAndFiltersCodexTools(t *testing.T) {
	payload := map[string]any{
		"model":           "grok-4.5",
		"tool_choice":     "auto",
		"client_metadata": map[string]any{"session_id": "x"},
		"reasoning":       map[string]any{"effort": "high", "context": "all_turns"},
		"input": []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{
				map[string]any{"type": "custom", "name": "js"},
				map[string]any{"type": "function", "name": "exec"},
				map[string]any{"type": "namespace", "name": "ns"},
			}},
			map[string]any{"type": "message", "role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hi"},
			}},
		},
	}
	bridged := normalizeGrokResponsesPayload(payload)

	input := payload["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input len = %d, want 1 (additional_tools dropped)", len(input))
	}
	if input[0].(map[string]any)["content"].([]any)[0].(map[string]any)["type"] != "input_text" {
		t.Fatal("content type not normalized")
	}
	tools := payload["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v, want bridged custom + function", tools)
	}
	js := tools[0].(map[string]any)
	if js["type"] != "function" || js["name"] != "js" {
		t.Fatalf("bridged tool = %v, want function js", js)
	}
	if _, ok := js["parameters"].(map[string]any)["properties"].(map[string]any)["input"]; !ok {
		t.Fatal("bridged tool missing input parameter")
	}
	if tools[1].(map[string]any)["name"] != "exec" {
		t.Fatalf("second tool = %v, want exec", tools[1])
	}
	if _, ok := bridged["js"]; !ok || len(bridged) != 1 {
		t.Fatalf("bridged = %v, want only js", bridged)
	}
	if _, ok := payload["client_metadata"]; ok {
		t.Fatal("client_metadata not stripped")
	}
	if _, ok := payload["reasoning"].(map[string]any)["context"]; ok {
		t.Fatal("reasoning.context not stripped")
	}
}

func TestNormalizeGrokResponsesPayloadDropsReasoning(t *testing.T) {
	payload := map[string]any{
		"model":   "grok-4.5",
		"include": []any{"reasoning.encrypted_content", "message.output_text.logprobs"},
		"input": []any{
			map[string]any{"type": "reasoning", "id": "rs_1", "encrypted_content": "blob"},
			map[string]any{"type": "message", "role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hi"},
			}},
		},
	}
	normalizeGrokResponsesPayload(payload)

	input := payload["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input len = %d, want 1 (reasoning item dropped)", len(input))
	}
	if input[0].(map[string]any)["type"] != "message" {
		t.Fatalf("remaining item = %v, want message", input[0])
	}
	include := payload["include"].([]any)
	if len(include) != 1 || include[0] != "message.output_text.logprobs" {
		t.Fatalf("include = %v, want only the non-encrypted entry", include)
	}
}

func TestNormalizeGrokResponsesPayloadDropsIncludeWhenOnlyEncrypted(t *testing.T) {
	payload := map[string]any{
		"include": []any{"reasoning.encrypted_content"},
		"input":   []any{},
	}
	normalizeGrokResponsesPayload(payload)
	if _, ok := payload["include"]; ok {
		t.Fatal("include should be dropped when only encrypted_content remains")
	}
}

func TestNormalizeGrokResponsesPayloadDropsToolChoiceWhenNoTools(t *testing.T) {
	payload := map[string]any{
		"tool_choice": "auto",
		"input": []any{
			map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{
				map[string]any{"type": "local_shell"},
				map[string]any{"type": "namespace", "name": "empty"},
			}},
		},
	}
	normalizeGrokResponsesPayload(payload)
	if _, ok := payload["tools"]; ok {
		t.Fatal("tools should be absent")
	}
	if _, ok := payload["tool_choice"]; ok {
		t.Fatal("tool_choice must be dropped when no supported tools remain")
	}
}

func TestNormalizeGrokResponsesPayloadBridgesCustomToolCalls(t *testing.T) {
	payload := map[string]any{
		"input": []any{
			map[string]any{"type": "custom_tool_call", "call_id": "call-1", "name": "exec", "input": "1+1"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "call-1", "output": "2"},
		},
	}
	normalizeGrokResponsesPayload(payload)

	input := payload["input"].([]any)
	call := input[0].(map[string]any)
	if call["type"] != "function_call" {
		t.Fatalf("call type = %v, want function_call", call["type"])
	}
	if call["arguments"] != `{"input":"1+1"}` {
		t.Fatalf("arguments = %v", call["arguments"])
	}
	if _, ok := call["input"]; ok {
		t.Fatal("input field must be removed")
	}
	output := input[1].(map[string]any)
	if output["type"] != "function_call_output" || output["output"] != "2" {
		t.Fatalf("output item = %v", output)
	}
}

func TestRewriteGrokBridgedOutputItem(t *testing.T) {
	bridged := map[string]struct{}{"exec": {}}
	item := map[string]any{"type": "function_call", "name": "exec", "call_id": "c1", "arguments": `{"input":"console.log(1)"}`}
	if !rewriteGrokBridgedOutputItem(item, bridged) {
		t.Fatal("expected rewrite")
	}
	if item["type"] != "custom_tool_call" || item["input"] != "console.log(1)" {
		t.Fatalf("item = %v", item)
	}
	if item["id"] != "ctc_c1" {
		t.Fatalf("item ID = %v, want ctc_c1", item["id"])
	}
	if _, ok := item["arguments"]; ok {
		t.Fatal("arguments must be removed")
	}
	plain := map[string]any{"type": "function_call", "name": "wait", "arguments": "{}"}
	if rewriteGrokBridgedOutputItem(plain, bridged) {
		t.Fatal("non-bridged call must not be rewritten")
	}
}

func TestNormalizeGrokImagesPayload(t *testing.T) {
	payload := map[string]any{
		"model":         "grok-imagine-image-quality",
		"prompt":        "a cat",
		"n":             2,
		"size":          "1024x1024",
		"quality":       "hd",
		"output_format": "png",
	}
	normalizeGrokImagesPayload(payload)

	if _, ok := payload["size"]; ok {
		t.Fatal("size must be dropped")
	}
	if payload["resolution"] != "1k" {
		t.Fatalf("resolution = %v, want 1k", payload["resolution"])
	}
	if payload["aspect_ratio"] != "1:1" {
		t.Fatalf("aspect_ratio = %v, want 1:1", payload["aspect_ratio"])
	}
	if payload["quality"] != "high" {
		t.Fatalf("quality = %v, want high", payload["quality"])
	}
	if payload["n"] != 2 || payload["output_format"] != "png" {
		t.Fatalf("supported params must be untouched: %v", payload)
	}
	if payload["enable_nsfw"] != true {
		t.Fatalf("enable_nsfw should default to true, got %v", payload["enable_nsfw"])
	}
}

func TestNormalizeGrokImagesNSFWDefault(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
		set  bool
	}{
		{"absent", nil, true, false},
		{"explicit false", false, false, true},
		{"explicit true", true, true, true},
		{"string false", "false", false, true},
		{"string true", "1", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{"prompt": "x"}
			if tc.set {
				payload["enable_nsfw"] = tc.in
			}
			normalizeGrokImagesPayload(payload)
			if payload["enable_nsfw"] != tc.want {
				t.Fatalf("enable_nsfw = %v, want %v", payload["enable_nsfw"], tc.want)
			}
		})
	}
}

func TestNormalizeGrokImagesPayloadEdgeCases(t *testing.T) {
	payload := map[string]any{"size": "2048x1152", "quality": "auto", "resolution": "1k", "aspect_ratio": "1:1"}
	normalizeGrokImagesPayload(payload)
	if payload["resolution"] != "1k" {
		t.Fatalf("explicit resolution must win, got %v", payload["resolution"])
	}
	if payload["aspect_ratio"] != "1:1" {
		t.Fatalf("explicit aspect_ratio must win, got %v", payload["aspect_ratio"])
	}
	if _, ok := payload["quality"]; ok {
		t.Fatal("unsupported quality value must be dropped")
	}

	payload = map[string]any{"size": "1792x1024", "quality": "standard"}
	normalizeGrokImagesPayload(payload)
	if payload["resolution"] != "2k" {
		t.Fatalf("1792 wide should map to 2k, got %v", payload["resolution"])
	}
	if payload["aspect_ratio"] != "16:9" {
		t.Fatalf("1792x1024 should map to 16:9, got %v", payload["aspect_ratio"])
	}
	if payload["quality"] != "medium" {
		t.Fatalf("standard should map to medium, got %v", payload["quality"])
	}

	payload = map[string]any{"size": "auto"}
	normalizeGrokImagesPayload(payload)
	if payload["aspect_ratio"] != "auto" {
		t.Fatalf("size auto should map aspect_ratio to auto, got %v", payload["aspect_ratio"])
	}
	if _, ok := payload["resolution"]; ok {
		t.Fatal("auto size must not set resolution")
	}
	if _, ok := payload["size"]; ok {
		t.Fatal("size must still be dropped")
	}

	payload = map[string]any{"size": "bogus"}
	normalizeGrokImagesPayload(payload)
	if _, ok := payload["aspect_ratio"]; ok {
		t.Fatal("unparseable size must not set aspect_ratio")
	}
	if _, ok := payload["resolution"]; ok {
		t.Fatal("unparseable size must not set resolution")
	}
}

func TestGrokAspectRatioFromDims(t *testing.T) {
	cases := []struct {
		w, h int
		want string
	}{
		{1024, 1024, "1:1"},
		{1920, 1080, "16:9"},
		{1080, 1920, "9:16"},
		{1024, 768, "4:3"},
		{1500, 1000, "3:2"},
		{2000, 1000, "2:1"},
		{2340, 1080, "19.5:9"},
	}
	for _, tc := range cases {
		if got := grokAspectRatioFromDims(tc.w, tc.h); got != tc.want {
			t.Fatalf("grokAspectRatioFromDims(%d,%d) = %s, want %s", tc.w, tc.h, got, tc.want)
		}
	}
}

func TestGrokUpstreamCharged(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"moderated with cost", `{"code":"imagine:content-moderated","error":"rejected","usage":{"cost_in_usd_ticks":500000000}}`, true},
		{"success with cost", `{"data":[{"b64_json":"x"}],"usage":{"cost_in_usd_ticks":500000000}}`, true},
		{"no cost", `{"error":"bad request"}`, false},
		{"zero cost", `{"usage":{"cost_in_usd_ticks":0}}`, false},
		{"garbage", `not json`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := grokUpstreamCharged([]byte(tc.body)); got != tc.want {
				t.Fatalf("grokUpstreamCharged = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGrokImagesTransformBufferedResponseMarksBilledOnModeration(t *testing.T) {
	adapter := grokImagesProtocolAdapter{}
	body := []byte(`{"code":"imagine:content-moderated","error":"rejected","usage":{"cost_in_usd_ticks":500000000}}`)
	response, err := adapter.TransformBufferedResponse(400, http.Header{}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse error = %v", err)
	}
	if !response.UpstreamBilled {
		t.Fatal("moderation rejection with upstream cost must be marked billed")
	}
	if response.Usage.ImageCount != 1 {
		t.Fatalf("ImageCount = %d, want 1", response.Usage.ImageCount)
	}
	if response.StatusCode != 400 {
		t.Fatalf("status code must be preserved, got %d", response.StatusCode)
	}
}

func TestGrokStreamBridgeRewritesFunctionCallEvents(t *testing.T) {
	bridge := newGrokStreamBridge(map[string]struct{}{"exec": {}})
	capture := &streamCaptureState{}

	added := bridge.rewriteBlock([]byte("event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"c1\",\"name\":\"exec\",\"arguments\":\"\"}}\n\n"), capture)
	if !strings.Contains(string(added), `"custom_tool_call"`) {
		t.Fatalf("added = %s", added)
	}
	if !strings.Contains(string(added), `"id":"ctc_c1"`) {
		t.Fatalf("added ID does not use custom tool prefix: %s", added)
	}

	delta := bridge.rewriteBlock([]byte("event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"fc_1\",\"delta\":\"{\\\"input\\\":\\\"1+\"}\n\n"), capture)
	if delta != nil {
		t.Fatalf("delta must be suppressed, got %s", delta)
	}

	done := bridge.rewriteBlock([]byte("event: response.function_call_arguments.done\ndata: {\"type\":\"response.function_call_arguments.done\",\"item_id\":\"fc_1\",\"output_index\":0,\"arguments\":\"{\\\"input\\\":\\\"1+1\\\"}\"}\n\n"), capture)
	text := string(done)
	if !strings.Contains(text, "response.custom_tool_call_input.delta") || !strings.Contains(text, "response.custom_tool_call_input.done") {
		t.Fatalf("done = %s", text)
	}
	if !strings.Contains(text, `"input":"1+1"`) {
		t.Fatalf("done missing input, got %s", text)
	}
	if !strings.Contains(text, `"item_id":"ctc_c1"`) {
		t.Fatalf("done item ID does not match added item: %s", text)
	}

	itemDone := bridge.rewriteBlock([]byte("event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"c1\",\"name\":\"exec\",\"arguments\":\"{\\\"input\\\":\\\"1+1\\\"}\"}}\n\n"), capture)
	if !strings.Contains(string(itemDone), `"custom_tool_call"`) || !strings.Contains(string(itemDone), `"input":"1+1"`) {
		t.Fatalf("itemDone = %s", itemDone)
	}

	completed := bridge.rewriteBlock([]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"output\":[{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"c1\",\"name\":\"exec\",\"arguments\":\"{\\\"input\\\":\\\"1+1\\\"}\"}],\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}}\n\n"), capture)
	if !strings.Contains(string(completed), `"custom_tool_call"`) || !strings.Contains(string(completed), `"id":"ctc_c1"`) {
		t.Fatalf("completed = %s", completed)
	}
	if !capture.streamCompleted {
		t.Fatal("capture must record completion")
	}

	passthrough := []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")
	if got := bridge.rewriteBlock(passthrough, capture); string(got) != string(passthrough) {
		t.Fatalf("unrelated event must pass through verbatim, got %s", got)
	}
}

func TestGrokStreamBridgeKeepsMissingItemIDsDistinct(t *testing.T) {
	t.Parallel()

	bridge := newGrokStreamBridge(map[string]struct{}{"exec": {}})
	capture := &streamCaptureState{}
	for _, callID := range []string{"c1", "c2"} {
		added := bridge.rewriteBlock([]byte(`event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"`+callID+`","name":"exec","arguments":""}}

`), capture)
		if !strings.Contains(string(added), `"id":"ctc_`+callID+`"`) {
			t.Fatalf("added call %s has incorrect ID: %s", callID, added)
		}
	}
	for _, callID := range []string{"c1", "c2"} {
		delta := bridge.rewriteBlock([]byte(`event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","item_id":"`+callID+`","delta":"{\"input\":\"`+callID+`\"}"}

`), capture)
		if delta != nil {
			t.Fatalf("delta for call %s must be suppressed, got %s", callID, delta)
		}
		done := bridge.rewriteBlock([]byte(`event: response.function_call_arguments.done
data: {"type":"response.function_call_arguments.done","item_id":"`+callID+`","arguments":"{\"input\":\"`+callID+`\"}"}

`), capture)
		if !strings.Contains(string(done), `"item_id":"ctc_`+callID+`"`) {
			t.Fatalf("done for call %s has incorrect ID: %s", callID, done)
		}
	}
}
