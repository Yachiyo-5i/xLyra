package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/downloads"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

type oauthCallbackProxyIDContextKey struct{}

func (h Handler) StartCodexOAuth(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}

	var payload oauthAuthorizeRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	sitePayload, err := oauthPendingSitePayload(payload)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_oauth_site", err.Error())
		return
	}
	publicBaseURL := strings.TrimSpace(payload.PublicBaseURL)
	if publicBaseURL == "" {
		publicBaseURL = requestPublicBaseURL(r)
	}
	result, err := h.oauth.StartCodexFlow(r.Context(), oauthsvc.StartCodexFlowParams{
		PublicBaseURL:      publicBaseURL,
		SuccessRedirectURL: payload.RedirectURL,
		FailureRedirectURL: payload.FailureRedirectURL,
		Site:               sitePayload,
		Metadata:           payload.Metadata,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_authorize_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusCreated, map[string]any{
		"provider":         "codex",
		"session_id":       result.SessionID.String(),
		"state":            result.State,
		"authorize_url":    result.AuthorizeURL,
		"callback_url":     result.CallbackURL,
		"callback_port":    result.CallbackPort,
		"relay_target_url": result.RelayTargetURL,
		"expires_at":       timeString(result.ExpiresAt),
	})
}

func (h Handler) StartAntigravityOAuth(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}

	var payload oauthAuthorizeRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	sitePayload, err := oauthPendingSitePayload(payload)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_oauth_site", err.Error())
		return
	}
	publicBaseURL := strings.TrimSpace(payload.PublicBaseURL)
	if publicBaseURL == "" {
		publicBaseURL = requestPublicBaseURL(r)
	}
	result, err := h.oauth.StartAntigravityFlow(r.Context(), oauthsvc.StartAntigravityFlowParams{
		PublicBaseURL:      publicBaseURL,
		SuccessRedirectURL: payload.RedirectURL,
		FailureRedirectURL: payload.FailureRedirectURL,
		Site:               sitePayload,
		Metadata:           payload.Metadata,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_authorize_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusCreated, map[string]any{
		"provider":         "antigravity",
		"session_id":       result.SessionID.String(),
		"state":            result.State,
		"authorize_url":    result.AuthorizeURL,
		"callback_url":     result.CallbackURL,
		"callback_port":    result.CallbackPort,
		"relay_target_url": result.RelayTargetURL,
		"expires_at":       timeString(result.ExpiresAt),
	})
}

func (h Handler) StartClaudeCodeOAuth(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}

	var payload oauthAuthorizeRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	sitePayload, err := oauthPendingSitePayload(payload)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_oauth_site", err.Error())
		return
	}
	result, err := h.oauth.StartClaudeCodeFlow(r.Context(), oauthsvc.StartClaudeCodeFlowParams{
		SuccessRedirectURL: payload.RedirectURL,
		FailureRedirectURL: payload.FailureRedirectURL,
		Site:               sitePayload,
		Metadata:           payload.Metadata,
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_authorize_failed", err.Error())
		return
	}

	h.writePayload(w, http.StatusCreated, map[string]any{
		"provider":      "claude_code",
		"session_id":    result.SessionID.String(),
		"state":         result.State,
		"authorize_url": result.AuthorizeURL,
		"callback_url":  result.CallbackURL,
		"expires_at":    timeString(result.ExpiresAt),
	})
}

