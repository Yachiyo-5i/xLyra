package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"xlyra/server/internal/config"
)

type RequestLog struct {
	ID                 uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	RequestID          string
	APIKeyID           uuid.NullUUID
	SiteID             uuid.NullUUID
	CanonicalModelID   uuid.NullUUID
	SiteModelID        uuid.NullUUID
	Endpoint           string
	StatusCode         int
	Success            bool
	ErrorType          sql.NullString
	LatencyMS          sql.NullInt64
	UpstreamLatencyMS  sql.NullInt64
	RequestTokens      sql.NullInt64
	ResponseTokens     sql.NullInt64
	Metadata           JSON          `gorm:"type:jsonb"`
	Internal           bool          `gorm:"default:false;not null"`
	ParentRequestLogID uuid.NullUUID `gorm:"type:uuid;index:request_logs_parent_request_log_id_idx"`
	CreatedAt          time.Time
}

type CreateRequestLogParams struct {
	RequestID          string
	APIKeyID           any
	SiteID             any
	CanonicalModelID   any
	SiteModelID        any
	Endpoint           string
	StatusCode         int
	Success            bool
	ErrorType          any
	LatencyMS          any
	UpstreamLatencyMS  any
	RequestTokens      any
	ResponseTokens     any
	Metadata           JSON
	Internal           bool
	ParentRequestLogID any
}

type ListRequestLogsParams struct {
	Limit              int
	Page               int
	PageSize           int
	Success            *bool
	SiteID             *uuid.UUID
	APIKeyID           *uuid.UUID
	Search             string
	ModelKey           string
	ErrorType          string
	Endpoint           string
	RequestID          string
	HideWithoutSite    bool
	CreatedFrom        *time.Time
	CreatedTo          *time.Time
	Internal           *bool
	ParentRequestLogID *uuid.UUID
}

type ListRequestLogsResult struct {
	Items     []RequestLogDetail
	Total     int64
	TotalCost float64
	Page      int
	PageSize  int
}

type RecentRateUsageSummary struct {
	RPM int64
	TPM int64
}

type RequestLogDetail struct {
	RequestLog
	APIKeyName            sql.NullString
	APIKeyMaskedKey       sql.NullString
	SiteName              sql.NullString
	SiteSlug              sql.NullString
	SiteType              sql.NullString
	CanonicalModelKey     sql.NullString
	SiteModelUpstreamName sql.NullString
	SiteModelDisplayName  sql.NullString
	UsagePromptTokens     sql.NullInt64
	UsageCompletionTokens sql.NullInt64
	UsageTotalTokens      sql.NullInt64
	EstimatedCost         sql.NullFloat64
	UsageCurrency         sql.NullString
}

type RequestLogRepository struct {
	db *gorm.DB
}

func NewRequestLogRepository(db *gorm.DB) RequestLogRepository {
	return RequestLogRepository{db: db}
}

func (r RequestLogRepository) Create(ctx context.Context, params CreateRequestLogParams) (RequestLog, error) {
	item := RequestLog{
		RequestID:          params.RequestID,
		APIKeyID:           nullUUIDFromAny(params.APIKeyID),
		SiteID:             nullUUIDFromAny(params.SiteID),
		CanonicalModelID:   nullUUIDFromAny(params.CanonicalModelID),
		SiteModelID:        nullUUIDFromAny(params.SiteModelID),
		Endpoint:           params.Endpoint,
		StatusCode:         params.StatusCode,
		Success:            params.Success,
		ErrorType:          nullStringFromAny(params.ErrorType),
		LatencyMS:          nullInt64FromAny(params.LatencyMS),
		UpstreamLatencyMS:  nullInt64FromAny(params.UpstreamLatencyMS),
		RequestTokens:      nullInt64FromAny(params.RequestTokens),
		ResponseTokens:     nullInt64FromAny(params.ResponseTokens),
		Metadata:           jsonDefault(params.Metadata, "{}"),
		Internal:           params.Internal,
		ParentRequestLogID: nullUUIDFromAny(params.ParentRequestLogID),
	}
	if err := r.db.WithContext(ctx).Create(&item).Error; err != nil {
		return RequestLog{}, fmt.Errorf("create request log: %w", err)
	}
	return item, nil
}

