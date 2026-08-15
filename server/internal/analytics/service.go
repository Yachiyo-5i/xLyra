package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

const (
	GroupByNone      = "none"
	GroupBySite      = "site"
	GroupByModel     = "model"
	GroupBySiteModel = "site_model"
	GroupByAPIKey    = "api_key"
	GroupByEndpoint  = "endpoint"
	GroupByErrorType = "error_type"

	defaultGroupBy   = GroupByModel
	defaultRangeDays = 30
	// 上限取值约为十年，用于支撑前端"全部"时间范围；天级聚合下内存可控。
	maxRangeDays     = 3660
	summaryNoneKey   = "none"
	unknownLabel     = "unknown"
	defaultCurrency  = "USD"
	totalSeriesKey   = "__total__"
	totalSeriesLabel = "总计"
	otherSeriesKey   = "other"
	otherSeriesLabel = "其他"
)

type Service struct {
	db       *store.Store
	timeZone config.TimeZone
}

func NewService(db *store.Store, timeZones ...config.TimeZone) *Service {
	return &Service{db: db, timeZone: config.TimeZoneOrDefault(timeZones...)}
}

func ValidGroupBy(value string) bool {
	switch strings.TrimSpace(value) {
	case GroupByNone, GroupBySite, GroupByModel, GroupBySiteModel, GroupByAPIKey, GroupByEndpoint, GroupByErrorType:
		return true
	default:
		return false
	}
}

type UsageParams struct {
	Now       time.Time
	From      time.Time
	To        time.Time
	GroupBy   string
	SiteIDs   []uuid.UUID
	ModelKeys []string
	APIKeyIDs []uuid.UUID
	Success   *bool
	Currency  string
}

func (s *Service) Usage(ctx context.Context, params UsageParams) (UsageAnalytics, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return UsageAnalytics{}, fmt.Errorf("analytics store is not initialized")
	}
	timeZone := config.TimeZoneOrDefault(s.timeZone)
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = timeZone.In(now)
	todayStart := timeZone.StartOfDay(now)

	from, to, days := resolveRange(timeZone, todayStart, params.From, params.To)
	groupBy := strings.TrimSpace(params.GroupBy)
	if groupBy == "" {
		groupBy = defaultGroupBy
	}
	if !ValidGroupBy(groupBy) {
		return UsageAnalytics{}, fmt.Errorf("analytics usage: invalid group_by %q", groupBy)
	}

	oldestBucket, err := store.NewRequestUsageSummaryRepository(s.db.DB()).OldestBucketStart(ctx)
	if err != nil {
		return UsageAnalytics{}, fmt.Errorf("analytics oldest bucket: %w", err)
	}

	granularity := "day"
	// 单日且该单日是今天时启用小时粒度（今天明细数据可用 ListFromDetailsByHour 按小时聚合）
	// 历史单日只有天级汇总行，强制用 day 粒度
	if days == 1 && !to.Before(todayStart) {
		granularity = "hour"
	}

	rows, err := s.loadRows(ctx, timeZone, todayStart, now, from, to, params, granularity)
	if err != nil {
		return UsageAnalytics{}, err
	}
	availableCurrencies, costByCurrency := currencyOverview(rows)
	displayCurrency := pickDisplayCurrency(params.Currency, availableCurrencies, costByCurrency)
	filteredRows := filterRowsByCurrency(rows, displayCurrency)

	prevFrom, prevTo := previousRange(from, days)
	previousRows, err := s.loadRows(ctx, timeZone, todayStart, now, prevFrom, prevTo, params, granularity)
	if err != nil {
		return UsageAnalytics{}, err
	}
	previousFiltered := filterRowsByCurrency(previousRows, displayCurrency)

	apiKeys, err := s.apiKeysByID(ctx, filteredRows)
	if err != nil {
		return UsageAnalytics{}, fmt.Errorf("analytics usage api keys: %w", err)
	}

	// 热力图用的 365 天 API Key 时序：固定用 365 天窗口，不受当前 from/to 筛选影响
	contributionDays := 365
	contributionFrom := todayStart.AddDate(0, 0, -(contributionDays - 1))
	contributionParams := UsageParams{
		APIKeyIDs: params.APIKeyIDs,
		Success:   params.Success,
	}
	contributionRows, err := s.loadRows(ctx, timeZone, todayStart, now, contributionFrom, now, contributionParams, "day")
	if err != nil {
		return UsageAnalytics{}, fmt.Errorf("analytics api key contributions: %w", err)
	}
	contributions := buildAPIKeyContributions(contributionRows, apiKeys, timeZone, contributionFrom)

	return UsageAnalytics{
		Meta: UsageMeta{
			From:                timeZone.Format(from, "2006-01-02"),
			To:                  timeZone.Format(to, "2006-01-02"),
			Days:                days,
			Timezone:            timeZone.Name,
			GeneratedAt:         timeZone.Format(now, time.RFC3339),
			GroupBy:             groupBy,
			Currency:            displayCurrency,
			AvailableCurrencies: availableCurrencies,
			Filters: UsageFilters{
				SiteIDs:   uuidFilterStrings(params.SiteIDs),
				ModelKeys: stringFilterValues(params.ModelKeys),
				APIKeyIDs: uuidFilterStrings(params.APIKeyIDs),
				Success:   params.Success,
			},
			DataFrom: func() *string {
				if oldestBucket == nil {
					return nil
				}
				v := timeZone.Format(*oldestBucket, "2006-01-02")
				return &v
			}(),
			Granularity: granularity,
		},
		Totals:              buildTotals(filteredRows, previousFiltered, costByCurrency, timeZone, prevFrom, prevTo),
		Breakdowns:          buildBreakdowns(filteredRows, apiKeys),
		Series:              buildSeries(filteredRows, groupBy, apiKeys, timeZone, granularity),
		APIKeyContributions: contributions,
	}, nil
}