func (h Handler) CodexOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_oauth_state", "state is required")
		return
	}
	if rawErr := strings.TrimSpace(r.URL.Query().Get("error")); rawErr != "" {
		// Redirect only to the trusted target captured on the session at authorize
		// time; never to an attacker-supplied redirect_url query parameter.
		if session, sessErr := h.oauth.SessionByState(r.Context(), state); sessErr == nil {
			target := oauthRedirectTarget(session, map[string]string{
				"status":  "error",
				"message": rawErr,
				"state":   state,
			})
			if target != "" {
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
		}
		h.writeError(w, r, http.StatusBadRequest, "oauth_callback_failed", rawErr)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	session, connection, pendingSite, err := h.oauth.HandleCodexCallback(r.Context(), state, code)
	if err != nil {
		target := oauthRedirectTarget(session, map[string]string{
			"status":  "error",
			"message": err.Error(),
			"state":   state,
		})
		if target != "" {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		h.writeError(w, r, http.StatusBadRequest, "oauth_callback_failed", err.Error())
		return
	}
	applyOAuthCallbackProxyOverride(r, &pendingSite)

	siteItem, err := h.oauthUpsertCodexSite(r, pendingSite, connection)
	if err != nil {
		target := oauthRedirectTarget(session, map[string]string{
			"status":  "error",
			"message": err.Error(),
			"state":   state,
		})
		if target != "" {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		h.writeError(w, r, http.StatusBadRequest, "oauth_site_bind_failed", err.Error())
		return
	}

	refreshResult, refreshErr := h.sites.RefreshState(r.Context(), siteItem.ID)
	if refreshErr != nil {
		_ = h.oauth.MarkConnectionUnavailable(r.Context(), connection.ID, refreshErr.Error())
	} else {
		h.invalidateGatewayModelsCache()
	}
	target := oauthRedirectTarget(session, map[string]string{
		"status":        successOrPartial(refreshErr),
		"provider":      "codex",
		"connection_id": connection.ID.String(),
		"site_id":       siteItem.ID.String(),
	})
	if refreshErr != nil {
		target = appendQuery(target, map[string]string{"message": refreshErr.Error()})
	}
	if target != "" {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	if refreshErr != nil {
		h.writePayload(w, http.StatusOK, map[string]any{
			"ok":            false,
			"message":       refreshErr.Error(),
			"provider":      "codex",
			"site":          h.sitePayloadWithState(siteItem, refreshResult.State),
			"connection_id": connection.ID.String(),
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(codexSuccessPage()))
}

func (h Handler) AntigravityOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_oauth_state", "state is required")
		return
	}
	if rawErr := strings.TrimSpace(r.URL.Query().Get("error")); rawErr != "" {
		// Redirect only to the trusted target captured on the session at authorize
		// time; never to an attacker-supplied redirect_url query parameter.
		if session, sessErr := h.oauth.SessionByState(r.Context(), state); sessErr == nil {
			target := oauthRedirectTarget(session, map[string]string{
				"status":  "error",
				"message": rawErr,
				"state":   state,
			})
			if target != "" {
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
		}
		h.writeError(w, r, http.StatusBadRequest, "oauth_callback_failed", rawErr)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	session, connection, pendingSite, err := h.oauth.HandleAntigravityCallback(r.Context(), state, code)
	if err != nil {
		target := oauthRedirectTarget(session, map[string]string{
			"status":  "error",
			"message": err.Error(),
			"state":   state,
		})
		if target != "" {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		h.writeError(w, r, http.StatusBadRequest, "oauth_callback_failed", err.Error())
		return
	}
	applyOAuthCallbackProxyOverride(r, &pendingSite)

	siteItem, err := h.oauthUpsertAntigravitySite(r, pendingSite, connection)
	if err != nil {
		target := oauthRedirectTarget(session, map[string]string{
			"status":  "error",
			"message": err.Error(),
			"state":   state,
		})
		if target != "" {
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		h.writeError(w, r, http.StatusBadRequest, "oauth_site_bind_failed", err.Error())
		return
	}

	refreshResult, refreshErr := h.sites.RefreshState(r.Context(), siteItem.ID)
	if refreshErr != nil {
		_ = h.oauth.MarkConnectionUnavailable(r.Context(), connection.ID, refreshErr.Error())
	} else {
		h.invalidateGatewayModelsCache()
	}
	target := oauthRedirectTarget(session, map[string]string{
		"status":        successOrPartial(refreshErr),
		"provider":      "antigravity",
		"connection_id": connection.ID.String(),
		"site_id":       siteItem.ID.String(),
	})
	if refreshErr != nil {
		target = appendQuery(target, map[string]string{"message": refreshErr.Error()})
	}
	if target != "" {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	if refreshErr != nil {
		h.writePayload(w, http.StatusOK, map[string]any{
			"ok":            false,
			"message":       refreshErr.Error(),
			"provider":      "antigravity",
			"site":          h.sitePayloadWithState(siteItem, refreshResult.State),
			"connection_id": connection.ID.String(),
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(antigravitySuccessPage()))
}

func (h Handler) CompleteClaudeCodeOAuth(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}

	var payload struct {
		SessionID           string  `json:"session_id"`
		AuthorizationResult string  `json:"authorization_result"`
		ProxyID             *string `json:"proxy_id"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	sessionID, err := uuid.Parse(strings.TrimSpace(payload.SessionID))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_oauth_session", "session_id must be a valid UUID")
		return
	}
	session, connection, pendingSite, err := h.oauth.HandleClaudeCodeAuthorizationResult(r.Context(), sessionID, payload.AuthorizationResult, payload.ProxyID)
	if err != nil {
		target := oauthRedirectTarget(session, map[string]string{
			"status":  "error",
			"message": err.Error(),
		})
		if target != "" {
			h.writePayload(w, http.StatusBadRequest, map[string]any{"ok": false, "provider": "claude_code", "status": "error", "message": err.Error(), "redirect_url": target})
			return
		}
		h.writeError(w, r, http.StatusBadRequest, "oauth_callback_failed", err.Error())
		return
	}
	if payload.ProxyID != nil {
		proxyID := strings.TrimSpace(*payload.ProxyID)
		pendingSite.ProxyID = &proxyID
	}

	siteItem, err := h.oauthUpsertClaudeCodeSite(r, pendingSite, connection)
	if err != nil {
		target := oauthRedirectTarget(session, map[string]string{
			"status":  "error",
			"message": err.Error(),
		})
		if target != "" {
			h.writePayload(w, http.StatusBadRequest, map[string]any{"ok": false, "provider": "claude_code", "status": "error", "message": err.Error(), "redirect_url": target})
			return
		}
		h.writeError(w, r, http.StatusBadRequest, "oauth_site_bind_failed", err.Error())
		return
	}

	refreshResult, refreshErr := h.sites.RefreshState(r.Context(), siteItem.ID)
	if refreshErr != nil {
		_ = h.oauth.MarkConnectionUnavailable(r.Context(), connection.ID, refreshErr.Error())
	} else {
		h.invalidateGatewayModelsCache()
	}
	target := oauthRedirectTarget(session, map[string]string{
		"status":        successOrPartial(refreshErr),
		"provider":      "claude_code",
		"connection_id": connection.ID.String(),
		"site_id":       siteItem.ID.String(),
	})
	if refreshErr != nil {
		target = appendQuery(target, map[string]string{"message": refreshErr.Error()})
	}

	h.writePayload(w, http.StatusOK, map[string]any{
		"ok":            refreshErr == nil,
		"provider":      "claude_code",
		"status":        successOrPartial(refreshErr),
		"message":       errorMessage(refreshErr),
		"connection_id": connection.ID.String(),
		"site_id":       siteItem.ID.String(),
		"site":          h.sitePayloadWithState(siteItem, refreshResult.State),
		"redirect_url":  target,
	})
}

func (h Handler) CompleteOAuthCallbackURL(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}

	provider := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	if provider != "codex" && provider != "antigravity" {
		h.writeError(w, r, http.StatusBadRequest, "unsupported_oauth_provider", "provider must be codex or antigravity")
		return
	}

	var payload struct {
		CallbackURL string  `json:"callback_url"`
		ProxyID     *string `json:"proxy_id"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}

	callbackURL, err := url.Parse(strings.TrimSpace(payload.CallbackURL))
	if err != nil || callbackURL == nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_callback_url", "callback_url must be a valid URL")
		return
	}
	query := callbackURL.Query()
	if strings.TrimSpace(query.Get("state")) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_oauth_state", "state is required")
		return
	}
	if strings.TrimSpace(query.Get("code")) == "" && strings.TrimSpace(query.Get("error")) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_callback_url", "callback_url must contain code or error")
		return
	}

	proxyReq := r.Clone(r.Context())
	if payload.ProxyID != nil {
		proxyReq = proxyReq.WithContext(context.WithValue(proxyReq.Context(), oauthCallbackProxyIDContextKey{}, strings.TrimSpace(*payload.ProxyID)))
	}
	proxyReq.Method = http.MethodGet
	proxyReq.Body = nil
	proxyReq.URL = &url.URL{
		Path:     "/api/v1/oauth/providers/" + provider + "/callback",
		RawQuery: callbackURL.RawQuery,
	}

	recorder := httptest.NewRecorder()
	switch provider {
	case "codex":
		h.CodexOAuthCallback(recorder, proxyReq)
	case "antigravity":
		h.AntigravityOAuthCallback(recorder, proxyReq)
	}

	location := strings.TrimSpace(recorder.Header().Get("Location"))
	if location != "" {
		result := oauthCallbackURLResult(location)
		if result["status"] == "error" {
			h.writeError(w, r, http.StatusBadRequest, "oauth_callback_failed", oauthStringValue(result["message"]))
			return
		}
		result["ok"] = true
		result["provider"] = provider
		result["redirect_url"] = location
		h.writePayload(w, http.StatusOK, result)
		return
	}

	if recorder.Code >= http.StatusBadRequest {
		message := strings.TrimSpace(recorder.Body.String())
		if message == "" {
			message = "OAuth callback failed"
		}
		h.writeError(w, r, http.StatusBadRequest, "oauth_callback_failed", message)
		return
	}

	h.writePayload(w, http.StatusOK, map[string]any{
		"ok":       true,
		"provider": provider,
		"status":   "success",
	})
}

func applyOAuthCallbackProxyOverride(r *http.Request, pending *oauthsvc.PendingSite) {
	if pending == nil {
		return
	}
	proxyID, ok := r.Context().Value(oauthCallbackProxyIDContextKey{}).(string)
	if !ok {
		return
	}
	proxyID = strings.TrimSpace(proxyID)
	pending.ProxyID = &proxyID
}

func (h Handler) ListOAuthConnections(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}
	items, err := h.oauth.ListConnections(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "oauth_connections_list_failed", "failed to list oauth connections")
		return
	}
	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if quotaError := oauthConnectionMetadataAuthError(item.Provider, item.Metadata); quotaError != "" && strings.EqualFold(item.Status, "connected") {
			_ = h.oauth.MarkConnectionUnavailable(r.Context(), item.ID, quotaError)
			item.Status = "reconnect_required"
		}
		payloadItems = append(payloadItems, h.oauthConnectionPayload(r, item))
	}
	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{"count": len(payloadItems)})
}

func (h Handler) GetOAuthConnection(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}
	connectionID, ok := connectionIDParam(w, r)
	if !ok {
		return
	}
	details, err := h.oauth.ConnectionByID(r.Context(), connectionID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "oauth_connection_not_found", "oauth connection was not found")
		return
	}
	h.writePayload(w, http.StatusOK, map[string]any{
		"connection": h.oauthConnectionDetailsPayload(r, details, nil),
	})
}

func (h Handler) RefreshOAuthConnection(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}
	connectionID, ok := connectionIDParam(w, r)
	if !ok {
		return
	}
	details, err := h.oauth.RefreshCodexConnection(r.Context(), connectionID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_connection_refresh_failed", err.Error())
		return
	}
	var refreshPayload any = nil
	if details.Connection.SiteID != nil && *details.Connection.SiteID != uuid.Nil {
		result, refreshErr := h.sites.RefreshState(r.Context(), *details.Connection.SiteID)
		if refreshErr != nil {
			_ = h.oauth.MarkConnectionUnavailable(r.Context(), connectionID, refreshErr.Error())
			if refreshed, err := h.oauth.ConnectionByID(r.Context(), connectionID); err == nil {
				details = refreshed
			}
		} else {
			h.invalidateGatewayModelsCache()
		}
		refreshPayload = map[string]any{
			"ok":      refreshErr == nil,
			"message": errorMessage(refreshErr),
			"site":    h.sitePayloadWithState(result.Site, result.State),
		}
	}
	resetCredits := h.fetchCodexResetCreditsIfApplicable(r.Context(), details, connectionID)
	h.writePayload(w, http.StatusOK, map[string]any{
		"connection": h.oauthConnectionDetailsPayload(r, details, resetCredits),
		"refresh":    refreshPayload,
	})
}

