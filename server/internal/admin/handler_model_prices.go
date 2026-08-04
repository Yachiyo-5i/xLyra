package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xlyra/server/internal/httpx"
	"xlyra/server/internal/site"
)

type modelPriceRequest struct {
	GroupName               string   `json:"group_name"`
	BillingType             string   `json:"billing_type"`
	Currency                string   `json:"currency"`
	InputValue              *float64 `json:"input_value"`
	OutputValue             *float64 `json:"output_value"`
	CacheInputValue         *float64 `json:"cache_input_value"`
	CreateCacheInputValue   *float64 `json:"create_cache_input_value"`
	CreateCache1hInputValue *float64 `json:"create_cache_1h_input_value"`
	AudioOutputValue        *float64 `json:"audio_output_value"`
	PerRequestValue         *float64 `json:"per_request_value"`
	GroupRatio              *float64 `json:"group_ratio"`
	ManualNote              string   `json:"manual_note"`
}

type bulkModelPriceRequest struct {
	CanonicalModelID string   `json:"canonical_model_id"`
	SiteModelIDs     []string `json:"site_model_ids"`
	modelPriceRequest
}

func (h Handler) ListModelPrices(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	filters, ok := modelPriceFiltersFromRequest(w, r)
	if !ok {
		return
	}
	result, err := h.sites.ListModelPrices(r.Context(), filters)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "model_prices_list_failed", "failed to list model prices")
		return
	}

	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, modelPriceItemPayload(item))
	}
	h.writeItems(w, http.StatusOK, items, map[string]any{
		"count":         result.Count,
		"priced_count":  result.PricedCount,
		"missing_count": result.MissingCount,
		"manual_count":  result.ManualCount,
	})
}

func (h Handler) UpdateSiteModelPricing(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	siteModelID, ok := siteModelIDParam(w, r)
	if !ok {
		return
	}
	var payload modelPriceRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	item, created, err := h.sites.UpsertModelPrice(r.Context(), siteModelID, payload.toInput())
	if err != nil {
		h.writeModelPriceError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	h.invalidateGatewayModelsCache()
	h.writePayload(w, status, map[string]any{
		"item": modelPriceItemPayload(item),
		"meta": map[string]any{
			"created": created,
		},
	})
}

func (h Handler) BulkUpdateModelPrices(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	var payload bulkModelPriceRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	input, ok := payload.toInput(w, r)
	if !ok {
		return
	}
	result, err := h.sites.BulkUpsertModelPrices(r.Context(), input)
	if err != nil {
		h.writeModelPriceError(w, r, err)
		return
	}

	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, map[string]any{
			"site_model_id":  item.Model.ID.String(),
			"pricing_id":     pricingIDValue(item),
			"site_id":        item.Site.ID.String(),
			"site_name":      item.Site.Name,
			"pricing_status": item.PricingStatus,
		})
	}
	h.invalidateGatewayModelsCache()
	h.writeItems(w, http.StatusOK, items, map[string]any{
		"count":         result.Count,
		"created_count": result.CreatedCount,
		"updated_count": result.UpdatedCount,
	})
}

func (h Handler) ResetSiteModelPricing(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	siteModelID, ok := siteModelIDParam(w, r)
	if !ok {
		return
	}
	item, err := h.sites.ResetModelPricing(r.Context(), siteModelID)
	if err != nil {
		h.writeModelPriceError(w, r, err)
		return
	}
	h.invalidateGatewayModelsCache()
	h.writePayload(w, http.StatusOK, map[string]any{
		"item": modelPriceItemPayload(item),
	})
}

func (h Handler) ResetSitePricing(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	siteID, err := uuid.Parse(chi.URLParam(r, "siteID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_site_id", "invalid site ID")
		return
	}
	result, err := h.sites.ResetSitePricing(r.Context(), siteID)
	if err != nil {
		h.writeModelPriceError(w, r, err)
		return
	}
	h.invalidateGatewayModelsCache()
	items := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, modelPriceItemPayload(item))
	}
	h.writePayload(w, http.StatusOK, map[string]any{
		"items": items,
		"meta":  map[string]any{"reset_count": result.ResetCount},
	})
}

