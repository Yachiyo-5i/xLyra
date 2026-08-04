package store

import (
	"database/sql"
	"math"
	"testing"

	"github.com/google/uuid"
)

func TestOpenAIKeyPricingUpgradeUsesSharedGroupRatio(t *testing.T) {
	t.Parallel()

	groups := openAIKeyPricingGroups([]SiteModelPricing{
		{ID: uuid.New(), ModelName: "gpt-a", GroupName: "default", GroupRatio: 1.5, Available: true},
		{ID: uuid.New(), ModelName: "gpt-b", GroupName: "vip", GroupRatio: 1.5, Available: true},
	})
	multiplier, ok := sharedOpenAIKeyPricingMultiplier(groups)
	if !ok || multiplier != 1.5 {
		t.Fatalf("shared multiplier = %v, %v; want 1.5, true", multiplier, ok)
	}
	if len(groups) != 2 || groups[1].target.ID != groups[1].source.ID {
		t.Fatalf("pricing groups = %#v, want one target per model", groups)
	}
}

func TestOpenAIKeyPricingUpgradeFoldsDifferentRatiosIntoBaseCost(t *testing.T) {
	t.Parallel()

	defaultID := uuid.New()
	vipID := uuid.New()
	groups := openAIKeyPricingGroups([]SiteModelPricing{
		{ID: defaultID, ModelName: "gpt-a", GroupName: "default", GroupRatio: 1.2, Available: false},
		{ID: vipID, ModelName: "gpt-a", GroupName: "vip", GroupRatio: 1.2, Available: true, InputValue: sql.NullFloat64{Float64: 2, Valid: true}},
		{ID: uuid.New(), ModelName: "gpt-b", GroupName: "default", GroupRatio: 1.4, Available: true},
	})
	if _, ok := sharedOpenAIKeyPricingMultiplier(groups); ok {
		t.Fatal("different group ratios must not be migrated to one key multiplier")
	}
	if len(groups) != 2 || groups[0].target.ID != defaultID || groups[0].source.ID != vipID {
		t.Fatalf("first pricing group = %#v, want default target and available vip source", groups[0])
	}
	normalized := groups[0].source
	scaleOpenAIKeyPricing(&normalized, normalized.GroupRatio)
	if !normalized.InputValue.Valid || math.Abs(normalized.InputValue.Float64-2.4) > 1e-9 {
		t.Fatalf("scaled input value = %#v, want 2.4", normalized.InputValue)
	}
}
