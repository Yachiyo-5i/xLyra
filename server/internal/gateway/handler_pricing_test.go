package gateway

import (
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestEstimateCostUsesPerRequestPricing(t *testing.T) {
	t.Parallel()

	quotaType := int64(1)
	perRequestValue := 0.08
	pricing := selectedPricing{
		Currency:        "USD",
		PerRequestValue: &perRequestValue,
		BillingType:     "per_request",
		QuotaType:       &quotaType,
	}

	got := estimateCost(completionUsage{PromptTokens: 1234, CompletionTokens: 5678}, pricing)
	if got == nil {
		t.Fatal("expected per-request cost")
	}
	if *got != perRequestValue {
		t.Fatalf("expected %f, got %f", perRequestValue, *got)
	}
}

func TestEstimateCostKeepsTokenPricing(t *testing.T) {
	t.Parallel()

	inputValue := 1.5
	outputValue := 2.5
	pricing := selectedPricing{
		Currency:    "USD",
		InputValue:  &inputValue,
		OutputValue: &outputValue,
		BillingType: "tokens",
	}

	got := estimateCost(completionUsage{PromptTokens: 1_000_000, CompletionTokens: 2_000_000}, pricing)
	if got == nil {
		t.Fatal("expected token cost")
	}
	want := 6.5
	if *got != want {
		t.Fatalf("expected %f, got %f", want, *got)
	}
}

func TestEstimateCostIncludesCachedPromptTokens(t *testing.T) {
	t.Parallel()

	inputValue := 2.5
	outputValue := 15.0
	cacheRatio := 0.1
	pricing := selectedPricing{
		Currency:    "USD",
		InputValue:  &inputValue,
		OutputValue: &outputValue,
		CacheRatio:  &cacheRatio,
		BillingType: "tokens",
	}

	got := estimateCost(gatewayUsage{
		PromptTokens:       1_000_000,
		CachedPromptTokens: 400_000,
		CompletionTokens:   200_000,
	}, pricing)
	if got == nil {
		t.Fatal("expected token cost")
	}
	want := 4.6
	if *got != want {
		t.Fatalf("expected %f, got %f", want, *got)
	}
}

func TestEstimateCostUsesImagePricing(t *testing.T) {
	t.Parallel()

	inputValue := 5.0
	imageRatio := 2.0
	pricing := selectedPricing{
		Currency:   "USD",
		InputValue: &inputValue,
		ImageRatio: &imageRatio,
	}

	got := estimateCost(gatewayUsage{ImageCount: 3}, pricing)
	if got == nil {
		t.Fatal("expected image cost")
	}
	want := 30.0
	if *got != want {
		t.Fatalf("expected %f, got %f", want, *got)
	}
}

func TestEstimateCostUsesImageTokenPricing(t *testing.T) {
	t.Parallel()

	inputValue := 5.0
	outputValue := 30.0
	imageRatio := 1.6
	pricing := selectedPricing{
		Currency:    "USD",
		InputValue:  &inputValue,
		OutputValue: &outputValue,
		ImageRatio:  &imageRatio,
	}

	got := estimateCost(gatewayUsage{
		ImageCount:        1,
		InputTextTokens:   1_000_000,
		InputImageTokens:  500_000,
		OutputImageTokens: 2_000_000,
	}, pricing)
	if got == nil {
		t.Fatal("expected image token cost")
	}
	want := 69.0
	if *got != want {
		t.Fatalf("expected %f, got %f", want, *got)
	}
}

func TestBillingAdjustmentFromPayloadUsesFastModeByModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		payload        map[string]any
		candidateModel string
		wantMode       string
		wantMultiplier float64
		wantReason     string
	}{
		{
			name: "gpt-5.5 fast",
			payload: map[string]any{
				"model":        "gpt-5.5-codex",
				"service_tier": "fast",
			},
			wantMode:       "fast",
			wantMultiplier: 2.5,
			wantReason:     "codex_fast_mode",
		},
		{
			name: "gpt-5.4 fast",
			payload: map[string]any{
				"model":        "gpt-5.4-codex",
				"service_tier": "fast",
			},
			wantMode:       "fast",
			wantMultiplier: 2,
			wantReason:     "codex_fast_mode",
		},
		{
			name: "gpt-5.4 mini fast has no surcharge",
			payload: map[string]any{
				"model":        "gpt-5.4-mini-codex",
				"service_tier": "fast",
			},
			wantMode:       "fast",
			wantMultiplier: 1,
		},
		{
			name: "priority alias is fast",
			payload: map[string]any{
				"model":        "gpt-5.5-codex",
				"service_tier": "priority",
			},
			wantMode:       "fast",
			wantMultiplier: 2.5,
			wantReason:     "codex_fast_mode",
		},
		{
			name: "non fast",
			payload: map[string]any{
				"model":        "gpt-5.5-codex",
				"service_tier": "flex",
			},
			wantMultiplier: 1,
		},
		{
			name: "falls back to candidate model",
			payload: map[string]any{
				"service_tier": "fast",
			},
			candidateModel: "gpt-5.4-codex",
			wantMode:       "fast",
			wantMultiplier: 2,
			wantReason:     "codex_fast_mode",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := billingAdjustmentFromPayload(tt.payload, routeengine.Candidate{
				Site: routeengine.CandidateSite{
					SiteType: "newapi",
				},
				Model: routeengine.CandidateModel{
					UpstreamName: tt.candidateModel,
				},
			})

			if got.Mode != tt.wantMode {
				t.Fatalf("mode = %q, want %q", got.Mode, tt.wantMode)
			}
			if got.Multiplier != tt.wantMultiplier {
				t.Fatalf("multiplier = %v, want %v", got.Multiplier, tt.wantMultiplier)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestApplyEstimatedCostBillingAdjustmentMultipliesFastCost(t *testing.T) {
	t.Parallel()

	baseCost := 0.25
	result := applyEstimatedCostBillingAdjustment(gatewayAttemptResult{
		estimatedCost:            &baseCost,
		credentialCostMultiplier: 1.2,
		billingMode:              "fast",
		costMultiplier:           2.5,
	})

	if result.baseEstimatedCost == nil || *result.baseEstimatedCost != baseCost {
		t.Fatalf("base cost = %#v, want %v", result.baseEstimatedCost, baseCost)
	}
	if result.estimatedCost == nil || *result.estimatedCost != 0.75 {
		t.Fatalf("estimated cost = %#v, want 0.75", result.estimatedCost)
	}
}

func TestApplyEstimatedCostBillingAdjustmentSnapshotsUnmodifiedBaseCost(t *testing.T) {
	t.Parallel()

	baseCost := 0.25
	result := applyEstimatedCostBillingAdjustment(gatewayAttemptResult{estimatedCost: &baseCost})
	if result.baseEstimatedCost == nil || *result.baseEstimatedCost != baseCost || result.estimatedCost == nil || *result.estimatedCost != baseCost {
		t.Fatalf("unmodified cost snapshot = %#v", result)
	}
}
