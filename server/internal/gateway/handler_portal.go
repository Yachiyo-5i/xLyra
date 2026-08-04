package gateway

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/config"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/store"
	"xlyra/server/internal/usage"
)

func (h Handler) portalConfig() config.PortalConfig {
	return config.ReadPortalConfig(h.confFile)
}

func (h Handler) PortalSettings(w http.ResponseWriter, r *http.Request) {
	cfg := h.portalConfig()
	if !cfg.Enabled {
		h.writeGatewayError(w, r, http.StatusNotFound, "not_found", "route not found")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{
		"enabled":       true,
		"show_requests": cfg.ShowRequests,
		"show_summary":  cfg.ShowSummary,
		"summary_days":  cfg.SummaryDays,
		"dimensions":    portalDimensionsPayload(cfg.Dimensions),
	})
}

func (h Handler) PortalOverview(w http.ResponseWriter, r *http.Request) {
	cfg := h.portalConfig()
	if !cfg.Enabled {
		h.writeGatewayError(w, r, http.StatusNotFound, "not_found", "route not found")
		return
	}
	apiKey, ok := auth.APIKeyFromContext(r.Context())
	if !ok {
		h.writeGatewayError(w, r, http.StatusUnauthorized, "unauthorized", "valid api key is required")
		return
	}
	now := time.Now()
	dailyUsed := apiKey.EffectiveDailyQuotaUsed(now, h.recorder.timeZone)
	weeklyUsed := apiKey.EffectiveWeeklyQuotaUsed(now, h.recorder.timeZone)
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{
		"key": map[string]any{
			"name":         apiKey.Name,
			"status":       apiKey.Status,
			"is_active":    apiKey.Status == "active",
			"masked_key":   apiKey.MaskedKey,
			"expires_at":   portalTimePtr(apiKey.ExpiresAt),
			"last_used_at": portalTimePtr(apiKey.LastUsedAt),
		},
		"quota": map[string]any{
			"unit":      "USD",
			"limit":     userBalanceNullFloat64(apiKey.QuotaLimit),
			"used":      apiKey.QuotaUsed,
			"remaining": userBalanceAvailable(apiKey),
			"unlimited": apiKey.QuotaUnlimited,
			"daily": map[string]any{
				"limit":     userBalanceNullFloat64(apiKey.QuotaDailyLimit),
				"used":      dailyUsed,
				"remaining": userBalancePeriodicAvailable(apiKey.QuotaDailyLimit, dailyUsed, apiKey.QuotaDailyUnlimited),
				"unlimited": apiKey.QuotaDailyUnlimited,
				"reset_at":  userBalanceResetAt(h.recorder.timeZone.StartOfDay(now).AddDate(0, 0, 1), apiKey.QuotaDailyUnlimited),
			},
			"weekly": map[string]any{
				"limit":     userBalanceNullFloat64(apiKey.QuotaWeeklyLimit),
				"used":      weeklyUsed,
				"remaining": userBalancePeriodicAvailable(apiKey.QuotaWeeklyLimit, weeklyUsed, apiKey.QuotaWeeklyUnlimited),
				"unlimited": apiKey.QuotaWeeklyUnlimited,
				"reset_at":  userBalanceResetAt(h.recorder.timeZone.StartOfWeek(now).AddDate(0, 0, 7), apiKey.QuotaWeeklyUnlimited),
			},
		},
	})
}

func (h Handler) PortalSummary(w http.ResponseWriter, r *http.Request) {
	cfg := h.portalConfig()
	if !cfg.Enabled {
		h.writeGatewayError(w, r, http.StatusNotFound, "not_found", "route not found")
		return
	}
	if !cfg.ShowSummary {
		h.writeGatewayError(w, r, http.StatusForbidden, "portal_summary_disabled", "usage summary is not available")
		return
	}
	if h.usage == nil {
		h.writeGatewayError(w, r, http.StatusServiceUnavailable, "usage_unavailable", "usage service is not available")
		return
	}
	apiKey, ok := auth.APIKeyFromContext(r.Context())
	if !ok {
		h.writeGatewayError(w, r, http.StatusUnauthorized, "unauthorized", "valid api key is required")
		return
	}

	days := cfg.SummaryDays
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.writeGatewayError(w, r, http.StatusBadRequest, "invalid_days", "days must be a positive integer")
			return
		}
		if parsed > 90 {
			parsed = 90
		}
		days = parsed
	}

	trend, err := h.usage.KeyDailyTrend(r.Context(), apiKey.ID, days, time.Now())
	if err != nil {
		h.writeGatewayError(w, r, http.StatusInternalServerError, "portal_summary_failed", "failed to load usage summary")
		return
	}

	dims := cfg.Dimensions
	buckets := make([]map[string]any, 0, len(trend.Buckets))
	for _, bucket := range trend.Buckets {
		buckets = append(buckets, portalUsageBucket(bucket, dims))
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{
		"range": map[string]any{
			"from": trend.From.Format(time.RFC3339),
			"to":   trend.To.Format(time.RFC3339),
			"days": trend.Days,
		},
		"currency": trend.Currency,
		"totals":   portalUsageBucket(trend.Totals, dims),
		"trend":    buckets,
	})
}

