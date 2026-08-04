package router

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"

	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestNewServiceKeepsStoreDependency(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	if service == nil {
		t.Fatal("expected service")
	}
	if service.db != nil {
		t.Fatalf("service db = %#v, want nil", service.db)
	}
}

func TestCandidatesRejectsBlankModelKeyBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	_, err := service.Candidates(context.Background(), CandidateQuery{ModelKey: " \t\n "})
	if err == nil {
		t.Fatal("expected blank model key to be rejected")
	}
	if err.Error() != "model_key is required" {
		t.Fatalf("error = %v, want model_key is required", err)
	}
}

func TestSelectAndPlanRejectBlankModelKeyBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := NewService(nil)
	if _, err := service.Select(context.Background(), CandidateQuery{ModelKey: " \t\n "}); err == nil || err.Error() != "model_key is required" {
		t.Fatalf("Select blank model error = %v, want model_key is required", err)
	}
	if _, err := service.Plan(context.Background(), CandidateQuery{ModelKey: " \t\n "}); err == nil || err.Error() != "model_key is required" {
		t.Fatalf("Plan blank model error = %v, want model_key is required", err)
	}
}

func TestRouteCandidateSupportsEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		endpointType string
		supported    []string
		want         bool
	}{
		{
			name:         "empty endpoint type matches all",
			endpointType: "",
			supported:    []string{"openai"},
			want:         true,
		},
		{
			name:         "exact match openai",
			endpointType: "openai",
			supported:    []string{"openai"},
			want:         true,
		},
		{
			name:         "exact match openai-response",
			endpointType: "openai-response",
			supported:    []string{"openai-response"},
			want:         true,
		},
		{
			name:         "openai-response compatible with openai",
			endpointType: "openai-response",
			supported:    []string{"openai"},
			want:         true,
		},
		{
			name:         "openai compatible with openai-response",
			endpointType: "openai",
			supported:    []string{"openai-response"},
			want:         true,
		},
		{
			name:         "openai-image not compatible with openai",
			endpointType: "openai-image",
			supported:    []string{"openai"},
			want:         false,
		},
		{
			name:         "openai not compatible with openai-image",
			endpointType: "openai",
			supported:    []string{"openai-image"},
			want:         false,
		},
		{
			name:         "anthropic-messages compatible with openai",
			endpointType: "anthropic-messages",
			supported:    []string{"openai"},
			want:         true,
		},
		{
			name:         "google-gemini compatible with openai-response",
			endpointType: "google-gemini",
			supported:    []string{"openai-response"},
			want:         true,
		},
		{
			name:         "embedding only compatible with embedding",
			endpointType: "openai-embedding",
			supported:    []string{"openai-embedding"},
			want:         true,
		},
		{
			name:         "embedding not compatible with text",
			endpointType: "openai-embedding",
			supported:    []string{"openai"},
			want:         false,
		},
		{
			name:         "unknown endpoint type rejected",
			endpointType: "unknown",
			supported:    []string{"unknown"},
			want:         false,
		},
		{
			name:         "no supported types",
			endpointType: "openai",
			supported:    []string{},
			want:         false,
		},
		{
			name:         "multiple supported types with match",
			endpointType: "openai-response",
			supported:    []string{"openai-image", "openai"},
			want:         true,
		},
		{
			name:         "trims and normalizes requested endpoint",
			endpointType: " OpenAI-Response ",
			supported:    []string{"openai"},
			want:         true,
		},
		{
			name:         "trims and normalizes supported endpoint",
			endpointType: "openai-image",
			supported:    []string{" OPENAI-IMAGE "},
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := store.RouteCandidateRow{
				SupportedEndpointTypes: tt.supported,
			}
			got := routeCandidateSupportsEndpoint(row, tt.endpointType)
			if got != tt.want {
				t.Fatalf("routeCandidateSupportsEndpoint(%q, %v) = %v, want %v", tt.endpointType, tt.supported, got, tt.want)
			}
		})
	}
}

