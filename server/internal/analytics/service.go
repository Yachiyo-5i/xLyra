package analytics

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

// 与前端站点排序一致，名称比较使用中文排序规则
var analyticsOptionCollator = collate.New(language.SimplifiedChinese)

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
	maxDatasetFacts  = 50000
)

type DatasetTooLargeError struct {
	Facts int
	Limit int
}

func (e DatasetTooLargeError) Error() string {
	return fmt.Sprintf("analytics dataset contains %d facts, limit is %d", e.Facts, e.Limit)
}

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
	Now                  time.Time
	From                 time.Time
	To                   time.Time
	GroupBy              string
	SiteIDs              []uuid.UUID
	ModelKeys            []string
	APIKeyIDs            []uuid.UUID
	Success              *bool
	Currency             string
	IncludeContributions *bool
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

	rows, granularity, err := s.loadRowsForRange(ctx, timeZone, todayStart, now, from, to, days, params)
	if err != nil {
		return UsageAnalytics{}, err
	}
	availableCurrencies, costByCurrency := currencyOverview(rows)
	displayCurrency := pickDisplayCurrency(params.Currency, availableCurrencies, costByCurrency)
	filteredRows := filterRowsByCurrency(rows, displayCurrency)

	prevFrom, prevTo := previousRange(from, days)
	previousRows, err := s.loadRows(ctx, timeZone, todayStart, now, prevFrom, prevTo, params, "day")
	if err != nil {
		return UsageAnalytics{}, err
	}
	previousFiltered := filterRowsByCurrency(previousRows, displayCurrency)

	apiKeys, err := s.apiKeysByID(ctx, filteredRows)
	if err != nil {
		return UsageAnalytics{}, fmt.Errorf("analytics usage api keys: %w", err)
	}
	contributions := []DailyAPIKeyUsagePoint{}
	if params.IncludeContributions == nil || *params.IncludeContributions {
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
		contributions = buildAPIKeyContributions(contributionRows, apiKeys, timeZone, contributionFrom)
	}

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

func (s *Service) Contributions(ctx context.Context, now time.Time, success *bool) (APIKeyContributions, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return APIKeyContributions{}, fmt.Errorf("analytics store is not initialized")
	}
	timeZone := config.TimeZoneOrDefault(s.timeZone)
	if now.IsZero() {
		now = time.Now()
	}
	now = timeZone.In(now)
	todayStart := timeZone.StartOfDay(now)
	from := todayStart.AddDate(0, 0, -364)
	params := UsageParams{Success: success}
	rows, err := s.loadRows(ctx, timeZone, todayStart, now, from, todayStart, params, "day")
	if err != nil {
		return APIKeyContributions{}, fmt.Errorf("analytics api key contributions: %w", err)
	}
	apiKeys, err := s.apiKeysByID(ctx, rows)
	if err != nil {
		return APIKeyContributions{}, fmt.Errorf("analytics contribution api keys: %w", err)
	}
	return APIKeyContributions{
		GeneratedAt: timeZone.Format(now, time.RFC3339),
		From:        timeZone.Format(from, "2006-01-02"),
		To:          timeZone.Format(todayStart, "2006-01-02"),
		Points:      buildAPIKeyContributions(rows, apiKeys, timeZone, from),
	}, nil
}

func (s *Service) Options(ctx context.Context) (AnalyticsOptions, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return AnalyticsOptions{}, fmt.Errorf("analytics store is not initialized")
	}
	sites, err := store.NewSiteRepository(s.db.DB()).ListOptions(ctx)
	if err != nil {
		return AnalyticsOptions{}, fmt.Errorf("analytics site options: %w", err)
	}
	apiKeys, err := store.NewAPIKeyRepository(s.db.DB()).ListOptions(ctx)
	if err != nil {
		return AnalyticsOptions{}, fmt.Errorf("analytics api key options: %w", err)
	}
	states, err := store.NewSiteStateRepository(s.db.DB()).List(ctx)
	if err != nil {
		return AnalyticsOptions{}, fmt.Errorf("analytics site states: %w", err)
	}
	sortAnalyticsSiteOptions(sites, states)
	sortAnalyticsAPIKeyOptions(apiKeys)
	result := AnalyticsOptions{
		Sites:   make([]AnalyticsOption, 0, len(sites)),
		APIKeys: make([]AnalyticsOption, 0, len(apiKeys)),
	}
	for _, site := range sites {
		result.Sites = append(result.Sites, AnalyticsOption{ID: site.ID.String(), Name: site.Name})
	}
	for _, apiKey := range apiKeys {
		result.APIKeys = append(result.APIKeys, AnalyticsOption{ID: apiKey.ID.String(), Name: apiKey.Name})
	}
	return result, nil
}

