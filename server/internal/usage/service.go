package usage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

var ErrInvalidChannelSplitQuery = errors.New("invalid channel split query")

type Service struct {
	db       *store.Store
	timeZone config.TimeZone
}

type RequestQuery struct {
	Limit           int
	Page            int
	PageSize        int
	Success         *bool
	SiteID          *uuid.UUID
	APIKeyID        *uuid.UUID
	Search          string
	ModelKey        string
	ErrorType       string
	Endpoint        string
	RequestID       string
	HideWithoutSite bool
	CreatedFrom     *time.Time
	CreatedTo       *time.Time
}

type RequestSummaryResult struct {
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
	Supported                  bool
	UnsupportedReason          string
}

type ChannelSplitQuery struct {
	SiteID            *uuid.UUID
	SiteIDs           []uuid.UUID
	OAuthConnectionID *uuid.UUID
	Range             string
	DateFrom          string
	DateTo            string
}

type ChannelSplitResult struct {
	Summary ChannelSplitSummary `json:"summary"`
	Items   []ChannelSplitItem  `json:"items"`
}

type ChannelSplitSummary struct {
	SiteID            string   `json:"site_id"`
	SiteIDs           []string `json:"site_ids"`
	SiteName          string   `json:"site_name"`
	SiteNames         []string `json:"site_names"`
	SiteSlug          string   `json:"site_slug"`
	SiteType          string   `json:"site_type"`
	TargetLabel       string   `json:"target_label"`
	OAuthConnectionID *string  `json:"oauth_connection_id"`
	OAuthProvider     *string  `json:"oauth_provider"`
	OAuthAccount      *string  `json:"oauth_account"`
	RangeAll          bool     `json:"range_all"`
	DateFrom          string   `json:"date_from"`
	DateTo            string   `json:"date_to"`
	RangeStart        string   `json:"range_start"`
	RangeEnd          string   `json:"range_end"`
	TimeZone          string   `json:"timezone"`
	GeneratedAt       string   `json:"generated_at"`
	RequestCount      int64    `json:"request_count"`
	SuccessCount      int64    `json:"success_count"`
	FailureCount      int64    `json:"failure_count"`
	PromptTokens      int64    `json:"prompt_tokens"`
	CompletionTokens  int64    `json:"completion_tokens"`
	TotalTokens       int64    `json:"total_tokens"`
	EstimatedCost     float64  `json:"estimated_cost"`
	Currency          string   `json:"currency"`
	APIKeyCount       int      `json:"api_key_count"`
}

