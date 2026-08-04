package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestSiteAPIKeyModelUpsertCreatesDefaultsOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeQueryRecordNotFound(t, db)
	captured := storeCaptureCreate[SiteAPIKeyModel](t, db, "site api key model", nil)

	siteID := uuid.New()
	credentialID := uuid.New()
	siteModelID := uuid.New()
	seenAt := time.Date(2026, 6, 23, 8, 30, 0, 0, time.UTC)
	syncedAt := seenAt.Add(time.Minute)
	model, err := NewSiteAPIKeyModelRepository(db).Upsert(context.Background(), UpsertSiteAPIKeyModelParams{
		SiteID:            siteID,
		SiteCredentialID:  credentialID,
		SiteModelID:       siteModelID,
		UpstreamModelName: "gpt-test",
		DisplayName:       "GPT Test",
		Available:         true,
		Enabled:           true,
		LastSeenAt:        seenAt,
		LastSyncedAt:      syncedAt,
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	if model.SiteCredentialID != credentialID || captured.SiteCredentialID != credentialID {
		t.Fatalf("credential IDs = item:%s captured:%s, want %s", model.SiteCredentialID, captured.SiteCredentialID, credentialID)
	}
	if captured.SiteID != siteID || captured.UpstreamModelName != "gpt-test" || captured.DisplayName != "GPT Test" {
		t.Fatalf("captured api key model identity = %#v", captured)
	}
	if !captured.SiteModelID.Valid || captured.SiteModelID.UUID != siteModelID {
		t.Fatalf("site model id = %#v, want valid %s", captured.SiteModelID, siteModelID)
	}
	if !captured.Available || !captured.Enabled {
		t.Fatalf("captured availability flags = %#v", captured)
	}
	if string(captured.Raw) != "{}" {
		t.Fatalf("raw = %s, want {}", captured.Raw)
	}
	if !captured.LastSeenAt.Valid || !captured.LastSeenAt.Time.Equal(seenAt) ||
		!captured.LastSyncedAt.Valid || !captured.LastSyncedAt.Time.Equal(syncedAt) {
		t.Fatalf("captured sync times = seen:%#v synced:%#v", captured.LastSeenAt, captured.LastSyncedAt)
	}
}

func TestSiteAPIKeyModelMarkUnavailableExceptSavesOnlyStaleOffline(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]SiteAPIKeyModel)
		if !ok {
			tx.AddError(errors.New("unexpected site api key model list destination"))
			return
		}
		*items = []SiteAPIKeyModel{
			{ID: uuid.New(), SiteID: siteID, SiteCredentialID: credentialID, UpstreamModelName: "keep", Available: true, Enabled: true},
			{ID: uuid.New(), SiteID: siteID, SiteCredentialID: credentialID, UpstreamModelName: "stale", Available: true, Enabled: true},
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	var saved []SiteAPIKeyModel
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteAPIKeyModel)
		if !ok {
			tx.AddError(errors.New("unexpected site api key model update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})

	if err := NewSiteAPIKeyModelRepository(db).MarkUnavailableExcept(context.Background(), credentialID, []string{"keep"}); err != nil {
		t.Fatalf("MarkUnavailableExcept returned error: %v", err)
	}

	if len(saved) != 1 {
		t.Fatalf("saved models = %d, want 1 stale model", len(saved))
	}
	if saved[0].UpstreamModelName != "stale" || saved[0].Available {
		t.Fatalf("saved stale model = %#v", saved[0])
	}
	if !saved[0].LastSyncedAt.Valid || saved[0].LastSyncedAt.Time.IsZero() {
		t.Fatalf("stale model sync time = %#v, want valid timestamp", saved[0].LastSyncedAt)
	}
}

func TestSiteAPIKeyModelMarkUnavailableByCredentialAndModel(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	siteModelID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteAPIKeyModel)
		if !ok {
			tx.AddError(errors.New("unexpected site api key model query destination"))
			return
		}
		*item = SiteAPIKeyModel{
			ID:               uuid.New(),
			SiteCredentialID: credentialID,
			SiteModelID:      uuid.NullUUID{UUID: siteModelID, Valid: true},
			Available:        true,
			Enabled:          true,
		}
		tx.Statement.RowsAffected = 1
	})
	var saved SiteAPIKeyModel
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteAPIKeyModel)
		if !ok {
			tx.AddError(errors.New("unexpected site api key model update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	if err := NewSiteAPIKeyModelRepository(db).MarkUnavailable(context.Background(), credentialID, siteModelID); err != nil {
		t.Fatalf("MarkUnavailable returned error: %v", err)
	}
	if saved.Available || !saved.Enabled || saved.SiteCredentialID != credentialID || !saved.SiteModelID.Valid || saved.SiteModelID.UUID != siteModelID {
		t.Fatalf("saved model = %#v, want only availability disabled", saved)
	}
}

func TestSiteAPIKeyModelMarkUnavailableIgnoresMissingBinding(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrRecordNotFound)
	})

	if err := NewSiteAPIKeyModelRepository(db).MarkUnavailable(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("MarkUnavailable returned error for missing binding: %v", err)
	}
}

