package adapter

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestModelsDevProviderNormalizationAppliesAliasesAndFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		siteType string
		want     string
	}{
		{name: "zhipu alias trims and maps", siteType: " zhipu ", want: "zhipuai"},
		{name: "moonshot alias maps", siteType: "moonshot", want: "moonshotai-cn"},
		{name: "kimi code alias maps", siteType: " KIMI_CODE ", want: "moonshotai-cn"},
		{name: "xiaomi mimo alias maps", siteType: " xiaomi_mimo ", want: "xiaomi"},
		{name: "unknown lowercases", siteType: " Custom_Provider ", want: "custom_provider"},
		{name: "blank stays blank", siteType: " \t ", want: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeAdapterModelsDevProvider(tt.siteType); got != tt.want {
				t.Fatalf("normalizeAdapterModelsDevProvider(%q) = %q, want %q", tt.siteType, got, tt.want)
			}
		})
	}
}

func TestAdapterValueConversionHandlesNumericAndSliceShapes(t *testing.T) {
	t.Parallel()

	floatTests := []struct {
		name  string
		value any
		want  float64
		ok    bool
	}{
		{name: "float32", value: float32(1.25), want: 1.25, ok: true},
		{name: "int", value: int(3), want: 3, ok: true},
		{name: "int32", value: int32(4), want: 4, ok: true},
		{name: "int64", value: int64(5), want: 5, ok: true},
		{name: "unsupported bool", value: true, ok: false},
	}
	for _, tt := range floatTests {
		tt := tt
		t.Run("float64FromAny "+tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := float64FromAny(tt.value)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v, got value %f", ok, tt.ok, got)
			}
			if ok {
				assertFloat(t, got, tt.want)
			}
		})
	}

	intTests := []struct {
		name  string
		value any
		want  int64
		ok    bool
	}{
		{name: "int", value: int(7), want: 7, ok: true},
		{name: "int64", value: int64(8), want: 8, ok: true},
		{name: "invalid json number", value: json.Number("nope"), ok: false},
		{name: "unsupported int32", value: int32(9), ok: false},
	}
	for _, tt := range intTests {
		tt := tt
		t.Run("int64FromAny "+tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := int64FromAny(tt.value)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v, got value %d", ok, tt.ok, got)
			}
			if ok && got != tt.want {
				t.Fatalf("value = %d, want %d", got, tt.want)
			}
		})
	}

	if got := anySliceFromAny([]string{"a", "b"}); !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Fatalf("anySliceFromAny([]string) = %#v", got)
	}
	source := []any{"x", 1}
	if got := anySliceFromAny(source); !reflect.DeepEqual(got, source) {
		t.Fatalf("anySliceFromAny([]any) = %#v, want source", got)
	}
	if got := anySliceFromAny("not a slice"); len(got) != 0 {
		t.Fatalf("anySliceFromAny(unsupported) = %#v, want empty slice", got)
	}
}

func TestCodexUsagePayloadParsingHandlesAvailabilityResetAndMessages(t *testing.T) {
	t.Parallel()

	availableCases := []struct {
		name    string
		payload map[string]any
		want    bool
	}{
		{name: "allowed false", payload: map[string]any{"allowed": false}, want: false},
		{name: "limit reached", payload: map[string]any{"limit_reached": true}, want: false},
		{name: "five hour exhausted", payload: map[string]any{"five_hour": map[string]any{"remaining_percent": 0}}, want: false},
		{name: "weekly exhausted", payload: map[string]any{"weekly": map[string]any{"remaining_percent": int64(0)}}, want: false},
		{name: "positive windows", payload: map[string]any{
			"allowed":   true,
			"five_hour": map[string]any{"remaining_percent": float64(1)},
			"weekly":    map[string]any{"remaining_percent": 2},
		}, want: true},
	}
	for _, tt := range availableCases {
		tt := tt
		t.Run("codexUsageAvailable "+tt.name, func(t *testing.T) {
			t.Parallel()

			if got := codexUsageAvailable(tt.payload); got != tt.want {
				t.Fatalf("codexUsageAvailable(%#v) = %v, want %v", tt.payload, got, tt.want)
			}
		})
	}

	resetTime := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	resetCases := []struct {
		name  string
		value any
		want  any
	}{
		{name: "positive int", value: int(10), want: int64(10)},
		{name: "positive int64", value: int64(11), want: int64(11)},
		{name: "positive float64", value: float64(12.9), want: int64(12)},
		{name: "rfc3339 string", value: resetTime.Format(time.RFC3339), want: resetTime.Unix()},
		{name: "plain string preserved", value: " later ", want: "later"},
		{name: "zero ignored", value: 0, want: nil},
		{name: "blank string ignored", value: " \t ", want: nil},
	}
	for _, tt := range resetCases {
		tt := tt
		t.Run("normalizeResetAtValue "+tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeResetAtValue(tt.value); got != tt.want {
				t.Fatalf("normalizeResetAtValue(%#v) = %#v, want %#v", tt.value, got, tt.want)
			}
		})
	}

	messagePayload := map[string]any{
		"outer": []any{
			map[string]any{"ignored": "value"},
			map[string]any{"nested": map[string]any{"status_message": " quota warming up "}},
		},
	}
	if got := codexUsageMessage(messagePayload); got != "quota warming up" {
		t.Fatalf("codexUsageMessage nested status = %q, want quota warming up", got)
	}
	if got := codexUsageMessage(map[string]any{"error": " upstream failed "}); got != "upstream failed" {
		t.Fatalf("codexUsageMessage error = %q, want upstream failed", got)
	}
	if got := codexUsageMessage([]any{"ignored", map[string]any{"detail": " retry later "}}); got != "retry later" {
		t.Fatalf("codexUsageMessage slice detail = %q, want retry later", got)
	}
	if got := codexUsageMessage(map[string]any{"nested": []any{map[string]any{"noop": true}}}); got != "" {
		t.Fatalf("codexUsageMessage empty nested = %q, want empty", got)
	}
}

