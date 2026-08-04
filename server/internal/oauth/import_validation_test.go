package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestImportAccountsRejectsInvalidSub2APIAccountsBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	result := NewImportService(nil, "master-key", nil).ImportAccounts(context.Background(), Sub2APIExport{
		Accounts: []Sub2APIAccount{
			{
				Name:     "unsupported@example.com",
				Platform: "unknown",
			},
			{
				Name:     "missing-token@example.com",
				Platform: "openai",
			},
			{
				Name:     "missing-meta@example.com",
				Platform: "openai",
				Credentials: Sub2APICredentials{
					AccessToken: "access",
				},
			},
			{
				Name:     "missing-account@example.com",
				Platform: "openai",
				Credentials: Sub2APICredentials{
					AccessToken: "access",
					IDToken:     importValidationIDToken(t, `{"email":"missing-account@example.com"}`),
				},
			},
		},
	}, false)

	if result.Meta.Total != 4 || result.Meta.Failed != 4 || result.Meta.Accepted != 0 || result.Meta.Queued != 0 || result.Meta.Succeeded != 0 {
		t.Fatalf("unexpected import meta: %#v", result.Meta)
	}
	assertImportItemFailure(t, result.Items, 0, "", "unsupported platform")
	assertImportItemFailure(t, result.Items, 1, codexProvider, "access_token is required")
	assertImportItemFailure(t, result.Items, 2, codexProvider, "missing chatgpt_account_id")
	assertImportItemFailure(t, result.Items, 3, codexProvider, "chatgpt_account_id is required")
}

func TestImportChatGPTTokenAccountsRejectsInvalidTokensBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		export    ChatGPTTokenExport
		wantError string
	}{
		{
			name: "missing_access_token",
			export: ChatGPTTokenExport{
				Tokens: ChatGPTTokenDetails{AccountID: "acct_123"},
			},
			wantError: "access_token is required",
		},
		{
			name: "missing_account_id",
			export: ChatGPTTokenExport{
				Tokens: ChatGPTTokenDetails{AccessToken: "access"},
			},
			wantError: "account_id is required",
		},
		{
			name: "missing_id_token_fallback_metadata",
			export: ChatGPTTokenExport{
				Tokens: ChatGPTTokenDetails{AccessToken: "access", AccountID: "acct_123"},
			},
			wantError: "missing email, plan_type, user_id, expires_at",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := NewImportService(nil, "master-key", nil).ImportChatGPTTokenAccounts(context.Background(), tc.export, false)

			assertSingleImportFailure(t, result, codexProvider, tc.wantError)
		})
	}
}

func TestImportCPAAccountsRejectsInvalidFlatAccountsBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		account      CPAExport
		wantProvider string
		wantError    string
	}{
		{
			name: "google_alias_missing_access_token",
			account: CPAExport{
				Type:  "google",
				Email: "agent@example.com",
			},
			wantProvider: antigravityProvider,
			wantError:    "access_token is required",
		},
		{
			name: "codex_missing_account_id",
			account: CPAExport{
				Type:        "codex",
				Email:       "agent@example.com",
				AccessToken: "access",
			},
			wantProvider: codexProvider,
			wantError:    "account_id is required",
		},
		{
			name: "codex_missing_id_token_fallback_metadata",
			account: CPAExport{
				Type:        "openai",
				AccountID:   "acct_123",
				Email:       "agent@example.com",
				AccessToken: "access",
				Expired:     "2026-06-22T00:00:00Z",
			},
			wantProvider: codexProvider,
			wantError:    "missing plan_type, user_id",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := NewImportService(nil, "master-key", nil).ImportCPAAccounts(context.Background(), tc.account, false)

			assertSingleImportFailure(t, result, tc.wantProvider, tc.wantError)
		})
	}
}

func TestMissingFlatTokenFallbackMetadataReportsEveryMissingFieldInOrder(t *testing.T) {
	t.Parallel()

	missing := missingFlatTokenFallbackMetadata(" ", " ", " ", " ", time.Time{})
	assertOAuthMissingFields(t, missing, "email", "account_id", "plan_type", "user_id", "expires_at")
}

func TestImportSiteProxyMetaTrimsProxyID(t *testing.T) {
	t.Parallel()

	if got := importSiteProxyMeta(" "); got != nil {
		t.Fatalf("empty proxy meta = %s, want nil", string(got))
	}

	got := importSiteProxyMeta(" proxy-main ")
	var meta map[string]any
	if err := json.Unmarshal(got, &meta); err != nil {
		t.Fatalf("proxy meta should be JSON: %v", err)
	}
	if meta["proxy_id"] != "proxy-main" {
		t.Fatalf("proxy_id = %#v, want proxy-main", meta["proxy_id"])
	}
}

func assertSingleImportFailure(t *testing.T, result ImportResult, wantProvider string, wantError string) {
	t.Helper()

	if result.Meta.Total != 1 || result.Meta.Failed != 1 || result.Meta.Accepted != 0 || result.Meta.Queued != 0 || result.Meta.Succeeded != 0 {
		t.Fatalf("unexpected import meta: %#v", result.Meta)
	}
	assertImportItemFailure(t, result.Items, 0, wantProvider, wantError)
}

func assertImportItemFailure(t *testing.T, items []ImportAccountResult, index int, wantProvider string, wantError string) {
	t.Helper()

	if len(items) <= index {
		t.Fatalf("items length = %d, want item at index %d: %#v", len(items), index, items)
	}
	item := items[index]
	if item.Status != "failed" {
		t.Fatalf("item[%d] status = %q, want failed: %#v", index, item.Status, item)
	}
	if item.Provider != wantProvider {
		t.Fatalf("item[%d] provider = %q, want %q: %#v", index, item.Provider, wantProvider, item)
	}
	if !strings.Contains(item.Error, wantError) {
		t.Fatalf("item[%d] error = %q, want to contain %q", index, item.Error, wantError)
	}
}

func importValidationIDToken(t *testing.T, payload string) string {
	t.Helper()

	if !json.Valid([]byte(payload)) {
		t.Fatalf("invalid test JWT payload: %s", payload)
	}
	return "header." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
}
