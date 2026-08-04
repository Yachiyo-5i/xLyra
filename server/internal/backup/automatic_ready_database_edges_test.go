package backup

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestExportAtRequiresReadyDependencies(t *testing.T) {
	t.Parallel()

	confFile := backupConfigFile(t)
	cases := []struct {
		name    string
		service Service
		want    string
	}{
		{name: "missing database", service: Service{confFile: confFile, masterKey: "master-key"}, want: "database is not available"},
		{name: "missing config", service: Service{db: backupReadyStore(t), masterKey: "master-key"}, want: "config persistence is not available"},
		{name: "missing master key", service: Service{db: backupReadyStore(t), confFile: confFile}, want: "master key is not available"},
	}
	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path, filename, err := tt.service.exportAt(context.Background(), "passphrase", time.Date(2026, 6, 24, 1, 2, 3, 0, time.UTC))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("exportAt path=%q filename=%q err=%v, want %q", path, filename, err, tt.want)
			}
		})
	}
}

func TestAutomaticRunReturnsZeroResultWhenUploadFails(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(backupReadyService(t), "master-key")
	cfg := automaticS3TestConfig()
	client := automaticS3TestClient(t, automaticS3TestTransport(t, nil, map[string]int{http.MethodPut: http.StatusInternalServerError}))

	result, err := service.run(context.Background(), cfg, client, "passphrase", time.Date(2026, 6, 24, 1, 2, 3, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "upload backup to S3") {
		t.Fatalf("run result=%#v err=%v, want upload error", result, err)
	}
	if result.File != (AutomaticBackupFile{}) || result.DeletedCount != 0 || result.DeletedKeys != nil {
		t.Fatalf("run result=%#v, want zero result on upload failure", result)
	}
}

func TestAutomaticRunKeepsUploadedFileWhenPruneFails(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(backupReadyService(t), "master-key")
	cfg := automaticS3TestConfig()
	cfg.RetentionCount = 1
	client := automaticS3TestClient(t, automaticS3TestTransport(t,
		[]automaticS3TestObject{
			{Key: "prod/xlyra-backup-20260623-010000.zip.xlyra", Size: 23, LastModified: "2026-06-23T01:00:00.000Z"},
			{Key: "prod/xlyra-backup-20260622-010000.zip.xlyra", Size: 22, LastModified: "2026-06-22T01:00:00.000Z"},
		}, map[string]int{http.MethodDelete: http.StatusInternalServerError}))

	createdAt := time.Date(2026, 6, 24, 1, 2, 3, 0, time.UTC)
	result, err := service.run(context.Background(), cfg, client, "passphrase", createdAt)
	if err == nil || !strings.Contains(err.Error(), "prune backup prod/xlyra-backup-20260622-010000.zip.xlyra") {
		t.Fatalf("run result=%#v err=%v, want prune error", result, err)
	}
	if result.File.Key != "prod/xlyra-backup-20260624-010203.zip.xlyra" || result.File.Size <= 0 || !result.File.LastModified.Equal(createdAt) {
		t.Fatalf("run result file=%#v, want uploaded file retained with metadata", result.File)
	}
	if result.DeletedCount != 0 || len(result.DeletedKeys) != 0 {
		t.Fatalf("run deleted fields=%d/%#v, want none after prune failure", result.DeletedCount, result.DeletedKeys)
	}
}

func TestDatabaseImportExportWrapTransactionBeginFailures(t *testing.T) {
	t.Parallel()

	txErr := errors.New("transaction begin failed")
	db := backupTransactionGorm(t, txErr)
	err := exportDatabase(context.Background(), db, "master-key", nil)
	if !errors.Is(err, txErr) {
		t.Fatalf("exportDatabase err=%v, want transaction begin failure", err)
	}

	dump := databaseDump{Tables: make(map[string][]map[string]any, len(backupTables))}
	for _, table := range backupTables {
		dump.Tables[table.Name] = nil
	}
	_, err = importDatabase(context.Background(), db, "master-key", dump)
	if !errors.Is(err, txErr) {
		t.Fatalf("importDatabase err=%v, want transaction begin failure", err)
	}
}

func backupReadyService(t *testing.T) Service {
	t.Helper()

	return Service{
		db:        backupReadyStore(t),
		confFile:  backupConfigFile(t),
		masterKey: "master-key",
		now:       func() time.Time { return time.Date(2026, 6, 24, 1, 2, 3, 0, time.UTC) },
		timeZone:  config.TimeZone{Location: time.UTC},
	}
}

func backupReadyStore(t *testing.T) *store.Store {
	t.Helper()

	db := backupTransactionGorm(t, nil)
	backupReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.RowsAffected = 0
	})
	return backupStoreWithGorm(t, db)
}

func TestAutomaticFileMutationsRejectBlankObjectKey(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(Service{confFile: backupReadyAutomaticConfigFile(t)}, "master-key")
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{
			name: "delete",
			call: func() error {
				return service.Delete(context.Background(), " ")
			},
		},
		{
			name: "restore",
			call: func() error {
				_, err := service.Restore(context.Background(), " ")
				return err
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.call(); err == nil || !strings.Contains(err.Error(), "backup object key is required") {
				t.Fatalf("%s empty key err=%v, want key validation", tc.name, err)
			}
		})
	}
}

func TestImportDatabaseRejectsUnregisteredDeleteOrderTable(t *testing.T) {
	dump := databaseDump{Tables: make(map[string][]map[string]any, len(backupTables))}
	for _, table := range backupTables {
		dump.Tables[table.Name] = nil
	}
	original := importDeleteOrder
	importDeleteOrder = append([]string{"not_registered"}, importDeleteOrder...)
	t.Cleanup(func() {
		importDeleteOrder = original
	})

	_, err := importDatabase(context.Background(), backupTransactionGorm(t, nil), "master-key", dump)
	if err == nil || !strings.Contains(err.Error(), "backup table not_registered was not registered") {
		t.Fatalf("importDatabase err=%v, want unregistered table error", err)
	}
}
