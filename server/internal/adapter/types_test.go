package adapter

import (
	"context"
	"reflect"
	"testing"
)

type capabilityOnlyModule struct {
	capabilities []Capability
}

func (m capabilityOnlyModule) SiteTypes() []string {
	return []string{" custom ", "", "custom-alt"}
}

func (m capabilityOnlyModule) Capabilities() []Capability {
	return m.capabilities
}

type inferredCapabilityModule struct{}

func (inferredCapabilityModule) SiteTypes() []string {
	return []string{"inferred"}
}

func (inferredCapabilityModule) Detect(_ context.Context, _ string) (DetectResult, error) {
	return DetectResult{}, nil
}

func (inferredCapabilityModule) ProbeHealth(_ context.Context, _ SiteConfig, _ string) ([]Model, error) {
	return nil, nil
}

func (inferredCapabilityModule) Scope() HealthProbeScope {
	return HealthProbeSiteScope
}

func (inferredCapabilityModule) ValidateCredentials(_ context.Context, _ SiteConfig, _ string) error {
	return nil
}

func (inferredCapabilityModule) ListModels(_ context.Context, _ SiteConfig, _ string) ([]Model, error) {
	return nil, nil
}

func (inferredCapabilityModule) ParsePricing(_ any) PricingSnapshot {
	return PricingSnapshot{}
}

func TestRegistryRegistersDefaultModulesBySiteType(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()

	cases := map[string]struct {
		wantType         string
		wantCapabilities []Capability
	}{
		"codex": {
			wantType:         "adapter.Codex",
			wantCapabilities: []Capability{CapabilityValidateCredential, CapabilityListModels, CapabilityFetchPricing, CapabilityFetchMetadata},
		},
		"antigravity": {
			wantType:         "adapter.Antigravity",
			wantCapabilities: []Capability{CapabilityValidateCredential, CapabilityListModels, CapabilityListAPIKeys, CapabilityFetchPricing},
		},
		"claude_code": {
			wantType:         "adapter.ClaudeCode",
			wantCapabilities: []Capability{CapabilityValidateCredential, CapabilityListModels, CapabilityListAPIKeys, CapabilitySummarizeAPIKey, CapabilityFetchUserSummary, CapabilityFetchBalance, CapabilityFetchMetadata},
		},
		"anthropic": {
			wantType:         "adapter.Anthropic",
			wantCapabilities: []Capability{CapabilityValidateCredential, CapabilityListModels},
		},
		"zhipu": {
			wantType:         "adapter.Zhipu",
			wantCapabilities: []Capability{CapabilityValidateCredential, CapabilityListModels, CapabilityFetchPricing},
		},
		"glm_code": {
			wantType:         "adapter.Zhipu",
			wantCapabilities: []Capability{CapabilityValidateCredential, CapabilityListModels, CapabilityFetchPricing},
		},
		"openai": {
			wantType:         "adapter.OpenAICompatible",
			wantCapabilities: []Capability{CapabilityValidateCredential, CapabilityListModels, CapabilityFetchPricing},
		},
		"newapi": {
			wantType:         "adapter.NewAPI",
			wantCapabilities: []Capability{CapabilityDetect, CapabilityValidateCredential, CapabilityListModels, CapabilityCheckin},
		},
		"xlyra": {
			wantType:         "adapter.XLyra",
			wantCapabilities: []Capability{CapabilityValidateCredential, CapabilityListModels, CapabilityListAPIKeys},
		},
		"google_gemini": {
			wantType:         "adapter.Google",
			wantCapabilities: []Capability{CapabilityValidateCredential, CapabilityListModels},
		},
		"grok": {
			wantType:         "adapter.Grok",
			wantCapabilities: []Capability{CapabilityHealthProbe, CapabilityValidateCredential, CapabilityListModels, CapabilitySummarizeAPIKey},
		},
		"opencode_go": {
			wantType:         "adapter.OpenCodeGo",
			wantCapabilities: []Capability{CapabilityHealthProbe, CapabilityListModels, CapabilityFetchPricing},
		},
	}

	for siteType, tt := range cases {
		siteType := siteType
		tt := tt
		t.Run(siteType, func(t *testing.T) {
			t.Parallel()

			module, ok := registry.ModuleForSiteType(siteType)
			if !ok {
				t.Fatalf("default registry missing %q", siteType)
			}
			if got := typeName(module); got != tt.wantType {
				t.Fatalf("module type = %s, want %s", got, tt.wantType)
			}
			capabilities := ModuleCapabilities(module)
			for _, capability := range tt.wantCapabilities {
				if !hasCapability(capabilities, capability) {
					t.Fatalf("%s capabilities missing %q: %#v", siteType, capability, capabilities)
				}
			}
		})
	}

	if _, ok := registry.ModuleForSiteType("missing"); ok {
		t.Fatal("unexpected module for missing site type")
	}

	modules := registry.Modules()
	if len(modules) != 12 {
		t.Fatalf("default module count = %d, want 12", len(modules))
	}
	modules[0] = nil
	fresh := registry.Modules()
	if fresh[0] == nil {
		t.Fatal("Modules should return a copy")
	}
}

