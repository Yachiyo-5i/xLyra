package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestAdminAccessTokenSetEnabledSavesNewestOffline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)
	newerID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]AdminAccessToken)
		if !ok {
			tx.AddError(errors.New("unexpected admin access token query destination"))
			return
		}
		*items = []AdminAccessToken{
			{ID: uuid.New(), TokenHash: "old", Enabled: true, CreatedAt: now.Add(-time.Hour)},
			{ID: newerID, TokenHash: "new", Enabled: true, CreatedAt: now},
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	var saved AdminAccessToken
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*AdminAccessToken)
		if !ok {
			tx.AddError(errors.New("unexpected admin access token update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	token, err := NewAdminAccessTokenRepository(db).SetEnabled(context.Background(), false)
	if err != nil {
		t.Fatalf("SetEnabled returned error: %v", err)
	}

	if token.ID != newerID || token.Enabled || saved.ID != newerID || saved.Enabled {
		t.Fatalf("saved token = item:%#v saved:%#v, want newest disabled", token, saved)
	}
}

func TestAdminAccessTokenGetReturnsNewestThenNotFoundOffline(t *testing.T) {
	t.Parallel()

	queryCalls := 0
	newestID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		items, ok := tx.Statement.Dest.(*[]AdminAccessToken)
		if !ok {
			tx.AddError(errors.New("unexpected admin access token query destination"))
			return
		}
		if queryCalls == 1 {
			*items = []AdminAccessToken{
				{ID: uuid.New(), TokenHash: "old", CreatedAt: time.Date(2026, 6, 23, 8, 0, 0, 0, time.UTC)},
				{ID: newestID, TokenHash: "newest", CreatedAt: time.Date(2026, 6, 23, 9, 0, 0, 0, time.UTC)},
			}
		} else {
			*items = nil
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})

	repo := NewAdminAccessTokenRepository(db)
	token, err := repo.Get(context.Background())
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if _, err := repo.Get(context.Background()); err == nil {
		t.Fatal("second Get error = nil, want not found")
	}
	if token.ID != newestID || queryCalls != 2 {
		t.Fatalf("get token = item:%#v queryCalls:%d, want newest then empty", token, queryCalls)
	}
}

func TestAdminUpdatePasswordHashAndLastLoginOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	loginAt := time.Date(2026, 6, 23, 10, 30, 0, 0, time.UTC)
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*Admin)
		if !ok {
			tx.AddError(errors.New("unexpected admin query destination"))
			return
		}
		*item = Admin{ID: adminID, Username: "root", PasswordHash: "old"}
		tx.Statement.RowsAffected = 1
	})
	var saved Admin
	var updateStatement *gorm.Statement
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		if item, ok := tx.Statement.Dest.(*Admin); ok {
			saved = *item
		} else {
			updateStatement = tx.Statement
		}
		tx.Statement.RowsAffected = 1
	})

	admin, err := NewAdminRepository(db).UpdatePasswordHash(context.Background(), adminID, "new-hash")
	if err != nil {
		t.Fatalf("UpdatePasswordHash returned error: %v", err)
	}
	if err := NewAdminRepository(db).UpdateLastLogin(context.Background(), adminID, loginAt); err != nil {
		t.Fatalf("UpdateLastLogin returned error: %v", err)
	}

	if admin.PasswordHash != "new-hash" || saved.PasswordHash != "new-hash" {
		t.Fatalf("password update = item:%#v saved:%#v", admin, saved)
	}
	if updateStatement == nil || updateStatement.Dest == nil {
		t.Fatalf("last login update statement = %#v, want captured update", updateStatement)
	}
}

func TestAdminSessionDeleteOthersKeepsCurrentOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	keepID := uuid.New()
	removeA := uuid.New()
	removeB := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]AdminSession)
		if !ok {
			tx.AddError(errors.New("unexpected admin session query destination"))
			return
		}
		*items = []AdminSession{
			{ID: keepID, AdminID: adminID},
			{ID: removeA, AdminID: adminID},
			{ID: removeB, AdminID: adminID},
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	deleted := []uuid.UUID{}
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*AdminSession)
		if !ok {
			tx.AddError(errors.New("unexpected admin session delete destination"))
			return
		}
		deleted = append(deleted, item.ID)
		tx.Statement.RowsAffected = 1
	})

	if err := NewAdminSessionRepository(db).DeleteOthers(context.Background(), adminID, keepID); err != nil {
		t.Fatalf("DeleteOthers returned error: %v", err)
	}

	if len(deleted) != 2 || deleted[0] != removeA || deleted[1] != removeB {
		t.Fatalf("deleted sessions = %#v, want only non-kept sessions", deleted)
	}
}

