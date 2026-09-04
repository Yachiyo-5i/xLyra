package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/store"
)

func (h Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "auth service is not available")
		return
	}

	adminID, ok := auth.AdminIDFromContext(r.Context())
	if !ok {
		h.writeError(w, r, http.StatusUnauthorized, "unauthorized", "admin session is required")
		return
	}

	var payload apiKeyRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	if strings.TrimSpace(payload.Name) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_api_key_name", "api key name is required")
		return
	}

	input, ok := apiKeyInputFromRequest(w, r, payload)
	if !ok {
		return
	}

	result, err := h.auth.CreateAPIKey(r.Context(), input, adminID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_create_failed", err.Error())
		return
	}

	models, _ := h.auth.APIKeyEffectiveSiteModels(r.Context(), result.APIKey)
	sites, _ := h.auth.APIKeySites(r.Context(), result.APIKey.ID)
	groups, _ := h.auth.APIKeySiteGroups(r.Context(), result.APIKey.ID)
	h.logInfo("api key created", "api_key_id", result.APIKey.ID, "name", result.APIKey.Name, "created_by_admin_id", adminID)
	h.invalidateGatewayModelsCacheForAPIKey(result.APIKey.ID)
	h.prewarmGatewayModelsCacheForAPIKey(result.APIKey)
	h.writePayload(w, http.StatusCreated, map[string]any{
		"key":        result.Key,
		"key_prefix": result.KeyPrefix,
		"api_key":    h.apiKeyPayload(r.Context(), result.APIKey, models, sites, groups, true),
	})
}

func (h Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "auth service is not available")
		return
	}

	syncView := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("view")), "sync")
	includeInternal := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_internal")), "true")
	if syncView {
		items, err := h.auth.ListAPIKeys(r.Context())
		if err != nil {
			h.writeError(w, r, http.StatusInternalServerError, "api_key_list_failed", "failed to list api keys")
			return
		}
		payloadItems := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if item.KeyKind == store.APIKeyKindAgentInternal && !includeInternal {
				continue
			}
			payloadItems = append(payloadItems, h.apiKeySyncPayload(item))
		}
		h.writeItems(w, http.StatusOK, payloadItems, map[string]any{"count": len(payloadItems), "view": "sync"})
		return
	}

	items, err := h.auth.ListAPIKeyDetails(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "api_key_list_failed", "failed to list api keys")
		return
	}
	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item.APIKey.KeyKind == store.APIKeyKindAgentInternal && !includeInternal {
			continue
		}
		payloadItems = append(payloadItems, h.apiKeyPayloadWithRateLimit(item.APIKey, item.Models, item.Sites, item.Groups, false, item.RateLimit))
	}
	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{"count": len(payloadItems)})
}

func (h Handler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	models, _ := h.auth.APIKeyEffectiveSiteModels(r.Context(), apiKey)
	sites, _ := h.auth.APIKeySites(r.Context(), apiKey.ID)
	groups, _ := h.auth.APIKeySiteGroups(r.Context(), apiKey.ID)
	h.writeResource(w, http.StatusOK, "api_key", h.apiKeyPayload(r.Context(), apiKey, models, sites, groups, false))
}

// RevealAPIKey returns the decrypted plaintext for a single key. List/detail
// responses only carry masked_key; this explicit, per-key, audited action is the
// only path that discloses plaintext (besides one-time creation).
func (h Handler) RevealAPIKey(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	plaintext, err := h.auth.APIKeyPlaintext(apiKey)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_reveal_failed", "failed to reveal api key")
		return
	}

	actor, _ := auth.AdminActorFromContext(r.Context())
	h.recordAudit(r, actor, "api_key.reveal", "api_key", apiKey.ID.String(), true, "", map[string]any{"name": apiKey.Name})
	h.writePayload(w, http.StatusOK, map[string]any{
		"id":         apiKey.ID.String(),
		"key":        plaintext,
		"masked_key": apiKey.MaskedKey,
	})
}

