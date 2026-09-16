package gateway

import (
	"encoding/json"
	"reflect"
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestChatToAnthropicForcedToolChoiceKeepsName(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "claude-opus-4-8"},
	}
	chatPayload := map[string]any{
		"model":      "claude-opus-4-8",
		"max_tokens": 128,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "lookup",
				"description": "Look something up",
				"parameters":  map[string]any{"type": "object"},
			},
		}},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "lookup"},
		},
	}

	out, err := convertRequestBetweenProtocols(
		canonicalProtocolOpenAIChat, canonicalProtocolAnthropicMessages,
		chatPayload, "claude-opus-4-8", candidate,
	)
	if err != nil {
		t.Fatalf("Chat → Anthropic conversion failed: %v", err)
	}
	choice, ok := out["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice missing or wrong type: %#v", out["tool_choice"])
	}
	if choice["type"] != "tool" {
		t.Errorf("tool_choice.type = %v, want tool", choice["type"])
	}
	if choice["name"] != "lookup" {
		t.Errorf("tool_choice.name = %v, want lookup", choice["name"])
	}
}

func TestChatToAnthropicNoArgToolGetsObjectSchema(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "claude-opus-4-8"},
	}
	chatPayload := map[string]any{
		"model":      "claude-opus-4-8",
		"max_tokens": 128,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "now",
				"description": "Current time, no args",
			},
		}},
	}

	out, err := convertRequestBetweenProtocols(
		canonicalProtocolOpenAIChat, canonicalProtocolAnthropicMessages,
		chatPayload, "claude-opus-4-8", candidate,
	)
	if err != nil {
		t.Fatalf("Chat → Anthropic conversion failed: %v", err)
	}
	tools, ok := out["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools missing: %#v", out["tools"])
	}
	tool0 := tools[0].(map[string]any)
	schema, ok := tool0["input_schema"].(map[string]any)
	if !ok {
		t.Fatalf("input_schema is not an object: %#v", tool0["input_schema"])
	}
	if schema["type"] != "object" {
		t.Errorf("input_schema.type = %v, want object", schema["type"])
	}
	if _, ok := schema["properties"].(map[string]any); !ok {
		t.Errorf("input_schema.properties missing: %#v", schema["properties"])
	}
}

func TestEncodeCanonicalToolsAsAnthropicSchemaFallback(t *testing.T) {
	t.Parallel()

	tools := encodeCanonicalToolsAsAnthropic([]canonicalTool{
		{Type: "function", Name: "a", Parameters: nil},
		{Type: "function", Name: "b", Parameters: map[string]any{}},
		{Type: "function", Name: "c", Parameters: map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}}},
	})
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
	for i, raw := range tools {
		tool := raw.(map[string]any)
		if tool["input_schema"] == nil {
			t.Fatalf("tool %d has null input_schema", i)
		}
	}
	if got := tools[2].(map[string]any)["input_schema"].(map[string]any)["properties"]; got == nil {
		t.Fatal("explicit schema properties dropped")
	}
}

func TestDeepSeekAnthropicMessagesDropsUnsupportedPatterns(t *testing.T) {
	t.Parallel()

	unsupported := `^(?!__.*__$)[^\p{Cc}\p{Cf}\p{Zl}\p{Zp}"\\./[\]]{1,200}$`
	payload := map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"metadata": map[string]any{"pattern": "["},
		"tools": []any{map[string]any{
			"name": "Artifact",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "pattern": unsupported},
					"slug": map[string]any{"type": "string", "pattern": "^[a-z]+$"},
					"nested": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string", "pattern": unsupported},
					},
				},
			},
		}},
	}
	adapter := newProviderAnthropicMessagesProtocolAdapter("deepseek", alternateProtocolDefinition{}, canonicalProtocolAnthropicMessages)
	payloadOut, err := adapter.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointMessages,
		Payload:        payload,
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-flash"}})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if !reflect.DeepEqual(payloadOut["metadata"], payload["metadata"]) {
		t.Fatal("non-schema data was changed")
	}

	tools := payloadOut["tools"].([]any)
	schema := tools[0].(map[string]any)["input_schema"].(map[string]any)
	properties := schema["properties"].(map[string]any)
	name := properties["name"].(map[string]any)
	if _, ok := name["pattern"]; ok {
		t.Fatalf("unsupported top-level pattern was retained: %#v", name)
	}
	slug := properties["slug"].(map[string]any)
	if slug["pattern"] != "^[a-z]+$" {
		t.Fatalf("supported pattern = %#v, want preserved pattern", slug["pattern"])
	}
	nestedItems := properties["nested"].(map[string]any)["items"].(map[string]any)
	if _, ok := nestedItems["pattern"]; ok {
		t.Fatalf("unsupported nested pattern was retained: %#v", nestedItems)
	}

	originalSchema := payload["tools"].([]any)[0].(map[string]any)["input_schema"].(map[string]any)
	originalName := originalSchema["properties"].(map[string]any)["name"].(map[string]any)
	if originalName["pattern"] != unsupported {
		t.Fatal("input payload was mutated")
	}
}

