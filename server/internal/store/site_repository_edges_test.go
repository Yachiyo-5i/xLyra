package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSiteCreateBuildsDefaultsOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	captured := storeCaptureCreate[Site](t, db, "site", func(item *Site) {
		if item.ID == uuid.Nil {
			item.ID = uuid.New()
		}
	})

	site, err := NewSiteRepository(db).Create(context.Background(), CreateSiteParams{
		Name:            "Primary Site",
		Slug:            "primary-site",
		SiteType:        "openai",
		BaseURL:         "https://api.example.com",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 2.5,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if site.ID == uuid.Nil {
		t.Fatal("Create should return ID assigned by create callback")
	}
	if captured.Name != "Primary Site" || captured.Slug != "primary-site" || captured.SiteType != "openai" {
		t.Fatalf("captured site identity = %#v", *captured)
	}
	if !captured.Enabled || captured.RoutingPriority != 2.5 || string(captured.Meta) != "{}" {
		t.Fatalf("captured site defaults = %#v", *captured)
	}
}

func TestSiteUpdateSavesFetchedSiteOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		if site, ok := tx.Statement.Dest.(*Site); ok {
			*site = Site{
				ID:       siteID,
				Name:     "Old",
				Slug:     "old",
				SiteType: "openai",
				BaseURL:  "https://old.example.com",
				Status:   "active",
				Enabled:  true,
				Meta:     JSON(`{"kept":true}`),
			}
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(errors.New("unexpected site update query destination"))
	})
	saved := storeCaptureUpdate[Site](t, db, "site", nil)

	updated, err := NewSiteRepository(db).Update(context.Background(), UpdateSiteParams{
		ID:              siteID,
		Name:            "New",
		Slug:            "new",
		SiteType:        "anthropic",
		BaseURL:         "https://new.example.com",
		Status:          "paused",
		Enabled:         false,
		RoutingPriority: 7,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}

	if updated.ID != siteID || saved.ID != siteID {
		t.Fatalf("updated IDs = item:%s saved:%s, want %s", updated.ID, saved.ID, siteID)
	}
	if saved.Name != "New" || saved.Slug != "new" || saved.SiteType != "anthropic" || saved.Status != "paused" {
		t.Fatalf("saved site fields = %#v", saved)
	}
	if saved.Enabled || saved.RoutingPriority != 7 || string(saved.Meta) != "{}" {
		t.Fatalf("saved site flags/defaults = %#v", saved)
	}
}

func TestSiteDeleteMarksSiteDeletedOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		if site, ok := tx.Statement.Dest.(*Site); ok {
			*site = Site{
				ID:      siteID,
				Name:    "Delete Me",
				Slug:    "delete-me",
				Status:  "active",
				Enabled: true,
				Meta:    JSON(`{"kept":"yes"}`),
			}
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(errors.New("unexpected site delete query destination"))
	})
	saved := storeCaptureUpdate[Site](t, db, "site delete", nil)

	if err := NewSiteRepository(db).Delete(context.Background(), siteID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	if saved.Enabled || saved.Status != SiteStatusDeleted {
		t.Fatalf("deleted site flags = %#v", saved)
	}
	if !strings.HasPrefix(saved.Slug, "delete-me-deleted-") {
		t.Fatalf("deleted slug = %q, want deleted slug prefix", saved.Slug)
	}
	var meta map[string]any
	if err := json.Unmarshal(saved.Meta, &meta); err != nil {
		t.Fatalf("decode deleted meta: %v", err)
	}
	if meta["kept"] != "yes" || meta["deleted_original_slug"] != "delete-me" || strings.TrimSpace(meta["deleted_at"].(string)) == "" {
		t.Fatalf("deleted meta = %#v", meta)
	}
}

func TestSiteStateMarkSyncStartedCreatesDefaultsOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrRecordNotFound)
	})
	var captured SiteState
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteState)
		if !ok {
			tx.AddError(errors.New("unexpected site state create destination"))
			return
		}
		captured = *item
		tx.Statement.RowsAffected = 1
	})

	siteID := uuid.New()
	state, err := NewSiteStateRepository(db).MarkSyncStarted(context.Background(), siteID)
	if err != nil {
		t.Fatalf("MarkSyncStarted returned error: %v", err)
	}

	if state.SiteID != siteID || captured.SiteID != siteID {
		t.Fatalf("site state IDs = item:%s captured:%s, want %s", state.SiteID, captured.SiteID, siteID)
	}
	if captured.SyncStatus != "syncing" || captured.SyncMessage.Valid || !captured.LastSyncStartedAt.Valid {
		t.Fatalf("sync started fields = %#v", captured)
	}
	if string(captured.RawStatus) != "{}" || string(captured.UserSummary) != "{}" ||
		string(captured.Pricing) != "{}" || string(captured.Checkin) != "{}" {
		t.Fatalf("sync started json defaults = %#v", captured)
	}
}

func TestSiteStateUpsertSavesExistingOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		if state, ok := tx.Statement.Dest.(*SiteState); ok {
			*state = SiteState{
				SiteID:      siteID,
				SyncStatus:  "old",
				RawStatus:   JSON(`{"old":true}`),
				UserSummary: JSON(`{"old":true}`),
				Pricing:     JSON(`{"old":true}`),
				Checkin:     JSON(`{"old":true}`),
			}
			tx.Statement.RowsAffected = 1
			return
		}
		tx.AddError(errors.New("unexpected site state query destination"))
	})
	var saved SiteState
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteState)
		if !ok {
			tx.AddError(errors.New("unexpected site state update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	state, err := NewSiteStateRepository(db).Upsert(context.Background(), UpsertSiteStateParams{
		SiteID:            siteID,
		ValidationOK:      true,
		ValidationMessage: "ok",
		SyncStatus:        "synced",
		SyncMessage:       "done",
		APIKeyCount:       2,
		ModelCount:        3,
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	if state.SiteID != siteID || saved.SiteID != siteID {
		t.Fatalf("site state IDs = item:%s saved:%s, want %s", state.SiteID, saved.SiteID, siteID)
	}
	if saved.SyncStatus != "synced" || saved.APIKeyCount != 2 || saved.ModelCount != 3 {
		t.Fatalf("saved site state fields = %#v", saved)
	}
	if !saved.ValidationOK.Valid || !saved.ValidationOK.Bool || !saved.ValidationMessage.Valid || saved.ValidationMessage.String != "ok" {
		t.Fatalf("saved validation fields = %#v", saved)
	}
	if !saved.SyncMessage.Valid || saved.SyncMessage.String != "done" {
		t.Fatalf("saved sync message = %#v", saved.SyncMessage)
	}
	if string(saved.RawStatus) != "{}" || string(saved.UserSummary) != `{"old":true}` ||
		string(saved.Pricing) != `{"old":true}` || string(saved.Checkin) != `{"old":true}` {
		t.Fatalf("saved json defaults = %#v", saved)
	}
}