func TestRegistryRegisterNormalizesAndSkipsEmptySiteTypes(t *testing.T) {
	t.Parallel()

	registry := Registry{modulesByType: map[string]Module{}}
	module := capabilityOnlyModule{capabilities: []Capability{CapabilityListModels}}
	registry.Register(module)

	if got, ok := registry.ModuleForSiteType("custom"); !ok || got == nil {
		t.Fatalf("expected trimmed site type lookup, got %T %v", got, ok)
	}
	if got, ok := registry.ModuleForSiteType(" custom "); !ok || got == nil {
		t.Fatalf("expected lookup to trim incoming site type, got %T %v", got, ok)
	}
	if got, ok := registry.ModuleForSiteType("custom-alt"); !ok || got == nil {
		t.Fatalf("expected secondary site type lookup, got %T %v", got, ok)
	}
	if len(registry.modulesByType) != 2 {
		t.Fatalf("registered site type count = %d, want 2", len(registry.modulesByType))
	}
}

func TestModuleCapabilitiesInfersInterfacesWhenDescriptorMissing(t *testing.T) {
	t.Parallel()

	got := ModuleCapabilities(inferredCapabilityModule{})
	want := []Capability{
		CapabilityDetect,
		CapabilityHealthProbe,
		CapabilityValidateCredential,
		CapabilityListModels,
	}
	if len(got) != len(want) {
		t.Fatalf("capabilities = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("capabilities = %#v, want %#v", got, want)
		}
	}
	if _, ok := AsPricingParser(inferredCapabilityModule{}); !ok {
		t.Fatal("expected inferred module to implement PricingParser")
	}
}

func TestModuleCapabilitiesUsesDescriptorAndDeduplicates(t *testing.T) {
	t.Parallel()

	got := ModuleCapabilities(capabilityOnlyModule{
		capabilities: []Capability{
			CapabilityListModels,
			"",
			CapabilityListModels,
			CapabilityFetchPricing,
		},
	})
	want := []Capability{CapabilityListModels, CapabilityFetchPricing}
	if len(got) != len(want) {
		t.Fatalf("capabilities = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("capabilities = %#v, want %#v", got, want)
		}
	}
}

func TestDefaultBaseURLProviderAdapter(t *testing.T) {
	t.Parallel()

	provider, ok := AsDefaultBaseURLProvider(NewGoogle())
	if !ok {
		t.Fatal("expected Google module to expose default base URL provider")
	}
	if got := provider.DefaultBaseURL(); got != googleDefaultBaseURL {
		t.Fatalf("DefaultBaseURL = %q, want %q", got, googleDefaultBaseURL)
	}

	if _, ok := AsDefaultBaseURLProvider(capabilityOnlyModule{}); ok {
		t.Fatal("capabilityOnlyModule should not expose default base URL provider")
	}
}

func typeName(value any) string {
	return reflect.TypeOf(value).String()
}

func hasCapability(values []Capability, want Capability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
