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

func TestDeepSeekPatternCleanupAppliesOnGenericSitesWithoutRetargeting(t *testing.T) {
	t.Parallel()

	unsupported := `^(?!__.*__$)[^\p{Cc}\p{Cf}\p{Zl}\p{Zp}"\\./[\]]{1,200}$`
	for _, tc := range []struct {
		siteType string
		baseURL  string
	}{
		{"newapi", "https://relay.example.test"},
		{"openai", "https://openai-relay.example.test"},
		{"oneapi", "https://oneapi.example.test"},
	} {
		t.Run(tc.siteType, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{
				"model":    "deepseek-v4-flash",
				"messages": []any{map[string]any{"role": "user", "content": "hi"}},
				"tools": []any{map[string]any{
					"name": "Artifact",
					"input_schema": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name": map[string]any{"type": "string", "pattern": unsupported},
							"slug": map[string]any{"type": "string", "pattern": "^[a-z]+$"},
						},
					},
				}},
			}
			candidate := routeengine.Candidate{
				Site: routeengine.CandidateSite{SiteType: tc.siteType, BaseURL: tc.baseURL},
				Model: routeengine.CandidateModel{
					UpstreamName:           "deepseek-v4-flash",
					SupportedEndpointTypes: []string{"anthropic-messages"},
				},
			}
			request := gatewayRequest{DownstreamPath: gatewayEndpointMessages, Payload: payload}
			protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), request, candidate)
			if err != nil {
				t.Fatal(err)
			}
			if got := protocol.ProtocolName(); got != "anthropic_messages" {
				t.Fatalf("protocol = %q, want generic anthropic adapter", got)
			}
			if got := protocol.UpstreamPath(tc.baseURL); got != tc.baseURL+"/v1/messages" {
				t.Fatalf("UpstreamPath = %q, want site messages URL", got)
			}
			payloadOut, err := protocol.BuildUpstreamPayload(request, candidate)
			if err != nil {
				t.Fatal(err)
			}
			properties := payloadOut["tools"].([]any)[0].(map[string]any)["input_schema"].(map[string]any)["properties"].(map[string]any)
			if _, ok := properties["name"].(map[string]any)["pattern"]; ok {
				t.Fatalf("unsupported pattern kept on %s", tc.siteType)
			}
			if properties["slug"].(map[string]any)["pattern"] != "^[a-z]+$" {
				t.Fatalf("supported pattern = %#v", properties["slug"])
			}
		})
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

