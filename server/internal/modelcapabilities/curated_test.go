package modelcapabilities

import (
	"context"
	"reflect"
	"testing"
)

func TestCuratedEndpointTypesUseGenerationRule(t *testing.T) {
	t.Parallel()

	cases := []struct {
		modelID string
		want    []string
	}{
		{"gpt-5.6-sol", []string{"openai", "openai-response"}},
		{"gpt-5.6-terra", []string{"openai", "openai-response"}},
		{"gpt-5.6-luna", []string{"openai", "openai-response"}},
		{"gpt-5.7-nova", []string{"openai", "openai-response"}},
		{"gpt-6", []string{"openai", "openai-response"}},
		{"gpt-6.1-foo", []string{"openai", "openai-response"}},
		{"gpt-5.5", []string{"openai", "openai-response"}},
		{"gpt-5.4-mini", []string{"openai", "openai-response"}},
		{"gpt-5.3-codex", []string{"openai", "openai-response"}},
		{"gpt-5-codex", []string{"openai-response"}},
		{"gpt-5.1-codex", []string{"openai-response"}},
		{"gpt-5.2-codex", []string{"openai-response"}},
		{"codex-auto-review", []string{"openai-response"}},
		{"gpt-4.1", []string{"openai", "openai-response"}},
		{"mimo-v2.5-tts", []string{"openai"}},
		{"mimo-v2.5-tts-voicedesign", []string{"openai"}},
		{"mimo-v2.5-tts-voiceclone", []string{"openai"}},
		{"gpt-image-2", []string{"openai-image"}},
		{"gpt-image-3", []string{"openai-image"}},
		{"text-embedding-3-large", []string{"openai-embedding"}},
		{"claude-sonnet-4-6", []string{"anthropic-messages"}},
		{"gpt-4o", []string{"openai"}},
		{"gpt-3.5-turbo", []string{"openai"}},
		{"o3-mini", []string{"openai"}},
		{"unknown-model", nil},
	}
	for _, tc := range cases {
		if got := EndpointTypesForModel(tc.modelID); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("EndpointTypesForModel(%q) = %#v, want %#v", tc.modelID, got, tc.want)
		}
	}
}

func TestInferEndpointTypesForModelUsesImageGrokAndFallbackRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		modelID string
		want    []string
	}{
		{"gpt-image-2", []string{"openai-image"}},
		{"gpt-4o-mini-tts", []string{"openai-audio-speech"}},
		{"tts-1", []string{"openai-audio-speech"}},
		{"tts-1-hd", []string{"openai-audio-speech"}},
		{"mimo-v2.5-tts", []string{"openai"}},
		{"mimo-v2.5-tts-voicedesign", []string{"openai"}},
		{"mimo-v2.5-tts-voiceclone", []string{"openai"}},
		{"flux-kontext-pro", []string{"openai-image"}},
		{"gemini-image-3", []string{"google-gemini"}},
		{"nano_banana_2", []string{"google-gemini"}},
		{"nano-banana-pro", []string{"google-gemini"}},
		{"grok-imagine-image-quality", []string{"openai-image"}},
		{"grok-4.5", []string{"openai", "openai-response"}},
		{"claude-sonnet-4-6", []string{"anthropic-messages"}},
		{"vendor-model", []string{"openai"}},
	}
	for _, tc := range cases {
		if got := InferEndpointTypesForModel(tc.modelID); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("InferEndpointTypesForModel(%q) = %#v, want %#v", tc.modelID, got, tc.want)
		}
	}
}

func TestInferEndpointTypesForSiteModelHonorsAnthropicBasePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		modelID  string
		siteType string
		baseURL  string
		want     []string
	}{
		{"deepseek-chat", "openai", "https://relay.example.com/anthropic", []string{"anthropic-messages"}},
		{"glm-4.7", "openai", "https://relay.example.com/api/anthropic", []string{"anthropic-messages"}},
		{"kimi-k2", "openai", "https://relay.example.com/Anthropic/", []string{"anthropic-messages"}},
		{"gpt-5.6", "openai", "https://relay.example.com/anthropic", []string{"anthropic-messages"}},
		{"deepseek-chat", "openai", "https://relay.example.com/v1", []string{"openai", "anthropic-messages"}},
		{"deepseek-chat", "openai", "https://anthropic.example.com", []string{"openai", "anthropic-messages"}},
		{"deepseek-chat", "openai", "https://relay.example.com/anthropic/v1", []string{"openai", "anthropic-messages"}},
		{"qwen3-coder", "openai", "", []string{"openai", "anthropic-messages"}},
		{"vendor-model", "xlyra", "", []string{"openai"}},
		{"claude-sonnet-4-6", "openai", "", []string{"anthropic-messages"}},
		{"grok-4.5", "openai", "", []string{"openai", "openai-response"}},
		{"gemini-image-3", "openai", "", []string{"openai-image"}},
		{"gemini-image-3", "xlyra", "", []string{"google-gemini"}},
		{"nano_banana_2", "openai", "", []string{"openai-image"}},
		{"nano-banana-pro", "openai", "", []string{"openai-image"}},
		{"nano-banana-pro", "xlyra", "", []string{"google-gemini"}},
	}
	for _, tc := range cases {
		if got := InferEndpointTypesForSiteModel(tc.modelID, tc.siteType, tc.baseURL); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("InferEndpointTypesForSiteModel(%q, %q, %q) = %#v, want %#v", tc.modelID, tc.siteType, tc.baseURL, got, tc.want)
		}
	}
}