func (h Handler) ListOAuthConnectionResetCredits(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}
	connectionID, ok := connectionIDParam(w, r)
	if !ok {
		return
	}
	credits, err := h.oauth.ListCodexRateLimitResetCredits(r.Context(), connectionID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_reset_credits_list_failed", err.Error())
		return
	}
	h.writePayload(w, http.StatusOK, map[string]any{
		"credits":            credits.Credits,
		"available_count":    credits.AvailableCount,
		"total_earned_count": credits.TotalEarnedCount,
	})
}

func (h Handler) ConsumeOAuthConnectionResetCredit(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}
	connectionID, ok := connectionIDParam(w, r)
	if !ok {
		return
	}
	var payload oauthResetCreditConsumeRequest
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if strings.TrimSpace(payload.IdempotencyKey) == "" {
		h.writeError(w, r, http.StatusBadRequest, "invalid_idempotency_key", "idempotency_key is required")
		return
	}
	result, details, err := h.oauth.ConsumeCodexRateLimitResetCredit(r.Context(), connectionID, payload.IdempotencyKey, payload.CreditID)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_reset_credit_consume_failed", err.Error())
		return
	}
	resetCredits := h.fetchCodexResetCreditsIfApplicable(r.Context(), details, connectionID)
	h.writePayload(w, http.StatusOK, map[string]any{
		"result":     result,
		"connection": h.oauthConnectionDetailsPayload(r, details, resetCredits),
	})
}

