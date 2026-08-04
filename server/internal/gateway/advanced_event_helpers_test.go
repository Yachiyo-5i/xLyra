package gateway

import "testing"

func TestAdvancedKindFromResponsesStreamEventClassifiesUnknownProviderEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event responsesStreamEvent
		raw   map[string]any
		want  string
		ok    bool
	}{
		{
			name:  "known output text event is not advanced",
			event: responsesStreamEvent{Type: "response.output_text.delta"},
			raw:   map[string]any{"type": "response.output_text.delta"},
			ok:    false,
		},
		{
			name:  "output item mcp event",
			event: responsesStreamEvent{Type: "response.output_item.added", Item: &responsesOutputItem{Type: "mcp_tool_call"}},
			raw:   map[string]any{"type": "response.output_item.added"},
			want:  "mcp",
			ok:    true,
		},
		{
			name:  "content part shell event from raw part",
			event: responsesStreamEvent{Type: "response.content_part.delta"},
			raw:   map[string]any{"part": map[string]any{"type": "terminal_command"}},
			want:  "shell",
			ok:    true,
		},
		{
			name:  "generic response patch event",
			event: responsesStreamEvent{Type: "response.custom_patch.created"},
			raw:   map[string]any{"type": "response.custom_patch.created"},
			want:  "patch",
			ok:    true,
		},
		{
			name:  "non response event ignored",
			event: responsesStreamEvent{Type: "thread.message.delta"},
			raw:   map[string]any{"type": "thread.message.delta"},
			ok:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := advancedKindFromResponsesStreamEvent(tt.event, tt.raw)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("kind = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAdvancedKindAndPhaseHelpersCoverTokenFamilies(t *testing.T) {
	t.Parallel()

	kindTests := map[string]string{
		"function_call_output": "tool_result",
		"authorization":        "approval",
		"computer":             "computer_use",
		"web_search":           "web_search",
		"file":                 "file",
		"code_execution":       "code_execution",
		"audio":                "audio",
		"transcript":           "transcript",
		"todo":                 "todo",
		"plan":                 "plan",
		"subagent":             "subagent",
		"vendor_custom":        "provider_event",
	}
	for tokens, want := range kindTests {
		tokens, want := tokens, want
		t.Run(tokens, func(t *testing.T) {
			t.Parallel()

			if got := advancedKindFromTokens(tokens); got != want {
				t.Fatalf("advancedKindFromTokens(%q) = %q, want %q", tokens, got, want)
			}
		})
	}

	phaseTests := map[string]string{
		"response.tool_call.started":       "started",
		"response.tool_call_delta":         "delta",
		"response.image.partial_image":     "delta",
		"response.tool_call_done":          "done",
		"response.tool_call.failed":        "failed",
		"response.provider_custom.updated": "event",
	}
	for eventName, want := range phaseTests {
		eventName, want := eventName, want
		t.Run(eventName, func(t *testing.T) {
			t.Parallel()

			if got := advancedPhaseFromResponsesStreamEvent(eventName); got != want {
				t.Fatalf("advancedPhaseFromResponsesStreamEvent(%q) = %q, want %q", eventName, got, want)
			}
		})
	}
}
