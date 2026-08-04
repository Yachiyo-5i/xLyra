package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestUpdateMetadataErrorPreservesArbitraryExistingMetadata(t *testing.T) {
	t.Parallel()

	encoded := updateMetadataError(store.JSON(`{"plan_type":"plus","count":2}`), "refresh failed")

	var meta map[string]any
	if err := json.Unmarshal(encoded, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta["plan_type"] != "plus" || meta["count"] != float64(2) {
		t.Fatalf("existing metadata was not preserved: %#v", meta)
	}
	if meta["last_error"] != "refresh failed" {
		t.Fatalf("last_error = %#v, want refresh failed", meta["last_error"])
	}
	if _, ok := meta["last_error_at"].(string); !ok {
		t.Fatalf("last_error_at = %#v, want timestamp string", meta["last_error_at"])
	}
}

func TestNormalizeCodexModelSnapshotsFillsDisplayAndEnabledDefaults(t *testing.T) {
	t.Parallel()

	siteModelID := uuid.NewString()
	models := normalizeCodexModelSnapshots([]map[string]any{
		{
			"id":      siteModelID,
			"name":    "gpt-5-mini",
			"display": "GPT 5 Mini",
			"status":  "inactive",
		},
		{
			"model": "o4-mini",
		},
	})

	if len(models) != 2 {
		t.Fatalf("models = %#v, want two normalized models", models)
	}
	first := models[0]
	if first["site_model_id"] != siteModelID || first["id"] != "gpt-5-mini" {
		t.Fatalf("first normalized model = %#v, want uuid moved to site_model_id and id from name", first)
	}
	if first["upstream_model_name"] != "gpt-5-mini" || first["display_name"] != "GPT 5 Mini" || first["enabled"] != false {
		t.Fatalf("first defaults = %#v", first)
	}
	second := models[1]
	if second["id"] != "o4-mini" || second["upstream_model_name"] != "o4-mini" || second["enabled"] != true {
		t.Fatalf("second defaults = %#v", second)
	}
}

func TestNormalizeCodexModelSnapshotsAcceptsOfficialSlugItems(t *testing.T) {
	t.Parallel()

	models := normalizeCodexModelSnapshots([]map[string]any{
		{
			"slug":         "gpt-5.6-sol",
			"display_name": "GPT-5.6-Sol",
			"visibility":   "list",
			"priority":     1,
		},
	})
	if len(models) != 1 {
		t.Fatalf("models = %#v, want one normalized model", models)
	}
	model := models[0]
	if model["id"] != "gpt-5.6-sol" || model["upstream_model_name"] != "gpt-5.6-sol" {
		t.Fatalf("official slug item not normalized: %#v", model)
	}
}

func TestOAuthScalarHelpersParseAuthErrorsUUIDsAndStrings(t *testing.T) {
	t.Parallel()

	if !isPermanentAuthError("upstream returned 401 unauthorized") {
		t.Fatal("401 errors should be permanent auth errors")
	}
	if !isPermanentAuthError("refresh_token_reused") {
		t.Fatal("refresh_token_reused should be a permanent auth error")
	}
	if isPermanentAuthError("temporary network timeout") {
		t.Fatal("temporary network timeout should not be permanent")
	}
	if !looksLikeUUID(uuid.NewString()) {
		t.Fatal("valid UUID should be recognized")
	}
	if looksLikeUUID("not-a-uuid") {
		t.Fatal("invalid UUID should be rejected")
	}
	if got := firstNonEmptyString(" ", "\t", " value "); got != "value" {
		t.Fatalf("firstNonEmptyString = %q, want value", got)
	}
	if got := stringFromAny(strings.Builder{}); got != "" {
		t.Fatalf("stringFromAny(non-string) = %q, want empty", got)
	}
}

func TestConnectionMutationNilIDsReturnWithoutStoreAccess(t *testing.T) {
	t.Parallel()

	service := &Service{}
	if err := service.UpdateConnectionSync(t.Context(), uuid.Nil, map[string]any{"models": []any{}}); err != nil {
		t.Fatalf("UpdateConnectionSync nil id error = %v, want nil", err)
	}
	if err := service.MarkConnectionAccessTokenOnly(t.Context(), uuid.Nil); err != nil {
		t.Fatalf("MarkConnectionAccessTokenOnly nil id error = %v, want nil", err)
	}
	if err := service.MarkConnectionUnavailable(t.Context(), uuid.Nil, "expired"); err != nil {
		t.Fatalf("MarkConnectionUnavailable nil id error = %v, want nil", err)
	}
	if err := service.MarkConnectionUnavailableBySiteID(t.Context(), uuid.Nil, "expired"); err != nil {
		t.Fatalf("MarkConnectionUnavailableBySiteID nil site id error = %v, want nil", err)
	}
}

func TestHTTPClientForConnectionNilGuardsReturnConfiguredClient(t *testing.T) {
	t.Parallel()

	configuredClient := &http.Client{}
	service := &Service{httpClient: configuredClient}
	siteID := uuid.New()

	for _, tc := range []struct {
		name       string
		connection store.OAuthConnection
	}{
		{name: "nil site id", connection: store.OAuthConnection{}},
		{name: "zero site id", connection: store.OAuthConnection{SiteID: &uuid.Nil}},
		{name: "nil store", connection: store.OAuthConnection{SiteID: &siteID}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, err := service.httpClientForConnection(context.Background(), tc.connection)
			if err != nil {
				t.Fatalf("httpClientForConnection error = %v, want nil", err)
			}
			if client != configuredClient {
				t.Fatalf("httpClientForConnection client = %p, want configured client %p", client, configuredClient)
			}
		})
	}
}