func (h Handler) ExportOAuthConnection(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.downloads == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_export_unavailable", "oauth export service is not available")
		return
	}
	connectionID, ok := connectionIDParam(w, r)
	if !ok {
		return
	}
	connection, err := h.oauth.ConnectionRecordByID(r.Context(), connectionID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "oauth_connection_not_found", "oauth connection was not found")
		return
	}
	if strings.TrimSpace(connection.Provider) != "codex" {
		h.writeError(w, r, http.StatusBadRequest, "oauth_export_unsupported_provider", "only codex oauth connections can be exported")
		return
	}
	filename := codexOAuthExportFilename(connection)
	ticket, err := h.downloads.Register(r.Context(), downloads.RegisterInput{
		Filename:    filename,
		ContentType: "application/json",
		Scope:       "oauth.codex.export",
		Generate: func(ctx context.Context) ([]byte, error) {
			details, err := h.oauth.ConnectionByID(ctx, connectionID)
			if err != nil {
				return nil, err
			}
			payload := map[string]any{
				"auth_mode":      "chatgpt",
				"OPENAI_API_KEY": nil,
				"tokens": map[string]any{
					"id_token":      details.IDToken,
					"access_token":  details.AccessToken,
					"refresh_token": details.RefreshToken,
					"account_id":    details.AccountID,
				},
				"last_refresh": codexOAuthExportLastRefresh(details),
			}
			return json.MarshalIndent(payload, "", "  ")
		},
	})
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_export_register_failed", err.Error())
		return
	}
	h.writePayload(w, http.StatusCreated, map[string]any{"download": ticket})
}

func (h Handler) UpdateOAuthConnectionModel(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}
	connectionID, ok := connectionIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		SiteModelID string `json:"site_model_id"`
		Model       string `json:"model"`
		Enabled     *bool  `json:"enabled"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if payload.Enabled == nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_enabled", "enabled is required")
		return
	}

	connection, err := h.oauth.ConnectionRecordByID(r.Context(), connectionID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "oauth_connection_not_found", "oauth connection was not found")
		return
	}
	if connection.SiteID == nil || *connection.SiteID == uuid.Nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_connection_unbound", "oauth connection is not bound to a site")
		return
	}

	modelID, err := h.oauthConnectionSiteModelID(r.Context(), *connection.SiteID, payload.SiteModelID, payload.Model)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_model_not_found", err.Error())
		return
	}
	model, err := h.sites.SetModelEnabled(r.Context(), *connection.SiteID, modelID, *payload.Enabled)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_model_update_failed", err.Error())
		return
	}

	response := map[string]any{
		"model": siteModelPayload(model),
	}
	if details, err := h.oauth.ConnectionByID(r.Context(), connectionID); err == nil {
		response["connection"] = h.oauthConnectionDetailsPayload(r, details, nil)
	}
	h.invalidateGatewayModelsCache()
	h.writePayload(w, http.StatusOK, response)
}

func (h Handler) UpdateOAuthConnectionModelsStatus(w http.ResponseWriter, r *http.Request) {
	if h.oauth == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "oauth_service_unavailable", "oauth service is not available")
		return
	}
	connectionID, ok := connectionIDParam(w, r)
	if !ok {
		return
	}

	var payload struct {
		SiteModelIDs []string `json:"site_model_ids"`
		Enabled      *bool    `json:"enabled"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if payload.Enabled == nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_enabled", "enabled is required")
		return
	}
	modelIDs, ok := parseUUIDList(w, r, payload.SiteModelIDs, "invalid_site_model_id", "site_model_id")
	if !ok {
		return
	}
	if len(modelIDs) == 0 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_site_model_ids", "site_model_ids is required")
		return
	}

	connection, err := h.oauth.ConnectionRecordByID(r.Context(), connectionID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "oauth_connection_not_found", "oauth connection was not found")
		return
	}
	if connection.SiteID == nil || *connection.SiteID == uuid.Nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_connection_unbound", "oauth connection is not bound to a site")
		return
	}

	models, err := h.sites.SetModelsEnabled(r.Context(), *connection.SiteID, modelIDs, *payload.Enabled)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "oauth_models_update_failed", err.Error())
		return
	}

	response := map[string]any{
		"items": siteModelPayloads(models),
		"meta":  map[string]any{"count": len(models)},
	}
	if details, err := h.oauth.ConnectionByID(r.Context(), connectionID); err == nil {
		response["connection"] = h.oauthConnectionDetailsPayload(r, details, nil)
	}
	h.invalidateGatewayModelsCache()
	h.writePayload(w, http.StatusOK, response)
}

func oauthPendingSitePayload(payload oauthAuthorizeRequest) (oauthsvc.PendingSite, error) {
	if payload.Site == nil {
		return oauthsvc.PendingSite{}, fmt.Errorf("site is required")
	}
	enabled := true
	if payload.Site.Enabled != nil {
		enabled = *payload.Site.Enabled
	}
	result := oauthsvc.PendingSite{
		Name:            strings.TrimSpace(payload.Site.Name),
		Slug:            strings.TrimSpace(payload.Site.Slug),
		BaseURL:         strings.TrimSpace(payload.Site.BaseURL),
		Enabled:         enabled,
		RoutingPriority: payload.Site.RoutingPriority,
		ProxyID:         payload.Site.ProxyID,
	}
	if payload.Site.SiteID != "" {
		siteID, err := uuid.Parse(payload.Site.SiteID)
		if err != nil {
			return oauthsvc.PendingSite{}, fmt.Errorf("site.site_id must be a valid UUID")
		}
		result.SiteID = siteID
	}
	if result.SiteID == uuid.Nil && (result.Name == "" || result.Slug == "") {
		return oauthsvc.PendingSite{}, fmt.Errorf("site.name and site.slug are required when creating a new oauth site")
	}
	if payload.Site.Gateway != nil {
		encoded, err := json.Marshal(payload.Site.Gateway.toSiteGatewayConfig())
		if err != nil {
			return oauthsvc.PendingSite{}, fmt.Errorf("marshal gateway config: %w", err)
		}
		result.GatewayConfig = encoded
	}
	return result, nil
}

