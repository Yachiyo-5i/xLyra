package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"xlyra/server/internal/config"
)

const (
	RequestUsageSummaryDayStatusComplete = "complete"
	requestUsageSummaryNoneKey           = "none"
	requestUsageSummaryDefaultCurrency   = "USD"
	requestUsageCleanupBatchSize         = 5000
	requestUsageCachedTokensBackfill     = "request_usage_cached_tokens_v1"
	requestUsageCacheWriteTokensBackfill = "request_usage_cache_write_tokens_v1"
)

type RequestUsageDailySummary struct {
	SummaryKey                 string    `gorm:"primaryKey;type:text"`
	BucketStart                time.Time `gorm:"index:request_usage_daily_summaries_bucket_idx;index:request_usage_daily_summaries_site_bucket_idx,priority:2;index:request_usage_daily_summaries_model_bucket_idx,priority:2;index:request_usage_daily_summaries_currency_bucket_idx,priority:2"`
	TimeZone                   string    `gorm:"column:timezone"`
	SiteKey                    string    `gorm:"index:request_usage_daily_summaries_site_bucket_idx,priority:1"`
	SiteID                     uuid.NullUUID
	SiteName                   string
	SiteSlug                   string
	SiteType                   string
	CanonicalModelKey          string `gorm:"index:request_usage_daily_summaries_model_bucket_idx,priority:1"`
	CanonicalModelID           uuid.NullUUID
	SiteModelKey               string
	SiteModelID                uuid.NullUUID
	UpstreamModelName          string
	APIKeyKey                  string
	APIKeyID                   uuid.NullUUID
	APIKeyName                 string
	Endpoint                   string
	StatusCode                 int
	Success                    bool
	Internal                   bool `gorm:"default:false;not null"`
	ErrorType                  string
	Currency                   string `gorm:"index:request_usage_daily_summaries_currency_bucket_idx,priority:1"`
	RequestCount               int64
	SuccessCount               int64
	FailureCount               int64
	PromptTokens               int64
	CompletionTokens           int64
	TotalTokens                int64
	CachedTokens               int64   `gorm:"not null;default:0"`
	CacheWriteTokens           int64   `gorm:"not null;default:0"`
	CacheCreationInputTokens   int64   `gorm:"not null;default:0"`
	CacheCreation5mInputTokens int64   `gorm:"not null;default:0"`
	CacheCreation1hInputTokens int64   `gorm:"not null;default:0"`
	CacheWriteCost             float64 `gorm:"type:numeric(18,8)"`
	EstimatedCost              float64 `gorm:"type:numeric(18,8)"`
	LatencyCount               int64
	LatencyTotalMS             int64
	LatencyMinMS               sql.NullInt64
	LatencyMaxMS               sql.NullInt64
	UpstreamLatencyCount       int64
	UpstreamLatencyTotalMS     int64
	UpstreamLatencyMinMS       sql.NullInt64
	UpstreamLatencyMaxMS       sql.NullInt64
	FirstRequestAt             sql.NullTime
	LastRequestAt              sql.NullTime
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

type RequestUsageSummaryDay struct {
	BucketStart      time.Time `gorm:"primaryKey;index:request_usage_summary_days_status_idx,priority:2"`
	TimeZone         string    `gorm:"primaryKey;column:timezone"`
	Status           string    `gorm:"index:request_usage_summary_days_status_idx,priority:1"`
	Source           string
	RequestCount     int64
	LastSummarizedAt sql.NullTime
	LastCleanedAt    sql.NullTime
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type RequestUsageSummaryRepository struct {
	db *gorm.DB
}

type RequestUsageCachedTokensBackfillResult struct {
	UpdatedUsageRecords int64
	RebuiltDays         int
}

type RequestUsageCacheWriteTokensBackfillResult struct {
	UpdatedUsageRecords int64
	RebuiltDays         int
}

type RequestUsageSummaryIncrement struct {
	RequestLog  RequestLog
	UsageRecord *UsageRecord
	TimeZone    config.TimeZone
}

type RequestUsageSummaryQuery struct {
	TimeZone string
	From     *time.Time
	To       *time.Time
	SiteID   *uuid.UUID
	SiteIDs  []uuid.UUID
	APIKeyID *uuid.UUID
}

type SiteUsageSummaryRow struct {
	SiteID           uuid.UUID
	SiteName         string
	SiteSlug         string
	SiteType         string
	RequestCount     int64
	SuccessCount     int64
	FailedCount      int64
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	EstimatedCost    float64
	Currency         string
	FirstRequestAt   time.Time
	LastRequestAt    time.Time
}

type RequestUsageCostSummaryQuery struct {
	TimeZone        string
	From            *time.Time
	To              *time.Time
	Success         *bool
	SiteID          *uuid.UUID
	APIKeyID        *uuid.UUID
	ModelKey        string
	ErrorType       string
	Endpoint        string
	HideWithoutSite bool
}

type RequestUsageCostSummary struct {
	TotalCost                  float64
	PromptTokens               int64
	CompletionTokens           int64
	TotalTokens                int64
	CachedTokens               int64
	CacheWriteTokens           int64
	CacheCreationInputTokens   int64
	CacheCreation5mInputTokens int64
	CacheCreation1hInputTokens int64
	CacheWriteTotalTokens      int64
	CacheWriteCost             float64
	Currency                   string
	// CostByCurrency holds the exact per-currency totals. TotalCost/Currency
	// expose a single currency-consistent view (the summary currency) rather than
	// summing across currencies.
	CostByCurrency           map[string]float64
	CacheWriteCostByCurrency map[string]float64
}

func NewRequestUsageSummaryRepository(db *gorm.DB) RequestUsageSummaryRepository {
	return RequestUsageSummaryRepository{db: db}
}

func (r RequestUsageSummaryRepository) Increment(ctx context.Context, params RequestUsageSummaryIncrement) error {
	if r.db == nil {
		return fmt.Errorf("request usage summary store is not initialized")
	}
	row, err := r.summaryFromRequestLog(ctx, params.RequestLog, params.UsageRecord, params.TimeZone)
	if err != nil {
		return fmt.Errorf("increment request usage summary: %w", err)
	}
	if err := r.incrementSummary(ctx, row); err != nil {
		return fmt.Errorf("increment request usage summary: %w", err)
	}
	return nil
}

func (r RequestUsageSummaryRepository) RebuildDay(ctx context.Context, day time.Time, timeZone config.TimeZone, source string) (RequestUsageSummaryDay, error) {
	if r.db == nil {
		return RequestUsageSummaryDay{}, fmt.Errorf("request usage summary store is not initialized")
	}
	timeZone = normalizeSummaryTimeZone(timeZone)
	bucketStart := timeZone.StartOfDay(day)
	bucketEnd := bucketStart.AddDate(0, 0, 1)

	logs, err := NewRequestLogRepository(r.db).ListCreatedBetween(ctx, bucketStart, bucketEnd)
	if err != nil {
		return RequestUsageSummaryDay{}, fmt.Errorf("rebuild request usage summary day: %w", err)
	}
	usageByLogID, err := NewRequestLogRepository(r.db).usageByRequestLogID(ctx, requestLogIDs(logs))
	if err != nil {
		return RequestUsageSummaryDay{}, fmt.Errorf("rebuild request usage summary day: %w", err)
	}
	rows, err := r.summariesFromRequestLogs(ctx, logs, usageByLogID, timeZone)
	if err != nil {
		return RequestUsageSummaryDay{}, fmt.Errorf("rebuild request usage summary day: %w", err)
	}

	var dayRecord RequestUsageSummaryDay
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := NewRequestUsageSummaryRepository(tx)
		if err := repo.deleteSummariesForDay(ctx, bucketStart, timeZone.Name); err != nil {
			return err
		}
		if len(rows) > 0 {
			if err := tx.WithContext(ctx).Create(&rows).Error; err != nil {
				return err
			}
		}
		record, err := repo.markDayComplete(ctx, bucketStart, timeZone.Name, source, int64(len(logs)))
		if err != nil {
			return err
		}
		dayRecord = record
		return nil
	})
	if err != nil {
		return RequestUsageSummaryDay{}, fmt.Errorf("rebuild request usage summary day: %w", err)
	}
	return dayRecord, nil
}

