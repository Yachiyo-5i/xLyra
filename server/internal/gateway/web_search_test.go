package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	routeengine "xlyra/server/internal/router"
)

func webSearchCandidate(model string) routeengine.Candidate {
	return routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: model}}
}

func findToolByKey(t *testing.T, tools any, key, value string) map[string]any {
	t.Helper()
	items, _ := tools.([]any)
	for _, item := range items {
		tool, _ := item.(map[string]any)
		if tool[key] == value {
			return tool
		}
		if _, ok := tool[value]; key == "has" && ok {
			return tool
		}
	}
	return nil
}

func TestWebSearchResponsesToAnthropicNativeTool(t *testing.T) {
	t.Parallel()
	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolAnthropicMessages, map[string]any{
		"model": "alias",
		"input": "latest news",
		"tools": []any{map[string]any{
			"type":          "web_search",
			"filters":       map[string]any{"allowed_domains": []any{"example.com"}},
			"user_location": map[string]any{"type": "approximate", "country": "US"},
		}},
	}, "alias", webSearchCandidate("claude-sonnet-4-5"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	tool := findToolByKey(t, payload["tools"], "type", anthropicWebSearchToolType)
	if tool == nil || tool["name"] != "web_search" {
		t.Fatalf("tools = %#v, want native anthropic web_search", payload["tools"])
	}
	if domains, _ := tool["allowed_domains"].([]string); len(domains) != 1 || domains[0] != "example.com" {
		t.Fatalf("allowed_domains = %#v", tool["allowed_domains"])
	}
	if loc, _ := tool["user_location"].(map[string]any); loc["country"] != "US" || loc["type"] != "approximate" {
		t.Fatalf("user_location = %#v", tool["user_location"])
	}
}

func TestWebSearchAnthropicToResponsesAndGemini(t *testing.T) {
	t.Parallel()
	source := map[string]any{
		"model":      "alias",
		"max_tokens": 100,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "web_search_20250305", "name": "web_search", "max_uses": 3,
			"allowed_domains": []any{"example.com"},
		}},
	}
	payload, err := convertRequestBetweenProtocols(canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIResponses, source, "alias", webSearchCandidate("gpt-5"))
	if err != nil {
		t.Fatalf("to responses: %v", err)
	}
	tool := findToolByKey(t, payload["tools"], "type", "web_search")
	if tool == nil {
		t.Fatalf("tools = %#v, want responses web_search", payload["tools"])
	}
	if filters, _ := tool["filters"].(map[string]any); filters == nil {
		t.Fatalf("filters missing: %#v", tool)
	}

	payload, err = convertRequestBetweenProtocols(canonicalProtocolAnthropicMessages, canonicalProtocolGoogleGemini, source, "alias", webSearchCandidate("gemini-2.5-pro"))
	if err != nil {
		t.Fatalf("to gemini: %v", err)
	}
	if findToolByKey(t, payload["tools"], "has", "googleSearch") == nil {
		t.Fatalf("tools = %#v, want googleSearch", payload["tools"])
	}
	if _, ok := payload["toolConfig"]; ok {
		t.Fatalf("toolConfig must be omitted when only googleSearch is declared: %#v", payload["toolConfig"])
	}
}

func TestWebSearchGeminiToAnthropicAndChat(t *testing.T) {
	t.Parallel()
	source := map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}}},
		"tools":    []any{map[string]any{"googleSearch": map[string]any{}}},
	}
	payload, err := convertRequestBetweenProtocols(canonicalProtocolGoogleGemini, canonicalProtocolAnthropicMessages, source, "alias", webSearchCandidate("claude-sonnet-4-5"))
	if err != nil {
		t.Fatalf("to anthropic: %v", err)
	}
	if findToolByKey(t, payload["tools"], "type", anthropicWebSearchToolType) == nil {
		t.Fatalf("tools = %#v", payload["tools"])
	}
	payload, err = convertRequestBetweenProtocols(canonicalProtocolGoogleGemini, canonicalProtocolOpenAIChat, source, "alias", webSearchCandidate("gpt-4o-search-preview"))
	if err != nil {
		t.Fatalf("to chat: %v", err)
	}
	if _, ok := payload["web_search_options"].(map[string]any); !ok {
		t.Fatalf("web_search_options missing: %#v", payload)
	}
}

