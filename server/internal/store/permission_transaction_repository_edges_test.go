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

func TestAPIKeyPermissionTransactionsOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	apiKeyID := uuid.New()
	firstID := uuid.New()
	secondID := uuid.New()
	db := storeTransactionGorm(t, "permission_transaction")
	deleteCalls := 0
	var groupCreates []APIKeySiteGroupPermission
	var modelCreates []APIKeySiteModelPermission
	var siteCreates []APIKeySitePermission
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *APIKeySiteGroupPermission:
			groupCreates = append(groupCreates, *dest)
		case *APIKeySiteModelPermission:
			modelCreates = append(modelCreates, *dest)
		case *APIKeySitePermission:
			siteCreates = append(siteCreates, *dest)
		default:
			tx.AddError(errors.New("unexpected api key permission create destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	accessRepo := NewAPIKeyAccessRepository(db)
	if err := accessRepo.SetSiteGroupPermissions(ctx, apiKeyID, []uuid.UUID{uuid.Nil, firstID, firstID, secondID}); err != nil {
		t.Fatalf("SetSiteGroupPermissions returned error: %v", err)
	}
	if err := accessRepo.SetSiteModelPermissions(ctx, apiKeyID, []uuid.UUID{uuid.Nil, firstID, firstID, secondID}); err != nil {
		t.Fatalf("SetSiteModelPermissions returned error: %v", err)
	}
	if err := NewAPIKeyRepository(db).SetSitePermissions(ctx, apiKeyID, []uuid.UUID{firstID, uuid.Nil, firstID, secondID}); err != nil {
		t.Fatalf("SetSitePermissions returned error: %v", err)
	}

	if deleteCalls != 3 {
		t.Fatalf("deleteCalls = %d, want one clear per permission set", deleteCalls)
	}
	if len(groupCreates) != 2 || groupCreates[0].GroupID != firstID || groupCreates[1].GroupID != secondID {
		t.Fatalf("groupCreates = %#v, want deduped non-nil group IDs", groupCreates)
	}
	if len(modelCreates) != 2 || modelCreates[0].SiteModelID != firstID || modelCreates[1].SiteModelID != secondID {
		t.Fatalf("modelCreates = %#v, want deduped non-nil model IDs", modelCreates)
	}
	if len(siteCreates) != 4 || siteCreates[1].SiteID != uuid.Nil {
		t.Fatalf("siteCreates = %#v, want site permissions to preserve input including nil/duplicates", siteCreates)
	}
}

func TestAdminAccessTokenReplaceAndInitialAdminOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	adminID := uuid.New()
	db := storeTransactionGorm(t, "permission_transaction")
	countCalls := 0
	deleteCalls := 0
	var createdToken AdminAccessToken
	var createdAdmin Admin
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected admin initial count destination"))
			return
		}
		countCalls++
		*count = 0
		tx.Statement.RowsAffected = 1
	})
	storeReplaceRowCallback(t, db, func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected admin initial row destination"))
			return
		}
		countCalls++
		*count = 0
		tx.Statement.RowsAffected = 1
	})
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*AdminAccessToken); !ok {
			tx.AddError(errors.New("unexpected access token delete destination"))
			return
		}
		deleteCalls++
		tx.Statement.RowsAffected = 1
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *AdminAccessToken:
			createdToken = *dest
			dest.ID = uuid.New()
		case *Admin:
			createdAdmin = *dest
			dest.ID = adminID
		default:
			tx.AddError(errors.New("unexpected admin create destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	token, err := NewAdminAccessTokenRepository(db).Replace(ctx, CreateAdminAccessTokenParams{
		AdminID:     adminID,
		TokenHash:   "access-token-hash",
		MaskedToken: "access-token",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("Replace returned error: %v", err)
	}
	admin, err := NewAdminRepository(db).CreateInitial(ctx, CreateAdminParams{
		Username:     "root",
		Nickname:     "Root",
		Avatar:       "avatar.png",
		PasswordHash: "hash",
		Role:         "owner",
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("CreateInitial returned error: %v", err)
	}

	if deleteCalls != 1 || token.ID == uuid.Nil || createdToken.AdminID != adminID ||
		createdToken.TokenHash != "access-token-hash" || !createdToken.Enabled {
		t.Fatalf("token=%#v createdToken=%#v deleteCalls=%d", token, createdToken, deleteCalls)
	}
	if countCalls == 0 || admin.ID != adminID || createdAdmin.Username != "root" ||
		createdAdmin.Nickname != "Root" || createdAdmin.Role != "owner" {
		t.Fatalf("admin=%#v createdAdmin=%#v countCalls=%d", admin, createdAdmin, countCalls)
	}
}

func TestCanonicalModelSyncUpsertCreateAndRateLimitCleanupOffline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	canonicalID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	queryCalls := 0
	var createdCanonical CanonicalModel
	var savedCanonical CanonicalModel
	deleteCalls := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]CanonicalModel:
			queryCalls++
			switch queryCalls {
			case 1:
				*dest = nil
			default:
				*dest = []CanonicalModel{{
					ID:           canonicalID,
					ModelKey:     "existing-canonical",
					DisplayName:  "Old",
					Status:       "active",
					Capabilities: JSON(`{"existing":true}`),
				}}
			}
			tx.Statement.RowsAffected = int64(len(*dest))
		default:
			tx.AddError(errors.New("unexpected canonical model query destination"))
		}
	})
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*CanonicalModel)
		if !ok {
			tx.AddError(errors.New("unexpected canonical create destination"))
			return
		}
		createdCanonical = *item
		item.ID = uuid.New()
		tx.Statement.RowsAffected = 1
	})
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*CanonicalModel)
		if !ok {
			tx.AddError(errors.New("unexpected canonical update destination"))
			return
		}
		savedCanonical = *item
		tx.Statement.RowsAffected = 1
	})
	storeReplaceDeleteCallback(t, db, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*GatewayRateLimitWindow); !ok {
			tx.AddError(errors.New("unexpected rate limit window delete destination"))
			return
		}
		deleteCalls++
		tx.Statement.RowsAffected = 3
	})

	repo := NewCanonicalModelRepository(db)
	created, err := repo.SyncUpsert(ctx, UpsertCanonicalModelParams{
		ModelKey:    "created-canonical",
		DisplayName: "Created",
		Provider:    "openai",
		Status:      "",
	})
	if err != nil {
		t.Fatalf("SyncUpsert create returned error: %v", err)
	}
	updated, err := repo.SyncUpsert(ctx, UpsertCanonicalModelParams{
		ModelKey:               "existing-canonical",
		DisplayName:            "Updated",
		Provider:               "openai",
		Category:               "chat",
		Capabilities:           JSON(`{"synced":true}`),
		Status:                 "",
		SupportedEndpointTypes: JSON(`["responses"]`),
		Modalities:             nil,
		PricingSource:          "",
		LastPricingSyncedAt:    sql.NullTime{Time: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), Valid: true},
	})
	if err != nil {
		t.Fatalf("SyncUpsert update returned error: %v", err)
	}
	if err := NewGatewayRateLimitRepository(db).DeleteWindowsBefore(ctx, time.Date(2026, 6, 24, 11, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("DeleteWindowsBefore returned error: %v", err)
	}

	if created.ID == uuid.Nil || createdCanonical.ModelKey != "created-canonical" ||
		string(createdCanonical.Capabilities) != "{}" || string(createdCanonical.SupportedEndpointTypes) != "[]" ||
		createdCanonical.PricingSource != "none" {
		t.Fatalf("created=%#v captured=%#v, want canonical defaults", created, createdCanonical)
	}
	if updated.ID != canonicalID || savedCanonical.DisplayName != "Updated" ||
		savedCanonical.Status != "active" || savedCanonical.PricingSource != "none" ||
		string(savedCanonical.SupportedEndpointTypes) != `["responses"]` ||
		string(savedCanonical.Modalities) != "[]" ||
		!savedCanonical.LastPricingSyncedAt.Valid {
		t.Fatalf("updated=%#v saved=%#v, want merged existing canonical model", updated, savedCanonical)
	}
	if deleteCalls != 1 {
		t.Fatalf("rate limit deleteCalls = %d, want one cleanup delete", deleteCalls)
	}
}