type fullCapabilityTestModule struct{}

func (fullCapabilityTestModule) SiteTypes() []string {
	return []string{"capability-all"}
}

func (fullCapabilityTestModule) Detect(_ context.Context, _ string) (DetectResult, error) {
	return DetectResult{}, nil
}

func (fullCapabilityTestModule) ValidateCredentials(_ context.Context, _ SiteConfig, _ string) error {
	return nil
}

func (fullCapabilityTestModule) ValidateSystemCredentials(_ context.Context, _ SiteConfig, _ SystemAuth) error {
	return nil
}

func (fullCapabilityTestModule) ListModels(_ context.Context, _ SiteConfig, _ string) ([]Model, error) {
	return nil, nil
}

func (fullCapabilityTestModule) ListModelsWithAuth(_ context.Context, _ SiteConfig, _ SystemAuth) ([]Model, error) {
	return nil, nil
}

func (fullCapabilityTestModule) ListAPIKeys(_ context.Context, _ SiteConfig, _ SystemAuth) ([]APIKey, error) {
	return nil, nil
}

func (fullCapabilityTestModule) SummarizeAPIKey(_ context.Context, _ SiteConfig, _ string) (APIKeySummary, error) {
	return APIKeySummary{}, nil
}

func (fullCapabilityTestModule) FetchUserSummary(_ context.Context, _ SiteConfig, _ SystemAuth) (UserSummary, error) {
	return UserSummary{}, nil
}

func (fullCapabilityTestModule) FetchBalance(_ context.Context, _ SiteConfig, _ SystemAuth) (BalanceSnapshot, error) {
	return BalanceSnapshot{}, nil
}

func (fullCapabilityTestModule) FetchPricing(_ context.Context, _ SiteConfig, _ SystemAuth) (PricingSnapshot, error) {
	return PricingSnapshot{}, nil
}

func (fullCapabilityTestModule) ExecuteCheckin(_ context.Context, _ SiteConfig, _ SystemAuth) (CheckinResult, error) {
	return CheckinResult{}, nil
}

func (fullCapabilityTestModule) FetchMetadata(_ context.Context, _ SiteConfig, _ SystemAuth) (MetadataSnapshot, error) {
	return MetadataSnapshot{}, nil
}

type systemCredentialCapabilityTestModule struct{}

func (systemCredentialCapabilityTestModule) SiteTypes() []string {
	return []string{"capability-system-only"}
}

func (systemCredentialCapabilityTestModule) ValidateSystemCredentials(_ context.Context, _ SiteConfig, _ SystemAuth) error {
	return nil
}

func (systemCredentialCapabilityTestModule) ListModelsWithAuth(_ context.Context, _ SiteConfig, _ SystemAuth) ([]Model, error) {
	return nil, nil
}

func TestModuleCapabilitiesInfersImplementedAdapterInterfaces(t *testing.T) {
	t.Parallel()

	got := ModuleCapabilities(fullCapabilityTestModule{})
	want := []Capability{
		CapabilityDetect,
		CapabilityValidateCredential,
		CapabilityListModels,
		CapabilityListAPIKeys,
		CapabilitySummarizeAPIKey,
		CapabilityFetchUserSummary,
		CapabilityFetchBalance,
		CapabilityFetchPricing,
		CapabilityCheckin,
		CapabilityFetchMetadata,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ModuleCapabilities(all) = %#v, want %#v", got, want)
	}

	systemOnly := ModuleCapabilities(systemCredentialCapabilityTestModule{})
	wantSystemOnly := []Capability{CapabilityValidateCredential, CapabilityListModels}
	if !reflect.DeepEqual(systemOnly, wantSystemOnly) {
		t.Fatalf("ModuleCapabilities(system only) = %#v, want %#v", systemOnly, wantSystemOnly)
	}
}

func TestZhipuModelListingValidatesKeysAndSeparatesCodingModels(t *testing.T) {
	t.Parallel()

	if _, err := NewZhipu().ListModels(context.Background(), SiteConfig{SiteType: zhipuSiteType}, " \t "); err == nil {
		t.Fatal("NewZhipu ListModels with blank key returned nil error")
	}
	if _, err := NewGLMCode().ListModels(context.Background(), SiteConfig{SiteType: glmCodeSiteType}, ""); err == nil {
		t.Fatal("NewGLMCode ListModels with blank key returned nil error")
	}

	general, err := NewZhipu().ListModels(context.Background(), SiteConfig{SiteType: zhipuSiteType}, "sk-test")
	if err != nil {
		t.Fatalf("NewZhipu ListModels returned error: %v", err)
	}
	coding, err := NewGLMCode().ListModels(context.Background(), SiteConfig{SiteType: glmCodeSiteType}, "sk-test")
	if err != nil {
		t.Fatalf("NewGLMCode ListModels returned error: %v", err)
	}
	if len(general) <= len(coding) {
		t.Fatalf("general zhipu model count = %d, coding count = %d, want general larger", len(general), len(coding))
	}
	if adapterModelExists(general, "embedding-3") == false || adapterModelExists(general, "glm-image") == false {
		t.Fatalf("general zhipu models missing embedding/image entries: %#v", general)
	}
	if adapterModelExists(coding, "embedding-3") || adapterModelExists(coding, "glm-image") {
		t.Fatalf("coding zhipu models should omit embedding/image entries: %#v", coding)
	}
}

func adapterModelExists(models []Model, name string) bool {
	for _, model := range models {
		if model.UpstreamName == name {
			return true
		}
	}
	return false
}
