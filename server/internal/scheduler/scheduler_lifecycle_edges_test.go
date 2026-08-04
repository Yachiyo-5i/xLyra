package scheduler

import (
	"log/slog"
	"testing"
	"time"

	"github.com/robfig/cron/v3"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/site"
	"xlyra/server/internal/usage"
)

func TestRegisterDefaultJobsHandlesCronRegistrationFailures(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, &site.Service{}, &catalog.SyncService{}, &usage.SummaryService{})
	scheduler.cron = cron.New(cron.WithParser(cron.NewParser(0)))

	scheduler.RegisterDefaultJobs()

	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("entries = %d, want 0 when cron parser rejects all job specs", len(entries))
	}
	if scheduler.siteRefreshID != 0 {
		t.Fatalf("siteRefreshID = %d, want 0 when configured site refresh registration fails", scheduler.siteRefreshID)
	}
	if scheduler.checkinID != 0 {
		t.Fatalf("checkinID = %d, want 0 when configured newapi checkin registration fails", scheduler.checkinID)
	}
}

func TestStartStopWithRegisteredJob(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{SiteHealthInterval: time.Hour}, &site.Service{}, nil, nil)
	scheduler.RegisterDefaultJobs()

	if entries := scheduler.cron.Entries(); len(entries) == 0 {
		t.Fatal("expected registered jobs before lifecycle exercise")
	}

	scheduler.Start()
	scheduler.Stop()
}

func TestStartStopWithoutRegisteredJobs(t *testing.T) {
	t.Parallel()

	scheduler := New(slog.Default(), Options{}, nil, nil, nil)

	scheduler.Start()
	scheduler.Stop()
}
