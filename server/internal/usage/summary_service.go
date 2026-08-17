package usage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

type SummaryService struct {
	db       *store.Store
	confFile *config.ConfigFile
	timeZone config.TimeZone
	runMu    sync.Mutex
}

type SummaryMaintenanceResult struct {
	SummarizedDays                   int
	DeletedDetailRows                int64
	BackfilledCachedUsageRecords     int64
	RebuiltCachedTokenDays           int
	BackfilledCacheWriteUsageRecords int64
	RebuiltCacheWriteTokenDays       int
	RebuiltHourlyRows                int
	DeletedHourlyRows                int64
}

func NewSummaryService(db *store.Store, confFile *config.ConfigFile, timeZones ...config.TimeZone) *SummaryService {
	return &SummaryService{
		db:       db,
		confFile: confFile,
		timeZone: summaryServiceTimeZone(timeZones...),
	}
}

func (s *SummaryService) StartupCheck(ctx context.Context, now time.Time) (SummaryMaintenanceResult, error) {
	if s != nil {
		s.runMu.Lock()
		defer s.runMu.Unlock()
	}
	result, err := s.ensureSummaries(ctx, now, "startup")
	if err != nil {
		return result, err
	}
	backfill, err := store.NewRequestUsageSummaryRepository(s.db.DB()).BackfillCachedTokens(ctx, now, s.timeZone)
	if err != nil {
		return result, err
	}
	result.BackfilledCachedUsageRecords = backfill.UpdatedUsageRecords
	result.RebuiltCachedTokenDays = backfill.RebuiltDays
	cacheWriteBackfill, err := store.NewRequestUsageSummaryRepository(s.db.DB()).BackfillCacheWriteTokens(ctx, now, s.timeZone)
	if err != nil {
		return result, err
	}
	result.BackfilledCacheWriteUsageRecords = cacheWriteBackfill.UpdatedUsageRecords
	result.RebuiltCacheWriteTokenDays = cacheWriteBackfill.RebuiltDays
	timeZone := config.TimeZoneOrDefault(s.timeZone)
	currentHour := timeZone.StartOfHour(now)
	dayStart := timeZone.StartOfDay(now)
	deleted, err := store.NewRequestUsageSummaryRepository(s.db.DB()).DeleteHourlyBefore(ctx, dayStart, timeZone)
	if err != nil {
		return result, err
	}
	result.DeletedHourlyRows = deleted
	rows, err := store.NewRequestUsageSummaryRepository(s.db.DB()).RebuildHourlyRange(ctx, dayStart, currentHour, timeZone)
	if err != nil {
		return result, err
	}
	result.RebuiltHourlyRows = rows
	return result, nil
}

func (s *SummaryService) DailyMaintenance(ctx context.Context, now time.Time) (SummaryMaintenanceResult, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return SummaryMaintenanceResult{}, fmt.Errorf("usage summary store is not initialized")
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if now.IsZero() {
		now = time.Now()
	}
	repo := store.NewRequestUsageSummaryRepository(s.db.DB())
	dayStart := s.timeZone.StartOfDay(now)
	deletedHourly, err := repo.DeleteHourlyBefore(ctx, dayStart, s.timeZone)
	if err != nil {
		return SummaryMaintenanceResult{}, err
	}
	yesterday := s.timeZone.StartOfDay(now).AddDate(0, 0, -1)
	if _, err := repo.RebuildDay(ctx, yesterday, s.timeZone, "daily"); err != nil {
		return SummaryMaintenanceResult{}, err
	}
	result, err := s.ensureSummaries(ctx, now, "daily")
	if err != nil {
		return SummaryMaintenanceResult{}, err
	}
	result.SummarizedDays++
	result.DeletedHourlyRows = deletedHourly

	general := config.ReadGeneralConfig(s.confFile)
	if !general.Data.RequestDetailCleanupEnabled {
		return result, nil
	}
	cutoff := s.timeZone.StartOfDay(now).AddDate(0, 0, -general.Data.RequestDetailRetentionDays)
	if _, err := repo.EnsureSummariesThrough(ctx, cutoff, s.timeZone, "cleanup"); err != nil {
		return result, err
	}
	deleted, err := repo.DeleteDetailsBefore(ctx, cutoff)
	if err != nil {
		return result, err
	}
	if deleted > 0 {
		if err := repo.MarkDetailsCleaned(ctx, cutoff, s.timeZone); err != nil {
			return result, err
		}
	}
	result.DeletedDetailRows = deleted
	return result, nil
}

func (s *SummaryService) ensureSummaries(ctx context.Context, now time.Time, source string) (SummaryMaintenanceResult, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return SummaryMaintenanceResult{}, fmt.Errorf("usage summary store is not initialized")
	}
	if now.IsZero() {
		now = time.Now()
	}
	repo := store.NewRequestUsageSummaryRepository(s.db.DB())
	count, err := repo.EnsureSummariesThrough(ctx, now, s.timeZone, source)
	if err != nil {
		return SummaryMaintenanceResult{}, err
	}
	return SummaryMaintenanceResult{SummarizedDays: count}, nil
}

func summaryServiceTimeZone(timeZones ...config.TimeZone) config.TimeZone {
	return config.TimeZoneOrDefault(timeZones...)
}