func (r RequestUsageSummaryRepository) ListFromDetails(ctx context.Context, start time.Time, end time.Time, timeZone config.TimeZone) ([]RequestUsageDailySummary, error) {
	if r.db == nil {
		return nil, fmt.Errorf("request usage summary store is not initialized")
	}
	if !end.After(start) {
		return nil, nil
	}
	logRepo := NewRequestLogRepository(r.db)
	logs, err := logRepo.ListCreatedBetween(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("list request usage summaries from details: %w", err)
	}
	usageByLogID, err := logRepo.usageByRequestLogID(ctx, requestLogIDs(logs))
	if err != nil {
		return nil, fmt.Errorf("list request usage summaries from details: %w", err)
	}
	rows, err := r.summariesFromRequestLogs(ctx, logs, usageByLogID, timeZone)
	if err != nil {
		return nil, fmt.Errorf("list request usage summaries from details: %w", err)
	}
	return rows, nil
}

func (r RequestUsageSummaryRepository) EnsureSummariesThrough(ctx context.Context, through time.Time, timeZone config.TimeZone, source string) (int, error) {
	if r.db == nil {
		return 0, fmt.Errorf("request usage summary store is not initialized")
	}
	timeZone = normalizeSummaryTimeZone(timeZone)
	oldest, err := NewRequestLogRepository(r.db).OldestCreatedAt(ctx)
	if err != nil {
		return 0, fmt.Errorf("ensure request usage summaries: %w", err)
	}
	if oldest == nil {
		return 0, nil
	}
	start := timeZone.StartOfDay(*oldest)
	end := timeZone.StartOfDay(through)
	if through.Before(end.AddDate(0, 0, 1)) {
		end = end.AddDate(0, 0, -1)
	}
	if end.Before(start) {
		return 0, nil
	}

	count := 0
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		complete, err := r.dayComplete(ctx, day, timeZone.Name)
		if err != nil {
			return count, fmt.Errorf("ensure request usage summaries: %w", err)
		}
		if complete {
			continue
		}
		if _, err := r.RebuildDay(ctx, day, timeZone, source); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func (r RequestUsageSummaryRepository) BackfillCachedTokens(ctx context.Context, through time.Time, timeZone config.TimeZone) (RequestUsageCachedTokensBackfillResult, error) {
	if r.db == nil {
		return RequestUsageCachedTokensBackfillResult{}, fmt.Errorf("request usage summary store is not initialized")
	}
	var marker schemaUpgradeMarker
	err := r.db.WithContext(ctx).Where(&schemaUpgradeMarker{Name: requestUsageCachedTokensBackfill}).First(&marker).Error
	if err == nil {
		return RequestUsageCachedTokensBackfillResult{}, nil
	}
	if err != gorm.ErrRecordNotFound {
		return RequestUsageCachedTokensBackfillResult{}, fmt.Errorf("check request usage cached tokens backfill: %w", err)
	}
	if through.IsZero() {
		through = time.Now()
	}
	timeZone = normalizeSummaryTimeZone(timeZone)
	oldest, err := NewRequestLogRepository(r.db).OldestCreatedAt(ctx)
	if err != nil {
		return RequestUsageCachedTokensBackfillResult{}, fmt.Errorf("find cached tokens backfill start: %w", err)
	}
	result := RequestUsageCachedTokensBackfillResult{}
	if oldest != nil {
		start := timeZone.StartOfDay(*oldest)
		today := timeZone.StartOfDay(through)
		for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
			end := day.AddDate(0, 0, 1)
			if end.After(through) {
				end = through
			}
			updated, requestCount, err := r.backfillCachedTokensBetween(ctx, day, end)
			if err != nil {
				return result, err
			}
			result.UpdatedUsageRecords += updated
			if day.Before(today) && requestCount > 0 {
				complete, err := r.dayHasCompleteDetails(ctx, day, timeZone.Name, requestCount)
				if err != nil {
					return result, err
				}
				if !complete {
					return result, fmt.Errorf("cached tokens backfill summary coverage incomplete for %s", day.Format("2006-01-02"))
				}
				if _, err := r.RebuildDay(ctx, day, timeZone, "cached_tokens_backfill"); err != nil {
					return result, err
				}
				result.RebuiltDays++
			}
		}
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaUpgradeMarker{
		Name:        requestUsageCachedTokensBackfill,
		CompletedAt: time.Now(),
	}).Error; err != nil {
		return result, fmt.Errorf("mark request usage cached tokens backfill complete: %w", err)
	}
	return result, nil
}

