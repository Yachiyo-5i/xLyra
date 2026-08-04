package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func storeOfflineGorm(t *testing.T, label string) *gorm.DB {
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
		t.Fatalf("open %s offline gorm db: %v", label, err)
	}
	return db
}

func storeRepositoryOfflineGorm(t *testing.T) *gorm.DB {
	t.Helper()

	return storeOfflineGorm(t, "repository")
}

func storeTransactionGorm(t *testing.T, label string) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(storeTransactionConnector{})
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open %s transaction gorm db: %v", label, err)
	}
	return db
}

func storeTransactionErrorGorm(t *testing.T, label string, beginErr error) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(storeTransactionConnector{beginErr: beginErr})
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open %s transaction error gorm db: %v", label, err)
	}
	return db
}

func storeWithGorm(db *gorm.DB) *Store {
	return &Store{db: db}
}

func storeTimePtr(value time.Time) *time.Time {
	return &value
}

func requireStoreCallback(t *testing.T, action string, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("replace %s callback: %v", action, err)
	}
}

func storeReplaceQueryCallback(t *testing.T, db *gorm.DB, fn func(*gorm.DB)) {
	t.Helper()

	requireStoreCallback(t, "query", db.Callback().Query().Replace("gorm:query", fn))
}

func storeReplaceRowCallback(t *testing.T, db *gorm.DB, fn func(*gorm.DB)) {
	t.Helper()

	requireStoreCallback(t, "row", db.Callback().Row().Replace("gorm:row", fn))
}

func storeReplaceCreateCallback(t *testing.T, db *gorm.DB, fn func(*gorm.DB)) {
	t.Helper()

	requireStoreCallback(t, "create", db.Callback().Create().Replace("gorm:create", fn))
}

func storeQueryRecordNotFound(t *testing.T, db *gorm.DB) {
	t.Helper()

	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrRecordNotFound)
	})
}

func storeCaptureCreate[T any](t *testing.T, db *gorm.DB, label string, mutate func(*T)) *T {
	t.Helper()

	var captured T
	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*T)
		if !ok {
			tx.AddError(errors.New("unexpected " + label + " create destination"))
			return
		}
		captured = *item
		if mutate != nil {
			mutate(item)
		}
		tx.Statement.RowsAffected = 1
	})
	return &captured
}

func storeCaptureUpdate[T any](t *testing.T, db *gorm.DB, label string, mutate func(*T)) *T {
	t.Helper()

	var captured T
	storeReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*T)
		if !ok {
			tx.AddError(errors.New("unexpected " + label + " update destination"))
			return
		}
		captured = *item
		if mutate != nil {
			mutate(item)
		}
		tx.Statement.RowsAffected = 1
	})
	return &captured
}

func storeCreateError(t *testing.T, db *gorm.DB, err error) {
	t.Helper()

	storeReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(err)
	})
}

func storeReplaceUpdateCallback(t *testing.T, db *gorm.DB, fn func(*gorm.DB)) {
	t.Helper()

	requireStoreCallback(t, "update", db.Callback().Update().Replace("gorm:update", fn))
}

func storeReplaceDeleteCallback(t *testing.T, db *gorm.DB, fn func(*gorm.DB)) {
	t.Helper()

	requireStoreCallback(t, "delete", db.Callback().Delete().Replace("gorm:delete", fn))
}

func assertStoreRecordNotFound(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("error = %v, want gorm.ErrRecordNotFound", err)
	}
}

func assertStoreRepositoryErrorContains(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want containing %q", err, want)
	}
}

type storeMigratorStub = bootstrapSchemaMigrator

type storeTransactionConnector struct {
	beginErr error
}

func (c storeTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return storeTransactionConn{beginErr: c.beginErr}, nil
}

func (storeTransactionConnector) Driver() driver.Driver {
	return storeTransactionDriver{}
}

type storeTransactionDriver struct{}

func (storeTransactionDriver) Open(string) (driver.Conn, error) {
	return storeTransactionConn{}, nil
}

type storeTransactionConn struct {
	beginErr error
}

func (storeTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("store fake driver only supports transactions")
}

func (storeTransactionConn) Close() error {
	return nil
}

func (c storeTransactionConn) Begin() (driver.Tx, error) {
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return storeTransactionTx{}, nil
}

func (c storeTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return storeTransactionTx{}, nil
}

type storeTransactionTx struct{}

func (storeTransactionTx) Commit() error {
	return nil
}

func (storeTransactionTx) Rollback() error {
	return nil
}