func TestEndpointTypeFamily(t *testing.T) {
	t.Parallel()

	for _, endpointType := range []string{"openai", "openai-response", "anthropic-messages", "google-gemini"} {
		if got := endpointTypeFamily(endpointType); got != "text" {
			t.Fatalf("endpointTypeFamily(%q) = %q, want text", endpointType, got)
		}
	}
	if got := endpointTypeFamily("openai-image"); got != "image" {
		t.Fatalf("endpointTypeFamily(openai-image) = %q, want image", got)
	}
	if got := endpointTypeFamily("openai-embedding"); got != "embedding" {
		t.Fatalf("endpointTypeFamily(openai-embedding) = %q, want embedding", got)
	}
	if got := endpointTypeFamily("unknown"); got != "" {
		t.Fatalf("endpointTypeFamily(unknown) = %q, want empty", got)
	}
}

func TestSiteResponsesToolConfigFromMeta(t *testing.T) {
	t.Parallel()

	meta := store.JSON(`{"gateway":{"responses_tool_policy":"compatibility","disabled_responses_tools":["image_generation"]}}`)

	cfg := siteGatewayConfig(meta)
	if cfg == nil || cfg.ResponsesToolPolicy != sitepkg.ResponsesToolPolicyCompatibility {
		t.Fatalf("siteGatewayConfig = %#v, want compatibility config", cfg)
	}
	if got := siteResponsesToolPolicy(meta); got != sitepkg.ResponsesToolPolicyCompatibility {
		t.Fatalf("siteResponsesToolPolicy = %q, want %q", got, sitepkg.ResponsesToolPolicyCompatibility)
	}
	tools := siteDisabledResponsesTools(meta)
	if len(tools) != 1 || tools[0] != sitepkg.ResponsesHostedToolImageGeneration {
		t.Fatalf("siteDisabledResponsesTools = %#v, want [%q]", tools, sitepkg.ResponsesHostedToolImageGeneration)
	}
}

func TestSiteResponsesToolConfigDefaultsForMissingOrInvalidMeta(t *testing.T) {
	t.Parallel()

	for _, meta := range []store.JSON{
		nil,
		store.JSON(`{}`),
		store.JSON(`{"gateway":{"responses_tool_policy":"unknown","disabled_responses_tools":["unknown"]}}`),
		store.JSON(`{invalid-json`),
	} {
		if got := siteResponsesToolPolicy(meta); got != sitepkg.ResponsesToolPolicyPassthrough {
			t.Fatalf("siteResponsesToolPolicy(%s) = %q, want passthrough", meta, got)
		}
		if tools := siteDisabledResponsesTools(meta); len(tools) != 0 {
			t.Fatalf("siteDisabledResponsesTools(%s) = %#v, want empty", meta, tools)
		}
	}
}

func TestCandidatePriceValueUsesPerRequestBeforeTokenPrices(t *testing.T) {
	t.Parallel()

	input := 1.2
	output := 3.4
	perRequest := 0.5
	got, ok := candidatePriceValue(Candidate{
		Pricing: CandidatePricing{
			InputValue:      &input,
			OutputValue:     &output,
			PerRequestValue: &perRequest,
		},
	})
	if !ok {
		t.Fatal("expected price value")
	}
	if got != perRequest {
		t.Fatalf("candidatePriceValue = %v, want %v", got, perRequest)
	}
}

func TestCandidatePriceValueSumsTokenPrices(t *testing.T) {
	t.Parallel()

	input := 1.2
	output := 3.4
	got, ok := candidatePriceValue(Candidate{
		Pricing: CandidatePricing{
			InputValue:  &input,
			OutputValue: &output,
		},
	})
	if !ok {
		t.Fatal("expected price value")
	}
	if got != input+output {
		t.Fatalf("candidatePriceValue = %v, want %v", got, input+output)
	}
}