func (r RequestUsageSummaryRepository) BackfillCacheWriteTokens(ctx context.Context, through time.Time, timeZone config.TimeZone) (RequestUsageCacheWriteTokensBackfillResult, error) {
	if r.db == nil {
		return RequestUsageCacheWriteTokensBackfillResult{}, fmt.Errorf("request usage summary store is not initialized")
	}
	var marker schemaUpgradeMarker
	err := r.db.WithContext(ctx).Where(&schemaUpgradeMarker{Name: requestUsageCacheWriteTokensBackfill}).First(&marker).Error
	if err == nil {
		return RequestUsageCacheWriteTokensBackfillResult{}, nil
	}
	if err != gorm.ErrRecordNotFound {
		return RequestUsageCacheWriteTokensBackfillResult{}, fmt.Errorf("check request usage cache write tokens backfill: %w", err)
	}
	if through.IsZero() {
		through = time.Now()
	}
	timeZone = normalizeSummaryTimeZone(timeZone)
	oldest, err := NewRequestLogRepository(r.db).OldestCreatedAt(ctx)
	if err != nil {
		return RequestUsageCacheWriteTokensBackfillResult{}, fmt.Errorf("find cache write tokens backfill start: %w", err)
	}
	result := RequestUsageCacheWriteTokensBackfillResult{}
	if oldest != nil {
		start := timeZone.StartOfDay(*oldest)
		today := timeZone.StartOfDay(through)
		for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
			end := day.AddDate(0, 0, 1)
			if end.After(through) {
				end = through
			}
			updated, requestCount, err := r.backfillCacheWriteTokensBetween(ctx, day, end)
			if err != nil {
				return result, err
			}
			result.UpdatedUsageRecords += updated
			if day.Before(today) && requestCount > 0 {
				complete, err := r.dayHasCompleteDetails(ctx, day, timeZone.Name, requestCount)
				if err != nil {
					return result, err
				}
				if !complete {
					return result, fmt.Errorf("cache write tokens backfill summary coverage incomplete for %s", day.Format("2006-01-02"))
				}
				if _, err := r.RebuildDay(ctx, day, timeZone, "cache_write_tokens_backfill"); err != nil {
					return result, err
				}
				result.RebuiltDays++
			}
		}
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&schemaUpgradeMarker{
		Name:        requestUsageCacheWriteTokensBackfill,
		CompletedAt: time.Now(),
	}).Error; err != nil {
		return result, fmt.Errorf("mark request usage cache write tokens backfill complete: %w", err)
	}
	return result, nil
}

func (r RequestUsageSummaryRepository) backfillCachedTokensBetween(ctx context.Context, start time.Time, end time.Time) (int64, int64, error) {
	logs, err := NewRequestLogRepository(r.db).ListCreatedBetween(ctx, start, end)
	if err != nil {
		return 0, 0, fmt.Errorf("list cached tokens backfill details: %w", err)
	}
	usageByLogID, err := NewRequestLogRepository(r.db).usageByRequestLogID(ctx, requestLogIDs(logs))
	if err != nil {
		return 0, 0, fmt.Errorf("list cached tokens backfill usage: %w", err)
	}
	updates := make([]UsageRecord, 0)
	for _, log := range logs {
		usage, ok := usageByLogID[log.ID]
		if !ok || usage.CachedTokens.Valid {
			continue
		}
		cachedTokens, known := requestUsageSummaryCachedTokens(log.Metadata)
		if !known {
			continue
		}
		if cachedTokens > int64(usage.PromptTokens) {
			cachedTokens = int64(usage.PromptTokens)
		}
		usage.CachedTokens = sql.NullInt64{Int64: cachedTokens, Valid: true}
		updates = append(updates, usage)
	}
	if len(updates) == 0 {
		return 0, int64(len(logs)), nil
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index := range updates {
			if err := tx.WithContext(ctx).Save(&updates[index]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, int64(len(logs)), fmt.Errorf("backfill cached tokens usage: %w", err)
	}
	return int64(len(updates)), int64(len(logs)), nil
}

func (r RequestUsageSummaryRepository) backfillCacheWriteTokensBetween(ctx context.Context, start time.Time, end time.Time) (int64, int64, error) {
	logs, err := NewRequestLogRepository(r.db).ListCreatedBetween(ctx, start, end)
	if err != nil {
		return 0, 0, fmt.Errorf("list cache write tokens backfill details: %w", err)
	}
	usageByLogID, err := NewRequestLogRepository(r.db).usageByRequestLogID(ctx, requestLogIDs(logs))
	if err != nil {
		return 0, 0, fmt.Errorf("list cache write tokens backfill usage: %w", err)
	}
	updates := make([]UsageRecord, 0)
	for _, log := range logs {
		usage, ok := usageByLogID[log.ID]
		if !ok {
			continue
		}
		cacheWriteTokens, known := requestUsageSummaryCacheWriteTokens(log.Metadata)
		if !known {
			continue
		}
		changed := false
		if !usage.CacheWriteTokens.Valid && cacheWriteTokens.CacheWriteTokens.Valid {
			usage.CacheWriteTokens = cacheWriteTokens.CacheWriteTokens
			changed = true
		}
		if !usage.CacheCreationInputTokens.Valid && cacheWriteTokens.CacheCreationInputTokens.Valid {
			usage.CacheCreationInputTokens = cacheWriteTokens.CacheCreationInputTokens
			changed = true
		}
		if !usage.CacheCreation5mInputTokens.Valid && cacheWriteTokens.CacheCreation5mInputTokens.Valid {
			usage.CacheCreation5mInputTokens = cacheWriteTokens.CacheCreation5mInputTokens
			changed = true
		}
		if !usage.CacheCreation1hInputTokens.Valid && cacheWriteTokens.CacheCreation1hInputTokens.Valid {
			usage.CacheCreation1hInputTokens = cacheWriteTokens.CacheCreation1hInputTokens
			changed = true
		}
		if changed {
			updates = append(updates, usage)
		}
	}
	if len(updates) == 0 {
		return 0, int64(len(logs)), nil
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for index := range updates {
			if err := tx.WithContext(ctx).Save(&updates[index]).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, int64(len(logs)), fmt.Errorf("backfill cache write tokens usage: %w", err)
	}
	return int64(len(updates)), int64(len(logs)), nil
}

func (r RequestUsageSummaryRepository) dayHasCompleteDetails(ctx context.Context, day time.Time, timeZone string, requestCount int64) (bool, error) {
	var summaryDay RequestUsageSummaryDay
	err := r.db.WithContext(ctx).Where(&RequestUsageSummaryDay{BucketStart: day, TimeZone: timeZone}).First(&summaryDay).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load cached tokens summary coverage: %w", err)
	}
	return summaryDay.Status == RequestUsageSummaryDayStatusComplete && summaryDay.RequestCount == requestCount, nil
}

func (r RequestUsageSummaryRepository) List(ctx context.Context, query RequestUsageSummaryQuery) ([]RequestUsageDailySummary, error) {
	if r.db == nil {
		return nil, fmt.Errorf("request usage summary store is not initialized")
	}
	exprs := make([]clause.Expression, 0, 4)
	if strings.TrimSpace(query.TimeZone) != "" {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "timezone"}, Value: strings.TrimSpace(query.TimeZone)})
	}
	if query.From != nil {
		exprs = append(exprs, clause.Gte{Column: clause.Column{Name: "bucket_start"}, Value: *query.From})
	}
	if query.To != nil {
		exprs = append(exprs, clause.Lt{Column: clause.Column{Name: "bucket_start"}, Value: *query.To})
	}
	if query.SiteID != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "site_id"}, Value: *query.SiteID})
	}
	if len(query.SiteIDs) > 0 {
		values := make([]any, 0, len(query.SiteIDs))
		for _, siteID := range query.SiteIDs {
			values = append(values, siteID)
		}
		exprs = append(exprs, clause.IN{Column: clause.Column{Name: "site_id"}, Values: values})
	}
	if query.APIKeyID != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "api_key_id"}, Value: *query.APIKeyID})
	}

	db := r.db.WithContext(ctx)
	if len(exprs) > 0 {
		db = db.Clauses(clause.Where{Exprs: exprs})
	}
	var rows []RequestUsageDailySummary
	if err := db.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list request usage summaries: %w", err)
	}
	return rows, nil
}

