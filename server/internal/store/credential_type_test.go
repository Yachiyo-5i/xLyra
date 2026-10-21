package store

import "testing"

func TestCredentialTypeClassification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		value      string
		apiKey     bool
		account    bool
		modelBound bool
		category   string
	}{
		{name: "empty", value: "", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "bare api key", value: "api_key", apiKey: true, account: false, modelBound: true, category: "api_key"},
		{name: "scoped api key", value: "api_key:x", apiKey: true, account: false, modelBound: true, category: "api_key"},
		{name: "api key prefix without colon", value: "api_key2", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "leading space", value: " api_key", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "different case", value: "API_KEY", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "grok account", value: "grok_sso:abc", apiKey: false, account: true, modelBound: true, category: "account"},
		{name: "oauth", value: "oauth", apiKey: false, account: false, modelBound: false, category: "oauth"},
		{name: "system token", value: "system_token", apiKey: false, account: false, modelBound: false, category: "system_token"},
		{name: "xlyra", value: "xlyra", apiKey: false, account: false, modelBound: false, category: "xlyra"},
		{name: "codex", value: "codex", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "future account prefix is not recognized yet", value: "account:x", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "grok without colon", value: "grok_sso", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "oauth with suffix", value: "oauth:1", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "trailing space", value: "api_key ", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "empty api key suffix", value: "api_key:", apiKey: true, account: false, modelBound: true, category: "api_key"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsAPIKeyCredentialType(tc.value); got != tc.apiKey {
				t.Fatalf("IsAPIKeyCredentialType(%q) = %v, want %v", tc.value, got, tc.apiKey)
			}
			if got := IsAccountCredentialType(tc.value); got != tc.account {
				t.Fatalf("IsAccountCredentialType(%q) = %v, want %v", tc.value, got, tc.account)
			}
			if got := IsModelBoundCredentialType(tc.value); got != tc.modelBound {
				t.Fatalf("IsModelBoundCredentialType(%q) = %v, want %v", tc.value, got, tc.modelBound)
			}
			if got := CredentialCategory(tc.value); got != tc.category {
				t.Fatalf("CredentialCategory(%q) = %q, want %q", tc.value, got, tc.category)
			}
		})
	}
}