func TestCandidatePriceValueHandlesPartialTokenPricesAndMissing(t *testing.T) {
	t.Parallel()

	input := 1.2
	got, ok := candidatePriceValue(Candidate{Pricing: CandidatePricing{InputValue: &input}})
	if !ok || got != input {
		t.Fatalf("input-only price = %v, %v; want %v true", got, ok, input)
	}

	output := 3.4
	got, ok = candidatePriceValue(Candidate{Pricing: CandidatePricing{OutputValue: &output}})
	if !ok || got != output {
		t.Fatalf("output-only price = %v, %v; want %v true", got, ok, output)
	}

	got, ok = candidatePriceValue(Candidate{})
	if ok || got != 0 {
		t.Fatalf("missing price = %v, %v; want 0 false", got, ok)
	}
}

func TestCandidatePriceValueTreatsZeroPriceAsKnown(t *testing.T) {
	t.Parallel()

	zero := 0.0
	got, ok := candidatePriceValue(Candidate{
		Pricing: CandidatePricing{
			PerRequestValue: &zero,
		},
	})
	if !ok || got != 0 {
		t.Fatalf("zero per-request price = %v, %v; want 0 true", got, ok)
	}

	got, ok = candidatePriceValue(Candidate{
		Pricing: CandidatePricing{
			InputValue:  &zero,
			OutputValue: &zero,
		},
	})
	if !ok || got != 0 {
		t.Fatalf("zero token prices = %v, %v; want 0 true", got, ok)
	}
}

func TestPriceScorePrefersLowerKnownPrice(t *testing.T) {
	t.Parallel()

	low := 1.0
	high := 5.0
	lowScore := priceScore(Candidate{Pricing: CandidatePricing{PerRequestValue: &low}}, low, high)
	highScore := priceScore(Candidate{Pricing: CandidatePricing{PerRequestValue: &high}}, low, high)
	if lowScore <= highScore {
		t.Fatalf("expected lower price to score higher, got low=%v high=%v", lowScore, highScore)
	}
}

func TestPriceScoreDefaultsForMissingOrSinglePrice(t *testing.T) {
	t.Parallel()

	if got := priceScore(Candidate{}, 1, 5); got != 2 {
		t.Fatalf("missing price score = %v, want 2", got)
	}

	value := 3.0
	if got := priceScore(Candidate{Pricing: CandidatePricing{PerRequestValue: &value}}, value, value); got != 10 {
		t.Fatalf("single known price score = %v, want 10", got)
	}
}

func TestScoreCandidateIncludesHealthCapacityAndPrice(t *testing.T) {
	t.Parallel()

	siteRate := 1.0
	modelRate := 0.5
	siteLatency := int64(1200)
	modelLatency := int64(400)
	price := 2.0
	score, breakdown := scoreCandidate(Candidate{
		Site: CandidateSite{
			RoutingPriority: 2.0,
		},
		Health: CandidateHealth{
			Status:             "healthy",
			RecentSuccessRate:  &siteRate,
			RecentAvgLatencyMS: &siteLatency,
			ModelSuccessRate:   &modelRate,
			ModelAvgLatencyMS:  &modelLatency,
		},
		Availability: CandidateAvailability{
			AvailableAPIKeys: 6,
		},
		Pricing: CandidatePricing{
			PerRequestValue: &price,
		},
	}, 1, 3)

	wantBreakdown := map[string]float64{
		"site_health":        40,
		"site_success_rate":  10,
		"site_latency":       3,
		"model_success_rate": 15,
		"model_latency":      15,
		"api_key_capacity":   15,
		"price":              5,
	}
	for key, want := range wantBreakdown {
		if breakdown[key] != want {
			t.Fatalf("breakdown[%q] = %v, want %v", key, breakdown[key], want)
		}
	}
	if _, ok := breakdown["routing_priority"]; ok {
		t.Fatalf("breakdown should not include routing_priority, got %#v", breakdown)
	}

	if score != 103 {
		t.Fatalf("scoreCandidate = %v, want 103", score)
	}
}

