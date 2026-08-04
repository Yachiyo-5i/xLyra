package dashboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"xlyra/server/internal/config"
	"xlyra/server/internal/inflight"
	"xlyra/server/internal/store"
)

const (
	overviewDays       = 90
	apiKeyActivityDays = 365
	failureReasonTopN  = 10
	highLatencyTopN    = 10
	uptimeBucketCount  = 24
	insufficientRouteN = 10
	highLatencyP95MS   = 30000
	highLatencyWindow  = 24 * time.Hour
	summaryNoneKey     = "none"

	attentionLimit            = 10
	attentionInitialTypeLimit = 3

	failureRateCriticalRequestN   = 10
	failureRateCriticalSuccessMax = 0.5
	highLatencyCriticalP95MS      = 120000
)

var overviewWindowDays = []int{7, 30, 90}

type Service struct {
	db       *store.Store
	timeZone config.TimeZone
}

func NewService(db *store.Store, timeZones ...config.TimeZone) *Service {
	return &Service{db: db, timeZone: serviceTimeZone(timeZones...)}
}

type UsageOverview struct {
	Meta    OverviewMeta              `json:"meta"`
	KPIs    OverviewKPIs              `json:"kpis"`
	Charts  OverviewCharts            `json:"charts"`
	Windows map[string]OverviewWindow `json:"windows"`
}

type InsightsOverview struct {
	Insights  OverviewInsights  `json:"insights"`
	Attention OverviewAttention `json:"attention"`
}

type OverviewMeta struct {
	Days          int    `json:"days"`
	AvailableDays []int  `json:"available_days"`
	Timezone      string `json:"timezone"`
	GeneratedAt   string `json:"generated_at"`
	TodayStart    string `json:"today_start"`
	RangeStart    string `json:"range_start"`
	RangeEnd      string `json:"range_end"`
}

type OverviewKPIs struct {
	Cost      CostKPI      `json:"cost"`
	Requests  RequestsKPI  `json:"requests"`
	RateLimit RateLimitKPI `json:"rate_limit"`
}

type EpaperSummary struct {
	Date           string                `json:"date"`
	RefreshAt      int64                 `json:"refresh_at"`
	KPIs           EpaperKPIs            `json:"kpis"`
	ModelTop3Today []EpaperModelCostItem `json:"model_top3_today"`
	CodexQuota     EpaperCodexQuota      `json:"codex_quota"`
}

type EpaperKPIs struct {
	TodayCost     float64 `json:"today_cost"`
	TotalCost     float64 `json:"total_cost"`
	TodayRequests int64   `json:"today_requests"`
	TotalRequests int64   `json:"total_requests"`
	TodayTokens   int64   `json:"today_tokens"`
	TotalTokens   int64   `json:"total_tokens"`
	RPMUsed       int64   `json:"rpm_used"`
	RPMLimit      *int64  `json:"rpm_limit"`
	TPMUsed       int64   `json:"tpm_used"`
	TPMLimit      *int64  `json:"tpm_limit"`
}

type EpaperModelCostItem struct {
	ModelKey string  `json:"model_key"`
	Cost     float64 `json:"cost"`
}

type EpaperCodexQuota struct {
	AccountCount int               `json:"account_count"`
	FiveHour     EpaperQuotaWindow `json:"five_hour"`
	Weekly       EpaperQuotaWindow `json:"weekly"`
}

type EpaperQuotaWindow struct {
	RemainingPercent *int   `json:"remaining_percent"`
	ResetAt          *int64 `json:"reset_at"`
}

type CostKPI struct {
	Today     float64 `json:"today"`
	Yesterday float64 `json:"yesterday"`
	Total     float64 `json:"total"`
	Currency  string  `json:"currency"`
}

type RequestsKPI struct {
	Today                 int64    `json:"today"`
	Yesterday             int64    `json:"yesterday"`
	Total                 int64    `json:"total"`
	TodayTokens           int64    `json:"today_tokens"`
	YesterdayTokens       int64    `json:"yesterday_tokens"`
	TotalTokens           int64    `json:"total_tokens"`
	TodayPromptTokens     int64    `json:"today_prompt_tokens"`
	TotalPromptTokens     int64    `json:"total_prompt_tokens"`
	TodayCompletionTokens int64    `json:"today_completion_tokens"`
	TotalCompletionTokens int64    `json:"total_completion_tokens"`
	TodayCachedTokens     int64    `json:"today_cached_tokens"`
	TotalCachedTokens     int64    `json:"total_cached_tokens"`
	SuccessRate           *float64 `json:"success_rate"`
}

type RateLimitKPI struct {
	RPM          RateLimitWindowKPI `json:"rpm"`
	TPM          TPMWindowKPI       `json:"tpm"`
	CompletedRPM int64              `json:"completed_rpm"`
}

type RateLimitWindowKPI struct {
	Used  int64  `json:"used"`
	Limit *int64 `json:"limit"`
}

type TPMWindowKPI struct {
	Used     int64  `json:"used"`
	Actual   int64  `json:"actual"`
	Reserved int64  `json:"reserved"`
	Limit    *int64 `json:"limit"`
}

type OverviewCharts struct {
	DailyModelCost      []DailyModelCostPoint    `json:"daily_model_cost"`
	DailyModelRequests  []DailyModelRequestPoint `json:"daily_model_requests"`
	DailySiteCost       []DailySiteCostPoint     `json:"daily_site_cost"`
	DailySiteRequests   []DailySiteRequestPoint  `json:"daily_site_requests"`
	DailyAPIKeyUsage    []DailyAPIKeyUsagePoint  `json:"daily_api_key_usage"`
	APIKeyContributions []DailyAPIKeyUsagePoint  `json:"api_key_contributions"`
	SiteCostSummary     []SiteCostSummaryItem    `json:"site_cost_summary"`
}

type OverviewWindow struct {
	Days            int                   `json:"days"`
	RangeStart      string                `json:"range_start"`
	RangeEnd        string                `json:"range_end"`
	SiteCostSummary []SiteCostSummaryItem `json:"site_cost_summary"`
	FailureReasons  []FailureReasonItem   `json:"failure_reasons"`
	HighLatency     []HighLatencyItem     `json:"high_latency"`
}

type DailyModelCostPoint struct {
	Date     string  `json:"date"`
	ModelID  *string `json:"model_id"`
	ModelKey string  `json:"model_key"`
	Cost     float64 `json:"cost"`
	Currency string  `json:"currency"`
}

type DailyModelRequestPoint struct {
	Date         string  `json:"date"`
	ModelID      *string `json:"model_id"`
	ModelKey     string  `json:"model_key"`
	RequestCount int64   `json:"request_count"`
}

type DailySiteCostPoint struct {
	Date     string  `json:"date"`
	SiteID   *string `json:"site_id"`
	SiteName string  `json:"site_name"`
	SiteSlug string  `json:"site_slug"`
	SiteType string  `json:"site_type"`
	SiteKey  string  `json:"site_key"`
	Cost     float64 `json:"cost"`
	Currency string  `json:"currency"`
}

type DailySiteRequestPoint struct {
	Date         string  `json:"date"`
	SiteID       *string `json:"site_id"`
	SiteName     string  `json:"site_name"`
	SiteSlug     string  `json:"site_slug"`
	SiteType     string  `json:"site_type"`
	SiteKey      string  `json:"site_key"`
	RequestCount int64   `json:"request_count"`
}

type DailyAPIKeyUsagePoint struct {
	Date        string  `json:"date"`
	APIKeyID    string  `json:"api_key_id"`
	APIKeyName  string  `json:"api_key_name"`
	TotalTokens int64   `json:"total_tokens"`
	Cost        float64 `json:"cost"`
	Currency    string  `json:"currency"`
}

type SiteCostSummaryItem struct {
	SiteID       string   `json:"site_id"`
	SiteName     string   `json:"site_name"`
	SiteSlug     string   `json:"site_slug"`
	SiteType     string   `json:"site_type"`
	RequestCount int64    `json:"request_count"`
	SuccessCount int64    `json:"success_count"`
	SuccessRate  *float64 `json:"success_rate"`
	TotalTokens  int64    `json:"total_tokens"`
	Cost         float64  `json:"cost"`
	Currency     string   `json:"currency"`
}

type OverviewHealth struct {
	UptimeRows []UptimeRow `json:"uptime_rows"`
}

type UptimeRow struct {
	SiteID   string         `json:"site_id"`
	SiteName string         `json:"site_name"`
	SiteSlug string         `json:"site_slug"`
	SiteType string         `json:"site_type"`
	Buckets  []UptimeBucket `json:"buckets"`
}

type UptimeBucket struct {
	Hour         string   `json:"hour"`
	Status       string   `json:"status"`
	SuccessCount int64    `json:"success_count"`
	FailureCount int64    `json:"failure_count"`
	TotalCount   int64    `json:"total_count"`
	SuccessRate  *float64 `json:"success_rate"`
}

type OverviewCooldowns struct {
	Items []CooldownItem `json:"items"`
}

type CooldownItem struct {
	ID                string         `json:"id"`
	SiteID            string         `json:"site_id"`
	SiteName          string         `json:"site_name"`
	SiteModelID       *string        `json:"site_model_id"`
	CanonicalModel    *string        `json:"canonical_model"`
	UpstreamModelName *string        `json:"upstream_model_name"`
	SiteCredentialID  *string        `json:"site_credential_id"`
	CredentialName    *string        `json:"credential_name"`
	MaskedKey         *string        `json:"masked_key"`
	Scope             string         `json:"scope"`
	Source            string         `json:"source"`
	Reason            string         `json:"reason"`
	ActiveUntil       string         `json:"active_until"`
	RemainingSeconds  int64          `json:"remaining_seconds"`
	Metadata          map[string]any `json:"metadata"`
}

type OverviewInsights struct {
	FailureReasons         []FailureReasonItem         `json:"failure_reasons"`
	InsufficientCandidates []InsufficientCandidateItem `json:"insufficient_candidates"`
	HighLatency            []HighLatencyItem           `json:"high_latency"`
}

type OverviewAttention struct {
	Items []AttentionItem `json:"items"`
}

type AttentionItem struct {
	ID        string          `json:"id"`
	Severity  string          `json:"severity"`
	Type      string          `json:"type"`
	Subject   map[string]any  `json:"subject,omitempty"`
	Metrics   map[string]any  `json:"metrics,omitempty"`
	Action    AttentionAction `json:"action"`
	UpdatedAt string          `json:"updated_at"`
}