func (s *Service) loadRows(ctx context.Context, timeZone config.TimeZone, todayStart time.Time, now time.Time, from time.Time, to time.Time, params UsageParams, granularity string) ([]store.RequestUsageDailySummary, error) {
	repo := store.NewRequestUsageSummaryRepository(s.db.DB())
	endExclusive := to.AddDate(0, 0, 1)
	historyEnd := endExclusive
	if historyEnd.After(todayStart) {
		historyEnd = todayStart
	}
	rows := make([]store.RequestUsageDailySummary, 0)
	if from.Before(historyEnd) {
		historyFrom := from
		query := store.RequestUsageSummaryQuery{
			TimeZone:  timeZone.Name,
			From:      &historyFrom,
			To:        &historyEnd,
			SiteIDs:   params.SiteIDs,
			APIKeyIDs: params.APIKeyIDs,
			ModelKeys: params.ModelKeys,
			Success:   params.Success,
		}
		historical, err := repo.List(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("analytics usage summaries: %w", err)
		}
		rows = append(rows, historical...)
	}
	if !todayStart.Before(from) && todayStart.Before(endExclusive) {
		var details []store.RequestUsageDailySummary
		var err error
		if granularity == "hour" {
			details, err = repo.ListFromDetailsByHour(ctx, todayStart, now, timeZone)
		} else {
			details, err = repo.ListFromDetails(ctx, todayStart, now, timeZone)
		}
		if err != nil {
			return nil, fmt.Errorf("analytics usage details: %w", err)
		}
		for _, row := range details {
			if matchesUsageFilters(row, params) {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func (s *Service) apiKeysByID(ctx context.Context, rows []store.RequestUsageDailySummary) (map[uuid.UUID]store.APIKey, error) {
	ids := make([]uuid.UUID, 0)
	seen := map[uuid.UUID]struct{}{}
	for _, row := range rows {
		if !row.APIKeyID.Valid || row.APIKeyID.UUID == uuid.Nil {
			continue
		}
		if _, ok := seen[row.APIKeyID.UUID]; ok {
			continue
		}
		seen[row.APIKeyID.UUID] = struct{}{}
		ids = append(ids, row.APIKeyID.UUID)
	}
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

func matchesUsageFilters(row store.RequestUsageDailySummary, params UsageParams) bool {
	if len(params.SiteIDs) > 0 {
		if !row.SiteID.Valid || !uuidInList(row.SiteID.UUID, params.SiteIDs) {
			return false
		}
	}
	if len(params.APIKeyIDs) > 0 {
		if !row.APIKeyID.Valid || !uuidInList(row.APIKeyID.UUID, params.APIKeyIDs) {
			return false
		}
	}
	if len(params.ModelKeys) > 0 && !stringInList(row.CanonicalModelKey, params.ModelKeys) {
		return false
	}
	if params.Success != nil && row.Success != *params.Success {
		return false
	}
	return true
}

func resolveRange(timeZone config.TimeZone, todayStart time.Time, from time.Time, to time.Time) (time.Time, time.Time, int) {
	if to.IsZero() || to.After(todayStart) {
		to = todayStart
	} else {
		to = timeZone.StartOfDay(to)
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -(defaultRangeDays - 1))
	} else {
		from = timeZone.StartOfDay(from)
	}
	if from.After(to) {
		from = to
	}
	days := dayCount(from, to)
	if days > maxRangeDays {
		from = to.AddDate(0, 0, -(maxRangeDays - 1))
		days = maxRangeDays
	}
	return from, to, days
}

func dayCount(from time.Time, to time.Time) int {
	days := int(to.Sub(from).Hours()/24) + 1
	if days < 0 {
		return 0
	}
	return days
}

func previousRange(from time.Time, days int) (time.Time, time.Time) {
	prevTo := from.AddDate(0, 0, -1)
	prevFrom := prevTo.AddDate(0, 0, -(days - 1))
	return prevFrom, prevTo
}

func uuidInList(value uuid.UUID, list []uuid.UUID) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func stringInList(value string, list []string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func uuidFilterStrings(values []uuid.UUID) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.String())
	}
	return result
}

func stringFilterValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}
