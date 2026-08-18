package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/newapi"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

func (h Handler) ListSites(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	deletedFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("deleted")))
	if deletedFilter == "" {
		deletedFilter = "exclude"
	}
	if deletedFilter != "exclude" && deletedFilter != "with_requests" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_deleted_filter", "deleted must be one of exclude, with_requests")
		return
	}

	var sites []store.Site
	var err error
	if deletedFilter == "with_requests" {
		sites, err = h.sites.ListWithDeletedRequestSites(r.Context())
	} else {
		sites, err = h.sites.List(r.Context())
	}
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_list_failed", "failed to list sites")
		return
	}

	items := make([]map[string]any, 0, len(sites))
	states, _ := h.sites.SiteStates(r.Context())
	oauthFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("oauth")))
	if oauthFilter == "" {
		oauthFilter = "all"
	}
	if oauthFilter != "all" && oauthFilter != "exclude" && oauthFilter != "only" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_oauth_filter", "oauth must be one of all, exclude, only")
		return
	}

	var usageBySite map[uuid.UUID]store.SiteUsageSummaryRow
	if h.usage != nil {
		if rows, err := h.usage.UsageSummaryBySite(r.Context(), nil); err == nil {
			usageBySite = make(map[uuid.UUID]store.SiteUsageSummaryRow, len(rows))
			for _, row := range rows {
				usageBySite[row.SiteID] = row
			}
		}
	}

	for _, item := range sites {
		isOAuthSite := sitepkg.CredentialTypeForSiteType(item.SiteType) == "oauth"
		if oauthFilter == "exclude" && isOAuthSite {
			continue
		}
		if oauthFilter == "only" && !isOAuthSite {
			continue
		}
		payload := h.sitePayloadWithState(item, states[item.ID])
		if usageBySite != nil {
			if row, ok := usageBySite[item.ID]; ok {
				payload["usage"] = siteUsagePayload(row)
			}
		}
		items = append(items, payload)
	}

	h.writeItems(w, http.StatusOK, items, map[string]any{"count": len(items)})
}

func (h Handler) CreateSite(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	var payload siteUpsertRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	baseURL := payload.BaseURL
	if strings.TrimSpace(baseURL) == "" {
		baseURL = gateway.OfficialBaseURLForProvider(payload.SiteType)
	}

	credentials, ok := h.resolveSiteCredentials(w, r, payload, true)
	if !ok {
		return
	}
	created, _, err := h.sites.Create(r.Context(), sitepkg.CreateSiteParams{
		Name:            payload.Name,
		Slug:            payload.Slug,
		SiteType:        payload.SiteType,
		BaseURL:         baseURL,
		Enabled:         enabled,
		RoutingPriority: payload.RoutingPriority,
		GatewayConfig:   payload.Gateway.toSiteGatewayConfig(),
		ProxyID:         payload.ProxyID,
		RequestHeaders:  requestHeadersFromInput(payload.RequestHeaders),
		Credentials:     credentials,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_create_failed", err.Error())
		return
	}

	h.logInfo("site created", "site_id", created.ID, "slug", created.Slug, "site_type", created.SiteType, "enabled", created.Enabled)
	response := h.siteSetupResponse(r, created)
	h.invalidateGatewayModelsCache()
	h.writePayload(w, http.StatusCreated, response)
}

func (h Handler) UpdateSite(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	var payload siteUpsertRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	baseURL := payload.BaseURL
	if strings.TrimSpace(baseURL) == "" {
		baseURL = gateway.OfficialBaseURLForProvider(payload.SiteType)
	}

	credentials, ok := h.resolveSiteCredentials(w, r, payload, false)
	if !ok {
		return
	}
	updated, _, err := h.sites.Update(r.Context(), sitepkg.UpdateSiteParams{
		ID:                siteID,
		Name:              payload.Name,
		Slug:              payload.Slug,
		SiteType:          payload.SiteType,
		BaseURL:           baseURL,
		Enabled:           enabled,
		RoutingPriority:   payload.RoutingPriority,
		GatewayConfig:     payload.Gateway.toSiteGatewayConfig(),
		ProxyID:           payload.ProxyID,
		RequestHeaders:    requestHeadersFromInput(payload.RequestHeaders),
		RequestHeadersSet: payload.RequestHeaders != nil,
		Credentials:       credentials,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_update_failed", err.Error())
		return
	}

	h.logInfo("site updated", "site_id", updated.ID, "slug", updated.Slug, "site_type", updated.SiteType, "enabled", updated.Enabled)
	if payload.SkipRefresh {
		h.invalidateGatewayModelsCache()
		h.writePayload(w, http.StatusOK, map[string]any{
			"site": h.sitePayloadWithStats(r, updated),
		})
		return
	}

	response := h.siteSetupResponse(r, updated)
	h.invalidateGatewayModelsCache()
	h.writePayload(w, http.StatusOK, response)
}

func (h Handler) DeleteSite(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	if err := h.sites.Delete(r.Context(), siteID); err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}

	h.logInfo("site deleted", "site_id", siteID)
	h.invalidateGatewayModelsCache()
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) UpdateSiteEnabled(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		Enabled *bool `json:"enabled"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if payload.Enabled == nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_enabled", "enabled is required")
		return
	}

	updated, err := h.sites.SetEnabled(r.Context(), siteID, *payload.Enabled)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_enabled_update_failed", err.Error())
		return
	}

	h.logInfo("site enabled state changed", "site_id", updated.ID, "enabled", updated.Enabled)
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "site", h.sitePayloadWithStats(r, updated))
}

func (h Handler) GetSite(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}

	payload := h.sitePayloadWithEditConfig(r, item)

	if h.usage != nil {
		if row, ok, err := h.usage.UsageSummaryForSite(r.Context(), siteID, nil); err == nil && ok {
			payload["usage"] = siteUsagePayload(row)
		}
	}

	h.writeResource(w, http.StatusOK, "site", payload)
}

func (h Handler) ValidateSite(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	if err := h.sites.Validate(r.Context(), siteID); err != nil {
		h.writePayload(w, http.StatusOK, map[string]any{
			"ok":      false,
			"message": err.Error(),
		})
		return
	}

	h.writePayload(w, http.StatusOK, map[string]any{"ok": true})
}

func (h Handler) CheckSiteHealth(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	result, err := h.sites.CheckSiteHealth(r.Context(), siteID, "manual")
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_health_check_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, siteHealthResultPayload(result))
}

func (h Handler) GetSiteHealth(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	result, err := h.sites.SiteHealth(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_health_get_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, siteHealthResultPayload(result))
}

func (h Handler) GetSiteHealthHistory(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return
		}
		if parsed > 200 {
			parsed = 200
		}
		limit = parsed
	}

	source := strings.TrimSpace(r.URL.Query().Get("source"))
	items, err := h.sites.SiteHealthHistory(r.Context(), siteID, limit, source)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_health_history_failed", err.Error())
		return
	}

	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, healthSnapshotPayload(item))
	}

	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{
		"count":  len(payloadItems),
		"limit":  limit,
		"source": emptyStringAsNil(source),
	})
}

func (h Handler) GetSiteHealthHourly(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	hours := 24
	if raw := strings.TrimSpace(r.URL.Query().Get("hours")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_hours", "hours must be a positive integer")
			return
		}
		if parsed > 168 {
			parsed = 168
		}
		hours = parsed
	}

	items, err := h.sites.SiteHealthHourly(r.Context(), siteID, hours)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_health_hourly_failed", err.Error())
		return
	}

	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, map[string]any{
			"bucket_start":  timeString(item.BucketStart),
			"status":        item.Status,
			"success_count": item.SuccessCount,
			"failure_count": item.FailureCount,
			"total_count":   item.TotalCount,
		})
	}

	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{
		"count": len(payloadItems),
		"hours": hours,
	})
}

func (h Handler) ListSiteHealth(w http.ResponseWriter, r *http.Request) {
	items, err := h.sites.List(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "sites_list_failed", "failed to list sites")
		return
	}

	states, err := h.sites.SiteHealthStates(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_health_list_failed", "failed to list site health states")
		return
	}

	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		state, ok := states[item.ID]
		if !ok {
			state = store.SiteHealthState{
				SiteID:   item.ID,
				Status:   "unknown",
				Metadata: []byte(`{}`),
			}
		}
		payloadItems = append(payloadItems, map[string]any{
			"site":   h.sitePayloadWithCapabilities(item),
			"health": siteHealthStatePayload(state),
		})
	}

	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{"count": len(payloadItems)})
}

func (h Handler) RefreshSite(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	result, err := h.sites.RefreshState(r.Context(), siteID)
	if result.Site.ID == uuid.Nil {
		if item, getErr := h.sites.Get(r.Context(), siteID); getErr == nil {
			result.Site = item
		}
	}
	payload := h.refreshResultPayload(r.Context(), result)
	if err != nil {
		payload["ok"] = false
		payload["message"] = err.Error()
		h.logWarn("site refresh failed", "site_id", siteID, "error", err)
		h.writePayload(w, http.StatusOK, payload)
		return
	}

	payload["ok"] = true
	h.logInfo("site refreshed", "site_id", siteID, "model_count", len(result.Models), "api_key_count", len(result.APIKeyStates))
	h.invalidateGatewayModelsCache()
	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) RefreshAllSites(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	results, err := h.sites.RefreshAllStates(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "sites_refresh_failed", "failed to refresh sites")
		return
	}

	items := make([]map[string]any, 0, len(results))
	successCount := 0
	failureCount := 0
	skippedCount := 0
	for _, item := range results {
		payload := h.refreshResultPayload(r.Context(), item.Result)
		if item.Skipped {
			payload["ok"] = true
			payload["skipped"] = true
			payload["message"] = item.SkipReason
			skippedCount++
		} else if item.Err != nil {
			payload["ok"] = false
			payload["message"] = item.Err.Error()
			failureCount++
		} else {
			payload["ok"] = true
			successCount++
		}
		items = append(items, payload)
	}

	h.logInfo("all sites refreshed", "count", len(items), "success_count", successCount, "failure_count", failureCount, "skipped_count", skippedCount)
	h.invalidateGatewayModelsCache()
	h.writePayload(w, http.StatusOK, map[string]any{
		"ok":    failureCount == 0,
		"items": items,
		"meta": map[string]any{
			"count":         len(items),
			"success_count": successCount,
			"failure_count": failureCount,
			"skipped_count": skippedCount,
		},
	})
}

func (h Handler) SyncSiteModels(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	result, err := h.sites.RefreshState(r.Context(), siteID)
	if err == nil {
		h.invalidateGatewayModelsCache()
	}

	h.writePayload(w, http.StatusOK, map[string]any{
		"ok": err == nil,
		"message": func() string {
			if err != nil {
				return err.Error()
			}
			return ""
		}(),
		"count": len(result.Models),
		"items": siteModelPayloads(result.Models),
	})
}

func (h Handler) ListSiteModels(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	models, err := h.sites.ListModels(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_models_list_failed", "failed to list site models")
		return
	}

	h.writeItems(w, http.StatusOK, siteModelPayloads(models), map[string]any{"count": len(models)})
}

func (h Handler) CreateSiteModel(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		UpstreamModelName      string   `json:"upstream_model_name"`
		CanonicalModelID       string   `json:"canonical_model_id"`
		SupportedEndpointTypes []string `json:"supported_endpoint_types"`
		SiteCredentialIDs      []string `json:"site_credential_ids"`
		Enabled                *bool    `json:"enabled"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	canonicalID, err := uuid.Parse(strings.TrimSpace(payload.CanonicalModelID))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_canonical_model_id", "canonical_model_id must be a valid UUID")
		return
	}
	credentialIDs, ok := parseUUIDList(w, r, payload.SiteCredentialIDs, "invalid_site_credential_id", "site_credential_id")
	if !ok {
		return
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}
	model, err := h.sites.CreateManualSiteModel(r.Context(), sitepkg.CreateManualSiteModelParams{
		SiteID:                 siteID,
		UpstreamModelName:      payload.UpstreamModelName,
		CanonicalModelID:       canonicalID,
		SupportedEndpointTypes: payload.SupportedEndpointTypes,
		SiteCredentialIDs:      credentialIDs,
		Enabled:                enabled,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_model_create_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusCreated, "model", siteModelPayload(model))
}

func (h Handler) DeleteSiteModel(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}

	if err := h.sites.DeleteManualSiteModel(r.Context(), siteID, modelID); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_model_delete_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) UpdateSiteModel(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		Enabled *bool `json:"enabled"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if payload.Enabled == nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_enabled", "enabled is required")
		return
	}

	model, err := h.sites.SetModelEnabled(r.Context(), siteID, modelID, *payload.Enabled)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_model_update_failed", err.Error())
		return
	}

	h.writeResource(w, http.StatusOK, "model", siteModelPayload(model))
	h.invalidateGatewayModelsCache()
}

func (h Handler) UpdateSiteModelsStatus(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		ModelIDs []string `json:"model_ids"`
		Enabled  *bool    `json:"enabled"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if payload.Enabled == nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_enabled", "enabled is required")
		return
	}
	modelIDs, ok := parseUUIDList(w, r, payload.ModelIDs, "invalid_model_id", "model_id")
	if !ok {
		return
	}
	if len(modelIDs) == 0 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_model_ids", "model_ids is required")
		return
	}

	models, err := h.sites.SetModelsEnabled(r.Context(), siteID, modelIDs, *payload.Enabled)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_models_update_failed", err.Error())
		return
	}

	h.writeItems(w, http.StatusOK, siteModelPayloads(models), map[string]any{"count": len(models)})
	h.invalidateGatewayModelsCache()
}

