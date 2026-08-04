package adapter

import (
	"encoding/json"
	"math"
	"testing"
)

func TestNewAPIParsePricingNormalizesRatioToUSDPerMillionTokens(t *testing.T) {
	t.Parallel()

	snapshot := NewNewAPI().ParsePricing(map[string]any{
		"quota_per_unit": float64(500000),
		"group_ratio": map[string]any{
			"default": float64(1),
			"vip":     float64(0.5),
		},
		"data": []any{
			map[string]any{
				"model_name":       "gpt-test",
				"model_ratio":      float64(0.875),
				"completion_ratio": float64(8),
				"enable_group":     []any{"default", "vip"},
			},
		},
	})

	if len(snapshot.Items) != 2 {
		t.Fatalf("expected two pricing rows, got %d", len(snapshot.Items))
	}

	defaultRow := pricingByGroup(t, snapshot.Items, "default")
	assertFloat(t, defaultRow.InputValue, 1.75)
	assertFloat(t, defaultRow.OutputValue, 14)
	if !defaultRow.HasInputValue || !defaultRow.HasOutputValue {
		t.Fatalf("expected normalized input/output values, got %#v", defaultRow)
	}

	vipRow := pricingByGroup(t, snapshot.Items, "vip")
	assertFloat(t, vipRow.InputValue, 0.875)
	assertFloat(t, vipRow.OutputValue, 7)
}

func TestNewAPIParsePricingKeepsFixedPriceRows(t *testing.T) {
	t.Parallel()

	snapshot := NewNewAPI().ParsePricing(map[string]any{
		"group_ratio": map[string]any{"default": float64(2)},
		"data": []any{
			map[string]any{
				"model_name":  "image-test",
				"quota_type":  float64(1),
				"model_price": float64(0.04),
			},
		},
	})

	row := pricingByGroup(t, snapshot.Items, "default")
	assertFloat(t, row.PerRequestValue, 0.08)
	if !row.HasPerRequestValue {
		t.Fatalf("expected normalized per-request value, got %#v", row)
	}
	if row.HasInputValue || row.HasOutputValue {
		t.Fatalf("expected per-request pricing to keep token values empty, got %#v", row)
	}
	if row.BillingType != "per_request" {
		t.Fatalf("expected per_request billing type, got %q", row.BillingType)
	}
}

