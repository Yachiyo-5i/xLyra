package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"xlyra/server/internal/store"
	"xlyra/server/internal/usage"
)

func (h Handler) ListRequestLogs(w http.ResponseWriter, r *http.Request) {
	if h.usage == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "request_log_service_unavailable", "request log service is not available")
		return
	}

	page := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_page", "page must be a positive integer")
			return
		}
		page = parsed
	}

	pageSize := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_page_size", "page_size must be a positive integer")
			return
		}
		if parsed > 200 {
			parsed = 200
		}
		pageSize = parsed
	}

	query, ok := h.requestLogFilters(w, r)
	if !ok {
		return
	}
	query.Page = page
	query.PageSize = pageSize

	result, err := h.usage.ListRequestsPage(r.Context(), query)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "request_log_list_failed", "failed to list request logs")
		return
	}

	payloadItems := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		payloadItems = append(payloadItems, requestLogPayload(item, false))
	}
	totalPages := 1
	if result.PageSize > 0 {
		totalPages = int((result.Total + int64(result.PageSize) - 1) / int64(result.PageSize))
	}
	if totalPages < 1 {
		totalPages = 1
	}
	recentUsage, err := h.usage.RecentRateUsage(r.Context(), time.Now())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "request_rate_usage_failed", "failed to load recent request rate usage")
		return
	}

	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{
		"count":             len(payloadItems),
		"total":             result.Total,
		"page":              result.Page,
		"page_size":         result.PageSize,
		"total_pages":       totalPages,
		"success":           query.Success,
		"site_id":           uuidPtrString(query.SiteID),
		"api_key_id":        uuidPtrString(query.APIKeyID),
		"search":            emptyStringAsNil(strings.TrimSpace(r.URL.Query().Get("search"))),
		"model_key":         emptyStringAsNil(strings.TrimSpace(r.URL.Query().Get("model_key"))),
		"error_type":        emptyStringAsNil(strings.TrimSpace(r.URL.Query().Get("error_type"))),
		"endpoint":          emptyStringAsNil(strings.TrimSpace(r.URL.Query().Get("endpoint"))),
		"request_id":        emptyStringAsNil(strings.TrimSpace(r.URL.Query().Get("request_id"))),
		"hide_without_site": query.HideWithoutSite,
		"created_from":      timePtrString(query.CreatedFrom),
		"created_to":        timePtrString(query.CreatedTo),
		"rate_usage": map[string]any{
			"rpm":            recentUsage.RPM,
			"tpm":            recentUsage.TPM,
			"window_seconds": 60,
		},
	})
}

func (h Handler) RequestLogSummary(w http.ResponseWriter, r *http.Request) {
	if h.usage == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "request_log_service_unavailable", "request log service is not available")
		return
	}

	query, ok := h.requestLogFilters(w, r)
	if !ok {
		return
	}
	result, err := h.usage.RequestSummary(r.Context(), query, time.Now())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "request_log_summary_failed", "failed to load request log summary")
		return
	}

	var totalCost any = result.TotalCost
	var promptTokens any = result.PromptTokens
	var completionTokens any = result.CompletionTokens
	var totalTokens any = result.TotalTokens
	var cachedTokens any = result.CachedTokens
	if !result.Supported {
		totalCost = nil
		promptTokens = nil
		completionTokens = nil
		totalTokens = nil
		cachedTokens = nil
	}
	h.writeResource(w, http.StatusOK, "summary", map[string]any{
		"total_cost":         totalCost,
		"prompt_tokens":      promptTokens,
		"completion_tokens":  completionTokens,
		"total_tokens":       totalTokens,
		"cached_tokens":      cachedTokens,
		"currency":           result.Currency,
		"supported":          result.Supported,
		"unsupported_reason": emptyStringAsNil(result.UnsupportedReason),
	})
}

