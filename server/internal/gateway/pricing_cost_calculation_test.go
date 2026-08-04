package gateway

import (
	"math"
	"testing"
)

func pricingAmount(value float64) *float64 {
	return &value
}

func TestPerRequestCostPrefersExplicitValue(t *testing.T) {
	t.Parallel()

	perRequestValue := pricingAmount(0.03)
	got, ok := perRequestCost(selectedPricing{
		PerRequestValue: perRequestValue,
		ModelPrice:      pricingAmount(0.10),
		GroupRatio:      pricingAmount(2.0),
	})

	if !ok {
		t.Fatal("expected explicit per-request value to be usable")
	}
	if got != *perRequestValue {
		t.Fatalf("per-request cost = %v, want explicit value %v", got, *perRequestValue)
	}
}

func TestPerRequestCostDerivesFromModelPriceAndGroupRatio(t *testing.T) {
	t.Parallel()

	modelPrice := pricingAmount(0.10)

	got, ok := perRequestCost(selectedPricing{
		ModelPrice: modelPrice,
		GroupRatio: pricingAmount(1.5),
	})

	if !ok {
		t.Fatal("expected model price to be usable")
	}
	if math.Abs(got-0.15) > 0.0000001 {
		t.Fatalf("per-request cost = %v, want model price multiplied by group ratio", got)
	}

	got, ok = perRequestCost(selectedPricing{ModelPrice: modelPrice})
	if !ok {
		t.Fatal("expected model price without group ratio to be usable")
	}
	if got != *modelPrice {
		t.Fatalf("per-request cost = %v, want raw model price %v", got, *modelPrice)
	}

	if got, ok := perRequestCost(selectedPricing{}); ok || got != 0 {
		t.Fatalf("missing per-request pricing = (%v, %v), want (0, false)", got, ok)
	}
}

func TestImageUnitCostRequiresInputAndRatio(t *testing.T) {
	t.Parallel()

	inputValue := pricingAmount(2.0)
	imageRatio := pricingAmount(0.25)

	got, ok := imageUnitCost(selectedPricing{
		InputValue: inputValue,
		ImageRatio: imageRatio,
	})
	if !ok {
		t.Fatal("expected image unit cost when input value and image ratio are present")
	}
	if got != 0.5 {
		t.Fatalf("image unit cost = %v, want 0.5", got)
	}

	for _, pricing := range []selectedPricing{
		{InputValue: inputValue},
		{ImageRatio: imageRatio},
		{},
	} {
		if got, ok := imageUnitCost(pricing); ok || got != 0 {
			t.Fatalf("image unit cost for incomplete pricing = (%v, %v), want (0, false)", got, ok)
		}
	}
}

func TestGatewayUsageNormalizationClampsCachedTokensAndFillsTotals(t *testing.T) {
	t.Parallel()

	got := gatewayUsage{
		PromptTokens:       100,
		CompletionTokens:   25,
		CachedPromptTokens: 150,
	}.normalized()
	if got.CachedPromptTokens != 100 {
		t.Fatalf("cached prompt tokens = %d, want prompt token cap 100", got.CachedPromptTokens)
	}
	if got.TotalTokens != 125 {
		t.Fatalf("total tokens = %d, want prompt + completion tokens", got.TotalTokens)
	}
	if got.uncachedPromptTokens() != 0 {
		t.Fatalf("uncached prompt tokens = %d, want 0 after cache cap", got.uncachedPromptTokens())
	}

	got = gatewayUsage{
		PromptTokens:       80,
		CompletionTokens:   20,
		CachedPromptTokens: -5,
	}.normalized()
	if got.CachedPromptTokens != 0 {
		t.Fatalf("negative cached prompt tokens normalized to %d, want 0", got.CachedPromptTokens)
	}
	if got.uncachedPromptTokens() != 80 {
		t.Fatalf("uncached prompt tokens = %d, want full prompt tokens", got.uncachedPromptTokens())
	}
}

