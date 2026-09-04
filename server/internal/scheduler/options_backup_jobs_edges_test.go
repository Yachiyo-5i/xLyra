package scheduler

import (
	"log/slog"
	"testing"
	"time"
)

func TestNewPreservesPositiveTimeoutAndDefaultsInvalidSiteHealthInterval(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{
		SiteHealthInterval: 0,
		SiteHealthTimeout:  3 * time.Second,
		SiteHealthWorkers:  2,
	}, nil, nil, nil)

	if scheduler.options.SiteHealthInterval != 15*time.Minute {
		t.Fatalf("SiteHealthInterval = %s, want default 15m", scheduler.options.SiteHealthInterval)
	}
	if scheduler.options.SiteHealthTimeout != 3*time.Second {
		t.Fatalf("SiteHealthTimeout = %s, want configured 3s", scheduler.options.SiteHealthTimeout)
	}
	if scheduler.options.SiteHealthWorkers != 2 {
		t.Fatalf("SiteHealthWorkers = %d, want configured 2", scheduler.options.SiteHealthWorkers)
	}
}

func TestRegisterDefaultJobsRegistersOnlyConfiguredAutomaticBackup(t *testing.T) {
	t.Parallel()

	confFile := schedulerTestConfigFile(t)
	schedulerSetAutomaticBackupConfig(t, confFile, true, "*/9 * * * *", schedulerCompleteBackupStorage())

	scheduler := New(
		slog.Default(),
		Options{ConfigFile: confFile},
		nil,
		nil,
		nil,
		schedulerAutomaticBackupService(),
	)
	scheduler.RegisterDefaultJobs()

	if scheduler.siteRefreshID != 0 || scheduler.checkinID != 0 {
		t.Fatalf("site jobs = refresh %d checkin %d, want none without site service", scheduler.siteRefreshID, scheduler.checkinID)
	}
	if scheduler.autoBackupID == 0 {
		t.Fatal("expected configured automatic backup job")
	}
	if entries := scheduler.cron.Entries(); len(entries) != 2 {
		t.Fatalf("entries = %d, want automatic backup plus codex version refresh job", len(entries))
	}
}

func TestRegisterConfiguredJobsSkipsAutomaticBackupWithIncompleteCredentials(t *testing.T) {
	t.Parallel()

	confFile := schedulerTestConfigFile(t)
	schedulerSetAutomaticBackupConfig(t, confFile, true, "*/11 * * * *", map[string]any{
		"endpoint": "s3.example.com",
		"bucket":   "xlyra",
	})

	scheduler := New(
		slog.Default(),
		Options{ConfigFile: confFile},
		nil,
		nil,
		nil,
		schedulerAutomaticBackupService(),
	)
	scheduler.RegisterConfiguredJobs()

	if scheduler.autoBackupID != 0 {
		t.Fatalf("autoBackupID = %d, want 0 with incomplete credentials", scheduler.autoBackupID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("entries = %d, want no registered jobs", len(entries))
	}
}