func TestNonDeepSeekAnthropicMessagesPreservesUnsupportedPatterns(t *testing.T) {
	t.Parallel()

	unsupported := `^(?!__.*__$)[a-z]+$`
	payload := map[string]any{
		"model":    "model",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools": []any{map[string]any{
			"name": "tool",
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"value": map[string]any{"type": "string", "pattern": unsupported}},
			},
		}},
	}
	adapter := newProviderAnthropicMessagesProtocolAdapter("other", alternateProtocolDefinition{}, canonicalProtocolAnthropicMessages)
	payloadOut, err := adapter.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointMessages,
		Payload:        payload,
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "model"}})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	pattern := payloadOut["tools"].([]any)[0].(map[string]any)["input_schema"].(map[string]any)["properties"].(map[string]any)["value"].(map[string]any)["pattern"]
	if pattern != unsupported {
		t.Fatalf("non-DeepSeek pattern = %#v, want preserved pattern", pattern)
	}
}

func TestDeepSeekPatternCleanupOnlyChangesSchemaConstraints(t *testing.T) {
	t.Parallel()

	raw := `{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "pattern": "["},
			"valid": {"pattern": "^\\p{L}+$"},
			"empty": {"pattern": ""},
			"nested": {"items": {"anyOf": [{"pattern": "["}, {"type": "null"}]}}
		},
		"$defs": {"pattern": {"pattern": "["}},
		"allOf": [{"properties": {"x": {"pattern": "["}}}],
		"required": ["pattern"],
		"additionalProperties": false,
		"default": {"pattern": "["},
		"examples": [{"pattern": "["}],
		"enum": [{"pattern": "["}],
		"const": {"pattern": "["}
	}`
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(raw), &want); err != nil {
		t.Fatal(err)
	}
	properties := want["properties"].(map[string]any)
	delete(properties["pattern"].(map[string]any), "pattern")
	delete(properties["nested"].(map[string]any)["items"].(map[string]any)["anyOf"].([]any)[0].(map[string]any), "pattern")
	delete(want["$defs"].(map[string]any)["pattern"].(map[string]any), "pattern")
	delete(want["allOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["x"].(map[string]any), "pattern")
	got := sanitizeDeepSeekAnthropicSchemaValue(schema)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema = %#v, want %#v", got, want)
	}
	var original map[string]any
	if err := json.Unmarshal([]byte(raw), &original); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(schema, original) {
		t.Fatal("input schema was mutated")
	}
}

func TestDeepSeekMessagesUpstreamDropsPatternsAfterConversion(t *testing.T) {
	t.Parallel()

	for _, downstream := range []canonicalProtocol{canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses} {
		t.Run(string(downstream), func(t *testing.T) {
			schema := map[string]any{"type": "string", "pattern": "["}
			request := gatewayRequest{
				DownstreamPath: protocolSpecs()[downstream].Path,
				Canonical: &canonicalRequest{
					SourceProtocol: downstream,
					Tools:          []canonicalTool{{Type: "function", Name: "Artifact", Parameters: schema}},
					Messages:       []canonicalMessage{{Type: "message", Role: "user", Content: []canonicalContentPart{{Type: "input_text", Text: "hi"}}}},
				},
			}
			candidate := routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "deepseek"}, Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-flash"}}
			adapter := newProviderAnthropicMessagesProtocolAdapter("deepseek", alternateProtocolDefinition{}, downstream)
			payload, err := adapter.BuildUpstreamPayload(request, candidate)
			if err != nil {
				t.Fatal(err)
			}
			got := payload["tools"].([]any)[0].(map[string]any)["input_schema"]
			if !reflect.DeepEqual(got, map[string]any{"type": "string"}) {
				t.Fatalf("schema = %#v, want pattern removed", got)
			}
			if schema["pattern"] != "[" {
				t.Fatal("canonical schema was mutated")
			}
			request.DownstreamPath = gatewayEndpointMessages
			chatPayload, err := (openAIChatProtocolAdapter{}).BuildUpstreamPayload(request, candidate)
			if err != nil {
				t.Fatal(err)
			}
			chatSchema := chatPayload["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["parameters"]
			if !reflect.DeepEqual(chatSchema, schema) {
				t.Fatalf("Chat Completions upstream schema changed: %#v", chatSchema)
			}
		})
	}
}

func TestAnthropicToolResultContentPreservesBlocks(t *testing.T) {
	t.Parallel()

	blocks := []any{
		map[string]any{"type": "text", "text": "see image"},
		map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "abc"}},
	}
	got := anthropicToolResultContent(canonicalMessage{
		Type:       "function_call_output",
		Output:     "flattened",
		RawContent: blocks,
	})
	arr, ok := got.([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("expected structured blocks preserved, got %#v", got)
	}

	str := anthropicToolResultContent(canonicalMessage{Type: "function_call_output", Output: "plain"})
	if str != "plain" {
		t.Fatalf("expected plain string output, got %#v", str)
	}
}

func TestAnthropicSystemValuePrefersStructuredBlocks(t *testing.T) {
	t.Parallel()

	system := []any{
		map[string]any{"type": "text", "text": "You are helpful.", "cache_control": map[string]any{"type": "ephemeral"}},
	}
	got := anthropicSystemValue(canonicalRequest{RawSystem: system, Instructions: "You are helpful."})
	arr, ok := got.([]any)
	if !ok || len(arr) != 1 {
		t.Fatalf("expected structured system preserved, got %#v", got)
	}
	block := arr[0].(map[string]any)
	if block["cache_control"] == nil {
		t.Fatal("cache_control dropped from structured system")
	}

	if got := anthropicSystemValue(canonicalRequest{Instructions: "plain"}); got != "plain" {
		t.Fatalf("expected flattened instructions, got %#v", got)
	}
}