// RotateAPIKey generates a fresh credential for an existing key while keeping
// every piece of configuration (sites, models, quota, expiry) intact. The old
// secret stops working as soon as the hash is replaced; the new plaintext is
// returned exactly once in this response.
func (h Handler) RotateAPIKey(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	result, err := h.auth.RotateAPIKey(r.Context(), apiKey.ID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_rotate_failed", err.Error())
		return
	}

	actor, _ := auth.AdminActorFromContext(r.Context())
	h.recordAudit(r, actor, "api_key.rotate", "api_key", apiKey.ID.String(), true, "", map[string]any{"name": apiKey.Name})
	h.logInfo("api key rotated", "api_key_id", apiKey.ID, "name", apiKey.Name)
	h.invalidateGatewayModelsCacheForAPIKey(result.APIKey.ID)
	h.prewarmGatewayModelsCacheForAPIKey(result.APIKey)
	models, _ := h.auth.APIKeyEffectiveSiteModels(r.Context(), result.APIKey)
	sites, _ := h.auth.APIKeySites(r.Context(), result.APIKey.ID)
	groups, _ := h.auth.APIKeySiteGroups(r.Context(), result.APIKey.ID)
	h.writePayload(w, http.StatusOK, map[string]any{
		"key":        result.Key,
		"key_prefix": result.KeyPrefix,
		"api_key":    h.apiKeyPayload(r.Context(), result.APIKey, models, sites, groups, false),
	})
}

func (h Handler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	var payload apiKeyRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	input, ok := apiKeyUpdateInputFromRequest(w, r, payload, apiKey)
	if !ok {
		return
	}

	updated, err := h.auth.UpdateAPIKey(r.Context(), apiKey.ID, input)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_update_failed", err.Error())
		return
	}

	models, _ := h.auth.APIKeyEffectiveSiteModels(r.Context(), updated)
	sites, _ := h.auth.APIKeySites(r.Context(), updated.ID)
	groups, _ := h.auth.APIKeySiteGroups(r.Context(), updated.ID)
	h.logInfo("api key updated", "api_key_id", updated.ID, "name", updated.Name)
	h.invalidateGatewayModelsCacheForAPIKey(updated.ID)
	h.prewarmGatewayModelsCacheForAPIKey(updated)
	h.writeResource(w, http.StatusOK, "api_key", h.apiKeyPayload(r.Context(), updated, models, sites, groups, false))
}

type increaseAPIKeyQuotaRequest struct {
	Amount float64 `json:"amount"`
	Reason string  `json:"reason"`
}

func (h Handler) IncreaseAPIKeyQuota(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}
	var payload increaseAPIKeyQuotaRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	payload.Reason = strings.TrimSpace(payload.Reason)
	if payload.Reason == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_quota_increase_reason", "reason is required")
		return
	}
	result, err := h.auth.IncreaseAPIKeyQuota(r.Context(), apiKey.ID, payload.Amount)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_quota_increase_failed", err.Error())
		return
	}
	actor, _ := auth.AdminActorFromContext(r.Context())
	h.recordAudit(r, actor, "api_key.quota.increase", "api_key", apiKey.ID.String(), true, "", map[string]any{
		"amount":       result.Amount,
		"reason":       payload.Reason,
		"limit_before": result.LimitBefore,
		"limit_after":  result.APIKey.QuotaLimit.Float64,
	})
	h.invalidateGatewayModelsCacheForAPIKey(apiKey.ID)
	h.prewarmGatewayModelsCacheForAPIKey(result.APIKey)
	models, _ := h.auth.APIKeyEffectiveSiteModels(r.Context(), result.APIKey)
	sites, _ := h.auth.APIKeySites(r.Context(), result.APIKey.ID)
	groups, _ := h.auth.APIKeySiteGroups(r.Context(), result.APIKey.ID)
	h.writeResource(w, http.StatusOK, "api_key", h.apiKeyPayload(r.Context(), result.APIKey, models, sites, groups, false))
}

type resetAPIKeyQuotaRequest struct {
	Scopes []string `json:"scopes"`
}

func (h Handler) ResetAPIKeyQuota(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}
	var payload resetAPIKeyQuotaRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	result, err := h.auth.ResetAPIKeyQuota(r.Context(), apiKey.ID, payload.Scopes)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_quota_reset_failed", err.Error())
		return
	}
	actor, _ := auth.AdminActorFromContext(r.Context())
	h.recordAudit(r, actor, "api_key.quota.reset", "api_key", apiKey.ID.String(), true, "", map[string]any{
		"scopes":             result.Scopes,
		"total_used_before":  result.TotalUsedBefore,
		"daily_used_before":  result.DailyUsedBefore,
		"weekly_used_before": result.WeeklyUsedBefore,
	})
	h.invalidateGatewayModelsCacheForAPIKey(apiKey.ID)
	h.prewarmGatewayModelsCacheForAPIKey(result.APIKey)
	models, _ := h.auth.APIKeyEffectiveSiteModels(r.Context(), result.APIKey)
	sites, _ := h.auth.APIKeySites(r.Context(), result.APIKey.ID)
	groups, _ := h.auth.APIKeySiteGroups(r.Context(), result.APIKey.ID)
	h.writeResource(w, http.StatusOK, "api_key", h.apiKeyPayload(r.Context(), result.APIKey, models, sites, groups, false))
}