func TestHealthAndLatencyScoreBuckets(t *testing.T) {
	t.Parallel()

	if healthScore("healthy") != 40 || healthScore("degraded") != 22 || healthScore("unknown") != 12 || healthScore("down") != 0 {
		t.Fatalf("unexpected health scores")
	}
	if got := successRateScore(nil, 30); got != 0 {
		t.Fatalf("nil success rate score = %v, want 0", got)
	}
	rate := 0.75
	if got := successRateScore(&rate, 20); got != 15 {
		t.Fatalf("success rate score = %v, want 15", got)
	}

	for _, tc := range []struct {
		latency int64
		want    float64
	}{
		{latency: 500, want: 10},
		{latency: 1500, want: 6},
		{latency: 3000, want: 2.5},
		{latency: 3001, want: 0},
	} {
		if got := latencyScore(&tc.latency, 10); got != tc.want {
			t.Fatalf("latencyScore(%d) = %v, want %v", tc.latency, got, tc.want)
		}
	}
	if got := latencyScore(nil, 10); got != 0 {
		t.Fatalf("nil latency score = %v, want 0", got)
	}
}

func TestIndexCooldownsSeparatesSiteAndModelAndIgnoresCredential(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	credentialID := uuid.New()
	modelCooldownID := uuid.New()
	credentialCooldownID := uuid.New()

	index := indexCooldowns([]store.RouteCooldown{
		{ID: siteID, SiteID: siteID},
		{
			ID:          modelCooldownID,
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
		},
		{
			ID:               credentialCooldownID,
			SiteID:           siteID,
			SiteModelID:      uuid.NullUUID{UUID: modelID, Valid: true},
			SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		},
	})

	if got := index.sites[siteID].ID; got != siteID {
		t.Fatalf("site cooldown ID = %s, want %s", got, siteID)
	}
	if got := index.models[modelID].ID; got != modelCooldownID {
		t.Fatalf("model cooldown ID = %s, want %s", got, modelCooldownID)
	}
	if len(index.sites) != 1 || len(index.models) != 1 {
		t.Fatalf("unexpected cooldown maps: site=%v model=%v", index.sites, index.models)
	}
}

func TestUUIDSet(t *testing.T) {
	t.Parallel()

	if got := uuidSet(nil); got != nil {
		t.Fatalf("uuidSet(nil) = %v, want nil", got)
	}

	id := uuid.New()
	got := uuidSet([]uuid.UUID{id, id})
	if _, ok := got[id]; !ok {
		t.Fatalf("uuidSet missing %s", id)
	}
	if len(got) != 1 {
		t.Fatalf("uuidSet length = %d, want 1", len(got))
	}
}

func TestNullableRouteValues(t *testing.T) {
	t.Parallel()

	floatValue := nullableFloat(sql.NullFloat64{Float64: 1.25, Valid: true})
	if floatValue == nil || *floatValue != 1.25 {
		t.Fatalf("nullableFloat valid = %#v, want 1.25", floatValue)
	}
	if nullableFloat(sql.NullFloat64{}) != nil {
		t.Fatal("nullableFloat invalid should be nil")
	}

	intValue := nullableInt64(sql.NullInt64{Int64: 42, Valid: true})
	if intValue == nil || *intValue != 42 {
		t.Fatalf("nullableInt64 valid = %#v, want 42", intValue)
	}
	if nullableInt64(sql.NullInt64{}) != nil {
		t.Fatal("nullableInt64 invalid should be nil")
	}

	stringValue := nullableString(sql.NullString{String: "ok", Valid: true})
	if stringValue == nil || *stringValue != "ok" {
		t.Fatalf("nullableString valid = %#v, want ok", stringValue)
	}
	if nullableString(sql.NullString{}) != nil {
		t.Fatal("nullableString invalid should be nil")
	}
}

func TestRouteMin(t *testing.T) {
	t.Parallel()

	if got := min(2, 5); got != 2 {
		t.Fatalf("min(2, 5) = %d, want 2", got)
	}
	if got := min(7, 3); got != 3 {
		t.Fatalf("min(7, 3) = %d, want 3", got)
	}
}
