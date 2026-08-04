package site

import (
	"testing"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestDefaultModelPricingGrokImage(t *testing.T) {
	site := store.Site{SiteType: "grok"}
	image := adapter.Model{
		UpstreamName: "grok-imagine-image-quality",
		Capabilities: map[string]any{"supported_endpoint_types": []string{"openai-image"}},
	}
	value, ok := defaultModelPricing(site, image)
	if !ok || value != grokImagePerRequestUSD {
		t.Fatalf("grok image default = (%v, %v), want (%v, true)", value, ok, grokImagePerRequestUSD)
	}

	imageAny := adapter.Model{
		Capabilities: map[string]any{"supported_endpoint_types": []any{"openai-image"}},
	}
	if _, ok := defaultModelPricing(site, imageAny); !ok {
		t.Fatal("[]any endpoint types should be recognized")
	}
}

func TestDefaultModelPricingSkipsNonImageAndNonGrok(t *testing.T) {
	grokChat := adapter.Model{
		UpstreamName: "grok-4.5",
		Capabilities: map[string]any{"supported_endpoint_types": []string{"openai", "openai-response"}},
	}
	if _, ok := defaultModelPricing(store.Site{SiteType: "grok"}, grokChat); ok {
		t.Fatal("grok chat model must not get a default per-request price")
	}

	otherImage := adapter.Model{
		Capabilities: map[string]any{"supported_endpoint_types": []string{"openai-image"}},
	}
	if _, ok := defaultModelPricing(store.Site{SiteType: "openai"}, otherImage); ok {
		t.Fatal("non-grok image model must not get the grok default")
	}
}
