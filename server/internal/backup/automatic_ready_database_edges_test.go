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

	"github.com/google/uuid"
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
	_, err := exportDatabase(context.Background(), db, "master-key", "", nil)
	if !errors.Is(err, txErr) {
		t.Fatalf("exportDatabase err=%v, want transaction begin failure", err)
	}

	dump := databaseDump{Tables: make(map[string][]map[string]any, len(backupTables))}
	for _, table := range backupTables {
		dump.Tables[table.Name] = nil
	}
	_, _, err = importDatabase(context.Background(), db, "master-key", dump, uuid.Nil)
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
				_, err := service.StartRestore(" ", ImportOptions{})
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

func TestRestoreDownloadCounterReportsBoundedProgress(t *testing.T) {
	t.Parallel()

	events := make([]ProgressEvent, 0, 2)
	counter := &restoreDownloadCounter{
		total: 10 << 20,
		next:  4 << 20,
		progress: func(event ProgressEvent) {
			events = append(events, event)
		},
	}
	if _, err := counter.Write(make([]byte, 3<<20)); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := counter.Write(make([]byte, 2<<20)); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if _, err := counter.Write(make([]byte, 5<<20)); err != nil {
		t.Fatalf("third write: %v", err)
	}
	if len(events) != 2 || events[0].Bytes != 5<<20 || events[1].Bytes != 10<<20 || events[1].Total != 10<<20 {
		t.Fatalf("events = %#v", events)
	}
}

func TestValidateRestoreObjectSizeRejectsOversizedBackup(t *testing.T) {
	t.Parallel()

	if err := validateRestoreObjectSize(MaxImportBytes); err != nil {
		t.Fatalf("maximum backup size rejected: %v", err)
	}
	if err := validateRestoreObjectSize(MaxImportBytes + 1); err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("oversized backup error = %v", err)
	}
}

func TestAutomaticOperationsAreMutuallyExclusive(t *testing.T) {
	service := NewAutomaticService(Service{}, "master-key")
	if !service.beginRestore() {
		t.Fatal("beginRestore rejected idle service")
	}
	if service.beginBackup() {
		t.Fatal("beginBackup accepted while restore was running")
	}
	service.endRestore()
	if !service.beginBackup() {
		t.Fatal("beginBackup rejected idle service")
	}
	if service.beginRestore() {
		t.Fatal("beginRestore accepted while backup was running")
	}
	service.endBackup()
}

func TestManualImportRejectedWhileAutomaticRestoreRuns(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(backupReadyService(t), "master-key")
	if !service.beginRestore() {
		t.Fatal("beginRestore rejected idle service")
	}
	if _, err := service.base.Import(context.Background(), "secret", []byte("encrypted"), ImportOptions{}); !errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("Import error = %v, want operation in progress", err)
	}
	service.endRestore()
	if _, err := service.base.Import(context.Background(), "secret", []byte("encrypted"), ImportOptions{}); errors.Is(err, ErrOperationInProgress) {
		t.Fatalf("Import error = %v after restore ended, want payload failure", err)
	}
}

func TestAutomaticOperationsRejectedWhileManualImportHoldsGuard(t *testing.T) {
	t.Parallel()

	base := backupReadyService(t)
	if !beginSharedOperation(base.db) {
		t.Fatal("shared operation rejected idle database")
	}
	defer endSharedOperation(base.db)
	service := NewAutomaticService(base, "master-key")
	if service.beginBackup() {
		t.Fatal("beginBackup accepted while manual import held the database guard")
	}
	if service.beginRestore() {
		t.Fatal("beginRestore accepted while manual import held the database guard")
	}
}

func TestDuplicateRestoreStartReconnectsToRunningTask(t *testing.T) {
	service := NewAutomaticService(Service{}, "master-key")
	firstCtx, firstCancel := context.WithCancel(context.Background())
	defer firstCancel()
	first := AutomaticRestoreTask{
		ID:       "first-task",
		Key:      "xlyra/prod/backup.xlyra",
		Status:   "running",
		Progress: ProgressEvent{Step: "download", Status: "in_progress"},
	}
	startedTask, started, err := service.beginRestoreTask(first, &restoreTaskControl{ctx: firstCtx, cancel: firstCancel, cancellable: true})
	if err != nil || !started || startedTask.ID != first.ID {
		t.Fatalf("first start task=%#v started=%v error=%v", startedTask, started, err)
	}
	active, ok := service.GetActiveRestoreTask()
	if !ok || active.ID != first.ID {
		t.Fatalf("active task=%#v found=%v", active, ok)
	}

	duplicateCtx, duplicateCancel := context.WithCancel(context.Background())
	defer duplicateCancel()
	duplicate := first
	duplicate.ID = "duplicate-task"
	reconnected, started, err := service.beginRestoreTask(duplicate, &restoreTaskControl{ctx: duplicateCtx, cancel: duplicateCancel, cancellable: true})
	if err != nil || started || reconnected.ID != first.ID {
		t.Fatalf("duplicate start task=%#v started=%v error=%v", reconnected, started, err)
	}

	different := duplicate
	different.ID = "different-task"
	different.Key = "xlyra/prod/other-backup.xlyra"
	if _, _, err := service.beginRestoreTask(different, nil); !errors.Is(err, ErrAutomaticAlreadyRunning) {
		t.Fatalf("different backup error = %v", err)
	}
	service.endRestore()
	if _, ok := service.GetActiveRestoreTask(); ok {
		t.Fatal("active task remained after restore ended")
	}
}