func (h Handler) oauthUpsertCodexSite(r *http.Request, pending oauthsvc.PendingSite, connection store.OAuthConnection) (store.Site, error) {
	desiredName, err := oauthsvc.CodexSiteNameFromEmail(connection.Email)
	if err != nil {
		return store.Site{}, err
	}
	if pending.SiteID != uuid.Nil {
		item, err := h.sites.Get(r.Context(), pending.SiteID)
		if err != nil {
			return store.Site{}, err
		}
		if strings.TrimSpace(item.SiteType) != "codex" {
			return store.Site{}, fmt.Errorf("existing site must have site_type \"codex\" to bind a codex oauth connection")
		}
		if err := h.oauth.BindConnectionSite(r.Context(), connection.ID, item.ID); err != nil {
			return store.Site{}, err
		}
		updated, err := h.sites.AttachOAuthConnection(r.Context(), item.ID, "codex", connection.ID, oauthSiteMeta(connection))
		if err != nil {
			return store.Site{}, err
		}
		if shouldAutoRenameCodexSite(updated.Name, desiredName) {
			renamed, _, err := h.sites.Update(r.Context(), site.UpdateSiteParams{
				ID:              updated.ID,
				Name:            desiredName,
				Slug:            updated.Slug,
				SiteType:        updated.SiteType,
				BaseURL:         updated.BaseURL,
				Enabled:         updated.Enabled,
				RoutingPriority: &updated.RoutingPriority,
				ProxyID:         pending.ProxyID,
			})
			if err != nil {
				return store.Site{}, err
			}
			updated = renamed
		}
		return updated, nil
	}
	if existing, ok, err := h.oauthExistingSite(r.Context(), "codex", connection); err != nil {
		return store.Site{}, err
	} else if ok {
		return h.attachOAuthSite(r, existing, "codex", pending, connection, desiredName, shouldAutoRenameCodexSite)
	}

	gatewayConfig := (*site.GatewayConfig)(nil)
	if len(pending.GatewayConfig) > 0 {
		var cfg site.GatewayConfig
		if err := json.Unmarshal(pending.GatewayConfig, &cfg); err == nil {
			gatewayConfig = &cfg
		}
	}
	baseURL := strings.TrimSpace(pending.BaseURL)
	if baseURL == "" {
		baseURL = "https://chatgpt.com/backend-api"
	}
	siteName := desiredName
	if siteName == "" {
		siteName = pending.Name
	}
	created, _, err := h.sites.Create(r.Context(), site.CreateSiteParams{
		Name:            siteName,
		Slug:            pending.Slug,
		SiteType:        "codex",
		BaseURL:         baseURL,
		Enabled:         pending.Enabled,
		RoutingPriority: pending.RoutingPriority,
		GatewayConfig:   gatewayConfig,
		ProxyID:         pending.ProxyID,
	})
	if err != nil {
		return store.Site{}, err
	}
	if err := h.oauth.BindConnectionSite(r.Context(), connection.ID, created.ID); err != nil {
		return store.Site{}, err
	}
	return h.sites.AttachOAuthConnection(r.Context(), created.ID, "codex", connection.ID, oauthSiteMeta(connection))
}

func (h Handler) oauthUpsertAntigravitySite(r *http.Request, pending oauthsvc.PendingSite, connection store.OAuthConnection) (store.Site, error) {
	desiredName, err := oauthsvc.SiteNameFromEmail("Antigravity", connection.Email)
	if err != nil {
		return store.Site{}, err
	}
	if pending.SiteID != uuid.Nil {
		item, err := h.sites.Get(r.Context(), pending.SiteID)
		if err != nil {
			return store.Site{}, err
		}
		if strings.TrimSpace(item.SiteType) != "antigravity" {
			return store.Site{}, fmt.Errorf("existing site must have site_type \"antigravity\" to bind an antigravity oauth connection")
		}
		if err := h.oauth.BindConnectionSite(r.Context(), connection.ID, item.ID); err != nil {
			return store.Site{}, err
		}
		updated, err := h.sites.AttachOAuthConnection(r.Context(), item.ID, "antigravity", connection.ID, oauthSiteMeta(connection))
		if err != nil {
			return store.Site{}, err
		}
		if shouldAutoRenameOAuthSite(updated.Name, desiredName, "antigravity", "antigravity oauth") {
			renamed, _, err := h.sites.Update(r.Context(), site.UpdateSiteParams{
				ID:              updated.ID,
				Name:            desiredName,
				Slug:            updated.Slug,
				SiteType:        updated.SiteType,
				BaseURL:         updated.BaseURL,
				Enabled:         updated.Enabled,
				RoutingPriority: &updated.RoutingPriority,
				ProxyID:         pending.ProxyID,
			})
			if err != nil {
				return store.Site{}, err
			}
			updated = renamed
		}
		return updated, nil
	}
	if existing, ok, err := h.oauthExistingSite(r.Context(), "antigravity", connection); err != nil {
		return store.Site{}, err
	} else if ok {
		return h.attachOAuthSite(r, existing, "antigravity", pending, connection, desiredName, func(currentName string, desiredName string) bool {
			return shouldAutoRenameOAuthSite(currentName, desiredName, "antigravity", "antigravity oauth")
		})
	}

	gatewayConfig := (*site.GatewayConfig)(nil)
	if len(pending.GatewayConfig) > 0 {
		var cfg site.GatewayConfig
		if err := json.Unmarshal(pending.GatewayConfig, &cfg); err == nil {
			gatewayConfig = &cfg
		}
	}
	baseURL := strings.TrimSpace(pending.BaseURL)
	if baseURL == "" {
		baseURL = "https://daily-cloudcode-pa.sandbox.googleapis.com"
	}
	siteName := desiredName
	if siteName == "" {
		siteName = pending.Name
	}
	created, _, err := h.sites.Create(r.Context(), site.CreateSiteParams{
		Name:            siteName,
		Slug:            pending.Slug,
		SiteType:        "antigravity",
		BaseURL:         baseURL,
		Enabled:         pending.Enabled,
		RoutingPriority: pending.RoutingPriority,
		GatewayConfig:   gatewayConfig,
		ProxyID:         pending.ProxyID,
	})
	if err != nil {
		return store.Site{}, err
	}
	if err := h.oauth.BindConnectionSite(r.Context(), connection.ID, created.ID); err != nil {
		return store.Site{}, err
	}
	return h.sites.AttachOAuthConnection(r.Context(), created.ID, "antigravity", connection.ID, oauthSiteMeta(connection))
}