func TestNewAPIParsePricingPayloadReadsDataMapVendorAndGroupRatios(t *testing.T) {
	t.Parallel()

	groups, items := parseNewAPIPricingPayload(map[string]any{
		"quota_per_unit": "250000",
		"group_ratio": map[string]any{
			"default": "1.5",
			"vip":     json.Number("0.25"),
		},
		"usable_group": map[string]any{
			"default": "Default Group",
			"vip":     "VIP Group",
		},
		"auto_groups": []any{"vip"},
		"vendors": []any{
			map[string]any{
				"id":          json.Number("7"),
				"name":        "Vendor Seven",
				"description": "Vendor fallback description",
				"icon":        "vendor-seven.svg",
			},
		},
		"data": map[string]any{
			"gpt-map": map[string]any{
				"display_name":           "GPT Map",
				"model_ratio":            "0.5",
				"completion_ratio":       json.Number("2"),
				"cache_ratio":            "0.125",
				"create_cache_ratio":     json.Number("0.25"),
				"image_ratio":            "3",
				"audio_ratio":            json.Number("4"),
				"audio_completion_ratio": "5",
				"vendor_id":              json.Number("7"),
				"enable_group":           []any{"default", "vip"},
				"owner_by":               "owner",
			},
		},
	})

	if len(groups) != 2 {
		t.Fatalf("expected two groups, got %d: %#v", len(groups), groups)
	}

	defaultGroup := pricingGroupByName(t, groups, "default")
	if defaultGroup.DisplayName != "Default Group" {
		t.Fatalf("expected default display name from usable_group, got %q", defaultGroup.DisplayName)
	}
	assertFloat(t, defaultGroup.Ratio, 1.5)
	if defaultGroup.IsAuto {
		t.Fatalf("expected default group to be manual, got %#v", defaultGroup)
	}

	vipGroup := pricingGroupByName(t, groups, "vip")
	if vipGroup.DisplayName != "VIP Group" {
		t.Fatalf("expected vip display name from usable_group, got %q", vipGroup.DisplayName)
	}
	assertFloat(t, vipGroup.Ratio, 0.25)
	if !vipGroup.IsAuto {
		t.Fatalf("expected vip group to be auto, got %#v", vipGroup)
	}

	if len(items) != 2 {
		t.Fatalf("expected two model pricing rows, got %d: %#v", len(items), items)
	}

	defaultRow := pricingByGroup(t, items, "default")
	if defaultRow.ModelName != "gpt-map" {
		t.Fatalf("expected model name from data map key, got %q", defaultRow.ModelName)
	}
	if defaultRow.DisplayName != "GPT Map" {
		t.Fatalf("expected display name from row, got %q", defaultRow.DisplayName)
	}
	assertFloat(t, defaultRow.GroupRatio, 1.5)
	assertFloat(t, defaultRow.ModelRatio, 0.5)
	assertFloat(t, defaultRow.CompletionRatio, 2)
	assertFloat(t, defaultRow.CacheRatio, 0.125)
	assertFloat(t, defaultRow.CreateCacheRatio, 0.25)
	assertFloat(t, defaultRow.ImageRatio, 3)
	assertFloat(t, defaultRow.AudioRatio, 4)
	assertFloat(t, defaultRow.AudioCompletionRatio, 5)
	if !defaultRow.HasModelRatio || !defaultRow.HasCompletionRatio || !defaultRow.HasCacheRatio ||
		!defaultRow.HasCreateCacheRatio || !defaultRow.HasImageRatio || !defaultRow.HasAudioRatio ||
		!defaultRow.HasAudioCompletionRatio {
		t.Fatalf("expected numeric string and json.Number ratios to be present, got %#v", defaultRow)
	}
	assertFloat(t, defaultRow.InputValue, 3)
	assertFloat(t, defaultRow.OutputValue, 6)
	if !defaultRow.HasInputValue || !defaultRow.HasOutputValue {
		t.Fatalf("expected default row to have normalized token values, got %#v", defaultRow)
	}
	if !defaultRow.HasVendorID || defaultRow.VendorID != 7 {
		t.Fatalf("expected vendor id from json.Number, got %#v", defaultRow)
	}
	if defaultRow.VendorName != "Vendor Seven" || defaultRow.VendorIcon != "vendor-seven.svg" ||
		defaultRow.Description != "Vendor fallback description" {
		t.Fatalf("expected vendor metadata from vendors list, got %#v", defaultRow)
	}
	if defaultRow.OwnerBy != "owner" {
		t.Fatalf("expected owner to be preserved, got %q", defaultRow.OwnerBy)
	}

	vipRow := pricingByGroup(t, items, "vip")
	assertFloat(t, vipRow.GroupRatio, 0.25)
	assertFloat(t, vipRow.InputValue, 0.5)
	assertFloat(t, vipRow.OutputValue, 1)
}