func TestSiteAPIKeyModelUpdateEnabledSavesFetchedModelOffline(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	modelID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteAPIKeyModel)
		if !ok {
			tx.AddError(errors.New("unexpected site api key model query destination"))
			return
		}
		*item = SiteAPIKeyModel{
			ID:                modelID,
			SiteCredentialID:  credentialID,
			UpstreamModelName: "gpt-toggle",
			Available:         true,
			Enabled:           false,
			Raw:               JSON(`{"old":true}`),
		}
		tx.Statement.RowsAffected = 1
	})
	var saved SiteAPIKeyModel
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteAPIKeyModel)
		if !ok {
			tx.AddError(errors.New("unexpected site api key model update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	model, err := NewSiteAPIKeyModelRepository(db).UpdateEnabled(context.Background(), credentialID, "gpt-toggle", true)
	if err != nil {
		t.Fatalf("UpdateEnabled returned error: %v", err)
	}

	if model.ID != modelID || saved.ID != modelID {
		t.Fatalf("updated model IDs = item:%s saved:%s, want %s", model.ID, saved.ID, modelID)
	}
	if !saved.Enabled || saved.UpstreamModelName != "gpt-toggle" || string(saved.Raw) != `{"old":true}` {
		t.Fatalf("saved api key model = %#v", saved)
	}
}

func TestSiteAPIKeyStateUpsertCreatesDefaultsOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeQueryRecordNotFound(t, db)
	captured := storeCaptureCreate[SiteAPIKeyState](t, db, "site api key state", nil)

	credentialID := uuid.New()
	siteID := uuid.New()
	syncedAt := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	state, err := NewSiteAPIKeyStateRepository(db).Upsert(context.Background(), UpsertSiteAPIKeyStateParams{
		SiteCredentialID:  credentialID,
		SiteID:            siteID,
		UpstreamID:        int64(42),
		Name:              "Primary",
		Enabled:           true,
		GroupName:         "paid",
		RemainQuota:       int64(100),
		UsedQuota:         int64(7),
		UnlimitedQuota:    false,
		ExpiredTime:       int64(1893456000),
		ModelLimitsEnable: true,
		SyncMessage:       "ok",
		LastSyncedAt:      syncedAt,
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}

	if state.SiteCredentialID != credentialID || captured.SiteCredentialID != credentialID || captured.SiteID != siteID {
		t.Fatalf("captured state IDs = item:%s captured:%s site:%s", state.SiteCredentialID, captured.SiteCredentialID, captured.SiteID)
	}
	if captured.SyncStatus != "synced" || !captured.Enabled || captured.Name != "Primary" {
		t.Fatalf("captured state core fields = %#v", captured)
	}
	if !captured.UpstreamID.Valid || captured.UpstreamID.Int64 != 42 ||
		!captured.GroupName.Valid || captured.GroupName.String != "paid" ||
		!captured.RemainQuota.Valid || captured.RemainQuota.Int64 != 100 ||
		!captured.UsedQuota.Valid || captured.UsedQuota.Int64 != 7 ||
		!captured.ExpiredTime.Valid || captured.ExpiredTime.Int64 != 1893456000 {
		t.Fatalf("captured nullable fields = %#v", captured)
	}
	if string(captured.UpstreamStatus) != "null" || string(captured.ModelLimits) != "[]" ||
		string(captured.Usage) != "{}" || string(captured.Raw) != "{}" {
		t.Fatalf("captured JSON defaults = %#v", captured)
	}
	if !captured.SyncMessage.Valid || captured.SyncMessage.String != "ok" ||
		!captured.LastSyncedAt.Valid || !captured.LastSyncedAt.Time.Equal(syncedAt) {
		t.Fatalf("captured sync metadata = message:%#v synced:%#v", captured.SyncMessage, captured.LastSyncedAt)
	}
}

func TestSiteAPIKeyStateUpdateEnabledSavesFetchedStateOffline(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteAPIKeyState)
		if !ok {
			tx.AddError(errors.New("unexpected site api key state query destination"))
			return
		}
		*item = SiteAPIKeyState{
			SiteCredentialID: credentialID,
			Name:             "Primary",
			Enabled:          false,
			SyncStatus:       "synced",
			Usage:            JSON(`{"old":true}`),
		}
		tx.Statement.RowsAffected = 1
	})
	var saved SiteAPIKeyState
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteAPIKeyState)
		if !ok {
			tx.AddError(errors.New("unexpected site api key state update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	state, err := NewSiteAPIKeyStateRepository(db).UpdateEnabled(context.Background(), credentialID, true)
	if err != nil {
		t.Fatalf("UpdateEnabled returned error: %v", err)
	}

	if state.SiteCredentialID != credentialID || saved.SiteCredentialID != credentialID {
		t.Fatalf("updated state credential IDs = item:%s saved:%s, want %s", state.SiteCredentialID, saved.SiteCredentialID, credentialID)
	}
	if !saved.Enabled || saved.Name != "Primary" || string(saved.Usage) != `{"old":true}` {
		t.Fatalf("saved api key state = %#v", saved)
	}
}

func TestSiteGroupCreateTrimsStringsAndCreatesOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	var captured SiteGroup
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteGroup)
		if !ok {
			tx.AddError(errors.New("unexpected site group create destination"))
			return
		}
		captured = *item
		tx.Statement.RowsAffected = 1
	})

	group, err := NewSiteGroupRepository(db).Create(context.Background(), UpsertSiteGroupParams{
		Name:        "  Operators  ",
		Slug:        " operators ",
		Description: "  Primary routing group  ",
		Enabled:     true,
		SortOrder:   9,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if group.Name != "Operators" || captured.Name != "Operators" ||
		captured.Slug != "operators" || captured.Description != "Primary routing group" {
		t.Fatalf("captured site group strings = item:%#v captured:%#v", group, captured)
	}
	if !captured.Enabled || captured.SortOrder != 9 {
		t.Fatalf("captured site group flags = %#v", captured)
	}
}

func TestSiteGroupDeleteReportsNotFoundOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*SiteGroup); !ok {
			tx.AddError(errors.New("unexpected site group delete destination"))
			return
		}
		tx.Statement.RowsAffected = 0
	})

	err := NewSiteGroupRepository(db).Delete(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("Delete error = nil, want not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Delete error = %v, want not found", err)
	}
}
