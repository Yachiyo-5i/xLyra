package site

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestMergePricingModelsStateReturnsExistingModelsWhenPricingSnapshotIsEmpty(t *testing.T) {
	t.Parallel()

	existing := []store.SiteModel{{
		ID:           uuid.New(),
		UpstreamName: "gpt-existing",
		Status:       "active",
	}}

	got, err := (&Service{}).mergePricingModelsState(t.Context(), store.Site{}, adapter.PricingSnapshot{}, existing, store.SiteModelRepository{})
	if err != nil {
		t.Fatalf("mergePricingModelsState() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != existing[0].ID {
		t.Fatalf("mergePricingModelsState() = %#v, want existing slice when pricing snapshot is empty", got)
	}
}

func TestSyncSiteModelsFromAdapterModelsSkipsEmptyAdapterModels(t *testing.T) {
	t.Parallel()

	got, err := (&Service{}).syncSiteModelsFromAdapterModels(t.Context(), store.Site{}, nil, store.SiteModelRepository{}, true)
	if err != nil {
		t.Fatalf("syncSiteModelsFromAdapterModels() error = %v", err)
	}
	if got != nil {
		t.Fatalf("syncSiteModelsFromAdapterModels() = %#v, want nil for empty adapter models", got)
	}
}

func TestNormalizeModelPriceInputMapsFixedPricingToPerRequestOnly(t *testing.T) {
	t.Parallel()

	inputValue := 1.25
	outputValue := 2.5
	cacheValue := 0.5
	perRequestValue := 0.03

	got, err := normalizeModelPriceInput(ModelPriceInput{
		GroupName:       " vip ",
		BillingType:     " FIXED ",
		Currency:        " eur ",
		InputValue:      &inputValue,
		OutputValue:     &outputValue,
		CacheInputValue: &cacheValue,
		PerRequestValue: &perRequestValue,
	})
	if err != nil {
		t.Fatalf("normalizeModelPriceInput() error = %v", err)
	}
	if got.BillingType != "per_request" || got.Currency != "EUR" || got.GroupName != "vip" {
		t.Fatalf("normalized identity = %#v, want fixed mapped to per_request with trimmed fields", got)
	}
	if got.PerRequestValue == nil || *got.PerRequestValue != perRequestValue {
		t.Fatalf("per_request value = %#v, want %v", got.PerRequestValue, perRequestValue)
	}
	if got.InputValue != nil || got.OutputValue != nil || got.CacheInputValue != nil {
		t.Fatalf("token fields = %#v/%#v/%#v, want cleared for per_request pricing", got.InputValue, got.OutputValue, got.CacheInputValue)
	}
}

func TestNormalizeModelPriceInputRequiresTokenValueForTokenPricing(t *testing.T) {
	t.Parallel()

	_, err := normalizeModelPriceInput(ModelPriceInput{
		GroupName:   " ",
		BillingType: " tokens ",
		Currency:    " ",
	})
	if err == nil || !strings.Contains(err.Error(), "tokens pricing requires input_value or output_value") {
		t.Fatalf("normalizeModelPriceInput() error = %v, want missing token value validation", err)
	}
}

func TestIsSiteAPIKeyCredentialTypeMatchesOnlyAPIKeyCredentialPrefix(t *testing.T) {
	t.Parallel()

	if !isSiteAPIKeyCredentialType(defaultCredentialType) {
		t.Fatalf("%q should be treated as an api key credential type", defaultCredentialType)
	}
	if !isSiteAPIKeyCredentialType(defaultCredentialType + ":upstream-123") {
		t.Fatal("typed api key credential with upstream suffix should be treated as api key")
	}
	if isSiteAPIKeyCredentialType("api_key_backup") {
		t.Fatal("api_key_backup should not match the strict api key type prefix")
	}
	if isSiteAPIKeyCredentialType(newAPIAccessTokenCredential) {
		t.Fatal("newapi access token credential should not be treated as a site api key")
	}
}

func TestSyncManualNewAPISiteAPIKeyModelsRequiresCredentialIDsBeforeStoreUse(t *testing.T) {
	t.Parallel()

	err := syncManualNewAPISiteAPIKeyModels(t.Context(), nil, uuid.New(), store.SiteModel{ID: uuid.New()}, []uuid.UUID{uuid.Nil, uuid.Nil}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "site_credential_ids is required for newapi sites") {
		t.Fatalf("syncManualNewAPISiteAPIKeyModels() error = %v, want credential id validation", err)
	}
}