func (h Handler) PortalRequests(w http.ResponseWriter, r *http.Request) {
	cfg := h.portalConfig()
	if !cfg.Enabled {
		h.writeGatewayError(w, r, http.StatusNotFound, "not_found", "route not found")
		return
	}
	if !cfg.ShowRequests {
		h.writeGatewayError(w, r, http.StatusForbidden, "portal_requests_disabled", "request details are not available")
		return
	}
	if h.usage == nil {
		h.writeGatewayError(w, r, http.StatusServiceUnavailable, "usage_unavailable", "usage service is not available")
		return
	}
	apiKey, ok := auth.APIKeyFromContext(r.Context())
	if !ok {
		h.writeGatewayError(w, r, http.StatusUnauthorized, "unauthorized", "valid api key is required")
		return
	}

	page := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.writeGatewayError(w, r, http.StatusBadRequest, "invalid_page", "page must be a positive integer")
			return
		}
		page = parsed
	}

	pageSize := cfg.RequestPageSizeMax
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.writeGatewayError(w, r, http.StatusBadRequest, "invalid_page_size", "page_size must be a positive integer")
			return
		}
		if parsed > cfg.RequestPageSizeMax {
			parsed = cfg.RequestPageSizeMax
		}
		pageSize = parsed
	}

	keyID := apiKey.ID
	query := usage.RequestQuery{
		Page:     page,
		PageSize: pageSize,
		APIKeyID: &keyID,
	}

	switch strings.TrimSpace(r.URL.Query().Get("status")) {
	case "success":
		value := true
		query.Success = &value
	case "failed":
		value := false
		query.Success = &value
	}
	if cfg.Dimensions.Model {
		if model := strings.TrimSpace(r.URL.Query().Get("model")); model != "" {
			query.ModelKey = model
		}
	}
	from, ok := parsePortalTime(w, r, "from")
	if !ok {
		return
	}
	to, ok := parsePortalTime(w, r, "to")
	if !ok {
		return
	}
	query.CreatedFrom = from
	query.CreatedTo = to

	result, err := h.usage.ListRequestsPage(r.Context(), query)
	if err != nil {
		h.writeGatewayError(w, r, http.StatusInternalServerError, "portal_requests_failed", "failed to list requests")
		return
	}

	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, portalRequestItem(item, cfg.Dimensions))
	}
	totalPages := 1
	if result.PageSize > 0 {
		totalPages = int((result.Total + int64(result.PageSize) - 1) / int64(result.PageSize))
	}
	if totalPages < 1 {
		totalPages = 1
	}

	stats := map[string]any{}
	if cfg.Dimensions.Tokens || cfg.Dimensions.Cost {
		if summary, err := h.usage.RequestSummary(r.Context(), query, time.Now()); err == nil && summary.Supported {
			if cfg.Dimensions.Tokens {
				stats["total_tokens"] = summary.TotalTokens
			}
			if cfg.Dimensions.Cost {
				stats["cost"] = summary.TotalCost
				stats["currency"] = summary.Currency
			}
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"page":        result.Page,
		"page_size":   result.PageSize,
		"total":       result.Total,
		"total_pages": totalPages,
		"stats":       stats,
	})
}

func (h Handler) PortalModels(w http.ResponseWriter, r *http.Request) {
	cfg := h.portalConfig()
	if !cfg.Enabled {
		h.writeGatewayError(w, r, http.StatusNotFound, "not_found", "route not found")
		return
	}
	apiKey, ok := auth.APIKeyFromContext(r.Context())
	if !ok {
		h.writeGatewayError(w, r, http.StatusUnauthorized, "unauthorized", "valid api key is required")
		return
	}

	models := []string{}
	if cfg.ShowRequests && cfg.Dimensions.Model && h.usage != nil {
		found, err := h.usage.KeyModels(r.Context(), apiKey.ID, cfg.SummaryDays)
		if err != nil {
			h.writeGatewayError(w, r, http.StatusInternalServerError, "portal_models_failed", "failed to list models")
			return
		}
		models = found
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, map[string]any{"models": models})
}

