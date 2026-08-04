package backup

import (
	"context"
	"testing"
	"time"
)

func TestAutomaticRunReturnsBaseExportFailureBeforeRemoteClientUse(t *testing.T) {
	t.Parallel()

	cfg := backupReadyAutomaticConfigFromInput(t)

	result, err := NewAutomaticService(Service{}, "master-key").run(context.Background(), cfg, nil, "passphrase", time.Date(2026, 6, 23, 1, 2, 3, 0, time.UTC))
	assertBackupErrorContains(t, "run export failure", err, "database is not available")
	if result.File != (AutomaticBackupFile{}) || result.DeletedCount != 0 || result.DeletedKeys != nil {
		t.Fatalf("run result = %#v, want zero result on export failure", result)
	}
}

func TestAutomaticRunFailuresReleaseRunningGuard(t *testing.T) {
	t.Parallel()

	service := NewAutomaticService(Service{}, "master-key")
	_, err := service.RunNow()
	assertBackupErrorContains(t, "RunNow", err, "automatic backup config")
	if service.running.Load() {
		t.Fatal("RunNow should release running guard after readyClient failure")
	}

	_, err = service.RunScheduled(context.Background())
	assertBackupErrorContains(t, "RunScheduled", err, "automatic backup config")
	if service.running.Load() {
		t.Fatal("RunScheduled should release running guard after readyClient failure")
	}
}
