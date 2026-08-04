package admin

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
	"xlyra/server/internal/usage"
)

func (h Handler) ListRoutes(w http.ResponseWriter, r *http.Request) {
	if h.router == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "router_service_unavailable", "router service is not available")
		return
	}

	windowHours := 24
	if raw := strings.TrimSpace(r.URL.Query().Get("window_hours")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_window_hours", "window_hours must be a positive integer")
			return
		}
		if parsed > 168 {
			parsed = 168
		}
		windowHours = parsed
	}

	items, err := h.router.Overview(r.Context(), time.Now().Add(-time.Duration(windowHours)*time.Hour))
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "route_overview_failed", "failed to list route overview")
		return
	}

	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, routeOverviewPayload(item))
	}

	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{
		"count":        len(payloadItems),
		"window_hours": windowHours,
	})
}

func (h Handler) ListRouteCooldowns(w http.ResponseWriter, r *http.Request) {
	if h.router == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "router_service_unavailable", "router service is not available")
		return
	}

	items, err := h.router.ActiveCooldowns(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "route_cooldowns_failed", "failed to list route cooldowns")
		return
	}

	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, map[string]any{
			"id":                 item.ID.String(),
			"site_id":            item.SiteID.String(),
			"site_model_id":      nullUUIDValue(item.SiteModelID),
			"site_credential_id": nullUUIDValue(item.SiteCredentialID),
			"scope":              item.Scope,
			"source":             item.Source,
			"reason":             item.Reason,
			"active_until":       timeString(item.ActiveUntil),
			"remaining_seconds":  max(int(time.Until(item.ActiveUntil).Seconds()), 0),
			"cleared_at":         nullTimeValue(item.ClearedAt),
			"metadata":           jsonRaw(item.Metadata),
			"created_at":         timeString(item.CreatedAt),
			"updated_at":         timeString(item.UpdatedAt),
		})
	}

	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{"count": len(payloadItems)})
}

func (h Handler) CreateRouteCooldown(w http.ResponseWriter, r *http.Request) {
	if h.router == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "router_service_unavailable", "router service is not available")
		return
	}

	var payload routeCooldownRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if payload.SiteID == uuid.Nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_site_id", "site_id is required")
		return
	}

	duration := time.Duration(payload.DurationSeconds) * time.Second
	item, err := h.router.ActivateCooldown(r.Context(), routeengine.CooldownInput{
		SiteID:           payload.SiteID,
		SiteModelID:      payload.SiteModelID,
		SiteCredentialID: payload.SiteCredentialID,
		Scope:            payload.Scope,
		Source:           payload.Source,
		Reason:           payload.Reason,
		Duration:         duration,
		Metadata: map[string]any{
			"trigger": "admin_api",
		},
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "route_cooldown_create_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusCreated, "cooldown", routeCooldownPayload(item))
}

func (h Handler) ClearRouteCooldown(w http.ResponseWriter, r *http.Request) {
	if h.router == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "router_service_unavailable", "router service is not available")
		return
	}

	var payload routeCooldownClearRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if payload.SiteID == uuid.Nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_site_id", "site_id is required")
		return
	}

	if err := h.router.ClearCooldown(r.Context(), payload.SiteID, payload.SiteModelID, payload.SiteCredentialID, payload.Source); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "route_cooldown_clear_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	h.writePayload(w, http.StatusOK, map[string]any{"ok": true})
}

func (h Handler) ListRouteCandidates(w http.ResponseWriter, r *http.Request) {
	if h.router == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "router_service_unavailable", "router service is not available")
		return
	}

	modelKey := strings.TrimSpace(r.URL.Query().Get("model_key"))
	if modelKey == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_model_key", "model_key is required")
		return
	}

	debug := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("debug")), "true")
	result, err := h.router.Candidates(r.Context(), routeengine.CandidateQuery{
		ModelKey: modelKey,
		Debug:    debug,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "route_candidates_failed", err.Error())
		return
	}

	items := routeCandidatePayloads(result.Items, debug)

	h.writePayload(w, http.StatusOK, map[string]any{
		"canonical_model": canonicalModelPayload(result.CanonicalModel),
		"items":           items,
		"meta": map[string]any{
			"count": len(items),
			"debug": debug,
		},
	})
}

