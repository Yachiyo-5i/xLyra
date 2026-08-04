package catalog

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
	"unsafe"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/store"
)

func catalogTransactionPostgresGorm(t *testing.T) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(catalogTransactionConnector{})
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open transaction gorm db: %v", err)
	}
	return db
}

type catalogTransactionConnector struct{}

func (catalogTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return catalogTransactionConn{}, nil
}
func (catalogTransactionConnector) Driver() driver.Driver { return catalogTransactionDriver{} }

type catalogTransactionDriver struct{}

func (catalogTransactionDriver) Open(string) (driver.Conn, error) {
	return catalogTransactionConn{}, nil
}

type catalogTransactionConn struct{}

func (catalogTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("catalog fake driver only supports transactions")
}
func (catalogTransactionConn) Close() error              { return nil }
func (catalogTransactionConn) Begin() (driver.Tx, error) { return catalogTransactionTx{}, nil }
func (catalogTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return catalogTransactionTx{}, nil
}

type catalogTransactionTx struct{}

func (catalogTransactionTx) Commit() error   { return nil }
func (catalogTransactionTx) Rollback() error { return nil }

func catalogPostgresGorm(t *testing.T) *gorm.DB {
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

func catalogStoreWithGorm(t *testing.T, db *gorm.DB) *store.Store {
	t.Helper()

	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}

func catalogServiceWithQueryCallback(t *testing.T, callback func(tx *gorm.DB)) *Service {
	t.Helper()

	db := catalogPostgresGorm(t)
	replaceCatalogQueryCallback(t, db, callback)
	return &Service{db: catalogStoreWithGorm(t, db)}
}

func replaceCatalogQueryCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Query().Replace("gorm:query", callback); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
}

func replaceCatalogCreateCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Create().Replace("gorm:create", callback); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
}

func replaceCatalogUpdateCallback(t *testing.T, db *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Update().Replace("gorm:update", callback); err != nil {
		t.Fatalf("replace update callback: %v", err)
	}
}