func sortAnalyticsSiteOptions(sites []store.SiteListOption, states map[uuid.UUID]store.SiteState) {
	sort.SliceStable(sites, func(i, j int) bool {
		aAbnormal := analyticsSiteOptionAbnormal(sites[i], states[sites[i].ID])
		bAbnormal := analyticsSiteOptionAbnormal(sites[j], states[sites[j].ID])
		if aAbnormal != bAbnormal {
			return !aAbnormal
		}
		if sites[i].Enabled != sites[j].Enabled {
			return sites[i].Enabled
		}
		if sites[i].RoutingPriority != sites[j].RoutingPriority {
			return sites[i].RoutingPriority > sites[j].RoutingPriority
		}
		if !sites[i].CreatedAt.Equal(sites[j].CreatedAt) {
			return sites[i].CreatedAt.Before(sites[j].CreatedAt)
		}
		if sites[i].Name != sites[j].Name {
			return analyticsOptionCollator.CompareString(sites[i].Name, sites[j].Name) < 0
		}
		return sites[i].ID.String() < sites[j].ID.String()
	})
}

func sortAnalyticsAPIKeyOptions(apiKeys []store.APIKeyListOption) {
	sort.SliceStable(apiKeys, func(i, j int) bool {
		aActive := apiKeys[i].Status == "active"
		bActive := apiKeys[j].Status == "active"
		if aActive != bActive {
			return aActive
		}
		if !apiKeys[i].CreatedAt.Equal(apiKeys[j].CreatedAt) {
			return apiKeys[i].CreatedAt.Before(apiKeys[j].CreatedAt)
		}
		return apiKeys[i].ID.String() < apiKeys[j].ID.String()
	})
}

func analyticsSiteOptionAbnormal(site store.SiteListOption, state store.SiteState) bool {
	if analyticsOptionStatusAbnormal(site.Status) {
		return true
	}
	if state.ValidationOK.Valid && !state.ValidationOK.Bool {
		return true
	}
	if analyticsOptionStatusAbnormal(state.SyncStatus) {
		return true
	}
	// 与站点列表的 failure_class 口径一致：同步失败且归因于凭据失效时视为异常
	if state.SyncMessage.Valid && strings.TrimSpace(state.SyncMessage.String) != "" &&
		!strings.EqualFold(strings.TrimSpace(state.SyncStatus), "synced") &&
		upstream.ClassifyMessage(state.SyncMessage.String).Class == upstream.FailureCredentialInvalid {
		return true
	}
	return false
}

func analyticsOptionStatusAbnormal(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error", "failed", "unhealthy", "invalid", "unavailable", "degraded":
		return true
	default:
		return false
	}
}

func (s *Service) Dataset(ctx context.Context, params UsageParams) (UsageDataset, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return UsageDataset{}, fmt.Errorf("analytics store is not initialized")
	}
	timeZone := config.TimeZoneOrDefault(s.timeZone)
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = timeZone.In(now)
	todayStart := timeZone.StartOfDay(now)
	from, to, days := resolveRange(timeZone, todayStart, params.From, params.To)
	params.SiteIDs = nil
	params.ModelKeys = nil
	params.APIKeyIDs = nil
	params.Currency = ""
	current, granularity, err := s.loadRowsForRange(ctx, timeZone, todayStart, now, from, to, days, params)
	if err != nil {
		return UsageDataset{}, err
	}
	previousFrom, previousTo := previousRange(from, days)
	previous, err := s.loadRows(ctx, timeZone, todayStart, now, previousFrom, previousTo, params, "day")
	if err != nil {
		return UsageDataset{}, err
	}
	allRows := make([]store.RequestUsageDailySummary, 0, len(current)+len(previous))
	allRows = append(allRows, current...)
	allRows = append(allRows, previous...)
	apiKeys, err := s.apiKeysByID(ctx, allRows)
	if err != nil {
		return UsageDataset{}, fmt.Errorf("analytics dataset api keys: %w", err)
	}
	currentFacts := buildUsageFacts(current, apiKeys, timeZone, granularity)
	previousFacts := buildUsageFacts(previous, apiKeys, timeZone, "day")
	if err := checkDatasetFactLimit(currentFacts, previousFacts); err != nil {
		return UsageDataset{}, err
	}
	oldestBucket, err := store.NewRequestUsageSummaryRepository(s.db.DB()).OldestBucketStart(ctx)
	if err != nil {
		return UsageDataset{}, fmt.Errorf("analytics dataset oldest bucket: %w", err)
	}
	var dataFrom *string
	if oldestBucket != nil {
		value := timeZone.Format(*oldestBucket, "2006-01-02")
		dataFrom = &value
	}
	return UsageDataset{
		Meta: UsageDatasetMeta{
			From:         timeZone.Format(from, "2006-01-02"),
			To:           timeZone.Format(to, "2006-01-02"),
			PreviousFrom: timeZone.Format(previousFrom, "2006-01-02"),
			PreviousTo:   timeZone.Format(previousTo, "2006-01-02"),
			Days:         days,
			Timezone:     timeZone.Name,
			GeneratedAt:  timeZone.Format(now, time.RFC3339),
			DataFrom:     dataFrom,
			Granularity:  granularity,
			FactCount:    len(currentFacts) + len(previousFacts),
			FactLimit:    maxDatasetFacts,
		},
		Current:  currentFacts,
		Previous: previousFacts,
	}, nil
}

