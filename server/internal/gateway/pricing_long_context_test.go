package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestLongContextRuleForModel(t *testing.T) {
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "gpt-5.4", "gpt-5.4-pro", "GPT-5.5"} {
		rule := longContextRuleForModel(model)
		if rule == nil {
			t.Fatalf("expected long context rule for %q", model)
		}
		if rule.ThresholdTokens != 272000 || rule.InputMultiplier != 2 || rule.OutputMultiplier != 1.5 {
			t.Fatalf("unexpected rule for %q: %+v", model, rule)
		}
	}
	for _, model := range []string{"", "gpt-5.3", "gpt-4o", "claude-opus-4-8", "gpt-5.6-codex", "gpt-5.4-mini", "gpt-image-2", "gpt-5.3-codex-spark", "grok-4.5"} {
		if rule := longContextRuleForModel(model); rule != nil {
			t.Fatalf("expected no rule for %q, got %+v", model, rule)
		}
	}
}

func longContextTestPricing() selectedPricing {
	input := 5.0
	output := 30.0
	cacheRatio := 0.1
	return selectedPricing{
		Currency:        "USD",
		InputValue:      &input,
		OutputValue:     &output,
		CacheRatio:      &cacheRatio,
		LongContextRule: &longContextRule{ThresholdTokens: 272000, InputMultiplier: 2, OutputMultiplier: 1.5},
	}
}

func TestApplyLongContextPricingBelowThreshold(t *testing.T) {
	pricing := applyLongContextPricing(gatewayUsage{PromptTokens: 272000, CompletionTokens: 100}, longContextTestPricing())
	if pricing.LongContextApplied {
		t.Fatal("threshold boundary must not trigger the long context tier")
	}
	if *pricing.InputValue != 5 || *pricing.OutputValue != 30 {
		t.Fatalf("prices must stay unchanged, got %v/%v", *pricing.InputValue, *pricing.OutputValue)
	}
}

func TestApplyLongContextPricingAboveThreshold(t *testing.T) {
	usage := gatewayUsage{PromptTokens: 272001, CachedPromptTokens: 100000, CompletionTokens: 2000}
	pricing := applyLongContextPricing(usage, longContextTestPricing())
	if !pricing.LongContextApplied {
		t.Fatal("expected long context tier applied")
	}
	if *pricing.InputValue != 10 || *pricing.OutputValue != 45 {
		t.Fatalf("expected 2x input and 1.5x output, got %v/%v", *pricing.InputValue, *pricing.OutputValue)
	}

	cost := estimateCost(usage, pricing)
	if cost == nil {
		t.Fatal("expected cost")
	}
	uncached := float64(272001-100000) * 10 / 1_000_000
	cached := float64(100000) * 10 * 0.1 / 1_000_000
	completion := float64(2000) * 45 / 1_000_000
	expected := uncached + cached + completion
	if diff := *cost - expected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected cost %v, got %v", expected, *cost)
	}
}

func TestApplyLongContextPricingIdempotent(t *testing.T) {
	usage := gatewayUsage{PromptTokens: 300000}
	pricing := applyLongContextPricing(usage, longContextTestPricing())
	again := applyLongContextPricing(usage, pricing)
	if *again.InputValue != 10 {
		t.Fatalf("second application must not double the price again, got %v", *again.InputValue)
	}
}

func TestApplyLongContextPricingSkipsImagesAndPerRequest(t *testing.T) {
	imagePricing := applyLongContextPricing(gatewayUsage{PromptTokens: 300000, ImageCount: 1}, longContextTestPricing())
	if imagePricing.LongContextApplied {
		t.Fatal("image usage must not trigger the long context tier")
	}

	perRequest := longContextTestPricing()
	perRequestValue := 0.1
	billingType := "per_request"
	perRequest.PerRequestValue = &perRequestValue
	perRequest.BillingType = billingType
	result := applyLongContextPricing(gatewayUsage{PromptTokens: 300000}, perRequest)
	if result.LongContextApplied {
		t.Fatal("per-request pricing must not trigger the long context tier")
	}
}

