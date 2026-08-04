package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	oauthsvc "xlyra/server/internal/oauth"
	sitepkg "xlyra/server/internal/site"
)

type siteGrokAccountUpdateRequest struct {
	Enabled *bool `json:"enabled"`
}

func (h Handler) ListGrokAccounts(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	accounts, err := h.sites.ListGrokAccounts(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "grok_accounts_list_failed", err.Error())
		return
	}
	h.writeItems(w, http.StatusOK, accounts, map[string]any{"count": len(accounts)})
}

func (h Handler) UpdateGrokAccount(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	credentialID, ok := grokCredentialIDParam(w, r)
	if !ok {
		return
	}
	var payload siteGrokAccountUpdateRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	account, err := h.sites.UpdateGrokAccount(r.Context(), siteID, credentialID, sitepkg.GrokAccountUpdate{Enabled: payload.Enabled})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "grok_account_update_failed", err.Error())
		return
	}
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "account", account)
}

func (h Handler) UpdateGrokAccountModel(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	credentialID, ok := grokCredentialIDParam(w, r)
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
	account, err := h.sites.SetGrokAccountModelEnabled(r.Context(), siteID, credentialID, payload.Model, payload.Enabled)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "grok_account_model_update_failed", err.Error())
		return
	}
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "account", account)
}

func (h Handler) DeleteGrokAccount(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	credentialID, ok := grokCredentialIDParam(w, r)
	if !ok {
		return
	}
	if err := h.sites.DeleteGrokAccount(r.Context(), siteID, credentialID); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "grok_account_delete_failed", err.Error())
		return
	}
	h.invalidateGatewayModelsCache()
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) RefreshGrokAccount(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	credentialID, ok := grokCredentialIDParam(w, r)
	if !ok {
		return
	}
	account, err := h.sites.RefreshGrokAccount(r.Context(), siteID, credentialID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "grok_account_refresh_failed", err.Error())
		return
	}
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "account", account)
}

func (h Handler) RefreshGrokAccounts(w http.ResponseWriter, r *http.Request) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return
	}
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	results, err := h.sites.RefreshGrokAccounts(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "grok_accounts_refresh_failed", err.Error())
		return
	}
	accounts, listErr := h.sites.ListGrokAccounts(r.Context(), siteID)
	if listErr != nil {
		h.writeError(w, r, http.StatusInternalServerError, "grok_accounts_list_failed", listErr.Error())
		return
	}
	failed := 0
	for _, result := range results {
		if result.Error != "" {
			failed++
		}
	}
	h.invalidateGatewayModelsCache()
	h.writeItems(w, http.StatusOK, accounts, map[string]any{"count": len(accounts), "failed": failed})
}

type grokDevicePollRequest struct {
	DeviceCode   string     `json:"device_code"`
	CredentialID *uuid.UUID `json:"credential_id"`
}

func (h Handler) StartGrokDeviceLogin(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	auth, err := h.oauth.StartGrokDeviceAuth(r.Context(), siteID)
	if err != nil {
		h.writeError(w, r, http.StatusBadGateway, "grok_device_start_failed", err.Error())
		return
	}
	h.writeResource(w, http.StatusOK, "device", map[string]any{
		"device_code":               auth.DeviceCode,
		"user_code":                 auth.UserCode,
		"verification_uri":          auth.VerificationURI,
		"verification_uri_complete": auth.VerificationURIComplete,
		"interval":                  auth.Interval,
		"expires_at":                auth.ExpiresAt,
	})
}

func (h Handler) PollGrokDeviceLogin(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return
	}
	var payload grokDevicePollRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.DeviceCode) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_device_code", "device_code is required")
		return
	}
	tokens, err := h.oauth.PollGrokDeviceToken(r.Context(), siteID, payload.DeviceCode)
	if err != nil {
		switch {
		case errors.Is(err, oauthsvc.ErrGrokAuthorizationPending):
			h.writeResource(w, http.StatusOK, "device", map[string]any{"status": "pending"})
		case errors.Is(err, oauthsvc.ErrGrokSlowDown):
			h.writeResource(w, http.StatusOK, "device", map[string]any{"status": "slow_down"})
		default:
			h.writeError(w, r, http.StatusBadRequest, "grok_device_poll_failed", err.Error())
		}
		return
	}
	account, err := h.sites.CompleteGrokDeviceLogin(r.Context(), siteID, tokens, payload.CredentialID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "grok_account_create_failed", err.Error())
		return
	}
	if refreshed, refreshErr := h.sites.RefreshGrokAccount(r.Context(), siteID, account.CredentialID); refreshErr == nil {
		account = refreshed
	}
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "device", map[string]any{"status": "complete", "account": account})
}

func grokCredentialIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	value := strings.TrimSpace(chi.URLParam(r, "credentialID"))
	id, err := uuid.Parse(value)
	if err != nil {
		http.Error(w, "invalid credential id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}
