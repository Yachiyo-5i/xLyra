package gateway

import (
	"strings"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

const codexRequestPolicyModel = "gpt-5.4-codex"

func codexRequestPolicyCandidate() routeengine.Candidate {
	return routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: codexRequestPolicyModel},
	}
}

// TestApplyCodexRequestPolicy_FromSpecs validates that Codex request policy
// behavior is fully driven by protocol_specs.json (the codex provider entry),
// not hardcoded. If someone adds a hardcoded delete/force back into
// applyCodexRequestPolicy this test should still pass — but if the specs.json
// entry is removed or changed, this test will detect the drift.
func TestApplyCodexRequestPolicy_FromSpecs(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"model":             codexRequestPolicyModel,
		"temperature":       0.5,
		"top_p":             0.9,
		"top_k":             40,
		"max_output_tokens": 1024,
		"stream_options":    map[string]any{"include_usage": true},
		"metadata":          map[string]any{"trace": "x"},
		"user":              "user-1",
		"input":             "hello",
	}

	result := applyCodexRequestPolicy(payload, codexRequestPolicyCandidate())

	// All unsupported fields should be removed
	for _, key := range []string{
		"temperature", "top_p", "top_k",
		"max_output_tokens", "stream_options",
		"metadata", "user",
	} {
		if _, ok := result[key]; ok {
			t.Errorf("expected %q to be removed by codex policy, got %#v", key, result[key])
		}
	}

	// Forced fields should be applied
	if result["stream"] != true {
		t.Errorf("expected stream=true, got %#v", result["stream"])
	}
	if result["store"] != false {
		t.Errorf("expected store=false, got %#v", result["store"])
	}
	if result["parallel_tool_calls"] != true {
		t.Errorf("expected parallel_tool_calls=true, got %#v", result["parallel_tool_calls"])
	}

	// include should be set to encrypted_content
	include := result["include"]
	if include == nil {
		t.Errorf("expected include to be set, got nil")
	} else {
		found := false
		switch typed := include.(type) {
		case []any:
			for _, item := range typed {
				if str, ok := item.(string); ok && str == "reasoning.encrypted_content" {
					found = true
					break
				}
			}
		case []string:
			for _, str := range typed {
				if str == "reasoning.encrypted_content" {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("expected include to contain reasoning.encrypted_content, got %#v", include)
		}
	}

	// Structural normalization: input should be converted to []any
	if _, ok := result["input"].([]any); !ok {
		t.Errorf("expected input to be []any after normalization, got %T", result["input"])
	}

	// Structural normalization: instructions should be set to ""
	if val, ok := result["instructions"]; !ok || val != "" {
		t.Errorf("expected instructions to be empty string, got %#v", val)
	}
}

func TestApplyCodexRequestPolicy_ServiceTierAllowedValues(t *testing.T) {
	t.Parallel()

	candidate := codexRequestPolicyCandidate()

	// priority is allowed for legacy compatibility.
	payload := map[string]any{
		"model":        codexRequestPolicyModel,
		"service_tier": "priority",
		"input":        "test",
	}
	result := applyCodexRequestPolicy(payload, candidate)
	if result["service_tier"] != "priority" {
		t.Errorf("expected service_tier=priority to be preserved, got %#v", result["service_tier"])
	}

	// fast is the documented Codex fast mode value.
	payloadFast := map[string]any{
		"model":        codexRequestPolicyModel,
		"service_tier": "fast",
		"input":        "test",
	}
	resultFast := applyCodexRequestPolicy(payloadFast, candidate)
	if resultFast["service_tier"] != "fast" {
		t.Errorf("expected service_tier=fast to be preserved, got %#v", resultFast["service_tier"])
	}

	// default is not allowed - should be removed
	payload2 := map[string]any{
		"model":        codexRequestPolicyModel,
		"service_tier": "default",
		"input":        "test",
	}
	result2 := applyCodexRequestPolicy(payload2, candidate)
	if _, ok := result2["service_tier"]; ok {
		t.Errorf("expected service_tier=default to be removed (not in allowed_values), got %#v", result2["service_tier"])
	}
}

func TestApplyCodexRequestPolicy_StructuralNormalization(t *testing.T) {
	t.Parallel()

	candidate := codexRequestPolicyCandidate()

	// system role should be converted to developer
	payload := map[string]any{
		"model": codexRequestPolicyModel,
		"input": []any{
			map[string]any{
				"role":    "system",
				"content": "you are helpful",
			},
		},
	}
	result := applyCodexRequestPolicy(payload, candidate)
	input, ok := result["input"].([]any)
	if !ok || len(input) == 0 {
		t.Fatalf("expected input to be []any, got %#v", result["input"])
	}
	msg, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("expected input[0] to be map, got %T", input[0])
	}
	if msg["role"] != "developer" {
		t.Errorf("expected system role converted to developer, got %#v", msg["role"])
	}

	// builtin tool type renames
	payload2 := map[string]any{
		"model": codexRequestPolicyModel,
		"input": "test",
		"tools": []any{
			map[string]any{"type": "web_search_preview"},
			map[string]any{"type": "computer_use_preview"},
		},
	}
	result2 := applyCodexRequestPolicy(payload2, candidate)
	tools, ok := result2["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %#v", result2["tools"])
	}
	if t0 := tools[0].(map[string]any)["type"]; t0 != "web_search" {
		t.Errorf("expected web_search_preview -> web_search, got %#v", t0)
	}
	if t1 := tools[1].(map[string]any)["type"]; t1 != "computer_use" {
		t.Errorf("expected computer_use_preview -> computer_use, got %#v", t1)
	}
}