func TestLongContextOfficialPriceTable(t *testing.T) {
	cases := []struct {
		model          string
		input          float64
		output         float64
		expectedInput  float64
		expectedOutput float64
	}{
		{model: "gpt-5.6-sol", input: 5, output: 30, expectedInput: 10, expectedOutput: 45},
		{model: "gpt-5.6-terra", input: 2.5, output: 15, expectedInput: 5, expectedOutput: 22.5},
		{model: "gpt-5.6-luna", input: 1, output: 6, expectedInput: 2, expectedOutput: 9},
		{model: "gpt-5.5", input: 4, output: 24, expectedInput: 8, expectedOutput: 36},
	}
	for _, testCase := range cases {
		input := testCase.input
		output := testCase.output
		cacheRatio := 0.1
		pricing := selectedPricing{
			Currency:        "USD",
			InputValue:      &input,
			OutputValue:     &output,
			CacheRatio:      &cacheRatio,
			LongContextRule: longContextRuleForModel(testCase.model),
		}
		if pricing.LongContextRule == nil {
			t.Fatalf("%s: expected rule", testCase.model)
		}
		applied := applyLongContextPricing(gatewayUsage{PromptTokens: 300000}, pricing)
		if *applied.InputValue != testCase.expectedInput || *applied.OutputValue != testCase.expectedOutput {
			t.Fatalf("%s: expected %v/%v, got %v/%v", testCase.model, testCase.expectedInput, testCase.expectedOutput, *applied.InputValue, *applied.OutputValue)
		}
		cachedPrice := *applied.InputValue * cacheRatio
		if cachedPrice != testCase.expectedInput*0.1 {
			t.Fatalf("%s: expected cached input price %v, got %v", testCase.model, testCase.expectedInput*0.1, cachedPrice)
		}
	}
}

func TestLongContextExactCostSol(t *testing.T) {
	usage := gatewayUsage{PromptTokens: 300000, CachedPromptTokens: 200000, CompletionTokens: 10000}
	pricing := applyLongContextPricing(usage, longContextTestPricing())
	cost := estimateCost(usage, pricing)
	if cost == nil {
		t.Fatal("expected cost")
	}
	expected := 100000*10.0/1_000_000 + 200000*1.0/1_000_000 + 10000*45.0/1_000_000
	if diff := *cost - expected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected %v (uncached $1.00 + cached $0.20 + output $0.45), got %v", expected, *cost)
	}

	short := gatewayUsage{PromptTokens: 200000, CachedPromptTokens: 100000, CompletionTokens: 10000}
	shortPricing := applyLongContextPricing(short, longContextTestPricing())
	if shortPricing.LongContextApplied {
		t.Fatal("200k input must stay on the short tier")
	}
	shortCost := estimateCost(short, shortPricing)
	shortExpected := 100000*5.0/1_000_000 + 100000*0.5/1_000_000 + 10000*30.0/1_000_000
	if diff := *shortCost - shortExpected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected short-tier cost %v, got %v", shortExpected, *shortCost)
	}
}

func TestLongContextThresholdCountsCachedInput(t *testing.T) {
	usage := gatewayUsage{PromptTokens: 280000, CachedPromptTokens: 270000, CompletionTokens: 10}
	pricing := applyLongContextPricing(usage, longContextTestPricing())
	if !pricing.LongContextApplied {
		t.Fatal("threshold must be evaluated on total input tokens including cached")
	}
}

func TestLongContextDoesNotApplyGroupRatioTwice(t *testing.T) {
	usage := gatewayUsage{PromptTokens: 300000, CompletionTokens: 1000}
	pricing := longContextTestPricing()
	groupRatio := 2.0
	pricing.GroupRatio = &groupRatio
	pricing = applyLongContextPricing(usage, pricing)
	cost := estimateCost(usage, pricing)
	expected := 300000*10.0/1_000_000 + 1000*45.0/1_000_000
	if diff := *cost - expected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected effective upstream price without a second group ratio, %v got %v", expected, *cost)
	}
}