func (h Handler) RequestChannelSplit(w http.ResponseWriter, r *http.Request) {
	if h.usage == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "request_log_service_unavailable", "request log service is not available")
		return
	}

	siteIDs, ok := parseSiteIDs(w, r, r.URL.Query()["site_id"])
	if !ok {
		return
	}
	oauthConnectionID, ok := optionalUUIDQuery(w, r, "oauth_connection_id")
	if !ok {
		return
	}

	result, err := h.usage.ChannelSplit(r.Context(), usage.ChannelSplitQuery{
		SiteIDs:           siteIDs,
		OAuthConnectionID: oauthConnectionID,
		Range:             strings.TrimSpace(r.URL.Query().Get("range")),
		DateFrom:          strings.TrimSpace(r.URL.Query().Get("date_from")),
		DateTo:            strings.TrimSpace(r.URL.Query().Get("date_to")),
	}, time.Now())
	if err != nil {
		if errors.Is(err, usage.ErrInvalidChannelSplitQuery) {
			h.writeError(w, r, http.StatusBadRequest, "invalid_channel_split_query", err.Error())
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			h.writeError(w, r, http.StatusNotFound, "channel_split_target_not_found", "channel split target was not found")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "channel_split_failed", "failed to load channel split")
		return
	}

	h.writePayload(w, http.StatusOK, result)
}

func (h Handler) GetRequestLog(w http.ResponseWriter, r *http.Request) {
	if h.usage == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "request_log_service_unavailable", "request log service is not available")
		return
	}

	requestLogID, ok := requestLogIDParam(w, r)
	if !ok {
		return
	}

	item, err := h.usage.GetRequest(r.Context(), requestLogID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			h.writeError(w, r, http.StatusNotFound, "request_log_not_found", "request log was not found")
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "request_log_get_failed", "failed to load request log")
		return
	}

	payload := requestLogPayload(item, true)
	if parentRequestID := requestLogParentRequestID(item); parentRequestID != "" {
		attempts, err := h.usage.ListRequestAttempts(r.Context(), parentRequestID)
		if err != nil {
			h.logWarn("failed to load request failover trace", "request_log_id", item.ID, "parent_request_id", parentRequestID, "error", err)
		} else if trace := requestLogFailoverTracePayload(attempts); trace != nil {
			payload["failover_trace"] = trace
		}
	}

	h.writeResource(w, http.StatusOK, "request", payload)
}

func (h Handler) requestLogFilters(w http.ResponseWriter, r *http.Request) (usage.RequestQuery, bool) {
	var success *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("success")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_success", "success must be true or false")
			return usage.RequestQuery{}, false
		}
		success = &parsed
	}

	hideWithoutSite := false
	if raw := strings.TrimSpace(r.URL.Query().Get("hide_without_site")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_hide_without_site", "hide_without_site must be true or false")
			return usage.RequestQuery{}, false
		}
		hideWithoutSite = parsed
	}

	siteID, ok := optionalUUIDQuery(w, r, "site_id")
	if !ok {
		return usage.RequestQuery{}, false
	}
	apiKeyID, ok := optionalUUIDQuery(w, r, "api_key_id")
	if !ok {
		return usage.RequestQuery{}, false
	}
	createdFrom, ok := optionalTimeQuery(w, r, "created_from")
	if !ok {
		return usage.RequestQuery{}, false
	}
	createdTo, ok := optionalTimeQuery(w, r, "created_to")
	if !ok {
		return usage.RequestQuery{}, false
	}

	return usage.RequestQuery{
		Success:         success,
		SiteID:          siteID,
		APIKeyID:        apiKeyID,
		Search:          strings.TrimSpace(r.URL.Query().Get("search")),
		ModelKey:        strings.TrimSpace(r.URL.Query().Get("model_key")),
		ErrorType:       strings.TrimSpace(r.URL.Query().Get("error_type")),
		Endpoint:        strings.TrimSpace(r.URL.Query().Get("endpoint")),
		RequestID:       strings.TrimSpace(r.URL.Query().Get("request_id")),
		HideWithoutSite: hideWithoutSite,
		CreatedFrom:     createdFrom,
		CreatedTo:       createdTo,
	}, true
}

