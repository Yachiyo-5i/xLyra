package backup

import (
	"testing"
	"time"

	"xlyra/server/internal/config"
)

func TestNormalizeBackupObjectKeyRequiresConfiguredPrefixAndBackupName(t *testing.T) {
	valid, err := normalizeBackupObjectKey("xlyra/prod", "xlyra/prod/xlyra-backup-20260621-030000.zip.xlyra")
	if err != nil {
		t.Fatalf("expected valid key: %v", err)
	}
	if valid != "xlyra/prod/xlyra-backup-20260621-030000.zip.xlyra" {
		t.Fatalf("unexpected key: %s", valid)
	}

	for _, key := range []string{
		"xlyra/prod/notes.txt",
		"xlyra/other/xlyra-backup-20260621-030000.zip.xlyra",
		"../xlyra/prod/xlyra-backup-20260621-030000.zip.xlyra",
		"",
	} {
		if _, err := normalizeBackupObjectKey("xlyra/prod", key); err == nil {
			t.Fatalf("expected key %q to be rejected", key)
		}
	}
}

func TestBackupObjectKeyNormalizesPrefix(t *testing.T) {
	if got := backupObjectKey("/xlyra/prod", "xlyra-backup-20260621-030000.zip.xlyra"); got != "xlyra/prod/xlyra-backup-20260621-030000.zip.xlyra" {
		t.Fatalf("unexpected object key: %s", got)
	}
}

func TestSortBackupFilesNewestFirst(t *testing.T) {
	oldest := time.Date(2026, 6, 20, 1, 0, 0, 0, time.UTC)
	newest := oldest.Add(2 * time.Hour)
	files := []AutomaticBackupFile{
		{Key: "xlyra/prod/xlyra-backup-old.zip.xlyra", LastModified: oldest},
		{Key: "xlyra/prod/xlyra-backup-new-a.zip.xlyra", LastModified: newest},
		{Key: "xlyra/prod/xlyra-backup-new-b.zip.xlyra", LastModified: newest},
	}

	sortBackupFiles(files)

	if files[0].Key != "xlyra/prod/xlyra-backup-new-b.zip.xlyra" {
		t.Fatalf("expected newest tie to sort by key desc first, got %s", files[0].Key)
	}
	if files[2].Key != "xlyra/prod/xlyra-backup-old.zip.xlyra" {
		t.Fatalf("expected oldest file last, got %s", files[2].Key)
	}
}

func TestMergeTaskFilesIncludesRunningAndDropsCompletedPlaceholder(t *testing.T) {
	service := NewAutomaticService(Service{}, "master-key")
	cfg := config.DefaultAutomaticBackupConfig()
	cfg.Storage.Prefix = "xlyra/prod/"
	started := time.Date(2026, 6, 21, 3, 0, 0, 0, time.UTC)

	running := AutomaticBackupFile{
		Key:          "xlyra/prod/xlyra-backup-running.zip.xlyra",
		Filename:     "xlyra-backup-running.zip.xlyra",
		LastModified: started,
		Status:       "running",
	}
	completed := AutomaticBackupFile{
		Key:          "xlyra/prod/xlyra-backup-complete.zip.xlyra",
		Filename:     "xlyra-backup-complete.zip.xlyra",
		LastModified: started.Add(time.Minute),
		Status:       "running",
	}
	service.storeTaskFile(running)
	service.storeTaskFile(completed)

	files := service.mergeTaskFiles(cfg, []AutomaticBackupFile{{
		Key:          completed.Key,
		Filename:     completed.Filename,
		Size:         42,
		LastModified: started.Add(2 * time.Minute),
	}})

	if len(files) != 2 {
		t.Fatalf("expected real file plus running placeholder, got %#v", files)
	}
	if files[0].Key != completed.Key || files[0].Status != "" {
		t.Fatalf("expected real completed file to win, got %#v", files[0])
	}
	if files[1].Key != running.Key || files[1].Status != "running" {
		t.Fatalf("expected running placeholder to remain, got %#v", files[1])
	}
	if _, ok := service.tasks[completed.Key]; ok {
		t.Fatalf("expected completed placeholder to be removed")
	}
}

func TestMergeTaskFilesDropsExpiredAndIgnoresForeignTaskPlaceholders(t *testing.T) {
	service := NewAutomaticService(Service{}, "master-key")
	cfg := config.DefaultAutomaticBackupConfig()
	cfg.Storage.Prefix = "xlyra/prod/"

	expired := AutomaticBackupFile{
		Key:          "xlyra/prod/xlyra-backup-expired.zip.xlyra",
		Filename:     "xlyra-backup-expired.zip.xlyra",
		LastModified: time.Now().UTC().Add(-automaticTaskRetention - time.Minute),
		Status:       "failed",
		Error:        "upload failed",
	}
	foreign := AutomaticBackupFile{
		Key:          "xlyra/other/xlyra-backup-running.zip.xlyra",
		Filename:     "xlyra-backup-running.zip.xlyra",
		LastModified: time.Now().UTC(),
		Status:       "running",
	}
	active := AutomaticBackupFile{
		Key:          "xlyra/prod/xlyra-backup-active.zip.xlyra",
		Filename:     "xlyra-backup-active.zip.xlyra",
		LastModified: time.Now().UTC(),
		Status:       "failed",
		Error:        "still visible",
	}
	service.storeTaskFile(expired)
	service.storeTaskFile(foreign)
	service.storeTaskFile(active)

	files := service.mergeTaskFiles(cfg, nil)

	if len(files) != 1 || files[0].Key != active.Key {
		t.Fatalf("expected only active task placeholder, got %#v", files)
	}
	if _, ok := service.tasks[expired.Key]; ok {
		t.Fatalf("expected expired task placeholder to be removed")
	}
	if _, ok := service.tasks[foreign.Key]; !ok {
		t.Fatalf("foreign task should be ignored but retained for its matching prefix")
	}
}

func TestStoreAndRemoveTaskFileInitializesAndDeletes(t *testing.T) {
	var service AutomaticService
	file := AutomaticBackupFile{
		Key:          "xlyra/prod/xlyra-backup-running.zip.xlyra",
		Filename:     "xlyra-backup-running.zip.xlyra",
		LastModified: time.Now().UTC(),
		Status:       "running",
	}

	service.storeTaskFile(file)
	if got := service.tasks[file.Key]; got.Status != "running" {
		t.Fatalf("stored task = %#v", got)
	}

	service.removeTaskFile(file.Key)
	if _, ok := service.tasks[file.Key]; ok {
		t.Fatalf("expected task to be removed")
	}
}

func TestIsBackupObjectRequiresPrefixAndBackupExtension(t *testing.T) {
	t.Parallel()

	if !isBackupObject("xlyra/prod", "/xlyra/prod/xlyra-backup-20260621-030000.zip.xlyra") {
		t.Fatal("expected normalized backup object to match")
	}
	for _, key := range []string{
		"xlyra/prod/notes.zip.xlyra",
		"xlyra/prod/xlyra-backup-20260621-030000.zip",
		"xlyra/other/xlyra-backup-20260621-030000.zip.xlyra",
		"",
	} {
		if isBackupObject("xlyra/prod", key) {
			t.Fatalf("expected %q to be rejected as backup object", key)
		}
	}
}
