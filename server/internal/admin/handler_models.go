package admin

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/store"
)

func (h Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	items, err := h.catalog.List(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "canonical_models_list_failed", "failed to list canonical models")
		return
	}

	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, canonicalModelItemPayload(item))
	}

	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{"count": len(payloadItems)})
}

func (h Handler) CreateModel(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	var payload canonicalModelRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	model, err := h.catalog.Create(r.Context(), payload.toInput())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "canonical_model_create_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusCreated, "model", canonicalModelPayload(model))
}

func (h Handler) UpdateModel(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}

	var payload canonicalModelRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	model, err := h.catalog.Update(r.Context(), modelID, payload.toInput())
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "canonical_model_update_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "model", canonicalModelPayload(model))
}

func (h Handler) UpdateModelPricing(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		InputPrice        *float64 `json:"input_price"`
		OutputPrice       *float64 `json:"output_price"`
		CacheReadRatio    *float64 `json:"cache_read_ratio"`
		CacheWriteRatio   *float64 `json:"cache_write_ratio"`
		CacheWrite1hRatio *float64 `json:"cache_write_1h_ratio"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	model, err := h.catalog.UpdatePricing(r.Context(), modelID, catalog.UpdateCanonicalPricingInput{
		InputPrice:        payload.InputPrice,
		OutputPrice:       payload.OutputPrice,
		CacheReadRatio:    payload.CacheReadRatio,
		CacheWriteRatio:   payload.CacheWriteRatio,
		CacheWrite1hRatio: payload.CacheWrite1hRatio,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "canonical_model_pricing_update_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	if h.sites != nil {
		sites := h.sites
		go sites.PropagateCanonicalPricing(context.WithoutCancel(r.Context()), model)
	}
	h.writeResource(w, http.StatusOK, "model", canonicalModelPayload(model))
}

func (h Handler) ResetModelPricing(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}

	model, err := h.catalog.UpdatePricing(r.Context(), modelID, catalog.UpdateCanonicalPricingInput{Reset: true})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "canonical_model_pricing_reset_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	if h.sites != nil {
		sites := h.sites
		go sites.PropagateCanonicalPricing(context.WithoutCancel(r.Context()), model)
	}
	h.writeResource(w, http.StatusOK, "model", canonicalModelPayload(model))
}

func (h Handler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}

	if _, err := h.catalog.Archive(r.Context(), modelID); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "canonical_model_archive_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) CreateModelAlias(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		Alias string `json:"alias"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	alias, err := h.catalog.AddAlias(r.Context(), modelID, payload.Alias)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "canonical_model_alias_create_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusCreated, "alias", canonicalModelAliasPayload(alias))
}

func (h Handler) DeleteModelAlias(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}
	aliasID, ok := aliasIDParam(w, r)
	if !ok {
		return
	}

	if err := h.catalog.DeleteAlias(r.Context(), modelID, aliasID); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "canonical_model_alias_delete_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) BindSiteModelCanonical(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	siteModelID, ok := siteModelIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		CanonicalModelID *string `json:"canonical_model_id"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	var canonicalID *uuid.UUID
	if payload.CanonicalModelID != nil && strings.TrimSpace(*payload.CanonicalModelID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(*payload.CanonicalModelID))
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_canonical_model_id", "canonical_model_id must be a valid UUID")
			return
		}
		canonicalID = &parsed
	}

	model, err := h.catalog.BindSiteModel(r.Context(), siteModelID, canonicalID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "site_model_bind_failed", err.Error())
		return
	}

	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "model", siteModelPayload(model))
}

func (h Handler) GetModelMatrix(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "catalog_service_unavailable", "catalog service is not available")
		return
	}

	modelID, ok := modelIDParam(w, r)
	if !ok {
		return
	}

	matrix, err := h.catalog.Matrix(r.Context(), modelID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "canonical_model_matrix_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, canonicalModelMatrixPayload(matrix))
}

type canonicalModelRequest struct {
	ModelKey     string         `json:"model_key"`
	DisplayName  string         `json:"display_name"`
	Provider     string         `json:"provider"`
	Category     string         `json:"category"`
	Capabilities map[string]any `json:"capabilities"`
	Status       string         `json:"status"`
}

func (r canonicalModelRequest) toInput() catalog.UpsertCanonicalModelInput {
	return catalog.UpsertCanonicalModelInput{
		ModelKey:     r.ModelKey,
		DisplayName:  r.DisplayName,
		Provider:     r.Provider,
		Category:     r.Category,
		Capabilities: r.Capabilities,
		Status:       r.Status,
	}
}

func canonicalModelPayload(model store.CanonicalModel) map[string]any {
	return map[string]any{
		"id":                     model.ID.String(),
		"model_key":              model.ModelKey,
		"display_name":           model.DisplayName,
		"provider":               model.Provider,
		"icon_url":               providerIconURL(model.Provider),
		"category":               model.Category,
		"capabilities":           jsonRaw(model.Capabilities),
		"status":                 model.Status,
		"input_price":            nullFloat64Value(model.InputPrice),
		"output_price":           nullFloat64Value(model.OutputPrice),
		"audio_output_price":     chainedScaledNullFloat(model.InputPrice, model.AudioRatio, model.AudioCompletionRatio),
		"cache_read_ratio":       nullFloat64Value(model.CacheReadRatio),
		"cache_write_ratio":      nullFloat64Value(model.CacheWriteRatio),
		"cache_write_1h_ratio":   nullFloat64Value(model.CacheWrite1hRatio),
		"pricing_source":         model.PricingSource,
		"last_pricing_synced_at": nullTimeValue(model.LastPricingSyncedAt),
		"created_at":             timeString(model.CreatedAt),
		"updated_at":             timeString(model.UpdatedAt),
	}
}

