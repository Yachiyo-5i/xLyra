package scheduler

import (
	"io"
	"log/slog"
	"reflect"
	"testing"
	"unsafe"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"xlyra/server/internal/backup"
	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

const schedulerTestMasterKey = "test-master-key"

func schedulerPostgresGorm(t *testing.T) *gorm.DB {
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

func schedulerStoreWithGorm(t *testing.T, db *gorm.DB) *store.Store {
	t.Helper()

	st := &store.Store{}
	field := reflect.ValueOf(st).Elem().FieldByName("db")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(db))
	return st
}

func schedulerDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func schedulerAutomaticBackupService() *backup.AutomaticService {
	return backup.NewAutomaticService(backup.Service{}, "master-key")
}

func schedulerSetAutomaticBackupConfig(t *testing.T, confFile *config.ConfigFile, enabled bool, cron string, storage map[string]any) {
	t.Helper()

	value := map[string]any{
		"enabled": enabled,
		"cron":    cron,
	}
	if storage != nil {
		value["storage"] = storage
	}
	if err := confFile.Set(config.AutomaticBackupConfigPath, value); err != nil {
		t.Fatalf("set automatic backup config: %v", err)
	}
}

func schedulerCompleteBackupStorage() map[string]any {
	return map[string]any{
		"endpoint":                    "s3.example.com",
		"bucket":                      "xlyra",
		"access_key":                  "access",
		"secret_key_encrypted":        "encrypted-secret",
		"backup_passphrase_encrypted": "encrypted-passphrase",
	}
}