func TestWebSearchDroppedWhenUpstreamUnsupported(t *testing.T) {
	t.Parallel()
	source := map[string]any{
		"model": "alias",
		"input": "hi",
		"tools": []any{
			map[string]any{"type": "function", "name": "f", "parameters": map[string]any{"type": "object"}},
			map[string]any{"type": "web_search"},
		},
	}
	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolAnthropicMessages, source, "alias", webSearchCandidate("deepseek-chat"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	tools, _ := payload["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %#v, want only function", tools)
	}
	payload, err = convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, source, "alias", webSearchCandidate("gpt-4o"))
	if err != nil {
		t.Fatalf("convert chat: %v", err)
	}
	if _, ok := payload["web_search_options"]; ok {
		t.Fatalf("web_search_options must not be sent to a non search model")
	}
}

func TestWebSearchGeminiKeepsFunctionsOnPreGemini3(t *testing.T) {
	t.Parallel()
	source := map[string]any{
		"model": "alias",
		"input": "hi",
		"tools": []any{
			map[string]any{"type": "function", "name": "f", "parameters": map[string]any{"type": "object"}},
			map[string]any{"type": "web_search"},
		},
	}
	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolGoogleGemini, source, "alias", webSearchCandidate("gemini-2.5-pro"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if findToolByKey(t, payload["tools"], "has", "googleSearch") != nil {
		t.Fatalf("googleSearch must be dropped next to function declarations on gemini-2.5")
	}
	payload, err = convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolGoogleGemini, source, "alias", webSearchCandidate("gemini-3-pro"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if findToolByKey(t, payload["tools"], "has", "googleSearch") == nil {
		t.Fatalf("googleSearch expected on gemini-3: %#v", payload["tools"])
	}
}

func TestWebSearchResponsesPassThroughKeepsRawTool(t *testing.T) {
	t.Parallel()
	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIResponses, map[string]any{
		"model": "alias",
		"input": "hi",
		"tools": []any{map[string]any{"type": "web_search_preview", "search_context_size": "high"}},
	}, "alias", webSearchCandidate("gpt-5"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	tool := findToolByKey(t, payload["tools"], "type", "web_search_preview")
	if tool == nil || tool["search_context_size"] != "high" {
		t.Fatalf("tools = %#v", payload["tools"])
	}
}

func decodeJSONMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, body)
	}
	return out
}

const anthropicSearchResponseBody = `{
  "id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-5","stop_reason":"end_turn",
  "content":[
    {"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"go 1.25 release"}},
    {"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[{"type":"web_search_result","url":"https://go.dev/doc","title":"Go Docs","encrypted_content":"abc"}]},
    {"type":"text","text":"Go 1.25 is out.","citations":[{"type":"web_search_result_location","url":"https://go.dev/doc","title":"Go Docs","cited_text":"Go 1.25"}]}
  ],
  "usage":{"input_tokens":10,"output_tokens":5}
}`

func TestWebSearchAnthropicResponseToResponsesAndChat(t *testing.T) {
	t.Parallel()
	body, _, err := convertResponseBetweenProtocols(canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIResponses, []byte(anthropicSearchResponseBody), responseConversionOptions{})
	if err != nil {
		t.Fatalf("to responses: %v", err)
	}
	out := decodeJSONMap(t, body)
	output, _ := out["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("output = %#v, want web_search_call + message", output)
	}
	call, _ := output[0].(map[string]any)
	action, _ := call["action"].(map[string]any)
	if call["type"] != "web_search_call" || action["query"] != "go 1.25 release" {
		t.Fatalf("call = %#v", call)
	}
	message, _ := output[1].(map[string]any)
	content, _ := message["content"].([]any)
	part, _ := content[0].(map[string]any)
	annotations, _ := part["annotations"].([]any)
	if len(annotations) != 1 {
		t.Fatalf("annotations = %#v", part["annotations"])
	}
	if ann, _ := annotations[0].(map[string]any); ann["url"] != "https://go.dev/doc" || ann["type"] != "url_citation" {
		t.Fatalf("annotation = %#v", ann)
	}

	body, _, err = convertResponseBetweenProtocols(canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIChat, []byte(anthropicSearchResponseBody), responseConversionOptions{})
	if err != nil {
		t.Fatalf("to chat: %v", err)
	}
	choices, _ := decodeJSONMap(t, body)["choices"].([]any)
	choice, _ := choices[0].(map[string]any)
	msg, _ := choice["message"].(map[string]any)
	annotations, _ = msg["annotations"].([]any)
	if len(annotations) != 1 {
		t.Fatalf("chat annotations = %#v", msg)
	}
}