func (h Handler) TestSiteModel(w http.ResponseWriter, r *http.Request) {
	if h.gateway == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "gateway_unavailable", "gateway service is not available")
		return
	}

	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		Prompt           string `json:"prompt"`
		TimeoutMS        int    `json:"timeout_ms"`
		Protocol         string `json:"protocol"`
		Stream           *bool  `json:"stream"`
		SiteCredentialID string `json:"site_credential_id"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_request_body", "failed to read request body")
		return
	}
	if strings.TrimSpace(string(body)) != "" {
		if err := json.Unmarshal(body, &payload); err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
			return
		}
	}
	var siteCredentialID uuid.UUID
	if raw := strings.TrimSpace(payload.SiteCredentialID); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_site_credential_id", "site_credential_id must be a UUID")
			return
		}
		siteCredentialID = parsed
	}

	timeout := time.Duration(payload.TimeoutMS) * time.Millisecond
	result, err := h.gateway.TestSiteModel(r.Context(), gateway.SiteModelTestInput{
		SiteID:           siteID,
		SiteModelID:      modelID,
		SiteCredentialID: siteCredentialID,
		Prompt:           payload.Prompt,
		Timeout:          timeout,
		Protocol:         payload.Protocol,
		Stream:           payload.Stream,
	})
	if err != nil {
		var testErr *gateway.SiteModelTestError
		if errors.As(err, &testErr) {
			h.writeError(w, r, testErr.StatusCode, testErr.Code, testErr.Message)
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "site_model_test_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, result)
}

func (h Handler) ListSiteAPIKeys(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}

	apiKeys, err := h.sites.APIKeys(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_credentials_list_failed", err.Error())
		return
	}

	apiKeyStates, _ := h.sites.APIKeyStates(r.Context(), siteID)
	statesByCredentialID := map[uuid.UUID]store.SiteAPIKeyState{}
	for _, state := range apiKeyStates {
		statesByCredentialID[state.SiteCredentialID] = state
	}

	items := make([]map[string]any, 0, len(apiKeys))
	for index, apiKey := range apiKeys {
		models, _ := h.sites.APIKeyModels(r.Context(), apiKey.Credential.ID)
		items = append(items, h.siteAPIKeyPayloadFromState(item, apiKey, statesByCredentialID[apiKey.Credential.ID], models, siteAPIKeyDefaultName(index)))
	}

	h.writeItems(w, http.StatusOK, items, map[string]any{"count": len(items)})
}

// RevealSiteAPIKey discloses the decrypted upstream secret for a single site key.
// List/detail responses only carry the masked secret; this explicit, per-key,
// audited action is the only path that returns plaintext.
func (h Handler) RevealSiteAPIKey(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	apiKeyID, ok := apiKeyIDParam(w, r)
	if !ok {
		return
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}
	apiKey, err := h.sites.APIKeyByCredentialID(r.Context(), siteID, apiKeyID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_api_key_not_found", "api key was not found")
		return
	}

	copyKey := siteAPIKeyCopySecret(item.SiteType, apiKey)
	if copyKey == "" {
		h.writeError(w, r, http.StatusBadRequest, "site_api_key_reveal_failed", "api key secret is not available")
		return
	}

	actor, _ := auth.AdminActorFromContext(r.Context())
	h.recordAudit(r, actor, "site_api_key.reveal", "site_api_key", apiKeyID.String(), true, "", map[string]any{"site_id": siteID.String()})
	h.writePayload(w, http.StatusOK, map[string]any{"id": apiKeyID.String(), "copy_key": copyKey})
}

func (h Handler) CreateSiteAPIKey(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		APIKey                 string   `json:"api_key"`
		Secret                 string   `json:"secret"`
		Name                   string   `json:"name"`
		RoutingPriority        *float64 `json:"routing_priority"`
		UpstreamCostMultiplier *float64 `json:"upstream_cost_multiplier"`
		CacheDomain            string   `json:"cache_domain"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	secret := strings.TrimSpace(payload.APIKey)
	if secret == "" {
		secret = strings.TrimSpace(payload.Secret)
	}
	if secret == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_api_key", "api_key is required")
		return
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}

	apiKey, err := h.sites.CreateAPIKey(r.Context(), siteID, sitepkg.CreateAPIKeyInput{
		APIKey:                 secret,
		DisplayName:            payload.Name,
		RoutingPriority:        payload.RoutingPriority,
		UpstreamCostMultiplier: payload.UpstreamCostMultiplier,
		CacheDomain:            payload.CacheDomain,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_api_key_create_failed", err.Error())
		return
	}

	// Fetch and bind the new key's models immediately so it is routable (and its
	// models are testable) without waiting for a manual or scheduled refresh.
	// Best-effort: a refresh failure must not fail key creation.
	if _, refreshErr := h.sites.RefreshSingleAPIKey(r.Context(), siteID, apiKey.Credential.ID); refreshErr != nil {
		h.logWarn("site api key initial refresh failed", "site_id", siteID, "credential_id", apiKey.Credential.ID, "error", refreshErr)
	} else if refreshed, err := h.sites.APIKeyByCredentialID(r.Context(), siteID, apiKey.Credential.ID); err == nil {
		apiKey = refreshed
	}

	apiKeyStates, _ := h.sites.APIKeyStates(r.Context(), siteID)
	statesByCredentialID := map[uuid.UUID]store.SiteAPIKeyState{}
	for _, state := range apiKeyStates {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	models, _ := h.sites.APIKeyModels(r.Context(), apiKey.Credential.ID)

	h.invalidateGatewayModelsCache()
	h.recordAudit(r, currentAdminActor(r), "site_api_key.create", "site_api_key", apiKey.Credential.ID.String(), true, "", map[string]any{
		"site_id":                  siteID.String(),
		"display_name":             apiKey.DisplayName,
		"routing_priority":         apiKey.RoutingPriority,
		"upstream_cost_multiplier": apiKey.UpstreamCostMultiplier,
		"cache_domain":             apiKey.CacheDomain,
		"enabled":                  apiKey.Enabled,
	})
	h.writeResource(w, http.StatusCreated, "api_key", h.siteAPIKeyPayloadFromState(item, apiKey, statesByCredentialID[apiKey.Credential.ID], models, h.siteAPIKeyDefaultName(r.Context(), siteID, apiKey.Credential.ID)))
}

func (h Handler) DeleteSiteAPIKey(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	apiKeyID, ok := apiKeyIDParam(w, r)
	if !ok {
		return
	}

	existing, _ := h.sites.APIKeyByCredentialID(r.Context(), siteID, apiKeyID)
	if err := h.sites.DeleteAPIKey(r.Context(), siteID, apiKeyID); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_api_key_delete_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	h.recordAudit(r, currentAdminActor(r), "site_api_key.delete", "site_api_key", apiKeyID.String(), true, "", map[string]any{
		"site_id":                  siteID.String(),
		"display_name":             existing.DisplayName,
		"routing_priority":         existing.RoutingPriority,
		"upstream_cost_multiplier": existing.UpstreamCostMultiplier,
		"enabled":                  existing.Enabled,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) ListSitePricing(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}

	groups, err := h.sites.SitePricingGroups(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_pricing_groups_list_failed", "failed to list site pricing groups")
		return
	}

	pricings, err := h.sites.SiteModelPricings(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_model_pricings_list_failed", "failed to list site model pricings")
		return
	}
	credentialPricings, err := h.sites.SiteCredentialModelPricings(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_credential_pricings_list_failed", "failed to list site credential pricings")
		return
	}
	credentialPricingsByModelID := credentialModelPricingsByModelID(credentialPricings)

	h.writePayload(w, http.StatusOK, map[string]any{
		"site":   h.sitePayloadWithStats(r, item),
		"groups": sitePricingGroupPayloads(groups),
		"items":  siteModelPricingPayloads(pricings, item, credentialPricingsByModelID),
		"meta": map[string]any{
			"group_count": len(groups),
			"count":       len(pricings),
		},
	})
}

func (h Handler) ListAllSitePricings(w http.ResponseWriter, r *http.Request) {
	items, err := h.sites.AllSiteModelPricings(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_model_pricings_list_failed", "failed to list site model pricings")
		return
	}

	sites, err := h.sites.List(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_list_failed", "failed to list sites")
		return
	}
	credentialPricings, err := h.sites.AllSiteCredentialModelPricings(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_credential_pricings_list_failed", "failed to list site credential pricings")
		return
	}
	credentialPricingsByModelID := credentialModelPricingsByModelID(credentialPricings)

	sitesByID := make(map[uuid.UUID]store.Site, len(sites))
	for _, site := range sites {
		sitesByID[site.ID] = site
	}

	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, siteModelPricingPayload(item, sitesByID[item.SiteID], credentialPricingsByModelID[item.SiteModelID.UUID]))
	}

	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{"count": len(payloadItems)})
}

