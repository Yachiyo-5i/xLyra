package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestAnthropicMessagesProxyStreamDetectsMessageStop(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-8[1m]","content":[],"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":4}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")
	resp := gatewayStreamTestResponse(stream)
	recorder := httptest.NewRecorder()

	capture, started, err := (anthropicMessagesProtocolAdapter{}).ProxyStream(context.Background(), recorder, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started {
		t.Fatal("expected downstream response to start")
	}
	if !capture.streamCompleted || !capture.sawDone || capture.endReason != "done" {
		t.Fatalf("expected completed stream, got capture=%+v", capture)
	}
	if capture.usage.PromptTokens != 14 || capture.usage.CompletionTokens != 2 || capture.usage.TotalTokens != 16 || capture.usage.CachedPromptTokens != 4 {
		t.Fatalf("expected anthropic usage to be captured, got %+v", capture.usage)
	}
	assertGatewayBodyContainsAll(t, recorder.Body.String(), `"type":"message_stop"`)
}

func TestCanonicalResponseFromAnthropicMessagesBody(t *testing.T) {
	t.Parallel()

	response, err := canonicalResponseFromAnthropicMessagesBody(anthropicBufferedMessageBodyForTest())
	if err != nil {
		t.Fatalf("canonicalResponseFromAnthropicMessagesBody returned error: %v", err)
	}
	if response.ID != "msg_123" || response.Model != "claude-test" || response.FinishReason != "tool_calls" {
		t.Fatalf("unexpected response metadata: %+v", response)
	}
	if response.Usage.PromptTokens != 18 || response.Usage.CompletionTokens != 5 || response.Usage.TotalTokens != 23 || response.Usage.CachedPromptTokens != 3 {
		t.Fatalf("unexpected usage: %+v", response.Usage)
	}
	if len(response.Output) != 3 {
		t.Fatalf("expected three output items, got %#v", response.Output)
	}
	reasoning := response.Output[0]
	if reasoning.Type != "reasoning" || reasoning.ID != "thinking_msg_123" || len(reasoning.Thinking) != 1 {
		t.Fatalf("unexpected reasoning item: %#v", reasoning)
	}
	if reasoning.Thinking[0].Thinking != "private chain" || reasoning.Thinking[0].Signature != "sig_123" {
		t.Fatalf("unexpected thinking block: %#v", reasoning.Thinking[0])
	}
	message := response.Output[1]
	if message.Type != "message" || message.Role != "assistant" || message.Text != "visible answer" {
		t.Fatalf("unexpected message item: %#v", message)
	}
	if len(message.Content) != 1 || message.Content[0].Type != "output_text" || message.Content[0].Text != "visible answer" {
		t.Fatalf("unexpected message content: %#v", message.Content)
	}
	call := response.Output[2]
	if call.Type != "function_call" || call.CallID != "toolu_123" || call.Name != "lookup" || call.Arguments != `{"city":"Tokyo"}` {
		t.Fatalf("unexpected function call item: %#v", call)
	}
}

func TestAnthropicMessagesTransformBufferedResponseToResponses(t *testing.T) {
	t.Parallel()

	adapter := anthropicMessagesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}
	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, anthropicBufferedMessageBodyForTest())
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.StatusCode != http.StatusOK || transformed.ContentType != "application/json" {
		t.Fatalf("unexpected transformed metadata: %#v", transformed)
	}
	if transformed.Usage.PromptTokens != 18 || transformed.Usage.CompletionTokens != 5 || transformed.Usage.TotalTokens != 23 || transformed.Usage.CachedPromptTokens != 3 {
		t.Fatalf("unexpected usage: %+v", transformed.Usage)
	}

	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatalf("decode transformed response: %v; body=%s", err, string(transformed.Body))
	}
	if out["object"] != "response" || out["model"] != "claude-test" {
		t.Fatalf("unexpected response envelope: %#v", out)
	}
	output, ok := out["output"].([]any)
	if !ok || len(output) != 3 {
		t.Fatalf("expected three response output items, got %#v", out["output"])
	}
	for i, wantType := range []string{"reasoning", "message", "function_call"} {
		item, _ := output[i].(map[string]any)
		if item["type"] != wantType {
			t.Fatalf("output[%d] type = %#v, want %q; output=%#v", i, item["type"], wantType, output)
		}
	}
}