func TestLongContextNotAppliedWithoutRule(t *testing.T) {
	usage := gatewayUsage{PromptTokens: 500000, CompletionTokens: 1000}
	input := 3.0
	output := 15.0
	pricing := selectedPricing{
		Currency:        "USD",
		InputValue:      &input,
		OutputValue:     &output,
		LongContextRule: longContextRuleForModel("claude-opus-4-8"),
	}
	result := applyLongContextPricing(usage, pricing)
	if result.LongContextApplied || *result.InputValue != 3 {
		t.Fatalf("models without a rule must never be scaled, got %+v", result)
	}
}

func TestCostCalculationMetadataLongContext(t *testing.T) {
	usage := gatewayUsage{PromptTokens: 300000, CompletionTokens: 500}
	pricing := applyLongContextPricing(usage, longContextTestPricing())
	cost := estimateCost(usage, pricing)
	meta := costCalculationMetadata(usage, pricing, cost, billingAdjustment{Multiplier: 1}, nil)
	if meta["long_context"] != true {
		t.Fatalf("expected long_context true, got %v", meta["long_context"])
	}
	if meta["long_context_threshold_tokens"] != 272000 {
		t.Fatalf("expected threshold in metadata, got %v", meta["long_context_threshold_tokens"])
	}
	if meta["input_value"] != 10.0 {
		t.Fatalf("expected effective input value in metadata, got %v", meta["input_value"])
	}

	short := gatewayUsage{PromptTokens: 1000, CompletionTokens: 50}
	shortPricing := applyLongContextPricing(short, longContextTestPricing())
	shortMeta := costCalculationMetadata(short, shortPricing, estimateCost(short, shortPricing), billingAdjustment{Multiplier: 1}, nil)
	if shortMeta["long_context"] != false {
		t.Fatalf("expected long_context false for short request, got %v", shortMeta["long_context"])
	}
}

func TestLongContextDoublesCacheWritePricing(t *testing.T) {
	usage := gatewayUsage{PromptTokens: 300000, CacheWriteTokens: 50000, CompletionTokens: 1000}
	pricing := longContextTestPricing()
	cacheWriteRatio := 1.25
	pricing.CacheWriteRatio = &cacheWriteRatio
	pricing = applyLongContextPricing(usage, pricing)
	cost := estimateCost(usage, pricing)
	if cost == nil {
		t.Fatal("expected cost")
	}
	expected := 250000*10.0/1_000_000 + 50000*10.0*1.25/1_000_000 + 1000*45.0/1_000_000
	if diff := *cost - expected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected cache write billed at doubled input price (%v), got %v", expected, *cost)
	}

	breakdown := gatewayUsage{PromptTokens: 300000, CacheCreationInputTokens: 40000, CacheCreation5mInputTokens: 30000, CacheCreation1hInputTokens: 10000, CompletionTokens: 1000}
	oneHourRatio := 2.0
	tiered := longContextTestPricing()
	tiered.CacheWriteRatio = &cacheWriteRatio
	tiered.CacheWrite1hRatio = &oneHourRatio
	tiered = applyLongContextPricing(breakdown, tiered)
	tieredCost := estimateCost(breakdown, tiered)
	tieredExpected := 260000*10.0/1_000_000 + 30000*10.0*1.25/1_000_000 + 10000*10.0*2.0/1_000_000 + 1000*45.0/1_000_000
	if diff := *tieredCost - tieredExpected; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("expected 5m/1h cache writes billed at doubled input price (%v), got %v", tieredExpected, *tieredCost)
	}
}