func TestRefreshAntigravityConnectionUsesAccessTokenOnlyMetadataWithoutStoreAccess(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "master-key")
	encryptedAccess, _, err := service.credentials.Encrypt("access-token")
	if err != nil {
		t.Fatalf("encrypt access token: %v", err)
	}

	futureConnection := store.OAuthConnection{
		ID:                   uuid.New(),
		Provider:             antigravityProvider,
		AccountID:            "acct-123",
		Email:                "user@example.com",
		EncryptedAccessToken: encryptedAccess,
		ExpiresAt:            sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true},
	}
	details, err := service.refreshAntigravityConnection(context.Background(), store.OAuthConnectionRepository{}, futureConnection)
	if err != nil {
		t.Fatalf("refreshAntigravityConnection access-token-only future expiry: %v", err)
	}
	if details.AccessToken != "access-token" || details.AccountID != "acct-123" || details.Email != "user@example.com" {
		t.Fatalf("unexpected access-token-only details: %#v", details)
	}

	expiredConnection := futureConnection
	expiredConnection.ExpiresAt = sql.NullTime{Time: time.Now().Add(-time.Hour), Valid: true}
	if _, err := service.refreshAntigravityConnection(context.Background(), store.OAuthConnectionRepository{}, expiredConnection); err == nil || !strings.Contains(err.Error(), "access token has expired") {
		t.Fatalf("expired access-token-only error = %v, want expired token error", err)
	}
}

func TestDisableSiteOnPermanentErrorSkipsStoreWhenErrorIsTemporaryOrSiteMissing(t *testing.T) {
	t.Parallel()

	service := &Service{}
	siteID := uuid.New()

	service.disableSiteOnPermanentError(context.Background(), store.OAuthConnection{SiteID: &siteID}, "temporary network timeout")
	service.disableSiteOnPermanentError(context.Background(), store.OAuthConnection{}, "invalid_grant")
}

func TestImportOAuthAccountReturnsMetadataMarshalFailureWithoutStoreAccess(t *testing.T) {
	t.Parallel()

	service := NewImportService(nil, "master-key", nil)
	result := service.importOAuthAccount(context.Background(), importOAuthAccountInput{
		Email:       "user@example.com",
		Provider:    codexProvider,
		AccountID:   "acct-123",
		AccessToken: "access-token",
		Metadata: map[string]any{
			"unmarshalable": make(chan int),
		},
	}, false)

	if result.Status != "failed" || !strings.Contains(result.Error, "marshal metadata:") {
		t.Fatalf("import result = %#v, want metadata marshal failure", result)
	}
}

func TestDetectAndImportRejectsMalformedCPAWithoutStoreAccess(t *testing.T) {
	t.Parallel()

	result := NewImportService(nil, "master-key", nil).DetectAndImport(context.Background(), []byte(`{"type":"codex","priority":"high"}`), false)

	assertSingleFailedImport(t, result, "parse CPA format:")
}
