package router

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"

	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestScoreCandidateAppliesBoundaryBucketBreakdown(t *testing.T) {
	t.Parallel()

	zeroRate := 0.0
	fullRate := 1.0
	fastButNotBestLatency := int64(501)
	mediumLatency := int64(1501)
	price := 5.0

	score, breakdown := scoreCandidate(Candidate{
		Health: CandidateHealth{
			Status:             "degraded",
			RecentSuccessRate:  &zeroRate,
			RecentAvgLatencyMS: &fastButNotBestLatency,
			ModelSuccessRate:   &fullRate,
			ModelAvgLatencyMS:  &mediumLatency,
		},
		Availability: CandidateAvailability{
			AvailableAPIKeys: 99,
		},
		Pricing: CandidatePricing{
			PerRequestValue: &price,
		},
	}, 0, 10)

	wantBreakdown := map[string]float64{
		"site_health":        22,
		"site_success_rate":  0,
		"site_latency":       3,
		"model_success_rate": 30,
		"model_latency":      3.75,
		"api_key_capacity":   15,
		"price":              5,
	}
	for key, want := range wantBreakdown {
		if breakdown[key] != want {
			t.Fatalf("breakdown[%q] = %v, want %v", key, breakdown[key], want)
		}
	}
	if score != 78.75 {
		t.Fatalf("scoreCandidate = %v, want 78.75", score)
	}
}