func (h Handler) writeModelPriceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, site.ErrModelPriceReadonly):
		h.writeError(w, r, http.StatusForbidden, "pricing_readonly", "newapi pricing is synced from upstream and cannot be edited manually")
	case errors.Is(err, site.ErrModelPriceCanonicalMismatch):
		h.writeError(w, r, http.StatusBadRequest, "canonical_model_mismatch", err.Error())
	case errors.Is(err, site.ErrModelPriceNoCanonical):
		h.writeError(w, r, http.StatusBadRequest, "no_canonical_pricing", "model has no canonical pricing to reset to")
	case errors.Is(err, site.ErrModelPriceInvalid):
		h.writeError(w, r, http.StatusBadRequest, "invalid_model_price", err.Error())
	default:
		h.writeError(w, r, http.StatusInternalServerError, "model_price_update_failed", "failed to update model price")
	}
}

func modelPriceFiltersFromRequest(w http.ResponseWriter, r *http.Request) (site.ModelPriceFilters, bool) {
	query := r.URL.Query()
	filters := site.ModelPriceFilters{
		Q:             strings.TrimSpace(query.Get("q")),
		SiteType:      strings.TrimSpace(query.Get("site_type")),
		Provider:      strings.TrimSpace(query.Get("provider")),
		ModelKey:      strings.TrimSpace(query.Get("model_key")),
		BillingType:   strings.TrimSpace(strings.ToLower(query.Get("billing_type"))),
		PricingStatus: strings.TrimSpace(strings.ToLower(query.Get("pricing_status"))),
	}
	if raw := strings.TrimSpace(query.Get("site_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_site_id", "site_id must be a valid UUID")
			return filters, false
		}
		filters.SiteID = id
	}
	if raw := strings.TrimSpace(query.Get("canonical_model_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, "invalid_canonical_model_id", "canonical_model_id must be a valid UUID")
			return filters, false
		}
		filters.CanonicalModelID = id
	}
	return filters, true
}

func (r modelPriceRequest) toInput() site.ModelPriceInput {
	return site.ModelPriceInput{
		GroupName:               r.GroupName,
		BillingType:             r.BillingType,
		Currency:                r.Currency,
		InputValue:              r.InputValue,
		OutputValue:             r.OutputValue,
		CacheInputValue:         r.CacheInputValue,
		CreateCacheInputValue:   r.CreateCacheInputValue,
		CreateCache1hInputValue: r.CreateCache1hInputValue,
		AudioOutputValue:        r.AudioOutputValue,
		PerRequestValue:         r.PerRequestValue,
		GroupRatio:              r.GroupRatio,
		ManualNote:              r.ManualNote,
	}
}

func (r bulkModelPriceRequest) toInput(w http.ResponseWriter, req *http.Request) (site.BulkModelPriceInput, bool) {
	canonicalID, err := uuid.Parse(strings.TrimSpace(r.CanonicalModelID))
	if err != nil {
		httpx.Error(w, req, http.StatusBadRequest, "invalid_canonical_model_id", "canonical_model_id must be a valid UUID")
		return site.BulkModelPriceInput{}, false
	}
	siteModelIDs := make([]uuid.UUID, 0, len(r.SiteModelIDs))
	for _, raw := range r.SiteModelIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			httpx.Error(w, req, http.StatusBadRequest, "invalid_site_model_id", "site_model_ids must contain valid UUID values")
			return site.BulkModelPriceInput{}, false
		}
		siteModelIDs = append(siteModelIDs, id)
	}
	return site.BulkModelPriceInput{
		CanonicalModelID: canonicalID,
		SiteModelIDs:     siteModelIDs,
		ModelPriceInput:  r.modelPriceRequest.toInput(),
	}, true
}