func (h Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	if err := h.auth.DeleteAPIKey(r.Context(), apiKey.ID); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_delete_failed", err.Error())
		return
	}

	h.logInfo("api key deleted", "api_key_id", apiKey.ID, "name", apiKey.Name)
	h.invalidateGatewayModelsCacheForAPIKey(apiKey.ID)
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) ListAPIKeySiteModels(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	models, err := h.auth.APIKeySiteModels(r.Context(), apiKey.ID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_site_models_failed", err.Error())
		return
	}

	h.writeItems(w, http.StatusOK, apiKeySiteModelPayloads(models), map[string]any{"count": len(models)})
}

func (h Handler) ListAPIKeySites(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	sites, err := h.auth.APIKeySites(r.Context(), apiKey.ID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_sites_failed", err.Error())
		return
	}

	h.writeItems(w, http.StatusOK, apiKeySitePayloads(sites), map[string]any{"count": len(sites)})
}

func (h Handler) ListAPIKeySiteGroups(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	groups, err := h.auth.APIKeySiteGroups(r.Context(), apiKey.ID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_site_groups_failed", err.Error())
		return
	}

	h.writeItems(w, http.StatusOK, apiKeySiteGroupPayloads(groups), map[string]any{"count": len(groups)})
}

func (h Handler) UpdateAPIKeySiteModels(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		ModelPolicy  string   `json:"model_policy"`
		SiteModelIDs []string `json:"site_model_ids"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	siteModelIDs, ok := parseUUIDList(w, r, payload.SiteModelIDs, "invalid_site_model_id", "site_model_id")
	if !ok {
		return
	}

	updated, models, err := h.auth.SetAPIKeySiteModels(r.Context(), apiKey.ID, siteModelIDs, payload.ModelPolicy)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_site_models_update_failed", err.Error())
		return
	}
	if visibleModels, err := h.auth.APIKeyEffectiveSiteModels(r.Context(), updated); err == nil {
		models = visibleModels
	}
	sites, _ := h.auth.APIKeySites(r.Context(), updated.ID)
	groups, _ := h.auth.APIKeySiteGroups(r.Context(), updated.ID)

	h.invalidateGatewayModelsCacheForAPIKey(updated.ID)
	h.prewarmGatewayModelsCacheForAPIKey(updated)
	h.writePayload(w, http.StatusOK, map[string]any{
		"api_key": h.apiKeyPayload(r.Context(), updated, models, sites, groups, false),
		"items":   apiKeySiteModelPayloads(models),
	})
}

func (h Handler) UpdateAPIKeySiteGroups(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		GroupIDs []string `json:"group_ids"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	groupIDs, ok := parseUUIDList(w, r, payload.GroupIDs, "invalid_group_id", "group_id")
	if !ok {
		return
	}

	updated, groups, err := h.auth.SetAPIKeySiteGroups(r.Context(), apiKey.ID, groupIDs)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_site_groups_update_failed", err.Error())
		return
	}
	models, _ := h.auth.APIKeyEffectiveSiteModels(r.Context(), updated)
	sites, _ := h.auth.APIKeySites(r.Context(), updated.ID)

	h.invalidateGatewayModelsCacheForAPIKey(updated.ID)
	h.prewarmGatewayModelsCacheForAPIKey(updated)
	h.writePayload(w, http.StatusOK, map[string]any{
		"api_key": h.apiKeyPayload(r.Context(), updated, models, sites, groups, false),
		"items":   apiKeySiteGroupPayloads(groups),
	})
}

func (h Handler) UpdateAPIKeySites(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		SitePolicy string   `json:"site_policy"`
		SiteIDs    []string `json:"site_ids"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	siteIDs, ok := parseSiteIDs(w, r, payload.SiteIDs)
	if !ok {
		return
	}

	updated, sites, err := h.auth.SetAPIKeySites(r.Context(), apiKey.ID, siteIDs, payload.SitePolicy)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_sites_update_failed", err.Error())
		return
	}
	models, _ := h.auth.APIKeyEffectiveSiteModels(r.Context(), updated)
	groups, _ := h.auth.APIKeySiteGroups(r.Context(), updated.ID)

	h.invalidateGatewayModelsCacheForAPIKey(updated.ID)
	h.prewarmGatewayModelsCacheForAPIKey(updated)
	h.writePayload(w, http.StatusOK, map[string]any{
		"api_key": h.apiKeyPayload(r.Context(), updated, models, sites, groups, false),
		"items":   apiKeySitePayloads(sites),
	})
}

func (h Handler) CheckAPIKeyModel(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		ModelKey string `json:"model_key"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	result, err := h.auth.CheckModelAccess(r.Context(), apiKey.ID, payload.ModelKey)
	if err != nil && !errors.Is(err, auth.ErrModelNotAllowed) && !errors.Is(err, auth.ErrSiteNotAllowed) {
		h.writeError(w, r, http.StatusBadRequest, "api_key_model_check_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, map[string]any{
		"allowed":   result.Allowed,
		"model_key": result.ModelKey,
		"api_key":   h.apiKeyPayload(r.Context(), result.APIKey, nil, nil, nil, false),
	})
}

