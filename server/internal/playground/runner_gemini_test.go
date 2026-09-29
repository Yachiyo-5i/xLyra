package playground

import (
	"context"
	"net/url"
	"testing"
)

func TestGeminiGatewayBodyMovesSystemMessagesToInstruction(t *testing.T) {
	t.Parallel()
	service := &Service{}
	body, err := service.geminiGatewayBody(context.Background(), RunPayload{
		Chat: &ChatConversation{
			SystemPrompt: "base instruction",
			Messages: []ChatMessage{
				{ID: "system", Role: "system", Content: "legacy instruction"},
				{ID: "developer", Role: "developer", Content: "developer instruction"},
				{ID: "user", Role: "user", Content: "hello"},
				{ID: "assistant", Role: "assistant", Content: "world"},
			},
		},
	})
	if err != nil {
		t.Fatalf("geminiGatewayBody returned error: %v", err)
	}
	contents, ok := body["contents"].([]map[string]any)
	if !ok || len(contents) != 2 {
		t.Fatalf("contents = %#v, want two user/model messages", body["contents"])
	}
	if contents[0]["role"] != "user" || contents[1]["role"] != "model" {
		t.Fatalf("contents roles = %#v, want user/model", contents)
	}
	instruction, ok := body["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatalf("systemInstruction = %#v, want object", body["systemInstruction"])
	}
	parts, ok := instruction["parts"].([]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("systemInstruction parts = %#v, want three parts", instruction["parts"])
	}
	for index, want := range []string{"base instruction", "legacy instruction", "developer instruction"} {
		part, ok := parts[index].(map[string]any)
		if !ok || part["text"] != want {
			t.Fatalf("systemInstruction part %d = %#v, want %q", index, parts[index], want)
		}
	}
}

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