func (h Handler) SelectRoute(w http.ResponseWriter, r *http.Request) {
	if h.router == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "router_service_unavailable", "router service is not available")
		return
	}

	query, ok := h.decodeRouteSelectionRequest(w, r)
	if !ok {
		return
	}

	plan, err := h.router.Plan(r.Context(), query)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "route_select_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, routeSelectionPlanPayload(plan, query.Debug))
}

func (h Handler) FailoverRoute(w http.ResponseWriter, r *http.Request) {
	if h.router == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "router_service_unavailable", "router service is not available")
		return
	}

	var payload routeSelectionRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	query, err := payload.toCandidateQuery()
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_route_query", err.Error())
		return
	}
	query.ExcludeSiteModelIDs = append(query.ExcludeSiteModelIDs, payload.FailedSiteModelIDs...)
	query.ExcludeSiteIDs = append(query.ExcludeSiteIDs, payload.FailedSiteIDs...)

	plan, err := h.router.Plan(r.Context(), query)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "route_failover_failed", err.Error())
		return
	}

	response := routeSelectionPlanPayload(plan, query.Debug)
	response["meta"] = map[string]any{
		"debug":                   query.Debug,
		"excluded_site_ids":       uuidStrings(query.ExcludeSiteIDs),
		"excluded_site_model_ids": uuidStrings(query.ExcludeSiteModelIDs),
		"failover_count":          len(plan.Failover),
	}
	h.writePayload(w, http.StatusOK, response)
}

func (h Handler) ListRouteTraces(w http.ResponseWriter, r *http.Request) {
	if h.usage == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "request_log_service_unavailable", "request log service is not available")
		return
	}

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		if parsed > 100 {
			parsed = 100
		}
		limit = parsed
	}

	items, err := h.usage.ListRequests(r.Context(), usage.RequestQuery{
		Limit:    min(limit*8, 200),
		ModelKey: strings.TrimSpace(r.URL.Query().Get("model_key")),
	})
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "route_traces_failed", "failed to list route traces")
		return
	}

	traces := routeTracePayloads(items, limit)
	h.writeItems(w, http.StatusOK, traces, map[string]any{
		"count":     len(traces),
		"limit":     limit,
		"model_key": emptyStringAsNil(strings.TrimSpace(r.URL.Query().Get("model_key"))),
	})
}

type routeTrace struct {
	ParentRequestID string
	Attempts        []store.RequestLogDetail
	StartedAt       time.Time
	FinishedAt      time.Time
	Success         bool
	TotalLatencyMS  int64
}

func routeTracePayloads(items []store.RequestLogDetail, limit int) []map[string]any {
	byParent := map[string]*routeTrace{}
	for _, item := range items {
		metadata := requestLogMetadata(item.Metadata)
		parent, _ := metadata["parent_request_id"].(string)
		parent = strings.TrimSpace(parent)
		if parent == "" {
			parent = item.RequestID
		}

		trace, ok := byParent[parent]
		if !ok {
			trace = &routeTrace{
				ParentRequestID: parent,
				StartedAt:       item.CreatedAt,
				FinishedAt:      item.CreatedAt,
			}
			byParent[parent] = trace
		}
		trace.Attempts = append(trace.Attempts, item)
		if item.CreatedAt.Before(trace.StartedAt) {
			trace.StartedAt = item.CreatedAt
		}
		if item.CreatedAt.After(trace.FinishedAt) {
			trace.FinishedAt = item.CreatedAt
		}
		if item.Success {
			trace.Success = true
		}
		if item.LatencyMS.Valid {
			trace.TotalLatencyMS += item.LatencyMS.Int64
		}
	}

	traces := make([]*routeTrace, 0, len(byParent))
	for _, trace := range byParent {
		sort.SliceStable(trace.Attempts, func(i, j int) bool {
			return trace.Attempts[i].CreatedAt.Before(trace.Attempts[j].CreatedAt)
		})
		traces = append(traces, trace)
	}
	sort.SliceStable(traces, func(i, j int) bool {
		return traces[i].FinishedAt.After(traces[j].FinishedAt)
	})
	if limit > 0 && len(traces) > limit {
		traces = traces[:limit]
	}

	payloadItems := make([]map[string]any, 0, len(traces))
	for _, trace := range traces {
		attempts := make([]map[string]any, 0, len(trace.Attempts))
		for _, attempt := range trace.Attempts {
			attempts = append(attempts, routeTraceAttemptPayload(attempt))
		}
		payloadItems = append(payloadItems, map[string]any{
			"parent_request_id": trace.ParentRequestID,
			"started_at":        timeString(trace.StartedAt),
			"finished_at":       timeString(trace.FinishedAt),
			"success":           trace.Success,
			"attempt_count":     len(attempts),
			"failover_count":    max(len(attempts)-1, 0),
			"total_latency_ms":  trace.TotalLatencyMS,
			"attempts":          attempts,
		})
	}
	return payloadItems
}