func TestApplyCodexRequestPolicy_NormalizesInputItemIDs(t *testing.T) {
	t.Parallel()

	nativeReasoning := map[string]any{
		"type":              "reasoning",
		"id":                "rs_native",
		"encrypted_content": "native-encrypted",
	}
	encryptedReasoning := map[string]any{
		"type":              "reasoning",
		"id":                "item_encrypted",
		"encrypted_content": "portable-encrypted",
	}
	foreignReasoning := map[string]any{
		"type":    "reasoning",
		"id":      "item_de7faf8adc13085119b67ddb",
		"summary": []any{map[string]any{"type": "summary_text", "text": "summary"}},
	}
	staleNativeReasoning := map[string]any{
		"type":    "reasoning",
		"id":      "rs_stale",
		"summary": []any{map[string]any{"type": "summary_text", "text": "stale"}},
	}
	message := map[string]any{
		"type":    "message",
		"id":      "item_message",
		"role":    "assistant",
		"content": []any{},
	}
	toolCall := map[string]any{
		"type":    "custom_tool_call",
		"id":      "item_tool",
		"call_id": "call_1",
		"name":    "shell",
		"input":   "pwd",
	}
	payload := map[string]any{
		"model": codexRequestPolicyModel,
		"input": []any{nativeReasoning, encryptedReasoning, foreignReasoning, staleNativeReasoning, message, toolCall},
	}

	result := applyCodexRequestPolicy(payload, codexRequestPolicyCandidate())
	input, ok := result["input"].([]any)
	if !ok || len(input) != 4 {
		t.Fatalf("input = %#v, want four portable items", result["input"])
	}
	if got := input[0].(map[string]any)["id"]; got != "rs_native" {
		t.Fatalf("native reasoning id = %#v, want rs_native", got)
	}
	if _, ok := input[1].(map[string]any)["id"]; ok {
		t.Fatalf("encrypted reasoning should not keep generated id: %#v", input[1])
	}
	if got := input[1].(map[string]any)["encrypted_content"]; got != "portable-encrypted" {
		t.Fatalf("encrypted reasoning content = %#v", got)
	}
	if _, ok := input[2].(map[string]any)["id"]; ok {
		t.Fatalf("message should not keep generated id: %#v", input[2])
	}
	if _, ok := input[3].(map[string]any)["id"]; ok {
		t.Fatalf("tool call should not keep generated id: %#v", input[3])
	}
	if got := input[3].(map[string]any)["call_id"]; got != "call_1" {
		t.Fatalf("tool call id = %#v, want call_1", got)
	}
	if encryptedReasoning["id"] != "item_encrypted" || message["id"] != "item_message" || toolCall["id"] != "item_tool" {
		t.Fatalf("original input items were mutated")
	}
}

func TestResponsesItemIDsUseCodexTypePrefixes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		itemType string
		prefix   string
	}{
		{itemType: "additional_tools", prefix: "at_"},
		{itemType: "message", prefix: "msg_"},
		{itemType: "agent_message", prefix: "amsg_"},
		{itemType: "reasoning", prefix: "rs_"},
		{itemType: "local_shell_call", prefix: "lsh_"},
		{itemType: "function_call", prefix: "fc_"},
		{itemType: "tool_search_call", prefix: "tsc_"},
		{itemType: "function_call_output", prefix: "fco_"},
		{itemType: "custom_tool_call", prefix: "ctc_"},
		{itemType: "custom_tool_call_output", prefix: "ctco_"},
		{itemType: "tool_search_output", prefix: "tso_"},
		{itemType: "web_search_call", prefix: "ws_"},
		{itemType: "image_generation_call", prefix: "ig_"},
		{itemType: "compaction", prefix: "cmp_"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.itemType, func(t *testing.T) {
			if got := responsesItemIDForType(tt.itemType, "foreign_value", "call_value"); got != tt.prefix+"call_value" {
				t.Fatalf("responsesItemIDForType() = %q, want %q", got, tt.prefix+"call_value")
			}
			if !responsesItemIDMatchesType(tt.itemType, tt.prefix+"native") {
				t.Fatalf("responsesItemIDMatchesType() rejected %q", tt.prefix+"native")
			}
		})
	}

	if got := responsesItemIDForType("context_compaction", "", "context"); got != "cmp_context" {
		t.Fatalf("context compaction ID = %q, want cmp_context", got)
	}
	if responsesItemIDMatchesType("compaction_trigger", "cmp_trigger") {
		t.Fatal("compaction_trigger must not accept an item ID")
	}
	if responsesItemIDMatchesType("function_call", "fc_") {
		t.Fatal("an item ID with only the function prefix must be rejected")
	}
}

