package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestCodexConnectionDetailsKeepsFutureAccessTokenOnlyConnection(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	encryptedAccess, _, err := service.credentials.Encrypt("access-token")
	if err != nil {
		t.Fatalf("encrypt access token: %v", err)
	}
	futureConnection := store.OAuthConnection{
		ID:                   uuid.New(),
		Provider:             codexProvider,
		AccountID:            "acct-123",
		Email:                "user@example.com",
		EncryptedAccessToken: encryptedAccess,
		ExpiresAt:            sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true},
		Metadata:             store.JSON(`{"plan_type":"plus"}`),
	}

	details, err := service.codexConnectionDetails(futureConnection)
	if err != nil {
		t.Fatalf("codexConnectionDetails future access-token-only: %v", err)
	}
	if details.AccessToken != "access-token" || details.AccountID != "acct-123" || details.Metadata["plan_type"] != "plus" {
		t.Fatalf("unexpected connection details: %#v", details)
	}

	expiredConnection := futureConnection
	expiredConnection.ExpiresAt = sql.NullTime{Time: time.Now().Add(-time.Hour), Valid: true}
	if strings.TrimSpace(expiredConnection.EncryptedRefreshToken) != "" {
		t.Fatal("test fixture should be access-token-only")
	}
}

func TestImportFallbackHelpersValidateMissingMetadataOffline(t *testing.T) {
	t.Parallel()

	missing := missingFlatTokenFallbackMetadata(" ", "acct-123", "plus", "user-123", time.Now())
	if len(missing) != 1 || missing[0] != "email" {
		t.Fatalf("missing flat metadata = %#v, want only email", missing)
	}

	account := Sub2APIAccount{
		Name:     "  fallback@example.com ",
		Platform: " openai ",
		Credentials: Sub2APICredentials{
			AccessToken: "access-token",
		},
	}
	result := NewImportService(nil, "master-key", nil).ImportAccounts(context.Background(), Sub2APIExport{Accounts: []Sub2APIAccount{account}}, false)
	assertSingleFailedImport(t, result, "id_token is missing and required metadata is incomplete: missing chatgpt_account_id")
}

func TestImportSiteProxyMetaAndTokenHelpersTrimBranches(t *testing.T) {
	t.Parallel()

	var meta map[string]any
	if err := json.Unmarshal(importSiteProxyMeta(" proxy-main "), &meta); err != nil {
		t.Fatalf("decode proxy meta: %v", err)
	}
	if meta["proxy_id"] != "proxy-main" {
		t.Fatalf("proxy meta = %#v, want trimmed proxy id", meta)
	}
	if got := importTokenMode("\trefresh-token\n"); got != "oauth_refresh" {
		t.Fatalf("refresh token mode = %q, want oauth_refresh", got)
	}
	if got := importRefreshWarning(" refresh-token "); got != "" {
		t.Fatalf("refresh warning for refreshable token = %q, want empty", got)
	}
	if got := importRefreshWarning(" "); got != importAccessTokenOnlyWarning {
		t.Fatalf("access-only warning = %q, want configured warning", got)
	}
}