type apiKeyRequest struct {
	Name                 string                  `json:"name"`
	CustomKey            string                  `json:"custom_key"`
	Status               string                  `json:"status"`
	ModelPolicy          string                  `json:"model_policy"`
	SiteModelIDs         []string                `json:"site_model_ids"`
	SitePolicy           string                  `json:"site_policy"`
	SiteIDs              []string                `json:"site_ids"`
	SiteGroupIDs         []string                `json:"site_group_ids"`
	ModelMappings        []modelRuleRequest      `json:"model_mappings"`
	ImageToolBridge      *imageToolBridgeRequest `json:"image_tool_bridge"`
	QuotaLimit           *float64                `json:"quota_limit"`
	QuotaUnlimited       *bool                   `json:"quota_unlimited"`
	QuotaDailyLimit      *float64                `json:"quota_daily_limit"`
	QuotaDailyUnlimited  *bool                   `json:"quota_daily_unlimited"`
	QuotaWeeklyLimit     *float64                `json:"quota_weekly_limit"`
	QuotaWeeklyUnlimited *bool                   `json:"quota_weekly_unlimited"`
	ExpiresAt            json.RawMessage         `json:"expires_at"`
	RateLimit            *rateLimitRequest       `json:"rate_limit"`
}

type modelRuleRequest struct {
	Pattern string `json:"pattern"`
	Target  string `json:"target"`
	Mode    string `json:"mode"`
}

func authModelRules(rules []modelRuleRequest) []store.APIKeyModelRule {
	if rules == nil {
		return nil
	}
	result := make([]store.APIKeyModelRule, 0, len(rules))
	for _, rule := range rules {
		result = append(result, store.APIKeyModelRule{
			Pattern: rule.Pattern,
			Target:  rule.Target,
			Mode:    rule.Mode,
		})
	}
	return result
}

type imageToolBridgeRequest struct {
	Enabled  bool    `json:"enabled"`
	Model    string  `json:"model"`
	SiteID   *string `json:"site_id"`
	MaxCalls *int    `json:"max_calls"`
}

type rateLimitRequest struct {
	Status   string `json:"status"`
	RPMLimit *int64 `json:"rpm_limit"`
	TPMLimit *int64 `json:"tpm_limit"`
}

func (h Handler) apiKeyByParam(w http.ResponseWriter, r *http.Request) (store.APIKey, bool) {
	if h.auth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "auth service is not available")
		return store.APIKey{}, false
	}

	apiKeyID, ok := apiKeyIDParam(w, r)
	if !ok {
		return store.APIKey{}, false
	}

	apiKey, err := h.auth.GetAPIKey(r.Context(), apiKeyID)
	if err != nil || apiKey.KeyKind == store.APIKeyKindAgentInternal {
		h.writeError(w, r, http.StatusNotFound, "api_key_not_found", "api key was not found")
		return store.APIKey{}, false
	}

	return apiKey, true
}