func (r RequestUsageSummaryRepository) SummarizeBySite(ctx context.Context, query RequestUsageSummaryQuery) ([]SiteUsageSummaryRow, error) {
	rows, err := r.List(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("summarize usage by site: %w", err)
	}
	return summarizeRequestUsageBySiteRows(rows), nil
}

func (r RequestUsageSummaryRepository) SiteIDsWithUsage(ctx context.Context, siteIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	result := map[uuid.UUID]bool{}
	if len(siteIDs) == 0 {
		return result, nil
	}
	rows, err := r.List(ctx, RequestUsageSummaryQuery{SiteIDs: siteIDs})
	if err != nil {
		return nil, fmt.Errorf("list request usage summary site ids: %w", err)
	}
	for _, row := range rows {
		if row.SiteID.Valid {
			result[row.SiteID.UUID] = true
		}
	}
	return result, nil
}

func summarizeRequestUsageBySiteRows(rows []RequestUsageDailySummary) []SiteUsageSummaryRow {
	rowsByID := map[uuid.UUID]*SiteUsageSummaryRow{}
	for _, row := range rows {
		if !row.SiteID.Valid {
			continue
		}
		siteID := row.SiteID.UUID
		summary := rowsByID[siteID]
		if summary == nil {
			summary = &SiteUsageSummaryRow{
				SiteID:   siteID,
				SiteName: row.SiteName,
				SiteSlug: row.SiteSlug,
				SiteType: row.SiteType,
				Currency: stringDefault(row.Currency, requestUsageSummaryDefaultCurrency),
			}
			rowsByID[siteID] = summary
		}
		summary.RequestCount += row.RequestCount
		summary.SuccessCount += row.SuccessCount
		summary.FailedCount += row.FailureCount
		summary.PromptTokens += row.PromptTokens
		summary.CompletionTokens += row.CompletionTokens
		summary.TotalTokens += row.TotalTokens
		summary.EstimatedCost += row.EstimatedCost
		if summary.Currency == "" || summary.Currency == requestUsageSummaryDefaultCurrency {
			summary.Currency = stringDefault(row.Currency, requestUsageSummaryDefaultCurrency)
		}
		if row.FirstRequestAt.Valid && (summary.FirstRequestAt.IsZero() || row.FirstRequestAt.Time.Before(summary.FirstRequestAt)) {
			summary.FirstRequestAt = row.FirstRequestAt.Time
		}
		if row.LastRequestAt.Valid && (summary.LastRequestAt.IsZero() || row.LastRequestAt.Time.After(summary.LastRequestAt)) {
			summary.LastRequestAt = row.LastRequestAt.Time
		}
	}

	result := make([]SiteUsageSummaryRow, 0, len(rowsByID))
	for _, row := range rowsByID {
		result = append(result, *row)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].EstimatedCost > result[j].EstimatedCost
	})
	return result
}

func (r RequestUsageSummaryRepository) CostSummary(ctx context.Context, query RequestUsageCostSummaryQuery) (RequestUsageCostSummary, error) {
	if r.db == nil {
		return RequestUsageCostSummary{}, fmt.Errorf("request usage summary store is not initialized")
	}
	exprs := make([]clause.Expression, 0, 10)
	if strings.TrimSpace(query.TimeZone) != "" {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "timezone"}, Value: strings.TrimSpace(query.TimeZone)})
	}
	if query.From != nil {
		exprs = append(exprs, clause.Gte{Column: clause.Column{Name: "bucket_start"}, Value: *query.From})
	}
	if query.To != nil {
		exprs = append(exprs, clause.Lt{Column: clause.Column{Name: "bucket_start"}, Value: *query.To})
	}
	if query.Success != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "success"}, Value: *query.Success})
	}
	if query.SiteID != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "site_id"}, Value: *query.SiteID})
	}
	if query.APIKeyID != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "api_key_id"}, Value: *query.APIKeyID})
	}
	if value := strings.TrimSpace(query.Endpoint); value != "" {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "endpoint"}, Value: value})
	}
	if value := strings.TrimSpace(query.ErrorType); value != "" {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "error_type"}, Value: value})
	}
	if query.HideWithoutSite {
		exprs = append(exprs, clause.Neq{Column: clause.Column{Name: "site_id"}, Value: nil})
	}

	db := r.db.WithContext(ctx)
	if len(exprs) > 0 {
		db = db.Clauses(clause.Where{Exprs: exprs})
	}
	var rows []RequestUsageDailySummary
	if err := db.Find(&rows).Error; err != nil {
		return RequestUsageCostSummary{}, fmt.Errorf("request usage cost summary: %w", err)
	}

	result := RequestUsageCostSummary{Currency: requestUsageSummaryDefaultCurrency}
	for _, row := range rows {
		if !requestUsageSummaryRowMatchesCostFilter(row, query) {
			continue
		}
		result.AddFloat(row.EstimatedCost, row.Currency)
		result.AddCacheWriteCost(sql.NullFloat64{Float64: row.CacheWriteCost, Valid: true}, row.Currency)
		result.AddUsage(row.PromptTokens, row.CompletionTokens, row.TotalTokens, row.CachedTokens, row.CacheWriteTokens, row.CacheCreationInputTokens, row.CacheCreation5mInputTokens, row.CacheCreation1hInputTokens)
	}
	return result, nil
}

func (r RequestUsageSummaryRepository) DeleteDetailsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if r.db == nil {
		return 0, fmt.Errorf("request usage summary store is not initialized")
	}
	var deleted int64
	for {
		count, err := NewRequestLogRepository(r.db).DeleteBefore(ctx, cutoff, requestUsageCleanupBatchSize)
		if err != nil {
			return deleted, fmt.Errorf("delete old request details: %w", err)
		}
		deleted += count
		if count == 0 {
			return deleted, nil
		}
	}
}

func (r RequestUsageSummaryRepository) MarkDetailsCleaned(ctx context.Context, cutoff time.Time, timeZone config.TimeZone) error {
	timeZone = normalizeSummaryTimeZone(timeZone)
	now := time.Now()
	days, err := r.listCompleteDaysBefore(ctx, cutoff, timeZone.Name)
	if err != nil {
		return fmt.Errorf("mark request details cleaned: %w", err)
	}
	for _, day := range days {
		day.LastCleanedAt = sql.NullTime{Time: now, Valid: true}
		if err := r.db.WithContext(ctx).Save(&day).Error; err != nil {
			return fmt.Errorf("mark request details cleaned: %w", err)
		}
	}
	return nil
}