func requestLogPayload(item store.RequestLogDetail, includeMetadata bool) map[string]any {
	metadata := requestLogMetadata(item.Metadata)
	attempt := intFromAny(metadata["attempt"])
	credentialAttempt := intFromAny(metadata["credential_attempt"])
	credential := requestLogCredentialProjection(metadata)
	payload := map[string]any{
		"id":                          item.ID.String(),
		"request_id":                  item.RequestID,
		"parent_request_id":           emptyStringAsNil(requestLogParentRequestID(item)),
		"scope":                       metadataString(metadata, "scope"),
		"is_test":                     metadata["test"] == true,
		"stream":                      metadata["stream"],
		"response_mode":               metadata["response_mode"],
		"stream_started":              requestMetadataBool(metadata, "stream_started"),
		"stream_completed":            requestMetadataBool(metadata, "stream_completed"),
		"stream_received_done":        requestMetadataBool(metadata, "stream_received_done"),
		"stream_incomplete":           requestMetadataBool(metadata, "stream_incomplete"),
		"stream_end_reason":           metadataString(metadata, "stream_end_reason"),
		"stream_failure_scope":        metadata["stream_failure_scope"],
		"pre_output_events_buffered":  requestMetadataInt64(metadata, "pre_output_events_buffered"),
		"pre_output_failure_deferred": requestMetadataBool(metadata, "pre_output_failure_deferred"),
		"protocol_conversion":         metadata["protocol_conversion"],
		"stage":                       metadataString(metadata, "stage"),
		"requested_model":             metadataString(metadata, "requested_model"),
		"original_model":              metadataString(metadata, "original_model"),
		"mapped_model":                metadataString(metadata, "mapped_model"),
		"mapping_mode":                metadataString(metadata, "mapping_mode"),
		"attempt":                     attempt,
		"credential_attempt":          credentialAttempt,
		"credential_total":            intFromAny(metadata["credential_total"]),
		"failover":                    attempt > 1 || credentialAttempt > 1,
		"next_credential_available":   requestMetadataBool(metadata, "next_credential_available"),
		"should_try_next_credential":  requestMetadataBool(metadata, "should_try_next_credential"),
		"failover_action":             metadataString(metadata, "failover_action"),
		"endpoint":                    item.Endpoint,
		"downstream_path":             valueOrFallback(metadata["downstream_path"], item.Endpoint),
		"upstream_path":               metadata["upstream_path"],
		"upstream_url":                metadata["upstream_url"],
		"status_code":                 item.StatusCode,
		"upstream_status_code":        metadata["upstream_status_code"],
		"success":                     item.Success,
		"error_type":                  nullStringValue(item.ErrorType),
		"latency_ms":                  nullInt64Value(item.LatencyMS),
		"first_byte_latency_ms":       requestMetadataInt64(metadata, "first_byte_latency"),
		"reasoning_effort":            metadataString(metadata, "reasoning_effort"),
		"upstream_latency_ms":         nullInt64Value(item.UpstreamLatencyMS),
		"request_tokens":              nullInt64Value(item.RequestTokens),
		"response_tokens":             nullInt64Value(item.ResponseTokens),
		"pricing_group":               metadata["pricing_group"],
		"api_key": map[string]any{
			"id":         nullUUIDValue(item.APIKeyID),
			"name":       nullStringValue(item.APIKeyName),
			"masked_key": nullStringValue(item.APIKeyMaskedKey),
		},
		"credential": credential,
		"site":       requestLogSiteProjection(item, metadata),
		"model":      requestLogModelProjection(item, metadata),
		"usage": map[string]any{
			"prompt_tokens":     nullInt64Value(item.UsagePromptTokens),
			"completion_tokens": nullInt64Value(item.UsageCompletionTokens),
			"total_tokens":      nullInt64Value(item.UsageTotalTokens),
			"estimated_cost":    nullFloat64Value(item.EstimatedCost),
			"currency":          nullStringValue(item.UsageCurrency),
		},
		"pricing":          metadataMap(metadata, "pricing"),
		"cost_calculation": metadataMap(metadata, "cost_calculation"),
		"rate_limit":       metadataMap(metadata, "rate_limit"),
		"created_at":       timeString(item.CreatedAt),
	}

	if !item.Success {
		payload["failure_response"] = valueOrFallback(metadata["upstream_response"], metadata["error_response"])
	}
	if includeMetadata {
		payload["metadata"] = requestLogMetadataForResponse(metadata)
	}

	return payload
}

