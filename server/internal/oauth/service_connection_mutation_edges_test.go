package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestUpdateConnectionSyncMergesMetadataAndSetsSyncTimeOffline(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	var saved store.OAuthConnection
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		if connection, ok := tx.Statement.Dest.(*store.OAuthConnection); ok {
			*connection = store.OAuthConnection{
				ID:       connectionID,
				Provider: codexProvider,
				Status:   "connected",
				Metadata: store.JSON(`{"existing":"kept"}`),
			}
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(errors.New("unexpected oauth connection sync query destination"))
	}, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected oauth connection sync save destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	if err := service.UpdateConnectionSync(context.Background(), connectionID, map[string]any{"models": []any{"gpt-5"}, "existing": "updated"}); err != nil {
		t.Fatalf("UpdateConnectionSync returned error: %v", err)
	}

	if saved.ID != connectionID || !saved.LastSyncAt.Valid {
		t.Fatalf("saved connection identity/sync = %#v", saved)
	}
	var meta map[string]any
	if err := json.Unmarshal(saved.Metadata, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta["existing"] != "updated" {
		t.Fatalf("metadata existing = %#v, want updated: %#v", meta["existing"], meta)
	}
	models, ok := meta["models"].([]any)
	if !ok || len(models) != 1 || models[0] != "gpt-5" {
		t.Fatalf("metadata models = %#v, want [gpt-5]", meta["models"])
	}
}

func TestMarkConnectionAccessTokenOnlyClearsRefreshAndErrorMetadataOffline(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	var saved store.OAuthConnection
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		if connection, ok := tx.Statement.Dest.(*store.OAuthConnection); ok {
			*connection = store.OAuthConnection{
				ID:                    connectionID,
				Provider:              codexProvider,
				Status:                "reconnect_required",
				EncryptedRefreshToken: "encrypted-refresh",
				MaskedRefreshToken:    "sk-...refresh",
				Metadata:              store.JSON(`{"last_error":"expired","last_error_at":"old","plan_type":"plus"}`),
			}
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(errors.New("unexpected access-token-only query destination"))
	}, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected access-token-only save destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	if err := service.MarkConnectionAccessTokenOnly(context.Background(), connectionID); err != nil {
		t.Fatalf("MarkConnectionAccessTokenOnly returned error: %v", err)
	}

	if saved.Status != "connected" || saved.EncryptedRefreshToken != "" || saved.MaskedRefreshToken != "" {
		t.Fatalf("saved access-token-only connection = %#v", saved)
	}
	var meta map[string]any
	if err := json.Unmarshal(saved.Metadata, &meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta["plan_type"] != "plus" || meta["refreshable"] != false || meta["token_mode"] != "access_token_only" {
		t.Fatalf("metadata access-token-only fields = %#v", meta)
	}
	if _, ok := meta["last_error"]; ok {
		t.Fatalf("metadata kept last_error: %#v", meta)
	}
	if _, ok := meta["last_error_at"]; ok {
		t.Fatalf("metadata kept last_error_at: %#v", meta)
	}
	if meta["refresh_warning"] != importAccessTokenOnlyWarning {
		t.Fatalf("refresh_warning = %#v, want access-token-only warning", meta["refresh_warning"])
	}
}

func TestBindConnectionSiteCanClearOrSetSiteOffline(t *testing.T) {
	t.Parallel()

	connectionID := uuid.New()
	oldSiteID := uuid.New()
	newSiteID := uuid.New()
	saved := []store.OAuthConnection{}
	service := oauthServiceWithQueryUpdate(t, func(tx *gorm.DB) {
		if connection, ok := tx.Statement.Dest.(*store.OAuthConnection); ok {
			*connection = store.OAuthConnection{
				ID:       connectionID,
				Provider: codexProvider,
				SiteID:   &oldSiteID,
				Status:   "connected",
			}
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(errors.New("unexpected bind query destination"))
	}, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected bind save destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})

	if err := service.BindConnectionSite(context.Background(), connectionID, uuid.Nil); err != nil {
		t.Fatalf("BindConnectionSite clear returned error: %v", err)
	}
	if err := service.BindConnectionSite(context.Background(), connectionID, newSiteID); err != nil {
		t.Fatalf("BindConnectionSite set returned error: %v", err)
	}

	if len(saved) != 2 {
		t.Fatalf("saved connections = %d, want 2", len(saved))
	}
	if saved[0].SiteID != nil {
		t.Fatalf("cleared site id = %#v, want nil", saved[0].SiteID)
	}
	if saved[1].SiteID == nil || *saved[1].SiteID != newSiteID {
		t.Fatalf("bound site id = %#v, want %s", saved[1].SiteID, newSiteID)
	}
}