func TestGatewayUsageNormalizationReadsPromptTokenDetails(t *testing.T) {
	t.Parallel()

	got := gatewayUsage{
		PromptTokens:       50,
		CompletionTokens:   10,
		PromptTokensDetail: &promptTokensDetails{CachedTokens: 15},
	}.normalized()

	if got.CachedPromptTokens != 15 {
		t.Fatalf("cached prompt tokens = %d, want details cached tokens", got.CachedPromptTokens)
	}
	if got.uncachedPromptTokens() != 35 {
		t.Fatalf("uncached prompt tokens = %d, want prompt minus cached tokens", got.uncachedPromptTokens())
	}
}

func TestGatewayUsageReadsChatCompletionsCacheWriteTokens(t *testing.T) {
	t.Parallel()

	got := gatewayUsage{
		PromptTokens:       100,
		CompletionTokens:   10,
		PromptTokensDetail: &promptTokensDetails{CachedTokens: 20, CacheWriteTokens: 30},
	}.normalized()

	if got.CacheWriteTokens != 30 {
		t.Fatalf("cache write tokens = %d, want prompt_tokens_details.cache_write_tokens", got.CacheWriteTokens)
	}
	if got.uncachedPromptTokens() != 50 {
		t.Fatalf("uncached prompt tokens = %d, want prompt minus cached minus cache write", got.uncachedPromptTokens())
	}
}

func TestEstimateCostBillsCacheWriteTokens(t *testing.T) {
	t.Parallel()

	pricing := selectedPricing{
		Currency:        "USD",
		InputValue:      pricingAmount(2.0),
		OutputValue:     pricingAmount(6.0),
		CacheWriteRatio: pricingAmount(1.25),
	}
	usage := gatewayUsage{
		PromptTokens:     1_000_000,
		CompletionTokens: 0,
		CacheWriteTokens: 400_000,
	}
	got := estimateCost(usage, pricing)
	if got == nil {
		t.Fatal("estimated cost = nil, want computed cost")
	}
	// uncached = 600k @ $2/M = 1.2 ; cache write = 400k @ $2*1.25/M = 1.0 ; total = 2.2
	want := 2.2
	if diff := *got - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("estimated cost = %v, want %v", *got, want)
	}

	meta := costCalculationMetadata(usage, pricing, got, billingAdjustment{}, nil)
	if meta["cache_write_tokens"] != 400_000 {
		t.Fatalf("metadata cache_write_tokens = %#v, want 400000", meta["cache_write_tokens"])
	}
}

func TestCostCalculationMetadataForPerRequestAndImageUnit(t *testing.T) {
	t.Parallel()

	perRequestValue := 0.02
	perRequestTotal := 0.02
	perRequestMeta := costCalculationMetadata(
		gatewayUsage{PromptTokens: 1_000, CompletionTokens: 200},
		selectedPricing{
			Currency:        "USD",
			BillingType:     "per_request",
			PerRequestValue: pricingAmount(perRequestValue),
		},
		&perRequestTotal,
		billingAdjustment{},
		nil,
	)
	if perRequestMeta["formula"] != "per_request_value" || perRequestMeta["per_request_cost"] != perRequestValue {
		t.Fatalf("unexpected per-request metadata: %#v", perRequestMeta)
	}
	if perRequestMeta["estimated_cost"] != perRequestTotal {
		t.Fatalf("per-request estimated cost = %#v, want %v", perRequestMeta["estimated_cost"], perRequestTotal)
	}

	imageUnitCost := 1.0
	imageTotal := 3.0
	imageMeta := costCalculationMetadata(
		gatewayUsage{ImageCount: 3},
		selectedPricing{
			Currency:    "USD",
			InputValue:  pricingAmount(2.0),
			ImageRatio:  pricingAmount(0.5),
			OutputValue: nil,
		},
		&imageTotal,
		billingAdjustment{},
		nil,
	)
	if imageMeta["formula"] != "image_count * input_value * image_ratio" || imageMeta["image_unit_cost"] != imageUnitCost {
		t.Fatalf("unexpected image unit metadata: %#v", imageMeta)
	}
	if imageMeta["estimated_cost"] != imageTotal {
		t.Fatalf("image estimated cost = %#v, want %v", imageMeta["estimated_cost"], imageTotal)
	}
}