func (r RequestLogRepository) ListRecentSiteModelAttempts(ctx context.Context, siteID uuid.UUID, siteModelID uuid.UUID, since time.Time, limit int) ([]RequestLog, error) {
	if limit <= 0 {
		limit = 10
	}
	var items []RequestLog
	if err := r.db.WithContext(ctx).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "site_id"}, Value: siteID},
			clause.Eq{Column: clause.Column{Name: "site_model_id"}, Value: siteModelID},
			clause.Gte{Column: clause.Column{Name: "created_at"}, Value: since},
		}}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "created_at"}, Desc: true},
		}}).
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list recent site model attempts: %w", err)
	}
	return items, nil
}

func (r RequestLogRepository) OldestCreatedAt(ctx context.Context) (*time.Time, error) {
	var items []RequestLog
	if err := r.db.WithContext(ctx).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "created_at"}},
		}}).
		Limit(1).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("oldest request log: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}
	value := items[0].CreatedAt
	return &value, nil
}

func (r RequestLogRepository) ListCreatedBetween(ctx context.Context, start time.Time, end time.Time) ([]RequestLog, error) {
	var items []RequestLog
	if err := r.db.WithContext(ctx).
		Clauses(requestLogCreatedRangeClause(start, end)).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list request logs by created range: %w", err)
	}
	return items, nil
}

func (r RequestLogRepository) DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 1000
	}
	var items []RequestLog
	if err := r.db.WithContext(ctx).
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Lt{Column: clause.Column{Name: "created_at"}, Value: cutoff},
		}}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "created_at"}},
		}}).
		Limit(limit).
		Find(&items).Error; err != nil {
		return 0, fmt.Errorf("list old request logs: %w", err)
	}
	if len(items) == 0 {
		return 0, nil
	}
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	result := r.db.WithContext(ctx).Delete(&RequestLog{}, ids)
	if result.Error != nil {
		return 0, fmt.Errorf("delete old request logs: %w", result.Error)
	}
	return result.RowsAffected, nil
}