func canonicalModelItemPayload(item catalog.CanonicalModelItem) map[string]any {
	payload := canonicalModelPayload(item.Model.CanonicalModel)
	payload["site_model_count"] = item.Model.SiteModelCount
	payload["site_count"] = item.Model.SiteCount
	payload["aliases"] = canonicalModelAliasPayloads(item.Aliases)

	return payload
}

func canonicalModelAliasPayload(alias store.CanonicalModelAlias) map[string]any {
	return map[string]any{
		"id":                 alias.ID.String(),
		"canonical_model_id": alias.CanonicalModelID.String(),
		"alias":              alias.Alias,
		"normalized_alias":   alias.NormalizedAlias,
		"source":             alias.Source,
		"created_at":         timeString(alias.CreatedAt),
		"updated_at":         timeString(alias.UpdatedAt),
	}
}

func canonicalModelAliasPayloads(aliases []store.CanonicalModelAlias) []map[string]any {
	items := make([]map[string]any, 0, len(aliases))
	for _, alias := range aliases {
		items = append(items, canonicalModelAliasPayload(alias))
	}
	return items
}

func canonicalModelMatrixPayload(matrix catalog.Matrix) map[string]any {
	items := make([]map[string]any, 0)
	indexBySiteModelID := map[uuid.UUID]int{}

	for _, row := range matrix.Rows {
		index, ok := indexBySiteModelID[row.SiteModelID]
		if !ok {
			item := map[string]any{
				"site_id":                    row.SiteID.String(),
				"site_name":                  row.SiteName,
				"site_slug":                  row.SiteSlug,
				"site_type":                  row.SiteType,
				"site_enabled":               row.SiteEnabled,
				"site_model_id":              row.SiteModelID.String(),
				"upstream_model_name":        row.UpstreamModelName,
				"display_name":               row.DisplayName,
				"model_status":               row.ModelStatus,
				"canonical_match_source":     row.MatchSource,
				"canonical_match_confidence": row.MatchConfidence,
				"canonical_matched_at":       nullTimeValue(row.MatchedAt),
				"api_key_count":              row.APIKeyCount,
				"available_api_key_count":    row.AvailableAPIKeyCount,
				"pricing":                    []map[string]any{},
				"created_at":                 timeString(row.SiteModelCreatedAt),
				"updated_at":                 timeString(row.SiteModelUpdatedAt),
			}
			items = append(items, item)
			index = len(items) - 1
			indexBySiteModelID[row.SiteModelID] = index
		}

		if row.GroupName.Valid {
			pricing := map[string]any{
				"group_name":        row.GroupName.String,
				"currency":          nullStringValue(row.Currency),
				"input_value":       nullFloat64Value(row.InputValue),
				"output_value":      nullFloat64Value(row.OutputValue),
				"per_request_value": nullFloat64Value(row.PerRequestValue),
				"billing_type":      nullStringValue(row.BillingType),
				"available":         nullBoolValue(row.PricingAvailable),
				"last_synced_at":    nullTimeValue(row.PricingLastSyncedAt),
			}
			items[index]["pricing"] = append(items[index]["pricing"].([]map[string]any), pricing)
		}
	}

	return map[string]any{
		"canonical_model": canonicalModelPayload(matrix.Model),
		"items":           items,
		"meta":            map[string]any{"count": len(items)},
	}
}

func providerIconURL(provider string) string {
	switch provider {
	case "openai":
		return "/brand-icons/openai.png"
	case "anthropic":
		return "/brand-icons/anthropic.png"
	case "google":
		return "/brand-icons/google.png"
	case "deepseek":
		return "/brand-icons/deepseek.png"
	case "xai":
		return "/brand-icons/xai.png"
	case "qwen":
		return "/brand-icons/qwen.png"
	case "zhipuai":
		return "/brand-icons/zhipu.png"
	case "moonshotai-cn", "moonshot":
		return "/brand-icons/moonshot.png"
	case "xiaomi":
		return "/brand-icons/xiaomi.png"
	case "minimax":
		return "/brand-icons/minimax.png"
	case "nvidia":
		return "/brand-icons/nvidia.png"
	case "kimi_code":
		return "/brand-icons/moonshot.png"
	case "bytedance":
		return "/brand-icons/bytedance-dark.png"
	case "vidu":
		return "/brand-icons/vidu-dark.png"
	case "stepfun":
		return "/brand-icons/stepfun-dark.png"
	case "alibaba":
		return "/brand-icons/alibaba-dark.png"
	case "kuaishou":
		return "/brand-icons/kuaishou.svg"
	case "baai":
		return "/brand-icons/baai-dark.png"
	case "flux":
		return "/brand-icons/flux-dark.png"
	case "hunyuan":
		return "/brand-icons/hunyuan-dark.png"
	case "sensenova":
		return "/brand-icons/sensenova-dark.png"
	default:
		return ""
	}
}
