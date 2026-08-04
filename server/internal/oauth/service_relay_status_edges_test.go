package oauth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestAuthOAuthCallbacksValidateRequiredInputsOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := NewService(nil, "validation-master-key")
	if _, _, _, err := service.HandleCodexCallback(ctx, " ", "code"); err == nil || err.Error() != "state and code are required" {
		t.Fatalf("HandleCodexCallback missing state error = %v, want state/code guard", err)
	}
	if _, _, _, err := service.HandleAntigravityCallback(ctx, "state", " "); err == nil || err.Error() != "state and code are required" {
		t.Fatalf("HandleAntigravityCallback missing code error = %v, want state/code guard", err)
	}
}

func TestAuthOAuthCallbackRelaysReturnMissingStateAndStoreErrorsOffline(t *testing.T) {
	t.Parallel()

	for name, handler := range map[string]http.HandlerFunc{
		"codex":       (&codexCallbackRelay{}).handleCallback,
		"antigravity": (&antigravityCallbackRelay{}).handleCallback,
	} {
		t.Run(name+"_missing_state", func(t *testing.T) {
			assertOAuthRelayResponse(t, handler, "/callback", http.StatusBadRequest, "state is required")
		})

		t.Run(name+"_missing_store", func(t *testing.T) {
			assertOAuthRelayResponse(t, handler, "/callback?state=missing-store-state", http.StatusServiceUnavailable, "oauth store is not available")
		})
	}
}

func TestAuthOAuthCallbackRelaysRejectMissingAndInvalidTargetsOffline(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		handler func(*codexCallbackRelay, *antigravityCallbackRelay) http.HandlerFunc
		meta    store.JSON
		want    string
	}{
		"codex_missing_target": {
			handler: func(c *codexCallbackRelay, _ *antigravityCallbackRelay) http.HandlerFunc { return c.handleCallback },
			meta:    store.JSON(`{}`),
			want:    "oauth relay target is not available",
		},
		"antigravity_invalid_target": {
			handler: func(_ *codexCallbackRelay, a *antigravityCallbackRelay) http.HandlerFunc { return a.handleCallback },
			meta:    store.JSON(`{"relay_target_url":"/relative"}`),
			want:    "oauth relay target is invalid",
		},
	} {
		t.Run(name, func(t *testing.T) {
			sessionID := uuid.New()
			db := oauthGormWithQueryUpdate(t, func(tx *gorm.DB) {
				session, ok := tx.Statement.Dest.(*store.OAuthSession)
				if !ok {
					tx.AddError(errors.New("unexpected relay query destination"))
					return
				}
				*session = store.OAuthSession{ID: sessionID, State: "relay-target-state", Status: "pending", Metadata: tc.meta}
				tx.Statement.RowsAffected = 1
			}, func(tx *gorm.DB) {
				tx.AddError(errors.New("relay guard should not update"))
			})
			st := oauthStoreWithGorm(t, db)
			codex := &codexCallbackRelay{db: st}
			antigravity := &antigravityCallbackRelay{db: st}
			assertOAuthRelayResponse(t, tc.handler(codex, antigravity), "/callback?state=relay-target-state&code=abc", http.StatusBadRequest, tc.want)
		})
	}
}

func TestAuthOAuthImportOAuthAccountFailsWhenMetadataCannotMarshalOffline(t *testing.T) {
	t.Parallel()

	service := NewImportService(nil, "metadata-marshal-master-key", nil)
	result := service.importOAuthAccount(context.Background(), importOAuthAccountInput{
		Email:       "user@example.com",
		Provider:    codexProvider,
		AccountID:   "acct-123",
		AccessToken: "access-token",
		Metadata:    map[string]any{"bad": make(chan int)},
	}, false)

	if result.Status != "failed" || !strings.Contains(result.Error, "marshal metadata") {
		t.Fatalf("importOAuthAccount result = %#v, want metadata marshal failure", result)
	}
}

func TestAuthOAuthRefreshAntigravityConnectionHandlesAccessOnlyTokensOffline(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "access-only-master-key")
	encryptedAccess, _, err := service.credentials.Encrypt("access-only-token")
	if err != nil {
		t.Fatalf("encrypt access: %v", err)
	}
	expired := store.OAuthConnection{
		ID:                   uuid.New(),
		Provider:             antigravityProvider,
		EncryptedAccessToken: encryptedAccess,
		ExpiresAt:            sql.NullTime{Time: time.Now().Add(-time.Minute), Valid: true},
	}
	repo := store.NewOAuthConnectionRepository(oauthGormWithQueryUpdate(t, func(tx *gorm.DB) {
		tx.AddError(errors.New("access-only antigravity refresh should not query"))
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("access-only antigravity refresh should not save"))
	}))
	if _, err := service.refreshAntigravityConnection(context.Background(), repo, expired); err == nil || !strings.Contains(err.Error(), "has no refresh_token") {
		t.Fatalf("refreshAntigravityConnection expired access-only error = %v, want no refresh token guard", err)
	}

	unexpired := expired
	unexpired.ExpiresAt = sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true}
	details, err := service.refreshAntigravityConnection(context.Background(), repo, unexpired)
	if err != nil {
		t.Fatalf("refreshAntigravityConnection unexpired access-only returned error: %v", err)
	}
	if details.AccessToken != "access-only-token" || details.Connection.ID != unexpired.ID {
		t.Fatalf("details = %#v, want decrypted unexpired connection", details)
	}
}

func TestAuthOAuthConnectionStatusSkipsNilIDsAndPermanentErrorChecksOffline(t *testing.T) {
	t.Parallel()

	service := NewService(nil, "connection-status-master-key")
	if err := service.MarkConnectionUnavailable(context.Background(), uuid.Nil, "boom"); err != nil {
		t.Fatalf("MarkConnectionUnavailable nil id returned error: %v", err)
	}
	if err := service.MarkConnectionUnavailableBySiteID(context.Background(), uuid.Nil, "boom"); err != nil {
		t.Fatalf("MarkConnectionUnavailableBySiteID nil site returned error: %v", err)
	}
	service.disableSiteOnPermanentError(context.Background(), store.OAuthConnection{}, "temporary timeout")
	service.disableSiteOnPermanentError(context.Background(), store.OAuthConnection{}, "401 unauthorized")
}

func TestAuthOAuthHTTPClientForConnectionReturnsSiteLookupErrorsOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	lookupErr := errors.New("site lookup stopped")
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		tx.AddError(lookupErr)
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("http client lookup should not update"))
	})

	client, err := service.httpClientForConnection(context.Background(), store.OAuthConnection{SiteID: &siteID})
	if client != nil || !errors.Is(err, lookupErr) {
		t.Fatalf("httpClientForConnection = %#v/%v, want site lookup error", client, err)
	}
}
