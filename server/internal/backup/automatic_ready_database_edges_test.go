package backup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
	if err == nil || !strings.Contains(err.Error(), "delete backup prod/xlyra-backup-20260622-010000.zip.xlyra") {
		t.Fatalf("run result=%#v err=%v, want prune error", result, err)
	}
	if result.File.Key != "prod/xlyra-backup-20260624-010203.zip.xlyra" || result.File.Size <= 0 || !result.File.LastModified.Equal(createdAt) {
		t.Fatalf("run result file=%#v, want uploaded file retained with metadata", result.File)
	}
	if result.DeletedCount != 0 || len(result.DeletedKeys) != 0 {
		t.Fatalf("run deleted fields=%d/%#v, want none after prune failure", result.DeletedCount, result.DeletedKeys)
	}
}

func TestRunScheduledKeepsCleanupFailureVisible(t *testing.T) {
	t.Parallel()

	var uploadedKey string
	var uploadedMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			uploadedMu.Lock()
			uploadedKey = strings.TrimPrefix(r.URL.Path, "/xlyra/")
			uploadedMu.Unlock()
			w.Header().Set("ETag", `"test-etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/xml")
			if r.URL.Query().Has("versioning") {
				_, _ = w.Write([]byte(`<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></VersioningConfiguration>`))
				return
			}
			uploadedMu.Lock()
			key := uploadedKey
			uploadedMu.Unlock()
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Name>xlyra</Name><Prefix>prod/</Prefix><KeyCount>2</KeyCount><MaxKeys>1000</MaxKeys><IsTruncated>false</IsTruncated><Contents><Key>` + key + `</Key><LastModified>2026-06-24T01:02:03.000Z</LastModified><ETag>etag</ETag><Size>24</Size><StorageClass>STANDARD</StorageClass></Contents><Contents><Key>prod/xlyra-backup-20260623-010000.zip.xlyra</Key><LastModified>2026-06-23T01:00:00.000Z</LastModified><ETag>etag</ETag><Size>23</Size><StorageClass>STANDARD</StorageClass></Contents></ListBucketResult>`))
		case http.MethodDelete:
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<Error><Code>InternalError</Code><Message>forced deletion error</Message></Error>`))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	cfg := backupReadyAutomaticConfigFromInput(t)
	cfg.RetentionCount = 1
	cfg.Storage.Endpoint = server.URL
	cfg.Storage.UseSSL = false
	base := backupReadyService(t)
	base.confFile = automaticConfigFile(t, cfg)
	base.now = func() time.Time { return time.Now().UTC() }
	service := NewAutomaticService(base, "master-key")

	result, err := service.RunScheduled(context.Background())
	if err == nil || !strings.Contains(err.Error(), "delete backup prod/xlyra-backup-20260623-010000.zip.xlyra") {
		t.Fatalf("RunScheduled result=%#v err=%v, want cleanup error", result, err)
	}
	files, err := service.ListFiles(context.Background())
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	for _, file := range files {
		if file.Key != result.File.Key {
			continue
		}
		if file.Status != "failed" || !strings.Contains(file.Error, "delete backup") {
			t.Fatalf("failed file = %#v", file)
		}
		return
	}
	t.Fatalf("failed scheduled backup %q not found in %#v", result.File.Key, files)
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