func (h Handler) oauthUpsertClaudeCodeSite(r *http.Request, pending oauthsvc.PendingSite, connection store.OAuthConnection) (store.Site, error) {
	desiredName, err := oauthsvc.SiteNameFromEmail("Claude", connection.Email)
	if err != nil {
		return store.Site{}, err
	}
	if pending.SiteID != uuid.Nil {
		item, err := h.sites.Get(r.Context(), pending.SiteID)
		if err != nil {
			return store.Site{}, err
		}
		if strings.TrimSpace(item.SiteType) != "claude_code" {
			return store.Site{}, fmt.Errorf("existing site must have site_type \"claude_code\" to bind a claude code oauth connection")
		}
		if err := h.oauth.BindConnectionSite(r.Context(), connection.ID, item.ID); err != nil {
			return store.Site{}, err
		}
		updated, err := h.sites.AttachOAuthConnection(r.Context(), item.ID, "claude_code", connection.ID, oauthSiteMeta(connection))
		if err != nil {
			return store.Site{}, err
		}
		if shouldAutoRenameOAuthSite(updated.Name, desiredName, "claude", "claude code", "claude code oauth") {
			renamed, _, err := h.sites.Update(r.Context(), site.UpdateSiteParams{
				ID:              updated.ID,
				Name:            desiredName,
				Slug:            updated.Slug,
				SiteType:        updated.SiteType,
				BaseURL:         updated.BaseURL,
				Enabled:         updated.Enabled,
				RoutingPriority: &updated.RoutingPriority,
				ProxyID:         pending.ProxyID,
			})
			if err != nil {
				return store.Site{}, err
			}
			updated = renamed
		}
		return updated, nil
	}
	if existing, ok, err := h.oauthExistingSite(r.Context(), "claude_code", connection); err != nil {
		return store.Site{}, err
	} else if ok {
		return h.attachOAuthSite(r, existing, "claude_code", pending, connection, desiredName, func(currentName string, desiredName string) bool {
			return shouldAutoRenameOAuthSite(currentName, desiredName, "claude", "claude code", "claude code oauth")
		})
	}

	gatewayConfig := (*site.GatewayConfig)(nil)
	if len(pending.GatewayConfig) > 0 {
		var cfg site.GatewayConfig
		if err := json.Unmarshal(pending.GatewayConfig, &cfg); err == nil {
			gatewayConfig = &cfg
		}
	}
	baseURL := strings.TrimSpace(pending.BaseURL)
	siteName := desiredName
	if siteName == "" {
		siteName = pending.Name
	}
	created, _, err := h.sites.Create(r.Context(), site.CreateSiteParams{
		Name:            siteName,
		Slug:            pending.Slug,
		SiteType:        "claude_code",
		BaseURL:         baseURL,
		Enabled:         pending.Enabled,
		RoutingPriority: pending.RoutingPriority,
		GatewayConfig:   gatewayConfig,
		ProxyID:         pending.ProxyID,
	})
	if err != nil {
		return store.Site{}, err
	}
	if err := h.oauth.BindConnectionSite(r.Context(), connection.ID, created.ID); err != nil {
		return store.Site{}, err
	}
	return h.sites.AttachOAuthConnection(r.Context(), created.ID, "claude_code", connection.ID, oauthSiteMeta(connection))
}

func (h Handler) oauthExistingSite(ctx context.Context, provider string, connection store.OAuthConnection) (store.Site, bool, error) {
	if connection.SiteID != nil && *connection.SiteID != uuid.Nil {
		item, err := h.sites.Get(ctx, *connection.SiteID)
		if err != nil {
			return store.Site{}, false, err
		}
		if strings.TrimSpace(item.SiteType) != provider {
			return store.Site{}, false, fmt.Errorf("existing oauth site must have site_type %q", provider)
		}
		return item, true, nil
	}
	item, err := store.NewSiteRepository(h.oauth.DB().DB()).FindOAuthSite(ctx, provider, strings.TrimSpace(connection.AccountID), strings.TrimSpace(connection.Email))
	if err != nil {
		if store.IsRecordNotFound(err) {
			return store.Site{}, false, nil
		}
		return store.Site{}, false, err
	}
	return item, true, nil
}

func (h Handler) attachOAuthSite(r *http.Request, item store.Site, provider string, pending oauthsvc.PendingSite, connection store.OAuthConnection, desiredName string, shouldRename func(string, string) bool) (store.Site, error) {
	if err := h.oauth.BindConnectionSite(r.Context(), connection.ID, item.ID); err != nil {
		return store.Site{}, err
	}
	updated, err := h.sites.AttachOAuthConnection(r.Context(), item.ID, provider, connection.ID, oauthSiteMeta(connection))
	if err != nil {
		return store.Site{}, err
	}
	if shouldRename(updated.Name, desiredName) {
		renamed, _, err := h.sites.Update(r.Context(), site.UpdateSiteParams{
			ID:              updated.ID,
			Name:            desiredName,
			Slug:            updated.Slug,
			SiteType:        updated.SiteType,
			BaseURL:         updated.BaseURL,
			Enabled:         updated.Enabled,
			RoutingPriority: &updated.RoutingPriority,
			ProxyID:         pending.ProxyID,
		})
		if err != nil {
			return store.Site{}, err
		}
		updated = renamed
	}
	return updated, nil
}