func TestNewAPIParsePricingMapDataExpandsGroupsAndVendorMetadata(t *testing.T) {
	t.Parallel()

	snapshot := NewNewAPI().ParsePricing(map[string]any{
		"quota_per_unit": json.Number("250000"),
		"group_ratio": map[string]any{
			"default": json.Number("1.5"),
			"vip":     "2",
		},
		"usable_group": map[string]any{
			"vip": "VIP Users",
		},
		"auto_groups": []any{"auto-only", "vip"},
		"vendors": []any{
			map[string]any{
				"id":          json.Number("7"),
				"name":        "Vendor Seven",
				"description": "vendor description",
				"icon":        "vendor-icon",
			},
		},
		"data": map[string]any{
			"gpt-map": map[string]any{
				"name":               "GPT Map",
				"enable_group":       "vip, missing-ratio",
				"model_ratio":        "0.5",
				"completion_ratio":   json.Number("3"),
				"cache_ratio":        0.25,
				"vendor_id":          json.Number("7"),
				"owner_by":           "owner-team",
				"audio_ratio":        "1.25",
				"create_cache_ratio": "0.75",
			},
		},
	})

	if len(snapshot.Groups) != 3 {
		t.Fatalf("groups length = %d, want 3: %#v", len(snapshot.Groups), snapshot.Groups)
	}
	assertPricingGroup(t, snapshot.Groups, "default", "default", 1.5, false)
	assertPricingGroup(t, snapshot.Groups, "vip", "VIP Users", 2, true)
	assertPricingGroup(t, snapshot.Groups, "auto-only", "auto-only", 1, true)

	if len(snapshot.Items) != 2 {
		t.Fatalf("pricing items length = %d, want 2: %#v", len(snapshot.Items), snapshot.Items)
	}
	vip := pricingByGroup(t, snapshot.Items, "vip")
	if vip.ModelName != "gpt-map" || vip.DisplayName != "GPT Map" || vip.GroupRatio != 2 {
		t.Fatalf("unexpected vip pricing row identity: %#v", vip)
	}
	if !vip.HasInputValue || vip.InputValue != 4 || !vip.HasOutputValue || vip.OutputValue != 12 {
		t.Fatalf("unexpected vip token prices: %#v", vip)
	}
	if !vip.HasVendorID || vip.VendorID != 7 || vip.VendorName != "Vendor Seven" || vip.VendorIcon != "vendor-icon" {
		t.Fatalf("unexpected vendor metadata: %#v", vip)
	}
	if vip.Description != "vendor description" || vip.OwnerBy != "owner-team" {
		t.Fatalf("unexpected descriptive metadata: %#v", vip)
	}
	if !vip.HasCacheRatio || vip.CacheRatio != 0.25 || !vip.HasCreateCacheRatio || vip.CreateCacheRatio != 0.75 {
		t.Fatalf("unexpected cache ratios: %#v", vip)
	}
	if !vip.HasAudioRatio || vip.AudioRatio != 1.25 {
		t.Fatalf("unexpected audio ratio: %#v", vip)
	}

	missingRatio := pricingByGroup(t, snapshot.Items, "missing-ratio")
	if missingRatio.GroupRatio != 1 || missingRatio.InputValue != 2 || missingRatio.OutputValue != 6 {
		t.Fatalf("unexpected fallback group prices: %#v", missingRatio)
	}
}

func TestNewAPIParsePricingPerRequestAndDefaultGroupBranches(t *testing.T) {
	t.Parallel()

	snapshot := NewNewAPI().ParsePricing(map[string]any{
		"data": []any{
			map[string]any{
				"model":       "per-request-model",
				"quota_type":  json.Number("1"),
				"model_price": "0.031",
			},
			map[string]any{
				"model_name":  "missing-price-model",
				"quota_type":  1,
				"model_ratio": "9",
			},
		},
	})

	if len(snapshot.Groups) != 1 {
		t.Fatalf("groups length = %d, want default group: %#v", len(snapshot.Groups), snapshot.Groups)
	}
	if snapshot.Groups[0].GroupName != "default" || snapshot.Groups[0].Ratio != 1 {
		t.Fatalf("unexpected default group: %#v", snapshot.Groups[0])
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("pricing items length = %d, want 2: %#v", len(snapshot.Items), snapshot.Items)
	}

	perRequest := pricingByModel(t, snapshot.Items, "per-request-model")
	if perRequest.BillingType != "per_request" || perRequest.QuotaType != 1 {
		t.Fatalf("unexpected per-request billing fields: %#v", perRequest)
	}
	if !perRequest.HasPerRequestValue || perRequest.PerRequestValue != 0.031 {
		t.Fatalf("unexpected per-request price: %#v", perRequest)
	}
	if perRequest.HasInputValue || perRequest.HasOutputValue {
		t.Fatalf("per-request pricing should not expose token prices: %#v", perRequest)
	}

	// quota_type=1 (per-request) but only a token model_ratio was provided. Rather
	// than persist model_ratio with no value column (which bills zero, F12), the
	// derivation falls back to token pricing so the model still bills.
	missingPrice := pricingByModel(t, snapshot.Items, "missing-price-model")
	if missingPrice.HasPerRequestValue {
		t.Fatalf("no model_price should not yield a per-request value: %#v", missingPrice)
	}
	if !missingPrice.HasInputValue || missingPrice.InputValue != 18 || !missingPrice.HasOutputValue || missingPrice.OutputValue != 18 {
		t.Fatalf("model_ratio-only row should fall back to token pricing (9*1*2), got: %#v", missingPrice)
	}
}

