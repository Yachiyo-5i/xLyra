package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"xlyra/server/internal/backup"
	"xlyra/server/internal/catalog"
	"xlyra/server/internal/config"
	"xlyra/server/internal/site"
	"xlyra/server/internal/usage"
)

const (
	usageSummaryCron          = "1 0 * * *"
	modelPricingSyncCron      = "@every 4h"
	automaticBackupJobTimeout = 10 * time.Minute
)

// cronLogger adapts *slog.Logger to the cron.Logger interface so the
// cron.Recover job wrapper can report recovered panics instead of letting an
// unhandled panic in any scheduled job crash the whole process.
type cronLogger struct {
	logger *slog.Logger
}

func (l cronLogger) Info(msg string, keysAndValues ...any) {
	l.logger.Info(msg, keysAndValues...)
}

func (l cronLogger) Error(err error, msg string, keysAndValues ...any) {
	l.logger.Error(msg, append([]any{"error", err}, keysAndValues...)...)
}

type Options struct {
	SiteHealthInterval time.Duration
	SiteHealthTimeout  time.Duration
	SiteHealthWorkers  int
	ConfigFile         *config.ConfigFile
}

type Scheduler struct {
	cron          *cron.Cron
	logger        *slog.Logger
	options       Options
	sites         *site.Service
	sync          *catalog.SyncService
	usageSummary  *usage.SummaryService
	autoBackups   *backup.AutomaticService
	configuredMu  sync.Mutex
	siteRefreshID cron.EntryID
	checkinID     cron.EntryID
	autoBackupID  cron.EntryID
	running       atomic.Bool
	syncing       atomic.Bool
	summarizing   atomic.Bool
	refreshing    atomic.Bool
	checkingIn    atomic.Bool

	modelsCacheInvalidator func()
}

func New(logger *slog.Logger, options Options, siteService *site.Service, syncService *catalog.SyncService, usageSummaryService *usage.SummaryService, autoBackupServices ...*backup.AutomaticService) *Scheduler {
	if options.SiteHealthInterval <= 0 {
		options.SiteHealthInterval = 15 * time.Minute
	}
	if options.SiteHealthTimeout <= 0 {
		options.SiteHealthTimeout = 10 * time.Second
	}
	if options.SiteHealthWorkers <= 0 {
		options.SiteHealthWorkers = 4
	}

	var autoBackups *backup.AutomaticService
	if len(autoBackupServices) > 0 {
		autoBackups = autoBackupServices[0]
	}
	return &Scheduler{
		cron:         cron.New(cron.WithChain(cron.Recover(cronLogger{logger: logger}))),
		logger:       logger,
		options:      options,
		sites:        siteService,
		sync:         syncService,
		usageSummary: usageSummaryService,
		autoBackups:  autoBackups,
	}
}

func (s *Scheduler) WithModelsCacheInvalidator(invalidate func()) *Scheduler {
	s.modelsCacheInvalidator = invalidate
	return s
}

func (s *Scheduler) RegisterDefaultJobs() {
	if s.sites == nil {
		s.logger.Warn("site health scheduler disabled: site service is unavailable")
	} else {
		spec := "@every " + s.options.SiteHealthInterval.String()
		if _, err := s.cron.AddFunc(spec, s.runSiteHealthChecks); err != nil {
			s.logger.Error("register site health scheduler failed", "error", err, "interval", s.options.SiteHealthInterval)
		} else {
			s.logger.Info(
				"site health scheduler registered",
				"interval", s.options.SiteHealthInterval,
				"timeout", s.options.SiteHealthTimeout,
				"workers", s.options.SiteHealthWorkers,
			)
		}
	}

	if s.sync == nil {
		s.logger.Warn("models.dev sync scheduler disabled: sync service is unavailable")
	} else {
		if _, err := s.cron.AddFunc(modelPricingSyncCron, s.runModelsDevSync); err != nil {
			s.logger.Error("register models.dev sync scheduler failed", "error", err)
		} else {
			s.logger.Info("models.dev sync scheduler registered", "interval", modelPricingSyncCron)
		}
	}

	if s.usageSummary == nil {
		s.logger.Warn("usage summary scheduler disabled: usage summary service is unavailable")
	} else {
		if _, err := s.cron.AddFunc(usageSummaryCron, s.runUsageSummaryMaintenance); err != nil {
			s.logger.Error("register usage summary scheduler failed", "error", err)
		} else {
			s.logger.Info("usage summary scheduler registered", "cron", usageSummaryCron)
		}
	}

	s.RegisterConfiguredJobs()
	if s.options.ConfigFile != nil {
		s.options.ConfigFile.OnChange(func(map[string]any) {
			go s.RegisterConfiguredJobs()
		})
	}
}

