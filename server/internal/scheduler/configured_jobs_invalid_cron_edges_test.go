package scheduler

import (
	"log/slog"
	"testing"

	"xlyra/server/internal/config"
	"xlyra/server/internal/site"
)

func TestRegisterConfiguredJobsDropsInvalidConfiguredCronJobs(t *testing.T) {
	t.Parallel()

	confFile := schedulerTestConfigFile(t)
	scheduler := New(slog.Default(), Options{ConfigFile: confFile}, &site.Service{}, nil, nil)

	scheduler.RegisterConfiguredJobs()
	if scheduler.siteRefreshID == 0 || scheduler.checkinID == 0 {
		t.Fatalf("expected initial configured jobs, got refresh=%d checkin=%d", scheduler.siteRefreshID, scheduler.checkinID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 2 {
		t.Fatalf("initial entries = %d, want 2", len(entries))
	}

	if err := confFile.Set(config.GeneralConfigPath+".tasks.site_refresh_cron", "bad"); err != nil {
		t.Fatalf("set site refresh cron: %v", err)
	}
	if err := confFile.Set(config.GeneralConfigPath+".tasks.newapi_checkin_cron", "also bad"); err != nil {
		t.Fatalf("set newapi checkin cron: %v", err)
	}

	scheduler.RegisterConfiguredJobs()

	if scheduler.siteRefreshID != 0 {
		t.Fatalf("siteRefreshID = %d, want 0 for invalid cron", scheduler.siteRefreshID)
	}
	if scheduler.checkinID != 0 {
		t.Fatalf("checkinID = %d, want 0 for invalid cron", scheduler.checkinID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("entries after invalid crons = %d, want 0", len(entries))
	}
}