func (r RequestLogRepository) ListDetailed(ctx context.Context, params ListRequestLogsParams) ([]RequestLogDetail, error) {
	params.Page = 1
	params.PageSize = params.Limit
	result, err := r.ListDetailedPage(ctx, params)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (r RequestLogRepository) ListDetailedPage(ctx context.Context, params ListRequestLogsParams) (ListRequestLogsResult, error) {
	page, pageSize := normalizeRequestLogPagination(params.Page, params.PageSize)

	exprs, err := r.requestLogFilterExpressions(ctx, params, true)
	if err != nil {
		return ListRequestLogsResult{}, err
	}

	var total int64
	if err := r.requestLogQuery(ctx, exprs).Count(&total).Error; err != nil {
		return ListRequestLogsResult{}, fmt.Errorf("count request logs: %w", err)
	}

	var logs []RequestLog
	if err := r.requestLogQuery(ctx, exprs).
		Clauses(requestLogDefaultOrderClause()).
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		Find(&logs).Error; err != nil {
		return ListRequestLogsResult{}, fmt.Errorf("list request logs: %w", err)
	}
	items, err := r.detailsForLogs(ctx, logs)
	if err != nil {
		return ListRequestLogsResult{}, err
	}

	return ListRequestLogsResult{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func normalizeRequestLogPagination(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}

func (r RequestLogRepository) RecentRateUsage(ctx context.Context, since time.Time) (RecentRateUsageSummary, error) {
	// This runs on the rate-limit hot path. Project only the columns the RPM/TPM
	// aggregation needs so the DB does not ship/scan the wide RequestLog and
	// UsageRecord rows (large text/JSON columns) on every rate check.
	var logs []RequestLog
	if err := r.db.WithContext(ctx).
		Select("id", "request_tokens", "response_tokens").
		Clauses(clause.Where{Exprs: []clause.Expression{
			clause.Gte{Column: clause.Column{Name: "created_at"}, Value: since},
		}}).
		Find(&logs).Error; err != nil {
		return RecentRateUsageSummary{}, fmt.Errorf("recent request rate usage: %w", err)
	}

	logIDs := make([]uuid.UUID, 0, len(logs))
	for _, log := range logs {
		logIDs = append(logIDs, log.ID)
	}
	tokensByRequestLogID := map[uuid.UUID]int64{}
	if len(logIDs) > 0 {
		var usageRecords []UsageRecord
		if err := r.db.WithContext(ctx).
			Select("request_log_id", "total_tokens").
			Where(map[string]any{"request_log_id": logIDs}).
			Find(&usageRecords).Error; err != nil {
			return RecentRateUsageSummary{}, fmt.Errorf("recent request rate usage: %w", err)
		}
		for _, u := range usageRecords {
			tokensByRequestLogID[u.RequestLogID] = int64(u.TotalTokens)
		}
	}

	summary := RecentRateUsageSummary{RPM: int64(len(logs))}
	for _, log := range logs {
		if tokens, ok := tokensByRequestLogID[log.ID]; ok {
			summary.TPM += tokens
			continue
		}
		if log.RequestTokens.Valid {
			summary.TPM += log.RequestTokens.Int64
		}
		if log.ResponseTokens.Valid {
			summary.TPM += log.ResponseTokens.Int64
		}
	}
	return summary, nil
}

func (r RequestLogRepository) CostSummary(ctx context.Context, params ListRequestLogsParams) (RequestUsageCostSummary, error) {
	exprs, err := r.requestLogFilterExpressions(ctx, params, false)
	if err != nil {
		return RequestUsageCostSummary{}, err
	}
	var logs []RequestLog
	if err := r.requestLogQuery(ctx, exprs).Find(&logs).Error; err != nil {
		return RequestUsageCostSummary{}, fmt.Errorf("list request logs for cost summary: %w", err)
	}
	usageByRequestLogID, err := r.usageByRequestLogID(ctx, requestLogIDs(logs))
	if err != nil {
		return RequestUsageCostSummary{}, fmt.Errorf("list request log usage for cost summary: %w", err)
	}
	result := RequestUsageCostSummary{Currency: requestUsageSummaryDefaultCurrency}
	for _, log := range logs {
		if usage, ok := usageByRequestLogID[log.ID]; ok {
			result.AddCost(usage.EstimatedCost, usage.Currency)
			cachedTokens := int64(0)
			if usage.CachedTokens.Valid {
				cachedTokens = usage.CachedTokens.Int64
			}
			result.AddUsage(int64(usage.PromptTokens), int64(usage.CompletionTokens), int64(usage.TotalTokens), cachedTokens)
			continue
		}
		promptTokens := int64(0)
		completionTokens := int64(0)
		if log.RequestTokens.Valid {
			promptTokens = log.RequestTokens.Int64
		}
		if log.ResponseTokens.Valid {
			completionTokens = log.ResponseTokens.Int64
		}
		result.AddUsage(promptTokens, completionTokens, promptTokens+completionTokens, 0)
	}
	return result, nil
}

type RequestLogDailyAggregate struct {
	Day              time.Time
	RequestCount     int64
	SuccessCount     int64
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	EstimatedCost    float64
	Currency         string
}

func (r RequestLogRepository) DailyKeyAggregate(ctx context.Context, params ListRequestLogsParams, timeZone config.TimeZone) ([]RequestLogDailyAggregate, error) {
	exprs, err := r.requestLogFilterExpressions(ctx, params, false)
	if err != nil {
		return nil, err
	}
	var logs []RequestLog
	if err := r.requestLogQuery(ctx, exprs).Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("list request logs for daily aggregate: %w", err)
	}
	usageByRequestLogID, err := r.usageByRequestLogID(ctx, requestLogIDs(logs))
	if err != nil {
		return nil, fmt.Errorf("list request log usage for daily aggregate: %w", err)
	}

	buckets := map[time.Time]*RequestLogDailyAggregate{}
	for _, log := range logs {
		day := timeZone.StartOfDay(log.CreatedAt)
		bucket, ok := buckets[day]
		if !ok {
			bucket = &RequestLogDailyAggregate{Day: day, Currency: requestUsageSummaryDefaultCurrency}
			buckets[day] = bucket
		}
		bucket.RequestCount++
		if log.Success {
			bucket.SuccessCount++
		}
		if usage, ok := usageByRequestLogID[log.ID]; ok {
			bucket.PromptTokens += int64(usage.PromptTokens)
			bucket.CompletionTokens += int64(usage.CompletionTokens)
			bucket.TotalTokens += int64(usage.TotalTokens)
			if usage.EstimatedCost.Valid {
				bucket.EstimatedCost += usage.EstimatedCost.Float64
			}
			continue
		}
		if log.RequestTokens.Valid {
			bucket.PromptTokens += log.RequestTokens.Int64
			bucket.TotalTokens += log.RequestTokens.Int64
		}
		if log.ResponseTokens.Valid {
			bucket.CompletionTokens += log.ResponseTokens.Int64
			bucket.TotalTokens += log.ResponseTokens.Int64
		}
	}

	result := make([]RequestLogDailyAggregate, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, *bucket)
	}
	return result, nil
}

func (r RequestLogRepository) SiteIDsWithRequests(ctx context.Context, siteIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	result := map[uuid.UUID]bool{}
	seen := map[uuid.UUID]bool{}
	for _, siteID := range siteIDs {
		if siteID == uuid.Nil || seen[siteID] {
			continue
		}
		seen[siteID] = true

		var log RequestLog
		err := r.db.WithContext(ctx).
			Where(map[string]any{"site_id": siteID}).
			First(&log).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("find request logs for site: %w", err)
		}
		result[siteID] = true
	}
	return result, nil
}

