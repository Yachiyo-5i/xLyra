package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	authcrypto "xlyra/server/internal/crypto"
	"xlyra/server/internal/store"
)

func TestChangeAdminPasswordSuccessDeletesOtherSessionsOffline(t *testing.T) {
	t.Parallel()

	adminID := uuid.New()
	keepSessionID := uuid.New()
	deleteSessionID := uuid.New()
	currentHash, err := authcrypto.HashPassword("current-password-123")
	if err != nil {
		t.Fatalf("hash current password: %v", err)
	}
	queryStep := 0
	var savedAdmin store.Admin
	deleteCount := 0
	service := authServiceWithGormCallbacks(t, func(tx *gorm.DB) {
		queryStep++
		switch dest := tx.Statement.Dest.(type) {
		case *store.Admin:
			*dest = store.Admin{ID: adminID, Username: "root", PasswordHash: currentHash, Status: "active"}
			tx.Statement.RowsAffected = 1
		case *[]store.AdminSession:
			*dest = []store.AdminSession{
				{ID: keepSessionID, AdminID: adminID},
				{ID: deleteSessionID, AdminID: adminID},
			}
			tx.Statement.RowsAffected = 2
		default:
			tx.AddError(errors.New("unexpected password query destination"))
		}
	}, nil, func(tx *gorm.DB) {
		admin, ok := tx.Statement.Dest.(*store.Admin)
		if !ok {
			tx.AddError(errors.New("unexpected password update destination"))
			return
		}
		savedAdmin = *admin
		tx.Statement.RowsAffected = 1
	})
	if err := service.db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		deleteCount++
		session, ok := tx.Statement.Dest.(*store.AdminSession)
		if !ok || session.ID != deleteSessionID {
			tx.AddError(errors.New("unexpected session delete destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}

	updated, err := service.ChangeAdminPassword(context.Background(), adminID, "current-password-123", "new-password-123", keepSessionID)
	if err != nil {
		t.Fatalf("ChangeAdminPassword returned error: %v", err)
	}
	if queryStep != 3 {
		t.Fatalf("query step count = %d, want admin lookup, update lookup, sessions lookup", queryStep)
	}
	if deleteCount != 1 {
		t.Fatalf("delete count = %d, want only non-kept session deleted", deleteCount)
	}
	if updated.ID != adminID || savedAdmin.ID != adminID {
		t.Fatalf("updated/saved admin = %#v/%#v, want admin %s", updated, savedAdmin, adminID)
	}
	if savedAdmin.PasswordHash == currentHash || !authcrypto.ComparePassword(savedAdmin.PasswordHash, "new-password-123") {
		t.Fatalf("saved password hash was not replaced with the new password")
	}
}

func TestCreateGatewayAPIKeyValueCustomBranchesOffline(t *testing.T) {
	t.Parallel()

	service := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected custom key query destination"))
			return
		}
		*count = 0
		tx.Statement.RowsAffected = 1
	})
	key, kind, err := service.createGatewayAPIKeyValue(context.Background(), "team-prod-key")
	if err != nil {
		t.Fatalf("createGatewayAPIKeyValue custom success returned error: %v", err)
	}
	if key != "team-prod-key" || kind != apiKeyCustomKind {
		t.Fatalf("custom key result = %q/%q, want key/custom", key, kind)
	}

	queryCount := 0
	conflictService := authServiceWithQueryCallback(t, func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected alias query destination"))
			return
		}
		queryCount++
		if queryCount == 2 {
			*count = 1
		} else {
			*count = 0
		}
		tx.Statement.RowsAffected = 1
	})
	compatibleCustomKey := apiKeyCompatiblePrefix + strings.Repeat("c", apiKeySecretLength)
	key, kind, err = conflictService.createGatewayAPIKeyValue(context.Background(), compatibleCustomKey)
	if key != "" || kind != "" || err == nil || err.Error() != "custom api key conflicts with an existing generated api key alias" {
		t.Fatalf("alias conflict result = %q/%q/%v, want alias conflict", key, kind, err)
	}
	if queryCount != 2 {
		t.Fatalf("alias conflict query count = %d, want custom hash and generated alias checks", queryCount)
	}
}

func TestCreateAPIKeyStopsAtUnavailableTransactionAfterCustomKeyValidation(t *testing.T) {
	t.Parallel()

	service := authServiceWithRejectingTransactionDB(t)
	queryCount := 0
	if err := service.db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected create api key query destination"))
			return
		}
		queryCount++
		*count = 0
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := service.db.Callback().Row().Replace("gorm:row", func(tx *gorm.DB) {
		count, ok := tx.Statement.Dest.(*int64)
		if !ok {
			tx.AddError(errors.New("unexpected create api key row destination"))
			return
		}
		queryCount++
		*count = 0
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace row callback: %v", err)
	}

	result, err := service.CreateAPIKey(context.Background(), CreateAPIKeyInput{
		Name:      "transaction-unavailable",
		CustomKey: "transaction-unavailable-custom-key",
	}, uuid.New())
	if err == nil {
		t.Fatal("CreateAPIKey returned nil error with unavailable transaction connection")
	}
	if result.Key != "" || result.KeyPrefix != "" || result.APIKey.ID != uuid.Nil {
		t.Fatalf("CreateAPIKey result = %#v, want zero on transaction error", result)
	}
	if queryCount != 1 {
		t.Fatalf("custom CreateAPIKey query count = %d, want only custom hash check before transaction", queryCount)
	}
}

func TestAPIKeyPermissionMutationsReturnTransactionErrorsOffline(t *testing.T) {
	t.Parallel()

	service := authServiceWithRejectingTransactionDB(t)
	apiKeyID := uuid.New()
	if apiKey, groups, err := service.SetAPIKeySiteGroups(context.Background(), apiKeyID, []uuid.UUID{uuid.Nil, uuid.New(), uuid.New()}); err == nil || apiKey.ID != uuid.Nil || groups != nil {
		t.Fatalf("SetAPIKeySiteGroups = %#v/%#v/%v, want zero result and transaction error", apiKey, groups, err)
	}
	if apiKey, sites, err := service.SetAPIKeySites(context.Background(), apiKeyID, []uuid.UUID{uuid.New()}, "allow_list"); err == nil || apiKey.ID != uuid.Nil || sites != nil {
		t.Fatalf("SetAPIKeySites = %#v/%#v/%v, want zero result and transaction error", apiKey, sites, err)
	}
}

func authServiceWithRejectingTransactionDB(t *testing.T) *Service {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=127.0.0.1 port=1 user=xlyra dbname=xlyra sslmode=disable connect_timeout=1",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open rejecting offline gorm db: %v", err)
	}
	return NewService(db, "rejecting-transaction-master-key")
}
