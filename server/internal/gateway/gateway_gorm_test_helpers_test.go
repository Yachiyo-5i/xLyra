package gateway

import (
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"unsafe"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/store"
)

func gatewayOfflineGorm(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=xlyra dbname=xlyra sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open offline gorm db: %v", err)
	}
	return db
}

func gatewayStoreWithGorm(t *testing.T, db *gorm.DB) *store.Store {
	t.Helper()

	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}

func gatewayGormWithQueryCallback(t *testing.T, callback func(*gorm.DB)) *gorm.DB {
	t.Helper()

	db := gatewayOfflineGorm(t)
	gatewayReplaceQueryCallback(t, db, callback)
	return db
}

func gatewayStoreWithQueryCallback(t *testing.T, callback func(*gorm.DB)) *store.Store {
	t.Helper()

	return gatewayStoreWithGorm(t, gatewayGormWithQueryCallback(t, callback))
}

func gatewayGormWithQueryError(t *testing.T, message string) (*gorm.DB, error) {
	t.Helper()

	queryErr := errors.New(message)
	return gatewayGormWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	}), queryErr
}

func gatewayStoreWithQueryError(t *testing.T, message string) (*store.Store, error) {
	t.Helper()

	queryErr := errors.New(message)
	return gatewayStoreWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	}), queryErr
}

func gatewayReplaceQueryCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Query().Replace("gorm:query", callback); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
}

func gatewayDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