func TestWebSearchAnthropicResponseToGeminiRoundTrip(t *testing.T) {
	t.Parallel()
	body, _, err := convertResponseBetweenProtocols(canonicalProtocolAnthropicMessages, canonicalProtocolGoogleGemini, []byte(anthropicSearchResponseBody), responseConversionOptions{})
	if err != nil {
		t.Fatalf("to gemini: %v", err)
	}
	candidates, _ := decodeJSONMap(t, body)["candidates"].([]any)
	candidate, _ := candidates[0].(map[string]any)
	grounding, _ := candidate["groundingMetadata"].(map[string]any)
	if grounding == nil {
		t.Fatalf("groundingMetadata missing: %#v", candidate)
	}
	if queries, _ := grounding["webSearchQueries"].([]any); len(queries) != 1 || queries[0] != "go 1.25 release" {
		t.Fatalf("queries = %#v", grounding["webSearchQueries"])
	}

	// Gemini -> Anthropic keeps queries and citations.
	back, _, err := convertResponseBetweenProtocols(canonicalProtocolGoogleGemini, canonicalProtocolAnthropicMessages, body, responseConversionOptions{})
	if err != nil {
		t.Fatalf("back to anthropic: %v", err)
	}
	content, _ := decodeJSONMap(t, back)["content"].([]any)
	types := []string{}
	for _, raw := range content {
		block, _ := raw.(map[string]any)
		types = append(types, anyString(block["type"]))
	}
	want := []string{"server_tool_use", "web_search_tool_result", "text"}
	if len(types) != len(want) {
		t.Fatalf("block types = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("block types = %v, want %v", types, want)
		}
	}
	textBlock, _ := content[2].(map[string]any)
	if citations, _ := textBlock["citations"].([]any); len(citations) != 1 {
		t.Fatalf("citations = %#v", textBlock["citations"])
	}
}