func (r RequestUsageSummaryRepository) incrementSummary(ctx context.Context, row RequestUsageDailySummary) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := NewRequestUsageSummaryRepository(tx)
		if err := repo.incrementExistingSummary(ctx, row); err == nil {
			return nil
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		if err := tx.WithContext(ctx).Create(&row).Error; err == nil {
			return nil
		}
		return repo.incrementExistingSummary(ctx, row)
	})
}

func (r RequestUsageSummaryRepository) incrementExistingSummary(ctx context.Context, row RequestUsageDailySummary) error {
	var existing RequestUsageDailySummary
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}).
		Where(&RequestUsageDailySummary{SummaryKey: row.SummaryKey}).
		First(&existing).Error
	if err != nil {
		return err
	}
	addSummaryValues(&existing, row)
	if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
		return err
	}
	return nil
}

func (r RequestUsageSummaryRepository) deleteSummariesForDay(ctx context.Context, bucketStart time.Time, timeZone string) error {
	if err := r.db.WithContext(ctx).
		Where(&RequestUsageDailySummary{BucketStart: bucketStart, TimeZone: timeZone}).
		Delete(&RequestUsageDailySummary{}).Error; err != nil {
		return fmt.Errorf("delete request usage summaries for day: %w", err)
	}
	return nil
}

func (r RequestUsageSummaryRepository) markDayComplete(ctx context.Context, bucketStart time.Time, timeZone string, source string, requestCount int64) (RequestUsageSummaryDay, error) {
	now := time.Now()
	var day RequestUsageSummaryDay
	err := r.db.WithContext(ctx).Where(&RequestUsageSummaryDay{BucketStart: bucketStart, TimeZone: timeZone}).First(&day).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return RequestUsageSummaryDay{}, err
	}
	day.BucketStart = bucketStart
	day.TimeZone = timeZone
	day.Status = RequestUsageSummaryDayStatusComplete
	day.Source = source
	day.RequestCount = requestCount
	day.LastSummarizedAt = sql.NullTime{Time: now, Valid: true}
	if err == gorm.ErrRecordNotFound {
		if err := r.db.WithContext(ctx).Create(&day).Error; err != nil {
			return RequestUsageSummaryDay{}, err
		}
		return day, nil
	}
	if err := r.db.WithContext(ctx).Save(&day).Error; err != nil {
		return RequestUsageSummaryDay{}, err
	}
	return day, nil
}

func (r RequestUsageSummaryRepository) dayComplete(ctx context.Context, bucketStart time.Time, timeZone string) (bool, error) {
	var day RequestUsageSummaryDay
	err := r.db.WithContext(ctx).Where(&RequestUsageSummaryDay{BucketStart: bucketStart, TimeZone: timeZone}).First(&day).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return day.Status == RequestUsageSummaryDayStatusComplete, nil
}

func (r RequestUsageSummaryRepository) listCompleteDaysBefore(ctx context.Context, cutoff time.Time, timeZone string) ([]RequestUsageSummaryDay, error) {
	var days []RequestUsageSummaryDay
	if err := r.db.WithContext(ctx).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "timezone"}, Value: timeZone},
			clause.Eq{Column: clause.Column{Name: "status"}, Value: RequestUsageSummaryDayStatusComplete},
			clause.Lt{Column: clause.Column{Name: "bucket_start"}, Value: cutoff},
		}}).
		Find(&days).Error; err != nil {
		return nil, err
	}
	return days, nil
}

func (r RequestUsageSummaryRepository) summariesFromRequestLogs(ctx context.Context, logs []RequestLog, usageByLogID map[uuid.UUID]UsageRecord, timeZone config.TimeZone) ([]RequestUsageDailySummary, error) {
	summaryContext, err := r.summaryContext(ctx, logs)
	if err != nil {
		return nil, err
	}
	byKey := map[string]*RequestUsageDailySummary{}
	for _, log := range logs {
		usage := usageByLogID[log.ID]
		var usagePtr *UsageRecord
		if usage.ID != uuid.Nil {
			usagePtr = &usage
		}
		row := summaryFromRequestLog(log, usagePtr, timeZone, summaryContext)
		existing := byKey[row.SummaryKey]
		if existing == nil {
			row.RequestCount = 0
			row.SuccessCount = 0
			row.FailureCount = 0
			row.PromptTokens = 0
			row.CompletionTokens = 0
			row.TotalTokens = 0
			row.CachedTokens = 0
			row.CacheWriteTokens = 0
			row.CacheCreationInputTokens = 0
			row.CacheCreation5mInputTokens = 0
			row.CacheCreation1hInputTokens = 0
			row.CacheWriteCost = 0
			row.EstimatedCost = 0
			row.LatencyCount = 0
			row.LatencyTotalMS = 0
			row.LatencyMinMS = sql.NullInt64{}
			row.LatencyMaxMS = sql.NullInt64{}
			row.UpstreamLatencyCount = 0
			row.UpstreamLatencyTotalMS = 0
			row.UpstreamLatencyMinMS = sql.NullInt64{}
			row.UpstreamLatencyMaxMS = sql.NullInt64{}
			row.FirstRequestAt = sql.NullTime{}
			row.LastRequestAt = sql.NullTime{}
			byKey[row.SummaryKey] = &row
			existing = &row
		}
		addSummaryValues(existing, summaryFromRequestLog(log, usagePtr, timeZone, summaryContext))
		byKey[row.SummaryKey] = existing
	}
	rows := make([]RequestUsageDailySummary, 0, len(byKey))
	for _, row := range byKey {
		rows = append(rows, *row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].SummaryKey < rows[j].SummaryKey
	})
	return rows, nil
}

func requestUsageSummaryRowMatchesCostFilter(row RequestUsageDailySummary, query RequestUsageCostSummaryQuery) bool {
	if value := strings.TrimSpace(query.ModelKey); value != "" {
		needle := strings.ToLower(value)
		return strings.Contains(strings.ToLower(row.CanonicalModelKey), needle) ||
			strings.Contains(strings.ToLower(row.UpstreamModelName), needle)
	}
	return true
}

func (s *RequestUsageCostSummary) AddCost(value sql.NullFloat64, currency string) {
	if !value.Valid {
		return
	}
	s.AddFloat(value.Float64, currency)
}

func (s *RequestUsageCostSummary) AddUsage(promptTokens int64, completionTokens int64, totalTokens int64, cachedTokens int64, cacheWriteTokens int64, cacheCreationInputTokens int64, cacheCreation5mInputTokens int64, cacheCreation1hInputTokens int64) {
	s.PromptTokens += promptTokens
	s.CompletionTokens += completionTokens
	s.TotalTokens += totalTokens
	s.CachedTokens += cachedTokens
	s.CacheWriteTokens += cacheWriteTokens
	s.CacheCreationInputTokens += cacheCreationInputTokens
	s.CacheCreation5mInputTokens += cacheCreation5mInputTokens
	s.CacheCreation1hInputTokens += cacheCreation1hInputTokens
	s.CacheWriteTotalTokens += cacheWriteTokens + cacheCreationTotalTokens(cacheCreationInputTokens, cacheCreation5mInputTokens, cacheCreation1hInputTokens)
}