func oauthSiteMeta(connection store.OAuthConnection) map[string]any {
	meta := map[string]any{
		"oauth_account_id": connection.AccountID,
		"oauth_email":      connection.Email,
	}
	connectionMeta := map[string]any{}
	if len(connection.Metadata) > 0 {
		_ = json.Unmarshal(connection.Metadata, &connectionMeta)
	}
	if planType, _ := connectionMeta["plan_type"].(string); strings.TrimSpace(planType) != "" {
		meta["oauth_plan_type"] = strings.TrimSpace(planType)
	}
	if tier, _ := connectionMeta["subscription_tier"].(string); strings.TrimSpace(tier) != "" {
		meta["oauth_plan_type"] = strings.TrimSpace(tier)
		meta["oauth_subscription_tier"] = strings.TrimSpace(tier)
	}
	if projectID, _ := connectionMeta["project_id"].(string); strings.TrimSpace(projectID) != "" {
		meta["oauth_project_id"] = strings.TrimSpace(projectID)
	}
	return meta
}

func codexOAuthExportFilename(connection store.OAuthConnection) string {
	email := strings.TrimSpace(connection.Email)
	if email != "" {
		local := strings.Split(email, "@")[0]
		if local != "" {
			return "codex-" + local + ".json"
		}
	}
	accountID := strings.TrimSpace(connection.AccountID)
	if len(accountID) > 8 {
		accountID = accountID[:8]
	}
	if accountID == "" {
		accountID = connection.ID.String()[:8]
	}
	return "codex-" + accountID + ".json"
}

func codexOAuthExportLastRefresh(details oauthsvc.CodexConnection) string {
	if details.Connection.LastRefreshAt.Valid {
		return details.Connection.LastRefreshAt.Time.UTC().Format(time.RFC3339Nano)
	}
	if value := oauthStringValue(details.Metadata["last_refresh"]); value != "" {
		return value
	}
	return ""
}

func shouldAutoRenameCodexSite(currentName string, desiredName string) bool {
	return shouldAutoRenameOAuthSite(currentName, desiredName, "codex", "codex oauth")
}

