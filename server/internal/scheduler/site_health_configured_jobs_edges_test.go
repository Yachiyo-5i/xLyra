package scheduler

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestSiteHealthChecksSkipDisabledSitesAndReleaseGuard(t *testing.T) {
	t.Parallel()

	listedSites := []store.Site{
		schedulerSiteHealthSite("disabled", false, time.Now()),
	}
	service := schedulerSiteHealthService(t, listedSites, nil)
	scheduler := New(schedulerDiscardLogger(), Options{SiteHealthWorkers: 10}, service, nil, nil)

	scheduler.runSiteHealthChecks()

	if scheduler.running.Load() {
		t.Fatal("site health guard should be released after no-enabled-sites run")
	}
}

func TestSiteHealthChecksOnlyEnabledSitesAndReleasesGuardAfterSnapshotWriteErrors(t *testing.T) {
	t.Parallel()

	now := time.Now()
	enabledOne := schedulerSiteHealthSite("enabled-1", true, now)
	disabled := schedulerSiteHealthSite("disabled", false, now.Add(-time.Minute))
	enabledTwo := schedulerSiteHealthSite("enabled-2", true, now.Add(-2*time.Minute))
	createErr := errors.New("site health snapshot write stopped")
	var snapshotAttempts atomic.Int32
	service := schedulerSiteHealthService(t, []store.Site{enabledOne, disabled, enabledTwo}, func(tx *gorm.DB) {
		snapshotAttempts.Add(1)
		tx.AddError(createErr)
	})
	scheduler := New(schedulerDiscardLogger(), Options{
		SiteHealthTimeout: 25 * time.Millisecond,
		SiteHealthWorkers: 99,
	}, service, nil, nil)

	scheduler.runSiteHealthChecks()

	if scheduler.running.Load() {
		t.Fatal("site health guard should be released after enabled-site write errors")
	}
	if got := snapshotAttempts.Load(); got != 2 {
		t.Fatalf("snapshot attempts = %d, want one per enabled site", got)
	}
}

func TestRegisterDefaultJobsAcceptsTinyPositiveSiteHealthInterval(t *testing.T) {
	t.Parallel()

	scheduler := New(schedulerDiscardLogger(), Options{
		SiteHealthInterval: time.Nanosecond,
	}, &site.Service{}, nil, nil)

	scheduler.RegisterDefaultJobs()

	if entries := scheduler.cron.Entries(); len(entries) != 4 {
		t.Fatalf("entries = %d, want site health plus configured site refresh and checkin jobs plus codex version refresh", len(entries))
	}
	if scheduler.siteRefreshID == 0 || scheduler.checkinID == 0 {
		t.Fatalf("expected configured site jobs to still register, got refresh=%d checkin=%d", scheduler.siteRefreshID, scheduler.checkinID)
	}
}

func TestRegisterConfiguredJobsClearsSiteJobIDsBeforeInvalidCronReplacement(t *testing.T) {
	t.Parallel()

	confFile := schedulerTestConfigFile(t)
	scheduler := New(schedulerDiscardLogger(), Options{ConfigFile: confFile}, &site.Service{}, nil, nil)
	scheduler.RegisterConfiguredJobs()
	if scheduler.siteRefreshID == 0 || scheduler.checkinID == 0 {
		t.Fatalf("expected initial configured jobs, got refresh=%d checkin=%d", scheduler.siteRefreshID, scheduler.checkinID)
	}

	if err := confFile.Set(config.GeneralConfigPath+".tasks.site_refresh_cron", "bad"); err != nil {
		t.Fatalf("set invalid site refresh cron: %v", err)
	}
	if err := confFile.Set(config.GeneralConfigPath+".tasks.newapi_checkin_cron", "also bad"); err != nil {
		t.Fatalf("set invalid newapi checkin cron: %v", err)
	}

	scheduler.RegisterConfiguredJobs()

	if scheduler.siteRefreshID != 0 || scheduler.checkinID != 0 {
		t.Fatalf("configured site job IDs = refresh %d checkin %d, want both cleared after invalid replacement", scheduler.siteRefreshID, scheduler.checkinID)
	}
	if entries := scheduler.cron.Entries(); len(entries) != 0 {
		t.Fatalf("entries = %d, want none after invalid configured cron replacement", len(entries))
	}
}

func schedulerSiteHealthSite(name string, enabled bool, createdAt time.Time) store.Site {
	return store.Site{
		ID:        uuid.New(),
		Name:      name,
		Slug:      name,
		SiteType:  "unsupported-site-health",
		Enabled:   enabled,
		CreatedAt: createdAt,
	}
}

func schedulerSiteHealthService(t *testing.T, listedSites []store.Site, createCallback func(*gorm.DB)) *site.Service {
	t.Helper()

	siteByID := make(map[uuid.UUID]store.Site, len(listedSites))
	for _, item := range listedSites {
		siteByID[item.ID] = item
	}

	db := schedulerPostgresGorm(t)
	if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.Site:
			*dest = append((*dest)[:0], listedSites...)
			tx.RowsAffected = int64(len(listedSites))
		case *store.Site:
			for _, item := range siteByID {
				*dest = item
				tx.RowsAffected = 1
				return
			}
			tx.AddError(gorm.ErrRecordNotFound)
		default:
			tx.RowsAffected = 0
		}
	}); err != nil {
		t.Fatalf("replace query callback: %v", err)
	}
	if err := db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) {
		tx.RowsAffected = 0
	}); err != nil {
		t.Fatalf("replace delete callback: %v", err)
	}
	if createCallback == nil {
		createCallback = func(tx *gorm.DB) {
			tx.RowsAffected = 1
		}
	}
	if err := db.Callback().Create().Replace("gorm:create", createCallback); err != nil {
		t.Fatalf("replace create callback: %v", err)
	}
	return site.NewService(schedulerStoreWithGorm(t, db), schedulerTestMasterKey)
}
