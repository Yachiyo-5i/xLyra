package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	sitepkg "xlyra/server/internal/site"
)

func (h Handler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	items, count, err := provider.List(r.Context(), siteID)
	if err != nil {
		h.writeAccountCallError(w, r, err, http.StatusBadRequest, "accounts_list_failed")
		return
	}
	h.writeItems(w, http.StatusOK, jsonSlice(items), map[string]any{"count": count})
}

func (h Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	credentialID, ok := grokCredentialIDParam(w, r)
	if !ok {
		return
	}
	var payload struct {
		Enabled *bool `json:"enabled"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	account, err := provider.Update(r.Context(), siteID, credentialID, payload.Enabled)
	if err != nil {
		h.writeAccountCallError(w, r, err, http.StatusBadRequest, "account_update_failed")
		return
	}
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "account", account)
}

func (h Handler) UpdateAccountModel(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	credentialID, ok := grokCredentialIDParam(w, r)
	if !ok {
		return
	}
	var payload struct {
		Model         string   `json:"model"`
		SiteModelID   string   `json:"site_model_id"`
		Enabled       *bool    `json:"enabled"`
		EndpointMode  string   `json:"endpoint_mode"`
		EndpointTypes []string `json:"endpoint_types"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	var siteModelID uuid.UUID
	if strings.TrimSpace(payload.SiteModelID) != "" {
		parsed, err := uuid.Parse(payload.SiteModelID)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_site_model_id", "site_model_id must be a valid UUID")
			return
		}
		siteModelID = parsed
	}
	var account any
	if strings.TrimSpace(payload.Model) != "" && payload.Enabled != nil {
		updated, err := provider.SetModelEnabled(r.Context(), siteID, credentialID, payload.Model, *payload.Enabled)
		if err != nil {
			h.writeAccountCallError(w, r, err, http.StatusBadRequest, "account_model_update_failed")
			return
		}
		account = updated
	}
	if siteModelID != uuid.Nil && (payload.EndpointMode != "" || payload.EndpointTypes != nil) {
		updated, err := provider.SetModelEndpoint(r.Context(), siteID, credentialID, siteModelID, payload.EndpointMode, payload.EndpointTypes)
		if err != nil {
			h.writeAccountCallError(w, r, err, http.StatusBadRequest, "account_model_endpoint_update_failed")
			return
		}
		account = updated
	}
	if account == nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_model_update", "model with enabled, or site_model_id with endpoint_mode/endpoint_types is required")
		return
	}
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "account", account)
}

func (h Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	credentialID, ok := grokCredentialIDParam(w, r)
	if !ok {
		return
	}
	if err := provider.Delete(r.Context(), siteID, credentialID); err != nil {
		h.writeAccountCallError(w, r, err, http.StatusBadRequest, "account_delete_failed")
		return
	}
	h.invalidateGatewayModelsCache()
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) RefreshAccount(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	credentialID, ok := grokCredentialIDParam(w, r)
	if !ok {
		return
	}
	account, err := provider.Refresh(r.Context(), siteID, credentialID)
	if err != nil {
		h.writeAccountCallError(w, r, err, http.StatusBadRequest, "account_refresh_failed")
		return
	}
	h.invalidateGatewayModelsCache()
	h.writeResource(w, http.StatusOK, "account", account)
}

func (h Handler) RefreshAccounts(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	items, err := provider.RefreshAll(r.Context(), siteID)
	if err != nil {
		h.writeAccountCallError(w, r, err, http.StatusBadRequest, "accounts_refresh_failed")
		return
	}
	failed := 0
	for _, item := range items {
		if item.Error != "" {
			failed++
		}
	}
	h.invalidateGatewayModelsCache()
	h.writeItems(w, http.StatusOK, items, map[string]any{"count": len(items), "failed": failed})
}

func (h Handler) StartAccountLogin(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	var payload sitepkg.AccountLoginParams
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && !errors.Is(err, io.EOF) {
			h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
			return
		}
	}
	login, err := provider.StartLogin(r.Context(), siteID, payload)
	if err != nil {
		h.writeAccountCallError(w, r, err, http.StatusBadGateway, "account_login_start_failed")
		return
	}
	h.writeResource(w, http.StatusOK, "login", login)
}