func routeTraceAttemptPayload(item store.RequestLogDetail) map[string]any {
	metadata := requestLogMetadata(item.Metadata)
	payload := map[string]any{
		"id":                   item.ID.String(),
		"request_id":           item.RequestID,
		"attempt":              intFromAny(metadata["attempt"]),
		"credential_attempt":   intFromAny(metadata["credential_attempt"]),
		"credential_total":     intFromAny(metadata["credential_total"]),
		"status_code":          item.StatusCode,
		"upstream_status_code": metadata["upstream_status_code"],
		"success":              item.Success,
		"error_type":           nullStringValue(item.ErrorType),
		"latency_ms":           nullInt64Value(item.LatencyMS),
		"upstream_latency_ms":  nullInt64Value(item.UpstreamLatencyMS),
		"stream":               metadata["stream"],
		"response_mode":        metadata["response_mode"],
		"downstream_path":      valueOrFallback(metadata["downstream_path"], item.Endpoint),
		"upstream_url":         metadata["upstream_url"],
		"pricing_group":        metadata["pricing_group"],
		"api_key": map[string]any{
			"id":         nullUUIDValue(item.APIKeyID),
			"name":       nullStringValue(item.APIKeyName),
			"masked_key": nullStringValue(item.APIKeyMaskedKey),
		},
		"credential": map[string]any{
			"id":                       metadata["credential_id"],
			"name":                     metadata["credential_name"],
			"masked_secret":            metadata["credential_masked"],
			"routing_priority":         metadata["credential_routing_priority"],
			"group_name":               metadata["pricing_group"],
			"upstream_cost_multiplier": metadata["credential_upstream_cost_multiplier"],
		},
		"cost": metadata["cost_calculation"],
		"site": map[string]any{
			"id":        nullUUIDValue(item.SiteID),
			"name":      nullStringValue(item.SiteName),
			"slug":      nullStringValue(item.SiteSlug),
			"site_type": nullStringValue(item.SiteType),
		},
		"model": map[string]any{
			"canonical_model_id": nullUUIDValue(item.CanonicalModelID),
			"canonical_model":    nullStringValue(item.CanonicalModelKey),
			"site_model_id":      nullUUIDValue(item.SiteModelID),
			"upstream_model":     nullStringValue(item.SiteModelUpstreamName),
			"display_name":       nullStringValue(item.SiteModelDisplayName),
		},
		"created_at": timeString(item.CreatedAt),
	}
	if !item.Success {
		payload["failure_response"] = valueOrFallback(metadata["upstream_response"], metadata["error_response"])
	}
	return payload
}

func routeSelectionPlanPayload(plan routeengine.SelectionPlan, debug bool) map[string]any {
	return map[string]any{
		"canonical_model": canonicalModelPayload(plan.CanonicalModel),
		"selected":        routeCandidatePayload(plan.Selected, debug),
		"failover":        routeCandidatePayloads(plan.Failover, debug),
		"meta": map[string]any{
			"debug":          debug,
			"failover_count": len(plan.Failover),
		},
	}
}

func routeOverviewPayload(item store.RouteOverviewRow) map[string]any {
	return map[string]any{
		"canonical_model": map[string]any{
			"id":           item.CanonicalModelID.String(),
			"model_key":    item.ModelKey,
			"display_name": item.DisplayName,
			"status":       item.Status,
		},
		"candidate_summary": map[string]any{
			"site_model_count": item.SiteModelCount,
			"site_count":       item.SiteCount,
			"eligible_count":   item.EligibleCount,
			"cooldown_count":   item.CooldownCount,
		},
		"traffic_24h": map[string]any{
			"request_count":     item.RequestCount24h,
			"success_count":     item.SuccessCount24h,
			"success_rate":      nullFloat64Value(item.SuccessRate24h),
			"avg_latency_ms":    nullInt64Value(item.AvgLatencyMS24h),
			"prompt_tokens":     nullInt64Value(item.PromptTokens24h),
			"completion_tokens": nullInt64Value(item.CompletionTokens24h),
			"estimated_cost":    nullFloat64Value(item.EstimatedCost24h),
		},
		"last_route": map[string]any{
			"routed_at":   nullTimeValue(item.LastRoutedAt),
			"site_name":   nullStringValue(item.LastSiteName),
			"status_code": nullInt64Value(item.LastStatusCode),
			"success":     nullBoolValue(item.LastSuccess),
		},
	}
}