func TestWebSearchResponsesResponseToAnthropic(t *testing.T) {
	t.Parallel()
	responsesBody := `{"id":"resp_1","created_at":1,"model":"gpt-5","output":[
	  {"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"weather","sources":[{"type":"url","url":"https://w.example"}]}},
	  {"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Sunny","annotations":[{"type":"url_citation","url":"https://w.example","title":"W","start_index":0,"end_index":5}]}]}
	],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
	body, _, err := convertResponseBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolAnthropicMessages, []byte(responsesBody), responseConversionOptions{})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	content, _ := decodeJSONMap(t, body)["content"].([]any)
	if len(content) != 3 {
		t.Fatalf("content = %#v", content)
	}
	use, _ := content[0].(map[string]any)
	input, _ := use["input"].(map[string]any)
	if use["type"] != "server_tool_use" || input["query"] != "weather" {
		t.Fatalf("server_tool_use = %#v", use)
	}
	result, _ := content[1].(map[string]any)
	if results, _ := result["content"].([]any); len(results) != 1 || result["tool_use_id"] != use["id"] {
		t.Fatalf("tool_result = %#v", result)
	}
}

func TestWebSearchHistoryFoldsAcrossProtocols(t *testing.T) {
	t.Parallel()
	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolAnthropicMessages, map[string]any{
		"model": "alias",
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "q"},
			map[string]any{"type": "web_search_call", "id": "ws_1", "action": map[string]any{"type": "search", "query": "weather"}},
			map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "Sunny"}}},
			map[string]any{"type": "message", "role": "user", "content": "thanks"},
		},
	}, "alias", webSearchCandidate("claude-sonnet-4-5"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	raw, _ := json.Marshal(payload["messages"])
	if !json.Valid(raw) || !strings.Contains(string(raw), "[web_search] query: weather") {
		t.Fatalf("messages = %s, want folded search note", raw)
	}
}

func TestWebSearchSameProtocolAnthropicPassThrough(t *testing.T) {
	t.Parallel()
	tool := map[string]any{"type": "web_search_20250305", "name": "web_search", "max_uses": 5.0, "blocked_domains": []any{"bad.com"}}
	adapter := anthropicMessagesProtocolAdapter{state: &anthropicMessagesProtocolState{}}
	payload, err := adapter.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointMessages,
		Payload: map[string]any{
			"model":      "alias",
			"max_tokens": 10,
			"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
			"tools":      []any{tool},
		},
	}, webSearchCandidate("claude-sonnet-4-5"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	tools, _ := payload["tools"].([]any)
	got, _ := tools[0].(map[string]any)
	if got["type"] != "web_search_20250305" || got["max_uses"] != 5.0 {
		t.Fatalf("tools = %#v, want untouched native tool", tools)
	}
}

func anthropicSearchStreamBody() string {
	events := []string{
		gatewaySSEEvent("message_start", `{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5","usage":{"input_tokens":10}}}`),
		gatewaySSEEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{}}}`),
		gatewaySSEEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`),
		gatewaySSEEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"go news\"}"}}`),
		gatewaySSEEvent("content_block_stop", `{"type":"content_block_stop","index":0}`),
		gatewaySSEEvent("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[{"type":"web_search_result","url":"https://go.dev","title":"Go","encrypted_content":"x"}]}}`),
		gatewaySSEEvent("content_block_stop", `{"type":"content_block_stop","index":1}`),
		gatewaySSEEvent("content_block_start", `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`),
		gatewaySSEEvent("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Go is great."}}`),
		gatewaySSEEvent("content_block_delta", `{"type":"content_block_delta","index":2,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://go.dev","title":"Go","cited_text":"Go"}}}`),
		gatewaySSEEvent("content_block_stop", `{"type":"content_block_stop","index":2}`),
		gatewaySSEEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`),
		gatewaySSEEvent("message_stop", `{"type":"message_stop"}`),
	}
	return strings.Join(events, "")
}

func TestWebSearchStreamAnthropicToResponses(t *testing.T) {
	t.Parallel()
	rec, _, _, err := proxyCanonicalStreamTest(t, anthropicSearchStreamBody(), canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	out := rec.Body.String()
	for _, want := range []string{
		"response.web_search_call.searching",
		`"query":"go news"`,
		"response.output_text.annotation.added",
		`"type":"url_citation"`,
		`"https://go.dev"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stream output missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "event: response.web_search_call.completed") != 1 {
		t.Fatalf("expected exactly one search call:\n%s", out)
	}
	// Terminal response carries both the search item and the cited message.
	last := out[strings.LastIndex(out, "event: response.completed"):]
	if !strings.Contains(last, `"type":"web_search_call"`) || !strings.Contains(last, `"annotations":[{`) {
		t.Fatalf("completed response missing search/annotations:\n%s", last)
	}
}

func TestWebSearchStreamAnthropicToChatAndGemini(t *testing.T) {
	t.Parallel()
	rec, _, _, err := proxyCanonicalStreamTest(t, anthropicSearchStreamBody(), canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("chat stream: %v", err)
	}
	if out := rec.Body.String(); !strings.Contains(out, `"annotations":[{`) || !strings.Contains(out, `"url_citation"`) {
		t.Fatalf("chat stream missing annotations:\n%s", out)
	}
	rec, _, _, err = proxyCanonicalStreamTest(t, anthropicSearchStreamBody(), canonicalProtocolAnthropicMessages, canonicalProtocolGoogleGemini, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("gemini stream: %v", err)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "groundingMetadata") || !strings.Contains(out, "webSearchQueries") || !strings.Contains(out, "groundingSupports") {
		t.Fatalf("gemini stream missing grounding:\n%s", out)
	}
}

func TestWebSearchStreamResponsesToAnthropic(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_1","model":"gpt-5","created_at":1}}`),
		gatewaySSEEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"type":"web_search_call","id":"ws_1","status":"in_progress"}}`),
		gatewaySSEEvent("response.web_search_call.searching", `{"type":"response.web_search_call.searching","item_id":"ws_1","output_index":0}`),
		gatewaySSEEvent("response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"weather","sources":[{"type":"url","url":"https://w.example"}]}}}`),
		gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","delta":"Sunny"}`),
		gatewaySSEEvent("response.output_text.annotation.added", `{"type":"response.output_text.annotation.added","annotation":{"type":"url_citation","url":"https://w.example","title":"W","start_index":0,"end_index":5}}`),
		gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`),
	}, "")
	rec, _, _, err := proxyCanonicalStreamTest(t, body, canonicalProtocolOpenAIResponses, canonicalProtocolAnthropicMessages, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	out := rec.Body.String()
	for _, want := range []string{`"type":"server_tool_use"`, `\"query\":\"weather\"`, `"type":"web_search_tool_result"`, `"type":"citations_delta"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("anthropic stream missing %q:\n%s", want, out)
		}
	}
}

