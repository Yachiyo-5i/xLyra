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

func TestAdminAccessTokenRepositoryDeleteUsesDeleteCallbackOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	deleteCalls := 0
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})

	if err := NewAdminAccessTokenRepository(db).Delete(context.Background()); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	if deleteCalls != 1 {
		t.Fatalf("deleteCalls=%d, want 1", deleteCalls)
	}
}

func TestAdminAuditLogRepositoryListAppliesFiltersSortingAndPaginationOffline(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	success := true
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]AdminAuditLog)
		if !ok {
			tx.AddError(errors.New("unexpected admin audit log query destination"))
			return
		}
		*items = []AdminAuditLog{
			{ID: uuid.New(), ActorType: "admin", Action: "login", Success: true, CreatedAt: base.Add(-time.Hour)},
			{ID: uuid.New(), ActorType: "admin", Action: "delete", Success: true, CreatedAt: base.Add(-2 * time.Hour)},
			{ID: uuid.New(), ActorType: "token", Action: "login", Success: true, CreatedAt: base.Add(-3 * time.Hour)},
			{ID: uuid.New(), ActorType: "admin", Action: "login", Success: false, CreatedAt: base.Add(time.Hour)},
			{ID: uuid.New(), ActorType: "admin", Action: "login", Success: true, CreatedAt: base},
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})

	items, total, err := NewAdminAuditLogRepository(db).List(context.Background(), AdminAuditLogFilters{
		Action:    "login",
		ActorType: "admin",
		Success:   &success,
		DateFrom:  storeTimePtr(base.Add(-90 * time.Minute)),
		Page:      2,
		PageSize:  1,
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	if total != 2 || len(items) != 1 || !items[0].CreatedAt.Equal(base.Add(-time.Hour)) {
		t.Fatalf("items=%#v total=%d, want second matching item after sorting", items, total)
	}
}

func TestOAuthConnectionRepositoryDeleteBySiteIDSkipsNilAndDeletesMatchesOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	otherSiteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	deleted := []uuid.UUID{}
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		items, ok := tx.Statement.Dest.(*[]OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected oauth query destination"))
			return
		}
		*items = []OAuthConnection{
			{ID: uuid.New(), Provider: "keep-nil"},
			{ID: uuid.New(), Provider: "delete", SiteID: &siteID},
			{ID: uuid.New(), Provider: "keep-other", SiteID: &otherSiteID},
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*OAuthConnection)
		if !ok {
			tx.AddError(errors.New("unexpected oauth delete destination"))
			return
		}
		deleted = append(deleted, item.ID)
		tx.Statement.RowsAffected = 1
	})

	repo := NewOAuthConnectionRepository(db)
	if err := repo.DeleteBySiteID(context.Background(), uuid.Nil); err != nil {
		t.Fatalf("DeleteBySiteID nil returned error: %v", err)
	}
	if err := repo.DeleteBySiteID(context.Background(), siteID); err != nil {
		t.Fatalf("DeleteBySiteID returned error: %v", err)
	}

	if queryCalls != 1 || len(deleted) != 1 {
		t.Fatalf("queryCalls=%d deleted=%#v, want guard skip then one delete", queryCalls, deleted)
	}
}

func TestHealthRepositoryDeletesSnapshotsAndSiteStatesViaCallbacksOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	deleteTypes := []string{}
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteTypes = append(deleteTypes, tx.Statement.Schema.Name)
		tx.Statement.RowsAffected = 1
	})

	repo := NewHealthRepository(db)
	cutoff := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	if err := repo.DeleteSnapshotsBefore(context.Background(), cutoff); err != nil {
		t.Fatalf("DeleteSnapshotsBefore returned error: %v", err)
	}
	if err := repo.DeleteSiteStatesBefore(context.Background(), cutoff); err != nil {
		t.Fatalf("DeleteSiteStatesBefore returned error: %v", err)
	}

	if len(deleteTypes) != 2 || deleteTypes[0] != "HealthSnapshot" || deleteTypes[1] != "SiteHealthState" {
		t.Fatalf("deleteTypes=%#v, want health snapshot then site state", deleteTypes)
	}
}

func TestRequestLogRepositoryDeleteBeforeEmptyAndOldestCreatedAtOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	deleteCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		items, ok := tx.Statement.Dest.(*[]RequestLog)
		if !ok {
			tx.AddError(errors.New("unexpected request log query destination"))
			return
		}
		if queryCalls == 1 {
			*items = nil
		} else {
			*items = []RequestLog{{ID: uuid.New(), CreatedAt: time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)}}
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})

	repo := NewRequestLogRepository(db)
	deleted, err := repo.DeleteBefore(context.Background(), time.Now(), 0)
	if err != nil {
		t.Fatalf("DeleteBefore returned error: %v", err)
	}
	oldest, err := repo.OldestCreatedAt(context.Background())
	if err != nil {
		t.Fatalf("OldestCreatedAt returned error: %v", err)
	}

	if deleted != 0 || deleteCalls != 0 || oldest == nil || queryCalls != 2 {
		t.Fatalf("deleted=%d deleteCalls=%d oldest=%v queryCalls=%d", deleted, deleteCalls, oldest, queryCalls)
	}
}