func apiKeyInputFromRequest(w http.ResponseWriter, r *http.Request, payload apiKeyRequest) (auth.CreateAPIKeyInput, bool) {
	expiresAt, _, ok := parseOptionalTime(w, r, payload.ExpiresAt)
	if !ok {
		return auth.CreateAPIKeyInput{}, false
	}

	quotaUnlimited := true
	if payload.QuotaLimit != nil {
		quotaUnlimited = false
	}
	if payload.QuotaUnlimited != nil {
		quotaUnlimited = *payload.QuotaUnlimited
	}
	dailyUnlimited := true
	if payload.QuotaDailyLimit != nil {
		dailyUnlimited = false
	}
	if payload.QuotaDailyUnlimited != nil {
		dailyUnlimited = *payload.QuotaDailyUnlimited
	}
	weeklyUnlimited := true
	if payload.QuotaWeeklyLimit != nil {
		weeklyUnlimited = false
	}
	if payload.QuotaWeeklyUnlimited != nil {
		weeklyUnlimited = *payload.QuotaWeeklyUnlimited
	}

	siteIDs, ok := parseSiteIDs(w, r, payload.SiteIDs)
	if !ok {
		return auth.CreateAPIKeyInput{}, false
	}
	siteModelIDs, ok := parseUUIDList(w, r, payload.SiteModelIDs, "invalid_site_model_id", "site_model_id")
	if !ok {
		return auth.CreateAPIKeyInput{}, false
	}
	siteGroupIDs, ok := parseUUIDList(w, r, payload.SiteGroupIDs, "invalid_group_id", "group_id")
	if !ok {
		return auth.CreateAPIKeyInput{}, false
	}
	imageToolBridge, ok := authImageToolBridgeInput(w, r, payload.ImageToolBridge)
	if !ok {
		return auth.CreateAPIKeyInput{}, false
	}

	return auth.CreateAPIKeyInput{
		Name:                 payload.Name,
		CustomKey:            payload.CustomKey,
		ModelPolicy:          payload.ModelPolicy,
		SiteModelIDs:         siteModelIDs,
		SitePolicy:           payload.SitePolicy,
		SiteIDs:              siteIDs,
		SiteGroupIDs:         siteGroupIDs,
		ModelRules:           authModelRules(payload.ModelMappings),
		ImageToolBridge:      imageToolBridge,
		QuotaLimit:           payload.QuotaLimit,
		QuotaUnlimited:       quotaUnlimited,
		QuotaDailyLimit:      payload.QuotaDailyLimit,
		QuotaDailyUnlimited:  &dailyUnlimited,
		QuotaWeeklyLimit:     payload.QuotaWeeklyLimit,
		QuotaWeeklyUnlimited: &weeklyUnlimited,
		ExpiresAt:            expiresAt,
		RateLimit:            authRateLimitInput(payload.RateLimit),
	}, true
}

func apiKeyUpdateInputFromRequest(w http.ResponseWriter, r *http.Request, payload apiKeyRequest, current store.APIKey) (auth.UpdateAPIKeyInput, bool) {
	expiresAt, expiresAtProvided, ok := parseOptionalTime(w, r, payload.ExpiresAt)
	if !ok {
		return auth.UpdateAPIKeyInput{}, false
	}
	if !expiresAtProvided {
		expiresAt = current.ExpiresAt
	}

	quotaLimit := payload.QuotaLimit
	if quotaLimit == nil && current.QuotaLimit.Valid {
		value := current.QuotaLimit.Float64
		quotaLimit = &value
	}

	quotaUnlimited := current.QuotaUnlimited
	if payload.QuotaUnlimited != nil {
		quotaUnlimited = *payload.QuotaUnlimited
	}
	dailyLimit := payload.QuotaDailyLimit
	if dailyLimit == nil && current.QuotaDailyLimit.Valid {
		value := current.QuotaDailyLimit.Float64
		dailyLimit = &value
	}
	dailyUnlimited := current.QuotaDailyUnlimited
	if payload.QuotaDailyUnlimited != nil {
		dailyUnlimited = *payload.QuotaDailyUnlimited
	}
	weeklyLimit := payload.QuotaWeeklyLimit
	if weeklyLimit == nil && current.QuotaWeeklyLimit.Valid {
		value := current.QuotaWeeklyLimit.Float64
		weeklyLimit = &value
	}
	weeklyUnlimited := current.QuotaWeeklyUnlimited
	if payload.QuotaWeeklyUnlimited != nil {
		weeklyUnlimited = *payload.QuotaWeeklyUnlimited
	}

	var siteIDs []uuid.UUID
	if payload.SiteIDs != nil {
		siteIDs, ok = parseSiteIDs(w, r, payload.SiteIDs)
		if !ok {
			return auth.UpdateAPIKeyInput{}, false
		}
	}
	var siteModelIDs []uuid.UUID
	if payload.SiteModelIDs != nil {
		siteModelIDs, ok = parseUUIDList(w, r, payload.SiteModelIDs, "invalid_site_model_id", "site_model_id")
		if !ok {
			return auth.UpdateAPIKeyInput{}, false
		}
	}
	var siteGroupIDs []uuid.UUID
	if payload.SiteGroupIDs != nil {
		siteGroupIDs, ok = parseUUIDList(w, r, payload.SiteGroupIDs, "invalid_group_id", "group_id")
		if !ok {
			return auth.UpdateAPIKeyInput{}, false
		}
	}

	imageToolBridge, ok := authImageToolBridgeInput(w, r, payload.ImageToolBridge)
	if !ok {
		return auth.UpdateAPIKeyInput{}, false
	}

	return auth.UpdateAPIKeyInput{
		Name:                 payload.Name,
		Status:               payload.Status,
		ModelPolicy:          payload.ModelPolicy,
		SiteModelIDs:         siteModelIDs,
		SitePolicy:           payload.SitePolicy,
		SiteIDs:              siteIDs,
		SiteGroupIDs:         siteGroupIDs,
		ModelRules:           authModelRules(payload.ModelMappings),
		ImageToolBridge:      imageToolBridge,
		QuotaLimit:           quotaLimit,
		QuotaUnlimited:       quotaUnlimited,
		QuotaDailyLimit:      dailyLimit,
		QuotaDailyUnlimited:  &dailyUnlimited,
		QuotaWeeklyLimit:     weeklyLimit,
		QuotaWeeklyUnlimited: &weeklyUnlimited,
		ExpiresAt:            expiresAt,
		RateLimit:            authRateLimitInput(payload.RateLimit),
	}, true
}