func shouldAutoRenameOAuthSite(currentName string, desiredName string, defaults ...string) bool {
	currentName = strings.TrimSpace(currentName)
	desiredName = strings.TrimSpace(desiredName)
	if desiredName == "" || currentName == "" || strings.EqualFold(currentName, desiredName) {
		return false
	}
	normalized := strings.ToLower(currentName)
	for _, value := range defaults {
		if normalized == strings.ToLower(strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func oauthRedirectTarget(session store.OAuthSession, values map[string]string) string {
	target := strings.TrimSpace(session.SuccessRedirectURL)
	if target == "" {
		target = strings.TrimSpace(session.FailureRedirectURL)
	}
	return appendQuery(target, values)
}

func appendQuery(target string, values map[string]string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return ""
	}
	query := parsed.Query()
	for key, value := range values {
		if strings.TrimSpace(value) != "" {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func requestPublicBaseURL(r *http.Request) string {
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

func successOrPartial(err error) string {
	if err == nil {
		return "success"
	}
	return "partial"
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func codexSuccessPage() string {
	return `<html><head><meta charset="utf-8"><title>Codex OAuth complete</title></head><body><h1>Codex OAuth complete</h1><p>You can close this window.</p></body></html>`
}

func antigravitySuccessPage() string {
	return `<html><head><meta charset="utf-8"><title>Antigravity OAuth complete</title></head><body><h1>Antigravity OAuth complete</h1><p>You can close this window.</p></body></html>`
}

func (h Handler) oauthConnectionPayload(r *http.Request, connection store.OAuthConnection) map[string]any {
	payload := map[string]any{
		"id":              connection.ID.String(),
		"provider":        connection.Provider,
		"status":          connection.Status,
		"account_id":      emptyStringAsNil(connection.AccountID),
		"email":           emptyStringAsNil(connection.Email),
		"site_id":         nil,
		"expires_at":      nullTimeValue(connection.ExpiresAt),
		"last_refresh_at": nullTimeValue(connection.LastRefreshAt),
		"last_sync_at":    nullTimeValue(connection.LastSyncAt),
		"created_at":      timeString(connection.CreatedAt),
		"updated_at":      timeString(connection.UpdatedAt),
	}
	if connection.SiteID != nil {
		payload["site_id"] = connection.SiteID.String()
		if h.sites != nil {
			if item, err := h.sites.Get(r.Context(), *connection.SiteID); err == nil {
				sitePayload := h.sitePayloadWithStats(r, item)
				if h.usage != nil {
					if row, ok, err := h.usage.UsageSummaryForSite(r.Context(), item.ID, nil); err == nil && ok {
						sitePayload["usage"] = siteUsagePayload(row)
					}
				}
				payload["site"] = sitePayload
			}
		}
	}
	meta := map[string]any{}
	if len(connection.Metadata) > 0 {
		_ = json.Unmarshal(connection.Metadata, &meta)
	}
	if len(meta) > 0 {
		payload["meta"] = meta
		if lastError, ok := meta["last_error"].(string); ok && strings.TrimSpace(lastError) != "" {
			payload["last_error"] = strings.TrimSpace(lastError)
		}
		if lastErrorAt, ok := meta["last_error_at"].(string); ok && strings.TrimSpace(lastErrorAt) != "" {
			payload["last_error_at"] = strings.TrimSpace(lastErrorAt)
		}
		if refreshable, ok := meta["refreshable"].(bool); ok {
			payload["refreshable"] = refreshable
		}
		if tokenMode, ok := meta["token_mode"].(string); ok && strings.TrimSpace(tokenMode) != "" {
			payload["token_mode"] = strings.TrimSpace(tokenMode)
		}
		if warning, ok := meta["refresh_warning"].(string); ok && strings.TrimSpace(warning) != "" {
			payload["refresh_warning"] = strings.TrimSpace(warning)
		}
		if quotaError := oauthQuotaAuthError(connection.Provider, meta); quotaError != "" {
			if strings.TrimSpace(oauthStringValue(payload["last_error"])) == "" {
				payload["last_error"] = quotaError
			}
			if strings.EqualFold(connection.Status, "connected") {
				payload["status"] = "reconnect_required"
			}
		}
	}
	return payload
}

func oauthQuotaAuthError(provider string, meta map[string]any) string {
	quota, ok := meta["quota"].(map[string]any)
	if !ok || len(quota) == 0 {
		return ""
	}
	if available, ok := quota["available"].(bool); ok && available {
		return ""
	}
	message := firstNonEmptyOAuthString(
		oauthStringValue(quota["error"]),
		oauthStringValue(quota["message"]),
		oauthStringValue(quota["detail"]),
	)
	if message == "" || !site.OAuthPermanentAuthRefreshError(provider, message) {
		return ""
	}
	return message
}

func oauthConnectionMetadataAuthError(provider string, metadata store.JSON) string {
	meta := map[string]any{}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &meta)
	}
	if len(meta) == 0 {
		return ""
	}
	return oauthQuotaAuthError(provider, meta)
}

func firstNonEmptyOAuthString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (h Handler) fetchCodexResetCreditsIfApplicable(ctx context.Context, details oauthsvc.CodexConnection, connectionID uuid.UUID) *adapter.CodexRateLimitResetCredits {
	if h.oauth == nil || details.Connection.Provider != "codex" {
		return nil
	}
	credits, err := h.oauth.ListCodexRateLimitResetCredits(ctx, connectionID)
	if err != nil {
		return nil
	}
	return &credits
}

func (h Handler) oauthConnectionDetailsPayload(r *http.Request, details oauthsvc.CodexConnection, resetCredits *adapter.CodexRateLimitResetCredits) map[string]any {
	payload := h.oauthConnectionPayload(r, details.Connection)
	payload["plan_type"] = emptyStringAsNil(details.PlanType)
	payload["quota"] = details.Quota
	payload["claims"] = details.Claims
	payload["models"] = h.oauthConnectionModelPayloads(r, details)
	if resetCredits != nil {
		payload["reset_credits"] = map[string]any{
			"credits":            resetCredits.Credits,
			"available_count":    resetCredits.AvailableCount,
			"total_earned_count": resetCredits.TotalEarnedCount,
		}
	}
	return payload
}

func (h Handler) oauthConnectionModelPayloads(r *http.Request, details oauthsvc.CodexConnection) []map[string]any {
	if h.sites != nil && details.Connection.SiteID != nil && *details.Connection.SiteID != uuid.Nil {
		if models, err := h.sites.ListModels(r.Context(), *details.Connection.SiteID); err == nil && len(models) > 0 {
			items := make([]map[string]any, 0, len(models))
			for _, model := range models {
				items = append(items, oauthSiteModelPayload(model))
			}
			return items
		}
	}

	items := make([]map[string]any, 0, len(details.Models))
	for _, item := range details.Models {
		model := map[string]any{}
		for key, value := range item {
			model[key] = value
		}
		if _, ok := model["enabled"]; !ok {
			status := strings.TrimSpace(oauthStringValue(model["status"]))
			model["enabled"] = status == "" || status == "active"
		}
		items = append(items, model)
	}
	return items
}

func oauthSiteModelPayload(model store.SiteModel) map[string]any {
	payload := map[string]any{
		"id":                  model.UpstreamName,
		"name":                model.UpstreamName,
		"display_name":        model.DisplayName,
		"display":             model.DisplayName,
		"site_model_id":       model.ID.String(),
		"upstream_model_name": model.UpstreamName,
		"status":              model.Status,
		"enabled":             model.Status == "active",
	}
	capabilities := map[string]any{}
	if len(model.Capabilities) > 0 {
		_ = json.Unmarshal(model.Capabilities, &capabilities)
	}
	if len(capabilities) > 0 {
		payload["capabilities"] = capabilities
		if quota, ok := capabilities["quota"].(map[string]any); ok {
			payload["quota"] = quota
		}
	}
	return payload
}

func (h Handler) oauthConnectionSiteModelID(ctx context.Context, siteID uuid.UUID, rawSiteModelID string, rawModel string) (uuid.UUID, error) {
	if value := strings.TrimSpace(rawSiteModelID); value != "" {
		modelID, err := uuid.Parse(value)
		if err != nil {
			return uuid.Nil, fmt.Errorf("site_model_id must be a valid UUID")
		}
		return modelID, nil
	}

	modelName := strings.TrimSpace(rawModel)
	if modelName == "" {
		return uuid.Nil, fmt.Errorf("site_model_id or model is required")
	}
	models, err := h.sites.ListModels(ctx, siteID)
	if err != nil {
		return uuid.Nil, err
	}
	for _, model := range models {
		if model.UpstreamName == modelName || model.DisplayName == modelName || model.ID.String() == modelName {
			return model.ID, nil
		}
	}
	return uuid.Nil, fmt.Errorf("model %q was not found for this oauth site", modelName)
}

func oauthStringValue(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func oauthCallbackURLResult(raw string) map[string]any {
	result := map[string]any{
		"status": "success",
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return result
	}
	query := parsed.Query()
	for _, key := range []string{"status", "message", "state", "connection_id", "site_id", "provider"} {
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			result[key] = value
		}
	}
	return result
}