// A token quota_type row that carries only a per-request model_price (malformed
// upstream data) must still derive a billable value instead of persisting
// model_price with no value column, which would bill zero (F12).
func TestNewAPIParsePricingTokenQuotaWithOnlyModelPriceFallsBackToPerRequest(t *testing.T) {
	t.Parallel()

	snapshot := NewNewAPI().ParsePricing(map[string]any{
		"group_ratio": map[string]any{"default": float64(2)},
		"data": []any{
			map[string]any{
				"model_name":  "token-quota-fixed-price",
				"quota_type":  float64(0),
				"model_price": float64(0.05),
			},
		},
	})

	row := pricingByModel(t, snapshot.Items, "token-quota-fixed-price")
	if !row.HasPerRequestValue || row.PerRequestValue != 0.1 {
		t.Fatalf("expected per-request fallback (0.05*2), got %#v", row)
	}
	if row.HasInputValue || row.HasOutputValue {
		t.Fatalf("model_price-only row should not derive token values: %#v", row)
	}
}

func TestNewAPI_float64FromAnyParsesStringAndJSONNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  any
		want   float64
		wantOK bool
	}{
		{name: "string decimal", value: " 0.125 ", want: 0.125, wantOK: true},
		{name: "json number decimal", value: json.Number("3.75"), want: 3.75, wantOK: true},
		{name: "empty string", value: "", wantOK: false},
		{name: "invalid string", value: "not-a-number", wantOK: false},
		{name: "invalid json number", value: json.Number("not-a-number"), wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := float64FromAny(tt.value)
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got ok=%v value=%f", tt.wantOK, ok, got)
			}
			if ok {
				assertFloat(t, got, tt.want)
			}
		})
	}
}

func TestNewAPI_int64FromAnyHandlesJSONNumberAndRejectsStringNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  any
		want   int64
		wantOK bool
	}{
		{name: "json number integer", value: json.Number("42"), want: 42, wantOK: true},
		{name: "float64 truncates", value: float64(42.9), want: 42, wantOK: true},
		{name: "string number rejected", value: "42", wantOK: false},
		{name: "spaced string number rejected", value: " 42 ", wantOK: false},
		{name: "decimal json number rejected", value: json.Number("42.5"), wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := int64FromAny(tt.value)
			if ok != tt.wantOK {
				t.Fatalf("expected ok=%v, got ok=%v value=%d", tt.wantOK, ok, got)
			}
			if ok && got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func pricingGroupByName(t *testing.T, groups []PricingGroup, group string) PricingGroup {
	t.Helper()
	for _, item := range groups {
		if item.GroupName == group {
			return item
		}
	}
	t.Fatalf("pricing group %q not found in %#v", group, groups)
	return PricingGroup{}
}

func pricingByGroup(t *testing.T, rows []ModelPricing, group string) ModelPricing {
	t.Helper()
	for _, row := range rows {
		if row.GroupName == group {
			return row
		}
	}
	t.Fatalf("pricing row for group %q not found in %#v", group, rows)
	return ModelPricing{}
}

func pricingByModel(t *testing.T, rows []ModelPricing, model string) ModelPricing {
	t.Helper()
	for _, row := range rows {
		if row.ModelName == model {
			return row
		}
	}
	t.Fatalf("pricing row for model %q not found in %#v", model, rows)
	return ModelPricing{}
}

func assertPricingGroup(t *testing.T, groups []PricingGroup, name string, display string, ratio float64, isAuto bool) {
	t.Helper()

	for _, group := range groups {
		if group.GroupName != name {
			continue
		}
		if group.DisplayName != display || group.Ratio != ratio || group.IsAuto != isAuto {
			t.Fatalf("group %q = %#v, want display=%q ratio=%v auto=%v", name, group, display, ratio, isAuto)
		}
		return
	}
	t.Fatalf("group %q not found in %#v", name, groups)
}

func assertFloat(t *testing.T, got float64, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("expected %f, got %f", want, got)
	}
}
