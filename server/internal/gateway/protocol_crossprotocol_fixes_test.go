package gateway

import (
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
