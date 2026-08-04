package scheduler

import (
	"log/slog"
	"testing"
)

func TestRegisterConfiguredJobsRemovesAutomaticBackupWhenDisabled(t *testing.T) {
	t.Parallel()

	confFile := schedulerTestConfigFile(t)
	schedulerSetAutomaticBackupConfig(t, confFile, true, "*/5 * * * *", schedulerCompleteBackupStorage())

	scheduler := New(
		slog.Default(),
		Options{ConfigFile: confFile},
		nil,
		nil,
		nil,
		schedulerAutomaticBackupService(),
	)
	scheduler.RegisterConfiguredJobs()
	if scheduler.autoBackupID == 0 {
		t.Fatal("expected automatic backup job id")
	}
	if entries := scheduler.cron.Entries(); len(entries) != 1 {
		t.Fatalf("entries before disabling backup = %d, want 1", len(entries))
	}

	schedulerSetAutomaticBackupConfig(t, confFile, false, "*/5 * * * *", nil)
	scheduler.RegisterConfiguredJobs()

	if scheduler.autoBackupID != 0 {
		t.Fatalf("autoBackupID after disabling backup = %d, want 0", scheduler.autoBackupID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("entries after disabling backup = %d, want 0", len(entries))
	}
}