func (s *Scheduler) RegisterConfiguredJobs() {
	s.configuredMu.Lock()
	defer s.configuredMu.Unlock()

	if s.siteRefreshID != 0 {
		s.cron.Remove(s.siteRefreshID)
		s.siteRefreshID = 0
	}
	if s.checkinID != 0 {
		s.cron.Remove(s.checkinID)
		s.checkinID = 0
	}
	if s.autoBackupID != 0 {
		s.cron.Remove(s.autoBackupID)
		s.autoBackupID = 0
	}

	general := config.ReadGeneralConfig(s.options.ConfigFile)
	if s.sites == nil {
		s.logger.Warn("configured site refresh/checkin scheduler disabled: site service is unavailable")
	} else {
		if id, err := s.cron.AddFunc(general.Tasks.SiteRefreshCron, s.runConfiguredSiteRefresh); err != nil {
			s.logger.Error("register configured site refresh failed", "error", err, "cron", general.Tasks.SiteRefreshCron)
		} else {
			s.siteRefreshID = id
			s.logger.Info("configured site refresh registered", "cron", general.Tasks.SiteRefreshCron)
		}
		if id, err := s.cron.AddFunc(general.Tasks.NewAPICheckinCron, s.runConfiguredNewAPICheckins); err != nil {
			s.logger.Error("register configured newapi checkin failed", "error", err, "cron", general.Tasks.NewAPICheckinCron)
		} else {
			s.checkinID = id
			s.logger.Info("configured newapi checkin registered", "cron", general.Tasks.NewAPICheckinCron)
		}
	}

	if s.autoBackups == nil {
		return
	}
	automatic, ok := config.ReadAutomaticBackupConfig(s.options.ConfigFile)
	if !ok || !automatic.Enabled {
		s.logger.Debug("automatic backup scheduler disabled")
		return
	}
	if !config.AutomaticBackupConfigHasCredentials(automatic) {
		s.logger.Warn("automatic backup scheduler disabled: credentials are incomplete")
		return
	}
	if id, err := s.cron.AddFunc(automatic.Cron, s.runAutomaticBackup); err != nil {
		s.logger.Error("register automatic backup failed", "error", err, "cron", automatic.Cron)
	} else {
		s.autoBackupID = id
		s.logger.Info("automatic backup registered", "cron", automatic.Cron)
	}
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
}

func (s *Scheduler) runSiteHealthChecks() {
	if !s.running.CompareAndSwap(false, true) {
		s.logger.Warn("site health scheduler skipped: previous run still active")
		return
	}
	defer s.running.Store(false)

	start := time.Now()
	if err := s.sites.CleanupExpiredHealthData(context.Background(), start); err != nil {
		s.logger.Warn("site health scheduler cleanup failed", "error", err)
	}

	sites, err := s.sites.List(context.Background())
	if err != nil {
		s.logger.Error("site health scheduler list sites failed", "error", err)
		return
	}

	enabled := make([]uuid.UUID, 0, len(sites))
	for _, item := range sites {
		if item.Enabled {
			enabled = append(enabled, item.ID)
		}
	}

	if len(enabled) == 0 {
		s.logger.Debug("site health scheduler skipped: no enabled sites")
		return
	}

	workers := s.options.SiteHealthWorkers
	if workers > len(enabled) {
		workers = len(enabled)
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, siteID := range enabled {
		siteID := siteID
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					s.logger.Error("site health scheduler worker panic recovered", "site_id", siteID, "panic", r)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), s.options.SiteHealthTimeout)
			defer cancel()

			result, err := s.sites.CheckSiteHealth(ctx, siteID, "scheduler")
			if err != nil {
				s.logger.Warn("site health scheduler check failed", "site_id", siteID, "error", err)
				return
			}

			s.logger.Debug(
				"site health scheduler checked site",
				"site_id", siteID,
				"status", result.State.Status,
				"success", result.Snapshot.Success,
				"latency_ms", result.Snapshot.LatencyMS.Int64,
			)
		}()
	}

	wg.Wait()
	s.logger.Info("site health scheduler finished", "site_count", len(enabled), "duration", time.Since(start))
}

