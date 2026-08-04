package config

import (
	"reflect"
	"testing"
)

func TestDefaultPortalConfigRoundTrip(t *testing.T) {
	t.Parallel()

	cfg := DefaultPortalConfig()
	if cfg.Enabled {
		t.Fatal("portal should default to disabled")
	}
	if cfg.Dimensions.Site || cfg.Dimensions.Upstream {
		t.Fatalf("site and upstream should default to hidden: %#v", cfg.Dimensions)
	}
	if !cfg.Dimensions.Model || !cfg.Dimensions.Cost {
		t.Fatalf("model and cost should default to visible: %#v", cfg.Dimensions)
	}

	roundTrip := PortalConfigFromRaw(PortalConfigToMap(cfg))
	if !reflect.DeepEqual(roundTrip, cfg) {
		t.Fatalf("map round trip mismatch:\n got: %#v\nwant: %#v", roundTrip, cfg)
	}
}

func TestPortalConfigFromRawAppliesOverrides(t *testing.T) {
	t.Parallel()

	cfg := PortalConfigFromRaw(map[string]any{
		"enabled":      true,
		"summary_days": float64(30),
		"dimensions": map[string]any{
			"site":     true,
			"upstream": true,
		},
	})
	if !cfg.Enabled {
		t.Fatal("enabled override not applied")
	}
	if cfg.SummaryDays != 30 {
		t.Fatalf("summary_days override not applied: %d", cfg.SummaryDays)
	}
	if !cfg.Dimensions.Site || !cfg.Dimensions.Upstream {
		t.Fatalf("dimension overrides not applied: %#v", cfg.Dimensions)
	}
	if !cfg.Dimensions.Model {
		t.Fatal("unspecified dimension should keep its default")
	}
}

func TestNormalizePortalConfigFillsZeroValues(t *testing.T) {
	t.Parallel()

	cfg := NormalizePortalConfig(PortalConfig{})
	if cfg.SummaryDays != DefaultPortalConfig().SummaryDays {
		t.Fatalf("summary_days should fall back to default, got %d", cfg.SummaryDays)
	}
	if cfg.RequestPageSizeMax != DefaultPortalConfig().RequestPageSizeMax {
		t.Fatalf("request_page_size_max should fall back to default, got %d", cfg.RequestPageSizeMax)
	}
}

func TestValidatePortalConfigBounds(t *testing.T) {
	t.Parallel()

	valid := DefaultPortalConfig()
	if err := ValidatePortalConfig(valid); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}

	tooManyDays := DefaultPortalConfig()
	tooManyDays.SummaryDays = 365
	if err := ValidatePortalConfig(tooManyDays); err == nil {
		t.Fatal("summary_days=365 should be rejected")
	}

	badPageSize := DefaultPortalConfig()
	badPageSize.RequestPageSizeMax = 0
	if err := ValidatePortalConfig(badPageSize); err == nil {
		t.Fatal("request_page_size_max=0 should be rejected")
	}
}