func checkDatasetFactLimit(current []UsageFact, previous []UsageFact) error {
	facts := len(current) + len(previous)
	if facts > maxDatasetFacts {
		return DatasetTooLargeError{Facts: facts, Limit: maxDatasetFacts}
	}
	return nil
}

func (s *Service) loadRowsForRange(ctx context.Context, timeZone config.TimeZone, todayStart time.Time, now time.Time, from time.Time, to time.Time, days int, params UsageParams) ([]store.RequestUsageDailySummary, string, error) {
	if days != 1 {
		rows, err := s.loadRows(ctx, timeZone, todayStart, now, from, to, params, "day")
		return rows, "day", err
	}
	if !to.Before(todayStart) {
		rows, err := s.loadRows(ctx, timeZone, todayStart, now, from, to, params, "hour")
		return rows, "hour", err
	}
	repo := store.NewRequestUsageSummaryRepository(s.db.DB())
	dailyRows, err := s.loadRows(ctx, timeZone, todayStart, now, from, to, UsageParams{}, "day")
	if err != nil {
		return nil, "day", err
	}
	filteredDailyRows := filterUsageRows(dailyRows, params)
	cleaned, err := repo.DetailsCleanedForDay(ctx, from, timeZone)
	if err != nil {
		return nil, "day", fmt.Errorf("analytics detail cleanup state: %w", err)
	}
	if cleaned {
		return filteredDailyRows, "day", nil
	}
	detailRows, err := repo.ListFromDetailsByHour(ctx, from, to.AddDate(0, 0, 1), timeZone)
	if err != nil {
		return nil, "day", fmt.Errorf("analytics single-day details: %w", err)
	}
	detailCount := requestCount(detailRows)
	dailyCount := requestCount(dailyRows)
	if hourlyDetailsComplete(dailyCount, detailCount) {
		return filterUsageRows(detailRows, params), "hour", nil
	}
	return filteredDailyRows, "day", nil
}

func hourlyDetailsComplete(dailyCount int64, detailCount int64) bool {
	return detailCount > 0 && (dailyCount == 0 || detailCount == dailyCount)
}

func filterUsageRows(rows []store.RequestUsageDailySummary, params UsageParams) []store.RequestUsageDailySummary {
	filtered := make([]store.RequestUsageDailySummary, 0, len(rows))
	for _, row := range rows {
		if matchesUsageFilters(row, params) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (s *Service) loadRows(ctx context.Context, timeZone config.TimeZone, todayStart time.Time, now time.Time, from time.Time, to time.Time, params UsageParams, granularity string) ([]store.RequestUsageDailySummary, error) {
	repo := store.NewRequestUsageSummaryRepository(s.db.DB())
	endExclusive := to.AddDate(0, 0, 1)
	historyEnd := endExclusive
	if granularity == "hour" && historyEnd.After(todayStart) {
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
	if granularity == "hour" && !todayStart.Before(from) && todayStart.Before(endExclusive) {
		currentHour := timeZone.StartOfHour(now)
		hourFrom := todayStart
		hourTo := currentHour
		hourly, err := repo.ListHourly(ctx, store.RequestUsageSummaryQuery{
			TimeZone:  timeZone.Name,
			From:      &hourFrom,
			To:        &hourTo,
			SiteIDs:   params.SiteIDs,
			APIKeyIDs: params.APIKeyIDs,
			ModelKeys: params.ModelKeys,
			Success:   params.Success,
		})
		if err != nil {
			return nil, fmt.Errorf("analytics hourly summaries: %w", err)
		}
		currentDetails, err := repo.ListFromDetailsByHour(ctx, currentHour, now, timeZone)
		if err != nil {
			return nil, fmt.Errorf("analytics current hour details: %w", err)
		}
		filteredCurrent := make([]store.RequestUsageDailySummary, 0, len(currentDetails))
		for _, row := range currentDetails {
			if matchesUsageFilters(row, params) {
				filteredCurrent = append(filteredCurrent, row)
			}
		}
		dailyFrom := todayStart
		dailyTo := todayStart.AddDate(0, 0, 1)
		daily, err := repo.List(ctx, store.RequestUsageSummaryQuery{
			TimeZone:  timeZone.Name,
			From:      &dailyFrom,
			To:        &dailyTo,
			SiteIDs:   params.SiteIDs,
			APIKeyIDs: params.APIKeyIDs,
			ModelKeys: params.ModelKeys,
			Success:   params.Success,
		})
		if err != nil {
			return nil, fmt.Errorf("analytics daily coverage: %w", err)
		}
		if requestCount(hourly)+requestCount(filteredCurrent) == requestCount(daily) {
			rows = append(rows, hourly...)
			rows = append(rows, filteredCurrent...)
		} else {
			details, err := repo.ListFromDetailsByHour(ctx, todayStart, now, timeZone)
			if err != nil {
				return nil, fmt.Errorf("analytics usage details: %w", err)
			}
			for _, row := range details {
				if matchesUsageFilters(row, params) {
					rows = append(rows, row)
				}
			}
		}
	}
	return rows, nil
}

func requestCount(rows []store.RequestUsageDailySummary) int64 {
	var count int64
	for _, row := range rows {
		count += row.RequestCount
	}
	return count
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
