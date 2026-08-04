package catalog

import (
	"testing"
	"time"
)

func TestLiteLLMProviderToCanonical(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		want string
		ok   bool
	}{
		"openai":                     {"openai", true},
		"text-completion-openai":     {"openai", true},
		"anthropic":                  {"anthropic", true},
		"gemini":                     {"google", true},
		"vertex_ai-language-models":  {"google", true},
		"vertex_ai-embedding-models": {"google", true},
		"xai":                        {"xai", true},
		"deepseek":                   {"deepseek", true},
		"bedrock":                    {"", false},
		"moonshot":                   {"", false},
		"":                           {"", false},
	}
	for provider, want := range cases {
		got, ok := litellmProviderToCanonical(provider)
		if got != want.want || ok != want.ok {
			t.Errorf("litellmProviderToCanonical(%q) = (%q,%v), want (%q,%v)", provider, got, ok, want.want, want.ok)
		}
	}
}

func TestLiteLLMModelParamsConvertsUnitsAndRatios(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	data := litellmModel{
		LiteLLMProvider:                     "openai",
		InputCostPerToken:                   5e-6,
		OutputCostPerToken:                  3e-5,
		CacheReadInputTokenCost:             5e-7,
		CacheCreationInputTokenCost:         6.25e-6,
		CacheCreationInputTokenCostAbove1hr: 0,
	}

	params, ok := litellmModelParams("openai", "gpt-5.6-sol", data, now)
	if !ok {
		t.Fatal("expected ok for non-empty model key")
	}
	if !params.InputPrice.Valid || params.InputPrice.Float64 != 5.0 {
		t.Fatalf("input price = %#v, want $5/M", params.InputPrice)
	}
	if !params.OutputPrice.Valid || params.OutputPrice.Float64 != 30.0 {
		t.Fatalf("output price = %#v, want $30/M", params.OutputPrice)
	}
	if !params.CacheReadRatio.Valid || !floatNear(params.CacheReadRatio.Float64, 0.1) {
		t.Fatalf("cache read ratio = %#v, want 0.1", params.CacheReadRatio)
	}
	if !params.CacheWriteRatio.Valid || !floatNear(params.CacheWriteRatio.Float64, 1.25) {
		t.Fatalf("cache write ratio = %#v, want 1.25", params.CacheWriteRatio)
	}
	if params.CacheWrite1hRatio.Valid {
		t.Fatalf("cache write 1h ratio should be unset, got %#v", params.CacheWrite1hRatio)
	}
	if params.PricingSource != litellmPricingSource {
		t.Fatalf("pricing source = %q, want %q", params.PricingSource, litellmPricingSource)
	}
}

func TestLiteLLMModelParamsAnthropic1hCacheRatio(t *testing.T) {
	t.Parallel()

	data := litellmModel{
		LiteLLMProvider:                     "anthropic",
		InputCostPerToken:                   1e-5,
		OutputCostPerToken:                  5e-5,
		CacheReadInputTokenCost:             1e-6,
		CacheCreationInputTokenCost:         1.25e-5,
		CacheCreationInputTokenCostAbove1hr: 2e-5,
	}
	params, ok := litellmModelParams("anthropic", "claude-fable-5", data, time.Unix(1, 0))
	if !ok {
		t.Fatal("expected ok")
	}
	if !params.CacheWriteRatio.Valid || !floatNear(params.CacheWriteRatio.Float64, 1.25) {
		t.Fatalf("write ratio = %#v, want 1.25", params.CacheWriteRatio)
	}
	if !params.CacheWrite1hRatio.Valid || !floatNear(params.CacheWrite1hRatio.Float64, 2.0) {
		t.Fatalf("write 1h ratio = %#v, want 2.0", params.CacheWrite1hRatio)
	}
}

func TestLiteLLMModelParamsEmptyKey(t *testing.T) {
	t.Parallel()

	if _, ok := litellmModelParams("openai", "   ", litellmModel{}, time.Unix(1, 0)); ok {
		t.Fatal("empty model key should be skipped")
	}
}

func floatNear(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-9
}

