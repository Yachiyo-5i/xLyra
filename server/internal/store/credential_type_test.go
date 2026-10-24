package store

import (
	"testing"

	"github.com/google/uuid"
)

const accountCredentialUUID = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

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
		{name: "unparseable account prefix", value: "account:x", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "plugin account", value: "account:acme.example/chat:" + accountCredentialUUID, apiKey: false, account: true, modelBound: true, category: "account"},
		{name: "account prefix alone", value: "account:", apiKey: false, account: false, modelBound: false, category: ""},
		{name: "account site type without slash", value: "account:openai:" + accountCredentialUUID, apiKey: false, account: false, modelBound: false, category: ""},
		{name: "account site type containing colon", value: "account:a/b:c:" + accountCredentialUUID, apiKey: false, account: false, modelBound: false, category: ""},
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

func TestAccountCredentialTypeRoundTrip(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("6BA7B810-9DAD-11D1-80B4-00C04FD430C8")
	got := NewAccountCredentialType("  ACME.Example/Chat ", id)
	want := "account:acme.example/chat:" + accountCredentialUUID
	if got != want {
		t.Fatalf("NewAccountCredentialType = %q, want %q", got, want)
	}
	siteType, parsed, ok := ParseAccountCredentialType(got)
	if !ok || siteType != "acme.example/chat" || parsed != id || parsed.String() != accountCredentialUUID {
		t.Fatalf("ParseAccountCredentialType(%q) = %q, %s, %v", got, siteType, parsed, ok)
	}

	rawUpper := "account:acme.example/chat:6BA7B810-9DAD-11D1-80B4-00C04FD430C8"
	siteType, parsed, ok = ParseAccountCredentialType(rawUpper)
	if !ok || siteType != "acme.example/chat" || parsed.String() != accountCredentialUUID {
		t.Fatalf("uppercase uuid Parse = %q, %s, %v", siteType, parsed, ok)
	}

	dotted := NewAccountCredentialType("a.b/c_d", uuid.MustParse(accountCredentialUUID))
	siteType, _, ok = ParseAccountCredentialType(dotted)
	if !ok || siteType != "a.b/c_d" {
		t.Fatalf("dotted site type Parse = %q, %v", siteType, ok)
	}
}

func TestParseAccountCredentialTypeRejects(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"account:",
		"account:acme.example/chat",
		"account:acme.example/chat:",
		"account::" + accountCredentialUUID,
		"account:openai:" + accountCredentialUUID,
		"account:a/b:c:" + accountCredentialUUID,
		"account:acme.example/chat:6ba7b8109dad11d180b400c04fd430c8",
		"account:acme.example/chat:{6ba7b810-9dad-11d1-80b4-00c04fd430c8}",
		"account:acme.example/chat:urn:uuid:" + accountCredentialUUID,
		"account:acme.example/chat:not-a-uuid",
		"Account:acme.example/chat:" + accountCredentialUUID,
		"grok_sso:" + accountCredentialUUID,
		"api_key",
		"api_key:" + accountCredentialUUID,
	}
	for _, value := range cases {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			siteType, id, ok := ParseAccountCredentialType(value)
			if ok || siteType != "" || id != uuid.Nil {
				t.Fatalf("ParseAccountCredentialType(%q) = %q, %s, %v, want reject", value, siteType, id, ok)
			}
		})
	}
}

func TestIsAccountCredentialTypeOf(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse(accountCredentialUUID)
	value := NewAccountCredentialType("acme.example/chat", id)
	if !IsAccountCredentialTypeOf(value, "acme.example/chat") {
		t.Fatal("matching site type should be true")
	}
	if !IsAccountCredentialTypeOf(value, "  ACME.EXAMPLE/Chat ") {
		t.Fatal("site type comparison should ignore case and surrounding space")
	}
	if IsAccountCredentialTypeOf(value, "other.example/chat") {
		t.Fatal("different site type should be false")
	}
	if IsAccountCredentialTypeOf("account:openai:"+accountCredentialUUID, "openai") {
		t.Fatal("unparseable account credential should be false")
	}
	if IsAccountCredentialTypeOf("grok_sso:"+accountCredentialUUID, "grok") {
		t.Fatal("grok credential should not match account site type")
	}
	mixed := "account:ACME.EXAMPLE/Chat:" + accountCredentialUUID
	if !IsAccountCredentialTypeOf(mixed, "acme.example/chat") {
		t.Fatal("parsed site type should compare case-insensitively")
	}
}