func (r RequestLogRepository) requestLogQuery(ctx context.Context, exprs []clause.Expression) *gorm.DB {
	db := r.db.WithContext(ctx).Model(&RequestLog{})
	if len(exprs) > 0 {
		db = db.Clauses(clause.Where{Exprs: exprs})
	}
	return db
}

func requestLogDefaultOrderClause() clause.OrderBy {
	return clause.OrderBy{Columns: []clause.OrderByColumn{
		{Column: clause.Column{Name: "created_at"}, Desc: true},
		{Column: clause.Column{Name: "id"}, Desc: true},
	}}
}

func (r RequestLogRepository) GetDetailed(ctx context.Context, requestLogID uuid.UUID) (RequestLogDetail, error) {
	var log RequestLog
	if err := r.db.WithContext(ctx).Where(&RequestLog{ID: requestLogID}).First(&log).Error; err != nil {
		return RequestLogDetail{}, fmt.Errorf("get request log detail: %w", err)
	}
	details, err := r.detailsForLogs(ctx, []RequestLog{log})
	if err != nil {
		return RequestLogDetail{}, err
	}
	if len(details) == 0 {
		return RequestLogDetail{}, fmt.Errorf("get request log detail: %w", gorm.ErrRecordNotFound)
	}
	return details[0], nil
}