func authImageToolBridgeInput(w http.ResponseWriter, r *http.Request, input *imageToolBridgeRequest) (*auth.ImageToolBridgeInput, bool) {
	if input == nil {
		return nil, true
	}
	result := &auth.ImageToolBridgeInput{
		Enabled:  input.Enabled,
		Model:    input.Model,
		MaxCalls: input.MaxCalls,
	}
	if input.SiteID != nil && strings.TrimSpace(*input.SiteID) != "" {
		siteID, err := uuid.Parse(strings.TrimSpace(*input.SiteID))
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_site_id", "image_tool_bridge.site_id must be a valid uuid")
			return nil, false
		}
		result.SiteID = &siteID
	}
	return result, true
}

func parseOptionalTime(w http.ResponseWriter, r *http.Request, raw json.RawMessage) (*time.Time, bool, bool) {
	if len(raw) == 0 {
		return nil, false, true
	}

	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, true, true
	}

	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_expires_at", "expires_at must be an RFC3339 string, null, or omitted")
		return nil, true, false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, true, true
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_expires_at", "expires_at must be RFC3339")
		return nil, true, false
	}
	if !parsed.After(time.Now()) {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_expires_at", "expires_at must be later than current time")
		return nil, true, false
	}
	return &parsed, true, true
}

func (h Handler) apiKeyPayload(ctx context.Context, item store.APIKey, models []store.APIKeySiteModelPermissionDetail, sites []store.APIKeySitePermission, groups []store.APIKeySiteGroupPermission, includePlaintext bool) map[string]any {
	return h.apiKeyPayloadWithRateLimit(item, models, sites, groups, includePlaintext, h.apiKeyRateLimit(ctx, item.ID))
}

func (h Handler) apiKeyPayloadWithRateLimit(item store.APIKey, models []store.APIKeySiteModelPermissionDetail, sites []store.APIKeySitePermission, groups []store.APIKeySiteGroupPermission, includePlaintext bool, rateLimit auth.RateLimitInput) map[string]any {
	var plaintext any
	if includePlaintext {
		if value, err := h.auth.APIKeyPlaintext(item); err == nil {
			plaintext = value
		}
	}
	now := time.Now()
	dailyUsed := item.EffectiveDailyQuotaUsed(now, h.timeZone)
	weeklyUsed := item.EffectiveWeeklyQuotaUsed(now, h.timeZone)
	name := item.Name
	if item.KeyKind == store.APIKeyKindAgentInternal {
		name = store.AgentAPIKeyName
	}

	return map[string]any{
		"id":                     item.ID.String(),
		"name":                   name,
		"key":                    plaintext,
		"key_prefix":             item.KeyPrefix,
		"masked_key":             item.MaskedKey,
		"key_kind":               item.KeyKind,
		"scope":                  item.Scope,
		"status":                 item.Status,
		"model_policy":           item.ModelPolicy,
		"site_policy":            item.SitePolicy,
		"model_mappings":         modelMappingsPayload(item.ModelMappings),
		"image_tool_bridge":      imageToolBridgePayload(item),
		"quota_limit":            nullFloat64Value(item.QuotaLimit),
		"quota_used":             item.QuotaUsed,
		"quota_total_used":       item.EffectiveTotalQuotaUsed(),
		"quota_available":        apiKeyQuotaAvailable(item),
		"quota_total_available":  apiKeyQuotaAvailable(item),
		"quota_total_reset_at":   timePtrValue(item.QuotaTotalResetAt),
		"quota_unlimited":        item.QuotaUnlimited,
		"quota_daily_limit":      nullFloat64Value(item.QuotaDailyLimit),
		"quota_daily_used":       dailyUsed,
		"quota_daily_available":  quotaAvailable(item.QuotaDailyLimit, dailyUsed, item.QuotaDailyUnlimited),
		"quota_daily_unlimited":  item.QuotaDailyUnlimited,
		"quota_daily_reset_at":   quotaResetAt(h.timeZone.StartOfDay(now).AddDate(0, 0, 1), item.QuotaDailyUnlimited),
		"quota_weekly_limit":     nullFloat64Value(item.QuotaWeeklyLimit),
		"quota_weekly_used":      weeklyUsed,
		"quota_weekly_available": quotaAvailable(item.QuotaWeeklyLimit, weeklyUsed, item.QuotaWeeklyUnlimited),
		"quota_weekly_unlimited": item.QuotaWeeklyUnlimited,
		"quota_weekly_reset_at":  quotaResetAt(h.timeZone.StartOfWeek(now).AddDate(0, 0, 7), item.QuotaWeeklyUnlimited),
		"rate_limit":             apiKeyRateLimitPayload(rateLimit),
		"expires_at":             timePtrValue(item.ExpiresAt),
		"last_used_at":           timePtrValue(item.LastUsedAt),
		"site_models":            apiKeySiteModelPayloads(models),
		"sites":                  apiKeySitePayloads(sites),
		"site_groups":            apiKeySiteGroupPayloads(groups),
		"created_at":             timeString(item.CreatedAt),
		"updated_at":             timeString(item.UpdatedAt),
	}
}

