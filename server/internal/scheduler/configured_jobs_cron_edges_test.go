package scheduler

import (
	"testing"

	"xlyra/server/internal/backup"
	"xlyra/server/internal/config"
	"xlyra/server/internal/site"
)

func TestRegisterConfiguredJobsKeepsValidSiteJobWhenOtherCronInvalid(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name            string
		siteRefreshCron string
		checkinCron     string
		wantRefresh     bool
		wantCheckin     bool
	}{
		{
			name:            "site refresh remains when checkin cron invalid",
			siteRefreshCron: "*/10 * * * *",
			checkinCron:     "not a cron",
			wantRefresh:     true,
		},
		{
			name:            "checkin remains when site refresh cron invalid",
			siteRefreshCron: "bad",
			checkinCron:     "15 8 * * *",
			wantCheckin:     true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scheduler := registerConfiguredSiteJobsWithCron(t, tc.siteRefreshCron, tc.checkinCron)

			assertConfiguredSiteJobIDs(t, scheduler, tc.wantRefresh, tc.wantCheckin, 1)
		})
	}
}

func TestRegisterConfiguredJobsSkipsAutomaticBackupWhenConfigMissing(t *testing.T) {
	t.Parallel()

	scheduler := New(
		schedulerDiscardLogger(),
		Options{ConfigFile: schedulerTestConfigFile(t)},
		nil,
		nil,
		nil,
		backup.NewAutomaticService(backup.Service{}, "master-key"),
	)
	scheduler.RegisterConfiguredJobs()

	if scheduler.autoBackupID != 0 {
		t.Fatalf("autoBackupID = %d, want 0 without automatic backup config", scheduler.autoBackupID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("entries = %d, want 0 without automatic backup config", len(entries))
	}
}

func registerConfiguredSiteJobsWithCron(t *testing.T, siteRefreshCron, checkinCron string) *Scheduler {
	t.Helper()

	confFile := schedulerTestConfigFile(t)
	if err := confFile.Set(config.GeneralConfigPath+".tasks.site_refresh_cron", siteRefreshCron); err != nil {
		t.Fatalf("set site refresh cron: %v", err)
	}
	if err := confFile.Set(config.GeneralConfigPath+".tasks.newapi_checkin_cron", checkinCron); err != nil {
		t.Fatalf("set newapi checkin cron: %v", err)
	}

	scheduler := New(schedulerDiscardLogger(), Options{ConfigFile: confFile}, &site.Service{}, nil, nil)
	scheduler.RegisterConfiguredJobs()
	return scheduler
}

func assertConfiguredSiteJobIDs(t *testing.T, scheduler *Scheduler, wantRefresh bool, wantCheckin bool, wantEntries int) {
	t.Helper()

	if (scheduler.siteRefreshID != 0) != wantRefresh {
		t.Fatalf("siteRefreshID = %d, want registered=%v", scheduler.siteRefreshID, wantRefresh)
	}
	if (scheduler.checkinID != 0) != wantCheckin {
		t.Fatalf("checkinID = %d, want registered=%v", scheduler.checkinID, wantCheckin)
	}
	if entries := scheduler.cron.Entries(); len(entries) != wantEntries {
		t.Fatalf("entries = %d, want %d", len(entries), wantEntries)
	}
}