func (h Handler) UpdateSiteAPIKey(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	apiKeyID, ok := apiKeyIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		Enabled                *bool    `json:"enabled"`
		Name                   *string  `json:"name"`
		RoutingPriority        *float64 `json:"routing_priority"`
		UpstreamCostMultiplier *float64 `json:"upstream_cost_multiplier"`
		CacheDomain            *string  `json:"cache_domain"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}
	existing, _ := h.sites.APIKeyByCredentialID(r.Context(), siteID, apiKeyID)

	apiKey, err := h.sites.UpdateAPIKey(r.Context(), siteID, apiKeyID, sitepkg.UpdateAPIKeyInput{
		Enabled:                payload.Enabled,
		DisplayName:            payload.Name,
		RoutingPriority:        payload.RoutingPriority,
		UpstreamCostMultiplier: payload.UpstreamCostMultiplier,
		CacheDomain:            payload.CacheDomain,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_api_key_update_failed", err.Error())
		return
	}

	apiKeyStates, _ := h.sites.APIKeyStates(r.Context(), siteID)
	statesByCredentialID := map[uuid.UUID]store.SiteAPIKeyState{}
	for _, state := range apiKeyStates {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	models, _ := h.sites.APIKeyModels(r.Context(), apiKey.Credential.ID)

	h.invalidateGatewayModelsCache()
	h.recordAudit(r, currentAdminActor(r), "site_api_key.update", "site_api_key", apiKeyID.String(), true, "", map[string]any{
		"site_id": siteID.String(),
		"old": map[string]any{
			"display_name":             existing.DisplayName,
			"routing_priority":         existing.RoutingPriority,
			"upstream_cost_multiplier": existing.UpstreamCostMultiplier,
			"cache_domain":             existing.CacheDomain,
			"enabled":                  existing.Enabled,
		},
		"new": map[string]any{
			"display_name":             apiKey.DisplayName,
			"routing_priority":         apiKey.RoutingPriority,
			"upstream_cost_multiplier": apiKey.UpstreamCostMultiplier,
			"cache_domain":             apiKey.CacheDomain,
			"enabled":                  apiKey.Enabled,
		},
	})
	h.writeResource(w, http.StatusOK, "api_key", h.siteAPIKeyPayloadFromState(item, apiKey, statesByCredentialID[apiKey.Credential.ID], models, h.siteAPIKeyDefaultName(r.Context(), siteID, apiKey.Credential.ID)))
}

func (h Handler) RefreshSiteAPIKey(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	apiKeyID, ok := apiKeyIDParam(w, r)
	if !ok {
		return
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}

	result, err := h.sites.RefreshSingleAPIKey(r.Context(), siteID, apiKeyID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_api_key_refresh_failed", err.Error())
		return
	}

	apiKey, err := h.sites.APIKeyByCredentialID(r.Context(), siteID, apiKeyID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "site_api_key_not_found", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "api_key", h.siteAPIKeyPayloadFromState(item, apiKey, result.State, result.Models, h.siteAPIKeyDefaultName(r.Context(), siteID, apiKeyID)))
}

func (h Handler) UpdateSiteAPIKeySecret(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	apiKeyID, ok := apiKeyIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		APIKey string `json:"api_key"`
		Secret string `json:"secret"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	secret := strings.TrimSpace(payload.APIKey)
	if secret == "" {
		secret = strings.TrimSpace(payload.Secret)
	}
	if secret == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_api_key", "api_key is required")
		return
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}

	apiKey, err := h.sites.SetAPIKeySecret(r.Context(), siteID, apiKeyID, secret)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_api_key_secret_update_failed", err.Error())
		return
	}

	apiKeyStates, _ := h.sites.APIKeyStates(r.Context(), siteID)
	statesByCredentialID := map[uuid.UUID]store.SiteAPIKeyState{}
	for _, state := range apiKeyStates {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	models, _ := h.sites.APIKeyModels(r.Context(), apiKey.Credential.ID)

	h.invalidateGatewayModelsCache()
	h.recordAudit(r, currentAdminActor(r), "site_api_key.secret_update", "site_api_key", apiKeyID.String(), true, "", map[string]any{
		"site_id": siteID.String(),
	})
	h.writeResource(w, http.StatusOK, "api_key", h.siteAPIKeyPayloadFromState(item, apiKey, statesByCredentialID[apiKey.Credential.ID], models, h.siteAPIKeyDefaultName(r.Context(), siteID, apiKey.Credential.ID)))
}