func parsePortalTime(w http.ResponseWriter, r *http.Request, param string) (*time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(param))
	if raw == "" {
		return nil, true
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_time_range", param+" must be an RFC3339 timestamp")
		return nil, false
	}
	return &parsed, true
}

func portalDimensionsPayload(dims config.PortalDimensions) map[string]any {
	return map[string]any{
		"site":     dims.Site,
		"model":    dims.Model,
		"tokens":   dims.Tokens,
		"cost":     dims.Cost,
		"latency":  dims.Latency,
		"endpoint": dims.Endpoint,
		"upstream": dims.Upstream,
		"error":    dims.Error,
	}
}

func portalUsageBucket(bucket usage.KeyDailyUsage, dims config.PortalDimensions) map[string]any {
	payload := map[string]any{
		"date":     bucket.Date,
		"requests": bucket.Requests,
		"success":  bucket.Success,
	}
	if dims.Tokens {
		payload["prompt_tokens"] = bucket.PromptTokens
		payload["completion_tokens"] = bucket.CompletionTokens
		payload["total_tokens"] = bucket.TotalTokens
	}
	if dims.Cost {
		payload["cost"] = bucket.Cost
	}
	return payload
}

func portalRequestItem(item store.RequestLogDetail, dims config.PortalDimensions) map[string]any {
	metadata := portalMetadata(item.Metadata)
	costCalc, _ := metadata["cost_calculation"].(map[string]any)
	payload := map[string]any{
		"id":          item.ID.String(),
		"request_id":  item.RequestID,
		"created_at":  item.CreatedAt.Format(time.RFC3339),
		"status_code": item.StatusCode,
		"success":     item.Success,
	}
	if dims.Endpoint {
		payload["endpoint"] = item.Endpoint
	}
	if dims.Latency {
		payload["latency_ms"] = portalNullInt(item.LatencyMS)
	}
	if dims.Site {
		payload["site"] = map[string]any{
			"name":      portalNullString(item.SiteName),
			"slug":      portalNullString(item.SiteSlug),
			"site_type": portalNullString(item.SiteType),
		}
	}
	if dims.Model {
		payload["model"] = map[string]any{
			"canonical_model": portalNullString(item.CanonicalModelKey),
			"upstream_model":  portalNullString(item.SiteModelUpstreamName),
			"display_name":    portalNullString(item.SiteModelDisplayName),
		}
	}
	if dims.Tokens {
		payload["usage"] = map[string]any{
			"prompt_tokens":      portalMetaNumberOr(costCalc, item.UsagePromptTokens, "prompt_tokens"),
			"completion_tokens":  portalMetaNumberOr(costCalc, item.UsageCompletionTokens, "completion_tokens"),
			"total_tokens":       portalNullInt(item.UsageTotalTokens),
			"cache_tokens":       portalMetaNumber(costCalc, "cache_tokens", "cached_tokens"),
			"cache_write_tokens": portalMetaNumber(costCalc, "cache_write_tokens", "cache_creation_tokens"),
		}
	}
	if dims.Cost {
		payload["cost"] = map[string]any{
			"estimated_cost": portalNullFloat(item.EstimatedCost),
			"currency":       portalNullString(item.UsageCurrency),
		}
	}
	if dims.Upstream {
		payload["upstream"] = map[string]any{
			"url":  metadata["upstream_url"],
			"path": metadata["upstream_path"],
		}
	}
	if dims.Error && !item.Success {
		payload["error_type"] = portalNullString(item.ErrorType)
	}
	return payload
}

func portalMetaNumber(source map[string]any, keys ...string) any {
	for _, key := range keys {
		switch value := source[key].(type) {
		case float64:
			return int64(value)
		case int64:
			return value
		case int:
			return int64(value)
		}
	}
	return nil
}

func portalMetaNumberOr(source map[string]any, fallback sql.NullInt64, keys ...string) any {
	if value := portalMetaNumber(source, keys...); value != nil {
		return value
	}
	return portalNullInt(fallback)
}

func portalMetadata(raw store.JSON) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed == nil {
		return map[string]any{}
	}
	return parsed
}

func portalTimePtr(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.Format(time.RFC3339)
}

func portalNullInt(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func portalNullString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func portalNullFloat(value sql.NullFloat64) any {
	if value.Valid {
		return value.Float64
	}
	return nil
}