func TestAPIKeyAccessEnabledSiteModelIDsFiltersCanonicalOffline(t *testing.T) {
	t.Parallel()

	apiKeyID := uuid.New()
	canonicalID := uuid.New()
	matchingModelID := uuid.New()
	otherModelID := uuid.New()
	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch items := tx.Statement.Dest.(type) {
		case *[]APIKeySiteModelPermission:
			*items = []APIKeySiteModelPermission{
				{APIKeyID: apiKeyID, SiteModelID: matchingModelID, Enabled: true},
				{APIKeyID: apiKeyID, SiteModelID: otherModelID, Enabled: true},
			}
		case *[]SiteModel:
			*items = []SiteModel{
				{ID: matchingModelID, SiteID: siteID, CanonicalID: uuid.NullUUID{UUID: canonicalID, Valid: true}},
				{ID: otherModelID, SiteID: siteID, CanonicalID: uuid.NullUUID{UUID: uuid.New(), Valid: true}},
			}
		case *[]Site:
			*items = []Site{{ID: siteID, Name: "Canonical access site"}}
		case *[]CanonicalModel:
			*items = []CanonicalModel{{ID: canonicalID, ModelKey: "canonical-access-model"}}
		default:
			tx.AddError(errors.New("unexpected api key access query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	repo := NewAPIKeyAccessRepository(db)
	allIDs, err := repo.EnabledSiteModelIDs(context.Background(), apiKeyID)
	if err != nil {
		t.Fatalf("EnabledSiteModelIDs returned error: %v", err)
	}
	canonicalIDs, err := repo.EnabledSiteModelIDsForCanonical(context.Background(), apiKeyID, canonicalID)
	if err != nil {
		t.Fatalf("EnabledSiteModelIDsForCanonical returned error: %v", err)
	}

	if len(allIDs) != 2 || allIDs[0] != matchingModelID || allIDs[1] != otherModelID {
		t.Fatalf("enabled model ids = %#v", allIDs)
	}
	if len(canonicalIDs) != 1 || canonicalIDs[0] != matchingModelID {
		t.Fatalf("canonical model ids = %#v, want only matching model", canonicalIDs)
	}
}

func TestSiteCredentialDeleteMethodsUseExpectedBranchesOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	apiCredentialID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		items, ok := tx.Statement.Dest.(*[]SiteCredential)
		if !ok {
			tx.AddError(errors.New("unexpected site credential query destination"))
			return
		}
		if queryCalls == 1 {
			*items = []SiteCredential{{ID: uuid.New(), SiteID: siteID, CredentialType: "oauth"}}
		} else {
			*items = []SiteCredential{
				{ID: apiCredentialID, SiteID: siteID, CredentialType: "api_key:1"},
				{ID: uuid.New(), SiteID: siteID, CredentialType: "oauth"},
			}
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	deleted := 0
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleted++
		tx.Statement.RowsAffected = 1
	})

	repo := NewSiteCredentialRepository(db)
	if err := repo.DeleteAPIKeysBySite(context.Background(), siteID); err != nil {
		t.Fatalf("DeleteAPIKeysBySite returned error: %v", err)
	}
	if err := repo.DeleteBySiteAndType(context.Background(), siteID, "api_key:1"); err != nil {
		t.Fatalf("DeleteBySiteAndType returned error: %v", err)
	}
	if err := repo.DeleteBySite(context.Background(), siteID); err != nil {
		t.Fatalf("DeleteBySite returned error: %v", err)
	}
	if err := repo.DeleteByID(context.Background(), apiCredentialID); err != nil {
		t.Fatalf("DeleteByID returned error: %v", err)
	}

	if queryCalls != 2 || deleted != 3 {
		t.Fatalf("credential callbacks = queries:%d deletes:%d, want 2 queries and 3 deletes", queryCalls, deleted)
	}
}

func TestSiteSetEnabledAndMetaPersistsVariantsOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*Site)
		if !ok {
			tx.AddError(errors.New("unexpected site query destination"))
			return
		}
		*item = Site{ID: siteID, Name: "enabled variants site", Slug: "enabled-variants", Enabled: true, Meta: JSON(`{"old":true}`)}
		tx.Statement.RowsAffected = 1
	})
	saved := []Site{}
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*Site)
		if !ok {
			tx.AddError(errors.New("unexpected site update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})

	repo := NewSiteRepository(db)
	disabled, err := repo.SetEnabled(context.Background(), siteID, false)
	if err != nil {
		t.Fatalf("SetEnabled returned error: %v", err)
	}
	enabled, err := repo.SetEnabledAndMeta(context.Background(), siteID, true, nil)
	if err != nil {
		t.Fatalf("SetEnabledAndMeta returned error: %v", err)
	}

	if len(saved) != 2 || disabled.Enabled || saved[0].Enabled || !enabled.Enabled || !saved[1].Enabled ||
		string(saved[1].Meta) != "{}" {
		t.Fatalf("site enabled saves = disabled:%#v enabled:%#v saved:%#v", disabled, enabled, saved)
	}
}

func TestSiteModelPricingMarkUnavailableExceptKeepsManualAndSeenOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	keepModelID := uuid.New()
	now := time.Date(2026, 6, 23, 11, 0, 0, 0, time.UTC)
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]SiteModelPricing)
		if !ok {
			tx.AddError(errors.New("unexpected pricing query destination"))
			return
		}
		*items = []SiteModelPricing{
			{
				ID:           uuid.New(),
				SiteID:       siteID,
				ModelName:    "stale",
				GroupName:    "default",
				Available:    true,
				LastSyncedAt: sql.NullTime{Time: now, Valid: true},
			},
			{
				ID:             uuid.New(),
				SiteID:         siteID,
				SiteModelID:    uuid.NullUUID{UUID: keepModelID, Valid: true},
				ModelName:      "manual",
				GroupName:      "default",
				ManualOverride: true,
				Available:      true,
			},
			{
				ID:        uuid.New(),
				SiteID:    siteID,
				ModelName: "seen",
				GroupName: "default",
				Available: true,
			},
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	saved := []SiteModelPricing{}
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteModelPricing)
		if !ok {
			tx.AddError(errors.New("unexpected pricing update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})

	keepKey := "seen" + string(rune(31)) + "default"
	if err := NewSiteModelPricingRepository(db).MarkUnavailableExcept(context.Background(), siteID, []string{keepKey}); err != nil {
		t.Fatalf("MarkUnavailableExcept returned error: %v", err)
	}

	if len(saved) != 1 || saved[0].ModelName != "stale" || saved[0].Available || !saved[0].LastSyncedAt.Valid {
		t.Fatalf("saved stale pricing = %#v, want only stale item unavailable", saved)
	}
}
