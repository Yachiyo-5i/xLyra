package backup

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func backupOfflineGorm(t *testing.T) *gorm.DB {
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

func backupTransactionGorm(t *testing.T, beginErr error) *gorm.DB {
	t.Helper()

	sqlDB := sql.OpenDB(backupTransactionConnector{beginErr: beginErr})
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

func backupStoreWithGorm(t *testing.T, db *gorm.DB) *store.Store {
	t.Helper()

	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}

func backupReplaceQueryCallback(t *testing.T, db *gorm.DB, fn func(*gorm.DB)) {
	t.Helper()

	if err := db.Callback().Query().Replace("gorm:query", fn); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
}

func assertBackupErrorContains(t *testing.T, label string, err error, want string) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want to contain %q", label, err, want)
	}
}

func backupEmptyStore(t *testing.T) *store.Store {
	t.Helper()

	return backupStoreWithGorm(t, &gorm.DB{})
}

func backupConfigFile(t *testing.T) *config.ConfigFile {
	t.Helper()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	return confFile
}

func backupReadyAutomaticConfigFromInput(t *testing.T) config.AutomaticBackupConfig {
	t.Helper()

	base := automaticS3TestConfig()
	return backupAutomaticConfigFromInput(t, AutomaticConfigInput{
		Cron:             base.Cron,
		RetentionCount:   base.RetentionCount,
		Endpoint:         base.Storage.Endpoint,
		Bucket:           base.Storage.Bucket,
		Prefix:           base.Storage.Prefix,
		AccessKey:        base.Storage.AccessKey,
		SecretKey:        "secret",
		BackupPassphrase: "passphrase",
		ForcePathStyle:   base.Storage.ForcePathStyle,
	}, config.AutomaticBackupConfig{})
}

func backupAutomaticConfigFromInput(t *testing.T, input AutomaticConfigInput, current config.AutomaticBackupConfig) config.AutomaticBackupConfig {
	t.Helper()

	builder := NewAutomaticService(Service{}, "master-key")
	cfg, err := builder.configFromInput(input, current)
	if err != nil {
		t.Fatalf("configFromInput: %v", err)
	}
	return cfg
}

func backupReadyAutomaticConfigFile(t *testing.T) *config.ConfigFile {
	t.Helper()

	return automaticConfigFile(t, backupReadyAutomaticConfigFromInput(t))
}

func backupImportExportService(t *testing.T) Service {
	t.Helper()

	return Service{
		db:        backupEmptyStore(t),
		confFile:  backupConfigFile(t),
		masterKey: "master-key",
		now:       func() time.Time { return time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC) },
	}
}

type backupTransactionConnector struct {
	beginErr error
}

func (c backupTransactionConnector) Connect(context.Context) (driver.Conn, error) {
	return backupTransactionConn{beginErr: c.beginErr}, nil
}

func (c backupTransactionConnector) Driver() driver.Driver {
	return backupTransactionDriver{beginErr: c.beginErr}
}

type backupTransactionDriver struct {
	beginErr error
}

func (d backupTransactionDriver) Open(string) (driver.Conn, error) {
	return backupTransactionConn{beginErr: d.beginErr}, nil
}

type backupTransactionConn struct {
	beginErr error
}

func (c backupTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("fake driver only supports transactions")
}

func (c backupTransactionConn) Close() error {
	return nil
}

func (c backupTransactionConn) Begin() (driver.Tx, error) {
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return backupTransactionTx{}, nil
}

func (c backupTransactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return backupTransactionTx{}, nil
}

type backupTransactionTx struct{}

func (backupTransactionTx) Commit() error {
	return nil
}

func (backupTransactionTx) Rollback() error {
	return nil
}