func requestLogParentRequestID(item store.RequestLogDetail) string {
	if value := strings.TrimSpace(item.ParentRequestID.String); item.ParentRequestID.Valid && value != "" {
		return value
	}
	value, _ := metadataString(requestLogMetadata(item.Metadata), "parent_request_id").(string)
	return strings.TrimSpace(value)
}

type requestLogFailoverChannel struct {
	key  string
	item store.RequestLogDetail
}

func requestLogFailoverTracePayload(attempts []store.RequestLogDetail) map[string]any {
	channels := requestLogFailoverChannels(attempts)
	credentialAttempts := requestLogCredentialAttemptsPayload(attempts)
	if len(channels) == 0 || (len(channels) == 1 && len(credentialAttempts) <= 1) {
		return nil
	}

	defaultChannel := requestLogFailoverChannelPayload(channels[0].item)
	if defaultChannel == nil {
		return nil
	}
	intermediateChannels := make([]map[string]any, 0, max(len(channels)-2, 0))
	for _, channel := range channels[1:max(len(channels)-1, 1)] {
		if payload := requestLogFailoverChannelPayload(channel.item); payload != nil {
			intermediateChannels = append(intermediateChannels, payload)
		}
	}

	finalChannel := requestLogFailoverChannelPayload(channels[len(channels)-1].item)
	if finalChannel == nil {
		return nil
	}
	trace := map[string]any{
		"default_channel":       defaultChannel,
		"intermediate_channels": intermediateChannels,
		"final_channel":         finalChannel,
	}
	if len(credentialAttempts) > 0 {
		trace["credential_attempts"] = credentialAttempts
	}
	return trace
}

func requestLogFailoverChannels(attempts []store.RequestLogDetail) []requestLogFailoverChannel {
	channels := make([]requestLogFailoverChannel, 0, len(attempts))
	for _, item := range attempts {
		if requestLogFailoverChannelPayload(item) == nil {
			continue
		}
		key := requestLogFailoverChannelKey(item)
		if len(channels) > 0 && channels[len(channels)-1].key == key {
			channels[len(channels)-1].item = item
			continue
		}
		channels = append(channels, requestLogFailoverChannel{key: key, item: item})
	}
	return channels
}

func requestLogFailoverChannelKey(item store.RequestLogDetail) string {
	metadata := requestLogMetadata(item.Metadata)
	if siteModelID, ok := metadataString(metadata, "site_model_id").(string); ok {
		return "site_model:" + siteModelID
	}
	if siteID, ok := metadataString(metadata, "site_id").(string); ok {
		return "site:" + siteID
	}
	site := requestLogSiteProjection(item, metadata)
	siteName, _ := site["name"].(string)
	upstreamModel, _ := requestLogModelProjection(item, metadata)["upstream_model"].(string)
	return strings.TrimSpace(siteName) + "\x00" + strings.TrimSpace(upstreamModel)
}

func requestLogFailoverChannelPayload(item store.RequestLogDetail) map[string]any {
	metadata := requestLogMetadata(item.Metadata)
	site := requestLogSiteProjection(item, metadata)
	if site["id"] == nil && site["name"] == nil {
		return nil
	}
	return map[string]any{
		"success":              item.Success,
		"site":                 site,
		"error_type":           valueOrFallback(nullStringValue(item.ErrorType), metadataString(metadata, "error_type")),
		"status_code":          requestLogStatusCode(item.StatusCode),
		"upstream_status_code": requestLogStatusCode(intFromAny(metadata["upstream_status_code"])),
	}
}