func TestAnthropicMessagesBridgesResponsesNamespaces(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"model":             "gpt-5.6",
		"max_output_tokens": 64,
		"tool_choice":       map[string]any{"type": "function", "name": "lookup", "namespace": "crm"},
		"tools": []any{
			map[string]any{
				"type":        "namespace",
				"name":        "crm",
				"description": "Customer records.",
				"tools": []any{map[string]any{
					"type":          "function",
					"name":          "lookup",
					"description":   "Find a customer.",
					"defer_loading": true,
					"parameters":    map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}},
				}},
			},
			map[string]any{
				"type":        "namespace",
				"name":        "support",
				"description": "Support tickets.",
				"tools": []any{map[string]any{
					"type":        "function",
					"name":        "lookup",
					"description": "Find a ticket.",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}},
				}},
			},
			map[string]any{"type": "tool_search"},
		},
		"input": []any{
			map[string]any{"type": "function_call", "call_id": "old_call", "name": "lookup", "namespace": "crm", "arguments": `{"id":"C1"}`},
			map[string]any{"type": "function_call_output", "call_id": "old_call", "output": "customer"},
			map[string]any{"type": "message", "role": "user", "content": "Find both records."},
		},
	}
	canonical, err := canonicalRequestFromOpenAIResponsesPayload(payload, "gpt-5.6")
	if err != nil {
		t.Fatalf("canonicalRequestFromOpenAIResponsesPayload returned error: %v", err)
	}
	adapter := newAnthropicMessagesProtocolAdapter(canonicalProtocolOpenAIResponses)
	upstream, err := adapter.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        payload,
		Canonical:      &canonical,
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "anthropic"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-20250514"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	tools := upstream["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %#v, want two flattened namespace functions", tools)
	}
	crmAlias := anyString(tools[0].(map[string]any)["name"])
	supportAlias := anyString(tools[1].(map[string]any)["name"])
	if crmAlias == "" || supportAlias == "" || crmAlias == supportAlias || crmAlias == "lookup" || supportAlias == "lookup" {
		t.Fatalf("namespace aliases = %q, %q, want distinct Messages-safe names", crmAlias, supportAlias)
	}
	if !strings.Contains(anyString(tools[0].(map[string]any)["description"]), "Namespace crm.") || !strings.Contains(anyString(tools[1].(map[string]any)["description"]), "Namespace support.") {
		t.Fatalf("flattened tool descriptions do not identify their namespaces: %#v", tools)
	}
	choice := upstream["tool_choice"].(map[string]any)
	if choice["type"] != "tool" || choice["name"] != crmAlias {
		t.Fatalf("tool_choice = %#v, want CRM alias %q", choice, crmAlias)
	}
	messages := upstream["messages"].([]any)
	historyCall := messages[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if historyCall["name"] != crmAlias {
		t.Fatalf("history tool name = %#v, want CRM alias %q", historyCall["name"], crmAlias)
	}

	body := []byte(fmt.Sprintf(`{"id":"msg_ns","model":"claude-test","role":"assistant","content":[{"type":"tool_use","id":"call_crm","name":%q,"input":{"id":"C1"}},{"type":"tool_use","id":"call_support","name":%q,"input":{"id":"T1"}}],"stop_reason":"tool_use","usage":{"input_tokens":4,"output_tokens":2}}`, crmAlias, supportAlias))
	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(transformed.Body, &response); err != nil {
		t.Fatalf("decode transformed response: %v", err)
	}
	output := response["output"].([]any)
	crmCall := output[0].(map[string]any)
	supportCall := output[1].(map[string]any)
	if crmCall["name"] != "lookup" || crmCall["namespace"] != "crm" {
		t.Fatalf("CRM call = %#v", crmCall)
	}
	if supportCall["name"] != "lookup" || supportCall["namespace"] != "support" {
		t.Fatalf("support call = %#v", supportCall)
	}

	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_ns_stream","model":"claude-test"}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_stream","name":"` + crmAlias + `","input":{}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"id\":\"C2\"}"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":4,"output_tokens":2}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
	recorder := httptest.NewRecorder()
	_, started, err := adapter.ProxyStream(context.Background(), recorder, gatewayStreamTestResponse(stream), time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || strings.Count(recorder.Body.String(), `"namespace":"crm"`) < 3 {
		t.Fatalf("stream did not restore namespace in all Responses items: %s", recorder.Body.String())
	}
}

func TestAnthropicMessagesResolvesStandardNamespaceToolChoice(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"model":             "gpt-5.6",
		"max_output_tokens": 64,
		"tool_choice":       map[string]any{"type": "function", "name": "lookup"},
		"tools": []any{map[string]any{
			"type": "namespace",
			"name": "crm",
			"tools": []any{map[string]any{
				"type":       "function",
				"name":       "lookup",
				"parameters": map[string]any{"type": "object"},
			}},
		}},
		"input": "Find the customer.",
	}
	canonical, err := canonicalRequestFromOpenAIResponsesPayload(payload, "gpt-5.6")
	if err != nil {
		t.Fatalf("canonicalRequestFromOpenAIResponsesPayload returned error: %v", err)
	}
	adapter := newAnthropicMessagesProtocolAdapter(canonicalProtocolOpenAIResponses)
	upstream, err := adapter.BuildUpstreamPayload(gatewayRequest{DownstreamPath: gatewayEndpointResponses, Payload: payload, Canonical: &canonical}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "anthropic"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-20250514"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	toolName := upstream["tools"].([]any)[0].(map[string]any)["name"]
	choice := upstream["tool_choice"].(map[string]any)
	if toolName == "lookup" || choice["name"] != toolName {
		t.Fatalf("tool_choice = %#v, tools = %#v", choice, upstream["tools"])
	}
}

func TestAnthropicMessagesRejectsAmbiguousNamespaceToolChoice(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"model":             "gpt-5.6",
		"max_output_tokens": 64,
		"tool_choice":       map[string]any{"type": "function", "name": "lookup"},
		"tools": []any{
			map[string]any{"type": "namespace", "name": "crm", "tools": []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}}}},
			map[string]any{"type": "namespace", "name": "support", "tools": []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}}}},
		},
		"input": "Find the record.",
	}
	canonical, err := canonicalRequestFromOpenAIResponsesPayload(payload, "gpt-5.6")
	if err != nil {
		t.Fatalf("canonicalRequestFromOpenAIResponsesPayload returned error: %v", err)
	}
	adapter := newAnthropicMessagesProtocolAdapter(canonicalProtocolOpenAIResponses)
	_, err = adapter.BuildUpstreamPayload(gatewayRequest{DownstreamPath: gatewayEndpointResponses, Payload: payload, Canonical: &canonical}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "anthropic"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-20250514"},
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous across 2 namespaces") {
		t.Fatalf("BuildUpstreamPayload error = %v", err)
	}
}