func routeCandidatePayloads(items []routeengine.Candidate, debug bool) []map[string]any {
	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, routeCandidatePayload(item, debug))
	}
	return payloadItems
}

func routeCandidatePayload(item routeengine.Candidate, debug bool) map[string]any {
	payload := map[string]any{
		"rank":  item.Rank,
		"score": item.Score,
		"site": map[string]any{
			"id":                               item.Site.ID.String(),
			"name":                             item.Site.Name,
			"slug":                             item.Site.Slug,
			"site_type":                        item.Site.SiteType,
			"base_url":                         item.Site.BaseURL,
			"routing_priority":                 item.Site.RoutingPriority,
			"supports_api_key_cost_multiplier": sitepkg.SupportsAPIKeyCostMultiplier(item.Site.SiteType),
		},
		"model": map[string]any{
			"site_model_id":              item.Model.SiteModelID.String(),
			"upstream_model_name":        item.Model.UpstreamName,
			"display_name":               item.Model.DisplayName,
			"canonical_match_source":     item.Model.MatchSource,
			"canonical_match_confidence": item.Model.MatchConfidence,
		},
		"health": map[string]any{
			"status":                item.Health.Status,
			"recent_success_rate":   pointerFloat64Value(item.Health.RecentSuccessRate),
			"recent_avg_latency_ms": pointerInt64Value(item.Health.RecentAvgLatencyMS),
			"consecutive_failures":  item.Health.ConsecutiveFailures,
			"model_success_rate":    pointerFloat64Value(item.Health.ModelSuccessRate),
			"model_avg_latency_ms":  pointerInt64Value(item.Health.ModelAvgLatencyMS),
			"model_request_count":   item.Health.ModelRequestCount,
		},
		"availability": map[string]any{
			"available_api_key_count": item.Availability.AvailableAPIKeys,
			"total_api_key_count":     item.Availability.TotalAPIKeys,
		},
		"credential": map[string]any{
			"id":                       pointerUUIDValue(item.Credential.ID),
			"name":                     item.Credential.Name,
			"routing_priority":         item.Credential.RoutingPriority,
			"group_name":               pointerStringValue(item.Credential.GroupName),
			"upstream_cost_multiplier": item.Credential.UpstreamCostMultiplier,
		},
		"pricing": map[string]any{
			"group_name":               pointerStringValue(item.Pricing.GroupName),
			"currency":                 pointerStringValue(item.Pricing.Currency),
			"base_input_value":         pointerFloat64Value(item.Pricing.BaseInputValue),
			"base_output_value":        pointerFloat64Value(item.Pricing.BaseOutputValue),
			"base_per_request_value":   pointerFloat64Value(item.Pricing.BasePerRequestValue),
			"input_value":              pointerFloat64Value(item.Pricing.InputValue),
			"output_value":             pointerFloat64Value(item.Pricing.OutputValue),
			"per_request_value":        pointerFloat64Value(item.Pricing.PerRequestValue),
			"upstream_cost_multiplier": item.Pricing.UpstreamCostMultiplier,
			"billing_type":             pointerStringValue(item.Pricing.BillingType),
			"quota_type":               pointerInt64Value(item.Pricing.QuotaType),
		},
	}
	if debug {
		payload["score_breakdown"] = item.ScoreBreakdown
	}
	return payload
}

func routeCooldownPayload(item store.RouteCooldown) map[string]any {
	return map[string]any{
		"id":                 item.ID.String(),
		"site_id":            item.SiteID.String(),
		"site_model_id":      nullUUIDValue(item.SiteModelID),
		"site_credential_id": nullUUIDValue(item.SiteCredentialID),
		"scope":              item.Scope,
		"source":             item.Source,
		"reason":             item.Reason,
		"active_until":       timeString(item.ActiveUntil),
		"remaining_seconds":  max(int(time.Until(item.ActiveUntil).Seconds()), 0),
		"cleared_at":         nullTimeValue(item.ClearedAt),
		"metadata":           jsonRaw(item.Metadata),
		"created_at":         timeString(item.CreatedAt),
		"updated_at":         timeString(item.UpdatedAt),
	}
}
