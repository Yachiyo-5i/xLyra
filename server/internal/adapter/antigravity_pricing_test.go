package adapter

import "testing"

func TestAntigravityPricingFromModelsDevMapsChannelAliases(t *testing.T) {
	catalog := antigravityModelsDevCatalog{
		"google": {
			Models: map[string]antigravityModelsDevModel{
				"gemini-3.1-pro-preview": {
					Name: "Gemini 3.1 Pro Preview",
					Cost: map[string]any{
						"input":      2.0,
						"output":     12.0,
						"cache_read": 0.2,
					},
				},
				"gemini-3.1-flash-image-preview": {
					Name: "Gemini 3.1 Flash Image (Preview)",
					Cost: map[string]any{
						"input":  0.25,
						"output": 60.0,
					},
				},
			},
		},
		"anthropic": {
			Models: map[string]antigravityModelsDevModel{
				"claude-opus-4-6": {
					Name: "Claude Opus 4.6",
					Cost: map[string]any{
						"input":       5.0,
						"output":      25.0,
						"cache_read":  0.5,
						"cache_write": 6.25,
					},
				},
			},
		},
	}
	snapshot := antigravityPricingFromModelsDev([]antigravityModelQuota{
		{Name: "gemini-3.1-pro-high", DisplayName: "Gemini 3.1 Pro High"},
		{Name: "gemini-3.1-flash-image", DisplayName: "Gemini 3.1 Flash Image"},
		{Name: "claude-opus-4-6-thinking", DisplayName: "Claude Opus 4.6 Thinking"},
		{Name: "unknown-model", DisplayName: "Unknown"},
	}, catalog)

	if len(snapshot.Items) != 3 {
		t.Fatalf("expected 3 priced models, got %d", len(snapshot.Items))
	}
	pro := antigravityPricingByModel(snapshot.Items, "gemini-3.1-pro-high")
	if pro == nil {
		t.Fatalf("missing pro alias pricing")
	}
	if pro.InputValue != 2 || pro.OutputValue != 12 {
		t.Fatalf("unexpected pro pricing: %#v", pro)
	}
	if !pro.HasCacheRatio || pro.CacheRatio != 0.1 {
		t.Fatalf("expected cache ratio 0.1, got %#v", pro.CacheRatio)
	}

	image := antigravityPricingByModel(snapshot.Items, "gemini-3.1-flash-image")
	if image == nil {
		t.Fatalf("missing image alias pricing")
	}
	if image.InputValue != 0.25 || image.OutputValue != 60 {
		t.Fatalf("unexpected image pricing: %#v", image)
	}

	thinking := antigravityPricingByModel(snapshot.Items, "claude-opus-4-6-thinking")
	if thinking == nil {
		t.Fatalf("missing thinking alias pricing")
	}
	if thinking.InputValue != 5 || thinking.OutputValue != 25 {
		t.Fatalf("unexpected thinking pricing: %#v", thinking)
	}
	if !thinking.HasCreateCacheRatio || thinking.CreateCacheRatio != 1.25 {
		t.Fatalf("expected create cache ratio 1.25, got %#v", thinking.CreateCacheRatio)
	}
}

func TestAntigravityCapabilitiesIncludeFetchPricing(t *testing.T) {
	capabilities := NewAntigravity().Capabilities()
	for _, capability := range capabilities {
		if capability == CapabilityFetchPricing {
			return
		}
	}
	t.Fatalf("antigravity capabilities missing fetch pricing: %#v", capabilities)
}

func antigravityPricingByModel(items []ModelPricing, name string) *ModelPricing {
	for i := range items {
		if items[i].ModelName == name {
			return &items[i]
		}
	}
	return nil
}