func TestAnthropicNamespaceToolAliasSkipsEveryOccupiedCandidate(t *testing.T) {
	t.Parallel()

	used := map[string]struct{}{}
	first := anthropicNamespaceToolAlias("crm", "lookup", used)
	used[first] = struct{}{}
	second := anthropicNamespaceToolAlias("crm", "lookup", used)
	used[second] = struct{}{}
	third := anthropicNamespaceToolAlias("crm", "lookup", used)
	if first == second || first == third || second == third {
		t.Fatalf("aliases were not unique: %q, %q, %q", first, second, third)
	}
}

func TestProviderAnthropicMessagesDirectPreservesToolRoundWithoutThinkingCache(t *testing.T) {
	t.Parallel()

	callID := "toolu_direct_k3"
	protocol := newProviderAnthropicMessagesProtocolAdapter("kimi_code", alternateProtocolDefinition{}, canonicalProtocolAnthropicMessages)
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointMessages,
		Payload: map[string]any{
			"model":      "claude-sonnet-4-5",
			"max_tokens": 1024,
			"messages": []any{
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "start"}}},
				map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": callID, "name": "Bash", "input": map[string]any{"command": "pwd"}}}},
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": callID, "content": "/repo"}}},
			},
		},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "kimi_code"},
		Model: routeengine.CandidateModel{UpstreamName: "k3"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "k3" {
		t.Fatalf("model = %#v, want k3", payload["model"])
	}
	messages := payload["messages"].([]any)
	assistantContent := messages[1].(map[string]any)["content"].([]any)
	userContent := messages[2].(map[string]any)["content"].([]any)
	if assistantContent[0].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("assistant tool round was degraded: %#v", messages)
	}
	if userContent[0].(map[string]any)["type"] != "tool_result" {
		t.Fatalf("user tool result was degraded: %#v", messages)
	}
}