func (s *RequestUsageCostSummary) AddCacheWriteCost(value sql.NullFloat64, currency string) {
	if !value.Valid {
		return
	}
	cur := stringDefault(strings.TrimSpace(currency), requestUsageSummaryDefaultCurrency)
	if s.CacheWriteCostByCurrency == nil {
		s.CacheWriteCostByCurrency = map[string]float64{}
	}
	s.CacheWriteCostByCurrency[cur] += value.Float64
	if strings.TrimSpace(s.Currency) == "" || s.Currency == requestUsageSummaryDefaultCurrency {
		s.Currency = cur
	}
	s.CacheWriteCost = s.CacheWriteCostByCurrency[s.Currency]
}

func (s *RequestUsageCostSummary) AddFloat(value float64, currency string) {
	cur := stringDefault(strings.TrimSpace(currency), requestUsageSummaryDefaultCurrency)
	if s.CostByCurrency == nil {
		s.CostByCurrency = map[string]float64{}
	}
	s.CostByCurrency[cur] += value
	// Prefer the first non-default currency as the summary currency (as before),
	// then report only that currency's total so the summary stays
	// currency-consistent instead of adding USD+EUR+… into one meaningless number.
	if strings.TrimSpace(s.Currency) == "" || s.Currency == requestUsageSummaryDefaultCurrency {
		s.Currency = cur
	}
	s.TotalCost = s.CostByCurrency[s.Currency]
	s.CacheWriteCost = s.CacheWriteCostByCurrency[s.Currency]
}

func cacheCreationTotalTokens(cacheCreationInputTokens int64, cacheCreation5mInputTokens int64, cacheCreation1hInputTokens int64) int64 {
	if cacheCreation5mInputTokens > 0 || cacheCreation1hInputTokens > 0 {
		return cacheCreation5mInputTokens + cacheCreation1hInputTokens
	}
	return cacheCreationInputTokens
}

func (r RequestUsageSummaryRepository) summaryFromRequestLog(ctx context.Context, log RequestLog, usage *UsageRecord, timeZone config.TimeZone) (RequestUsageDailySummary, error) {
	summaryContext, err := r.summaryContext(ctx, []RequestLog{log})
	if err != nil {
		return RequestUsageDailySummary{}, err
	}
	return summaryFromRequestLog(log, usage, timeZone, summaryContext), nil
}

type requestUsageSummaryContext struct {
	Sites           map[uuid.UUID]Site
	CanonicalModels map[uuid.UUID]CanonicalModel
	SiteModels      map[uuid.UUID]SiteModel
	APIKeys         map[uuid.UUID]APIKey
}

func (r RequestUsageSummaryRepository) summaryContext(ctx context.Context, logs []RequestLog) (requestUsageSummaryContext, error) {
	siteIDs := make([]uuid.UUID, 0)
	canonicalModelIDs := make([]uuid.UUID, 0)
	siteModelIDs := make([]uuid.UUID, 0)
	apiKeyIDs := make([]uuid.UUID, 0)
	for _, log := range logs {
		if log.SiteID.Valid {
			siteIDs = appendUUIDOnce(siteIDs, log.SiteID.UUID)
		} else if metadataSiteID := requestUsageSummaryMetadataUUID(log.Metadata, "site_id"); metadataSiteID.Valid {
			siteIDs = appendUUIDOnce(siteIDs, metadataSiteID.UUID)
		}
		if log.CanonicalModelID.Valid {
			canonicalModelIDs = appendUUIDOnce(canonicalModelIDs, log.CanonicalModelID.UUID)
		}
		if log.SiteModelID.Valid {
			siteModelIDs = appendUUIDOnce(siteModelIDs, log.SiteModelID.UUID)
		}
		if log.APIKeyID.Valid {
			apiKeyIDs = appendUUIDOnce(apiKeyIDs, log.APIKeyID.UUID)
		}
	}

	result := requestUsageSummaryContext{
		Sites:           map[uuid.UUID]Site{},
		CanonicalModels: map[uuid.UUID]CanonicalModel{},
		SiteModels:      map[uuid.UUID]SiteModel{},
		APIKeys:         map[uuid.UUID]APIKey{},
	}
	if len(siteIDs) > 0 {
		var sites []Site
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": siteIDs}).Find(&sites).Error; err != nil {
			return result, err
		}
		for _, site := range sites {
			result.Sites[site.ID] = site
		}
	}
	if len(canonicalModelIDs) > 0 {
		var models []CanonicalModel
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": canonicalModelIDs}).Find(&models).Error; err != nil {
			return result, err
		}
		for _, model := range models {
			result.CanonicalModels[model.ID] = model
		}
	}
	if len(siteModelIDs) > 0 {
		var models []SiteModel
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": siteModelIDs}).Find(&models).Error; err != nil {
			return result, err
		}
		for _, model := range models {
			result.SiteModels[model.ID] = model
		}
	}
	if len(apiKeyIDs) > 0 {
		var apiKeys []APIKey
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": apiKeyIDs}).Find(&apiKeys).Error; err != nil {
			return result, err
		}
		for _, apiKey := range apiKeys {
			result.APIKeys[apiKey.ID] = apiKey
		}
	}
	return result, nil
}