func (h Handler) apiKeySyncPayload(item store.APIKey) map[string]any {
	now := time.Now()
	dailyUsed := item.EffectiveDailyQuotaUsed(now, h.timeZone)
	weeklyUsed := item.EffectiveWeeklyQuotaUsed(now, h.timeZone)
	name := item.Name
	if item.KeyKind == store.APIKeyKindAgentInternal {
		name = store.AgentAPIKeyName
	}
	return map[string]any{
		"id":                     item.ID.String(),
		"name":                   name,
		"key":                    nil,
		"key_prefix":             item.KeyPrefix,
		"masked_key":             item.MaskedKey,
		"key_kind":               item.KeyKind,
		"scope":                  item.Scope,
		"status":                 item.Status,
		"quota_limit":            nullFloat64Value(item.QuotaLimit),
		"quota_used":             item.QuotaUsed,
		"quota_total_used":       item.EffectiveTotalQuotaUsed(),
		"quota_available":        apiKeyQuotaAvailable(item),
		"quota_total_available":  apiKeyQuotaAvailable(item),
		"quota_total_reset_at":   timePtrValue(item.QuotaTotalResetAt),
		"quota_unlimited":        item.QuotaUnlimited,
		"quota_daily_limit":      nullFloat64Value(item.QuotaDailyLimit),
		"quota_daily_used":       dailyUsed,
		"quota_daily_available":  quotaAvailable(item.QuotaDailyLimit, dailyUsed, item.QuotaDailyUnlimited),
		"quota_daily_unlimited":  item.QuotaDailyUnlimited,
		"quota_weekly_limit":     nullFloat64Value(item.QuotaWeeklyLimit),
		"quota_weekly_used":      weeklyUsed,
		"quota_weekly_available": quotaAvailable(item.QuotaWeeklyLimit, weeklyUsed, item.QuotaWeeklyUnlimited),
		"quota_weekly_unlimited": item.QuotaWeeklyUnlimited,
		"expires_at":             timePtrValue(item.ExpiresAt),
		"created_at":             timeString(item.CreatedAt),
		"updated_at":             timeString(item.UpdatedAt),
	}
}

func imageToolBridgePayload(item store.APIKey) any {
	cfg, ok := item.ImageBridge()
	if !ok {
		return nil
	}
	payload := map[string]any{
		"enabled":   cfg.Enabled,
		"model":     cfg.Model,
		"max_calls": cfg.MaxCalls,
		"site_id":   nil,
	}
	if cfg.SiteID != nil {
		payload["site_id"] = cfg.SiteID.String()
	}
	return payload
}

func authRateLimitInput(input *rateLimitRequest) *auth.RateLimitInput {
	if input == nil {
		return nil
	}
	return &auth.RateLimitInput{
		Status:   input.Status,
		RPMLimit: input.RPMLimit,
		TPMLimit: input.TPMLimit,
	}
}

func (h Handler) apiKeyRateLimitPayload(ctx context.Context, apiKeyID uuid.UUID) map[string]any {
	return apiKeyRateLimitPayload(h.apiKeyRateLimit(ctx, apiKeyID))
}

