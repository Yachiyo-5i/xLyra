package scheduler

import (
	"log/slog"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"xlyra/server/internal/config"
	"xlyra/server/internal/site"
	"xlyra/server/internal/usage"
)

func TestRegisterDefaultJobsConfigFileChangeRebuildsConfiguredJobs(t *testing.T) {
	t.Parallel()

	confFile := schedulerTestConfigFile(t)
	scheduler := New(slog.Default(), Options{ConfigFile: confFile}, &site.Service{}, nil, nil)
	scheduler.RegisterDefaultJobs()
	if scheduler.siteRefreshID == 0 || scheduler.checkinID == 0 {
		t.Fatalf("expected initial configured jobs, got refresh=%d checkin=%d", scheduler.siteRefreshID, scheduler.checkinID)
	}
	firstRefreshID := scheduler.siteRefreshID
	firstCheckinID := scheduler.checkinID

	if err := confFile.Set(config.GeneralConfigPath+".tasks.site_refresh_cron", "*/7 * * * *"); err != nil {
		t.Fatalf("set site refresh cron: %v", err)
	}

	readIDs := func() (cron.EntryID, cron.EntryID) {
		scheduler.configuredMu.Lock()
		defer scheduler.configuredMu.Unlock()
		return scheduler.siteRefreshID, scheduler.checkinID
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		refreshID, checkinID := readIDs()
		if refreshID != 0 &&
			checkinID != 0 &&
			refreshID != firstRefreshID &&
			checkinID != firstCheckinID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	refreshID, checkinID := readIDs()
	t.Fatalf(
		"configured jobs were not rebuilt after config change: refresh %d -> %d, checkin %d -> %d",
		firstRefreshID,
		refreshID,
		firstCheckinID,
		checkinID,
	)
}

func TestRegisterConfiguredJobsSkipsAutomaticBackupWithInvalidCron(t *testing.T) {
	t.Parallel()

	confFile := schedulerTestConfigFile(t)
	schedulerSetAutomaticBackupConfig(t, confFile, true, "not a cron", schedulerCompleteBackupStorage())

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
		t.Fatalf("autoBackupID = %d, want 0 for invalid cron", scheduler.autoBackupID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("entries = %d, want 0 when automatic backup cron is invalid", len(entries))
	}
}

func TestRegisterConfiguredJobsClearsExistingJobsWhenServicesDisappear(t *testing.T) {
	t.Parallel()

	confFile := schedulerTestConfigFile(t)
	schedulerSetAutomaticBackupConfig(t, confFile, true, "*/5 * * * *", schedulerCompleteBackupStorage())

	scheduler := New(
		slog.Default(),
		Options{ConfigFile: confFile},
		&site.Service{},
		nil,
		nil,
		schedulerAutomaticBackupService(),
	)
	scheduler.RegisterConfiguredJobs()
	if scheduler.siteRefreshID == 0 || scheduler.checkinID == 0 || scheduler.autoBackupID == 0 {
		t.Fatalf("expected all configured jobs, got refresh=%d checkin=%d backup=%d", scheduler.siteRefreshID, scheduler.checkinID, scheduler.autoBackupID)
	}

	scheduler.sites = nil
	scheduler.autoBackups = nil
	scheduler.RegisterConfiguredJobs()

	if scheduler.siteRefreshID != 0 || scheduler.checkinID != 0 || scheduler.autoBackupID != 0 {
		t.Fatalf("expected configured jobs to be cleared, got refresh=%d checkin=%d backup=%d", scheduler.siteRefreshID, scheduler.checkinID, scheduler.autoBackupID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("entries after services disappeared = %d, want 0", len(entries))
	}
}

func TestUsageSummaryMaintenanceReleasesGuardWhenServiceStoreIsMissing(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, nil, nil, usage.NewSummaryService(nil, nil))
	scheduler.runUsageSummaryMaintenance()

	if scheduler.summarizing.Load() {
		t.Fatal("usage summary guard should be released after store initialization error")
	}
}

func TestNewAppliesDefaultsOnlyToInvalidOptionFields(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{
		SiteHealthInterval: 30 * time.Minute,
		SiteHealthTimeout:  -1,
		SiteHealthWorkers:  0,
	}, nil, nil, nil)

	if scheduler.options.SiteHealthInterval != 30*time.Minute {
		t.Fatalf("SiteHealthInterval = %s, want custom 30m", scheduler.options.SiteHealthInterval)
	}
	if scheduler.options.SiteHealthTimeout != 10*time.Second {
		t.Fatalf("SiteHealthTimeout = %s, want default 10s", scheduler.options.SiteHealthTimeout)
	}
	if scheduler.options.SiteHealthWorkers != 4 {
		t.Fatalf("SiteHealthWorkers = %d, want default 4", scheduler.options.SiteHealthWorkers)
	}
}