func summaryFromRequestLog(log RequestLog, usage *UsageRecord, timeZone config.TimeZone, context requestUsageSummaryContext) RequestUsageDailySummary {
	timeZone = normalizeSummaryTimeZone(timeZone)
	bucketStart := timeZone.StartOfDay(log.CreatedAt)
	currency := requestUsageSummaryDefaultCurrency
	promptTokens := int64(0)
	completionTokens := int64(0)
	totalTokens := int64(0)
	cachedTokens := int64(0)
	cacheWriteTokens := int64(0)
	cacheCreationInputTokens := int64(0)
	cacheCreation5mInputTokens := int64(0)
	cacheCreation1hInputTokens := int64(0)
	cacheWriteCost := float64(0)
	estimatedCost := float64(0)
	if usage != nil {
		currency = stringDefault(usage.Currency, requestUsageSummaryDefaultCurrency)
		promptTokens = int64(usage.PromptTokens)
		completionTokens = int64(usage.CompletionTokens)
		totalTokens = int64(usage.TotalTokens)
		if usage.CachedTokens.Valid {
			cachedTokens = usage.CachedTokens.Int64
		}
		if usage.CacheWriteTokens.Valid {
			cacheWriteTokens = usage.CacheWriteTokens.Int64
		}
		if usage.CacheCreationInputTokens.Valid {
			cacheCreationInputTokens = usage.CacheCreationInputTokens.Int64
		}
		if usage.CacheCreation5mInputTokens.Valid {
			cacheCreation5mInputTokens = usage.CacheCreation5mInputTokens.Int64
		}
		if usage.CacheCreation1hInputTokens.Valid {
			cacheCreation1hInputTokens = usage.CacheCreation1hInputTokens.Int64
		}
		if usage.CacheWriteCost.Valid {
			cacheWriteCost = usage.CacheWriteCost.Float64
		}
		if usage.EstimatedCost.Valid {
			estimatedCost = usage.EstimatedCost.Float64
		}
	} else {
		if log.RequestTokens.Valid {
			promptTokens = log.RequestTokens.Int64
		}
		if log.ResponseTokens.Valid {
			completionTokens = log.ResponseTokens.Int64
		}
		totalTokens = promptTokens + completionTokens
	}

	row := RequestUsageDailySummary{
		BucketStart:                bucketStart,
		TimeZone:                   timeZone.Name,
		SiteKey:                    nullableUUIDSummaryKey(log.SiteID),
		CanonicalModelKey:          nullableUUIDSummaryKey(log.CanonicalModelID),
		SiteModelKey:               nullableUUIDSummaryKey(log.SiteModelID),
		APIKeyKey:                  nullableUUIDSummaryKey(log.APIKeyID),
		Endpoint:                   stringDefault(strings.TrimSpace(log.Endpoint), requestUsageSummaryNoneKey),
		StatusCode:                 log.StatusCode,
		Success:                    log.Success,
		Internal:                   log.Internal,
		ErrorType:                  requestUsageSummaryErrorType(log),
		Currency:                   currency,
		RequestCount:               1,
		PromptTokens:               promptTokens,
		CompletionTokens:           completionTokens,
		TotalTokens:                totalTokens,
		CachedTokens:               cachedTokens,
		CacheWriteTokens:           cacheWriteTokens,
		CacheCreationInputTokens:   cacheCreationInputTokens,
		CacheCreation5mInputTokens: cacheCreation5mInputTokens,
		CacheCreation1hInputTokens: cacheCreation1hInputTokens,
		CacheWriteCost:             cacheWriteCost,
		EstimatedCost:              estimatedCost,
		FirstRequestAt:             sql.NullTime{Time: log.CreatedAt, Valid: true},
		LastRequestAt:              sql.NullTime{Time: log.CreatedAt, Valid: true},
	}
	if log.Success {
		row.SuccessCount = 1
	} else {
		row.FailureCount = 1
	}
	metadata := requestUsageSummaryMetadata(log.Metadata)
	summarySiteID := log.SiteID
	if !summarySiteID.Valid {
		summarySiteID = requestUsageSummaryMetadataUUIDFromMap(metadata, "site_id")
	}
	if summarySiteID.Valid {
		row.SiteID = summarySiteID
		row.SiteKey = summarySiteID.UUID.String()
		if site, ok := context.Sites[summarySiteID.UUID]; ok && site.ID != uuid.Nil {
			row.SiteName = site.Name
			row.SiteSlug = site.Slug
			row.SiteType = site.SiteType
		} else {
			row.SiteName = requestUsageSummaryMetadataString(metadata, "site_name")
			row.SiteSlug = requestUsageSummaryMetadataString(metadata, "site_slug")
			row.SiteType = requestUsageSummaryMetadataString(metadata, "site_type")
		}
	} else {
		row.SiteName = requestUsageSummaryMetadataString(metadata, "site_name")
		row.SiteSlug = requestUsageSummaryMetadataString(metadata, "site_slug")
		row.SiteType = requestUsageSummaryMetadataString(metadata, "site_type")
		row.SiteKey = stringDefault(stringDefault(row.SiteName, row.SiteSlug), requestUsageSummaryNoneKey)
	}
	if log.CanonicalModelID.Valid {
		row.CanonicalModelID = log.CanonicalModelID
		if model, ok := context.CanonicalModels[log.CanonicalModelID.UUID]; ok && model.ModelKey != "" {
			row.CanonicalModelKey = model.ModelKey
		}
	}
	if log.SiteModelID.Valid {
		row.SiteModelID = log.SiteModelID
		if model, ok := context.SiteModels[log.SiteModelID.UUID]; ok {
			row.UpstreamModelName = model.UpstreamName
		}
	}
	if log.APIKeyID.Valid {
		row.APIKeyID = log.APIKeyID
		if apiKey, ok := context.APIKeys[log.APIKeyID.UUID]; ok {
			row.APIKeyName = apiKey.Name
		}
	}
	if log.LatencyMS.Valid {
		row.LatencyCount = 1
		row.LatencyTotalMS = log.LatencyMS.Int64
		row.LatencyMinMS = log.LatencyMS
		row.LatencyMaxMS = log.LatencyMS
	}
	if log.UpstreamLatencyMS.Valid {
		row.UpstreamLatencyCount = 1
		row.UpstreamLatencyTotalMS = log.UpstreamLatencyMS.Int64
		row.UpstreamLatencyMinMS = log.UpstreamLatencyMS
		row.UpstreamLatencyMaxMS = log.UpstreamLatencyMS
	}
	row.SummaryKey = requestUsageSummaryKey(row)
	return row
}

func addSummaryValues(existing *RequestUsageDailySummary, delta RequestUsageDailySummary) {
	existing.RequestCount += delta.RequestCount
	existing.SuccessCount += delta.SuccessCount
	existing.FailureCount += delta.FailureCount
	existing.PromptTokens += delta.PromptTokens
	existing.CompletionTokens += delta.CompletionTokens
	existing.TotalTokens += delta.TotalTokens
	existing.CachedTokens += delta.CachedTokens
	existing.CacheWriteTokens += delta.CacheWriteTokens
	existing.CacheCreationInputTokens += delta.CacheCreationInputTokens
	existing.CacheCreation5mInputTokens += delta.CacheCreation5mInputTokens
	existing.CacheCreation1hInputTokens += delta.CacheCreation1hInputTokens
	existing.CacheWriteCost += delta.CacheWriteCost
	existing.EstimatedCost += delta.EstimatedCost
	existing.LatencyCount += delta.LatencyCount
	existing.LatencyTotalMS += delta.LatencyTotalMS
	existing.LatencyMinMS = minNullableInt(existing.LatencyMinMS, delta.LatencyMinMS)
	existing.LatencyMaxMS = maxNullableInt(existing.LatencyMaxMS, delta.LatencyMaxMS)
	existing.UpstreamLatencyCount += delta.UpstreamLatencyCount
	existing.UpstreamLatencyTotalMS += delta.UpstreamLatencyTotalMS
	existing.UpstreamLatencyMinMS = minNullableInt(existing.UpstreamLatencyMinMS, delta.UpstreamLatencyMinMS)
	existing.UpstreamLatencyMaxMS = maxNullableInt(existing.UpstreamLatencyMaxMS, delta.UpstreamLatencyMaxMS)
	existing.FirstRequestAt = minNullableTime(existing.FirstRequestAt, delta.FirstRequestAt)
	existing.LastRequestAt = maxNullableTime(existing.LastRequestAt, delta.LastRequestAt)
	if existing.SiteName == "" {
		existing.SiteName = delta.SiteName
	}
	if existing.SiteSlug == "" {
		existing.SiteSlug = delta.SiteSlug
	}
	if existing.SiteType == "" {
		existing.SiteType = delta.SiteType
	}
	if existing.UpstreamModelName == "" {
		existing.UpstreamModelName = delta.UpstreamModelName
	}
	if existing.APIKeyName == "" {
		existing.APIKeyName = delta.APIKeyName
	}
}