func TestAnthropicToolInputSchemaFlattensTopLevelCombinators(t *testing.T) {
	t.Parallel()

	nested := map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "number"}}}
	original := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"mode": nested,
			"a":    map[string]any{"type": "string", "title": "top"},
		},
		"required": []any{"z"},
		"anyOf": []any{
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"a": map[string]any{"type": "number", "title": "branch"}, "b": map[string]any{"type": "string"}},
				"required":   []any{"a", "b"},
			},
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"c": map[string]any{"type": "boolean"}},
				"required":   []any{"a"},
			},
		},
		"oneOf": []any{
			map[string]any{"type": "object", "properties": map[string]any{"d": map[string]any{"type": "string"}}, "required": []any{"d", "e"}},
			map[string]any{"type": "object", "properties": map[string]any{"e": map[string]any{"type": "string"}}, "required": []any{"d"}},
		},
		"allOf": []any{
			map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}, "required": []any{"x"}},
			map[string]any{"type": "object", "properties": map[string]any{"y": map[string]any{"type": "string"}}, "required": []any{"y"}},
		},
		"not":         map[string]any{"type": "null"},
		"enum":        []any{"nope"},
		"description": "kept",
		"$defs":       map[string]any{"Foo": map[string]any{"type": "string"}},
		"definitions": map[string]any{"Bar": map[string]any{"type": "number"}},
	}
	got := anthropicToolInputSchema(original).(map[string]any)
	if _, ok := original["anyOf"]; !ok || original["properties"].(map[string]any)["a"].(map[string]any)["title"] != "top" {
		t.Fatalf("input schema was mutated: %#v", original)
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf", "not", "enum"} {
		if _, ok := got[key]; ok {
			t.Fatalf("top-level %s kept: %#v", key, got[key])
		}
	}
	if got["type"] != "object" || got["description"] != "kept" {
		t.Fatalf("rewritten schema = %#v", got)
	}
	props := got["properties"].(map[string]any)
	if props["a"].(map[string]any)["title"] != "top" {
		t.Fatalf("top-level property lost to a branch: %#v", props["a"])
	}
	if props["b"] == nil || props["c"] == nil || props["d"] == nil || props["e"] == nil || props["x"] == nil || props["y"] == nil {
		t.Fatalf("branch properties = %#v", props)
	}
	if _, ok := props["mode"].(map[string]any)["anyOf"]; !ok {
		t.Fatalf("nested anyOf was rewritten: %#v", props["mode"])
	}
	if !schemaStringSetEqual(got["required"], "z", "a", "d", "x", "y") {
		t.Fatalf("required = %#v", got["required"])
	}
	if got["$defs"] == nil || got["definitions"] == nil {
		t.Fatalf("defs dropped: %#v", got)
	}

	allOfOnly := anthropicToolInputSchema(map[string]any{
		"type":     "object",
		"required": []any{"z"},
		"allOf": []any{
			map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}, "required": []any{"x"}},
			map[string]any{"type": "object", "properties": map[string]any{"y": map[string]any{"type": "string"}}, "required": []any{"y"}},
		},
	}).(map[string]any)
	if !schemaStringSetEqual(allOfOnly["required"], "z", "x", "y") {
		t.Fatalf("allOf required = %#v", allOfOnly["required"])
	}

	oneOfOnly := anthropicToolInputSchema(map[string]any{
		"oneOf": []any{
			map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a", "b"}},
			map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"type": "string"}}, "required": []any{"a"}},
		},
	}).(map[string]any)
	if !schemaStringSetEqual(oneOfOnly["required"], "a") {
		t.Fatalf("oneOf required = %#v", oneOfOnly["required"])
	}
	if _, ok := oneOfOnly["properties"].(map[string]any)["a"]; !ok {
		t.Fatalf("oneOf properties = %#v", oneOfOnly["properties"])
	}

	degenerate := anthropicToolInputSchema(map[string]any{
		"description": "drop",
		"properties":  map[string]any{"keep": map[string]any{"type": "string"}},
		"anyOf":       []any{map[string]any{"type": "string"}, map[string]any{"type": "number"}},
		"$defs":       map[string]any{"Foo": map[string]any{"type": "string"}},
		"definitions": map[string]any{"Bar": map[string]any{"type": "number"}},
	}).(map[string]any)
	if degenerate["type"] != "object" || degenerate["additionalProperties"] != true || degenerate["description"] != nil || degenerate["anyOf"] != nil {
		t.Fatalf("degenerate schema = %#v", degenerate)
	}
	if degenerate["properties"].(map[string]any)["keep"] == nil || degenerate["$defs"] == nil || degenerate["definitions"] == nil {
		t.Fatalf("degenerate schema dropped retained fields: %#v", degenerate)
	}
	empty := anthropicToolInputSchema(map[string]any{
		"anyOf": []any{map[string]any{"type": "string"}},
	}).(map[string]any)
	if empty["additionalProperties"] != true || len(empty["properties"].(map[string]any)) != 0 || empty["type"] != "object" {
		t.Fatalf("empty degenerate schema = %#v", empty)
	}

	encoded := encodeCanonicalToolsAsAnthropic([]canonicalTool{{
		Type:       "function",
		Name:       "mcp",
		Parameters: map[string]any{"anyOf": []any{map[string]any{"type": "string"}}},
	}})
	encodedSchema := encoded[0].(map[string]any)["input_schema"].(map[string]any)
	if _, ok := encodedSchema["anyOf"]; ok || encodedSchema["additionalProperties"] != true {
		t.Fatalf("encodeCanonicalToolsAsAnthropic schema = %#v", encodedSchema)
	}
	chat := encodeCanonicalToolsAsChat([]canonicalTool{{
		Type:       "function",
		Name:       "mcp",
		Parameters: map[string]any{"anyOf": []any{map[string]any{"type": "string"}}},
	}}).([]any)
	chatSchema := chat[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	if _, ok := chatSchema["anyOf"]; !ok {
		t.Fatalf("chat parameters were rewritten: %#v", chatSchema)
	}

	rawSchema := map[string]any{"anyOf": []any{map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}, "required": []any{"q"}}}}
	payload := map[string]any{
		"model":    "claude-sonnet-4-5",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"tools":    []any{map[string]any{"name": "mcp", "input_schema": rawSchema}},
	}
	finished := finishAnthropicUpstreamPayload(payload, gatewayRequest{DownstreamPath: gatewayEndpointMessages}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-5"},
	})
	finishedSchema := finished["tools"].([]any)[0].(map[string]any)["input_schema"].(map[string]any)
	if _, ok := rawSchema["anyOf"]; !ok {
		t.Fatal("passthrough input_schema was mutated")
	}
	if _, ok := finishedSchema["anyOf"]; ok || finishedSchema["properties"].(map[string]any)["q"] == nil {
		t.Fatalf("passthrough schema = %#v", finishedSchema)
	}
}

func schemaStringSetEqual(value any, want ...string) bool {
	got := map[string]struct{}{}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			got[anyString(item)] = struct{}{}
		}
	case []string:
		for _, item := range typed {
			got[item] = struct{}{}
		}
	default:
		return false
	}
	if len(got) != len(want) {
		return false
	}
	for _, item := range want {
		if _, ok := got[item]; !ok {
			return false
		}
	}
	return true
}