func (r RequestLogRepository) detailsForLogs(ctx context.Context, logs []RequestLog) ([]RequestLogDetail, error) {
	if len(logs) == 0 {
		return []RequestLogDetail{}, nil
	}

	logIDs := make([]uuid.UUID, 0, len(logs))
	apiKeyIDs := make([]uuid.UUID, 0, len(logs))
	siteIDs := make([]uuid.UUID, 0, len(logs))
	canonicalModelIDs := make([]uuid.UUID, 0, len(logs))
	siteModelIDs := make([]uuid.UUID, 0, len(logs))
	for _, log := range logs {
		logIDs = append(logIDs, log.ID)
		if log.APIKeyID.Valid {
			apiKeyIDs = appendUUIDOnce(apiKeyIDs, log.APIKeyID.UUID)
		}
		if log.SiteID.Valid {
			siteIDs = appendUUIDOnce(siteIDs, log.SiteID.UUID)
		}
		if log.CanonicalModelID.Valid {
			canonicalModelIDs = appendUUIDOnce(canonicalModelIDs, log.CanonicalModelID.UUID)
		}
		if log.SiteModelID.Valid {
			siteModelIDs = appendUUIDOnce(siteModelIDs, log.SiteModelID.UUID)
		}
	}

	var apiKeys []APIKey
	var sites []Site
	var canonicalModels []CanonicalModel
	var siteModels []SiteModel
	if len(apiKeyIDs) > 0 {
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": apiKeyIDs}).Find(&apiKeys).Error; err != nil {
			return nil, fmt.Errorf("list request log api keys: %w", err)
		}
	}
	if len(siteIDs) > 0 {
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": siteIDs}).Find(&sites).Error; err != nil {
			return nil, fmt.Errorf("list request log sites: %w", err)
		}
	}
	if len(canonicalModelIDs) > 0 {
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": canonicalModelIDs}).Find(&canonicalModels).Error; err != nil {
			return nil, fmt.Errorf("list request log canonical models: %w", err)
		}
	}
	if len(siteModelIDs) > 0 {
		if err := r.db.WithContext(ctx).Where(map[string]any{"id": siteModelIDs}).Find(&siteModels).Error; err != nil {
			return nil, fmt.Errorf("list request log site models: %w", err)
		}
	}
	usageByRequestLogID, err := r.usageByRequestLogID(ctx, logIDs)
	if err != nil {
		return nil, fmt.Errorf("list request log usage records: %w", err)
	}
	apiKeysByID := map[uuid.UUID]APIKey{}
	for _, item := range apiKeys {
		apiKeysByID[item.ID] = item
	}
	sitesByID := map[uuid.UUID]Site{}
	for _, item := range sites {
		sitesByID[item.ID] = item
	}
	canonicalByID := map[uuid.UUID]CanonicalModel{}
	for _, item := range canonicalModels {
		canonicalByID[item.ID] = item
	}
	siteModelsByID := map[uuid.UUID]SiteModel{}
	for _, item := range siteModels {
		siteModelsByID[item.ID] = item
	}
	details := make([]RequestLogDetail, 0, len(logs))
	for _, log := range logs {
		detail := RequestLogDetail{RequestLog: log}
		if log.APIKeyID.Valid {
			if apiKey, ok := apiKeysByID[log.APIKeyID.UUID]; ok {
				detail.APIKeyName = sql.NullString{String: apiKey.Name, Valid: true}
				detail.APIKeyMaskedKey = sql.NullString{String: apiKey.MaskedKey, Valid: true}
			}
		}
		if log.SiteID.Valid {
			if site, ok := sitesByID[log.SiteID.UUID]; ok {
				detail.SiteName = sql.NullString{String: site.Name, Valid: true}
				detail.SiteSlug = sql.NullString{String: site.Slug, Valid: true}
				detail.SiteType = sql.NullString{String: site.SiteType, Valid: true}
			}
		}
		if log.CanonicalModelID.Valid {
			if model, ok := canonicalByID[log.CanonicalModelID.UUID]; ok {
				detail.CanonicalModelKey = sql.NullString{String: model.ModelKey, Valid: true}
			}
		}
		if log.SiteModelID.Valid {
			if model, ok := siteModelsByID[log.SiteModelID.UUID]; ok {
				detail.SiteModelUpstreamName = sql.NullString{String: model.UpstreamName, Valid: true}
				detail.SiteModelDisplayName = sql.NullString{String: model.DisplayName, Valid: true}
			}
		}
		if usage, ok := usageByRequestLogID[log.ID]; ok {
			detail.UsagePromptTokens = sql.NullInt64{Int64: int64(usage.PromptTokens), Valid: true}
			detail.UsageCompletionTokens = sql.NullInt64{Int64: int64(usage.CompletionTokens), Valid: true}
			detail.UsageTotalTokens = sql.NullInt64{Int64: int64(usage.TotalTokens), Valid: true}
			detail.EstimatedCost = usage.EstimatedCost
			detail.UsageCurrency = sql.NullString{String: usage.Currency, Valid: true}
		}
		details = append(details, detail)
	}
	return details, nil
}

