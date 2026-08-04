package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestEnsureConnectionFreshRejectsProviderMismatchOffline(t *testing.T) {
	t.Parallel()

	encryptedAccess, connection := encryptedOAuthConnectionForProvider(t, codexProvider)
	connection.EncryptedAccessToken = encryptedAccess
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected ensure query destination"))
			return
		}
		*items = []store.OAuthConnection{connection}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("ensure provider mismatch should not save"))
	})

	if _, err := service.EnsureAntigravityConnectionFresh(context.Background(), *connection.SiteID); err == nil || !strings.Contains(err.Error(), "does not support antigravity refresh") {
		t.Fatalf("EnsureAntigravityConnectionFresh error = %v, want provider mismatch", err)
	}
}

func TestEnsureCodexConnectionFreshReturnsUnexpiredDetailsOffline(t *testing.T) {
	t.Parallel()

	encryptedAccess, connection := encryptedOAuthConnectionForProvider(t, codexProvider)
	connection.EncryptedAccessToken = encryptedAccess
	connection.ExpiresAt = sql.NullTime{Time: time.Now().Add(2 * time.Hour), Valid: true}
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected codex ensure query destination"))
			return
		}
		*items = []store.OAuthConnection{connection}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("fresh codex connection should not save"))
	})

	details, err := service.EnsureCodexConnectionFresh(context.Background(), *connection.SiteID)
	if err != nil {
		t.Fatalf("EnsureCodexConnectionFresh returned error: %v", err)
	}
	if details.AccessToken != "fixture-access-token" || details.Connection.ID != connection.ID {
		t.Fatalf("details = %#v, want decrypted unexpired connection", details)
	}
}

func TestRefreshCodexConnectionRejectsUnsupportedProviderAndExpiredAccessOnlyOffline(t *testing.T) {
	t.Parallel()

	encryptedAccess, connection := encryptedOAuthConnectionForProvider(t, "other")
	connection.EncryptedAccessToken = encryptedAccess
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected refresh query destination"))
			return
		}
		*item = connection
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		tx.AddError(errors.New("refresh guard should not save"))
	})

	if _, err := service.RefreshCodexConnection(context.Background(), connection.ID); err == nil || !strings.Contains(err.Error(), "is not supported") {
		t.Fatalf("RefreshCodexConnection unsupported error = %v", err)
	}

	connection.Provider = codexProvider
	connection.ExpiresAt = sql.NullTime{Time: time.Now().Add(-time.Hour), Valid: true}
	if _, err := service.RefreshCodexConnection(context.Background(), connection.ID); err == nil || !strings.Contains(err.Error(), "has no refresh_token") {
		t.Fatalf("RefreshCodexConnection expired access-only error = %v, want no refresh token guard", err)
	}
}

func TestUpdateImportedSiteMetaReturnsWhenSiteMissingOffline(t *testing.T) {
	t.Parallel()

	saveCount := 0
	service := NewImportService(oauthStoreWithGorm(t, oauthGormWithQueryUpdate(t, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrRecordNotFound)
	}, func(tx *gorm.DB) {
		saveCount++
	})), "master-key", nil)

	service.updateImportedSiteMeta(context.Background(), store.NewSiteRepository(service.db.DB()), uuid.New(), codexProvider, uuid.New(), "acct", "user@example.com", nil)
	if saveCount != 0 {
		t.Fatalf("save count = %d, want no save for missing site", saveCount)
	}
}

func TestUpdateImportedSiteMetaMergesPlanAndProxyOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	connectionID := uuid.New()
	var saved store.Site
	db := oauthGormWithQueryUpdate(t, func(tx *gorm.DB) {
		site, ok := tx.Statement.Dest.(*store.Site)
		if !ok {
			tx.AddError(errors.New("unexpected site query destination"))
			return
		}
		*site = store.Site{ID: siteID, Name: "Imported", Meta: store.JSON(`{"kept":"yes"}`)}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		site, ok := tx.Statement.Dest.(*store.Site)
		if !ok {
			tx.AddError(errors.New("unexpected site save destination"))
			return
		}
		saved = *site
		tx.Statement.RowsAffected = 1
	})
	service := NewImportService(oauthStoreWithGorm(t, db), "master-key", nil)
	service.proxyID = " proxy-main "

	service.updateImportedSiteMeta(context.Background(), store.NewSiteRepository(db), siteID, codexProvider, connectionID, "acct-123", "user@example.com", map[string]any{"plan_type": " plus "})

	var meta map[string]any
	if err := json.Unmarshal(saved.Meta, &meta); err != nil {
		t.Fatalf("decode saved meta: %v", err)
	}
	if meta["kept"] != "yes" || meta["oauth_provider"] != codexProvider || meta["oauth_connection_id"] != connectionID.String() || meta["oauth_plan_type"] != "plus" || meta["proxy_id"] != "proxy-main" {
		t.Fatalf("saved meta = %#v, want merged oauth metadata", meta)
	}
}

func TestUpdateImportedSiteMetaRebuildsOauthFieldsOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	connectionID := uuid.New()
	var saved store.Site
	db := oauthGormWithQueryUpdate(t, func(tx *gorm.DB) {
		site, ok := tx.Statement.Dest.(*store.Site)
		if !ok {
			tx.AddError(errors.New("unexpected site query destination"))
			return
		}
		*site = store.Site{ID: siteID, Name: "Imported", Meta: store.JSON(`{"kept":"yes","oauth_connection_id":"old-connection","oauth_provider":"old-provider","oauth_plan_type":"legacy","proxy_id":"old-proxy"}`)}
		tx.Statement.RowsAffected = 1
	}, func(tx *gorm.DB) {
		site, ok := tx.Statement.Dest.(*store.Site)
		if !ok {
			tx.AddError(errors.New("unexpected site save destination"))
			return
		}
		saved = *site
		tx.Statement.RowsAffected = 1
	})
	service := NewImportService(oauthStoreWithGorm(t, db), "master-key", nil)
	service.proxyID = " proxy-main "

	service.updateImportedSiteMeta(context.Background(), store.NewSiteRepository(db), siteID, codexProvider, connectionID, "acct-123", "user@example.com", map[string]any{"plan_type": " plus "})

	var meta map[string]any
	if err := json.Unmarshal(saved.Meta, &meta); err != nil {
		t.Fatalf("decode saved meta: %v", err)
	}
	if meta["kept"] != "yes" || meta["oauth_provider"] != codexProvider || meta["oauth_connection_id"] != connectionID.String() || meta["oauth_plan_type"] != "plus" || meta["proxy_id"] != "proxy-main" {
		t.Fatalf("saved meta = %#v, want rebuilt oauth metadata", meta)
	}
	if _, ok := meta["oauth_subscription_tier"]; ok {
		t.Fatalf("saved meta should not keep unrelated oauth fields: %#v", meta)
	}
}

func encryptedOAuthConnectionForProvider(t *testing.T, provider string) (string, store.OAuthConnection) {
	t.Helper()

	service := NewService(nil, "master-key")
	encryptedAccess, _, err := service.credentials.Encrypt("fixture-access-token")
	if err != nil {
		t.Fatalf("encrypt access token: %v", err)
	}
	siteID := uuid.New()
	return encryptedAccess, store.OAuthConnection{
		ID:                   uuid.New(),
		Provider:             provider,
		SiteID:               &siteID,
		AccountID:            "acct-123",
		Email:                "user@example.com",
		EncryptedAccessToken: encryptedAccess,
		Status:               "connected",
	}
}
