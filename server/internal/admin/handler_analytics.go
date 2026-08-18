package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/analytics"
	"xlyra/server/internal/config"
)

func (h Handler) AnalyticsUsage(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "analytics_service_unavailable", "analytics service is not available")
		return
	}

	params, ok := h.parseAnalyticsUsageParams(w, r)
	if !ok {
		return
	}

	payload, err := h.analytics.Usage(r.Context(), params)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "analytics_usage_failed", "failed to load usage analytics")
		return
	}

	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) AnalyticsOptions(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "analytics_service_unavailable", "analytics service is not available")
		return
	}
	payload, err := h.analytics.Options(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "analytics_options_failed", "failed to load analytics options")
		return
	}
	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) AnalyticsAPIKeyContributions(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "analytics_service_unavailable", "analytics service is not available")
		return
	}
	success := true
	payload, err := h.analytics.Contributions(r.Context(), time.Now(), &success)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "analytics_contributions_failed", "failed to load analytics contributions")
		return
	}
	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) AnalyticsDataset(w http.ResponseWriter, r *http.Request) {
	if h.analytics == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "analytics_service_unavailable", "analytics service is not available")
		return
	}
	params, ok := h.parseAnalyticsUsageParams(w, r)
	if !ok {
		return
	}
	success := true
	params.Success = &success
	payload, err := h.analytics.Dataset(r.Context(), params)
	if err != nil {
		var tooLarge analytics.DatasetTooLargeError
		if errors.As(err, &tooLarge) {
			h.writeError(w, r, http.StatusRequestEntityTooLarge, "analytics_dataset_too_large", "analytics dataset is too large for local filtering")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "analytics_dataset_failed", "failed to load analytics dataset")
		return
	}
	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) parseAnalyticsUsageParams(w http.ResponseWriter, r *http.Request) (analytics.UsageParams, bool) {
	query := r.URL.Query()
	timeZone := config.TimeZoneOrDefault(h.timeZone)
	params := analytics.UsageParams{Now: time.Now()}

	if raw := strings.TrimSpace(query.Get("from")); raw != "" {
		value, err := time.ParseInLocation("2006-01-02", raw, timeZone.Location)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_from", "from must be a YYYY-MM-DD date")
			return analytics.UsageParams{}, false
		}
		params.From = value
	}
	if raw := strings.TrimSpace(query.Get("to")); raw != "" {
		value, err := time.ParseInLocation("2006-01-02", raw, timeZone.Location)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_to", "to must be a YYYY-MM-DD date")
			return analytics.UsageParams{}, false
		}
		params.To = value
	}
	if !params.From.IsZero() && !params.To.IsZero() && params.From.After(params.To) {
		h.writeError(w, r, http.StatusBadRequest, "invalid_date_range", "from must not be after to")
		return analytics.UsageParams{}, false
	}

	if raw := strings.TrimSpace(query.Get("group_by")); raw != "" {
		if !analytics.ValidGroupBy(raw) {
			h.writeError(w, r, http.StatusBadRequest, "invalid_group_by", "group_by must be one of none, site, model, site_model, api_key, endpoint, error_type")
			return analytics.UsageParams{}, false
		}
		params.GroupBy = raw
	}

	siteIDs, ok := h.parseAnalyticsUUIDList(w, r, "site_ids")
	if !ok {
		return analytics.UsageParams{}, false
	}
	params.SiteIDs = siteIDs

	apiKeyIDs, ok := h.parseAnalyticsUUIDList(w, r, "api_key_ids")
	if !ok {
		return analytics.UsageParams{}, false
	}
	params.APIKeyIDs = apiKeyIDs

	if raw := strings.TrimSpace(query.Get("model_keys")); raw != "" {
		for _, item := range strings.Split(raw, ",") {
			if value := strings.TrimSpace(item); value != "" {
				params.ModelKeys = append(params.ModelKeys, value)
			}
		}
	}

	if raw := strings.TrimSpace(query.Get("success")); raw != "" {
		switch strings.ToLower(raw) {
		case "true":
			value := true
			params.Success = &value
		case "false":
			value := false
			params.Success = &value
		default:
			h.writeError(w, r, http.StatusBadRequest, "invalid_success", "success must be true or false")
			return analytics.UsageParams{}, false
		}
	}

	params.Currency = strings.TrimSpace(query.Get("currency"))
	if raw := strings.TrimSpace(query.Get("include_contributions")); raw != "" {
		switch strings.ToLower(raw) {
		case "true":
			value := true
			params.IncludeContributions = &value
		case "false":
			value := false
			params.IncludeContributions = &value
		default:
			h.writeError(w, r, http.StatusBadRequest, "invalid_include_contributions", "include_contributions must be true or false")
			return analytics.UsageParams{}, false
		}
	}
	return params, true
}

func (h Handler) parseAnalyticsUUIDList(w http.ResponseWriter, r *http.Request, key string) ([]uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, true
	}
	values := make([]uuid.UUID, 0)
	for _, item := range strings.Split(raw, ",") {
		text := strings.TrimSpace(item)
		if text == "" {
			continue
		}
		value, err := uuid.Parse(text)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_"+key, key+" must be a comma-separated list of UUIDs")
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}