func (r RequestLogRepository) usageByRequestLogID(ctx context.Context, logIDs []uuid.UUID) (map[uuid.UUID]UsageRecord, error) {
	result := map[uuid.UUID]UsageRecord{}
	if len(logIDs) == 0 {
		return result, nil
	}
	var usageRecords []UsageRecord
	if err := r.db.WithContext(ctx).Where(map[string]any{"request_log_id": logIDs}).Find(&usageRecords).Error; err != nil {
		return nil, err
	}
	for _, item := range usageRecords {
		result[item.RequestLogID] = item
	}
	return result, nil
}

func (r RequestLogRepository) requestLogFilterExpressions(ctx context.Context, params ListRequestLogsParams, includeSearch bool) ([]clause.Expression, error) {
	exprs := requestLogDirectFilterExpressions(params)

	if value := strings.TrimSpace(params.ModelKey); value != "" {
		modelExpr, err := r.requestLogModelFilterExpression(ctx, value)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, modelExpr)
	}

	if includeSearch {
		if value := strings.TrimSpace(params.RequestID); value != "" {
			exprs = append(exprs, clause.Like{
				Column: clause.Column{Name: "request_id"},
				Value:  requestLogLikePattern(value),
			})
		}
		if value := strings.TrimSpace(params.Search); value != "" {
			searchExpr, err := r.requestLogSearchExpression(ctx, value)
			if err != nil {
				return nil, err
			}
			exprs = append(exprs, searchExpr)
		}
	}

	return exprs, nil
}

func requestLogDirectFilterExpressions(params ListRequestLogsParams) []clause.Expression {
	exprs := make([]clause.Expression, 0, 8)
	if params.Success != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "success"}, Value: *params.Success})
	}
	if params.SiteID != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "site_id"}, Value: *params.SiteID})
	}
	if params.APIKeyID != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "api_key_id"}, Value: *params.APIKeyID})
	}
	if value := strings.TrimSpace(params.ErrorType); value != "" {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "error_type"}, Value: value})
	}
	if value := strings.TrimSpace(params.Endpoint); value != "" {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "endpoint"}, Value: value})
	}
	if params.HideWithoutSite {
		exprs = append(exprs, clause.Neq{Column: clause.Column{Name: "site_id"}, Value: nil})
	}
	if params.Internal != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "internal"}, Value: *params.Internal})
	}
	if params.ParentRequestLogID != nil {
		exprs = append(exprs, clause.Eq{Column: clause.Column{Name: "parent_request_log_id"}, Value: *params.ParentRequestLogID})
	}
	if params.CreatedFrom != nil {
		exprs = append(exprs, clause.Gte{Column: clause.Column{Name: "created_at"}, Value: *params.CreatedFrom})
	}
	if params.CreatedTo != nil {
		exprs = append(exprs, clause.Lte{Column: clause.Column{Name: "created_at"}, Value: *params.CreatedTo})
	}
	return exprs
}

func (r RequestLogRepository) requestLogModelFilterExpression(ctx context.Context, value string) (clause.Expression, error) {
	ids, err := r.requestLogSearchIDs(ctx, value)
	if err != nil {
		return nil, err
	}
	exprs := make([]clause.Expression, 0, 2)
	if len(ids.CanonicalModelIDs) > 0 {
		exprs = append(exprs, requestLogUUIDInExpression("canonical_model_id", ids.CanonicalModelIDs))
	}
	if len(ids.SiteModelIDs) > 0 {
		exprs = append(exprs, requestLogUUIDInExpression("site_model_id", ids.SiteModelIDs))
	}
	if len(exprs) == 0 {
		return requestLogNeverExpression(), nil
	}
	return clause.Or(exprs...), nil
}