func TestWebSearchStreamGeminiGroundingToResponses(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		gatewaySSEEvent("", `{"candidates":[{"content":{"parts":[{"text":"Sunny today."}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}`),
		gatewaySSEEvent("", `{"candidates":[{"content":{"parts":[{"text":""}]},"finishReason":"STOP","groundingMetadata":{"webSearchQueries":["weather"],"groundingChunks":[{"web":{"uri":"https://w.example","title":"W"}}],"groundingSupports":[{"segment":{"startIndex":0,"endIndex":12,"text":"Sunny today."},"groundingChunkIndices":[0]}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}`),
	}, "")
	rec, _, _, err := proxyCanonicalStreamTest(t, body, canonicalProtocolGoogleGemini, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	out := rec.Body.String()
	for _, want := range []string{`"query":"weather"`, `"type":"url_citation"`, `"end_index":12`} {
		if !strings.Contains(out, want) {
			t.Fatalf("responses stream missing %q:\n%s", want, out)
		}
	}
}

func TestWebSearchUsageCounting(t *testing.T) {
	t.Parallel()
	_, usage, err := convertResponseBetweenProtocols(canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIResponses, []byte(anthropicSearchResponseBody), responseConversionOptions{})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if usage.WebSearchRequests != 1 {
		t.Fatalf("WebSearchRequests = %d, want 1", usage.WebSearchRequests)
	}
	_, capture, _, err := proxyCanonicalStreamTest(t, anthropicSearchStreamBody(), canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if capture.webSearchRequests != 1 {
		t.Fatalf("capture.webSearchRequests = %d, want 1", capture.webSearchRequests)
	}
	parsed := parseResponsesUsage([]byte(`{"output":[{"type":"web_search_call","id":"ws_1"},{"type":"web_search_call","id":"ws_2"}]}`))
	if parsed.WebSearchRequests != 2 {
		t.Fatalf("parseResponsesUsage = %d, want 2", parsed.WebSearchRequests)
	}
	body := `{"usage":{"input_tokens":1,"output_tokens":1,"server_tool_use":{"web_search_requests":3}}}`
	var root struct {
		Usage anthropicMessageUsageBody `json:"usage"`
	}
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		t.Fatal(err)
	}
	if got := gatewayUsageFromAnthropicUsage(root.Usage).WebSearchRequests; got != 3 {
		t.Fatalf("anthropic usage web_search_requests = %d, want 3", got)
	}
}

func TestWebSearchForcedToolChoiceMapsOrDrops(t *testing.T) {
	t.Parallel()
	source := map[string]any{
		"model":       "alias",
		"input":       "hi",
		"tools":       []any{map[string]any{"type": "web_search"}},
		"tool_choice": map[string]any{"type": "web_search"},
	}
	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolAnthropicMessages, source, "alias", webSearchCandidate("claude-sonnet-4-5"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if choice, _ := payload["tool_choice"].(map[string]any); choice["type"] != "tool" || choice["name"] != "web_search" {
		t.Fatalf("tool_choice = %#v, want forced web_search", payload["tool_choice"])
	}
	payload, err = convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolAnthropicMessages, source, "alias", webSearchCandidate("deepseek-chat"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if _, ok := payload["tool_choice"]; ok {
		t.Fatalf("tool_choice must be dropped with the search tool: %#v", payload["tool_choice"])
	}
	payload, err = convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, map[string]any{
		"model": "alias", "input": "hi", "tools": []any{map[string]any{"type": "web_search"}}, "tool_choice": "required",
	}, "alias", webSearchCandidate("gpt-4o"))
	if err != nil {
		t.Fatalf("convert chat: %v", err)
	}
	if _, ok := payload["tool_choice"]; ok {
		t.Fatalf("chat tool_choice must be omitted when no tools remain: %#v", payload)
	}
}
