package gateway

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func gatewayStoreWithSiteModelCapabilities(t *testing.T, siteModelID uuid.UUID, capabilities store.JSON) *store.Store {
	t.Helper()

	return gatewayStoreWithQueryCallback(t, func(tx *gorm.DB) {
		if dest, ok := tx.Statement.Dest.(*store.SiteModel); ok {
			*dest = store.SiteModel{
				ID:           siteModelID,
				Capabilities: capabilities,
			}
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(gorm.ErrRecordNotFound)
	})
}

func TestOpenAIResponsesAdapterBuildsResponsesPathAndPreservesNativePayload(t *testing.T) {
	t.Parallel()

	adapter := openAIResponsesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}
	if got := adapter.UpstreamPath(" https://api.example.test/ "); got != "https://api.example.test/v1/responses" {
		t.Fatalf("UpstreamPath = %q, want trimmed responses path", got)
	}

	payload, err := adapter.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model": "alias-model",
			"input": "hello",
		},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4"}})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "gpt-5.4" || payload["input"] != "hello" {
		t.Fatalf("payload = %#v, want upstream model while preserving input", payload)
	}
}

func TestUpstreamBillingMetadataUsesPayloadModelAndCandidateFallback(t *testing.T) {
	t.Parallel()

	withPayloadModel := applyUpstreamBillingMetadata(gatewayAttemptResult{}, map[string]any{
		"service_tier": " Priority ",
		"model":        "gpt-5.5-codex",
	}, routeengine.Candidate{})
	if withPayloadModel.serviceTier != "priority" || withPayloadModel.billingMode != "fast" ||
		withPayloadModel.costMultiplier != 2.5 || withPayloadModel.multiplierReason != "codex_fast_mode" {
		t.Fatalf("payload model billing metadata = %#v", withPayloadModel)
	}

	withCandidateModel := applyUpstreamBillingMetadata(gatewayAttemptResult{}, map[string]any{
		"service_tier": "fast",
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4"}})
	if withCandidateModel.costMultiplier != 2 || withCandidateModel.billingMode != "fast" {
		t.Fatalf("candidate model billing metadata = %#v", withCandidateModel)
	}

	defaultTier := applyUpstreamBillingMetadata(gatewayAttemptResult{}, map[string]any{
		"service_tier": "standard",
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.5"}})
	if defaultTier.serviceTier != "standard" || defaultTier.billingMode != "" || defaultTier.costMultiplier != 1 {
		t.Fatalf("standard tier billing metadata = %#v", defaultTier)
	}
}

func TestSupportedEndpointTypesFromCapabilitiesLoadsRawValues(t *testing.T) {
	t.Parallel()

	siteModelID := uuid.New()
	db := gatewayStoreWithSiteModelCapabilities(t, siteModelID, store.JSON(`{
		"supported_endpoint_types": [" openai-response ", "anthropic-messages", 7, ""]
	}`))

	values, err := (openAIProtocolResolver{db: db}).supportedEndpointTypesFromCapabilities(context.Background(), siteModelID)
	if err != nil {
		t.Fatalf("supportedEndpointTypesFromCapabilities returned error: %v", err)
	}
	if len(values) != 4 || values[0] != " openai-response " || values[1] != "anthropic-messages" || values[2] != "" || values[3] != "" {
		t.Fatalf("capability endpoint values = %#v", values)
	}
}

func TestOpenAIChatUpstreamResolverReturnsChatCompletionsAdapter(t *testing.T) {
	t.Parallel()

	adapter, err := (openAIChatUpstreamResolver{}).Resolve(context.Background(), gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4"}})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got := adapter.ProtocolName(); got != "openai_chat_completions" {
		t.Fatalf("ProtocolName = %q, want openai_chat_completions", got)
	}
}

func TestGatewayPricingUsesCandidateFallbackWhenRepositoryErrors(t *testing.T) {
	t.Parallel()

	db := gatewayGormWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrInvalidDB)
	})

	groupName := " candidate-group "
	currency := " EUR "
	inputValue := 1.25
	outputValue := 2.5
	perRequestValue := 0.75
	billingType := "fixed"
	quotaType := int64(1)
	got := (Handler{db: gatewayStoreWithGorm(t, db)}).gatewayPricing(context.Background(), routeengine.Candidate{
		Model: routeengine.CandidateModel{SiteModelID: uuid.New()},
		Pricing: routeengine.CandidatePricing{
			GroupName:       &groupName,
			Currency:        &currency,
			InputValue:      &inputValue,
			OutputValue:     &outputValue,
			PerRequestValue: &perRequestValue,
			BillingType:     &billingType,
			QuotaType:       &quotaType,
		},
	}, "ignored")

	if got.GroupName != "candidate-group" || got.Currency != "EUR" {
		t.Fatalf("fallback group/currency = %#v", got)
	}
	if got.InputValue == nil || *got.InputValue != inputValue ||
		got.OutputValue == nil || *got.OutputValue != outputValue ||
		got.PerRequestValue == nil || *got.PerRequestValue != perRequestValue {
		t.Fatalf("fallback numeric pricing = %#v", got)
	}
	if got.BillingType != billingType || got.QuotaType == nil || *got.QuotaType != quotaType {
		t.Fatalf("fallback billing fields = %#v", got)
	}
}

func TestSupportedEndpointTypesMergesAndNormalizesCapabilities(t *testing.T) {
	t.Parallel()

	siteModelID := uuid.New()
	db := gatewayStoreWithSiteModelCapabilities(t, siteModelID, store.JSON(`{
		"supported_endpoint_types": [
			" OpenAI-Response ",
			"openai-response",
			"ANTHROPIC-MESSAGES",
			"",
			7
		]
	}`))

	values, err := (openAIProtocolResolver{db: db}).supportedEndpointTypes(context.Background(), siteModelID)
	if err != nil {
		t.Fatalf("supportedEndpointTypes returned error: %v", err)
	}
	got := map[string]bool{}
	for _, value := range values {
		got[value] = true
	}
	if len(got) != 2 || !got[upstreamEndpointTypeOpenAIResponse] || !got[upstreamEndpointTypeAnthropicMessages] {
		t.Fatalf("normalized endpoint types = %#v", values)
	}
}