func (r RequestLogRepository) requestLogSearchExpression(ctx context.Context, value string) (clause.Expression, error) {
	ids, err := r.requestLogSearchIDs(ctx, value)
	if err != nil {
		return nil, err
	}
	exprs := []clause.Expression{
		clause.Like{
			Column: clause.Column{Name: "request_id"},
			Value:  requestLogLikePattern(value),
		},
	}
	if len(ids.SiteIDs) > 0 {
		exprs = append(exprs, requestLogUUIDInExpression("site_id", ids.SiteIDs))
	}
	if len(ids.CanonicalModelIDs) > 0 {
		exprs = append(exprs, requestLogUUIDInExpression("canonical_model_id", ids.CanonicalModelIDs))
	}
	if len(ids.SiteModelIDs) > 0 {
		exprs = append(exprs, requestLogUUIDInExpression("site_model_id", ids.SiteModelIDs))
	}
	return clause.Or(exprs...), nil
}

type requestLogSearchIDs struct {
	SiteIDs           []uuid.UUID
	CanonicalModelIDs []uuid.UUID
	SiteModelIDs      []uuid.UUID
}

func (r RequestLogRepository) requestLogSearchIDs(ctx context.Context, value string) (requestLogSearchIDs, error) {
	needle := strings.ToLower(strings.TrimSpace(value))
	if needle == "" {
		return requestLogSearchIDs{}, nil
	}

	var sites []Site
	if err := r.db.WithContext(ctx).Find(&sites).Error; err != nil {
		return requestLogSearchIDs{}, fmt.Errorf("list sites for request search: %w", err)
	}
	var canonicalModels []CanonicalModel
	if err := r.db.WithContext(ctx).Find(&canonicalModels).Error; err != nil {
		return requestLogSearchIDs{}, fmt.Errorf("list canonical models for request search: %w", err)
	}
	var siteModels []SiteModel
	if err := r.db.WithContext(ctx).Find(&siteModels).Error; err != nil {
		return requestLogSearchIDs{}, fmt.Errorf("list site models for request search: %w", err)
	}

	result := requestLogSearchIDs{}
	for _, site := range sites {
		if requestLogTextMatches(needle, site.Name, site.Slug, site.SiteType) {
			result.SiteIDs = appendUUIDOnce(result.SiteIDs, site.ID)
		}
	}
	for _, model := range canonicalModels {
		if requestLogTextMatches(needle, model.ModelKey) {
			result.CanonicalModelIDs = appendUUIDOnce(result.CanonicalModelIDs, model.ID)
		}
	}
	for _, model := range siteModels {
		if requestLogTextMatches(needle, model.UpstreamName, model.DisplayName) {
			result.SiteModelIDs = appendUUIDOnce(result.SiteModelIDs, model.ID)
		}
	}
	return result, nil
}

func requestLogTextMatches(needle string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func requestLogLikePattern(value string) string {
	return "%" + strings.TrimSpace(value) + "%"
}

func requestLogUUIDInExpression(column string, values []uuid.UUID) clause.Expression {
	items := make([]interface{}, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}
	return clause.IN{Column: clause.Column{Name: column}, Values: items}
}

func requestLogNeverExpression() clause.Expression {
	return clause.Eq{Column: clause.Column{Name: "id"}, Value: uuid.Nil}
}

func appendUUIDOnce(values []uuid.UUID, value uuid.UUID) []uuid.UUID {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}

func requestLogCreatedRangeClause(start time.Time, end time.Time) clause.Where {
	return clause.Where{Exprs: []clause.Expression{
		clause.Gte{Column: clause.Column{Name: "created_at"}, Value: start},
		clause.Lt{Column: clause.Column{Name: "created_at"}, Value: end},
	}}
}