func TestUsesModelNameEndpointInferencePreservesSpecializedChannels(t *testing.T) {
	t.Parallel()

	for _, siteType := range []string{
		"newapi", "codex", "antigravity", "deepseek", "minimax", "xiaomi_mimo",
		"moonshot", "kimi_code", "google", "google_gemini", "zhipu", "glm_code",
	} {
		if UsesModelNameEndpointInference(siteType) {
			t.Errorf("UsesModelNameEndpointInference(%q) = true, want false", siteType)
		}
	}
	for _, siteType := range []string{"openai", "xlyra", "anthropic", "grok"} {
		if !UsesModelNameEndpointInference(siteType) {
			t.Errorf("UsesModelNameEndpointInference(%q) = false, want true", siteType)
		}
	}
}

func TestCuratedLookupDoesNotOverrideCodexDeclaredEndpointTypes(t *testing.T) {
	t.Parallel()

	source := curatedSource{}
	values, ok, err := source.Lookup(context.Background(), Input{
		Provider: "openai",
		ModelID:  "gpt-5-codex",
		BaseCapabilities: map[string]any{
			"source":                   "codex",
			"supported_endpoint_types": []string{"openai", "openai-response"},
		},
	})
	if err != nil {
		t.Fatalf("curated Lookup: %v", err)
	}
	if ok || values != nil {
		t.Fatalf("curated must not override codex-declared endpoint types, got %#v", values)
	}

	_, ok, err = source.Lookup(context.Background(), Input{
		Provider: "openai",
		ModelID:  "gpt-5-codex",
		BaseCapabilities: map[string]any{
			"source":                   "codex",
			"supported_endpoint_types": []any{"openai"},
		},
	})
	if err != nil || ok {
		t.Fatalf("curated must not override codex []any endpoint types (ok=%v err=%v)", ok, err)
	}

	values, ok, err = source.Lookup(context.Background(), Input{
		Provider: "openai",
		ModelID:  "claude-3-5-sonnet",
		BaseCapabilities: map[string]any{
			"source":                   "upstream",
			"supported_endpoint_types": []string{"openai"},
		},
	})
	if err != nil || !ok {
		t.Fatalf("curated should correct non-codex declarations (ok=%v err=%v)", ok, err)
	}
	endpoints, _ := values["supported_endpoint_types"].([]string)
	if !reflect.DeepEqual(endpoints, []string{"anthropic-messages"}) {
		t.Fatalf("claude correction endpoints = %#v", endpoints)
	}

	values, ok, err = source.Lookup(context.Background(), Input{
		Provider:         "openai",
		ModelID:          "gpt-5.6-sol",
		BaseCapabilities: map[string]any{"source": "codex"},
	})
	if err != nil || !ok {
		t.Fatalf("curated should match gpt-5.6-sol without explicit types (ok=%v err=%v)", ok, err)
	}
	endpoints, _ = values["supported_endpoint_types"].([]string)
	if !reflect.DeepEqual(endpoints, []string{"openai", "openai-response"}) {
		t.Fatalf("gpt-5.6-sol endpoints = %#v", endpoints)
	}
}

func TestEnrichKeepsCodexEndpointTypesOverCurated(t *testing.T) {
	t.Parallel()

	service := New()
	result := service.Enrich(context.Background(), Input{
		Provider: "openai",
		ModelID:  "gpt-5-codex",
		BaseCapabilities: map[string]any{
			"source":                   "codex",
			"supported_endpoint_types": []string{"openai", "openai-response"},
		},
	})
	endpoints := normalizeStringSlice(result.Capabilities["supported_endpoint_types"])
	if !reflect.DeepEqual(endpoints, []string{"openai", "openai-response"}) {
		t.Fatalf("codex endpoint types were overridden: %#v", endpoints)
	}
}