func TestHealthSuccessAndLatencyScoresAtBucketEdges(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status string
		want   float64
	}{
		{name: "healthy status earns full score", status: "healthy", want: 40},
		{name: "blank status has no score", status: "", want: 0},
		{name: "unnormalized healthy status has no score", status: "HEALTHY", want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := healthScore(tc.status); got != tc.want {
				t.Fatalf("healthScore(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}

	zeroRate := 0.0
	fullRate := 1.0
	if got := successRateScore(&zeroRate, 7); got != 0 {
		t.Fatalf("successRateScore(zero) = %v, want 0", got)
	}
	if got := successRateScore(&fullRate, 7); got != 7 {
		t.Fatalf("successRateScore(full) = %v, want 7", got)
	}

	zeroLatency := int64(0)
	justPastBestBucket := int64(501)
	justPastMiddleBucket := int64(1501)
	if got := latencyScore(&zeroLatency, 8); got != 8 {
		t.Fatalf("latencyScore(0) = %v, want 8", got)
	}
	if got := latencyScore(&justPastBestBucket, 8); got != 4.8 {
		t.Fatalf("latencyScore(501) = %v, want 4.8", got)
	}
	if got := latencyScore(&justPastMiddleBucket, 8); got != 2 {
		t.Fatalf("latencyScore(1501) = %v, want 2", got)
	}
}

func TestPriceScoreInterpolatesBetweenKnownExtremes(t *testing.T) {
	t.Parallel()

	minPrice := 2.0
	midpointPrice := 6.0
	maxPrice := 10.0

	if got := priceScore(Candidate{Pricing: CandidatePricing{PerRequestValue: &minPrice}}, minPrice, maxPrice); got != 10 {
		t.Fatalf("priceScore(minimum price) = %v, want 10", got)
	}
	if got := priceScore(Candidate{Pricing: CandidatePricing{PerRequestValue: &midpointPrice}}, minPrice, maxPrice); got != 5 {
		t.Fatalf("priceScore(midpoint price) = %v, want 5", got)
	}
	if got := priceScore(Candidate{Pricing: CandidatePricing{PerRequestValue: &maxPrice}}, minPrice, maxPrice); got != 0 {
		t.Fatalf("priceScore(maximum price) = %v, want 0", got)
	}
}

func TestCandidatePriceValueUsesZeroPerRequestPriceBeforeTokenPrices(t *testing.T) {
	t.Parallel()

	zeroPerRequest := 0.0
	input := 2.0
	output := 3.0
	got, ok := candidatePriceValue(Candidate{
		Pricing: CandidatePricing{
			InputValue:      &input,
			OutputValue:     &output,
			PerRequestValue: &zeroPerRequest,
		},
	})
	if !ok || got != 0 {
		t.Fatalf("candidatePriceValue = %v, %v; want 0 true", got, ok)
	}
}

func TestRouteCandidateSupportsEndpointEdgeCases(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		endpointType string
		supported    []string
		want         bool
	}{
		{name: "blank request endpoint matches all candidates", endpointType: " \t\n ", supported: nil, want: true},
		{name: "unknown supported endpoint is skipped before later text family match", endpointType: "openai", supported: []string{"unknown", " openai-response "}, want: true},
		{name: "unknown request endpoint cannot match text family", endpointType: "unknown", supported: []string{"openai"}, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := routeCandidateSupportsEndpoint(store.RouteCandidateRow{SupportedEndpointTypes: tc.supported}, tc.endpointType)
			if got != tc.want {
				t.Fatalf("routeCandidateSupportsEndpoint(%q, %#v) = %v, want %v", tc.endpointType, tc.supported, got, tc.want)
			}
		})
	}

	for _, endpointType := range []string{"openai-response", "anthropic-messages", "google-gemini"} {
		row := store.RouteCandidateRow{
			UpstreamModelName:      "mimo-v2.5-tts-voiceclone",
			SupportedEndpointTypes: []string{"openai", "anthropic-messages", "openai-audio-speech"},
		}
		if routeCandidateSupportsEndpoint(row, endpointType) {
			t.Fatalf("MiMo TTS must not match endpoint %q", endpointType)
		}
	}
	for _, endpointType := range []string{"openai", "openai-audio-speech"} {
		if !routeCandidateSupportsEndpoint(store.RouteCandidateRow{UpstreamModelName: "mimo-v2.5-tts", SupportedEndpointTypes: []string{"openai-audio-speech"}}, endpointType) {
			t.Fatalf("MiMo TTS must route through %q even with stale endpoint metadata", endpointType)
		}
	}
}

func TestEndpointTypeFamilyRequiresKnownNormalizedValues(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "text endpoints share family across providers", a: "openai-response", b: "google-gemini", want: true},
		{name: "image and embedding endpoints are separate families", a: "openai-image", b: "openai-embedding", want: false},
		{name: "unknown endpoint values are not a family", a: "unknown", b: "unknown", want: false},
		{name: "family comparison expects normalized endpoint values", a: "OpenAI", b: "openai", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := endpointTypeSameFamily(tc.a, tc.b); got != tc.want {
				t.Fatalf("endpointTypeSameFamily(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}

	if got := endpointTypeFamily(""); got != "" {
		t.Fatalf("endpointTypeFamily(empty) = %q, want empty", got)
	}
	if got := endpointTypeFamily(" OPENAI "); got != "" {
		t.Fatalf("endpointTypeFamily(untrimmed uppercase) = %q, want empty", got)
	}
}

func TestIndexCooldownsReturnsMapsAndKeepsLastSiteModelCooldown(t *testing.T) {
	t.Parallel()

	emptyIndex := indexCooldowns(nil)
	if emptyIndex.sites == nil || emptyIndex.models == nil || emptyIndex.transientModels == nil || emptyIndex.siteCredentialCounts == nil || emptyIndex.modelCredentialCounts == nil {
		t.Fatalf("indexCooldowns(nil) returned nil maps: %#v", emptyIndex)
	}
	if len(emptyIndex.sites) != 0 || len(emptyIndex.models) != 0 || len(emptyIndex.transientModels) != 0 {
		t.Fatalf("indexCooldowns(nil) lengths = site %d model %d transient %d, want zero", len(emptyIndex.sites), len(emptyIndex.models), len(emptyIndex.transientModels))
	}

	siteID := uuid.New()
	modelID := uuid.New()
	credentialID := uuid.New()
	firstSiteCooldownID := uuid.New()
	secondSiteCooldownID := uuid.New()
	firstModelCooldownID := uuid.New()
	ignoredCredentialCooldownID := uuid.New()
	secondModelCooldownID := uuid.New()

	index := indexCooldowns([]store.RouteCooldown{
		{ID: firstSiteCooldownID, SiteID: siteID},
		{ID: secondSiteCooldownID, SiteID: siteID},
		{
			ID:          firstModelCooldownID,
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
		},
		{
			ID:               ignoredCredentialCooldownID,
			SiteID:           siteID,
			SiteModelID:      uuid.NullUUID{UUID: modelID, Valid: true},
			SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		},
		{
			ID:          secondModelCooldownID,
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
		},
	})

	if got := index.sites[siteID].ID; got != secondSiteCooldownID {
		t.Fatalf("site cooldown ID = %s, want final site cooldown %s", got, secondSiteCooldownID)
	}
	if got := index.models[modelID].ID; got != secondModelCooldownID {
		t.Fatalf("model cooldown ID = %s, want final model cooldown %s", got, secondModelCooldownID)
	}
	if index.modelCredentialCounts[modelID] != 1 {
		t.Fatalf("model credential cooldown count = %d, want 1", index.modelCredentialCounts[modelID])
	}
	if len(index.sites) != 1 || len(index.models) != 1 {
		t.Fatalf("cooldown map lengths = site %d model %d, want 1 and 1", len(index.sites), len(index.models))
	}
}

func TestUUIDSetIncludesNilUUIDAndEmptySliceIsNil(t *testing.T) {
	t.Parallel()

	if got := uuidSet([]uuid.UUID{}); got != nil {
		t.Fatalf("uuidSet(empty) = %#v, want nil", got)
	}

	id := uuid.New()
	got := uuidSet([]uuid.UUID{uuid.Nil, id, uuid.Nil})
	if _, ok := got[uuid.Nil]; !ok {
		t.Fatal("uuidSet missing uuid.Nil")
	}
	if _, ok := got[id]; !ok {
		t.Fatalf("uuidSet missing %s", id)
	}
	if len(got) != 2 {
		t.Fatalf("uuidSet length = %d, want 2", len(got))
	}
}

func TestNullableRouteValuesPreserveValidZeroValues(t *testing.T) {
	t.Parallel()

	floatValue := nullableFloat(sql.NullFloat64{Float64: 0, Valid: true})
	if floatValue == nil || *floatValue != 0 {
		t.Fatalf("nullableFloat zero = %#v, want pointer to 0", floatValue)
	}
	if nullableFloat(sql.NullFloat64{Float64: 99, Valid: false}) != nil {
		t.Fatal("nullableFloat invalid non-zero should be nil")
	}

	intValue := nullableInt64(sql.NullInt64{Int64: 0, Valid: true})
	if intValue == nil || *intValue != 0 {
		t.Fatalf("nullableInt64 zero = %#v, want pointer to 0", intValue)
	}
	if nullableInt64(sql.NullInt64{Int64: 99, Valid: false}) != nil {
		t.Fatal("nullableInt64 invalid non-zero should be nil")
	}

	stringValue := nullableString(sql.NullString{String: "", Valid: true})
	if stringValue == nil || *stringValue != "" {
		t.Fatalf("nullableString empty valid = %#v, want pointer to empty string", stringValue)
	}
	if nullableString(sql.NullString{String: "ignored", Valid: false}) != nil {
		t.Fatal("nullableString invalid non-empty should be nil")
	}
}

func TestSiteGatewayConfigNormalizesLegacyResponsesOptions(t *testing.T) {
	t.Parallel()

	meta := store.JSON(`{"gateway":{"responses_image_generation_policy":" strip_auto_tool ","responses_tool_policy":" compatibility ","disabled_responses_tools":["image_generation","image_generation","unknown"]}}`)

	cfg := siteGatewayConfig(meta)
	if cfg == nil {
		t.Fatal("siteGatewayConfig returned nil")
	}
	if cfg.ResponsesToolPolicy != sitepkg.ResponsesToolPolicyCompatibility {
		t.Fatalf("responses tool policy = %q, want %q", cfg.ResponsesToolPolicy, sitepkg.ResponsesToolPolicyCompatibility)
	}
	if cfg.ResponsesImageGenerationPolicy != "" {
		t.Fatalf("legacy responses image policy = %q, want empty", cfg.ResponsesImageGenerationPolicy)
	}
	if len(cfg.DisabledResponsesTools) != 1 || cfg.DisabledResponsesTools[0] != sitepkg.ResponsesHostedToolImageGeneration {
		t.Fatalf("gateway disabled tools = %#v, want image_generation only", cfg.DisabledResponsesTools)
	}
	if got := siteResponsesToolPolicy(meta); got != sitepkg.ResponsesToolPolicyCompatibility {
		t.Fatalf("siteResponsesToolPolicy = %q, want %q", got, sitepkg.ResponsesToolPolicyCompatibility)
	}
	tools := siteDisabledResponsesTools(meta)
	if len(tools) != 1 || tools[0] != sitepkg.ResponsesHostedToolImageGeneration {
		t.Fatalf("siteDisabledResponsesTools = %#v, want image_generation only", tools)
	}
}

func TestSiteGatewayConfigIgnoresUnusableGatewayValues(t *testing.T) {
	t.Parallel()

	for _, meta := range []store.JSON{
		nil,
		store.JSON(`{}`),
		store.JSON(`{"gateway":null}`),
		store.JSON(`{"gateway":[]}`),
		store.JSON(`{"gateway":{"request_timeout_ms":0}}`),
	} {
		if cfg := siteGatewayConfig(meta); cfg != nil {
			t.Fatalf("siteGatewayConfig(%s) = %#v, want nil", meta, cfg)
		}
		if got := siteResponsesToolPolicy(meta); got != sitepkg.ResponsesToolPolicyPassthrough {
			t.Fatalf("siteResponsesToolPolicy(%s) = %q, want %q", meta, got, sitepkg.ResponsesToolPolicyPassthrough)
		}
		if tools := siteDisabledResponsesTools(meta); len(tools) != 0 {
			t.Fatalf("siteDisabledResponsesTools(%s) = %#v, want empty", meta, tools)
		}
	}
}
