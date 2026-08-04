package scheduler

import (
	"log/slog"
	"testing"
	"time"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/config"
	"xlyra/server/internal/site"
	"xlyra/server/internal/usage"
)

func TestUsageSummaryCronRunsAtOneMinuteAfterMidnight(t *testing.T) {
	t.Parallel()

	if usageSummaryCron != "1 0 * * *" {
		t.Fatalf("unexpected usage summary cron: %s", usageSummaryCron)
	}
}

func TestModelPricingSyncCronRunsEveryFourHours(t *testing.T) {
	t.Parallel()

	if modelPricingSyncCron != "@every 4h" {
		t.Fatalf("unexpected model pricing sync cron: %s", modelPricingSyncCron)
	}
}

func TestNewAppliesDefaultOptions(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, nil, nil, nil)

	if scheduler.options.SiteHealthInterval != 15*time.Minute {
		t.Fatalf("SiteHealthInterval = %s, want 15m", scheduler.options.SiteHealthInterval)
	}
	if scheduler.options.SiteHealthTimeout != 10*time.Second {
		t.Fatalf("SiteHealthTimeout = %s, want 10s", scheduler.options.SiteHealthTimeout)
	}
	if scheduler.options.SiteHealthWorkers != 4 {
		t.Fatalf("SiteHealthWorkers = %d, want 4", scheduler.options.SiteHealthWorkers)
	}
}

func TestNewPreservesCustomOptions(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{
		SiteHealthInterval: 30 * time.Minute,
		SiteHealthTimeout:  45 * time.Second,
		SiteHealthWorkers:  8,
	}, nil, nil, nil)

	if scheduler.options.SiteHealthInterval != 30*time.Minute {
		t.Fatalf("SiteHealthInterval = %s, want 30m", scheduler.options.SiteHealthInterval)
	}
	if scheduler.options.SiteHealthTimeout != 45*time.Second {
		t.Fatalf("SiteHealthTimeout = %s, want 45s", scheduler.options.SiteHealthTimeout)
	}
	if scheduler.options.SiteHealthWorkers != 8 {
		t.Fatalf("SiteHealthWorkers = %d, want 8", scheduler.options.SiteHealthWorkers)
	}
}

func TestRegisterDefaultJobsWithUnavailableServicesDoesNotRegisterJobs(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, nil, nil, nil)

	scheduler.RegisterDefaultJobs()

	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("expected no registered jobs without services, got %d", len(entries))
	}
}

func TestRegisterDefaultJobsWithCoreServicesRegistersBaseAndConfiguredJobs(t *testing.T) {
	t.Parallel()

	scheduler := New(
		slog.Default(),
		Options{},
		&site.Service{},
		&catalog.SyncService{},
		&usage.SummaryService{},
	)

	scheduler.RegisterDefaultJobs()

	if entries := scheduler.cron.Entries(); len(entries) != 5 {
		t.Fatalf("expected site health, models.dev sync, usage summary, site refresh, and newapi checkin jobs, got %d", len(entries))
	}
	if scheduler.siteRefreshID == 0 {
		t.Fatal("expected configured site refresh job id")
	}
	if scheduler.checkinID == 0 {
		t.Fatal("expected configured newapi checkin job id")
	}
	if scheduler.autoBackupID != 0 {
		t.Fatal("automatic backup should not be registered without backup service")
	}
}

func TestRegisterConfiguredJobsReplacesExistingConfiguredSiteJobs(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, &site.Service{}, nil, nil)
	scheduler.RegisterConfiguredJobs()

	firstRefreshID := scheduler.siteRefreshID
	firstCheckinID := scheduler.checkinID
	if firstRefreshID == 0 || firstCheckinID == 0 {
		t.Fatalf("expected configured site jobs, got refresh=%d checkin=%d", firstRefreshID, firstCheckinID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 2 {
		t.Fatalf("entries after first registration = %d, want 2", len(entries))
	}

	scheduler.RegisterConfiguredJobs()

	if scheduler.siteRefreshID == 0 || scheduler.checkinID == 0 {
		t.Fatalf("expected configured site jobs after replacement, got refresh=%d checkin=%d", scheduler.siteRefreshID, scheduler.checkinID)
	}
	if scheduler.siteRefreshID == firstRefreshID || scheduler.checkinID == firstCheckinID {
		t.Fatalf("configured job IDs were not replaced: refresh=%d checkin=%d", scheduler.siteRefreshID, scheduler.checkinID)
	}
	entries := scheduler.cron.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries after replacement = %d, want 2", len(entries))
	}
	for _, entry := range entries {
		if entry.ID == firstRefreshID || entry.ID == firstCheckinID {
			t.Fatalf("old configured job entry %d was not removed", entry.ID)
		}
	}
}

func TestRegisterConfiguredJobsRegistersAutomaticBackupWithCredentials(t *testing.T) {
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
		t.Fatalf("entries = %d, want 1 automatic backup job", len(entries))
	}
}

func TestScheduledJobsSkipWhenPreviousRunIsActive(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, nil, nil, nil)

	scheduler.running.Store(true)
	scheduler.runSiteHealthChecks()
	if !scheduler.running.Load() {
		t.Fatal("site health guard should remain active after skipped run")
	}

	scheduler.syncing.Store(true)
	scheduler.runModelsDevSync()
	if !scheduler.syncing.Load() {
		t.Fatal("models.dev guard should remain active after skipped run")
	}

	scheduler.summarizing.Store(true)
	scheduler.runUsageSummaryMaintenance()
	if !scheduler.summarizing.Load() {
		t.Fatal("usage summary guard should remain active after skipped run")
	}

	scheduler.refreshing.Store(true)
	scheduler.runConfiguredSiteRefresh()
	if !scheduler.refreshing.Load() {
		t.Fatal("site refresh guard should remain active after skipped run")
	}

	scheduler.checkingIn.Store(true)
	scheduler.runConfiguredNewAPICheckins()
	if !scheduler.checkingIn.Load() {
		t.Fatal("newapi checkin guard should remain active after skipped run")
	}
}

func TestUsageSummaryMaintenanceWithUnavailableServiceReleasesGuard(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, nil, nil, nil)

	scheduler.runUsageSummaryMaintenance()

	if scheduler.summarizing.Load() {
		t.Fatal("usage summary guard should be released when service is unavailable")
	}
}

func TestAutomaticBackupJobSkipsWhenServiceUnavailable(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, nil, nil, nil)

	scheduler.runAutomaticBackup()

	if scheduler.autoBackups != nil {
		t.Fatal("automatic backup service should remain unavailable")
	}
}

// A job that panics must be caught by the cron.Recover wrapper so it neither
// crashes the process nor stops the scheduler from firing subsequent runs.
// Without the recover chain the panic in cron's per-job goroutine would take
// down the whole test binary. (F1 regression.)
func TestScheduledJobPanicIsRecovered(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, nil, nil, nil)

	ran := make(chan struct{}, 1)
	if _, err := scheduler.cron.AddFunc("@every 200ms", func() {
		select {
		case ran <- struct{}{}:
		default:
		}
		panic("scheduled job boom")
	}); err != nil {
		t.Fatalf("add panic job: %v", err)
	}

	scheduler.Start()
	defer scheduler.Stop()

	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("panic job did not run")
	}

	// A second firing proves the scheduler survived the first job's panic and
	// keeps scheduling instead of the process having crashed.
	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler stopped firing after a job panicked")
	}
}

func schedulerTestConfigFile(t *testing.T) *config.ConfigFile {
	t.Helper()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	return confFile
}
