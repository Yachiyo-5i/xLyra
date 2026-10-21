package store

import (
	"reflect"
	"testing"
)

func TestResolveSiteModelEndpointPolicyMatchesCatalogSystemOneAlias(t *testing.T) {
	policy := ResolveSiteModelEndpointPolicy(
		CanonicalModel{SupportedEndpointTypes: JSON(`["systemone"]`)},
		SiteModel{Capabilities: JSON(`{"supported_endpoint_types":["typesafe-systemone"]}`)},
		SiteModelEndpointOverride{},
	)

	if !reflect.DeepEqual(policy.CatalogEndpointTypes, []string{"typesafe-systemone"}) {
		t.Fatalf("catalog endpoint types = %#v", policy.CatalogEndpointTypes)
	}
	if !reflect.DeepEqual(policy.SupportedEndpointTypes, []string{"typesafe-systemone"}) {
		t.Fatalf("supported endpoint types = %#v", policy.SupportedEndpointTypes)
	}
}

func TestNormalizeModelEndpointTypesDeduplicatesSystemOneAliases(t *testing.T) {
	got := NormalizeModelEndpointTypes([]string{"systemone", "typesafe-systemone", "SYSTEMONE"})
	if !reflect.DeepEqual(got, []string{"typesafe-systemone"}) {
		t.Fatalf("normalized endpoint types = %#v", got)
	}
}

func TestResolveSiteModelEndpointPolicyForOpenCodeUsesModelMappingAndHonorsOverrides(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		model    string
		siteType string
		baseURL  string
		override SiteModelEndpointOverride
		want     []string
	}{
		{name: "exact OpenAI mapping", model: "minimax-m3.1-flash-free", siteType: "opencode_go", want: []string{"openai"}},
		{name: "JEV URL mapping", model: "jev-1.13-free", siteType: "openai", baseURL: "https://opencode.ai/zen", want: []string{"typesafe-systemone"}},
		{name: "disabled override", model: "jev-1.13-free", siteType: "opencode_go", override: SiteModelEndpointOverride{Mode: "disabled"}, want: []string{}},
		{name: "allowlist remains authoritative", model: "minimax-m3.1-flash-free", siteType: "opencode_go", override: SiteModelEndpointOverride{Mode: "allowlist", EndpointTypes: JSON(`["anthropic-messages"]`)}, want: []string{}},
		{name: "non OpenCode site", model: "jev-1.13-free", siteType: "openai", baseURL: "https://api.example.test", want: []string{"anthropic-messages"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			canonical := CanonicalModel{SupportedEndpointTypes: JSON(`["anthropic-messages"]`)}
			model := SiteModel{
				UpstreamName: tc.model,
				Capabilities: JSON(`{"supported_endpoint_types":["anthropic-messages"]}`),
			}
			policy, err := ResolveSiteModelEndpointPolicyForSite(canonical, model, tc.override, tc.siteType, tc.baseURL)
			if err != nil {
				t.Fatalf("ResolveSiteModelEndpointPolicyForSite: %v", err)
			}
			if !reflect.DeepEqual(policy.SupportedEndpointTypes, tc.want) {
				t.Fatalf("SupportedEndpointTypes = %#v, want %#v", policy.SupportedEndpointTypes, tc.want)
			}
		})
	}
}
