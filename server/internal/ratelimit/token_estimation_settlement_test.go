package ratelimit

import (
	"testing"
)

func TestPromptTokenEstimateIgnoresReservationControls(t *testing.T) {
	t.Parallel()

	base := chatPromptPayload()
	withControls := chatPromptPayload()
	for key, value := range map[string]any{
		"max_tokens":            int64(128),
		"max_completion_tokens": int64(256),
		"max_output_tokens":     int64(512),
		"stream":                true,
	} {
		withControls[key] = value
	}

	if got, want := estimatePromptTokens(withControls), estimatePromptTokens(base); got != want {
		t.Fatalf("estimatePromptTokens with controls = %d, want %d", got, want)
	}
}

func TestPromptTokenEstimateKeepsMinimumForEmptyPayload(t *testing.T) {
	t.Parallel()

	if got := estimatePromptTokens(map[string]any{}); got != 1 {
		t.Fatalf("empty prompt tokens = %d, want 1", got)
	}
}

func chatPromptPayload() map[string]any {
	return map[string]any{"messages": []map[string]string{{"role": "user", "content": "hello"}}}
}
