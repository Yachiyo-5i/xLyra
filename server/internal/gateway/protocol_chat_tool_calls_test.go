package gateway

import "testing"

// Parallel Responses function_call items must become one assistant message
// whose tool_calls are answered by the tool messages that follow it. Splitting
// them into one assistant message per call is rejected by strict upstreams.
func TestEncodeChatMessagesMergesParallelToolCalls(t *testing.T) {
	t.Parallel()

	messages := encodeCanonicalMessagesAsChatMessages([]canonicalMessage{
		{Type: "message", Role: "user", Content: []canonicalContentPart{{Type: "input_text", Text: "go"}}},
		{Type: "function_call", ToolCallID: "call_A", Name: "shell", Arguments: `{"cmd":"a"}`},
		{Type: "function_call", ToolCallID: "call_B", Name: "shell", Arguments: `{"cmd":"b"}`},
		{Type: "function_call_output", ToolCallID: "call_A", Output: "a"},
		{Type: "function_call_output", ToolCallID: "call_B", Output: "b"},
		{Type: "message", Role: "assistant", Content: []canonicalContentPart{{Type: "output_text", Text: "mid"}}},
		{Type: "function_call", ToolCallID: "call_C", Name: "shell", Arguments: `{"cmd":"c"}`},
		{Type: "function_call_output", ToolCallID: "call_C", Output: "c"},
		{Type: "function_call", ToolCallID: "call_D", Name: "shell", Arguments: `{"cmd":"d"}`},
		{Type: "function_call_output", ToolCallID: "call_D", Output: "d"},
	})

	type step struct {
		role  string
		calls []string
		reply string
	}
	want := []step{
		{role: "user"},
		{role: "assistant", calls: []string{"call_A", "call_B"}},
		{role: "tool", reply: "call_A"},
		{role: "tool", reply: "call_B"},
		{role: "assistant"},
		{role: "assistant", calls: []string{"call_C"}},
		{role: "tool", reply: "call_C"},
		{role: "assistant", calls: []string{"call_D"}},
		{role: "tool", reply: "call_D"},
	}
	if len(messages) != len(want) {
		t.Fatalf("got %d messages, want %d: %#v", len(messages), len(want), messages)
	}
	for i, raw := range messages {
		message, _ := raw.(map[string]any)
		if message["role"] != want[i].role {
			t.Fatalf("message %d role = %v, want %s", i, message["role"], want[i].role)
		}
		var calls []string
		if toolCalls, ok := message["tool_calls"].([]any); ok {
			for _, call := range toolCalls {
				calls = append(calls, anyString(call.(map[string]any)["id"]))
			}
		}
		if len(calls) != len(want[i].calls) {
			t.Fatalf("message %d tool_calls = %v, want %v", i, calls, want[i].calls)
		}
		for j := range calls {
			if calls[j] != want[i].calls[j] {
				t.Fatalf("message %d tool_calls = %v, want %v", i, calls, want[i].calls)
			}
		}
		if reply, _ := message["tool_call_id"].(string); reply != want[i].reply {
			t.Fatalf("message %d tool_call_id = %q, want %q", i, reply, want[i].reply)
		}
	}
}
