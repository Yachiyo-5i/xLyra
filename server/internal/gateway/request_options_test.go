package gateway

import (
	"strings"
	"testing"
)

func TestNormalizeClientRequestOptionsAcceptsSupportedReasoningEfforts(t *testing.T) {
	t.Parallel()

	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"} {
		effort := effort
		t.Run(effort, func(t *testing.T) {
			t.Parallel()
			payload := map[string]any{
				"reasoning":    map[string]any{"effort": " " + effort + " "},
				"service_tier": " FAST ",
			}
			if failure := normalizeClientRequestOptions(payload); failure != nil {
				t.Fatalf("normalizeClientRequestOptions returned failure: %#v", failure)
			}
			if got := payload["reasoning"].(map[string]any)["effort"]; got != effort {
				t.Fatalf("reasoning effort = %#v, want %q", got, effort)
			}
			if got := payload["service_tier"]; got != "fast" {
				t.Fatalf("service tier = %#v, want fast", got)
			}
		})
	}
}

func TestNormalizeClientRequestOptionsRejectsUnsupportedReasoningEffort(t *testing.T) {
	t.Parallel()

	tests := []map[string]any{
		{"reasoning_effort": "light"},
		{"reasoning": map[string]any{"effort": "auto"}},
		{"reasoning_effort": 3},
		{"reasoning_effort": "high", "reasoning": map[string]any{"effort": "ultra"}},
	}
	for _, payload := range tests {
		if failure := normalizeClientRequestOptions(payload); failure == nil || failure.code != "invalid_reasoning_effort" || failure.status != 400 {
			t.Fatalf("failure = %#v, want invalid_reasoning_effort", failure)
		}
	}
}

func TestNormalizeClientRequestOptionsReportsUnsupportedReasoningEffortValue(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"reasoning": map[string]any{"effort": " Light "}}
	failure := normalizeClientRequestOptions(payload)
	if failure == nil {
		t.Fatal("normalizeClientRequestOptions returned no failure")
	}
	if !strings.Contains(failure.message, `reasoning.effort value " Light " is not supported`) {
		t.Fatalf("failure message = %q", failure.message)
	}
}

func TestNormalizeClientRequestOptionsNormalizesUppercaseEffort(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"reasoning_effort": " ULTRA "}
	if failure := normalizeClientRequestOptions(payload); failure != nil {
		t.Fatalf("normalizeClientRequestOptions returned failure: %#v", failure)
	}
	if got := payload["reasoning_effort"]; got != "ultra" {
		t.Fatalf("reasoning effort = %#v, want ultra", got)
	}
}