func TestAnthropicMessagesTransformBufferedResponseToChat(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"id":"msg_chat",
		"type":"message",
		"role":"assistant",
		"model":"claude-test",
		"content":[
			{"type":"thinking","thinking":"private chain"},
			{"type":"text","text":"visible answer"}
		],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":4,"output_tokens":6}
	}`)
	adapter := anthropicMessagesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIChat}
	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatalf("decode transformed chat response: %v; body=%s", err, string(transformed.Body))
	}
	if out["object"] != "chat.completion" || transformed.Usage.TotalTokens != 10 {
		t.Fatalf("unexpected chat response metadata: out=%#v usage=%+v", out, transformed.Usage)
	}
	message := firstChatChoiceMessage(t, out)
	if message["content"] != "visible answer" || message["reasoning_content"] != "private chain" {
		t.Fatalf("unexpected chat message: %#v", message)
	}
}

func TestAnthropicMessagesTransformBufferedToolUseToChat(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"id":"msg_tool",
		"type":"message",
		"role":"assistant",
		"model":"claude-test",
		"content":[{"type":"tool_use","id":"toolu_456","name":"lookup","input":{"q":"weather"}}],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":7,"output_tokens":2}
	}`)
	adapter := anthropicMessagesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIChat}
	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatalf("decode transformed chat response: %v; body=%s", err, string(transformed.Body))
	}
	message := firstChatChoiceMessage(t, out)
	toolCalls, ok := message["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected one chat tool call, got %#v", message["tool_calls"])
	}
	toolCall, _ := toolCalls[0].(map[string]any)
	function, _ := toolCall["function"].(map[string]any)
	if toolCall["id"] != "toolu_456" || function["name"] != "lookup" || function["arguments"] != `{"q":"weather"}` {
		t.Fatalf("unexpected chat tool call: %#v", toolCall)
	}
}

func TestAnthropicMessagesTransformBufferedResponsePassesThroughErrors(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error":{"message":"bad request"}}`)
	adapter := anthropicMessagesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}
	transformed, err := adapter.TransformBufferedResponse(http.StatusBadRequest, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.StatusCode != http.StatusBadRequest || transformed.ContentType != "application/json" || string(transformed.Body) != string(body) {
		t.Fatalf("expected error passthrough, got %#v", transformed)
	}
	if transformed.Usage != (gatewayUsage{}) {
		t.Fatalf("expected no usage on passthrough error, got %+v", transformed.Usage)
	}
}

func TestProviderAnthropicMessagesAdapterWrapsTransformAndHeaders(t *testing.T) {
	t.Parallel()

	adapter := providerAnthropicMessagesProtocolAdapter{
		provider:           "deepseek",
		downstreamProtocol: canonicalProtocolOpenAIResponses,
	}
	if got := adapter.ProtocolName(); got != "deepseek_anthropic_messages_to_responses" {
		t.Fatalf("ProtocolName = %q", got)
	}

	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, anthropicBufferedMessageBodyForTest())
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.ContentType != "application/json" || transformed.Usage.TotalTokens != 23 {
		t.Fatalf("unexpected transformed provider response: %#v", transformed)
	}
	assertGatewayBodyContainsAll(t, string(transformed.Body), `"object":"response"`)

	req := httptest.NewRequest(http.MethodPost, gatewayEndpointMessages, nil)
	req.Header.Set("Authorization", "Bearer downstream")
	adapter.ApplyUpstreamHeaders(req, "upstream-key", "", false)
	if got := req.Header.Get("Authorization"); got != "Bearer downstream" {
		t.Fatalf("provider adapter should preserve Authorization header, got %q", got)
	}
	if got := req.Header.Get("x-api-key"); got != "upstream-key" {
		t.Fatalf("x-api-key = %q, want upstream-key", got)
	}
}

func TestAnthropicMessagesApplyUpstreamHeadersUsesAnthropicDefaults(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, gatewayEndpointMessages, nil)
	req.Header.Set("Authorization", "Bearer downstream")
	(anthropicMessagesProtocolAdapter{}).ApplyUpstreamHeaders(req, "upstream-key", "", false)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization header = %q, want removed", got)
	}
	if got := req.Header.Get("x-api-key"); got != "upstream-key" {
		t.Fatalf("x-api-key = %q, want upstream-key", got)
	}
	if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version = %q, want default", got)
	}

	req = httptest.NewRequest(http.MethodPost, gatewayEndpointMessages, nil)
	req.Header.Set("anthropic-version", "2024-01-01")
	(anthropicMessagesProtocolAdapter{}).ApplyUpstreamHeaders(req, "upstream-key", "", false)
	if got := req.Header.Get("anthropic-version"); got != "2024-01-01" {
		t.Fatalf("anthropic-version override = %q, want preserved", got)
	}
}

