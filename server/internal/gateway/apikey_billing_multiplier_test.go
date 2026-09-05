package gateway

import (
	"context"
	"testing"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

func TestGatewayBillingMultiplierCombinesCredentialServiceAndAPIKeyMultipliers(t *testing.T) {
	t.Parallel()

	got := gatewayBillingMultiplier(gatewayAttemptResult{
		credentialCostMultiplier: 1.25,
		billingMode:              "fast",
		costMultiplier:           2,
		apiKeyBillingMultiplier:  3,
	})
	if got != 7.5 {
		t.Fatalf("gatewayBillingMultiplier = %v, want 7.5", got)
	}
}

func TestGatewayBillingMultiplierDefaultsMissingMultipliersToOne(t *testing.T) {
	t.Parallel()

	got := gatewayBillingMultiplier(gatewayAttemptResult{})
	if got != 1 {
		t.Fatalf("gatewayBillingMultiplier = %v, want 1", got)
	}
}

func TestAttemptMetadataRecordsAPIKeyBillingMultiplier(t *testing.T) {
	t.Parallel()

	estimatedCost := 2.0
	baseEstimatedCost := 1.0
	metadata := attemptMetadata(
		context.Background(),
		"request-id",
		"parent-request-id",
		uuid.Nil,
		uuid.Nil,
		routeengine.Candidate{},
		gatewayAttemptResult{
			estimatedCost:           &estimatedCost,
			baseEstimatedCost:       &baseEstimatedCost,
			apiKeyBillingMultiplier: 2.5,
		},
	)
	calculation, ok := metadata["cost_calculation"].(map[string]any)
	if !ok {
		t.Fatalf("cost_calculation metadata = %T, want map[string]any", metadata["cost_calculation"])
	}
	if got := calculation["api_key_billing_multiplier"]; got != 2.5 {
		t.Fatalf("api_key_billing_multiplier = %v, want 2.5", got)
	}
}