func (h Handler) apiKeyRateLimit(ctx context.Context, apiKeyID uuid.UUID) auth.RateLimitInput {
	if h.auth == nil {
		return auth.RateLimitInput{Status: store.RateLimitStatusDisabled}
	}
	config, err := h.auth.APIKeyRateLimit(ctx, apiKeyID)
	if err != nil {
		return auth.RateLimitInput{Status: store.RateLimitStatusDisabled}
	}
	return config
}

func apiKeyRateLimitPayload(config auth.RateLimitInput) map[string]any {
	return map[string]any{
		"status":    config.Status,
		"rpm_limit": int64PtrValue(config.RPMLimit),
		"tpm_limit": int64PtrValue(config.TPMLimit),
	}
}

func int64PtrValue(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func apiKeySiteModelPayloads(items []store.APIKeySiteModelPermissionDetail) []map[string]any {
	if items == nil {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id":                  item.ID.String(),
			"api_key_id":          item.APIKeyID.String(),
			"site_model_id":       item.SiteModelID.String(),
			"site_id":             item.SiteID.String(),
			"site_name":           item.SiteName,
			"site_slug":           item.SiteSlug,
			"site_type":           item.SiteType,
			"upstream_model_name": item.UpstreamModelName,
			"display_name":        item.DisplayName,
			"canonical_model_id":  nullUUIDValue(item.CanonicalModelID),
			"canonical_model_key": nullStringValue(item.CanonicalModelKey),
			"enabled":             item.Enabled,
			"created_at":          timeString(item.CreatedAt),
			"updated_at":          timeString(item.UpdatedAt),
		})
	}
	return result
}

func apiKeySiteGroupPayloads(items []store.APIKeySiteGroupPermission) []map[string]any {
	if items == nil {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id":         item.ID.String(),
			"api_key_id": item.APIKeyID.String(),
			"group_id":   item.GroupID.String(),
			"enabled":    item.Enabled,
			"created_at": timeString(item.CreatedAt),
			"updated_at": timeString(item.UpdatedAt),
		})
	}
	return result
}

func apiKeySitePayloads(items []store.APIKeySitePermission) []map[string]any {
	if items == nil {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		result = append(result, map[string]any{
			"id":         item.ID.String(),
			"api_key_id": item.APIKeyID.String(),
			"site_id":    item.SiteID.String(),
			"enabled":    item.Enabled,
			"created_at": timeString(item.CreatedAt),
			"updated_at": timeString(item.UpdatedAt),
		})
	}
	return result
}

func parseSiteIDs(w http.ResponseWriter, r *http.Request, raw []string) ([]uuid.UUID, bool) {
	return parseUUIDList(w, r, raw, "invalid_site_id", "site_id")
}

func parseUUIDList(w http.ResponseWriter, r *http.Request, raw []string, code string, label string) ([]uuid.UUID, bool) {
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, code, fmt.Sprintf("invalid %s: %s", label, s))
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func apiKeyQuotaAvailable(item store.APIKey) any {
	if item.QuotaUnlimited || !item.QuotaLimit.Valid {
		return nil
	}
	available := item.QuotaLimit.Float64 - item.EffectiveTotalQuotaUsed()
	if available < 0 {
		return 0
	}
	return available
}

func quotaAvailable(limit sql.NullFloat64, used float64, unlimited bool) any {
	if unlimited || !limit.Valid {
		return nil
	}
	available := limit.Float64 - used
	if available < 0 {
		return float64(0)
	}
	return available
}

func quotaResetAt(resetAt time.Time, unlimited bool) any {
	if unlimited {
		return nil
	}
	return resetAt.Format(time.RFC3339)
}

func modelMappingsPayload(raw store.JSON) []store.APIKeyModelRule {
	return store.ParseAPIKeyModelRules(raw)
}

func (h Handler) UpdateAPIKeyModelMappings(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := h.apiKeyByParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		ModelMappings []modelRuleRequest `json:"model_mappings"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	updated, err := h.auth.SetAPIKeyModelMappings(r.Context(), apiKey.ID, authModelRules(payload.ModelMappings))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "api_key_model_mappings_update_failed", err.Error())
		return
	}

	models, _ := h.auth.APIKeyEffectiveSiteModels(r.Context(), updated)
	sites, _ := h.auth.APIKeySites(r.Context(), updated.ID)
	groups, _ := h.auth.APIKeySiteGroups(r.Context(), updated.ID)
	h.invalidateGatewayModelsCacheForAPIKey(updated.ID)
	h.prewarmGatewayModelsCacheForAPIKey(updated)
	h.writePayload(w, http.StatusOK, map[string]any{
		"api_key": h.apiKeyPayload(r.Context(), updated, models, sites, groups, false),
	})
}