type ChannelSplitItem struct {
	APIKeyID         string  `json:"api_key_id"`
	APIKeyName       string  `json:"api_key_name"`
	MaskedKey        string  `json:"masked_key"`
	RequestCount     int64   `json:"request_count"`
	SuccessCount     int64   `json:"success_count"`
	FailureCount     int64   `json:"failure_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	EstimatedCost    float64 `json:"estimated_cost"`
	Currency         string  `json:"currency"`
	TokenShare       float64 `json:"token_share"`
	CostShare        float64 `json:"cost_share"`
}

type requestTimeRange struct {
	From time.Time
	To   time.Time
}

func NewService(db *store.Store, timeZones ...config.TimeZone) *Service {
	return &Service{db: db, timeZone: usageServiceTimeZone(timeZones...)}
}

func (s *Service) ListRequests(ctx context.Context, query RequestQuery) ([]store.RequestLogDetail, error) {
	return store.NewRequestLogRepository(s.db.DB()).ListDetailed(ctx, store.ListRequestLogsParams{
		Limit:           query.Limit,
		Page:            query.Page,
		PageSize:        query.PageSize,
		Success:         query.Success,
		SiteID:          query.SiteID,
		APIKeyID:        query.APIKeyID,
		Search:          query.Search,
		ModelKey:        query.ModelKey,
		ErrorType:       query.ErrorType,
		Endpoint:        query.Endpoint,
		RequestID:       query.RequestID,
		HideWithoutSite: query.HideWithoutSite,
		CreatedFrom:     query.CreatedFrom,
		CreatedTo:       query.CreatedTo,
	})
}

func (s *Service) ListRequestsPage(ctx context.Context, query RequestQuery) (store.ListRequestLogsResult, error) {
	return store.NewRequestLogRepository(s.db.DB()).ListDetailedPage(ctx, store.ListRequestLogsParams{
		Page:            query.Page,
		PageSize:        query.PageSize,
		Success:         query.Success,
		SiteID:          query.SiteID,
		APIKeyID:        query.APIKeyID,
		Search:          query.Search,
		ModelKey:        query.ModelKey,
		ErrorType:       query.ErrorType,
		Endpoint:        query.Endpoint,
		RequestID:       query.RequestID,
		HideWithoutSite: query.HideWithoutSite,
		CreatedFrom:     query.CreatedFrom,
		CreatedTo:       query.CreatedTo,
	})
}

func (s *Service) GetRequest(ctx context.Context, requestLogID uuid.UUID) (store.RequestLogDetail, error) {
	return store.NewRequestLogRepository(s.db.DB()).GetDetailed(ctx, requestLogID)
}

func (s *Service) ListRequestAttempts(ctx context.Context, parentRequestID string) ([]store.RequestLogDetail, error) {
	return store.NewRequestLogRepository(s.db.DB()).ListAttemptsForParentRequest(ctx, parentRequestID)
}

func (s *Service) RecentRateUsage(ctx context.Context, now time.Time) (store.RecentRateUsageSummary, error) {
	if now.IsZero() {
		now = time.Now()
	}
	return store.NewRequestLogRepository(s.db.DB()).RecentRateUsage(ctx, now.Add(-time.Minute))
}

func (s *Service) RequestSummary(ctx context.Context, query RequestQuery, now time.Time) (RequestSummaryResult, error) {
	if strings.TrimSpace(query.Search) != "" || strings.TrimSpace(query.RequestID) != "" {
		return RequestSummaryResult{
			Currency:          "USD",
			Supported:         false,
			UnsupportedReason: "search_filter",
		}, nil
	}
	if now.IsZero() {
		now = time.Now()
	}

	summaryFrom, summaryTo, includeSummary := s.requestSummaryBucketRange(query.CreatedFrom, query.CreatedTo, now)
	total := store.RequestUsageCostSummary{Currency: "USD"}
	if includeSummary {
		rows, err := store.NewRequestUsageSummaryRepository(s.db.DB()).CostSummary(ctx, store.RequestUsageCostSummaryQuery{
			TimeZone:        s.timeZone.Name,
			From:            summaryFrom,
			To:              summaryTo,
			Success:         query.Success,
			SiteID:          query.SiteID,
			APIKeyID:        query.APIKeyID,
			ModelKey:        query.ModelKey,
			ErrorType:       query.ErrorType,
			Endpoint:        query.Endpoint,
			HideWithoutSite: query.HideWithoutSite,
		})
		if err != nil {
			return RequestSummaryResult{}, err
		}
		total.AddFloat(rows.TotalCost, rows.Currency)
		total.AddCacheWriteCost(sql.NullFloat64{Float64: rows.CacheWriteCost, Valid: true}, rows.Currency)
		total.AddUsage(rows.PromptTokens, rows.CompletionTokens, rows.TotalTokens, rows.CachedTokens, rows.CacheWriteTokens, rows.CacheCreationInputTokens, rows.CacheCreation5mInputTokens, rows.CacheCreation1hInputTokens)
	}

	for _, window := range s.requestSummaryDetailWindows(query.CreatedFrom, query.CreatedTo, summaryFrom, summaryTo, now) {
		from := window.From
		to := window.To
		rows, err := store.NewRequestLogRepository(s.db.DB()).CostSummary(ctx, store.ListRequestLogsParams{
			Success:         query.Success,
			SiteID:          query.SiteID,
			APIKeyID:        query.APIKeyID,
			ModelKey:        query.ModelKey,
			ErrorType:       query.ErrorType,
			Endpoint:        query.Endpoint,
			HideWithoutSite: query.HideWithoutSite,
			CreatedFrom:     &from,
			CreatedTo:       &to,
		})
		if err != nil {
			return RequestSummaryResult{}, err
		}
		total.AddFloat(rows.TotalCost, rows.Currency)
		total.AddCacheWriteCost(sql.NullFloat64{Float64: rows.CacheWriteCost, Valid: true}, rows.Currency)
		total.AddUsage(rows.PromptTokens, rows.CompletionTokens, rows.TotalTokens, rows.CachedTokens, rows.CacheWriteTokens, rows.CacheCreationInputTokens, rows.CacheCreation5mInputTokens, rows.CacheCreation1hInputTokens)
	}

	return RequestSummaryResult{
		TotalCost:                  total.TotalCost,
		PromptTokens:               total.PromptTokens,
		CompletionTokens:           total.CompletionTokens,
		TotalTokens:                total.TotalTokens,
		CachedTokens:               total.CachedTokens,
		CacheWriteTokens:           total.CacheWriteTokens,
		CacheCreationInputTokens:   total.CacheCreationInputTokens,
		CacheCreation5mInputTokens: total.CacheCreation5mInputTokens,
		CacheCreation1hInputTokens: total.CacheCreation1hInputTokens,
		CacheWriteTotalTokens:      total.CacheWriteTotalTokens,
		CacheWriteCost:             total.CacheWriteCost,
		Currency:                   total.Currency,
		Supported:                  true,
	}, nil
}

type KeyDailyUsage struct {
	Date             string
	Requests         int64
	Success          int64
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	Cost             float64
}

type KeyUsageTrend struct {
	From     time.Time
	To       time.Time
	Days     int
	Currency string
	Totals   KeyDailyUsage
	Buckets  []KeyDailyUsage
}

func (s *Service) KeyDailyTrend(ctx context.Context, apiKeyID uuid.UUID, days int, now time.Time) (KeyUsageTrend, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if days < 1 {
		days = 1
	}
	todayStart := s.timeZone.StartOfDay(now)
	startDay := todayStart.AddDate(0, 0, -(days - 1))

	byDay := make(map[time.Time]*KeyDailyUsage, days)
	order := make([]time.Time, 0, days)
	for d := 0; d < days; d++ {
		day := startDay.AddDate(0, 0, d)
		byDay[day] = &KeyDailyUsage{Date: day.Format("2006-01-02")}
		order = append(order, day)
	}
	add := func(at time.Time, requests, success, prompt, completion, total int64, cost float64) {
		bucket, ok := byDay[s.timeZone.StartOfDay(at)]
		if !ok {
			return
		}
		bucket.Requests += requests
		bucket.Success += success
		bucket.PromptTokens += prompt
		bucket.CompletionTokens += completion
		bucket.TotalTokens += total
		bucket.Cost += cost
	}

	if startDay.Before(todayStart) {
		rows, err := store.NewRequestUsageSummaryRepository(s.db.DB()).List(ctx, store.RequestUsageSummaryQuery{
			TimeZone: s.timeZone.Name,
			From:     &startDay,
			To:       &todayStart,
			APIKeyID: &apiKeyID,
		})
		if err != nil {
			return KeyUsageTrend{}, err
		}
		for _, row := range rows {
			add(row.BucketStart, row.RequestCount, row.SuccessCount, row.PromptTokens, row.CompletionTokens, row.TotalTokens, row.EstimatedCost)
		}
	}

	aggregates, err := store.NewRequestLogRepository(s.db.DB()).DailyKeyAggregate(ctx, store.ListRequestLogsParams{
		APIKeyID:    &apiKeyID,
		CreatedFrom: &todayStart,
		CreatedTo:   &now,
	}, s.timeZone)
	if err != nil {
		return KeyUsageTrend{}, err
	}
	for _, aggregate := range aggregates {
		add(aggregate.Day, aggregate.RequestCount, aggregate.SuccessCount, aggregate.PromptTokens, aggregate.CompletionTokens, aggregate.TotalTokens, aggregate.EstimatedCost)
	}

	trend := KeyUsageTrend{
		From:     startDay,
		To:       now,
		Days:     days,
		Currency: "USD",
		Buckets:  make([]KeyDailyUsage, 0, days),
	}
	for _, day := range order {
		bucket := byDay[day]
		trend.Totals.Requests += bucket.Requests
		trend.Totals.Success += bucket.Success
		trend.Totals.PromptTokens += bucket.PromptTokens
		trend.Totals.CompletionTokens += bucket.CompletionTokens
		trend.Totals.TotalTokens += bucket.TotalTokens
		trend.Totals.Cost += bucket.Cost
		trend.Buckets = append(trend.Buckets, *bucket)
	}
	return trend, nil
}

func (s *Service) KeyModels(ctx context.Context, apiKeyID uuid.UUID, days int) ([]string, error) {
	if days < 1 {
		days = 30
	}
	now := time.Now()
	from := s.timeZone.StartOfDay(now).AddDate(0, 0, -(days - 1))
	rows, err := store.NewRequestUsageSummaryRepository(s.db.DB()).List(ctx, store.RequestUsageSummaryQuery{
		TimeZone: s.timeZone.Name,
		From:     &from,
		APIKeyID: &apiKeyID,
	})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	models := make([]string, 0, len(rows))
	for _, row := range rows {
		key := strings.TrimSpace(row.CanonicalModelKey)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		models = append(models, key)
	}
	sort.Strings(models)
	return models, nil
}

func (s *Service) ChannelSplit(ctx context.Context, query ChannelSplitQuery, now time.Time) (ChannelSplitResult, error) {
	if s == nil || s.db == nil || s.db.DB() == nil {
		return ChannelSplitResult{}, fmt.Errorf("usage store is not initialized")
	}
	if now.IsZero() {
		now = time.Now()
	}
	timeZone := usageServiceTimeZone(s.timeZone)
	now = now.In(timeZone.Location)

	target, err := s.channelSplitTarget(ctx, query)
	if err != nil {
		return ChannelSplitResult{}, err
	}
	dateRange, err := channelSplitDateRange(query, now, timeZone)
	if err != nil {
		return ChannelSplitResult{}, err
	}

	rows, err := s.channelSplitRows(ctx, target.siteIDs(), dateRange, timeZone)
	if err != nil {
		return ChannelSplitResult{}, err
	}
	apiKeys, err := s.channelSplitAPIKeys(ctx, channelSplitAPIKeyIDs(rows, target.siteIDs()))
	if err != nil {
		return ChannelSplitResult{}, err
	}
	items := channelSplitItemsFromRows(rows, target.siteIDs(), apiKeys)
	summary := channelSplitSummaryFromItems(items, target, dateRange, now, timeZone)
	return ChannelSplitResult{Summary: summary, Items: items}, nil
}

type channelSplitSite struct {
	ID       uuid.UUID
	Name     string
	Slug     string
	SiteType string
}

type channelSplitTarget struct {
	Sites             []channelSplitSite
	OAuthConnectionID *uuid.UUID
	OAuthProvider     string
	OAuthAccount      string
}

func (target channelSplitTarget) siteIDs() []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(target.Sites))
	for _, site := range target.Sites {
		ids = appendChannelSplitUUIDOnce(ids, site.ID)
	}
	return ids
}

type channelSplitWindow struct {
	All               bool
	DateFrom          *time.Time
	DateTo            *time.Time
	RangeStart        *time.Time
	RangeEndExclusive *time.Time
}

func (s *Service) channelSplitTarget(ctx context.Context, query ChannelSplitQuery) (channelSplitTarget, error) {
	siteIDs := channelSplitRequestedSiteIDs(query)
	var oauthConnectionID *uuid.UUID
	var oauthProvider string
	var oauthAccount string

	if query.OAuthConnectionID != nil {
		connection, err := store.NewOAuthConnectionRepository(s.db.DB()).GetByID(ctx, *query.OAuthConnectionID)
		if err != nil {
			return channelSplitTarget{}, err
		}
		if connection.SiteID == nil || *connection.SiteID == uuid.Nil {
			return channelSplitTarget{}, invalidChannelSplitQuery("oauth connection is not bound to a site")
		}
		if len(siteIDs) == 0 {
			siteIDs = append(siteIDs, *connection.SiteID)
		}
		oauthConnectionID = &connection.ID
		oauthProvider = connection.Provider
		oauthAccount = strings.TrimSpace(connection.Email)
		if oauthAccount == "" {
			oauthAccount = strings.TrimSpace(connection.AccountID)
		}
	}

	if len(siteIDs) == 0 {
		return channelSplitTarget{}, invalidChannelSplitQuery("site_id or oauth_connection_id is required")
	}

	sites, err := store.NewSiteRepository(s.db.DB()).ListByIDsIncludingDeleted(ctx, siteIDs)
	if err != nil {
		return channelSplitTarget{}, err
	}
	sitesByID := map[uuid.UUID]store.Site{}
	for _, site := range sites {
		sitesByID[site.ID] = site
	}
	targetSites := make([]channelSplitSite, 0, len(siteIDs))
	for _, siteID := range siteIDs {
		site, ok := sitesByID[siteID]
		if !ok {
			return channelSplitTarget{}, fmt.Errorf("channel split site: %w", gorm.ErrRecordNotFound)
		}
		targetSites = append(targetSites, channelSplitSite{
			ID:       site.ID,
			Name:     site.Name,
			Slug:     site.Slug,
			SiteType: site.SiteType,
		})
	}
	return channelSplitTarget{
		Sites:             targetSites,
		OAuthConnectionID: oauthConnectionID,
		OAuthProvider:     oauthProvider,
		OAuthAccount:      oauthAccount,
	}, nil
}

func channelSplitDateRange(query ChannelSplitQuery, now time.Time, timeZone config.TimeZone) (channelSplitWindow, error) {
	timeZone = usageServiceTimeZone(timeZone)
	if rangeValue := strings.ToLower(strings.TrimSpace(query.Range)); rangeValue != "" {
		if rangeValue != "all" {
			return channelSplitWindow{}, invalidChannelSplitQuery("range must be all")
		}
		return channelSplitWindow{All: true}, nil
	}

	today := timeZone.StartOfDay(now)
	dateFrom := today.AddDate(0, 0, -29)
	dateTo := today

	if strings.TrimSpace(query.DateFrom) != "" {
		parsed, err := parseChannelSplitDate(query.DateFrom, timeZone.Location)
		if err != nil {
			return channelSplitWindow{}, invalidChannelSplitQuery("date_from must use YYYY-MM-DD")
		}
		dateFrom = parsed
	}
	if strings.TrimSpace(query.DateTo) != "" {
		parsed, err := parseChannelSplitDate(query.DateTo, timeZone.Location)
		if err != nil {
			return channelSplitWindow{}, invalidChannelSplitQuery("date_to must use YYYY-MM-DD")
		}
		dateTo = parsed
	}
	if dateFrom.After(dateTo) {
		return channelSplitWindow{}, invalidChannelSplitQuery("date_from must be before or equal to date_to")
	}
	rangeEnd := dateTo.AddDate(0, 0, 1)
	return channelSplitWindow{
		All:               false,
		DateFrom:          &dateFrom,
		DateTo:            &dateTo,
		RangeStart:        &dateFrom,
		RangeEndExclusive: &rangeEnd,
	}, nil
}

func parseChannelSplitDate(value string, location *time.Location) (time.Time, error) {
	if location == nil {
		location = time.Local
	}
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(value), location)
}

func (s *Service) channelSplitRows(ctx context.Context, siteIDs []uuid.UUID, dateRange channelSplitWindow, timeZone config.TimeZone) ([]store.RequestUsageDailySummary, error) {
	repo := store.NewRequestUsageSummaryRepository(s.db.DB())
	return repo.List(ctx, store.RequestUsageSummaryQuery{
		TimeZone: timeZone.Name,
		From:     dateRange.RangeStart,
		To:       dateRange.RangeEndExclusive,
		SiteIDs:  siteIDs,
	})
}

func (s *Service) channelSplitAPIKeys(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]store.APIKey, error) {
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

func channelSplitAPIKeyIDs(rows []store.RequestUsageDailySummary, siteIDs []uuid.UUID) []uuid.UUID {
	siteSet := channelSplitSiteSet(siteIDs)
	ids := make([]uuid.UUID, 0)
	for _, row := range rows {
		if !channelSplitRowMatchesSites(row, siteSet) || !row.APIKeyID.Valid {
			continue
		}
		ids = appendChannelSplitUUIDOnce(ids, row.APIKeyID.UUID)
	}
	return ids
}

type channelSplitAggregate struct {
	apiKeyID         uuid.UUID
	apiKeyName       string
	maskedKey        string
	requestCount     int64
	successCount     int64
	failureCount     int64
	promptTokens     int64
	completionTokens int64
	totalTokens      int64
	estimatedCost    float64
	currency         string
}

func channelSplitItemsFromRows(rows []store.RequestUsageDailySummary, siteIDs []uuid.UUID, apiKeys map[uuid.UUID]store.APIKey) []ChannelSplitItem {
	siteSet := channelSplitSiteSet(siteIDs)
	byAPIKey := map[uuid.UUID]*channelSplitAggregate{}
	for _, row := range rows {
		if !channelSplitRowMatchesSites(row, siteSet) || !row.APIKeyID.Valid {
			continue
		}
		apiKeyID := row.APIKeyID.UUID
		item := byAPIKey[apiKeyID]
		if item == nil {
			item = &channelSplitAggregate{
				apiKeyID:   apiKeyID,
				apiKeyName: strings.TrimSpace(row.APIKeyName),
				currency:   "USD",
			}
			byAPIKey[apiKeyID] = item
		}
		item.requestCount += row.RequestCount
		item.successCount += row.SuccessCount
		item.failureCount += row.FailureCount
		item.promptTokens += row.PromptTokens
		item.completionTokens += row.CompletionTokens
		item.totalTokens += row.TotalTokens
		item.estimatedCost += row.EstimatedCost
		if item.apiKeyName == "" {
			item.apiKeyName = strings.TrimSpace(row.APIKeyName)
		}
		if row.Currency != "" {
			item.currency = row.Currency
		}
	}

	items := make([]ChannelSplitItem, 0, len(byAPIKey))
	for apiKeyID, aggregate := range byAPIKey {
		if apiKey, ok := apiKeys[apiKeyID]; ok {
			if strings.TrimSpace(apiKey.Name) != "" {
				aggregate.apiKeyName = apiKey.Name
			}
			aggregate.maskedKey = apiKey.MaskedKey
		}
		items = append(items, ChannelSplitItem{
			APIKeyID:         aggregate.apiKeyID.String(),
			APIKeyName:       aggregate.apiKeyName,
			MaskedKey:        aggregate.maskedKey,
			RequestCount:     aggregate.requestCount,
			SuccessCount:     aggregate.successCount,
			FailureCount:     aggregate.failureCount,
			PromptTokens:     aggregate.promptTokens,
			CompletionTokens: aggregate.completionTokens,
			TotalTokens:      aggregate.totalTokens,
			EstimatedCost:    aggregate.estimatedCost,
			Currency:         channelSplitStringDefault(aggregate.currency, "USD"),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].EstimatedCost != items[j].EstimatedCost {
			return items[i].EstimatedCost > items[j].EstimatedCost
		}
		if items[i].TotalTokens != items[j].TotalTokens {
			return items[i].TotalTokens > items[j].TotalTokens
		}
		return items[i].APIKeyName < items[j].APIKeyName
	})

	var totalCost float64
	var totalTokens int64
	for _, item := range items {
		totalCost += item.EstimatedCost
		totalTokens += item.TotalTokens
	}
	for index := range items {
		if totalCost > 0 {
			items[index].CostShare = items[index].EstimatedCost / totalCost
		}
		if totalTokens > 0 {
			items[index].TokenShare = float64(items[index].TotalTokens) / float64(totalTokens)
		}
	}
	return items
}

func channelSplitSummaryFromItems(items []ChannelSplitItem, target channelSplitTarget, dateRange channelSplitWindow, now time.Time, timeZone config.TimeZone) ChannelSplitSummary {
	firstSite := channelSplitSite{}
	if len(target.Sites) > 0 {
		firstSite = target.Sites[0]
	}
	summary := ChannelSplitSummary{
		SiteID:      channelSplitUUIDString(firstSite.ID),
		SiteName:    firstSite.Name,
		SiteSlug:    firstSite.Slug,
		SiteType:    firstSite.SiteType,
		SiteIDs:     channelSplitTargetSiteIDs(target),
		SiteNames:   channelSplitTargetSiteNames(target),
		TargetLabel: channelSplitTargetLabel(target),
		RangeAll:    dateRange.All,
		TimeZone:    timeZone.Name,
		GeneratedAt: timeZone.Format(now, time.RFC3339),
		Currency:    "USD",
		APIKeyCount: len(items),
	}
	if dateRange.DateFrom != nil {
		summary.DateFrom = dateRange.DateFrom.Format("2006-01-02")
	}
	if dateRange.DateTo != nil {
		summary.DateTo = dateRange.DateTo.Format("2006-01-02")
	}
	if dateRange.RangeStart != nil {
		summary.RangeStart = timeZone.Format(*dateRange.RangeStart, time.RFC3339)
	}
	if dateRange.RangeEndExclusive != nil {
		summary.RangeEnd = timeZone.Format(*dateRange.RangeEndExclusive, time.RFC3339)
	}
	if target.OAuthConnectionID != nil {
		value := target.OAuthConnectionID.String()
		summary.OAuthConnectionID = &value
	}
	if target.OAuthProvider != "" {
		value := target.OAuthProvider
		summary.OAuthProvider = &value
	}
	if target.OAuthAccount != "" {
		value := target.OAuthAccount
		summary.OAuthAccount = &value
	}
	for _, item := range items {
		summary.RequestCount += item.RequestCount
		summary.SuccessCount += item.SuccessCount
		summary.FailureCount += item.FailureCount
		summary.PromptTokens += item.PromptTokens
		summary.CompletionTokens += item.CompletionTokens
		summary.TotalTokens += item.TotalTokens
		summary.EstimatedCost += item.EstimatedCost
		if item.Currency != "" {
			summary.Currency = item.Currency
		}
	}
	return summary
}

func invalidChannelSplitQuery(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidChannelSplitQuery, fmt.Sprintf(format, args...))
}

func channelSplitRequestedSiteIDs(query ChannelSplitQuery) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(query.SiteIDs)+1)
	if query.SiteID != nil {
		ids = appendChannelSplitUUIDOnce(ids, *query.SiteID)
	}
	for _, siteID := range query.SiteIDs {
		ids = appendChannelSplitUUIDOnce(ids, siteID)
	}
	return ids
}

func channelSplitSiteSet(siteIDs []uuid.UUID) map[uuid.UUID]struct{} {
	result := map[uuid.UUID]struct{}{}
	for _, siteID := range siteIDs {
		if siteID != uuid.Nil {
			result[siteID] = struct{}{}
		}
	}
	return result
}

func channelSplitRowMatchesSites(row store.RequestUsageDailySummary, siteSet map[uuid.UUID]struct{}) bool {
	if !row.SiteID.Valid {
		return false
	}
	_, ok := siteSet[row.SiteID.UUID]
	return ok
}

func channelSplitTargetSiteIDs(target channelSplitTarget) []string {
	ids := make([]string, 0, len(target.Sites))
	for _, site := range target.Sites {
		ids = append(ids, site.ID.String())
	}
	return ids
}

func channelSplitTargetSiteNames(target channelSplitTarget) []string {
	names := make([]string, 0, len(target.Sites))
	for _, site := range target.Sites {
		names = append(names, site.Name)
	}
	return names
}

func channelSplitTargetLabel(target channelSplitTarget) string {
	if len(target.Sites) == 0 {
		return ""
	}
	if len(target.Sites) == 1 {
		return target.Sites[0].Name
	}
	return fmt.Sprintf("%s +%d", target.Sites[0].Name, len(target.Sites)-1)
}

func channelSplitUUIDString(value uuid.UUID) string {
	if value == uuid.Nil {
		return ""
	}
	return value.String()
}

func minTime(left time.Time, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func maxTime(left time.Time, right time.Time) time.Time {
	if left.After(right) {
		return left
	}
	return right
}

func appendChannelSplitUUIDOnce(values []uuid.UUID, value uuid.UUID) []uuid.UUID {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}

func channelSplitStringDefault(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func (s *Service) requestSummaryBucketRange(from *time.Time, to *time.Time, now time.Time) (*time.Time, *time.Time, bool) {
	todayStart := s.timeZone.StartOfDay(now)
	var summaryFrom *time.Time
	if from != nil {
		start := s.timeZone.StartOfDay(*from)
		if from.After(start) {
			start = start.AddDate(0, 0, 1)
		}
		summaryFrom = &start
	}

	var summaryTo *time.Time
	if to != nil {
		start := s.timeZone.StartOfDay(*to)
		next := start.AddDate(0, 0, 1)
		currentStart := s.timeZone.StartOfDay(now)
		fromCoversDay := from == nil || !from.After(start)
		if !to.Before(next.Add(-time.Nanosecond)) || (start.Equal(currentStart) && !to.Before(now) && fromCoversDay) {
			summaryTo = &next
		} else {
			summaryTo = &start
		}
	}
	if summaryTo == nil || summaryTo.After(todayStart) {
		summaryTo = &todayStart
	}

	if summaryFrom != nil && summaryTo != nil && !summaryFrom.Before(*summaryTo) {
		return summaryFrom, summaryTo, false
	}
	return summaryFrom, summaryTo, true
}

func (s *Service) requestSummaryDetailWindows(from *time.Time, to *time.Time, summaryFrom *time.Time, summaryTo *time.Time, now time.Time) []requestTimeRange {
	windows := []requestTimeRange{}
	todayStart := s.timeZone.StartOfDay(now)
	if from != nil {
		start := s.timeZone.StartOfDay(*from)
		if start.Before(todayStart) && from.After(start) && !requestSummaryRangeCoversDay(start, summaryFrom, summaryTo) {
			end := start.AddDate(0, 0, 1)
			endInclusive := inclusiveRangeEnd(end)
			if to != nil && to.Before(end) {
				endInclusive = *to
			}
			windows = appendRequestTimeRange(windows, *from, endInclusive)
		}
	}
	if to != nil {
		start := s.timeZone.StartOfDay(*to)
		if start.Before(todayStart) && !requestSummaryRangeCoversDay(start, summaryFrom, summaryTo) {
			rangeStart := start
			if from != nil && from.After(rangeStart) {
				rangeStart = *from
			}
			windows = appendRequestTimeRange(windows, rangeStart, *to)
		}
	}
	if (from == nil || !from.After(now)) && (to == nil || !to.Before(todayStart)) {
		rangeStart := todayStart
		if from != nil && from.After(rangeStart) {
			rangeStart = *from
		}
		rangeEnd := now
		if to != nil && to.Before(rangeEnd) {
			rangeEnd = *to
		}
		windows = appendRequestTimeRange(windows, rangeStart, rangeEnd)
	}
	return windows
}

func requestSummaryRangeCoversDay(day time.Time, from *time.Time, to *time.Time) bool {
	if from != nil && day.Before(*from) {
		return false
	}
	if to != nil && !day.Before(*to) {
		return false
	}
	return true
}

func appendRequestTimeRange(windows []requestTimeRange, from time.Time, to time.Time) []requestTimeRange {
	if to.Before(from) {
		return windows
	}
	for _, window := range windows {
		if window.From.Equal(from) && window.To.Equal(to) {
			return windows
		}
	}
	return append(windows, requestTimeRange{From: from, To: to})
}

func inclusiveRangeEnd(end time.Time) time.Time {
	return end.Add(-time.Nanosecond)
}

func (s *Service) UsageSummaryBySite(ctx context.Context, after *time.Time) ([]store.SiteUsageSummaryRow, error) {
	query := store.RequestUsageSummaryQuery{TimeZone: s.timeZone.Name}
	if after != nil {
		from := s.timeZone.StartOfDay(*after)
		query.From = &from
	}
	return store.NewRequestUsageSummaryRepository(s.db.DB()).SummarizeBySite(ctx, query)
}

func (s *Service) UsageSummaryForSite(ctx context.Context, siteID uuid.UUID, after *time.Time) (store.SiteUsageSummaryRow, bool, error) {
	query := store.RequestUsageSummaryQuery{TimeZone: s.timeZone.Name, SiteID: &siteID}
	if after != nil {
		from := s.timeZone.StartOfDay(*after)
		query.From = &from
	}
	rows, err := store.NewRequestUsageSummaryRepository(s.db.DB()).SummarizeBySite(ctx, query)
	if err != nil {
		return store.SiteUsageSummaryRow{}, false, err
	}
	if len(rows) == 0 {
		return store.SiteUsageSummaryRow{}, false, nil
	}
	return rows[0], true, nil
}

func usageServiceTimeZone(timeZones ...config.TimeZone) config.TimeZone {
	return config.TimeZoneOrDefault(timeZones...)
}