func TestAnthropicMessagesProtocolHelpers(t *testing.T) {
	t.Parallel()

	if got := encodeCanonicalToolChoiceAsAnthropic("auto").(map[string]any); got["type"] != "auto" {
		t.Fatalf("auto tool choice = %#v", got)
	}
	if got := encodeCanonicalToolChoiceAsAnthropic("none").(map[string]any); got["type"] != "none" {
		t.Fatalf("none tool choice = %#v", got)
	}
	if got := encodeCanonicalToolChoiceAsAnthropic("required").(map[string]any); got["type"] != "any" {
		t.Fatalf("required tool choice = %#v", got)
	}
	functionChoice := encodeCanonicalToolChoiceAsAnthropic(map[string]any{"type": "function", "name": "lookup"}).(map[string]any)
	if functionChoice["type"] != "tool" || functionChoice["name"] != "lookup" {
		t.Fatalf("function tool choice = %#v", functionChoice)
	}
	if got := encodeCanonicalToolChoiceAsAnthropic("bogus"); got != nil {
		t.Fatalf("invalid tool choice = %#v, want nil", got)
	}

	base64URL := anthropicImageURL(map[string]any{"type": "base64", "media_type": "image/jpeg", "data": " ZmFrZQ== "})
	if base64URL != "data:image/jpeg;base64,ZmFrZQ==" {
		t.Fatalf("base64 image URL = %q", base64URL)
	}
	if got := anthropicImageURL(map[string]any{"type": "base64", "data": "abc"}); got != "data:image/png;base64,abc" {
		t.Fatalf("default image media type URL = %q", got)
	}
	if got := anthropicImageURL(map[string]any{"type": "url", "url": " https://example.test/image.png "}); got != "https://example.test/image.png" {
		t.Fatalf("external image URL = %q", got)
	}
	source := anthropicImageSource("data:image/webp;base64,ZmFrZQ==")
	if source["type"] != "base64" || source["media_type"] != "image/webp" || source["data"] != "ZmFrZQ==" {
		t.Fatalf("data URL source = %#v", source)
	}
	source = anthropicImageSource("https://example.test/image.png")
	if source["type"] != "url" || source["url"] != "https://example.test/image.png" {
		t.Fatalf("external URL source = %#v", source)
	}

	if got := marshalJSONToString(nil); got != "{}" {
		t.Fatalf("empty raw JSON = %q", got)
	}
	if got := marshalJSONToString(json.RawMessage(`{"ok":true}`)); got != `{"ok":true}` {
		t.Fatalf("valid raw JSON = %q", got)
	}
	if got := marshalJSONToString(json.RawMessage(`{bad`)); got != "{}" {
		t.Fatalf("invalid raw JSON = %q", got)
	}

	if got := anthropicSystemText(" keep this "); got != "keep this" {
		t.Fatalf("string system text = %q", got)
	}
	system := []any{
		map[string]any{"type": "text", "text": "first"},
		map[string]any{"type": "image", "text": "ignored"},
		map[string]any{"type": "text", "text": " second "},
	}
	if got := anthropicSystemText(system); got != "first\n\nsecond" {
		t.Fatalf("block system text = %q", got)
	}
}

func anthropicBufferedMessageBodyForTest() []byte {
	return []byte(`{
		"id":"msg_123",
		"type":"message",
		"role":"assistant",
		"model":"claude-test",
		"content":[
			{"type":"thinking","thinking":"private chain","signature":"sig_123"},
			{"type":"text","text":"visible answer"},
			{"type":"tool_use","id":"toolu_123","name":"lookup","input":{"city":"Tokyo"}}
		],
		"stop_reason":"tool_use",
		"usage":{
			"input_tokens":13,
			"output_tokens":5,
			"cache_creation_input_tokens":2,
			"cache_read_input_tokens":3
		}
	}`)
}

func firstChatChoiceMessage(t *testing.T, out map[string]any) map[string]any {
	t.Helper()

	choices, ok := out["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("expected one chat choice, got %#v", out["choices"])
	}
	choice, _ := choices[0].(map[string]any)
	message, ok := choice["message"].(map[string]any)
	if !ok {
		t.Fatalf("expected chat choice message, got %#v", choice["message"])
	}
	return message
}
func TestAnthropicContentHasMeaningfulTextCoversNonTextAndWhitespaceBlocks(t *testing.T) {
	t.Parallel()

	if anthropicContentHasMeaningfulText([]any{
		"not-a-block",
		map[string]any{"type": "tool_use", "text": "ignored"},
		map[string]any{"type": " text ", "text": " \t\n "},
	}) {
		t.Fatal("expected non-text and whitespace-only text blocks to be ignored")
	}

	if !anthropicContentHasMeaningfulText([]any{
		map[string]any{"type": "text", "text": " visible "},
	}) {
		t.Fatal("expected non-empty text block to be meaningful")
	}
}