type AttentionAction struct {
	Type   string            `json:"type"`
	Params map[string]string `json:"params,omitempty"`
}

type FailureReasonItem struct {
	Reason       string `json:"reason"`
	RequestCount int64  `json:"request_count"`
}

type InsufficientCandidateItem struct {
	CanonicalModelID string `json:"canonical_model_id"`
	ModelKey         string `json:"model_key"`
	DisplayName      string `json:"display_name"`
	SiteModelCount   int    `json:"site_model_count"`
	SiteCount        int    `json:"site_count"`
	EligibleCount    int    `json:"eligible_count"`
	CooldownCount    int    `json:"cooldown_count"`
	RequestCount24h  int    `json:"request_count_24h"`
}

type HighLatencyItem struct {
	SiteID       *string `json:"site_id"`
	SiteName     string  `json:"site_name"`
	ModelID      *string `json:"model_id"`
	ModelKey     string  `json:"model_key"`
	RequestCount int64   `json:"request_count"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	P95LatencyMS float64 `json:"p95_latency_ms"`
}

type timeWindow struct {
	TimeZoneName string
	Location     *time.Location
	Now          time.Time
	TodayStart   time.Time
	RangeStart   time.Time
	RangeEnd     time.Time
}

func (w timeWindow) format(value time.Time, layout string) string {
	return config.TimeZone{Name: w.TimeZoneName, Location: w.Location}.Format(value, layout)
}

func (s *Service) Usage(ctx context.Context, now time.Time) (UsageOverview, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return UsageOverview{}, fmt.Errorf("dashboard store is not initialized")
	}
	window := s.newTimeWindow(overviewDays, now)
	rows, err := s.usageSummaryRows(ctx, window)
	if err != nil {
		return UsageOverview{}, fmt.Errorf("dashboard usage summaries: %w", err)
	}
	return s.usageFromSummaries(ctx, window, rows)
}

func (s *Service) Cooldowns(ctx context.Context, now time.Time) (OverviewCooldowns, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return OverviewCooldowns{}, fmt.Errorf("dashboard store is not initialized")
	}
	window := s.newTimeWindow(overviewDays, now)
	items, err := s.cooldowns(ctx, window)
	if err != nil {
		return OverviewCooldowns{}, err
	}
	return OverviewCooldowns{Items: items}, nil
}

func (s *Service) Health(ctx context.Context, now time.Time) (OverviewHealth, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return OverviewHealth{}, fmt.Errorf("dashboard store is not initialized")
	}
	window := s.newTimeWindow(overviewDays, now)
	rows, err := s.uptimeRows(ctx, window)
	if err != nil {
		return OverviewHealth{}, err
	}
	return OverviewHealth{UptimeRows: rows}, nil
}

func (s *Service) Insights(ctx context.Context, now time.Time) (InsightsOverview, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return InsightsOverview{}, fmt.Errorf("dashboard store is not initialized")
	}
	window := s.newTimeWindow(overviewDays, now)
	summaries, err := s.usageSummaryRows(ctx, window)
	if err != nil {
		return InsightsOverview{}, fmt.Errorf("dashboard insight summaries: %w", err)
	}
	windows, err := s.overviewWindowsFromSummaries(ctx, window.Now, summaries)
	if err != nil {
		return InsightsOverview{}, err
	}
	failures, err := s.failureReasonsFromSummaries(window, summaries)
	if err != nil {
		return InsightsOverview{}, err
	}
	cooldowns, err := s.cooldowns(ctx, window)
	if err != nil {
		return InsightsOverview{}, err
	}
	insufficient, err := s.insufficientCandidates(ctx, window)
	if err != nil {
		return InsightsOverview{}, err
	}
	latency, err := s.highLatencyRecent(ctx, window)
	if err != nil {
		return InsightsOverview{}, err
	}
	attention, err := s.attentionItems(ctx, window, cooldowns, insufficient, windows, latency)
	if err != nil {
		return InsightsOverview{}, err
	}
	return InsightsOverview{
		Insights: OverviewInsights{
			FailureReasons:         failures,
			InsufficientCandidates: insufficient,
			HighLatency:            latency,
		},
		Attention: OverviewAttention{Items: attention},
	}, nil
}

func (s *Service) EpaperSummary(ctx context.Context, now time.Time) (EpaperSummary, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return EpaperSummary{}, fmt.Errorf("dashboard store is not initialized")
	}
	window := s.newTimeWindow(1, now)
	rows, err := s.usageSummaryRows(ctx, window)
	if err != nil {
		return EpaperSummary{}, fmt.Errorf("dashboard epaper usage summaries: %w", err)
	}
	cost := costKPIFromSummaries(window, rows)
	requests := requestsKPIFromSummaries(window, rows)
	rateLimit, err := s.rateLimitKPI(ctx, window)
	if err != nil {
		return EpaperSummary{}, err
	}
	modelTop3 := epaperModelTop3TodayFromSummaries(window, rows)
	codexQuota, err := s.epaperCodexQuota(ctx)
	if err != nil {
		return EpaperSummary{}, err
	}

	return EpaperSummary{
		Date:      window.format(window.TodayStart, "2006-01-02"),
		RefreshAt: window.Now.Unix(),
		KPIs: EpaperKPIs{
			TodayCost:     cost.Today,
			TotalCost:     cost.Total,
			TodayRequests: requests.Today,
			TotalRequests: requests.Total,
			TodayTokens:   requests.TodayTokens,
			TotalTokens:   requests.TotalTokens,
			RPMUsed:       rateLimit.RPM.Used,
			RPMLimit:      rateLimit.RPM.Limit,
			TPMUsed:       rateLimit.TPM.Used,
			TPMLimit:      rateLimit.TPM.Limit,
		},
		ModelTop3Today: modelTop3,
		CodexQuota:     codexQuota,
	}, nil
}

func (s *Service) usageSummaryRows(ctx context.Context, window timeWindow) ([]store.RequestUsageDailySummary, error) {
	repo := store.NewRequestUsageSummaryRepository(s.db.DB())
	rows, err := repo.List(ctx, dashboardHistoricalSummaryQuery(window))
	if err != nil {
		return nil, err
	}
	todayStart, todayEnd, timeZone := dashboardTodayDetailRange(window)
	todayRows, err := repo.ListFromDetails(ctx, todayStart, todayEnd, timeZone)
	if err != nil {
		return nil, err
	}
	return append(rows, todayRows...), nil
}

func dashboardHistoricalSummaryQuery(window timeWindow) store.RequestUsageSummaryQuery {
	historyEnd := window.TodayStart
	return store.RequestUsageSummaryQuery{
		TimeZone: window.TimeZoneName,
		To:       &historyEnd,
	}
}

func dashboardTodayDetailRange(window timeWindow) (time.Time, time.Time, config.TimeZone) {
	return window.TodayStart, window.Now, config.TimeZone{
		Name:     window.TimeZoneName,
		Location: window.Location,
	}
}

func serviceTimeZone(timeZones ...config.TimeZone) config.TimeZone {
	return config.TimeZoneOrDefault(timeZones...)
}

func (s *Service) newTimeWindow(days int, now time.Time) timeWindow {
	return newTimeWindow(days, now, s.timeZone)
}

func newTimeWindow(days int, now time.Time, timeZone config.TimeZone) timeWindow {
	timeZone = serviceTimeZone(timeZone)
	location := timeZone.Location
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(location)
	todayStart := timeZone.StartOfDay(now)
	rangeStart := todayStart.AddDate(0, 0, -days+1)
	return timeWindow{
		TimeZoneName: timeZone.Name,
		Location:     location,
		Now:          now,
		TodayStart:   todayStart,
		RangeStart:   rangeStart,
		RangeEnd:     now,
	}
}

func (s *Service) usageFromSummaries(ctx context.Context, window timeWindow, rows []store.RequestUsageDailySummary) (UsageOverview, error) {
	cost := costKPIFromSummaries(window, rows)
	requests := requestsKPIFromSummaries(window, rows)
	rateLimit, err := s.rateLimitKPI(ctx, window)
	if err != nil {
		return UsageOverview{}, err
	}
	dailyCost, err := s.dailyModelCostFromSummaries(window, rows)
	if err != nil {
		return UsageOverview{}, err
	}
	dailyRequests, err := s.dailyModelRequestsFromSummaries(window, rows)
	if err != nil {
		return UsageOverview{}, err
	}
	dailySiteCost, err := s.dailySiteCostFromSummaries(ctx, window, rows)
	if err != nil {
		return UsageOverview{}, err
	}
	dailySiteRequests, err := s.dailySiteRequestsFromSummaries(ctx, window, rows)
	if err != nil {
		return UsageOverview{}, err
	}
	apiKeys, err := s.apiKeysByID(ctx, dashboardAPIKeyUsageIDs(rows))
	if err != nil {
		return UsageOverview{}, fmt.Errorf("dashboard api key names: %w", err)
	}
	dailyAPIKeyUsage := dailyAPIKeyUsageFromSummaries(window, rows, apiKeys)
	apiKeyContributions := dailyAPIKeyUsageFromSummaries(s.newTimeWindow(apiKeyActivityDays, window.Now), rows, apiKeys)
	siteCosts, err := s.siteCostSummaryFromSummaries(ctx, window, rows)
	if err != nil {
		return UsageOverview{}, err
	}
	windows, err := s.overviewWindowsFromSummaries(ctx, window.Now, rows)
	if err != nil {
		return UsageOverview{}, err
	}
	return UsageOverview{
		Meta: OverviewMeta{
			Days:          overviewDays,
			AvailableDays: append([]int(nil), overviewWindowDays...),
			Timezone:      window.TimeZoneName,
			GeneratedAt:   window.format(window.Now, time.RFC3339),
			TodayStart:    window.format(window.TodayStart, time.RFC3339),
			RangeStart:    window.format(window.RangeStart, time.RFC3339),
			RangeEnd:      window.format(window.RangeEnd, time.RFC3339),
		},
		KPIs: OverviewKPIs{
			Cost:      cost,
			Requests:  requests,
			RateLimit: rateLimit,
		},
		Charts: OverviewCharts{
			DailyModelCost:      dailyCost,
			DailyModelRequests:  dailyRequests,
			DailySiteCost:       dailySiteCost,
			DailySiteRequests:   dailySiteRequests,
			DailyAPIKeyUsage:    dailyAPIKeyUsage,
			APIKeyContributions: apiKeyContributions,
			SiteCostSummary:     siteCosts,
		},
		Windows: windows,
	}, nil
}

func costKPIFromSummaries(window timeWindow, rows []store.RequestUsageDailySummary) CostKPI {
	yesterdayStart := window.TodayStart.AddDate(0, 0, -1)
	result := CostKPI{Currency: "USD"}
	for _, row := range rows {
		result.Total += row.EstimatedCost
		if row.Currency != "" {
			result.Currency = row.Currency
		}
		if !row.BucketStart.Before(window.TodayStart) {
			result.Today += row.EstimatedCost
			continue
		}
		if !row.BucketStart.Before(yesterdayStart) && row.BucketStart.Before(window.TodayStart) {
			result.Yesterday += row.EstimatedCost
		}
	}
	return result
}

func requestsKPIFromSummaries(window timeWindow, rows []store.RequestUsageDailySummary) RequestsKPI {
	yesterdayStart := window.TodayStart.AddDate(0, 0, -1)
	var todayAttempts int64
	var todaySuccess int64
	result := RequestsKPI{}
	for _, row := range rows {
		if row.Success {
			result.Total += row.SuccessCount
			result.TotalTokens += row.TotalTokens
			result.TotalPromptTokens += row.PromptTokens
			result.TotalCompletionTokens += row.CompletionTokens
			result.TotalCachedTokens += row.CachedTokens
		}
		if !row.BucketStart.Before(window.TodayStart) {
			todayAttempts += row.RequestCount
			todaySuccess += row.SuccessCount
			if row.Success {
				result.Today += row.SuccessCount
				result.TodayTokens += row.TotalTokens
				result.TodayPromptTokens += row.PromptTokens
				result.TodayCompletionTokens += row.CompletionTokens
				result.TodayCachedTokens += row.CachedTokens
			}
			continue
		}
		if !row.BucketStart.Before(yesterdayStart) && row.BucketStart.Before(window.TodayStart) && row.Success {
			result.Yesterday += row.SuccessCount
			result.YesterdayTokens += row.TotalTokens
		}
	}
	if todayAttempts > 0 {
		value := float64(todaySuccess) / float64(todayAttempts)
		result.SuccessRate = &value
	}
	return result
}

func (s *Service) dailyModelCostFromSummaries(window timeWindow, rows []store.RequestUsageDailySummary) ([]DailyModelCostPoint, error) {
	type aggregate struct {
		date     string
		modelID  uuid.NullUUID
		modelKey string
		cost     float64
		currency string
	}
	byKey := map[string]*aggregate{}
	for _, row := range rows {
		if row.BucketStart.Before(window.RangeStart) {
			continue
		}
		date := summaryDate(row.BucketStart, window)
		modelKey := defaultString(row.CanonicalModelKey, "unknown")
		if modelKey == summaryNoneKey {
			modelKey = "unknown"
		}
		currency := defaultString(row.Currency, "USD")
		key := date + "\x00" + nullableUUIDKey(row.CanonicalModelID) + "\x00" + modelKey + "\x00" + currency
		item := byKey[key]
		if item == nil {
			item = &aggregate{date: date, modelID: row.CanonicalModelID, modelKey: modelKey, currency: currency}
			byKey[key] = item
		}
		item.cost += row.EstimatedCost
	}
	rowsOut := make([]aggregate, 0, len(byKey))
	for _, row := range byKey {
		rowsOut = append(rowsOut, *row)
	}
	sort.SliceStable(rowsOut, func(i, j int) bool {
		if rowsOut[i].date != rowsOut[j].date {
			return rowsOut[i].date < rowsOut[j].date
		}
		return rowsOut[i].cost > rowsOut[j].cost
	})
	items := make([]DailyModelCostPoint, 0, len(rowsOut))
	for _, row := range rowsOut {
		items = append(items, DailyModelCostPoint{
			Date:     row.date,
			ModelID:  nullableUUIDString(row.modelID),
			ModelKey: row.modelKey,
			Cost:     row.cost,
			Currency: defaultString(row.currency, "USD"),
		})
	}
	return items, nil
}

func (s *Service) dailyModelRequestsFromSummaries(window timeWindow, rows []store.RequestUsageDailySummary) ([]DailyModelRequestPoint, error) {
	type aggregate struct {
		date         string
		modelID      uuid.NullUUID
		modelKey     string
		requestCount int64
	}
	byKey := map[string]*aggregate{}
	for _, row := range rows {
		if row.BucketStart.Before(window.RangeStart) || !row.Success {
			continue
		}
		date := summaryDate(row.BucketStart, window)
		modelKey := defaultString(row.CanonicalModelKey, "unknown")
		if modelKey == summaryNoneKey {
			modelKey = "unknown"
		}
		key := date + "\x00" + nullableUUIDKey(row.CanonicalModelID) + "\x00" + modelKey
		item := byKey[key]
		if item == nil {
			item = &aggregate{date: date, modelID: row.CanonicalModelID, modelKey: modelKey}
			byKey[key] = item
		}
		item.requestCount += row.SuccessCount
	}
	rowsOut := make([]aggregate, 0, len(byKey))
	for _, row := range byKey {
		rowsOut = append(rowsOut, *row)
	}
	sort.SliceStable(rowsOut, func(i, j int) bool {
		if rowsOut[i].date != rowsOut[j].date {
			return rowsOut[i].date < rowsOut[j].date
		}
		return rowsOut[i].requestCount > rowsOut[j].requestCount
	})
	items := make([]DailyModelRequestPoint, 0, len(rowsOut))
	for _, row := range rowsOut {
		items = append(items, DailyModelRequestPoint{
			Date:         row.date,
			ModelID:      nullableUUIDString(row.modelID),
			ModelKey:     row.modelKey,
			RequestCount: row.requestCount,
		})
	}
	return items, nil
}

func (s *Service) dailySiteCostFromSummaries(ctx context.Context, window timeWindow, rows []store.RequestUsageDailySummary) ([]DailySiteCostPoint, error) {
	sites, err := s.sitesByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard daily site cost: %w", err)
	}
	type aggregate struct {
		date     string
		siteID   uuid.NullUUID
		siteName string
		siteSlug string
		siteType string
		siteKey  string
		cost     float64
		currency string
	}
	byKey := map[string]*aggregate{}
	for _, row := range rows {
		if row.BucketStart.Before(window.RangeStart) {
			continue
		}
		date := summaryDate(row.BucketStart, window)
		snapshot := resolveSummarySiteSnapshot(row, sites)
		currency := defaultString(row.Currency, "USD")
		key := date + "\x00" + snapshot.aggregateKey + "\x00" + currency
		item := byKey[key]
		if item == nil {
			item = &aggregate{
				date:     date,
				siteID:   snapshot.siteID,
				siteName: snapshot.siteName,
				siteSlug: snapshot.siteSlug,
				siteType: snapshot.siteType,
				siteKey:  snapshot.siteKey,
				currency: currency,
			}
			byKey[key] = item
		}
		item.cost += row.EstimatedCost
	}
	rowsOut := make([]aggregate, 0, len(byKey))
	for _, row := range byKey {
		rowsOut = append(rowsOut, *row)
	}
	sort.SliceStable(rowsOut, func(i, j int) bool {
		if rowsOut[i].date != rowsOut[j].date {
			return rowsOut[i].date < rowsOut[j].date
		}
		return rowsOut[i].cost > rowsOut[j].cost
	})
	items := make([]DailySiteCostPoint, 0, len(rowsOut))
	for _, row := range rowsOut {
		items = append(items, DailySiteCostPoint{
			Date:     row.date,
			SiteID:   nullableUUIDString(row.siteID),
			SiteName: row.siteName,
			SiteSlug: row.siteSlug,
			SiteType: row.siteType,
			SiteKey:  defaultString(row.siteKey, "unknown"),
			Cost:     row.cost,
			Currency: defaultString(row.currency, "USD"),
		})
	}
	return items, nil
}

func (s *Service) dailySiteRequestsFromSummaries(ctx context.Context, window timeWindow, rows []store.RequestUsageDailySummary) ([]DailySiteRequestPoint, error) {
	sites, err := s.sitesByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard daily site requests: %w", err)
	}
	type aggregate struct {
		date         string
		siteID       uuid.NullUUID
		siteName     string
		siteSlug     string
		siteType     string
		siteKey      string
		requestCount int64
	}
	byKey := map[string]*aggregate{}
	for _, row := range rows {
		if row.BucketStart.Before(window.RangeStart) || !row.Success {
			continue
		}
		date := summaryDate(row.BucketStart, window)
		snapshot := resolveSummarySiteSnapshot(row, sites)
		key := date + "\x00" + snapshot.aggregateKey
		item := byKey[key]
		if item == nil {
			item = &aggregate{
				date:     date,
				siteID:   snapshot.siteID,
				siteName: snapshot.siteName,
				siteSlug: snapshot.siteSlug,
				siteType: snapshot.siteType,
				siteKey:  snapshot.siteKey,
			}
			byKey[key] = item
		}
		item.requestCount += row.SuccessCount
	}
	rowsOut := make([]aggregate, 0, len(byKey))
	for _, row := range byKey {
		rowsOut = append(rowsOut, *row)
	}
	sort.SliceStable(rowsOut, func(i, j int) bool {
		if rowsOut[i].date != rowsOut[j].date {
			return rowsOut[i].date < rowsOut[j].date
		}
		return rowsOut[i].requestCount > rowsOut[j].requestCount
	})
	items := make([]DailySiteRequestPoint, 0, len(rowsOut))
	for _, row := range rowsOut {
		items = append(items, DailySiteRequestPoint{
			Date:         row.date,
			SiteID:       nullableUUIDString(row.siteID),
			SiteName:     row.siteName,
			SiteSlug:     row.siteSlug,
			SiteType:     row.siteType,
			SiteKey:      defaultString(row.siteKey, "unknown"),
			RequestCount: row.requestCount,
		})
	}
	return items, nil
}

func dailyAPIKeyUsageFromSummaries(window timeWindow, rows []store.RequestUsageDailySummary, apiKeys map[uuid.UUID]store.APIKey) []DailyAPIKeyUsagePoint {
	type aggregate struct {
		date        string
		apiKeyID    uuid.UUID
		apiKeyName  string
		totalTokens int64
		cost        float64
		currency    string
	}
	byKey := map[string]*aggregate{}
	for _, row := range rows {
		if row.BucketStart.Before(window.RangeStart) || !row.APIKeyID.Valid {
			continue
		}
		date := summaryDate(row.BucketStart, window)
		apiKeyID := row.APIKeyID.UUID
		currency := defaultString(row.Currency, "USD")
		key := date + "\x00" + apiKeyID.String() + "\x00" + currency
		item := byKey[key]
		if item == nil {
			item = &aggregate{
				date:       date,
				apiKeyID:   apiKeyID,
				apiKeyName: strings.TrimSpace(row.APIKeyName),
				currency:   currency,
			}
			byKey[key] = item
		}
		if item.apiKeyName == "" {
			item.apiKeyName = strings.TrimSpace(row.APIKeyName)
		}
		item.totalTokens += row.TotalTokens
		item.cost += row.EstimatedCost
	}
	rowsOut := make([]aggregate, 0, len(byKey))
	for _, row := range byKey {
		rowsOut = append(rowsOut, *row)
	}
	sort.SliceStable(rowsOut, func(i, j int) bool {
		if rowsOut[i].date != rowsOut[j].date {
			return rowsOut[i].date < rowsOut[j].date
		}
		if rowsOut[i].totalTokens != rowsOut[j].totalTokens {
			return rowsOut[i].totalTokens > rowsOut[j].totalTokens
		}
		return dashboardAPIKeyUsageName(rowsOut[i].apiKeyID, rowsOut[i].apiKeyName, apiKeys) < dashboardAPIKeyUsageName(rowsOut[j].apiKeyID, rowsOut[j].apiKeyName, apiKeys)
	})
	items := make([]DailyAPIKeyUsagePoint, 0, len(rowsOut))
	for _, row := range rowsOut {
		apiKeyID := row.apiKeyID.String()
		items = append(items, DailyAPIKeyUsagePoint{
			Date:        row.date,
			APIKeyID:    apiKeyID,
			APIKeyName:  defaultString(dashboardAPIKeyUsageName(row.apiKeyID, row.apiKeyName, apiKeys), apiKeyID),
			TotalTokens: row.totalTokens,
			Cost:        row.cost,
			Currency:    defaultString(row.currency, "USD"),
		})
	}
	return items
}

func (s *Service) apiKeysByID(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]store.APIKey, error) {
	result := map[uuid.UUID]store.APIKey{}
	if len(ids) == 0 {
		return result, nil
	}
	apiKeys, err := store.NewAPIKeyRepository(s.db.DB()).ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, apiKey := range apiKeys {
		result[apiKey.ID] = apiKey
	}
	return result, nil
}

func dashboardAPIKeyUsageIDs(rows []store.RequestUsageDailySummary) []uuid.UUID {
	ids := make([]uuid.UUID, 0)
	seen := map[uuid.UUID]struct{}{}
	for _, row := range rows {
		if !row.APIKeyID.Valid {
			continue
		}
		apiKeyID := row.APIKeyID.UUID
		if _, ok := seen[apiKeyID]; ok {
			continue
		}
		seen[apiKeyID] = struct{}{}
		ids = append(ids, apiKeyID)
	}
	return ids
}

func dashboardAPIKeyUsageName(apiKeyID uuid.UUID, snapshotName string, apiKeys map[uuid.UUID]store.APIKey) string {
	if apiKey, ok := apiKeys[apiKeyID]; ok {
		if name := strings.TrimSpace(apiKey.Name); name != "" {
			return name
		}
	}
	return strings.TrimSpace(snapshotName)
}

func (s *Service) siteCostSummaryFromSummaries(ctx context.Context, window timeWindow, rows []store.RequestUsageDailySummary) ([]SiteCostSummaryItem, error) {
	sites, err := s.sitesByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard site cost summary: %w", err)
	}
	type aggregate struct {
		siteID       uuid.NullUUID
		siteName     string
		siteSlug     string
		siteType     string
		siteKey      string
		requestCount int64
		successCount int64
		totalTokens  int64
		cost         float64
		currency     string
	}
	bySite := map[string]*aggregate{}
	for _, row := range rows {
		if row.BucketStart.Before(window.RangeStart) {
			continue
		}
		snapshot := resolveSummarySiteSnapshot(row, sites)
		item := bySite[snapshot.aggregateKey]
		if item == nil {
			item = &aggregate{
				siteID:   snapshot.siteID,
				siteName: snapshot.siteName,
				siteSlug: snapshot.siteSlug,
				siteType: snapshot.siteType,
				siteKey:  snapshot.siteKey,
				currency: "USD",
			}
			bySite[snapshot.aggregateKey] = item
		}
		item.requestCount += row.RequestCount
		item.successCount += row.SuccessCount
		item.totalTokens += row.TotalTokens
		item.cost += row.EstimatedCost
		if row.Currency != "" {
			item.currency = row.Currency
		}
	}
	rowsOut := make([]aggregate, 0, len(bySite))
	for _, row := range bySite {
		if row.cost > 0 && dashboardSiteActive(row.siteID, sites) {
			rowsOut = append(rowsOut, *row)
		}
	}
	sort.SliceStable(rowsOut, func(i, j int) bool {
		if rowsOut[i].cost != rowsOut[j].cost {
			return rowsOut[i].cost > rowsOut[j].cost
		}
		if rowsOut[i].requestCount != rowsOut[j].requestCount {
			return rowsOut[i].requestCount > rowsOut[j].requestCount
		}
		return rowsOut[i].siteName < rowsOut[j].siteName
	})
	items := make([]SiteCostSummaryItem, 0, len(rowsOut))
	for _, row := range rowsOut {
		var successRate *float64
		if row.requestCount > 0 {
			value := float64(row.successCount) / float64(row.requestCount)
			successRate = &value
		}
		items = append(items, SiteCostSummaryItem{
			SiteID:       dashboardSiteIDString(row.siteID, row.siteKey),
			SiteName:     row.siteName,
			SiteSlug:     row.siteSlug,
			SiteType:     row.siteType,
			RequestCount: row.requestCount,
			SuccessCount: row.successCount,
			SuccessRate:  successRate,
			TotalTokens:  row.totalTokens,
			Cost:         row.cost,
			Currency:     defaultString(row.currency, "USD"),
		})
	}
	return items, nil
}

func (s *Service) overviewWindowsFromSummaries(ctx context.Context, now time.Time, rows []store.RequestUsageDailySummary) (map[string]OverviewWindow, error) {
	windows := make(map[string]OverviewWindow, len(overviewWindowDays))
	for _, days := range overviewWindowDays {
		window := s.newTimeWindow(days, now)
		siteCosts, err := s.siteCostSummaryFromSummaries(ctx, window, rows)
		if err != nil {
			return nil, err
		}
		failures, err := s.failureReasonsFromSummaries(window, rows)
		if err != nil {
			return nil, err
		}
		windows[fmt.Sprintf("%d", days)] = OverviewWindow{
			Days:            days,
			RangeStart:      window.format(window.RangeStart, time.RFC3339),
			RangeEnd:        window.format(window.RangeEnd, time.RFC3339),
			SiteCostSummary: siteCosts,
			FailureReasons:  failures,
			HighLatency:     []HighLatencyItem{},
		}
	}
	return windows, nil
}

func (s *Service) failureReasonsFromSummaries(window timeWindow, rows []store.RequestUsageDailySummary) ([]FailureReasonItem, error) {
	counts := map[string]int64{}
	for _, row := range rows {
		if row.BucketStart.Before(window.RangeStart) || row.Success {
			continue
		}
		reason := strings.TrimSpace(row.ErrorType)
		if reason == "" || reason == summaryNoneKey {
			reason = "unknown"
		}
		counts[reason] += row.FailureCount
	}
	items := make([]FailureReasonItem, 0, len(counts))
	for reason, count := range counts {
		items = append(items, FailureReasonItem{Reason: reason, RequestCount: count})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].RequestCount != items[j].RequestCount {
			return items[i].RequestCount > items[j].RequestCount
		}
		return items[i].Reason < items[j].Reason
	})
	if len(items) > failureReasonTopN {
		items = items[:failureReasonTopN]
	}
	return items, nil
}

func (s *Service) rateLimitKPI(ctx context.Context, window timeWindow) (RateLimitKPI, error) {
	var rateLimits []store.GatewayRateLimit
	if err := s.db.DB().WithContext(ctx).Where(&store.GatewayRateLimit{Scope: store.RateLimitScopeGlobal, Status: store.RateLimitStatusEnabled}).Find(&rateLimits).Error; err != nil {
		return RateLimitKPI{}, fmt.Errorf("dashboard rate limit kpi: %w", err)
	}
	var row struct {
		RPMLimit sql.NullInt64
		TPMLimit sql.NullInt64
	}
	if len(rateLimits) > 0 {
		row.RPMLimit = rateLimits[0].RPMLimit
		row.TPMLimit = rateLimits[0].TPMLimit
	}
	recent, err := store.NewRequestLogRepository(s.db.DB()).RecentRateUsage(ctx, window.Now.Add(-time.Minute))
	if err != nil {
		return RateLimitKPI{}, fmt.Errorf("dashboard recent rate usage: %w", err)
	}
	inFlight := inflight.Count()
	return RateLimitKPI{
		RPM: RateLimitWindowKPI{
			Used:  inFlight,
			Limit: nullIntPtr(row.RPMLimit),
		},
		TPM: TPMWindowKPI{
			Used:     recent.TPM,
			Actual:   recent.TPM,
			Reserved: 0,
			Limit:    nullIntPtr(row.TPMLimit),
		},
		CompletedRPM: recent.RPM,
	}, nil
}

func epaperModelTop3TodayFromSummaries(window timeWindow, rows []store.RequestUsageDailySummary) []EpaperModelCostItem {
	type aggregate struct {
		modelKey string
		cost     float64
	}
	byKey := map[string]*aggregate{}
	tomorrowStart := window.TodayStart.AddDate(0, 0, 1)
	for _, row := range rows {
		if row.BucketStart.Before(window.TodayStart) || !row.BucketStart.Before(tomorrowStart) {
			continue
		}
		modelKey := epaperSummaryModelKey(row)
		item := byKey[modelKey]
		if item == nil {
			item = &aggregate{modelKey: modelKey}
			byKey[modelKey] = item
		}
		item.cost += row.EstimatedCost
	}
	aggregateRows := make([]aggregate, 0, len(byKey))
	for _, item := range byKey {
		if item.cost <= 0 {
			continue
		}
		aggregateRows = append(aggregateRows, *item)
	}
	sort.SliceStable(aggregateRows, func(i, j int) bool {
		if aggregateRows[i].cost != aggregateRows[j].cost {
			return aggregateRows[i].cost > aggregateRows[j].cost
		}
		return aggregateRows[i].modelKey < aggregateRows[j].modelKey
	})
	if len(aggregateRows) > 3 {
		aggregateRows = aggregateRows[:3]
	}
	items := make([]EpaperModelCostItem, 0, len(aggregateRows))
	for _, row := range aggregateRows {
		items = append(items, EpaperModelCostItem{ModelKey: row.modelKey, Cost: row.cost})
	}
	return items
}

func epaperSummaryModelKey(row store.RequestUsageDailySummary) string {
	if value := strings.TrimSpace(row.CanonicalModelKey); value != "" && value != summaryNoneKey {
		return value
	}
	if value := strings.TrimSpace(row.UpstreamModelName); value != "" && value != summaryNoneKey {
		return value
	}
	return "unknown"
}

func (s *Service) epaperCodexQuota(ctx context.Context) (EpaperCodexQuota, error) {
	sites, err := s.activeSiteList(ctx)
	if err != nil {
		return EpaperCodexQuota{}, fmt.Errorf("dashboard epaper codex quota sites: %w", err)
	}
	states, err := store.NewSiteStateRepository(s.db.DB()).List(ctx)
	if err != nil {
		return EpaperCodexQuota{}, fmt.Errorf("dashboard epaper codex quota states: %w", err)
	}
	items := make([]map[string]any, 0)
	for _, site := range sites {
		if site.SiteType != "codex" {
			continue
		}
		state, ok := states[site.ID]
		if !ok {
			continue
		}
		quota := epaperCodexQuotaPayload(state.UserSummary)
		if len(quota) == 0 {
			continue
		}
		items = append(items, quota)
	}
	return summarizeEpaperCodexQuota(items), nil
}

func dashboardSiteActive(siteID uuid.NullUUID, sites map[uuid.UUID]store.Site) bool {
	if !siteID.Valid || siteID.UUID == uuid.Nil {
		return false
	}
	site, ok := sites[siteID.UUID]
	if !ok || site.ID == uuid.Nil {
		return false
	}
	return !store.SiteDeleted(site) && site.Enabled
}

type uptimeAggregate struct {
	SiteID       uuid.UUID
	BucketTime   time.Time
	SuccessCount int64
	FailureCount int64
	TotalCount   int64
}

func (s *Service) uptimeRows(ctx context.Context, window timeWindow) ([]UptimeRow, error) {
	sites, err := s.activeSiteList(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard uptime sites: %w", err)
	}
	sort.SliceStable(sites, func(i, j int) bool {
		if !sites[i].CreatedAt.Equal(sites[j].CreatedAt) {
			return sites[i].CreatedAt.Before(sites[j].CreatedAt)
		}
		return sites[i].ID.String() < sites[j].ID.String()
	})
	currentHour := config.TimeZone{Name: window.TimeZoneName, Location: window.Location}.StartOfHour(window.Now)
	firstHour := currentHour.Add(-time.Duration(uptimeBucketCount-1) * time.Hour)
	endHour := currentHour.Add(time.Hour)

	var aggregates []uptimeAggregate
	var snapshots []store.HealthSnapshot
	if err := s.db.DB().WithContext(ctx).
		Clauses(timeRangeClause("checked_at", firstHour, endHour)).
		Where(&store.HealthSnapshot{Scope: "site", Source: "scheduler"}).
		Find(&snapshots).Error; err != nil {
		return nil, fmt.Errorf("dashboard uptime buckets: %w", err)
	}
	aggregateByKey := map[string]*uptimeAggregate{}
	for _, snapshot := range snapshots {
		checkedAt := snapshot.CheckedAt.In(window.Location)
		if checkedAt.Before(firstHour) || !checkedAt.Before(endHour) {
			continue
		}
		bucketTime := config.TimeZone{Name: window.TimeZoneName, Location: window.Location}.StartOfHour(checkedAt)
		key := snapshot.SiteID.String() + "\x00" + uptimeHourKey(bucketTime)
		aggregate := aggregateByKey[key]
		if aggregate == nil {
			aggregate = &uptimeAggregate{SiteID: snapshot.SiteID, BucketTime: bucketTime}
			aggregateByKey[key] = aggregate
		}
		aggregate.TotalCount++
		if snapshot.Success {
			aggregate.SuccessCount++
		} else {
			aggregate.FailureCount++
		}
	}
	for _, aggregate := range aggregateByKey {
		aggregates = append(aggregates, *aggregate)
	}
	bySiteHour := map[uuid.UUID]map[string]uptimeAggregate{}
	for _, aggregate := range aggregates {
		if bySiteHour[aggregate.SiteID] == nil {
			bySiteHour[aggregate.SiteID] = map[string]uptimeAggregate{}
		}
		bySiteHour[aggregate.SiteID][uptimeHourKey(aggregate.BucketTime)] = aggregate
	}

	rows := make([]UptimeRow, 0, len(sites))
	for _, site := range sites {
		buckets := make([]UptimeBucket, 0, uptimeBucketCount)
		for index := 0; index < uptimeBucketCount; index++ {
			hour := firstHour.Add(time.Duration(index) * time.Hour)
			aggregate := bySiteHour[site.ID][uptimeHourKey(hour)]
			buckets = append(buckets, uptimeBucket{
				hour:         hour,
				successCount: aggregate.SuccessCount,
				failureCount: aggregate.FailureCount,
				totalCount:   aggregate.TotalCount,
			}.payload(window))
		}
		rows = append(rows, UptimeRow{
			SiteID:   site.ID.String(),
			SiteName: site.Name,
			SiteSlug: site.Slug,
			SiteType: site.SiteType,
			Buckets:  buckets,
		})
	}
	return rows, nil
}

type uptimeBucket struct {
	hour         time.Time
	successCount int64
	failureCount int64
	totalCount   int64
}

func (b uptimeBucket) payload(window timeWindow) UptimeBucket {
	status := BucketStatus(b.successCount, b.failureCount)
	var successRate *float64
	if b.totalCount > 0 {
		value := float64(b.successCount) / float64(b.totalCount)
		successRate = &value
	}
	return UptimeBucket{
		Hour:         window.format(b.hour, time.RFC3339),
		Status:       status,
		SuccessCount: b.successCount,
		FailureCount: b.failureCount,
		TotalCount:   b.totalCount,
		SuccessRate:  successRate,
	}
}

func BucketStatus(successCount int64, failureCount int64) string {
	total := successCount + failureCount
	if total == 0 {
		return "idle"
	}
	if successCount == total {
		return "healthy"
	}
	if failureCount == total {
		return "unhealthy"
	}
	return "degraded"
}

func (s *Service) cooldowns(ctx context.Context, window timeWindow) ([]CooldownItem, error) {
	now := window.Now
	rows, err := store.NewRouteCooldownRepository(s.db.DB()).ListActive(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("dashboard cooldowns: %w", err)
	}
	sites, err := s.sitesByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard cooldowns: %w", err)
	}
	siteModels, err := s.siteModelsByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard cooldowns: %w", err)
	}
	canonicalModels, err := s.canonicalModelsByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard cooldowns: %w", err)
	}
	credentials, err := s.siteCredentialsByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard cooldowns: %w", err)
	}
	apiKeyStates, err := s.siteAPIKeyStatesByCredentialID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard cooldowns: %w", err)
	}
	items := make([]CooldownItem, 0, len(rows))
	for _, row := range rows {
		remaining := int64(row.ActiveUntil.Sub(now).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		site := sites[row.SiteID]
		if site.ID == uuid.Nil || store.SiteDeleted(site) {
			continue
		}
		var canonicalModel sql.NullString
		var upstreamModelName sql.NullString
		if row.SiteModelID.Valid {
			siteModel := siteModels[row.SiteModelID.UUID]
			if siteModel.UpstreamName != "" {
				upstreamModelName = sql.NullString{String: siteModel.UpstreamName, Valid: true}
			}
			if siteModel.CanonicalID.Valid {
				if model := canonicalModels[siteModel.CanonicalID.UUID]; model.ModelKey != "" {
					canonicalModel = sql.NullString{String: model.ModelKey, Valid: true}
				}
			}
		}
		var credentialName sql.NullString
		var maskedKey sql.NullString
		if row.SiteCredentialID.Valid {
			credential := credentials[row.SiteCredentialID.UUID]
			state := apiKeyStates[row.SiteCredentialID.UUID]
			name := credential.CredentialType
			if state.Name != "" {
				name = state.Name
			}
			if name != "" {
				credentialName = sql.NullString{String: name, Valid: true}
			}
			if credential.MaskedSecret != "" {
				maskedKey = sql.NullString{String: credential.MaskedSecret, Valid: true}
			}
		}
		items = append(items, CooldownItem{
			ID:                row.ID.String(),
			SiteID:            row.SiteID.String(),
			SiteName:          site.Name,
			SiteModelID:       nullableUUIDString(row.SiteModelID),
			CanonicalModel:    nullableStringPtr(canonicalModel),
			UpstreamModelName: nullableStringPtr(upstreamModelName),
			SiteCredentialID:  nullableUUIDString(row.SiteCredentialID),
			CredentialName:    nullableStringPtr(credentialName),
			MaskedKey:         nullableStringPtr(maskedKey),
			Scope:             row.Scope,
			Source:            row.Source,
			Reason:            row.Reason,
			ActiveUntil:       window.format(row.ActiveUntil, time.RFC3339),
			RemainingSeconds:  remaining,
			Metadata:          jsonMap(row.Metadata),
		})
	}
	return items, nil
}

func (s *Service) insufficientCandidates(ctx context.Context, window timeWindow) ([]InsufficientCandidateItem, error) {
	rows, err := store.NewRouteInsightRepository(s.db.DB()).ListCandidateAvailability(ctx, window.Now.Add(-24*time.Hour))
	if err != nil {
		return nil, fmt.Errorf("dashboard insufficient candidates: %w", err)
	}
	filtered := make([]store.RouteOverviewRow, 0)
	for _, row := range rows {
		if row.EligibleCount <= 1 {
			filtered = append(filtered, row)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].EligibleCount != filtered[j].EligibleCount {
			return filtered[i].EligibleCount < filtered[j].EligibleCount
		}
		if filtered[i].SiteModelCount != filtered[j].SiteModelCount {
			return filtered[i].SiteModelCount > filtered[j].SiteModelCount
		}
		return filtered[i].ModelKey < filtered[j].ModelKey
	})
	if len(filtered) > insufficientRouteN {
		filtered = filtered[:insufficientRouteN]
	}
	items := make([]InsufficientCandidateItem, 0, len(filtered))
	for _, row := range filtered {
		items = append(items, InsufficientCandidateItem{
			CanonicalModelID: row.CanonicalModelID.String(),
			ModelKey:         row.ModelKey,
			DisplayName:      row.DisplayName,
			SiteModelCount:   row.SiteModelCount,
			SiteCount:        row.SiteCount,
			EligibleCount:    row.EligibleCount,
			CooldownCount:    row.CooldownCount,
			RequestCount24h:  row.RequestCount24h,
		})
	}
	return items, nil
}

func (s *Service) highLatencyRecent(ctx context.Context, window timeWindow) ([]HighLatencyItem, error) {
	latencyWindow := timeWindow{
		TimeZoneName: window.TimeZoneName,
		Location:     window.Location,
		Now:          window.Now,
		TodayStart:   window.TodayStart,
		RangeStart:   window.Now.Add(-highLatencyWindow),
		RangeEnd:     window.Now,
	}
	return s.highLatency(ctx, latencyWindow)
}

func (s *Service) highLatency(ctx context.Context, window timeWindow) ([]HighLatencyItem, error) {
	logs, err := s.requestLogsSince(ctx, window.RangeStart)
	if err != nil {
		return nil, fmt.Errorf("dashboard high latency: %w", err)
	}
	sites, err := s.sitesByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard high latency: %w", err)
	}
	models, err := s.canonicalModelsByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard high latency: %w", err)
	}
	type aggregate struct {
		siteID   uuid.NullUUID
		modelID  uuid.NullUUID
		latency  []int64
		latencyF []float64
	}
	byKey := map[string]*aggregate{}
	for _, log := range logs {
		if log.CreatedAt.Before(window.RangeStart) || !log.LatencyMS.Valid {
			continue
		}
		key := nullableUUIDKey(log.SiteID) + "\x00" + nullableUUIDKey(log.CanonicalModelID)
		item := byKey[key]
		if item == nil {
			item = &aggregate{siteID: log.SiteID, modelID: log.CanonicalModelID}
			byKey[key] = item
		}
		item.latency = append(item.latency, log.LatencyMS.Int64)
		item.latencyF = append(item.latencyF, float64(log.LatencyMS.Int64))
	}
	rows := make([]HighLatencyItem, 0, len(byKey))
	for _, item := range byKey {
		if len(item.latency) == 0 {
			continue
		}
		sum := int64(0)
		for _, value := range item.latency {
			sum += value
		}
		siteName := ""
		if item.siteID.Valid {
			siteName = sites[item.siteID.UUID].Name
		}
		modelKey := "unknown"
		if item.modelID.Valid {
			if model := models[item.modelID.UUID]; model.ModelKey != "" {
				modelKey = model.ModelKey
			}
		}
		rows = append(rows, HighLatencyItem{
			SiteID:       nullableUUIDString(item.siteID),
			SiteName:     siteName,
			ModelID:      nullableUUIDString(item.modelID),
			ModelKey:     modelKey,
			RequestCount: int64(len(item.latency)),
			AvgLatencyMS: float64(sum) / float64(len(item.latency)),
			P95LatencyMS: percentileCont(item.latencyF, 0.95),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].P95LatencyMS != rows[j].P95LatencyMS {
			return rows[i].P95LatencyMS > rows[j].P95LatencyMS
		}
		return rows[i].AvgLatencyMS > rows[j].AvgLatencyMS
	})
	if len(rows) > highLatencyTopN {
		rows = rows[:highLatencyTopN]
	}
	items := make([]HighLatencyItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, row)
	}
	return items, nil
}

func (s *Service) attentionItems(ctx context.Context, window timeWindow, cooldowns []CooldownItem, insufficient []InsufficientCandidateItem, windows map[string]OverviewWindow, latency []HighLatencyItem) ([]AttentionItem, error) {
	items := make([]AttentionItem, 0)
	updatedAt := window.format(window.Now, time.RFC3339)
	activeSites, err := s.activeSitesByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard attention active sites: %w", err)
	}

	items = append(items, routeCandidateAttentionItems(insufficient, updatedAt)...)

	for _, cooldown := range cooldowns {
		scope := cooldown.Scope
		if scope == "" {
			scope = "site"
		}
		action := AttentionAction{
			Type:   "open_sites",
			Params: map[string]string{"site_id": cooldown.SiteID},
		}
		if cooldown.CanonicalModel != nil && *cooldown.CanonicalModel != "" {
			action = AttentionAction{
				Type:   "open_routes",
				Params: map[string]string{"model_key": *cooldown.CanonicalModel},
			}
		}
		if cooldown.SiteCredentialID != nil {
			action = AttentionAction{
				Type:   "open_site_api_keys",
				Params: map[string]string{"site_id": cooldown.SiteID},
			}
		}
		items = append(items, AttentionItem{
			ID:       "cooldown:" + cooldown.ID,
			Severity: "warning",
			Type:     "active_cooldown",
			Subject: map[string]any{
				"site_id":            cooldown.SiteID,
				"site_name":          cooldown.SiteName,
				"site_model_id":      cooldown.SiteModelID,
				"site_credential_id": cooldown.SiteCredentialID,
				"credential_name":    cooldown.CredentialName,
				"model_key":          cooldown.CanonicalModel,
				"scope":              scope,
			},
			UpdatedAt: updatedAt,
			Metrics: map[string]any{
				"reason":            cooldown.Reason,
				"source":            cooldown.Source,
				"remaining_seconds": cooldown.RemainingSeconds,
			},
			Action: action,
		})
	}

	healthItems, err := s.unhealthySiteAttention(ctx, updatedAt)
	if err != nil {
		return nil, err
	}
	items = append(items, healthItems...)

	missingPricing, err := s.missingPricingAttention(ctx, updatedAt)
	if err != nil {
		return nil, err
	}
	if missingPricing != nil {
		items = append(items, *missingPricing)
	}

	if sevenDay, ok := windows["7"]; ok {
		items = append(items, failureRateAttention(sevenDay, updatedAt, activeSites)...)
		if rateLimit := rateLimitAttention(sevenDay, updatedAt); rateLimit != nil {
			items = append(items, *rateLimit)
		}
	}
	items = append(items, latencyAttention(latency, updatedAt)...)

	sortAttentionItems(items)
	return selectAttentionItems(items), nil
}

func routeCandidateAttentionItems(insufficient []InsufficientCandidateItem, updatedAt string) []AttentionItem {
	items := make([]AttentionItem, 0)
	for _, candidate := range insufficient {
		if candidate.RequestCount24h == 0 {
			continue
		}
		itemType := "low_route_candidates"
		severity := "warning"
		suffix := "low_candidates"
		if candidate.EligibleCount == 0 {
			itemType = "no_route_candidates"
			severity = "critical"
			suffix = "no_candidates"
		}
		items = append(items, AttentionItem{
			ID:       "route:" + candidate.ModelKey + ":" + suffix,
			Severity: severity,
			Type:     itemType,
			Subject: map[string]any{
				"canonical_model_id": candidate.CanonicalModelID,
				"model_key":          candidate.ModelKey,
			},
			UpdatedAt: updatedAt,
			Metrics: map[string]any{
				"eligible_count":    candidate.EligibleCount,
				"cooldown_count":    candidate.CooldownCount,
				"site_model_count":  candidate.SiteModelCount,
				"site_count":        candidate.SiteCount,
				"request_count_24h": candidate.RequestCount24h,
			},
			Action: AttentionAction{
				Type:   "open_routes",
				Params: map[string]string{"model_key": candidate.ModelKey},
			},
		})
	}
	return items
}

func sortAttentionItems(items []AttentionItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left := attentionSeverityRank(items[i].Severity)
		right := attentionSeverityRank(items[j].Severity)
		if left != right {
			return left < right
		}
		leftType := attentionTypeRank(items[i].Type)
		rightType := attentionTypeRank(items[j].Type)
		if leftType != rightType {
			return leftType < rightType
		}
		return items[i].ID < items[j].ID
	})
}

func selectAttentionItems(items []AttentionItem) []AttentionItem {
	if len(items) <= attentionLimit {
		return items
	}
	selected := make([]AttentionItem, 0, attentionLimit)
	deferred := make([]AttentionItem, 0, len(items)-attentionLimit)
	countByType := map[string]int{}

	for _, item := range items {
		if len(selected) < attentionLimit && countByType[item.Type] < attentionInitialTypeLimit {
			selected = append(selected, item)
			countByType[item.Type]++
			continue
		}
		deferred = append(deferred, item)
	}
	for _, item := range deferred {
		if len(selected) >= attentionLimit {
			break
		}
		selected = append(selected, item)
	}
	sortAttentionItems(selected)
	return selected
}

func (s *Service) unhealthySiteAttention(ctx context.Context, updatedAt string) ([]AttentionItem, error) {
	var states []store.SiteHealthState
	if err := s.db.DB().WithContext(ctx).Where(&store.SiteHealthState{Status: "unhealthy"}).Find(&states).Error; err != nil {
		return nil, fmt.Errorf("dashboard unhealthy site attention: %w", err)
	}
	sites, err := s.sitesByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard unhealthy site attention: %w", err)
	}
	type row struct {
		site                store.Site
		status              store.SiteHealthState
		consecutiveFailures int
		checkedAt           time.Time
	}
	rows := make([]row, 0)
	for _, state := range states {
		site := sites[state.SiteID]
		if !site.Enabled || state.Status != "unhealthy" {
			continue
		}
		checkedAt := time.Time{}
		if state.CheckedAt.Valid {
			checkedAt = state.CheckedAt.Time
		}
		rows = append(rows, row{site: site, status: state, consecutiveFailures: state.ConsecutiveFailures, checkedAt: checkedAt})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].consecutiveFailures != rows[j].consecutiveFailures {
			return rows[i].consecutiveFailures > rows[j].consecutiveFailures
		}
		return rows[i].checkedAt.After(rows[j].checkedAt)
	})
	if len(rows) > 5 {
		rows = rows[:5]
	}
	items := make([]AttentionItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, AttentionItem{
			ID:       "site:" + row.site.ID.String() + ":unhealthy",
			Severity: "critical",
			Type:     "site_unhealthy",
			Subject: map[string]any{
				"site_id":   row.site.ID.String(),
				"site_name": row.site.Name,
				"site_slug": row.site.Slug,
			},
			UpdatedAt: updatedAt,
			Metrics: map[string]any{
				"health_status":        row.status.Status,
				"consecutive_failures": row.status.ConsecutiveFailures,
				"message":              nullableStringPtr(row.status.Message),
				"checked_at":           nullTimeString(row.status.CheckedAt, s.timeZone),
			},
			Action: AttentionAction{
				Type:   "open_sites",
				Params: map[string]string{"site_id": row.site.ID.String()},
			},
		})
	}
	return items, nil
}

func (s *Service) missingPricingAttention(ctx context.Context, updatedAt string) (*AttentionItem, error) {
	var siteModels []store.SiteModel
	if err := s.db.DB().WithContext(ctx).Where(&store.SiteModel{Status: "active"}).Find(&siteModels).Error; err != nil {
		return nil, fmt.Errorf("dashboard missing pricing attention: %w", err)
	}
	sites, err := s.sitesByID(ctx)
	if err != nil {
		return nil, fmt.Errorf("dashboard missing pricing attention: %w", err)
	}
	var pricings []store.SiteModelPricing
	if err := s.db.DB().WithContext(ctx).Where(&store.SiteModelPricing{Available: true}).Find(&pricings).Error; err != nil {
		return nil, fmt.Errorf("dashboard missing pricing attention: %w", err)
	}
	pricingsBySiteModelID := map[uuid.UUID][]store.SiteModelPricing{}
	for _, pricing := range pricings {
		if pricing.SiteModelID.Valid {
			pricingsBySiteModelID[pricing.SiteModelID.UUID] = append(pricingsBySiteModelID[pricing.SiteModelID.UUID], pricing)
		}
	}
	var row struct {
		MissingCount int64
		ModelCount   int64
		SiteCount    int64
	}
	modelIDs := map[uuid.UUID]struct{}{}
	siteIDs := map[uuid.UUID]struct{}{}
	for _, siteModel := range siteModels {
		site := sites[siteModel.SiteID]
		if isNewAPISite(site.SiteType) || !site.Enabled || site.Status != "active" || siteModel.Status != "active" {
			continue
		}
		if hasAvailablePricing(pricingsBySiteModelID[siteModel.ID]) {
			continue
		}
		row.MissingCount++
		siteIDs[siteModel.SiteID] = struct{}{}
		if siteModel.CanonicalID.Valid {
			modelIDs[siteModel.CanonicalID.UUID] = struct{}{}
		}
	}
	row.ModelCount = int64(len(modelIDs))
	row.SiteCount = int64(len(siteIDs))
	if row.MissingCount == 0 {
		return nil, nil
	}
	return &AttentionItem{
		ID:       "pricing:missing",
		Severity: "info",
		Type:     "missing_model_pricing",
		Metrics: map[string]any{
			"missing_count": row.MissingCount,
			"model_count":   row.ModelCount,
			"site_count":    row.SiteCount,
		},
		UpdatedAt: updatedAt,
		Action: AttentionAction{
			Type:   "open_model_prices",
			Params: map[string]string{"pricing_status": "missing"},
		},
	}, nil
}

func failureRateAttention(window OverviewWindow, updatedAt string, activeSites map[uuid.UUID]store.Site) []AttentionItem {
	items := make([]AttentionItem, 0)
	for _, site := range window.SiteCostSummary {
		siteID, ok := parseActiveDashboardSiteID(site.SiteID, activeSites)
		if !ok {
			continue
		}
		if site.RequestCount < 5 || site.SuccessRate == nil || *site.SuccessRate >= 0.8 {
			continue
		}
		severity := "warning"
		if site.RequestCount >= failureRateCriticalRequestN && *site.SuccessRate < failureRateCriticalSuccessMax {
			severity = "critical"
		}
		items = append(items, AttentionItem{
			ID:       "site:" + site.SiteID + ":failure_rate",
			Severity: severity,
			Type:     "high_failure_rate",
			Subject: map[string]any{
				"site_id":   siteID.String(),
				"site_name": site.SiteName,
				"site_type": site.SiteType,
			},
			UpdatedAt: updatedAt,
			Metrics: map[string]any{
				"success_rate":  site.SuccessRate,
				"request_count": site.RequestCount,
				"success_count": site.SuccessCount,
			},
			Action: AttentionAction{
				Type:   "open_requests",
				Params: map[string]string{"site_id": siteID.String(), "success": "false"},
			},
		})
	}
	return items
}

func parseActiveDashboardSiteID(siteID string, activeSites map[uuid.UUID]store.Site) (uuid.UUID, bool) {
	parsed, err := uuid.Parse(strings.TrimSpace(siteID))
	if err != nil || parsed == uuid.Nil {
		return uuid.Nil, false
	}
	site, ok := activeSites[parsed]
	if !ok || site.ID == uuid.Nil {
		return uuid.Nil, false
	}
	return parsed, true
}

func rateLimitAttention(window OverviewWindow, updatedAt string) *AttentionItem {
	var count int64
	for _, failure := range window.FailureReasons {
		if failure.Reason == "rate_limit_exceeded" {
			count = failure.RequestCount
			break
		}
	}
	if count == 0 {
		return nil
	}
	return &AttentionItem{
		ID:       "gateway:rate_limit_exceeded",
		Severity: "warning",
		Type:     "rate_limit_exceeded",
		Metrics: map[string]any{
			"request_count": count,
		},
		UpdatedAt: updatedAt,
		Action: AttentionAction{
			Type:   "open_requests",
			Params: map[string]string{"error_type": "rate_limit_exceeded"},
		},
	}
}

func latencyAttention(latency []HighLatencyItem, updatedAt string) []AttentionItem {
	items := make([]AttentionItem, 0)
	for _, item := range latency {
		if len(items) >= 3 || item.ModelKey == "unknown" || item.RequestCount < 3 || item.P95LatencyMS < highLatencyP95MS {
			continue
		}
		subject := map[string]any{
			"model_id":  item.ModelID,
			"model_key": item.ModelKey,
		}
		params := map[string]string{
			"model_key": item.ModelKey,
		}
		if item.SiteID != nil && *item.SiteID != "" {
			subject["site_id"] = *item.SiteID
			subject["site_name"] = item.SiteName
			params["site_id"] = *item.SiteID
		}
		severity := "warning"
		if item.P95LatencyMS >= highLatencyCriticalP95MS {
			severity = "critical"
		}
		items = append(items, AttentionItem{
			ID:        "latency:" + item.ModelKey + ":" + defaultStringPtr(item.SiteID, "unknown"),
			Severity:  severity,
			Type:      "high_latency",
			Subject:   subject,
			UpdatedAt: updatedAt,
			Metrics: map[string]any{
				"request_count":  item.RequestCount,
				"avg_latency_ms": item.AvgLatencyMS,
				"p95_latency_ms": item.P95LatencyMS,
			},
			Action: AttentionAction{
				Type:   "open_requests",
				Params: params,
			},
		})
	}
	return items
}

func attentionSeverityRank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}

func attentionTypeRank(itemType string) int {
	switch itemType {
	case "no_route_candidates":
		return 0
	case "active_cooldown":
		return 1
	case "site_unhealthy":
		return 2
	case "high_failure_rate":
		return 3
	case "rate_limit_exceeded":
		return 4
	case "low_route_candidates":
		return 5
	case "missing_model_pricing":
		return 6
	case "high_latency":
		return 7
	default:
		return 99
	}
}

func nullTimeString(value sql.NullTime, timeZones ...config.TimeZone) *string {
	if !value.Valid {
		return nil
	}
	text := serviceTimeZone(timeZones...).Format(value.Time, time.RFC3339)
	return &text
}

func defaultStringPtr(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}

func nullFloat(value sql.NullFloat64) float64 {
	if value.Valid {
		return value.Float64
	}
	return 0
}

func nullInt(value sql.NullInt64) int64 {
	if value.Valid {
		return value.Int64
	}
	return 0
}

func nullIntPtr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullableUUIDString(value uuid.NullUUID) *string {
	if !value.Valid || value.UUID == uuid.Nil {
		return nil
	}
	text := value.UUID.String()
	return &text
}

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	return &value.String
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func nullableUUIDKey(value uuid.NullUUID) string {
	if !value.Valid || value.UUID == uuid.Nil {
		return ""
	}
	return value.UUID.String()
}

type dashboardSiteSnapshot struct {
	siteID       uuid.NullUUID
	siteName     string
	siteSlug     string
	siteType     string
	siteKey      string
	aggregateKey string
}

func summaryDate(value time.Time, window timeWindow) string {
	return value.In(window.Location).Format("2006-01-02")
}

func resolveSummarySiteSnapshot(row store.RequestUsageDailySummary, sites map[uuid.UUID]store.Site) dashboardSiteSnapshot {
	if row.SiteID.Valid && row.SiteID.UUID != uuid.Nil {
		if site, ok := sites[row.SiteID.UUID]; ok && site.ID != uuid.Nil {
			return dashboardSiteSnapshot{
				siteID:       uuid.NullUUID{UUID: site.ID, Valid: true},
				siteName:     site.Name,
				siteSlug:     site.Slug,
				siteType:     site.SiteType,
				siteKey:      defaultString(defaultString(site.Name, site.Slug), site.ID.String()),
				aggregateKey: "site:" + site.ID.String(),
			}
		}
		return dashboardSiteSnapshot{
			siteID:       row.SiteID,
			siteName:     row.SiteName,
			siteSlug:     row.SiteSlug,
			siteType:     row.SiteType,
			siteKey:      defaultString(defaultString(row.SiteName, row.SiteSlug), row.SiteID.UUID.String()),
			aggregateKey: "site:" + row.SiteID.UUID.String(),
		}
	}
	if row.SiteName != "" || row.SiteSlug != "" || row.SiteType != "" {
		aggregateKey := ""
		if row.SiteSlug != "" {
			aggregateKey = "snapshot-slug:" + row.SiteSlug
		} else if row.SiteName != "" {
			aggregateKey = "snapshot-name:" + row.SiteName
		} else {
			aggregateKey = "snapshot-type:" + row.SiteType
		}
		return dashboardSiteSnapshot{
			siteName:     row.SiteName,
			siteSlug:     row.SiteSlug,
			siteType:     row.SiteType,
			siteKey:      defaultString(defaultString(row.SiteName, row.SiteSlug), "unknown"),
			aggregateKey: aggregateKey,
		}
	}
	if row.SiteKey != "" && row.SiteKey != summaryNoneKey {
		return dashboardSiteSnapshot{siteKey: row.SiteKey, aggregateKey: row.SiteKey}
	}
	return dashboardSiteSnapshot{siteKey: "unknown", aggregateKey: "unknown"}
}

func dashboardSiteIDString(value uuid.NullUUID, fallback string) string {
	if value.Valid && value.UUID != uuid.Nil {
		return value.UUID.String()
	}
	return fallback
}

func (s *Service) siteList(ctx context.Context) ([]store.Site, error) {
	var sites []store.Site
	if err := s.db.DB().WithContext(ctx).Find(&sites).Error; err != nil {
		return nil, err
	}
	return sites, nil
}

func (s *Service) activeSiteList(ctx context.Context) ([]store.Site, error) {
	sites, err := s.siteList(ctx)
	if err != nil {
		return nil, err
	}
	return filterDashboardUptimeSites(sites), nil
}

func (s *Service) activeSitesByID(ctx context.Context) (map[uuid.UUID]store.Site, error) {
	sites, err := s.activeSiteList(ctx)
	if err != nil {
		return nil, err
	}
	result := map[uuid.UUID]store.Site{}
	for _, site := range sites {
		result[site.ID] = site
	}
	return result, nil
}

func filterDashboardUptimeSites(sites []store.Site) []store.Site {
	active := sites[:0]
	for _, site := range sites {
		if store.SiteDeleted(site) || !site.Enabled {
			continue
		}
		active = append(active, site)
	}
	return active
}

func (s *Service) sitesByID(ctx context.Context) (map[uuid.UUID]store.Site, error) {
	sites, err := s.siteList(ctx)
	if err != nil {
		return nil, err
	}
	result := map[uuid.UUID]store.Site{}
	for _, site := range sites {
		result[site.ID] = site
	}
	return result, nil
}

func (s *Service) canonicalModelsByID(ctx context.Context) (map[uuid.UUID]store.CanonicalModel, error) {
	var models []store.CanonicalModel
	if err := s.db.DB().WithContext(ctx).Find(&models).Error; err != nil {
		return nil, err
	}
	result := map[uuid.UUID]store.CanonicalModel{}
	for _, model := range models {
		result[model.ID] = model
	}
	return result, nil
}

func (s *Service) siteModelsByID(ctx context.Context) (map[uuid.UUID]store.SiteModel, error) {
	var models []store.SiteModel
	if err := s.db.DB().WithContext(ctx).Find(&models).Error; err != nil {
		return nil, err
	}
	result := map[uuid.UUID]store.SiteModel{}
	for _, model := range models {
		result[model.ID] = model
	}
	return result, nil
}

func (s *Service) siteCredentialsByID(ctx context.Context) (map[uuid.UUID]store.SiteCredential, error) {
	var credentials []store.SiteCredential
	if err := s.db.DB().WithContext(ctx).Find(&credentials).Error; err != nil {
		return nil, err
	}
	result := map[uuid.UUID]store.SiteCredential{}
	for _, credential := range credentials {
		result[credential.ID] = credential
	}
	return result, nil
}

func (s *Service) siteAPIKeyStatesByCredentialID(ctx context.Context) (map[uuid.UUID]store.SiteAPIKeyState, error) {
	var states []store.SiteAPIKeyState
	if err := s.db.DB().WithContext(ctx).Find(&states).Error; err != nil {
		return nil, err
	}
	result := map[uuid.UUID]store.SiteAPIKeyState{}
	for _, state := range states {
		result[state.SiteCredentialID] = state
	}
	return result, nil
}

func (s *Service) requestLogsSince(ctx context.Context, since time.Time) ([]store.RequestLog, error) {
	var logs []store.RequestLog
	if err := s.db.DB().WithContext(ctx).
		Clauses(timeGteClause("created_at", since)).
		Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func timeGteClause(column string, value time.Time) clause.Where {
	return clause.Where{Exprs: []clause.Expression{
		clause.Gte{Column: clause.Column{Name: column}, Value: value},
	}}
}

func timeRangeClause(column string, start time.Time, end time.Time) clause.Where {
	return clause.Where{Exprs: []clause.Expression{
		clause.Gte{Column: clause.Column{Name: column}, Value: start},
		clause.Lt{Column: clause.Column{Name: column}, Value: end},
	}}
}

func percentileCont(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}
	position := percentile * float64(len(values)-1)
	lower := int(position)
	upper := lower + 1
	lowerValue := selectFloat64(values, lower)
	if upper >= len(values) {
		return lowerValue
	}
	upperValue := selectFloat64(values, upper)
	fraction := position - float64(lower)
	return lowerValue + (upperValue-lowerValue)*fraction
}

func selectFloat64(values []float64, index int) float64 {
	left := 0
	right := len(values) - 1
	for {
		if left == right {
			return values[left]
		}
		pivot := partitionFloat64(values, left, right, (left+right)/2)
		switch {
		case index == pivot:
			return values[pivot]
		case index < pivot:
			right = pivot - 1
		default:
			left = pivot + 1
		}
	}
}

func partitionFloat64(values []float64, left int, right int, pivot int) int {
	pivotValue := values[pivot]
	values[pivot], values[right] = values[right], values[pivot]
	store := left
	for index := left; index < right; index++ {
		if values[index] < pivotValue {
			values[store], values[index] = values[index], values[store]
			store++
		}
	}
	values[right], values[store] = values[store], values[right]
	return store
}

func hasAvailablePricing(items []store.SiteModelPricing) bool {
	for _, item := range items {
		if !item.Available {
			continue
		}
		switch item.BillingType {
		case "tokens":
			if item.InputValue.Valid || item.OutputValue.Valid {
				return true
			}
		case "per_request":
			if item.PerRequestValue.Valid {
				return true
			}
		}
	}
	return false
}

func isNewAPISite(siteType string) bool {
	switch siteType {
	case "newapi", "new_api":
		return true
	default:
		return false
	}
}

func uptimeHourKey(value time.Time) string {
	return value.Format("2006-01-02 15:00:00")
}

func jsonMap(raw store.JSON) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func epaperCodexQuotaPayload(raw store.JSON) map[string]any {
	payload := jsonMap(raw)
	if len(payload) == 0 {
		return nil
	}
	user, _ := payload["user"].(map[string]any)
	if len(user) > 0 {
		if quota, _ := user["quota"].(map[string]any); len(quota) > 0 {
			return quota
		}
	}
	if quota, _ := payload["quota"].(map[string]any); len(quota) > 0 {
		return quota
	}
	return nil
}

type epaperQuotaAccumulator struct {
	accountCount int
	fiveHour     epaperQuotaWindowAccumulator
	weekly       epaperQuotaWindowAccumulator
}

type epaperQuotaWindowAccumulator struct {
	remainingSum int
	remainingN   int
	resetAt      *int64
}

func summarizeEpaperCodexQuota(items []map[string]any) EpaperCodexQuota {
	acc := epaperQuotaAccumulator{}
	for _, item := range items {
		if summarizeEpaperCodexQuotaItem(item, &acc) {
			acc.accountCount++
		}
	}
	return EpaperCodexQuota{
		AccountCount: acc.accountCount,
		FiveHour:     acc.fiveHour.summary(),
		Weekly:       acc.weekly.summary(),
	}
}

func summarizeEpaperCodexQuotaItem(item map[string]any, acc *epaperQuotaAccumulator) bool {
	counted := false
	if window, _ := item["five_hour"].(map[string]any); len(window) > 0 {
		if acc.fiveHour.add(window) {
			counted = true
		}
	}
	if window, _ := item["weekly"].(map[string]any); len(window) > 0 {
		if acc.weekly.add(window) {
			counted = true
		}
	}
	return counted
}

func (acc *epaperQuotaWindowAccumulator) add(window map[string]any) bool {
	counted := false
	if remaining := intPtrFromAny(window["remaining_percent"]); remaining != nil {
		acc.remainingSum += *remaining
		acc.remainingN++
		counted = true
	}
	if resetAt := unixSecondsPtrFromAny(window["reset_at"]); resetAt != nil && *resetAt > 0 {
		if acc.resetAt == nil || *resetAt < *acc.resetAt {
			acc.resetAt = resetAt
		}
	}
	return counted
}

func (acc epaperQuotaWindowAccumulator) summary() EpaperQuotaWindow {
	var remaining *int
	if acc.remainingN > 0 {
		value := int(math.Round(float64(acc.remainingSum) / float64(acc.remainingN)))
		remaining = &value
	}
	return EpaperQuotaWindow{
		RemainingPercent: remaining,
		ResetAt:          acc.resetAt,
	}
}

func intPtrFromAny(value any) *int {
	switch typed := value.(type) {
	case nil:
		return nil
	case int:
		return &typed
	case int64:
		value := int(typed)
		return &value
	case float64:
		value := int(math.Round(typed))
		return &value
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			value := int(parsed)
			return &value
		}
		if parsed, err := typed.Float64(); err == nil {
			value := int(math.Round(parsed))
			return &value
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		var parsed json.Number = json.Number(text)
		return intPtrFromAny(parsed)
	}
	return nil
}

func unixSecondsPtrFromAny(value any) *int64 {
	switch typed := value.(type) {
	case nil:
		return nil
	case int:
		value := int64(typed)
		return &value
	case int64:
		return &typed
	case float64:
		value := int64(math.Round(typed))
		return &value
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return &parsed
		}
		if parsed, err := typed.Float64(); err == nil {
			value := int64(math.Round(parsed))
			return &value
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			value := parsed.Unix()
			return &value
		}
		var number json.Number = json.Number(text)
		return unixSecondsPtrFromAny(number)
	}
	return nil
}
