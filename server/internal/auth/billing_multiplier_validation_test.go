package auth

import (
	"math"
	"testing"
)

func TestValidateAPIKeyBillingMultiplierDefaultsNilToOne(t *testing.T) {
	t.Parallel()

	multiplier, err := validateAPIKeyBillingMultiplier(nil)
	if err != nil {
		t.Fatalf("validateAPIKeyBillingMultiplier(nil) error = %v, want nil", err)
	}
	if multiplier != 1 {
		t.Fatalf("validateAPIKeyBillingMultiplier(nil) = %v, want 1", multiplier)
	}
}

func TestValidateAPIKeyBillingMultiplierAcceptsPositiveValues(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{0.01, 1, 1.5, 100} {
		multiplier, err := validateAPIKeyBillingMultiplier(&value)
		if err != nil {
			t.Fatalf("validateAPIKeyBillingMultiplier(%v) error = %v, want nil", value, err)
		}
		if multiplier != value {
			t.Fatalf("validateAPIKeyBillingMultiplier(%v) = %v, want %v", value, multiplier, value)
		}
	}
}

func TestValidateAPIKeyBillingMultiplierRejectsNonPositiveValues(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{0, -1} {
		if _, err := validateAPIKeyBillingMultiplier(&value); err == nil {
			t.Fatalf("validateAPIKeyBillingMultiplier(%v) error = nil, want rejection", value)
		}
	}
}

func TestValidateAPIKeyBillingMultiplierRejectsNonFiniteValues(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := validateAPIKeyBillingMultiplier(&value); err == nil {
			t.Fatalf("validateAPIKeyBillingMultiplier(%v) error = nil, want rejection", value)
		}
	}
}

func TestValidateAPIKeyBillingMultiplierRejectsAboveCap(t *testing.T) {
	t.Parallel()

	value := 100.01
	if _, err := validateAPIKeyBillingMultiplier(&value); err == nil {
		t.Fatalf("validateAPIKeyBillingMultiplier(%v) error = nil, want rejection", value)
	}
}