func (h Handler) PollAccountLogin(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	var payload struct {
		Session      string     `json:"session"`
		CredentialID *uuid.UUID `json:"credential_id"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.Session) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_login_session", "session is required")
		return
	}
	login, err := provider.PollLogin(r.Context(), siteID, payload.Session, payload.CredentialID)
	if err != nil {
		h.writeAccountCallError(w, r, err, http.StatusBadRequest, "account_login_failed")
		return
	}
	if login.Status == "complete" {
		h.invalidateGatewayModelsCache()
	}
	h.writeResource(w, http.StatusOK, "login", login)
}

func (h Handler) ImportAccounts(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	var payload struct {
		Files []sitepkg.AccountImportFile `json:"files"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	items, err := provider.Import(r.Context(), siteID, payload.Files)
	if err != nil {
		h.writeAccountCallError(w, r, err, http.StatusBadRequest, "account_import_failed")
		return
	}
	body, meta := accountImportResponse(items)
	h.invalidateGatewayModelsCache()
	h.writeItems(w, http.StatusOK, body, meta)
}

func (h Handler) ListAccountRegions(w http.ResponseWriter, r *http.Request) {
	siteID, provider, ok := h.accountProvider(w, r)
	if !ok {
		return
	}
	items, defaultID, err := provider.Regions(r.Context(), siteID)
	if err != nil {
		h.writeAccountCallError(w, r, err, http.StatusBadRequest, "account_regions_failed")
		return
	}
	h.writeItems(w, http.StatusOK, jsonSlice(items), map[string]any{
		"count":   sliceLen(items),
		"default": defaultID,
	})
}

func (h Handler) accountProvider(w http.ResponseWriter, r *http.Request) (uuid.UUID, sitepkg.AccountProvider, bool) {
	if h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "site_service_unavailable", "site service is not available")
		return uuid.Nil, nil, false
	}
	siteID, ok := siteIDParam(w, r)
	if !ok {
		return uuid.Nil, nil, false
	}
	item, err := h.sites.Get(r.Context(), siteID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), "record not found") {
			h.writeError(w, r, http.StatusNotFound, "site_not_found", "site was not found")
			return uuid.Nil, nil, false
		}
		h.writeAccountCallError(w, r, err, http.StatusBadRequest, "site_lookup_failed")
		return uuid.Nil, nil, false
	}
	provider, ok := h.sites.AccountProviderFor(item.SiteType)
	if !ok {
		if sitepkg.PluginProviderUnavailable(item.SiteType) {
			h.writeError(w, r, http.StatusConflict, "provider_unavailable", "provider plugin is not enabled")
			return uuid.Nil, nil, false
		}
		h.writeError(w, r, http.StatusNotFound, "accounts_not_supported", "site type does not support accounts")
		return uuid.Nil, nil, false
	}
	return siteID, provider, true
}

func (h Handler) writeAccountCallError(w http.ResponseWriter, r *http.Request, err error, status int, code string) {
	switch {
	case errors.Is(err, sitepkg.ErrOAuthUnavailable):
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
	case errors.Is(err, sitepkg.ErrAccountImportUnsupported):
		h.writeError(w, r, http.StatusNotFound, "account_import_unsupported", err.Error())
	default:
		h.writeError(w, r, status, code, err.Error())
	}
}

func accountImportResponse(items any) (any, map[string]any) {
	switch typed := items.(type) {
	case []sitepkg.PluginAccountImportItem:
		if typed == nil {
			typed = []sitepkg.PluginAccountImportItem{}
		}
		return typed, accountImportMeta(importStatuses(typed, func(item sitepkg.PluginAccountImportItem) string { return item.Status }))
	default:
		body := jsonSlice(items)
		return body, map[string]any{"total": sliceLen(body), "imported": 0, "failed": 0}
	}
}

func accountImportMeta(imported, failed, total int) map[string]any {
	return map[string]any{"total": total, "imported": imported, "failed": failed}
}

func importStatuses[T any](items []T, status func(T) string) (imported, failed, total int) {
	total = len(items)
	for _, item := range items {
		if status(item) == "imported" {
			imported++
			continue
		}
		if status(item) != "" {
			failed++
		}
	}
	return imported, failed, total
}

func jsonSlice(items any) any {
	value := reflect.ValueOf(items)
	if !value.IsValid() || (value.Kind() == reflect.Slice && value.IsNil()) {
		return []any{}
	}
	return items
}

func sliceLen(items any) int {
	value := reflect.ValueOf(items)
	if !value.IsValid() {
		return 0
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Array:
		return value.Len()
	default:
		return 0
	}
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