func TestAutomaticRestoreTaskSnapshotsAreIsolated(t *testing.T) {
	service := NewAutomaticService(Service{}, "master-key")
	finishedAt := time.Now().UTC()
	summary := ImportSummary{Tables: 22, Rows: 800000, FormatVersion: 2}
	task := AutomaticRestoreTask{
		ID:         "task-id",
		Status:     "completed",
		FinishedAt: &finishedAt,
		Summary:    &summary,
		Progress:   ProgressEvent{Step: "complete", Status: "complete", Summary: &summary},
	}
	service.storeRestoreTask(task, nil)

	first, err := service.GetRestoreTask(task.ID)
	if err != nil {
		t.Fatalf("GetRestoreTask: %v", err)
	}
	first.Summary.Rows = 1
	first.Progress.Summary.Rows = 2
	first.FinishedAt = nil
	second, err := service.GetRestoreTask(task.ID)
	if err != nil {
		t.Fatalf("GetRestoreTask second read: %v", err)
	}
	if second.Summary == nil || second.Summary.Rows != summary.Rows || second.Progress.Summary == nil || second.Progress.Summary.Rows != summary.Rows || second.FinishedAt == nil {
		t.Fatalf("stored task changed through returned snapshot: %#v", second)
	}
	if _, err := service.GetRestoreTask("missing"); !errors.Is(err, ErrRestoreTaskNotFound) {
		t.Fatalf("missing task error = %v", err)
	}
}

func TestCancelRestoreTaskCancelsOnlyBeforeCommit(t *testing.T) {
	service := NewAutomaticService(Service{}, "master-key")
	ctx, cancel := context.WithCancel(context.Background())
	control := &restoreTaskControl{ctx: ctx, cancel: cancel, cancellable: true}
	task := AutomaticRestoreTask{
		ID:          "cancel-task",
		Status:      "running",
		Cancellable: true,
		Progress:    ProgressEvent{Step: "import", Status: "in_progress"},
	}
	service.storeRestoreTask(task, control)

	canceled, err := service.CancelRestoreTask(task.ID)
	if err != nil {
		t.Fatalf("CancelRestoreTask: %v", err)
	}
	if canceled.Status != "canceling" || canceled.Cancellable {
		t.Fatalf("canceled task = %#v", canceled)
	}
	if !errors.Is(ctx.Err(), context.Canceled) || !control.wasCanceled() {
		t.Fatalf("restore context was not canceled: %v", ctx.Err())
	}
	if err := control.disable(); !errors.Is(err, context.Canceled) {
		t.Fatalf("disable after cancel error = %v", err)
	}
	if _, err := service.CancelRestoreTask(task.ID); !errors.Is(err, ErrRestoreCannotCancel) {
		t.Fatalf("second cancel error = %v", err)
	}
}

func TestRestoreControlRejectsCancellationAfterCommitGate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	control := &restoreTaskControl{ctx: ctx, cancel: cancel, cancellable: true}
	if err := control.disable(); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if control.requestCancel() {
		t.Fatal("cancel accepted after commit gate")
	}
	if ctx.Err() != nil {
		t.Fatalf("context canceled after commit gate: %v", ctx.Err())
	}
}

func TestFinishedRestoreTaskAllowsImmediateRetry(t *testing.T) {
	service := NewAutomaticService(Service{}, "master-key")
	if !service.beginRestore() {
		t.Fatal("beginRestore rejected idle service")
	}
	task := AutomaticRestoreTask{
		ID:          "finished-cancel-task",
		Status:      "canceling",
		Cancellable: false,
		Progress:    ProgressEvent{Step: "import", Status: "in_progress"},
	}
	service.storeRestoreTask(task, nil)
	service.finishRestoreTask(task.ID, ImportSummary{}, context.Canceled, true)

	finished, err := service.GetRestoreTask(task.ID)
	if err != nil {
		t.Fatalf("GetRestoreTask: %v", err)
	}
	if finished.Status != "canceled" {
		t.Fatalf("finished status = %q, want canceled", finished.Status)
	}
	if !service.beginRestore() {
		t.Fatal("retry rejected after canceled task became visible")
	}
	service.endRestore()
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

	_, _, err := importDatabase(context.Background(), backupTransactionGorm(t, nil), "master-key", dump, uuid.Nil)
	if err == nil || !strings.Contains(err.Error(), "backup table not_registered was not registered") {
		t.Fatalf("importDatabase err=%v, want unregistered table error", err)
	}
}
