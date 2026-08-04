package admin

import (
	"net/http"
	"strings"
)

func (h Handler) DetectSiteType(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}

	var payload struct {
		BaseURL string `json:"base_url"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.BaseURL) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_base_url", "base_url is required")
		return
	}

	result, err := h.sites.DetectSiteType(r.Context(), payload.BaseURL)
	if err != nil {
		h.writeError(w, r, http.StatusBadGateway, "site_type_detect_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, result)
}

func (h Handler) NewAPIUserSummary(w http.ResponseWriter, r *http.Request) {
	var payload newAPIUserSummaryRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	h.respondNewAPIUserSummary(w, r, payload.BaseURL, payload.AccessToken, payload.UserID)
}

func (h Handler) SiteNewAPIUserSummary(w http.ResponseWriter, r *http.Request) {
	baseURL, ok := h.newAPISiteBaseURL(w, r)
	if !ok {
		return
	}

	var payload newAPIUserSummaryRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	h.respondNewAPIUserSummary(w, r, baseURL, payload.AccessToken, payload.UserID)
}

func (h Handler) NewAPIAPIKeySummary(w http.ResponseWriter, r *http.Request) {
	var payload newAPIAPIKeySummaryRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	h.respondNewAPIAPIKeySummary(w, r, payload.BaseURL, payload.APIKey)
}

func (h Handler) SiteNewAPIAPIKeySummary(w http.ResponseWriter, r *http.Request) {
	baseURL, ok := h.newAPISiteBaseURL(w, r)
	if !ok {
		return
	}

	var payload newAPIAPIKeySummaryRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	h.respondNewAPIAPIKeySummary(w, r, baseURL, payload.APIKey)
}

func (h Handler) NewAPICheckin(w http.ResponseWriter, r *http.Request) {
	var payload newAPICheckinRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	h.respondNewAPICheckin(w, r, payload.BaseURL, payload.AccessToken, payload.UserID)
}

func (h Handler) SiteNewAPICheckin(w http.ResponseWriter, r *http.Request) {
	baseURL, ok := h.newAPISiteBaseURL(w, r)
	if !ok {
		return
	}

	var payload newAPICheckinRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	h.respondNewAPICheckin(w, r, baseURL, payload.AccessToken, payload.UserID)
}

func (h Handler) respondNewAPIUserSummary(w http.ResponseWriter, r *http.Request, baseURL string, accessToken string, userID int) {
	if h.newAPI == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "newapi_service_unavailable", "newapi service is not available")
		return
	}
	if strings.TrimSpace(baseURL) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_base_url", "base_url is required")
		return
	}
	if strings.TrimSpace(accessToken) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_access_token", "access_token is required")
		return
	}
	if userID <= 0 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_user_id", "user_id must be greater than 0")
		return
	}

	result, err := h.newAPI.UserSummary(r.Context(), baseURL, accessToken, userID)
	if err != nil {
		h.writeError(w, r, http.StatusBadGateway, "newapi_user_summary_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, result)
}

func (h Handler) respondNewAPIAPIKeySummary(w http.ResponseWriter, r *http.Request, baseURL string, apiKey string) {
	if h.newAPI == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "newapi_service_unavailable", "newapi service is not available")
		return
	}
	if strings.TrimSpace(baseURL) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_base_url", "base_url is required")
		return
	}
	if strings.TrimSpace(apiKey) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_api_key", "api_key is required")
		return
	}

	result, err := h.newAPI.APIKeySummary(r.Context(), baseURL, apiKey)
	if err != nil {
		h.writeError(w, r, http.StatusBadGateway, "newapi_api_key_summary_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, result)
}

func (h Handler) respondNewAPICheckin(w http.ResponseWriter, r *http.Request, baseURL string, accessToken string, userID int) {
	if h.newAPI == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "newapi_service_unavailable", "newapi service is not available")
		return
	}
	if strings.TrimSpace(baseURL) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_base_url", "base_url is required")
		return
	}
	if strings.TrimSpace(accessToken) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_access_token", "access_token is required")
		return
	}
	if userID <= 0 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_user_id", "user_id must be greater than 0")
		return
	}

	result, err := h.newAPI.DoCheckin(r.Context(), baseURL, accessToken, userID)
	if err != nil {
		h.writeError(w, r, http.StatusBadGateway, "newapi_checkin_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusOK, result)
}

func (h Handler) newAPISiteBaseURL(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return "", false
	}

	siteID, ok := siteIDParam(w, r)
	if !ok {
		return "", false
	}

	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
		return "", false
	}

	if item.SiteType != "newapi" {
		h.writeError(w, r, http.StatusBadRequest, "site_type_mismatch", "site_type must be newapi")
		return "", false
	}

	return item.BaseURL, true
}