func requestUsageSummaryKey(row RequestUsageDailySummary) string {
	parts := []string{
		row.BucketStart.UTC().Format(time.RFC3339),
		row.TimeZone,
		row.SiteKey,
		row.CanonicalModelKey,
		row.SiteModelKey,
		row.APIKeyKey,
		row.Endpoint,
		fmt.Sprintf("%d", row.StatusCode),
		fmt.Sprintf("%t", row.Success),
		row.ErrorType,
		row.Currency,
	}
	if row.Internal {
		parts = append(parts, "internal")
	}
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(hash[:])
}

func requestLogIDs(logs []RequestLog) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(logs))
	for _, log := range logs {
		ids = append(ids, log.ID)
	}
	return ids
}

func nullableUUIDSummaryKey(value uuid.NullUUID) string {
	if !value.Valid {
		return requestUsageSummaryNoneKey
	}
	return value.UUID.String()
}

func nullableStringSummaryValue(value sql.NullString) string {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return requestUsageSummaryNoneKey
	}
	return strings.TrimSpace(value.String)
}

func requestUsageSummaryErrorType(log RequestLog) string {
	if value := nullableStringSummaryValue(log.ErrorType); value != requestUsageSummaryNoneKey {
		return value
	}
	if value := requestUsageSummaryMetadataString(requestUsageSummaryMetadata(log.Metadata), "error_code"); value != "" {
		return value
	}
	if !log.Success {
		return "unknown"
	}
	return requestUsageSummaryNoneKey
}

func requestUsageSummaryMetadata(raw JSON) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return map[string]any{}
	}
	return values
}

func requestUsageSummaryMetadataString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func requestUsageSummaryCachedTokens(raw JSON) (int64, bool) {
	metadata := requestUsageSummaryMetadata(raw)
	costCalculation, ok := metadata["cost_calculation"].(map[string]any)
	if !ok {
		return 0, false
	}
	for _, key := range []string{"cached_tokens", "cache_tokens"} {
		value, exists := costCalculation[key]
		if !exists {
			continue
		}
		switch typed := value.(type) {
		case float64:
			if typed < 0 || typed != float64(int64(typed)) {
				return 0, false
			}
			return int64(typed), true
		case json.Number:
			parsed, err := typed.Int64()
			if err != nil || parsed < 0 {
				return 0, false
			}
			return parsed, true
		case int64:
			if typed < 0 {
				return 0, false
			}
			return typed, true
		case int:
			if typed < 0 {
				return 0, false
			}
			return int64(typed), true
		}
		return 0, false
	}
	return 0, false
}

type requestUsageCacheWriteTokens struct {
	CacheWriteTokens           sql.NullInt64
	CacheCreationInputTokens   sql.NullInt64
	CacheCreation5mInputTokens sql.NullInt64
	CacheCreation1hInputTokens sql.NullInt64
}

func requestUsageSummaryCacheWriteTokens(raw JSON) (requestUsageCacheWriteTokens, bool) {
	metadata := requestUsageSummaryMetadata(raw)
	costCalculation, ok := metadata["cost_calculation"].(map[string]any)
	if !ok {
		return requestUsageCacheWriteTokens{}, false
	}
	result := requestUsageCacheWriteTokens{
		CacheWriteTokens:           requestUsageSummaryMetadataInt64(costCalculation, "cache_write_tokens"),
		CacheCreationInputTokens:   requestUsageSummaryMetadataInt64(costCalculation, "cache_creation_tokens"),
		CacheCreation5mInputTokens: requestUsageSummaryMetadataInt64(costCalculation, "cache_creation_5m_tokens"),
		CacheCreation1hInputTokens: requestUsageSummaryMetadataInt64(costCalculation, "cache_creation_1h_tokens"),
	}
	return result, result.CacheWriteTokens.Valid || result.CacheCreationInputTokens.Valid || result.CacheCreation5mInputTokens.Valid || result.CacheCreation1hInputTokens.Valid
}

func requestUsageSummaryMetadataInt64(values map[string]any, key string) sql.NullInt64 {
	value, ok := values[key]
	if !ok {
		return sql.NullInt64{}
	}
	switch typed := value.(type) {
	case float64:
		if typed < 0 || typed != float64(int64(typed)) {
			return sql.NullInt64{}
		}
		return sql.NullInt64{Int64: int64(typed), Valid: true}
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil || parsed < 0 {
			return sql.NullInt64{}
		}
		return sql.NullInt64{Int64: parsed, Valid: true}
	case int64:
		if typed < 0 {
			return sql.NullInt64{}
		}
		return sql.NullInt64{Int64: typed, Valid: true}
	case int:
		if typed < 0 {
			return sql.NullInt64{}
		}
		return sql.NullInt64{Int64: int64(typed), Valid: true}
	default:
		return sql.NullInt64{}
	}
}

func requestUsageSummaryMetadataUUID(raw JSON, key string) uuid.NullUUID {
	return requestUsageSummaryMetadataUUIDFromMap(requestUsageSummaryMetadata(raw), key)
}

func requestUsageSummaryMetadataUUIDFromMap(values map[string]any, key string) uuid.NullUUID {
	text := requestUsageSummaryMetadataString(values, key)
	if text == "" {
		return uuid.NullUUID{}
	}
	parsed, err := uuid.Parse(text)
	if err != nil || parsed == uuid.Nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}
}

func minNullableInt(left sql.NullInt64, right sql.NullInt64) sql.NullInt64 {
	if !right.Valid {
		return left
	}
	if !left.Valid || right.Int64 < left.Int64 {
		return right
	}
	return left
}

func maxNullableInt(left sql.NullInt64, right sql.NullInt64) sql.NullInt64 {
	if !right.Valid {
		return left
	}
	if !left.Valid || right.Int64 > left.Int64 {
		return right
	}
	return left
}

func minNullableTime(left sql.NullTime, right sql.NullTime) sql.NullTime {
	if !right.Valid {
		return left
	}
	if !left.Valid || right.Time.Before(left.Time) {
		return right
	}
	return left
}

func maxNullableTime(left sql.NullTime, right sql.NullTime) sql.NullTime {
	if !right.Valid {
		return left
	}
	if !left.Valid || right.Time.After(left.Time) {
		return right
	}
	return left
}

func normalizeSummaryTimeZone(timeZone config.TimeZone) config.TimeZone {
	if timeZone.Location != nil {
		return timeZone
	}
	return config.LoadTimeZone(timeZone.Name)
}
