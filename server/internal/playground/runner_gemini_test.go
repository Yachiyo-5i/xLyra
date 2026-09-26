package playground

import (
	"net/url"
	"testing"
)

func TestGatewayPathGeminiIncludesModel(t *testing.T) {
	t.Parallel()
	got := gatewayPath("gemini", "gemini-3.1-pro")
	want := "/v1beta/models/" + url.PathEscape("gemini-3.1-pro") + ":streamGenerateContent"
	if got != want {
		t.Fatalf("gatewayPath = %q, want %q", got, want)
	}
}

func TestConsumeGeminiChatEvent(t *testing.T) {
	t.Parallel()
	state := &chatRunState{}
	consumeGeminiChatEvent(map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{
				"parts": []any{
					map[string]any{"text": "think", "thought": true},
					map[string]any{"text": "hello"},
				},
			},
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount":     float64(3),
			"candidatesTokenCount": float64(2),
			"thoughtsTokenCount":   float64(5),
			"totalTokenCount":      float64(10),
		},
	}, state)
	if state.content != "hello" {
		t.Fatalf("content = %q, want hello", state.content)
	}
	if state.reasoning != "think" {
		t.Fatalf("reasoning = %q, want think", state.reasoning)
	}
	if state.usage == nil || state.usage.PromptTokens != 3 || state.usage.CompletionTokens != 2 || state.usage.TotalTokens != 10 {
		t.Fatalf("usage = %#v", state.usage)
	}
}

func TestPlaygroundGeminiThinkingConfig(t *testing.T) {
	t.Parallel()
	none := playgroundGeminiThinkingConfig("none")
	if none["thinkingBudget"] != 0 || none["includeThoughts"] != false {
		t.Fatalf("none config = %#v", none)
	}
	high := playgroundGeminiThinkingConfig("high")
	if high["thinkingLevel"] != "HIGH" || high["thinkingBudget"] != 24576 {
		t.Fatalf("high config = %#v", high)
	}
	if playgroundGeminiThinkingConfig("") != nil {
		t.Fatalf("empty effort should omit thinkingConfig")
	}
}
