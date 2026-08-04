package catalog

import (
	"strings"
	"testing"
)

func TestCuratedFallbackPricesCoverTTS1Family(t *testing.T) {
	for _, key := range []string{"tts-1", "tts-1-hd", "tts-1-1106"} {
		price, ok := curatedFallbackPrices[key]
		if !ok {
			t.Fatalf("curated fallback missing %q", key)
		}
		if price.InputPrice <= 0 {
			t.Fatalf("%q input price = %v, want positive per-1M-char price", key, price.InputPrice)
		}
	}
	if curatedFallbackPrices["tts-1"].InputPrice != 15 || curatedFallbackPrices["tts-1-hd"].InputPrice != 30 {
		t.Fatalf("tts-1 should be $15/1M and tts-1-hd $30/1M chars")
	}
	for key := range curatedFallbackPrices {
		if strings.TrimSpace(key) != key || key == "" {
			t.Fatalf("curated key %q must be a trimmed non-empty normalized model key", key)
		}
	}
}
