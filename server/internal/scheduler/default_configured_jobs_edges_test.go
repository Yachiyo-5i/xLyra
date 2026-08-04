package scheduler

import (
	"log/slog"
	"testing"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/config"
	"xlyra/server/internal/site"
	"xlyra/server/internal/usage"
)

func TestRegisterDefaultJobsSupportsPartialCoreServices(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		sites     *site.Service
		sync      *catalog.SyncService
		summary   *usage.SummaryService
		wantCount int
		wantSite  bool
	}{
		{
			name:      "models_and_usage_without_sites",
			sync:      &catalog.SyncService{},
			summary:   &usage.SummaryService{},
			wantCount: 2,
		},
		{
			name:      "sites_without_models_or_usage",
			sites:     &site.Service{},
			wantCount: 3,
			wantSite:  true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheduler := New(slog.Default(), Options{}, tc.sites, tc.sync, tc.summary)
			scheduler.RegisterDefaultJobs()

			if entries := scheduler.cron.Entries(); len(entries) != tc.wantCount {
				t.Fatalf("entries = %d, want %d", len(entries), tc.wantCount)
			}
			if tc.wantSite {
				if scheduler.siteRefreshID == 0 || scheduler.checkinID == 0 {
					t.Fatalf("expected configured site jobs, got refresh=%d checkin=%d", scheduler.siteRefreshID, scheduler.checkinID)
				}
				return
			}
			if scheduler.siteRefreshID != 0 || scheduler.checkinID != 0 {
				t.Fatalf("site job IDs = refresh %d checkin %d, want both 0 without site service", scheduler.siteRefreshID, scheduler.checkinID)
			}
		})
	}
}

func TestRegisterConfiguredJobsClearsOldAutomaticBackupBeforeInvalidCron(t *testing.T) {
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

	if err := confFile.Set(config.AutomaticBackupConfigPath+".cron", "not a cron"); err != nil {
		t.Fatalf("set invalid automatic backup cron: %v", err)
	}
	scheduler.RegisterConfiguredJobs()

	if scheduler.autoBackupID != 0 {
		t.Fatalf("autoBackupID = %d, want 0 after invalid cron replacement", scheduler.autoBackupID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("entries = %d, want old automatic backup job removed", len(entries))
	}
}