func TestLitellmModelParamsMapsCharacterPriceForTTS(t *testing.T) {
	now := time.Unix(0, 0)

	tts1, ok := litellmModelParams("openai", "tts-1", litellmModel{
		LiteLLMProvider:       "openai",
		InputCostPerCharacter: 1.5e-5,
	}, now)
	if !ok {
		t.Fatal("expected ok for tts-1")
	}
	if !tts1.InputPrice.Valid || tts1.InputPrice.Float64 != 15.0 {
		t.Fatalf("tts-1 input price = %#v, want $15/1M chars", tts1.InputPrice)
	}
	if tts1.Category != "audio" {
		t.Fatalf("tts-1 category = %q, want audio", tts1.Category)
	}

	hd, _ := litellmModelParams("openai", "tts-1-hd", litellmModel{
		LiteLLMProvider:       "openai",
		InputCostPerCharacter: 3e-5,
	}, now)
	if !hd.InputPrice.Valid || hd.InputPrice.Float64 != 30.0 {
		t.Fatalf("tts-1-hd input price = %#v, want $30/1M chars", hd.InputPrice)
	}
}

func TestLitellmModelParamsPrefersTokenPriceOverCharacter(t *testing.T) {
	now := time.Unix(0, 0)

	params, _ := litellmModelParams("openai", "gpt-4o-mini-tts", litellmModel{
		LiteLLMProvider:       "openai",
		InputCostPerToken:     2.5e-6,
		InputCostPerCharacter: 1.5e-5,
	}, now)
	if !params.InputPrice.Valid || params.InputPrice.Float64 != 2.5 {
		t.Fatalf("gpt-4o-mini-tts input price = %#v, want token price $2.5/1M (not char)", params.InputPrice)
	}
}

func TestLitellmModelParamsMapsAudioOutputToRatio(t *testing.T) {
	now := time.Unix(0, 0)

	params, _ := litellmModelParams("openai", "gpt-4o-mini-tts", litellmModel{
		LiteLLMProvider:         "openai",
		InputCostPerToken:       2.5e-6,
		OutputCostPerToken:      1e-5,
		OutputCostPerAudioToken: 1.2e-5,
	}, now)
	if !params.AudioRatio.Valid || !floatNear(params.AudioRatio.Float64, 4.8) {
		t.Fatalf("audio ratio = %#v, want 4.8 (12/2.5)", params.AudioRatio)
	}
	if !params.AudioCompletionRatio.Valid || params.AudioCompletionRatio.Float64 != 1 {
		t.Fatalf("audio completion ratio = %#v, want 1", params.AudioCompletionRatio)
	}
	if got := params.InputPrice.Float64 * params.AudioRatio.Float64 * params.AudioCompletionRatio.Float64; !floatNear(got, 12.0) {
		t.Fatalf("derived audio output value = %v, want $12/1M", got)
	}

	noAudio, _ := litellmModelParams("openai", "gpt-5.6", litellmModel{
		LiteLLMProvider:   "openai",
		InputCostPerToken: 1e-6,
	}, now)
	if noAudio.AudioRatio.Valid {
		t.Fatalf("non-audio model should have no audio ratio, got %#v", noAudio.AudioRatio)
	}
}

func TestCuratedFallbackGptMiniTtsOverridesToOfficialAudioPrice(t *testing.T) {
	p, ok := curatedFallbackPrices["gpt-4o-mini-tts"]
	if !ok {
		t.Fatal("curated fallback missing gpt-4o-mini-tts")
	}
	if !p.Override {
		t.Fatal("gpt-4o-mini-tts must be an override (corrects stale litellm input)")
	}
	if p.InputPrice != 0.60 {
		t.Fatalf("input price = %v, want $0.60/1M official", p.InputPrice)
	}
	if got := p.InputPrice * p.AudioRatio * p.AudioCompletionRatio; got != 12.0 {
		t.Fatalf("derived audio output = %v, want $12/1M", got)
	}
	if tts1 := curatedFallbackPrices["tts-1"]; tts1.Override {
		t.Fatal("tts-1 must stay fill-missing (not override)")
	}
}