func (s *Scheduler) runModelsDevSync() {
	if !s.syncing.CompareAndSwap(false, true) {
		s.logger.Warn("models.dev sync skipped: previous run still active")
		return
	}
	defer s.syncing.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := s.sync.SyncAll(ctx); err != nil {
		s.logger.Error("models.dev sync failed", "error", err)
		return
	}
	if s.modelsCacheInvalidator != nil {
		s.modelsCacheInvalidator()
	}
}

func (s *Scheduler) runUsageSummaryMaintenance() {
	if !s.summarizing.CompareAndSwap(false, true) {
		s.logger.Warn("usage summary maintenance skipped: previous run still active")
		return
	}
	defer s.summarizing.Store(false)
	if s.usageSummary == nil {
		return
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := s.usageSummary.DailyMaintenance(ctx, start)
	if err != nil {
		s.logger.Error("usage summary maintenance failed", "error", err, "duration", time.Since(start))
		return
	}
	s.logger.Info(
		"usage summary maintenance finished",
		"summarized_days", result.SummarizedDays,
		"deleted_detail_rows", result.DeletedDetailRows,
		"deleted_hourly_rows", result.DeletedHourlyRows,
		"duration", time.Since(start),
	)
}

func (s *Scheduler) runConfiguredSiteRefresh() {
	if !s.refreshing.CompareAndSwap(false, true) {
		s.logger.Warn("configured site refresh skipped: previous run still active")
		return
	}
	defer s.refreshing.Store(false)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	results, err := s.sites.RefreshAllStates(ctx)
	if err != nil {
		s.logger.Error("configured site refresh failed", "error", err)
		return
	}
	successCount := 0
	failureCount := 0
	skippedCount := 0
	for _, item := range results {
		if item.Skipped {
			skippedCount++
			continue
		}
		if item.Err != nil {
			failureCount++
			if site.OAuthPermanentAuthRefreshError(item.Result.Site.SiteType, item.Err.Error()) {
				continue
			}
			s.logger.Warn("configured site refresh item failed", "site_id", item.Result.Site.ID, "site_name", item.Result.Site.Name, "error", item.Err)
			continue
		}
		successCount++
	}
	s.logger.Info("configured site refresh finished", "count", len(results), "success_count", successCount, "failure_count", failureCount, "skipped_count", skippedCount, "duration", time.Since(start))
	if successCount > 0 && s.modelsCacheInvalidator != nil {
		s.modelsCacheInvalidator()
	}
}

func (s *Scheduler) runConfiguredNewAPICheckins() {
	if !s.checkingIn.CompareAndSwap(false, true) {
		s.logger.Warn("configured newapi checkin skipped: previous run still active")
		return
	}
	defer s.checkingIn.Store(false)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	results, err := s.sites.RunNewAPICheckins(ctx)
	if err != nil {
		s.logger.Error("configured newapi checkin failed", "error", err)
		return
	}
	successCount := 0
	skippedCount := 0
	failureCount := 0
	for _, item := range results {
		switch item.Status {
		case "success", "partial":
			successCount++
		case "skipped":
			skippedCount++
		default:
			failureCount++
		}
		if item.Err != nil {
			s.logger.Warn("configured newapi checkin item failed", "site_id", item.SiteID, "site_name", item.SiteName, "error", item.Err)
		} else {
			s.logger.Debug("configured newapi checkin item", "site_id", item.SiteID, "site_name", item.SiteName, "status", item.Status, "message", item.Message)
		}
	}
	s.logger.Info("configured newapi checkin finished", "count", len(results), "success_count", successCount, "skipped_count", skippedCount, "failure_count", failureCount, "duration", time.Since(start))
	if successCount > 0 && s.modelsCacheInvalidator != nil {
		s.modelsCacheInvalidator()
	}
}

func (s *Scheduler) runAutomaticBackup() {
	if s.autoBackups == nil {
		return
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), automaticBackupJobTimeout)
	defer cancel()
	result, err := s.autoBackups.RunScheduled(ctx)
	if err != nil {
		if backup.IsAlreadyRunningError(err) {
			s.logger.Warn("automatic backup skipped: previous run still active")
			return
		}
		s.logger.Error("automatic backup failed", "error", err, "duration", time.Since(start))
		return
	}
	s.logger.Info(
		"automatic backup finished",
		"key", result.File.Key,
		"size", result.File.Size,
		"deleted_count", result.DeletedCount,
		"duration", time.Since(start),
	)
}