func (h Handler) UpdateSiteAPIKeyModel(w http.ResponseWriter, r *http.Request) {
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	apiKeyID, ok := apiKeyIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		Model   string `json:"model"`
		Enabled bool   `json:"enabled"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}

	apiKey, err := h.sites.SetAPIKeyModelEnabled(r.Context(), siteID, apiKeyID, payload.Model, payload.Enabled)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_api_key_model_update_failed", err.Error())
		return
	}

	apiKeyStates, _ := h.sites.APIKeyStates(r.Context(), siteID)
	statesByCredentialID := map[uuid.UUID]store.SiteAPIKeyState{}
	for _, state := range apiKeyStates {
		statesByCredentialID[state.SiteCredentialID] = state
	}
	models, _ := h.sites.APIKeyModels(r.Context(), apiKey.Credential.ID)

	h.invalidateGatewayModelsCache()
	h.recordAudit(r, currentAdminActor(r), "site_api_key.model_update", "site_api_key", apiKeyID.String(), true, "", map[string]any{
		"site_id": siteID.String(),
		"model":   strings.TrimSpace(payload.Model),
		"enabled": payload.Enabled,
	})
	h.writeResource(w, http.StatusOK, "api_key", h.siteAPIKeyPayloadFromState(item, apiKey, statesByCredentialID[apiKey.Credential.ID], models, h.siteAPIKeyDefaultName(r.Context(), siteID, apiKey.Credential.ID)))
}

func (h Handler) siteSetupResponse(r *http.Request, item store.Site) map[string]any {
	responseSite := item
	response := map[string]any{
		"ok":   true,
		"site": h.sitePayloadWithCapabilities(responseSite),
	}

	refreshResult, refreshErr := h.sites.RefreshState(r.Context(), item.ID)
	if refreshResult.Site.ID != uuid.Nil {
		responseSite = refreshResult.Site
		response["site"] = h.sitePayloadWithCapabilities(responseSite)
	}
	if refreshResult.State.SiteID != uuid.Nil {
		response["site"] = h.sitePayloadWithState(responseSite, refreshResult.State)
	}
	if refreshErr != nil {
		response["ok"] = false
		response["message"] = refreshErr.Error()
		response["validation"] = map[string]any{"ok": false, "message": refreshErr.Error()}
		response["model_sync"] = map[string]any{"ok": false, "message": refreshErr.Error()}
	} else {
		response["validation"] = map[string]any{"ok": true}
		response["model_sync"] = map[string]any{
			"ok":    true,
			"count": len(refreshResult.Models),
			"items": siteModelPayloads(refreshResult.Models),
		}
	}

	if isNewAPISiteType(item.SiteType) {
		newAPIResponse := map[string]any{}

		newAPIResponse["api_keys_summary"] = map[string]any{
			"ok":    refreshErr == nil,
			"count": len(refreshResult.APIKeyStates),
		}

		if refreshResult.State.SiteID != uuid.Nil {
			newAPIResponse["user_summary"] = map[string]any{
				"ok":   refreshErr == nil,
				"data": jsonRaw(refreshResult.State.UserSummary),
			}
		}

		response["newapi"] = newAPIResponse
	}

	return response
}

func isNewAPISiteType(siteType string) bool {
	return sitepkg.CredentialTypeForSiteType(siteType) == "system_token"
}

func isOpenAICompatibleSiteType(siteType string) bool {
	return sitepkg.CredentialTypeForSiteType(siteType) == "api_key"
}

func (h Handler) resolveSiteCredentials(w http.ResponseWriter, r *http.Request, payload siteUpsertRequest, required bool) ([]sitepkg.CredentialInput, bool) {
	credType := sitepkg.CredentialTypeForSiteType(payload.SiteType)

	switch credType {
	case "grok_sso":
		return nil, true
	case "system_token":
		if payload.NewAPI == nil || strings.TrimSpace(payload.NewAPI.AccessToken) == "" || payload.NewAPI.UserID <= 0 {
			h.writeError(w, r, http.StatusBadRequest, "invalid_newapi_auth", "newapi.access_token and newapi.user_id are required for a newapi site")
			return nil, false
		}

		return []sitepkg.CredentialInput{{
			Type:   "newapi_access_token",
			Secret: payload.NewAPI.AccessToken,
			Meta: map[string]any{
				"user_id": payload.NewAPI.UserID,
			},
		}}, true
	case "api_key":
		if len(payload.APIKeys) > 0 {
			credentials := make([]sitepkg.CredentialInput, 0, len(payload.APIKeys))
			for index, item := range payload.APIKeys {
				apiKey := strings.TrimSpace(item.APIKey)
				if apiKey == "" {
					h.writeError(w, r, http.StatusBadRequest, "invalid_api_key", fmt.Sprintf("api_keys[%d].api_key is required", index))
					return nil, false
				}
				displayName := item.Name
				credentials = append(credentials, sitepkg.CredentialInput{
					Type:                   "api_key",
					Secret:                 apiKey,
					Meta:                   map[string]any{"enabled": true},
					DisplayName:            &displayName,
					RoutingPriority:        item.RoutingPriority,
					UpstreamCostMultiplier: item.UpstreamCostMultiplier,
				})
			}
			return credentials, true
		}
		apiKey := strings.TrimSpace(payload.APIKey)
		if apiKey == "" {
			if required {
				h.writeError(w, r, http.StatusBadRequest, "invalid_api_key", "api_key is required")
				return nil, false
			}
			return nil, true
		}
		return []sitepkg.CredentialInput{{Type: "api_key", Secret: apiKey}}, true
	case "xlyra":
		if payload.XLyra == nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_xlyra_auth", "xlyra auth_config is required")
			return nil, false
		}
		authMode := strings.TrimSpace(payload.XLyra.AuthMode)
		switch authMode {
		case "access_token":
			accessToken := strings.TrimSpace(payload.XLyra.AccessToken)
			if accessToken == "" {
				h.writeError(w, r, http.StatusBadRequest, "invalid_xlyra_auth", "xlyra.access_token is required")
				return nil, false
			}
			return []sitepkg.CredentialInput{{
				Type:   "xlyra_access_token",
				Secret: accessToken,
				Meta:   map[string]any{"auth_mode": authMode},
			}}, true
		case "api_key":
			apiKey := strings.TrimSpace(payload.XLyra.APIKey)
			if apiKey == "" {
				apiKey = strings.TrimSpace(payload.APIKey)
			}
			if apiKey == "" {
				if !required {
					return nil, true
				}
				h.writeError(w, r, http.StatusBadRequest, "invalid_xlyra_auth", "xlyra.api_key is required")
				return nil, false
			}
			return []sitepkg.CredentialInput{{
				Type:   "api_key",
				Secret: apiKey,
				Meta:   map[string]any{"auth_mode": authMode},
			}}, true
		default:
			h.writeError(w, r, http.StatusBadRequest, "invalid_xlyra_auth", "xlyra.auth_mode must be access_token or api_key")
			return nil, false
		}
	case "oauth":
		return nil, true
	default:
		return nil, true
	}
}

func normalizedRequestSiteType(siteType string) string {
	siteType = strings.TrimSpace(siteType)
	if siteType == "" {
		return "openai"
	}

	return siteType
}

func (h Handler) persistFreshAPIKeyMeta(r *http.Request, siteID uuid.UUID, apiKey sitepkg.APIKeyCredential, freshKey newapi.UserAPIKey) sitepkg.APIKeyCredential {
	patch := freshAPIKeyMetaPatch(apiKey.Meta, freshKey)
	if len(patch) == 0 {
		return apiKey
	}

	updated, err := h.sites.PatchAPIKeyMeta(r.Context(), siteID, apiKey.Credential.ID, patch)
	if err != nil {
		if h.logger != nil {
			h.logWarn("failed to persist fresh api key metadata", "site_id", siteID, "api_key_id", apiKey.Credential.ID, "error", err)
		}
		return apiKey
	}

	return updated
}

func freshAPIKeyMetaPatch(current map[string]any, freshKey newapi.UserAPIKey) map[string]any {
	if freshKey.ID <= 0 && len(freshKey.Raw) == 0 {
		return nil
	}

	patch := map[string]any{}
	setIfChanged := func(key string, value any) {
		if !metaValuesEqual(current[key], value) {
			patch[key] = value
		}
	}

	if freshKey.ID > 0 {
		setIfChanged("upstream_id", freshKey.ID)
	}
	if strings.TrimSpace(freshKey.Name) != "" {
		setIfChanged("name", strings.TrimSpace(freshKey.Name))
	}
	if freshKey.Status != nil {
		setIfChanged("status", freshKey.Status)
	}
	for _, key := range []string{
		"remain_quota",
		"used_quota",
		"unlimited_quota",
		"model_limits_enabled",
		"model_limits",
		"expired_time",
		"group",
	} {
		if value, ok := freshKey.Raw[key]; ok {
			setIfChanged(key, value)
		}
	}

	return patch
}

func metaValuesEqual(left any, right any) bool {
	leftNumber, leftOK := numberAsFloat(left)
	rightNumber, rightOK := numberAsFloat(right)
	if leftOK && rightOK {
		return leftNumber == rightNumber
	}

	return reflect.DeepEqual(left, right)
}

func numberAsFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		parsed, err := strconv.ParseFloat(v.String(), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func (h Handler) freshNewAPIKeyByID(r *http.Request, item store.Site, siteID uuid.UUID, upstreamID int) newapi.UserAPIKey {
	if upstreamID <= 0 || !isNewAPISiteType(item.SiteType) || h.newAPI == nil {
		return newapi.UserAPIKey{}
	}
	auth, err := h.sites.SystemAuth(r.Context(), siteID)
	if err != nil {
		return newapi.UserAPIKey{}
	}
	freshKeys, err := h.newAPI.UserAPIKeys(r.Context(), item.BaseURL, auth.AccessToken, auth.UserID)
	if err != nil {
		return newapi.UserAPIKey{}
	}
	for _, freshKey := range freshKeys {
		if freshKey.ID == upstreamID {
			return freshKey
		}
	}

	return newapi.UserAPIKey{}
}

func (h Handler) siteAPIKeyPayload(r *http.Request, item store.Site, apiKey sitepkg.APIKeyCredential, freshKey newapi.UserAPIKey) map[string]any {
	upstreamName := apiKey.Name
	keyItem := map[string]any{
		"id":                       apiKey.Credential.ID.String(),
		"upstream_id":              apiKey.UpstreamID,
		"name":                     siteAPIKeyEffectiveName(apiKey.DisplayName, upstreamName, apiKey.MaskedSecret),
		"display_name":             apiKey.DisplayName,
		"upstream_name":            upstreamName,
		"routing_priority":         apiKey.RoutingPriority,
		"upstream_cost_multiplier": apiKey.UpstreamCostMultiplier,
		"cache_domain":             apiKey.CacheDomain,
		"key":                      apiKey.MaskedSecret,
		"status":                   apiKey.Meta["status"],
		"enabled":                  apiKey.Enabled,
		"models":                   []string{},
		"model_items":              []map[string]any{},
	}
	if probe, ok := apiKey.Meta[sitepkg.QuotaProbeCredentialMetaKey]; ok {
		keyItem["quota_probe"] = probe
	}
	if freshKey.Name != "" {
		keyItem["upstream_name"] = freshKey.Name
		if strings.TrimSpace(apiKey.DisplayName) == "" {
			keyItem["name"] = freshKey.Name
		}
	}
	if freshKey.Status != nil {
		keyItem["status"] = freshKey.Status
	}
	if keyItem["status"] == nil {
		keyItem["status"] = "active"
	}
	keyName, _ := keyItem["name"].(string)
	if legacyDefaultAPIKeyName(keyName) {
		keyItem["name"] = siteAPIKeyDefaultName(0)
	}

	if isNewAPISiteType(item.SiteType) && h.newAPI != nil {
		if summary, err := h.newAPI.APIKeySummary(r.Context(), item.BaseURL, apiKey.Secret); err == nil {
			keyItem["usage"] = summary.Usage
			if !usageHasQuotaData(summary.Usage) {
				keyItem["usage"] = usageFromCredentialMeta(apiKey.Meta, freshKey.Raw)
			}
			modelIDs := apiKeySummaryModelIDs(summary.Models)
			keyItem["models"] = modelIDs
			keyItem["model_items"] = apiKeyModelItems(modelIDs, apiKey.Meta)
		} else {
			keyItem["message"] = err.Error()
			keyItem["usage"] = usageFromCredentialMeta(apiKey.Meta, freshKey.Raw)
		}
	} else if models, err := h.sites.ListModels(r.Context(), item.ID); err == nil {
		modelNames := siteModelNames(models)
		keyItem["models"] = modelNames
		keyItem["model_items"] = apiKeyModelItems(modelNames, apiKey.Meta)
	}

	return keyItem
}

func (h Handler) siteAPIKeyPayloadFromState(item store.Site, apiKey sitepkg.APIKeyCredential, state store.SiteAPIKeyState, models []store.SiteAPIKeyModel, fallbackName string) map[string]any {
	hasState := state.SiteCredentialID != uuid.Nil
	upstreamID := apiKey.UpstreamID
	if hasState && state.UpstreamID.Valid {
		upstreamID = int(state.UpstreamID.Int64)
	}

	upstreamName := apiKey.Name
	if hasState && strings.TrimSpace(state.Name) != "" {
		upstreamName = state.Name
	}
	name := siteAPIKeyEffectiveName(apiKey.DisplayName, upstreamName, fallbackName, apiKey.MaskedSecret)
	if legacyDefaultAPIKeyName(name) {
		name = fallbackName
	}

	enabled := apiKey.Enabled
	if hasState {
		enabled = state.Enabled
	}

	status := apiKey.Meta["status"]
	if hasState && len(state.UpstreamStatus) > 0 {
		if parsed := jsonRaw(state.UpstreamStatus); parsed != nil {
			status = parsed
		}
	}
	if status == nil {
		status = "active"
	}
	if hasState && strings.EqualFold(strings.TrimSpace(state.SyncStatus), "stale") {
		status = "stale"
	}

	modelNames := make([]string, 0, len(models))
	modelItems := make([]map[string]any, 0, len(models))
	for _, model := range models {
		if !model.Available {
			continue
		}
		modelNames = append(modelNames, model.UpstreamModelName)
		modelItems = append(modelItems, map[string]any{
			"name":    model.UpstreamModelName,
			"enabled": model.Enabled,
		})
	}
	var usage any = usageFromCredentialMeta(apiKey.Meta, nil)
	if hasState && len(state.Usage) > 0 {
		usage = jsonRaw(state.Usage)
	}

	keyItem := map[string]any{
		"id":                       apiKey.Credential.ID.String(),
		"upstream_id":              upstreamID,
		"name":                     name,
		"display_name":             apiKey.DisplayName,
		"upstream_name":            upstreamName,
		"routing_priority":         apiKey.RoutingPriority,
		"upstream_cost_multiplier": apiKey.UpstreamCostMultiplier,
		"cache_domain":             apiKey.CacheDomain,
		"key":                      apiKey.MaskedSecret,
		"status":                   status,
		"enabled":                  enabled,
		"secret_missing":           apiKey.SecretMissing,
		"can_complete":             apiKey.SecretMissing,
		"usage":                    usage,
		"models":                   modelNames,
		"model_items":              modelItems,
	}
	if hasState && strings.TrimSpace(state.SyncStatus) != "" {
		keyItem["sync_status"] = state.SyncStatus
	}
	if hasState && state.GroupName.Valid && strings.TrimSpace(state.GroupName.String) != "" {
		keyItem["group"] = state.GroupName.String
	} else if group, ok := apiKey.Meta["group"].(string); ok && strings.TrimSpace(group) != "" {
		keyItem["group"] = group
	}
	if hasState && state.SyncMessage.Valid {
		keyItem["message"] = state.SyncMessage.String
	}
	if probe, ok := apiKey.Meta[sitepkg.QuotaProbeCredentialMetaKey]; ok {
		keyItem["quota_probe"] = probe
	}

	return keyItem
}

func siteAPIKeyEffectiveName(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (h Handler) siteAPIKeyDefaultName(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID) string {
	apiKeys, err := h.sites.APIKeys(ctx, siteID)
	if err != nil {
		return siteAPIKeyDefaultName(0)
	}
	for index, apiKey := range apiKeys {
		if apiKey.Credential.ID == credentialID {
			return siteAPIKeyDefaultName(index)
		}
	}
	return siteAPIKeyDefaultName(0)
}

func siteAPIKeyDefaultName(index int) string {
	if index < 0 {
		index = 0
	}
	return "Apikey " + strconv.Itoa(index+1)
}

func legacyDefaultAPIKeyName(name string) bool {
	switch strings.TrimSpace(name) {
	case "", "默认 Key", "Default Key", "デフォルトキー":
		return true
	default:
		return false
	}
}

func siteAPIKeyCopySecret(_ string, apiKey sitepkg.APIKeyCredential) string {
	if strings.TrimSpace(apiKey.Secret) != "" {
		return apiKey.Secret
	}
	return ""
}

func sitePayload(item store.Site) map[string]any {
	meta := jsonRaw(item.Meta)
	payload := map[string]any{
		"id":               item.ID.String(),
		"name":             item.Name,
		"slug":             item.Slug,
		"site_type":        item.SiteType,
		"icon_url":         siteTypeIconURL(item.SiteType),
		"base_url":         item.BaseURL,
		"status":           item.Status,
		"enabled":          item.Enabled,
		"routing_priority": item.RoutingPriority,
		"gateway_config":   sitepkg.GatewayConfigFromSiteMeta(item.Meta),
		"meta":             meta,
		"created_at":       timeString(item.CreatedAt),
		"updated_at":       timeString(item.UpdatedAt),
	}

	if metaMap, ok := meta.(map[string]any); ok {
		if v, ok := metaMap["proxy_id"].(string); ok {
			payload["proxy_id"] = strings.TrimSpace(v)
		}
		if v, ok := metaMap["request_headers"]; ok {
			payload["request_headers"] = v
		}
		if account := oauthAccountPayload(metaMap); account != nil {
			payload["oauth_account"] = account
		}
		if v, ok := metaMap["quota_probe_summary"]; ok {
			payload["quota_probe"] = v
		}
	}

	return payload
}

func oauthAccountPayload(meta map[string]any) map[string]any {
	provider, _ := meta["oauth_provider"].(string)
	connectionID, _ := meta["oauth_connection_id"].(string)
	accountID, _ := meta["oauth_account_id"].(string)
	email, _ := meta["oauth_email"].(string)
	planType, _ := meta["oauth_plan_type"].(string)
	if strings.TrimSpace(provider) == "" && strings.TrimSpace(connectionID) == "" && strings.TrimSpace(accountID) == "" && strings.TrimSpace(email) == "" {
		return nil
	}
	return map[string]any{
		"provider":      strings.TrimSpace(provider),
		"connection_id": strings.TrimSpace(connectionID),
		"account_id":    strings.TrimSpace(accountID),
		"email":         strings.TrimSpace(email),
		"plan_type":     strings.TrimSpace(planType),
	}
}

func requestHeadersFromInput(input *[]siteRequestHeader) map[string]string {
	if input == nil || len(*input) == 0 {
		return nil
	}
	result := make(map[string]string, len(*input))
	for _, h := range *input {
		key := strings.TrimSpace(h.Key)
		if key == "" {
			continue
		}
		result[key] = h.Value
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (h Handler) sitePayloadWithCapabilities(item store.Site) map[string]any {
	payload := sitePayload(item)
	if h.sites != nil {
		payload["capabilities"] = h.sites.Capabilities(item.SiteType)
	}
	payload["supports_multiple_api_keys"] = sitepkg.SupportsMultipleAPIKeys(item.SiteType)
	payload["supports_api_key_cost_multiplier"] = sitepkg.SupportsAPIKeyCostMultiplier(item.SiteType)

	return payload
}

func (h Handler) sitePayloadWithStats(r *http.Request, site store.Site) map[string]any {
	payload := h.sitePayloadWithCapabilities(site)
	if h.sites == nil {
		return payload
	}
	if state, err := h.sites.SiteState(r.Context(), site.ID); err == nil {
		return h.sitePayloadWithState(site, state)
	}

	if models, err := h.sites.ListModels(r.Context(), site.ID); err == nil {
		payload["model_count"] = len(models)
	}
	if apiKeys, err := h.sites.APIKeys(r.Context(), site.ID); err == nil {
		payload["api_key_count"] = len(apiKeys)
	}

	return payload
}

func (h Handler) sitePayloadWithEditConfig(r *http.Request, site store.Site) map[string]any {
	payload := h.sitePayloadWithStats(r, site)
	if h.sites == nil {
		return payload
	}
	if sitepkg.CredentialTypeForSiteType(site.SiteType) == "oauth" {
		if h.oauth == nil {
			return payload
		}
		if connection, err := h.oauth.ConnectionBySiteID(r.Context(), site.ID); err == nil {
			provider := strings.TrimSpace(site.SiteType)
			payload["auth_config"] = map[string]any{
				provider: map[string]any{
					"provider":        provider,
					"connection_id":   connection.Connection.ID.String(),
					"status":          connection.Connection.Status,
					"email":           connection.Email,
					"account_id":      connection.AccountID,
					"plan_type":       connection.PlanType,
					"expires_at":      nullTimeValue(connection.Connection.ExpiresAt),
					"last_refresh_at": nullTimeValue(connection.Connection.LastRefreshAt),
					"last_sync_at":    nullTimeValue(connection.Connection.LastSyncAt),
					"quota":           connection.Quota,
				},
			}
		}
		return payload
	}
	if !isNewAPISiteType(site.SiteType) {
		if site.SiteType == "xlyra" {
			payload["auth_config"] = h.xlyraAuthConfig(r.Context(), site.ID)
		}
		return payload
	}

	auth, err := h.sites.SystemAuth(r.Context(), site.ID)
	if err != nil {
		return payload
	}

	payload["auth_config"] = map[string]any{
		"newapi": map[string]any{
			"access_token": auth.AccessToken,
			"user_id":      auth.UserID,
		},
	}

	return payload
}

func (h Handler) xlyraAuthConfig(ctx context.Context, siteID uuid.UUID) map[string]any {
	result := map[string]any{"xlyra": map[string]any{"auth_mode": "api_key"}}
	if h.sites == nil {
		return result
	}
	if auth, err := h.sites.SystemAuth(ctx, siteID); err == nil && strings.TrimSpace(auth.AccessToken) != "" {
		result["xlyra"] = map[string]any{
			"auth_mode":    "access_token",
			"access_token": auth.AccessToken,
		}
	}
	return result
}

func (h Handler) sitePayloadWithState(site store.Site, state store.SiteState) map[string]any {
	payload := h.sitePayloadWithCapabilities(site)
	if state.SiteID == uuid.Nil {
		payload["model_count"] = 0
		payload["api_key_count"] = 0
		payload["sync_state"] = map[string]any{"status": "pending"}
		return payload
	}

	payload["model_count"] = state.ModelCount
	payload["api_key_count"] = state.APIKeyCount
	payload["sync_state"] = siteStatePayload(state)
	if state.ValidationOK.Valid {
		payload["validation"] = map[string]any{
			"ok":      state.ValidationOK.Bool,
			"message": nullStringValue(state.ValidationMessage),
		}
	}

	return payload
}

func siteStatePayload(state store.SiteState) map[string]any {
	return map[string]any{
		"status":               state.SyncStatus,
		"message":              nullStringValue(state.SyncMessage),
		"failure_class":        siteStateFailureClass(state),
		"validation_ok":        nullBoolValue(state.ValidationOK),
		"validation_message":   nullStringValue(state.ValidationMessage),
		"last_sync_started_at": nullTimeValue(state.LastSyncStartedAt),
		"last_synced_at":       nullTimeValue(state.LastSyncedAt),
		"api_key_count":        state.APIKeyCount,
		"model_count":          state.ModelCount,
		"raw_status":           jsonRaw(state.RawStatus),
		"user_summary":         jsonRaw(state.UserSummary),
		"pricing":              jsonRaw(state.Pricing),
		"checkin":              jsonRaw(state.Checkin),
		"updated_at":           timeString(state.UpdatedAt),
	}
}

func siteStateFailureClass(state store.SiteState) any {
	if !state.SyncMessage.Valid || strings.TrimSpace(state.SyncMessage.String) == "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(state.SyncStatus), "synced") {
		return nil
	}
	return string(upstream.ClassifyMessage(state.SyncMessage.String).Class)
}

func siteUsagePayload(row store.SiteUsageSummaryRow) map[string]any {
	return map[string]any{
		"request_count":     row.RequestCount,
		"success_count":     row.SuccessCount,
		"failed_count":      row.FailedCount,
		"prompt_tokens":     row.PromptTokens,
		"completion_tokens": row.CompletionTokens,
		"total_tokens":      row.TotalTokens,
		"estimated_cost":    row.EstimatedCost,
		"currency":          row.Currency,
		"first_request_at":  timeValue(row.FirstRequestAt),
		"last_request_at":   timeValue(row.LastRequestAt),
	}
}

func siteHealthResultPayload(result sitepkg.HealthResult) map[string]any {
	recent := make([]map[string]any, 0, len(result.Recent))
	for _, item := range result.Recent {
		recent = append(recent, healthSnapshotPayload(item))
	}

	payload := map[string]any{
		"site":   sitePayload(result.Site),
		"health": siteHealthStatePayload(result.State),
		"recent": recent,
		"meta":   map[string]any{"recent_count": len(recent)},
	}
	if result.Snapshot.ID != uuid.Nil {
		payload["snapshot"] = healthSnapshotPayload(result.Snapshot)
		payload["ok"] = result.Snapshot.Success
	}

	return payload
}

func siteHealthStatePayload(state store.SiteHealthState) map[string]any {
	return map[string]any{
		"site_id":               state.SiteID.String(),
		"status":                state.Status,
		"last_snapshot_id":      nullUUIDValue(state.LastSnapshotID),
		"last_success_at":       nullTimeValue(state.LastSuccessAt),
		"last_failure_at":       nullTimeValue(state.LastFailureAt),
		"consecutive_failures":  state.ConsecutiveFailures,
		"recent_success_rate":   nullFloat64Value(state.RecentSuccessRate),
		"recent_avg_latency_ms": nullInt64Value(state.RecentAvgLatencyMS),
		"checked_at":            nullTimeValue(state.CheckedAt),
		"message":               nullStringValue(state.Message),
		"metadata":              jsonRaw(state.Metadata),
		"created_at":            timeValue(state.CreatedAt),
		"updated_at":            timeValue(state.UpdatedAt),
	}
}

func healthSnapshotPayload(item store.HealthSnapshot) map[string]any {
	return map[string]any{
		"id":            item.ID.String(),
		"site_id":       item.SiteID.String(),
		"site_model_id": nullUUIDValue(item.SiteModelID),
		"scope":         item.Scope,
		"source":        item.Source,
		"endpoint":      item.Endpoint,
		"method":        item.Method,
		"success":       item.Success,
		"status_code":   nullInt64Value(item.StatusCode),
		"latency_ms":    nullInt64Value(item.LatencyMS),
		"error_type":    nullStringValue(item.ErrorType),
		"error_message": nullStringValue(item.ErrorMessage),
		"checked_at":    timeString(item.CheckedAt),
		"metadata":      jsonRaw(item.Metadata),
	}
}

func (h Handler) refreshResultPayload(ctx context.Context, result sitepkg.RefreshResult) map[string]any {
	sitePayload := h.sitePayloadWithState(result.Site, result.State)
	if h.usage != nil {
		if row, ok, err := h.usage.UsageSummaryForSite(ctx, result.Site.ID, nil); err == nil && ok {
			sitePayload["usage"] = siteUsagePayload(row)
		}
	}
	payload := map[string]any{
		"site":         sitePayload,
		"state":        siteStatePayload(result.State),
		"model_sync":   map[string]any{"ok": true, "count": len(result.Models), "items": siteModelPayloads(result.Models)},
		"api_key_sync": map[string]any{"ok": true, "count": len(result.APIKeyStates)},
	}
	return payload
}

func siteModelPayload(model store.SiteModel) map[string]any {
	return map[string]any{
		"id":                         model.ID.String(),
		"site_id":                    model.SiteID.String(),
		"canonical_model_id":         nullUUIDValue(model.CanonicalID),
		"upstream_model_name":        model.UpstreamName,
		"display_name":               model.DisplayName,
		"capabilities":               jsonRaw(model.Capabilities),
		"status":                     model.Status,
		"enabled":                    model.Status == "active",
		"canonical_match_source":     model.MatchSource,
		"canonical_match_confidence": model.MatchConfidence,
		"canonical_matched_at":       nullTimeValue(model.MatchedAt),
		"created_at":                 timeString(model.CreatedAt),
		"updated_at":                 timeString(model.UpdatedAt),
	}
}

func siteModelPayloads(models []store.SiteModel) []map[string]any {
	items := make([]map[string]any, 0, len(models))
	for _, model := range models {
		items = append(items, siteModelPayload(model))
	}

	return items
}

func sitePricingGroupPayload(group store.SitePricingGroup) map[string]any {
	return map[string]any{
		"id":             group.ID.String(),
		"site_id":        group.SiteID.String(),
		"group_name":     group.GroupName,
		"display_name":   nullStringValue(group.DisplayName),
		"ratio":          group.Ratio,
		"is_auto":        group.IsAuto,
		"available":      group.Available,
		"raw":            jsonRaw(group.Raw),
		"last_synced_at": nullTimeValue(group.LastSyncedAt),
		"created_at":     timeString(group.CreatedAt),
		"updated_at":     timeString(group.UpdatedAt),
	}
}

func sitePricingGroupPayloads(groups []store.SitePricingGroup) []map[string]any {
	items := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		items = append(items, sitePricingGroupPayload(group))
	}
	return items
}

func siteModelPricingPayload(item store.SiteModelPricing, siteItem store.Site, credentialPricings ...[]sitepkg.CredentialModelPricing) map[string]any {
	derived := siteModelPricingDerivedValues(item)
	payload := map[string]any{
		"id":                          item.ID.String(),
		"site_id":                     item.SiteID.String(),
		"site_model_id":               nullUUIDValue(item.SiteModelID),
		"model_name":                  item.ModelName,
		"group_name":                  item.GroupName,
		"quota_type":                  item.QuotaType,
		"billing_type":                item.BillingType,
		"currency":                    item.Currency,
		"group_ratio":                 item.GroupRatio,
		"model_ratio":                 nullFloat64Value(item.ModelRatio),
		"completion_ratio":            nullFloat64Value(item.CompletionRatio),
		"cache_ratio":                 nullFloat64Value(item.CacheRatio),
		"create_cache_ratio":          nullFloat64Value(item.CreateCacheRatio),
		"create_cache_1h_ratio":       nullFloat64Value(item.CreateCache1hRatio),
		"image_ratio":                 nullFloat64Value(item.ImageRatio),
		"audio_ratio":                 nullFloat64Value(item.AudioRatio),
		"audio_completion_ratio":      nullFloat64Value(item.AudioCompletionRatio),
		"model_price":                 nullFloat64Value(item.ModelPrice),
		"per_request_value":           nullFloat64Value(item.PerRequestValue),
		"input_value":                 nullFloat64Value(item.InputValue),
		"output_value":                nullFloat64Value(item.OutputValue),
		"cache_input_value":           derived.CacheInputValue,
		"create_cache_input_value":    derived.CreateCacheInputValue,
		"create_cache_1h_input_value": derived.CreateCache1hInputValue,
		"image_input_value":           derived.ImageInputValue,
		"audio_input_value":           derived.AudioInputValue,
		"audio_output_value":          derived.AudioOutputValue,
		"calculation":                 derived.Calculation,
		"vendor_id":                   nullInt64Value(item.VendorID),
		"vendor_name":                 nullStringValue(item.VendorName),
		"vendor_icon":                 nullStringValue(item.VendorIcon),
		"description":                 nullStringValue(item.Description),
		"owner_by":                    nullStringValue(item.OwnerBy),
		"pricing_source":              item.PricingSource,
		"manual_override":             item.ManualOverride,
		"manual_updated_at":           nullTimeValue(item.ManualUpdatedAt),
		"manual_note":                 nullStringValue(item.ManualNote),
		"available":                   item.Available,
		"raw":                         jsonRaw(item.Raw),
		"last_synced_at":              nullTimeValue(item.LastSyncedAt),
		"created_at":                  timeString(item.CreatedAt),
		"updated_at":                  timeString(item.UpdatedAt),
	}
	if len(credentialPricings) > 0 {
		payload["credential_pricings"] = credentialModelPricingPayloads(credentialPricings[0], &item)
	}

	if siteItem.ID != uuid.Nil {
		payload["site_name"] = siteItem.Name
		payload["site_slug"] = siteItem.Slug
		payload["site_type"] = siteItem.SiteType
		payload["site_enabled"] = siteItem.Enabled
	}

	return payload
}

type siteModelPricingDerived struct {
	CacheInputValue         any
	CreateCacheInputValue   any
	CreateCache1hInputValue any
	ImageInputValue         any
	AudioInputValue         any
	AudioOutputValue        any
	Calculation             map[string]any
}

func siteModelPricingDerivedValues(item store.SiteModelPricing) siteModelPricingDerived {
	inputValue := nullFloat64Value(item.InputValue)
	outputValue := nullFloat64Value(item.OutputValue)
	perRequestValue := nullFloat64Value(item.PerRequestValue)
	cacheInputValue := scaledNullFloat(item.InputValue, item.CacheRatio)
	createCacheInputValue := scaledNullFloat(item.InputValue, item.CreateCacheRatio)
	createCache1hInputValue := scaledNullFloat(item.InputValue, item.CreateCache1hRatio)
	imageInputValue := scaledNullFloat(item.InputValue, item.ImageRatio)
	audioInputValue := scaledNullFloat(item.InputValue, item.AudioRatio)
	audioOutputValue := chainedScaledNullFloat(item.InputValue, item.AudioRatio, item.AudioCompletionRatio)

	return siteModelPricingDerived{
		CacheInputValue:         cacheInputValue,
		CreateCacheInputValue:   createCacheInputValue,
		CreateCache1hInputValue: createCache1hInputValue,
		ImageInputValue:         imageInputValue,
		AudioInputValue:         audioInputValue,
		AudioOutputValue:        audioOutputValue,
		Calculation: map[string]any{
			"input": map[string]any{
				"formula": "input_value = model_ratio * group_ratio * usd_per_1m_tokens",
				"value":   inputValue,
			},
			"output": map[string]any{
				"formula": "output_value = input_value * completion_ratio",
				"value":   outputValue,
			},
			"per_request": map[string]any{
				"formula": "per_request_value = model_price * group_ratio",
				"value":   perRequestValue,
			},
			"cache_input": map[string]any{
				"formula": "cache_input_value = input_value * cache_ratio",
				"value":   cacheInputValue,
			},
			"create_cache_input": map[string]any{
				"formula": "create_cache_input_value = input_value * create_cache_ratio",
				"value":   createCacheInputValue,
			},
			"create_cache_1h_input": map[string]any{
				"formula": "create_cache_1h_input_value = input_value * create_cache_1h_ratio",
				"value":   createCache1hInputValue,
			},
			"image_input": map[string]any{
				"formula": "image_input_value = input_value * image_ratio",
				"value":   imageInputValue,
			},
			"audio_input": map[string]any{
				"formula": "audio_input_value = input_value * audio_ratio",
				"value":   audioInputValue,
			},
			"audio_output": map[string]any{
				"formula": "audio_output_value = input_value * audio_ratio * audio_completion_ratio",
				"value":   audioOutputValue,
			},
		},
	}
}

func siteModelPricingPayloads(items []store.SiteModelPricing, siteItem store.Site, credentialPricings ...map[uuid.UUID][]sitepkg.CredentialModelPricing) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		var modelPricings []sitepkg.CredentialModelPricing
		if len(credentialPricings) > 0 && item.SiteModelID.Valid {
			modelPricings = credentialPricings[0][item.SiteModelID.UUID]
		}
		result = append(result, siteModelPricingPayload(item, siteItem, modelPricings))
	}
	return result
}

func credentialModelPricingsByModelID(items []sitepkg.CredentialModelPricing) map[uuid.UUID][]sitepkg.CredentialModelPricing {
	result := make(map[uuid.UUID][]sitepkg.CredentialModelPricing)
	for _, item := range items {
		result[item.SiteModelID] = append(result[item.SiteModelID], item)
	}
	return result
}

func credentialModelPricingPayloads(items []sitepkg.CredentialModelPricing, pricing *store.SiteModelPricing) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload := map[string]any{
			"credential_id":               item.SiteCredentialID.String(),
			"credential_name":             item.CredentialName,
			"routing_priority":            item.RoutingPriority,
			"group_ratio":                 item.GroupRatio,
			"credential_enabled":          item.CredentialEnabled,
			"credential_usable":           item.CredentialUsable,
			"model_enabled":               item.ModelEnabled,
			"model_available":             item.ModelAvailable,
			"billing_type":                nil,
			"currency":                    nil,
			"input_value":                 nil,
			"output_value":                nil,
			"cache_input_value":           nil,
			"create_cache_input_value":    nil,
			"create_cache_1h_input_value": nil,
			"audio_output_value":          nil,
			"per_request_value":           nil,
		}
		if pricing != nil {
			derived := siteModelPricingDerivedValues(*pricing)
			payload["billing_type"] = pricing.BillingType
			payload["currency"] = pricing.Currency
			payload["input_value"] = multiplyPricingValue(nullFloat64Value(pricing.InputValue), item.GroupRatio)
			payload["output_value"] = multiplyPricingValue(nullFloat64Value(pricing.OutputValue), item.GroupRatio)
			payload["cache_input_value"] = multiplyPricingValue(derived.CacheInputValue, item.GroupRatio)
			payload["create_cache_input_value"] = multiplyPricingValue(derived.CreateCacheInputValue, item.GroupRatio)
			payload["create_cache_1h_input_value"] = multiplyPricingValue(derived.CreateCache1hInputValue, item.GroupRatio)
			payload["audio_output_value"] = multiplyPricingValue(derived.AudioOutputValue, item.GroupRatio)
			payload["per_request_value"] = multiplyPricingValue(nullFloat64Value(pricing.PerRequestValue), item.GroupRatio)
		}
		result = append(result, payload)
	}
	return result
}

func multiplyPricingValue(value any, multiplier float64) any {
	number, ok := value.(float64)
	if !ok {
		return nil
	}
	return number * multiplier
}

func siteModelNames(models []store.SiteModel) []string {
	names := make([]string, 0, len(models))
	for _, model := range models {
		name := strings.TrimSpace(model.DisplayName)
		if name == "" {
			name = strings.TrimSpace(model.UpstreamName)
		}
		if name != "" {
			names = append(names, name)
		}
	}

	return names
}

func apiKeySummaryModelIDs(models any) []string {
	modelMap, ok := models.(map[string]any)
	if !ok {
		return []string{}
	}

	data, ok := modelMap["data"].([]any)
	if !ok {
		return []string{}
	}

	ids := make([]string, 0, len(data))
	for _, item := range data {
		modelItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := modelItem["id"].(string)
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}

	return ids
}

func apiKeyModelItems(models []string, meta map[string]any) []map[string]any {
	disabled := stringSetFromAny(meta["disabled_models"])
	items := make([]map[string]any, 0, len(models))
	for _, model := range models {
		if strings.TrimSpace(model) == "" {
			continue
		}
		_, isDisabled := disabled[model]
		items = append(items, map[string]any{
			"name":    model,
			"enabled": !isDisabled,
		})
	}

	return items
}

func stringSetFromAny(value any) map[string]struct{} {
	result := map[string]struct{}{}
	switch items := value.(type) {
	case []any:
		for _, item := range items {
			text, _ := item.(string)
			text = strings.TrimSpace(text)
			if text != "" {
				result[text] = struct{}{}
			}
		}
	case []string:
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item != "" {
				result[item] = struct{}{}
			}
		}
	}

	return result
}

func usageHasQuotaData(usage any) bool {
	usageMap, ok := usage.(map[string]any)
	if !ok {
		return false
	}
	if _, ok := usageMap["five_hour"]; ok {
		return true
	}
	if _, ok := usageMap["weekly"]; ok {
		return true
	}
	data, ok := usageMap["data"].(map[string]any)
	if !ok {
		return false
	}
	_, hasGranted := data["total_granted"]
	_, hasUsed := data["total_used"]
	_, hasAvailable := data["total_available"]
	return hasGranted || hasUsed || hasAvailable
}

func usageFromCredentialMeta(meta map[string]any, raw map[string]any) map[string]any {
	source := meta
	if len(raw) > 0 {
		source = raw
	}
	if !tokenQuotaDataAvailable(source) {
		return map[string]any{
			"success": false,
			"source":  "token_list",
			"message": "quota data unavailable",
		}
	}

	remain := intFromAny(source["remain_quota"])
	used := intFromAny(source["used_quota"])
	return map[string]any{
		"success": true,
		"source":  "token_list",
		"data": map[string]any{
			"object":               "token_usage",
			"name":                 source["name"],
			"total_granted":        remain + used,
			"total_used":           used,
			"total_available":      remain,
			"unlimited_quota":      source["unlimited_quota"],
			"model_limits":         source["model_limits"],
			"model_limits_enabled": source["model_limits_enabled"],
			"expires_at":           source["expired_time"],
		},
	}
}

func tokenQuotaDataAvailable(source map[string]any) bool {
	if source["unlimited_quota"] == true {
		return true
	}
	_, hasRemain := source["remain_quota"]
	_, hasUsed := source["used_quota"]
	return hasRemain || hasUsed
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		parsed, _ := strconv.Atoi(v.String())
		return parsed
	default:
		return 0
	}
}

func siteTypeIconURL(siteType string) string {
	switch strings.TrimSpace(strings.ToLower(siteType)) {
	case "openai":
		return "/brand-icons/openai.png"
	case "opencode_go":
		return "/brand-icons/opencode.png"
	case "anthropic":
		return "/brand-icons/anthropic.png"
	case "google_gemini":
		return "/brand-icons/google.png"
	case "deepseek":
		return "/brand-icons/deepseek.png"
	case "minimax":
		return "/brand-icons/minimax.png"
	case "xiaomi_mimo":
		return "/brand-icons/xiaomi.png"
	case "moonshot":
		return "/brand-icons/moonshot.png"
	case "kimi_code":
		return "/brand-icons/moonshot.png"
	case "newapi":
		return "/brand-icons/newapi.png"
	case "xlyra":
		return "/brand-icons/xlyra.png"
	case "codex":
		return "/oauth-icons/codex.svg"
	case "antigravity":
		return "/oauth-icons/antigravity.png"
	case "claude_code":
		return "/brand-icons/anthropic.png"
	default:
		return ""
	}
}