func TestLongContextBufferedResponsesFlowPersistsTieredCost(t *testing.T) {
	candidate := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.6-sol"}}
	pricing := longContextTestPricing()
	pricing.LongContextRule = longContextRuleForModel(candidate.Model.UpstreamName)
	responseBody := `{"id":"resp_long","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":300000,"output_tokens":10000,"total_tokens":310000,"input_tokens_details":{"cached_tokens":200000}}}`

	result := (Handler{logger: gatewayDiscardLogger()}).handleBufferedResponse(
		context.Background(),
		"req-long-buffered",
		uuid.Nil,
		uuid.Nil,
		candidate,
		openAIResponsesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses},
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		},
		gatewayAttemptResult{attempt: 1, currency: "USD", downstreamPath: gatewayEndpointResponses, pricing: pricing},
		time.Now(),
	)

	if !result.success || result.promptTokens != 300000 || result.cachedPromptTokens != 200000 || result.completionTokens != 10000 {
		t.Fatalf("buffered long-context usage = %#v", result)
	}
	if !result.pricing.LongContextApplied || *result.pricing.InputValue != 10 || *result.pricing.OutputValue != 45 {
		t.Fatalf("buffered long-context pricing = %#v", result.pricing)
	}
	expectedCost := 1.65
	if result.estimatedCost == nil || *result.estimatedCost != expectedCost {
		t.Fatalf("buffered long-context cost = %v, want %v", result.estimatedCost, expectedCost)
	}

	metadata := attemptMetadata(context.Background(), "req-long-buffered:1", "req-long-buffered", uuid.Nil, uuid.Nil, candidate, result)
	calculation, ok := metadata["cost_calculation"].(map[string]any)
	if !ok || calculation["long_context"] != true || calculation["long_context_input_multiplier"] != 2.0 || calculation["long_context_output_multiplier"] != 1.5 {
		t.Fatalf("buffered long-context metadata = %#v", metadata["cost_calculation"])
	}

	db := gatewayOfflineGorm(t)
	var persisted store.UsageRecord
	if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.UsageRecord)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		item.ID = uuid.New()
		persisted = *item
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace usage create callback: %v", err)
	}
	_, err := store.NewUsageRecordRepository(db).Create(context.Background(), store.CreateUsageRecordParams{
		RequestLogID:     uuid.New(),
		PromptTokens:     result.promptTokens,
		CompletionTokens: result.completionTokens,
		TotalTokens:      result.promptTokens + result.completionTokens,
		CachedTokens:     result.cachedPromptTokens,
		EstimatedCost:    *result.estimatedCost,
		Currency:         result.currency,
	})
	if err != nil {
		t.Fatalf("persist long-context usage: %v", err)
	}
	if !persisted.EstimatedCost.Valid || persisted.EstimatedCost.Float64 != expectedCost || persisted.PromptTokens != 300000 || persisted.CachedTokens.Int64 != 200000 {
		t.Fatalf("persisted long-context usage = %#v", persisted)
	}
}

func TestLongContextStreamingFlowUsesTieredCost(t *testing.T) {
	pricing := longContextTestPricing()
	result := (Handler{logger: gatewayDiscardLogger()}).handleStreamResponse(
		context.Background(),
		httptest.NewRecorder(),
		"req-long-stream",
		uuid.Nil,
		uuid.Nil,
		routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.6-sol"}},
		&testGatewayProtocol{
			streamCapture: streamCaptureState{
				usage:           gatewayUsage{PromptTokens: 300001, CompletionTokens: 1000},
				streamCompleted: true,
				sawDone:         true,
				endReason:       "done",
			},
			streamStarted: true,
		},
		&http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(""))},
		gatewayAttemptResult{attempt: 1, currency: "USD", downstreamPath: gatewayEndpointResponses, pricing: pricing, stream: true},
		time.Now(),
	)

	expectedCost := 300001*10.0/1_000_000 + 1000*45.0/1_000_000
	if !result.success || !result.pricing.LongContextApplied || result.estimatedCost == nil {
		t.Fatalf("streaming long-context result = %#v", result)
	}
	if diff := *result.estimatedCost - expectedCost; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("streaming long-context cost = %v, want %v", *result.estimatedCost, expectedCost)
	}
}
