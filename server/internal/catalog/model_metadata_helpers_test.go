package catalog

import (
	"encoding/json"
	"testing"

	"xlyra/server/internal/store"
)

func TestModelKeysNormalizeDecoratedInputsAndCanonicalizeUpstreamNames(t *testing.T) {
	t.Parallel()

	normalizeCases := []struct {
		input string
		want  string
	}{
		{input: " models/openai/GPT+4o??? ", want: "gpt-plus-4o"},
		{input: "deepseek/DeepSeek V3.1!!!", want: "deepseek-v3.1"},
		{input: "model/google/Gemini___2.5   Flash", want: "gemini-2.5-flash"},
		{input: "xai/...Grok---4.Image++", want: "grok-4.image-plus-plus"},
	}
	for _, tt := range normalizeCases {
		tt := tt
		t.Run("normalize "+tt.input, func(t *testing.T) {
			t.Parallel()

			if got := NormalizeModelKey(tt.input); got != tt.want {
				t.Fatalf("NormalizeModelKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	canonicalCases := []struct {
		input string
		want  string
	}{
		{input: "route-search-gemini-2.5-pro-search-business-inference", want: "gemini-2.5-pro"},
		{input: "official-tts-1-business", want: "tts-1"},
		{input: "vendor-gpt-search-business", want: "gpt"},
		{input: "search-business", want: "search-business"},
	}
	for _, tt := range canonicalCases {
		tt := tt
		t.Run("canonical "+tt.input, func(t *testing.T) {
			t.Parallel()

			if got := CanonicalModelKeyFromUpstream(tt.input); got != tt.want {
				t.Fatalf("CanonicalModelKeyFromUpstream(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInferProviderRecognizesCanonicalizedVendorModelKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{input: "route-compat-openai-o4-mini", want: "openai"},
		{input: "google/deep-research-pro", want: "google"},
		{input: "Nano Banana Pro", want: "google"},
		{input: "perplexity-sonar-reasoning", want: "perplexity"},
		{input: "vendor-moonshot-v1-128k", want: "moonshotai-cn"},
		{input: "models/xai/grok-4", want: "xai"},
		{input: "vendor-custom-chat", want: "unknown"},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			if got := InferProvider(tt.input); got != tt.want {
				t.Fatalf("InferProvider(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInferCategoryAppliesSpecializedModelTypePrecedence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{input: "route-text-embedding-3-large-audio", want: "embedding"},
		{input: "vendor-rerank-image-model", want: "rerank"},
		{input: "vendor-tts-image-model", want: "audio"},
		{input: "vendor-dall-e-3-video", want: "image"},
		{input: "vendor-veo-3-preview", want: "video"},
		{input: "plain-chat-model", want: "chat"},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			if got := InferCategory(tt.input); got != tt.want {
				t.Fatalf("InferCategory(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCanonicalModelParamsInfersDefaultsFromDecoratedModelKey(t *testing.T) {
	t.Parallel()

	params, err := canonicalModelParams(UpsertCanonicalModelInput{
		ModelKey:    "models/google/Gemini 2.5 Flash Search",
		DisplayName: "   ",
		Provider:    "   ",
		Category:    "   ",
		Status:      "   ",
	})
	if err != nil {
		t.Fatalf("canonicalModelParams: %v", err)
	}

	if params.ModelKey != "gemini-2.5-flash" {
		t.Fatalf("ModelKey = %q, want gemini-2.5-flash", params.ModelKey)
	}
	if params.DisplayName != params.ModelKey {
		t.Fatalf("DisplayName = %q, want %q", params.DisplayName, params.ModelKey)
	}
	if params.Provider != "google" {
		t.Fatalf("Provider = %q, want google", params.Provider)
	}
	if params.Category != "chat" {
		t.Fatalf("Category = %q, want chat", params.Category)
	}
	if params.Status != "active" {
		t.Fatalf("Status = %q, want active", params.Status)
	}
	if string(params.Capabilities) != "{}" || !json.Valid(params.Capabilities) {
		t.Fatalf("Capabilities = %s, want valid empty object", string(params.Capabilities))
	}

	_, err = canonicalModelParams(UpsertCanonicalModelInput{ModelKey: "models/openai/!!!"})
	assertCatalogErrorContains(t, "canonicalModelParams", err, "model_key is required")
}

func TestModelDisplayNameAndCanonicalSuffixNoiseHelpers(t *testing.T) {
	t.Parallel()

	displayCases := []struct {
		model store.SiteModel
		want  string
	}{
		{model: store.SiteModel{DisplayName: "  Custom Name  ", UpstreamName: " upstream "}, want: "Custom Name"},
		{model: store.SiteModel{DisplayName: "   ", UpstreamName: " upstream "}, want: "upstream"},
		{model: store.SiteModel{DisplayName: "   ", UpstreamName: "   "}, want: ""},
	}
	for _, tt := range displayCases {
		tt := tt
		t.Run("display "+tt.want, func(t *testing.T) {
			t.Parallel()

			if got := displayNameFromModel(tt.model); got != tt.want {
				t.Fatalf("displayNameFromModel(%#v) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}

	suffixCases := []struct {
		name   string
		tokens []string
		want   []string
	}{
		{name: "trims trailing noise", tokens: []string{"gpt", "4o", "search", "business"}, want: []string{"gpt", "4o"}},
		{name: "keeps first token when all noise", tokens: []string{"search", "business"}, want: []string{"search"}},
		{name: "preserves internal noise before non-noise suffix", tokens: []string{"gpt", "search", "preview"}, want: []string{"gpt", "search", "preview"}},
		{name: "handles empty tokens", tokens: nil, want: nil},
	}
	for _, tt := range suffixCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertStringSliceEqual(t, trimCanonicalSuffixNoise(tt.tokens), tt.want)
		})
	}
}

func TestInferEndpointTypesPrioritizesCuratedModelCategoryAndProviderFallbacks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		provider string
		modelKey string
		category string
		want     []string
	}{
		{
			name:     "curated model beats category fallback",
			provider: "custom",
			modelKey: "gpt-image-2",
			category: "chat",
			want:     []string{"openai-image"},
		},
		{
			name:     "image category beats codex name fallback",
			provider: "custom",
			modelKey: "vendor-codex-image-model",
			category: "image",
			want:     []string{"openai-image"},
		},
		{
			name:     "codex fallback trims and lowercases",
			provider: "custom",
			modelKey: " VENDOR-CODEX-RUNNER ",
			category: "chat",
			want:     []string{"openai-response"},
		},
		{
			name:     "anthropic provider fallback after model name checks",
			provider: "anthropic",
			modelKey: "vendor-chat-model",
			category: "chat",
			want:     []string{"anthropic-messages"},
		},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertStringSliceEqual(t, inferEndpointTypes(tt.provider, tt.modelKey, tt.category), tt.want)
		})
	}
}

func TestSyncNumericExtractorsHandleMissingNegativeAndFractionalValues(t *testing.T) {
	t.Parallel()

	values := map[string]any{
		"missing":  nil,
		"negative": float64(-1.25),
		"zero":     int64(0),
		"positive": int(7),
		"uint":     uint(9),
	}

	assertCatalogFloat(t, values, "absent", 0)
	assertCatalogFloat(t, values, "negative", -1.25)
	assertCatalogFloat(t, values, "uint", 0)

	assertCatalogCost(t, values, "positive", 7, true)
	assertCatalogCost(t, values, "negative", 0, false)
	assertCatalogCost(t, values, "absent", 0, false)

	limits := map[string]any{
		"zero":     float64(0),
		"negative": int64(-8),
		"fraction": float64(4096.9),
	}
	assertCatalogInt(t, limits, "zero", 0, true)
	assertCatalogInt(t, limits, "negative", -8, true)
	assertCatalogInt(t, limits, "fraction", 4096, true)
	assertCatalogInt(t, limits, "absent", 0, false)
}
