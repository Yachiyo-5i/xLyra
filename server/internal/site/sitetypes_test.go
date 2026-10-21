package site

import (
	"strings"
	"testing"
)

func TestCredentialHelpersMatchPreRegistryBehavior(t *testing.T) {
	t.Parallel()

	// Categories and capability flags are the pre-registry switch results.
	// grok's category is grok_sso, distinct from the public credential_type grok_oauth.
	cases := []struct {
		siteType string
		category string
		multi    bool
		cost     bool
	}{
		{siteType: "openai", category: "api_key", multi: true, cost: true},
		{siteType: "anthropic", category: "api_key", multi: true, cost: true},
		{siteType: "google_gemini", category: "api_key", multi: true, cost: true},
		{siteType: "grok", category: "grok_sso", multi: false, cost: false},
		{siteType: "opencode_go", category: "api_key", multi: true, cost: false},
		{siteType: "deepseek", category: "api_key", multi: true, cost: false},
		{siteType: "minimax", category: "api_key", multi: true, cost: false},
		{siteType: "xiaomi_mimo", category: "api_key", multi: true, cost: false},
		{siteType: "moonshot", category: "api_key", multi: true, cost: false},
		{siteType: "kimi_code", category: "api_key", multi: true, cost: false},
		{siteType: "zhipu", category: "api_key", multi: true, cost: false},
		{siteType: "glm_code", category: "api_key", multi: true, cost: false},
		{siteType: "typesafe", category: "api_key", multi: true, cost: false},
		{siteType: "newapi", category: "system_token", multi: false, cost: false},
		{siteType: "xlyra", category: "xlyra", multi: false, cost: false},
		{siteType: "codex", category: "oauth", multi: false, cost: false},
		{siteType: "antigravity", category: "oauth", multi: false, cost: false},
		{siteType: "claude_code", category: "oauth", multi: false, cost: false},
		{siteType: "not_a_site_type", category: "api_key", multi: true, cost: false},
	}

	for _, tc := range cases {
		for _, siteType := range siteTypeInputVariants(tc.siteType) {
			if got := CredentialTypeForSiteType(siteType); got != tc.category {
				t.Fatalf("CredentialTypeForSiteType(%q) = %q, want %q", siteType, got, tc.category)
			}
			if got := SupportsMultipleAPIKeys(siteType); got != tc.multi {
				t.Fatalf("SupportsMultipleAPIKeys(%q) = %v, want %v", siteType, got, tc.multi)
			}
			if got := SupportsAPIKeyCostMultiplier(siteType); got != tc.cost {
				t.Fatalf("SupportsAPIKeyCostMultiplier(%q) = %v, want %v", siteType, got, tc.cost)
			}
		}
	}

	// Blank input stays api_key for the category helpers. Cost multiplier still
	// follows normalizeSiteType, which maps a blank type to openai.
	for _, siteType := range []string{"", " ", "   ", "\t"} {
		if got := CredentialTypeForSiteType(siteType); got != "api_key" {
			t.Fatalf("CredentialTypeForSiteType(%q) = %q, want api_key", siteType, got)
		}
		if !SupportsMultipleAPIKeys(siteType) {
			t.Fatalf("SupportsMultipleAPIKeys(%q) = false, want true", siteType)
		}
		if !SupportsAPIKeyCostMultiplier(siteType) {
			t.Fatalf("SupportsAPIKeyCostMultiplier(%q) = false, want true", siteType)
		}
	}
}

func TestSiteTypeAccountCapabilities(t *testing.T) {
	t.Parallel()

	for _, desc := range SiteTypeDescriptors() {
		switch desc.SiteType {
		case "grok":
			if desc.Accounts == nil || desc.Accounts.LoginKind != accountLoginDeviceCode || desc.Accounts.SupportsImport || desc.Accounts.SupportsRegions {
				t.Fatalf("grok accounts = %#v", desc.Accounts)
			}
		default:
			if desc.Accounts != nil {
				t.Fatalf("%s accounts = %#v, want nil", desc.SiteType, desc.Accounts)
			}
		}
	}
}

func siteTypeInputVariants(siteType string) []string {
	return []string{
		siteType,
		strings.ToUpper(siteType),
		" " + siteType + " ",
		" " + strings.ToUpper(siteType) + " ",
	}
}