func TestApplyCodexRequestPolicyRemovesMismatchedKnownItemIDs(t *testing.T) {
	t.Parallel()

	input := make([]any, 0, 4)
	for _, item := range []map[string]any{
		{"type": "message", "id": "fc_message", "role": "assistant", "content": []any{}},
		{"type": "function_call", "id": "ctc_call", "call_id": "call_1", "name": "lookup", "arguments": "{}"},
		{"type": "custom_tool_call", "id": "fc_custom", "call_id": "call_2", "name": "apply_patch", "input": "patch"},
		{"type": "function_call_output", "id": "ctco_output", "call_id": "call_2", "output": "ok"},
	} {
		input = append(input, item)
	}
	payload := map[string]any{"model": codexRequestPolicyModel, "input": input}
	result := applyCodexRequestPolicy(payload, codexRequestPolicyCandidate())
	items := result["input"].([]any)
	for index, raw := range items {
		item := raw.(map[string]any)
		if _, ok := item["id"]; ok {
			t.Fatalf("item %d kept mismatched ID: %#v", index, item)
		}
	}
	if got := items[1].(map[string]any)["call_id"]; got != "call_1" {
		t.Fatalf("function call correlation ID = %#v, want call_1", got)
	}
}

func TestApplyCodexRequestPolicyShortensOnlyLongCallIDs(t *testing.T) {
	t.Parallel()

	shortCallID := strings.Repeat("s", codexResponsesMaxCallIDLength)
	longCallID := strings.Repeat("l", codexResponsesMaxCallIDLength+22)
	payload := map[string]any{
		"model": codexRequestPolicyModel,
		"input": []any{
			map[string]any{"type": "function_call", "call_id": shortCallID, "name": "lookup", "arguments": "{}"},
			map[string]any{"type": "function_call", "call_id": longCallID, "name": "lookup", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": longCallID, "output": "ok"},
			map[string]any{"type": "custom_tool_call", "call_id": longCallID, "name": "exec", "input": "pwd"},
			map[string]any{"type": "custom_tool_call_output", "call_id": longCallID, "output": "ok"},
		},
	}
	originalInput := payload["input"].([]any)

	result := applyCodexRequestPolicy(payload, codexRequestPolicyCandidate())
	input := result["input"].([]any)
	if got := input[0].(map[string]any)["call_id"]; got != shortCallID {
		t.Fatalf("short call_id = %#v, want unchanged %q", got, shortCallID)
	}
	shortened, _ := input[1].(map[string]any)["call_id"].(string)
	if len(shortened) > codexResponsesMaxCallIDLength || shortened == longCallID {
		t.Fatalf("long call_id = %q, want a shortened ID", shortened)
	}
	for index := 2; index < len(input); index++ {
		if got := input[index].(map[string]any)["call_id"]; got != shortened {
			t.Fatalf("input[%d].call_id = %#v, want %q", index, got, shortened)
		}
	}
	if got := originalInput[1].(map[string]any)["call_id"]; got != longCallID {
		t.Fatalf("caller payload call_id = %#v, want unchanged %q", got, longCallID)
	}
	second := applyCodexRequestPolicy(map[string]any{
		"model": codexRequestPolicyModel,
		"input": []any{map[string]any{"type": "function_call", "call_id": longCallID, "name": "lookup", "arguments": "{}"}},
	}, codexRequestPolicyCandidate())
	if got := second["input"].([]any)[0].(map[string]any)["call_id"]; got != shortened {
		t.Fatalf("replayed call_id = %#v, want stable %q", got, shortened)
	}
}

func TestApplyCodexRequestPolicyCachesCollisionMappings(t *testing.T) {
	t.Parallel()

	longCallID := strings.Repeat("l", codexResponsesMaxCallIDLength+22) + "_collision"
	baseCallID := codexShortCallID(longCallID, 0)
	scope := "collision-cache-test"
	first := applyCodexRequestPolicyWithCallIDMappingScope(map[string]any{
		"model": codexRequestPolicyModel,
		"input": []any{
			map[string]any{"type": "function_call", "call_id": baseCallID, "name": "existing", "arguments": "{}"},
			map[string]any{"type": "function_call", "call_id": longCallID, "name": "lookup", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": longCallID, "output": "ok"},
		},
	}, codexRequestPolicyCandidate(), scope)
	firstInput := first["input"].([]any)
	mappedCallID := firstInput[1].(map[string]any)["call_id"].(string)
	if mappedCallID == baseCallID || len(mappedCallID) > codexResponsesMaxCallIDLength {
		t.Fatalf("collision call_id = %q, want a different valid ID", mappedCallID)
	}
	if got := firstInput[2].(map[string]any)["call_id"]; got != mappedCallID {
		t.Fatalf("function output call_id = %#v, want %q", got, mappedCallID)
	}

	second := applyCodexRequestPolicyWithCallIDMappingScope(map[string]any{
		"model": codexRequestPolicyModel,
		"input": []any{
			map[string]any{"type": "function_call", "call_id": longCallID, "name": "lookup", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": longCallID, "output": "ok"},
		},
	}, codexRequestPolicyCandidate(), scope)
	for index, rawItem := range second["input"].([]any) {
		if got := rawItem.(map[string]any)["call_id"]; got != mappedCallID {
			t.Fatalf("replayed input[%d].call_id = %#v, want cached %q", index, got, mappedCallID)
		}
	}
}

func TestApplyCodexRequestPolicyReplacesCachedMappingWhenItBecomesOccupied(t *testing.T) {
	t.Parallel()

	longCallID := strings.Repeat("l", codexResponsesMaxCallIDLength+22) + "_cache_refresh"
	baseCallID := codexShortCallID(longCallID, 0)
	scope := "collision-cache-refresh-test"
	first := applyCodexRequestPolicyWithCallIDMappingScope(map[string]any{
		"model": codexRequestPolicyModel,
		"input": []any{
			map[string]any{"type": "function_call", "call_id": baseCallID, "name": "existing", "arguments": "{}"},
			map[string]any{"type": "function_call", "call_id": longCallID, "name": "lookup", "arguments": "{}"},
		},
	}, codexRequestPolicyCandidate(), scope)
	firstMappedCallID := first["input"].([]any)[1].(map[string]any)["call_id"].(string)

	second := applyCodexRequestPolicyWithCallIDMappingScope(map[string]any{
		"model": codexRequestPolicyModel,
		"input": []any{
			map[string]any{"type": "function_call", "call_id": firstMappedCallID, "name": "new_existing", "arguments": "{}"},
			map[string]any{"type": "function_call", "call_id": longCallID, "name": "lookup", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": longCallID, "output": "ok"},
		},
	}, codexRequestPolicyCandidate(), scope)
	secondInput := second["input"].([]any)
	if got := secondInput[0].(map[string]any)["call_id"]; got != firstMappedCallID {
		t.Fatalf("new short call_id = %#v, want unchanged %q", got, firstMappedCallID)
	}
	secondMappedCallID := secondInput[1].(map[string]any)["call_id"].(string)
	if secondMappedCallID == firstMappedCallID || secondMappedCallID == baseCallID {
		t.Fatalf("remapped collision call_id = %q, want a new available ID", secondMappedCallID)
	}
	if got := secondInput[2].(map[string]any)["call_id"]; got != secondMappedCallID {
		t.Fatalf("function output call_id = %#v, want %q", got, secondMappedCallID)
	}

	third := applyCodexRequestPolicyWithCallIDMappingScope(map[string]any{
		"model": codexRequestPolicyModel,
		"input": []any{map[string]any{"type": "function_call", "call_id": longCallID, "name": "lookup", "arguments": "{}"}},
	}, codexRequestPolicyCandidate(), scope)
	if got := third["input"].([]any)[0].(map[string]any)["call_id"]; got != secondMappedCallID {
		t.Fatalf("replayed remapped call_id = %#v, want cached %q", got, secondMappedCallID)
	}
}

func TestCodexCallIDCollisionCacheExpires(t *testing.T) {
	t.Parallel()

	cache := newCodexCallIDCollisionCache()
	now := time.Unix(100, 0)
	cache.remember("scope", "original", "mapped", now)
	if got, ok := cache.lookup("scope", "original", now.Add(codexCallIDCollisionCacheTTL-time.Nanosecond)); !ok || got != "mapped" {
		t.Fatalf("cache lookup = %q, %v; want mapped, true", got, ok)
	}
	if got, ok := cache.lookup("scope", "original", now.Add(codexCallIDCollisionCacheTTL*2)); ok || got != "" {
		t.Fatalf("expired cache lookup = %q, %v; want empty, false", got, ok)
	}
}