func TestSiteModelPricingRepositoryMarkUnavailableSkipsManualAndSeenOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	var saved []SiteModelPricing
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]SiteModelPricing)
		if !ok {
			tx.AddError(errors.New("unexpected pricing query destination"))
			return
		}
		*items = []SiteModelPricing{
			{ID: uuid.New(), SiteID: siteID, ModelName: "manual", GroupName: "default", ManualOverride: true, Available: true},
			{ID: uuid.New(), SiteID: siteID, ModelName: "seen", GroupName: "default", Available: true},
			{ID: uuid.New(), SiteID: siteID, ModelName: "stale", GroupName: "default", Available: true},
		}
		tx.Statement.RowsAffected = int64(len(*items))
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteModelPricing)
		if !ok {
			tx.AddError(errors.New("unexpected pricing update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})

	key := "seen" + string(rune(31)) + "default"
	if err := NewSiteModelPricingRepository(db).MarkUnavailableExcept(context.Background(), siteID, []string{key}); err != nil {
		t.Fatalf("MarkUnavailableExcept returned error: %v", err)
	}

	if len(saved) != 1 || saved[0].ModelName != "stale" || saved[0].Available || !saved[0].LastSyncedAt.Valid {
		t.Fatalf("saved=%#v, want only stale pricing marked unavailable", saved)
	}
}

func TestSiteCredentialRepositoryUpsertCreatesThenUpdatesExistingCredentialOffline(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	createCalls := 0
	saveCalls := 0
	var created SiteCredential
	var saved SiteCredential
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		item, ok := tx.Statement.Dest.(*SiteCredential)
		if !ok {
			tx.AddError(errors.New("unexpected site credential query destination"))
			return
		}
		if queryCalls == 1 {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*item = SiteCredential{ID: uuid.New(), SiteID: siteID, CredentialType: "api_key", EncryptedSecret: "old"}
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteCredential)
		if !ok {
			tx.AddError(errors.New("unexpected site credential create destination"))
			return
		}
		createCalls++
		created = *item
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteCredential)
		if !ok {
			tx.AddError(errors.New("unexpected site credential update destination"))
			return
		}
		saveCalls++
		saved = *item
		tx.Statement.RowsAffected = 1
	})

	repo := NewSiteCredentialRepository(db)
	first, err := repo.Upsert(context.Background(), UpsertSiteCredentialParams{
		SiteID:          siteID,
		CredentialType:  "api_key",
		EncryptedSecret: "new",
		MaskedSecret:    "sk-***",
	})
	if err != nil {
		t.Fatalf("first Upsert returned error: %v", err)
	}
	second, err := repo.Upsert(context.Background(), UpsertSiteCredentialParams{
		SiteID:          siteID,
		CredentialType:  "api_key",
		EncryptedSecret: "newer",
		MaskedSecret:    "sk-newer",
		Meta:            JSON(`{"scenario":"update-existing"}`),
	})
	if err != nil {
		t.Fatalf("second Upsert returned error: %v", err)
	}

	if createCalls != 1 || saveCalls != 1 || string(first.Meta) != "{}" {
		t.Fatalf("createCalls=%d saveCalls=%d first=%#v", createCalls, saveCalls, first)
	}
	if created.EncryptedSecret != "new" || second.EncryptedSecret != "newer" || string(saved.Meta) != `{"scenario":"update-existing"}` {
		t.Fatalf("created=%#v saved=%#v second=%#v", created, saved, second)
	}
}

func TestRequestLogRepositoryRecentRateUsageFallsBackToSnapshotTokensOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		switch items := tx.Statement.Dest.(type) {
		case *[]RequestLog:
			*items = []RequestLog{
				{ID: uuid.New(), RequestTokens: sql.NullInt64{Int64: 4, Valid: true}, ResponseTokens: sql.NullInt64{Int64: 6, Valid: true}},
				{ID: uuid.New(), RequestTokens: sql.NullInt64{Int64: 3, Valid: true}},
			}
			tx.Statement.RowsAffected = int64(len(*items))
		case *[]UsageRecord:
			*items = nil
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(errors.New("unexpected recent usage query destination"))
		}
	})

	summary, err := NewRequestLogRepository(db).RecentRateUsage(context.Background(), time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("RecentRateUsage returned error: %v", err)
	}

	if queryCalls != 2 || summary.RPM != 2 || summary.TPM != 13 {
		t.Fatalf("queryCalls=%d summary=%#v, want snapshot token fallback", queryCalls, summary)
	}
}