func modelPriceItemPayload(item site.ModelPriceItem) map[string]any {
	modelPayload := map[string]any{
		"site_model_id":       item.Model.ID.String(),
		"upstream_model_name": item.Model.UpstreamName,
		"display_name":        item.Model.DisplayName,
		"status":              item.Model.Status,
	}
	if item.Model.CanonicalID.Valid {
		modelPayload["canonical_model_id"] = item.Model.CanonicalID.UUID.String()
	} else {
		modelPayload["canonical_model_id"] = nil
	}
	if item.Canonical != nil {
		modelPayload["canonical_model_key"] = item.Canonical.ModelKey
		modelPayload["canonical_display_name"] = item.Canonical.DisplayName
		modelPayload["provider"] = item.Canonical.Provider
		modelPayload["category"] = item.Canonical.Category
	} else {
		modelPayload["canonical_model_key"] = nil
		modelPayload["canonical_display_name"] = nil
		modelPayload["provider"] = nil
		modelPayload["category"] = nil
	}

	payload := map[string]any{
		"site_model_id":  item.Model.ID.String(),
		"pricing_id":     pricingIDValue(item),
		"site":           modelPriceSitePayload(item),
		"model":          modelPayload,
		"pricing":        modelPricePricingPayload(item),
		"editable":       item.Editable,
		"edit_reason":    modelPriceEmptyToNil(item.EditReason),
		"pricing_status": item.PricingStatus,
	}
	if len(item.CredentialPricings) > 0 {
		payload["credential_pricings"] = credentialModelPricingPayloads(item.CredentialPricings, item.Pricing)
	}
	return payload
}

func modelPriceSitePayload(item site.ModelPriceItem) map[string]any {
	return map[string]any{
		"id":      item.Site.ID.String(),
		"name":    item.Site.Name,
		"slug":    item.Site.Slug,
		"type":    item.Site.SiteType,
		"enabled": item.Site.Enabled,
	}
}

func modelPricePricingPayload(item site.ModelPriceItem) any {
	if item.Pricing == nil {
		return nil
	}
	derived := siteModelPricingDerivedValues(*item.Pricing)
	return map[string]any{
		"id":                          item.Pricing.ID.String(),
		"group_name":                  item.Pricing.GroupName,
		"billing_type":                item.Pricing.BillingType,
		"quota_type":                  item.Pricing.QuotaType,
		"currency":                    item.Pricing.Currency,
		"group_ratio":                 item.Pricing.GroupRatio,
		"input_value":                 nullFloat64Value(item.Pricing.InputValue),
		"output_value":                nullFloat64Value(item.Pricing.OutputValue),
		"cache_ratio":                 nullFloat64Value(item.Pricing.CacheRatio),
		"cache_input_value":           derived.CacheInputValue,
		"create_cache_ratio":          nullFloat64Value(item.Pricing.CreateCacheRatio),
		"create_cache_input_value":    derived.CreateCacheInputValue,
		"create_cache_1h_ratio":       nullFloat64Value(item.Pricing.CreateCache1hRatio),
		"create_cache_1h_input_value": derived.CreateCache1hInputValue,
		"audio_output_value":          derived.AudioOutputValue,
		"per_request_value":           nullFloat64Value(item.Pricing.PerRequestValue),
		"pricing_source":              item.Pricing.PricingSource,
		"manual_override":             item.Pricing.ManualOverride,
		"manual_updated_at":           nullTimeValue(item.Pricing.ManualUpdatedAt),
		"manual_note":                 nullStringValue(item.Pricing.ManualNote),
		"available":                   item.Pricing.Available,
		"last_synced_at":              nullTimeValue(item.Pricing.LastSyncedAt),
		"created_at":                  timeString(item.Pricing.CreatedAt),
		"updated_at":                  timeString(item.Pricing.UpdatedAt),
	}
}

func pricingIDValue(item site.ModelPriceItem) any {
	if item.Pricing == nil {
		return nil
	}
	return item.Pricing.ID.String()
}

func modelPriceEmptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
