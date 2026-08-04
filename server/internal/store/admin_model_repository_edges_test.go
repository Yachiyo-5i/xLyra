package store

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestAdminAccessTokenRepositoryTouchLastUsedAndDeleteUseOfflineCallbacks(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	deleteCalls := 0
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})
	var updateStatement *gorm.Statement
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		updateStatement = tx.Statement
		tx.Statement.RowsAffected = 1
	})

	tokenID := uuid.New()
	repo := NewAdminAccessTokenRepository(db)
	if err := repo.TouchLastUsed(context.Background(), tokenID, net.ParseIP("127.0.0.1"), "access-token-touch-agent", time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("TouchLastUsed returned error: %v", err)
	}
	if err := repo.Delete(context.Background()); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want explicit delete", deleteCalls)
	}
	if updateStatement == nil {
		t.Fatal("TouchLastUsed did not run update callback")
	}
	if updates, ok := updateStatement.Dest.(map[string]any); !ok || updates["last_used_user_agent"] != "access-token-touch-agent" {
		t.Fatalf("touch update dest = %#v, want user agent update map", updateStatement.Dest)
	}
}

func TestAdminAuditLogRepositoryCreateDefaultsActorAndClearsMissingRefsOffline(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 0
	})
	storeReplaceRowCallback(t, db, func(tx *gorm.DB) {
		tx.Statement.RowsAffected = 0
	})
	var created AdminAuditLog
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*AdminAuditLog)
		if !ok {
			tx.AddError(errors.New("unexpected admin audit log create destination"))
			return
		}
		created = *item
		item.ID = uuid.New()
		tx.Statement.RowsAffected = 1
	})

	adminID := uuid.New()
	log, err := NewAdminAuditLogRepository(db).Create(context.Background(), CreateAdminAuditLogParams{
		ActorType:      "",
		AdminID:        adminID,
		AdminSessionID: uuid.New(),
		AccessTokenID:  uuid.New(),
		Action:         "admin_audit.create",
		ResourceType:   "admin_audit_log",
		ResourceID:     "resource-1",
		IPAddress:      net.ParseIP("192.0.2.16"),
		UserAgent:      "audit-log-create-agent",
		RequestID:      "audit-log-create-request",
		Success:        true,
		Metadata:       map[string]any{"scenario": "missing_refs_are_cleared"},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if log.ID == uuid.Nil || created.ActorType != "system" || created.AdminID.Valid ||
		created.AdminSessionID.Valid || created.AccessTokenID.Valid {
		t.Fatalf("created audit log refs/defaults = item:%#v captured:%#v", log, created)
	}
	if created.Action != "admin_audit.create" || created.UserAgent != "audit-log-create-agent" ||
		!net.IP(created.IPAddress).Equal(net.ParseIP("192.0.2.16")) || string(created.Metadata) != `{"scenario":"missing_refs_are_cleared"}` {
		t.Fatalf("created audit log fields = %#v", created)
	}
}

func TestSiteGroupRepositoryCreateUpdateDeleteNormalizesFieldsOffline(t *testing.T) {
	t.Parallel()

	groupID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteGroup)
		if !ok {
			tx.AddError(errors.New("unexpected site group query destination"))
			return
		}
		*item = SiteGroup{ID: groupID, Name: "old", Slug: "old", Enabled: true}
		tx.Statement.RowsAffected = 1
	})
	var saved SiteGroup
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteGroup)
		if !ok {
			tx.AddError(errors.New("unexpected site group update destination"))
			return
		}
		saved = *item
		tx.Statement.RowsAffected = 1
	})
	var created SiteGroup
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*SiteGroup)
		if !ok {
			tx.AddError(errors.New("unexpected site group create destination"))
			return
		}
		created = *item
		item.ID = uuid.New()
		tx.Statement.RowsAffected = 1
	})
	deleteCalls := 0
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})

	repo := NewSiteGroupRepository(db)
	made, err := repo.Create(context.Background(), UpsertSiteGroupParams{
		Name:        " Created Group ",
		Slug:        " created-group ",
		Description: " created description ",
		Enabled:     true,
		SortOrder:   3,
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	updated, err := repo.Update(context.Background(), UpsertSiteGroupParams{
		ID:          groupID,
		Name:        " New Group ",
		Slug:        " new-group ",
		Description: " description ",
		Enabled:     false,
		SortOrder:   7,
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if err := repo.Delete(context.Background(), groupID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	if made.ID == uuid.Nil || created.Name != "Created Group" || created.Slug != "created-group" ||
		created.Description != "created description" || !created.Enabled || created.SortOrder != 3 {
		t.Fatalf("created site group = item:%#v captured:%#v", made, created)
	}
	if updated.Name != "New Group" || saved.Slug != "new-group" || saved.Description != "description" ||
		saved.Enabled || saved.SortOrder != 7 {
		t.Fatalf("updated site group = item:%#v saved:%#v", updated, saved)
	}
	if deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", deleteCalls)
	}
}

func TestCanonicalModelRepositoryUpsertManualCreateAndRestorePersistUpdatesOffline(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		queryCalls++
		switch dest := tx.Statement.Dest.(type) {
		case *[]CanonicalModel:
			*dest = []CanonicalModel{
				{
					ID:           modelID,
					ModelKey:     "old-key",
					DisplayName:  "Old",
					Provider:     "old-provider",
					Status:       "archived",
					Capabilities: JSON(`{"raw_name":"old","auto_created":true}`),
				},
			}
		case *CanonicalModel:
			*dest = CanonicalModel{
				ID:           modelID,
				ModelKey:     "old-key",
				DisplayName:  "Old",
				Provider:     "old-provider",
				Status:       "archived",
				Capabilities: JSON(`{"raw_name":"old","auto_created":true}`),
			}
		default:
			tx.AddError(errors.New("unexpected canonical model query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})
	var saved []CanonicalModel
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*CanonicalModel)
		if !ok {
			tx.AddError(errors.New("unexpected canonical model update destination"))
			return
		}
		saved = append(saved, *item)
		tx.Statement.RowsAffected = 1
	})

	repo := NewCanonicalModelRepository(db)
	upserted, err := repo.Upsert(context.Background(), UpsertCanonicalModelParams{
		ModelKey: "old-key",
		Status:   "active",
	})
	if err != nil {
		t.Fatalf("Upsert returned error: %v", err)
	}
	manual, err := repo.MarkManualCreated(context.Background(), modelID, UpsertCanonicalModelParams{
		DisplayName: "Manual",
		Status:      "",
	})
	if err != nil {
		t.Fatalf("MarkManualCreated returned error: %v", err)
	}
	restored, err := repo.Restore(context.Background(), modelID)
	if err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	if queryCalls != 3 || len(saved) != 3 {
		t.Fatalf("callback counts = query:%d saved:%d, want 3 each", queryCalls, len(saved))
	}
	if upserted.Status != "active" || saved[0].Status != "active" {
		t.Fatalf("upsert restore status = item:%#v saved:%#v", upserted, saved[0])
	}
	if manual.DisplayName != "Manual" || manual.Status != "active" ||
		string(saved[1].Capabilities) != `{"auto_created":false,"manual_created":true,"source":"manual_create"}` {
		t.Fatalf("manual canonical model = item:%#v saved:%#v", manual, saved[1])
	}
	if restored.Status != "active" || saved[2].Status != "active" {
		t.Fatalf("restored canonical model = item:%#v saved:%#v", restored, saved[2])
	}
}

func TestCanonicalMatrixRowMapsModelAndPricingPresence(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	pricingID := uuid.New()
	matchedAt := sql.NullTime{Time: time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC), Valid: true}
	syncedAt := sql.NullTime{Time: time.Date(2026, 6, 22, 11, 0, 0, 0, time.UTC), Valid: true}
	modelCreatedAt := time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC)
	modelUpdatedAt := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)

	row := canonicalMatrixRow(
		Site{ID: siteID, Name: "NewAPI", Slug: "newapi", SiteType: "newapi", Enabled: true},
		SiteModel{
			ID:              modelID,
			UpstreamName:    "gpt-test",
			DisplayName:     "GPT Test",
			Status:          "active",
			MatchSource:     "manual",
			MatchConfidence: 95,
			MatchedAt:       matchedAt,
			CreatedAt:       modelCreatedAt,
			UpdatedAt:       modelUpdatedAt,
		},
		3,
		2,
		SiteModelPricing{
			ID:              pricingID,
			GroupName:       "vip",
			Currency:        "USD",
			InputValue:      sql.NullFloat64{Float64: 1.25, Valid: true},
			OutputValue:     sql.NullFloat64{Float64: 2.5, Valid: true},
			PerRequestValue: sql.NullFloat64{Float64: 0.01, Valid: true},
			BillingType:     "tokens",
			Available:       true,
			LastSyncedAt:    syncedAt,
		},
	)

	if row.SiteID != siteID || row.SiteModelID != modelID || row.APIKeyCount != 3 || row.AvailableAPIKeyCount != 2 {
		t.Fatalf("unexpected matrix ids/counts: %#v", row)
	}
	if row.SiteName != "NewAPI" || row.SiteSlug != "newapi" || row.UpstreamModelName != "gpt-test" || row.DisplayName != "GPT Test" {
		t.Fatalf("unexpected matrix names: %#v", row)
	}
	if row.MatchSource != "manual" || row.MatchConfidence != 95 || !row.MatchedAt.Valid {
		t.Fatalf("unexpected match fields: %#v", row)
	}
	if !row.GroupName.Valid || row.GroupName.String != "vip" || !row.Currency.Valid || row.Currency.String != "USD" {
		t.Fatalf("pricing strings not mapped: %#v", row)
	}
	if !row.PricingAvailable.Valid || !row.PricingAvailable.Bool || !row.PricingLastSyncedAt.Valid {
		t.Fatalf("pricing presence not mapped: %#v", row)
	}
	if !row.SiteModelCreatedAt.Equal(modelCreatedAt) || !row.SiteModelUpdatedAt.Equal(modelUpdatedAt) {
		t.Fatalf("model timestamps not mapped: %#v", row)
	}

	withoutPricing := canonicalMatrixRow(Site{ID: siteID}, SiteModel{ID: modelID}, 0, 0, SiteModelPricing{})
	if withoutPricing.GroupName.Valid || withoutPricing.Currency.Valid || withoutPricing.BillingType.Valid || withoutPricing.PricingAvailable.Valid {
		t.Fatalf("zero pricing should produce invalid pricing fields: %#v", withoutPricing)
	}
}