func requestLogCredentialAttemptsPayload(attempts []store.RequestLogDetail) []map[string]any {
	result := make([]map[string]any, 0, len(attempts))
	for _, item := range attempts {
		metadata := requestLogMetadata(item.Metadata)
		credentialAttempt := intFromAny(metadata["credential_attempt"])
		credential := requestLogCredentialProjection(metadata)
		if credentialAttempt <= 0 && credential == nil {
			continue
		}
		payload := map[string]any{
			"success":              item.Success,
			"site":                 requestLogSiteProjection(item, metadata),
			"credential":           credential,
			"credential_attempt":   credentialAttempt,
			"credential_total":     intFromAny(metadata["credential_total"]),
			"error_type":           valueOrFallback(nullStringValue(item.ErrorType), metadataString(metadata, "error_type")),
			"status_code":          requestLogStatusCode(item.StatusCode),
			"upstream_status_code": requestLogStatusCode(intFromAny(metadata["upstream_status_code"])),
		}
		result = append(result, payload)
	}
	return result
}

func requestLogStatusCode(value int) any {
	if value < http.StatusContinue || value > 599 {
		return nil
	}
	return value
}

func requestLogSiteProjection(item store.RequestLogDetail, metadata map[string]any) map[string]any {
	return map[string]any{
		"id":        valueOrFallback(metadata["site_id"], nullUUIDValue(item.SiteID)),
		"name":      valueOrFallback(metadata["site_name"], nullStringValue(item.SiteName)),
		"slug":      valueOrFallback(metadata["site_slug"], nullStringValue(item.SiteSlug)),
		"site_type": valueOrFallback(metadata["site_type"], nullStringValue(item.SiteType)),
	}
}

func requestLogModelProjection(item store.RequestLogDetail, metadata map[string]any) map[string]any {
	return map[string]any{
		"canonical_model_id": nullUUIDValue(item.CanonicalModelID),
		"canonical_model":    nullStringValue(item.CanonicalModelKey),
		"site_model_id":      valueOrFallback(metadata["site_model_id"], nullUUIDValue(item.SiteModelID)),
		"upstream_model":     valueOrFallback(metadata["upstream_model"], nullStringValue(item.SiteModelUpstreamName)),
		"display_name":       valueOrFallback(metadata["site_model_display_name"], nullStringValue(item.SiteModelDisplayName)),
	}
}

func requestLogCredentialProjection(metadata map[string]any) any {
	credentialMeta, _ := metadata["credential"].(map[string]any)
	id := metadataString(metadata, "credential_id")
	name := metadataString(metadata, "credential_name")
	if credentialMeta != nil {
		id = valueOrFallback(metadataString(credentialMeta, "id"), id)
		name = valueOrFallback(metadataString(credentialMeta, "name"), name)
	}
	if id == nil && name == nil {
		return nil
	}
	return map[string]any{
		"id":   id,
		"name": name,
	}
}

func requestMetadataInt64(metadata map[string]any, key string) any {
	var value int64
	switch raw := metadata[key].(type) {
	case int:
		value = int64(raw)
	case int8:
		value = int64(raw)
	case int16:
		value = int64(raw)
	case int32:
		value = int64(raw)
	case int64:
		value = raw
	case uint:
		value = int64(raw)
	case uint8:
		value = int64(raw)
	case uint16:
		value = int64(raw)
	case uint32:
		value = int64(raw)
	case uint64:
		if raw > uint64(^uint64(0)>>1) {
			return nil
		}
		value = int64(raw)
	case float32:
		if float32(int64(raw)) != raw {
			return nil
		}
		value = int64(raw)
	case float64:
		if float64(int64(raw)) != raw {
			return nil
		}
		value = int64(raw)
	default:
		return nil
	}
	if value <= 0 {
		return nil
	}
	return value
}

func requestMetadataBool(metadata map[string]any, key string) any {
	value, ok := metadata[key].(bool)
	if !ok {
		return nil
	}
	return value
}

func requestLogMetadataForResponse(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(metadata))
	for key, value := range metadata {
		result[key] = value
	}
	delete(result, "credential_masked")
	if credential, ok := metadata["credential"].(map[string]any); ok {
		clean := make(map[string]any, len(credential))
		for key, value := range credential {
			clean[key] = value
		}
		delete(clean, "masked_secret")
		result["credential"] = clean
	}
	return result
}
