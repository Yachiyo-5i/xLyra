package usage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

type usageGormCallbacks struct {
	query          func(*gorm.DB)
	create         func(*gorm.DB)
	update         func(*gorm.DB)
	deleteCallback func(*gorm.DB)
}

func usageServiceWithQueryCallback(t *testing.T, callback func(*gorm.DB)) *Service {
	t.Helper()

	db := usageGormWithQueryCallback(t, callback)
	return NewService(usageStoreWithGorm(t, db), config.LoadTimeZone("UTC"))
}

func usageServiceWithQueryError(t *testing.T, message string) (*Service, error) {
	t.Helper()

	queryErr := errors.New(message)
	return usageServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	}), queryErr
}

func usageSummaryServiceWithQueryCallback(t *testing.T, callback func(*gorm.DB)) *SummaryService {
	t.Helper()

	db := usageGormWithQueryCallback(t, callback)
	return NewSummaryService(usageStoreWithGorm(t, db), nil, config.LoadTimeZone("UTC"))
}

func usageSummaryServiceWithQueryError(t *testing.T, message string) (*SummaryService, error) {
	t.Helper()

	queryErr := errors.New(message)
	return usageSummaryServiceWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(queryErr)
	}), queryErr
}

func usageGormWithQueryCallback(t *testing.T, callback func(*gorm.DB)) *gorm.DB {
	t.Helper()

	return usagePostgresGormWithCallbacks(t, usageGormCallbacks{query: callback})
}

func usageServiceWithDryRunGorm(t *testing.T) *Service {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=xlyra dbname=xlyra sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		DryRun:               true,
		Logger:               gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open dry-run gorm db: %v", err)
	}
	return NewService(usageStoreWithGorm(t, db), config.LoadTimeZone("UTC"))
}

func usagePostgresGorm(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=xlyra dbname=xlyra sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open offline gorm db: %v", err)
	}
	return db
}

func usageTransactionGorm(t *testing.T) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(usageTransactionConnector{})
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
		t.Fatalf("open transaction gorm db: %v", err)
	}
	return db
}

func usageTransactionGormWithCallbacks(t *testing.T, callbacks usageGormCallbacks) *gorm.DB {
	t.Helper()

	return usageGormWithCallbacks(t, usageTransactionGorm(t), callbacks)
}

func usagePostgresGormWithCallbacks(t *testing.T, callbacks usageGormCallbacks) *gorm.DB {
	t.Helper()

	return usageGormWithCallbacks(t, usagePostgresGorm(t), callbacks)
}

func usageGormWithCallbacks(t *testing.T, db *gorm.DB, callbacks usageGormCallbacks) *gorm.DB {
	t.Helper()

	if callbacks.query != nil {
		if err := db.Callback().Query().Replace("gorm:query", callbacks.query); err != nil {
			t.Fatalf("replace query callback: %v", err)
		}
	}
	if callbacks.deleteCallback != nil {
		if err := db.Callback().Delete().Replace("gorm:delete", callbacks.deleteCallback); err != nil {
			t.Fatalf("replace delete callback: %v", err)
		}
	}
	if callbacks.create != nil {
		if err := db.Callback().Create().Replace("gorm:create", callbacks.create); err != nil {
			t.Fatalf("replace create callback: %v", err)
		}
	}
	if callbacks.update != nil {
		if err := db.Callback().Update().Replace("gorm:update", callbacks.update); err != nil {
			t.Fatalf("replace update callback: %v", err)
		}
	}
	return db
}

func usageStoreWithGorm(t *testing.T, db *gorm.DB) *store.Store {
	t.Helper()

	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}

func usageGeneralConfigFile(t *testing.T, cleanupEnabled bool, retentionDays int) *config.ConfigFile {
	t.Helper()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load temp config file: %v", err)
	}
	general := config.DefaultGeneralConfig()
	general.Data.RequestDetailCleanupEnabled = cleanupEnabled
	general.Data.RequestDetailRetentionDays = retentionDays
	if err := confFile.Set(config.GeneralConfigPath, config.GeneralConfigToMap(general)); err != nil {
		t.Fatalf("set general config: %v", err)
	}
	return confFile
}

func assertSummaryMaintenanceError(t *testing.T, label string, result SummaryMaintenanceResult, err error, wantSubstring string) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), wantSubstring) {
		t.Fatalf("%s error = %v, want to contain %q", label, err, wantSubstring)
	}
	if result != (SummaryMaintenanceResult{}) {
		t.Fatalf("%s result = %#v, want zero result", label, result)
	}
}

type usageTransactionConnector struct{}

func (usageTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return usageTransactionConn{}, nil
}

func (usageTransactionConnector) Driver() driver.Driver {
	return usageTransactionDriver{}
}

type usageTransactionDriver struct{}

func (usageTransactionDriver) Open(string) (driver.Conn, error) {
	return usageTransactionConn{}, nil
}

type usageTransactionConn struct{}

func (usageTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("fake driver only supports transactions")
}

func (usageTransactionConn) Close() error {
	return nil
}

func (usageTransactionConn) Begin() (driver.Tx, error) {
	return usageTransactionTx{}, nil
}

func (usageTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return usageTransactionTx{}, nil
}

type usageTransactionTx struct{}

func (usageTransactionTx) Commit() error {
	return nil
}

func (usageTransactionTx) Rollback() error {
	return nil
}
