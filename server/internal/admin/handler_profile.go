package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/store"
)

func (h Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	h.writePayload(w, http.StatusOK, map[string]any{"admin": adminPayload(admin)})
}

func (h Handler) UpdateProfileAccount(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	var payload struct {
		Username string `json:"username"`
		Nickname string `json:"nickname"`
		Avatar   string `json:"avatar"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	updated, err := h.auth.UpdateAdminProfile(r.Context(), admin.ID, payload.Username, payload.Nickname, payload.Avatar)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "profile_update_failed", err.Error())
		return
	}
	h.recordAudit(r, currentAdminActor(r), "profile.update", "admin", updated.ID.String(), true, "", nil)
	h.writePayload(w, http.StatusOK, map[string]any{"admin": adminPayload(updated)})
}

func (h Handler) UpdateProfilePassword(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	var payload struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	actor := currentAdminActor(r)
	updated, err := h.auth.ChangeAdminPassword(r.Context(), admin.ID, payload.CurrentPassword, payload.NewPassword, actor.SessionID)
	if err != nil {
		h.recordAudit(r, actor, "profile.password.update", "admin", admin.ID.String(), false, "password_update_failed", nil)
		h.writeError(w, r, http.StatusBadRequest, "password_update_failed", err.Error())
		return
	}
	h.recordAudit(r, actor, "profile.password.update", "admin", admin.ID.String(), true, "", nil)
	h.writePayload(w, http.StatusOK, map[string]any{"admin": adminPayload(updated)})
}

func (h Handler) ListProfileSessions(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	sessions, err := h.auth.ListAdminSessions(r.Context(), admin.ID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "sessions_list_failed", "failed to list sessions")
		return
	}
	currentActor := currentAdminActor(r)
	items := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, adminSessionPayload(session, session.ID == currentActor.SessionID))
	}
	h.writeItems(w, http.StatusOK, items, map[string]any{"count": len(items)})
}

func (h Handler) DeleteProfileSession(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_session_id", "session id must be a valid UUID")
		return
	}
	if err := h.auth.DeleteAdminSession(r.Context(), admin.ID, sessionID); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "session_delete_failed", err.Error())
		return
	}
	h.recordAudit(r, currentAdminActor(r), "profile.session.delete", "admin_session", sessionID.String(), true, "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) DeleteOtherProfileSessions(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	actor := currentAdminActor(r)
	if err := h.auth.DeleteOtherAdminSessions(r.Context(), admin.ID, actor.SessionID); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "sessions_delete_failed", "failed to delete sessions")
		return
	}
	h.recordAudit(r, actor, "profile.sessions.delete_others", "admin", admin.ID.String(), true, "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) GetProfileAccessToken(w http.ResponseWriter, r *http.Request) {
	item, err := h.auth.GetAdminAccessToken(r.Context())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || strings.Contains(err.Error(), "record not found") {
			h.writePayload(w, http.StatusOK, map[string]any{"access_token": nil})
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "access_token_get_failed", "failed to get access token")
		return
	}
	h.writePayload(w, http.StatusOK, map[string]any{"access_token": adminAccessTokenPayload(item, false)})
}

func (h Handler) CreateProfileAccessToken(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	result, err := h.auth.ReplaceAdminAccessToken(r.Context(), admin.ID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "access_token_create_failed", "failed to create access token")
		return
	}
	h.recordAudit(r, currentAdminActor(r), "profile.access_token.replace", "admin_access_token", result.AccessToken.ID.String(), true, "", nil)
	payload := adminAccessTokenPayload(result.AccessToken, true)
	payload["token"] = result.Token
	h.writePayload(w, http.StatusCreated, map[string]any{"access_token": payload})
}

func (h Handler) UpdateProfileAccessTokenEnabled(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	item, err := h.auth.SetAdminAccessTokenEnabled(r.Context(), payload.Enabled)
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "access_token_update_failed", err.Error())
		return
	}
	h.recordAudit(r, currentAdminActor(r), "profile.access_token.enabled", "admin_access_token", item.ID.String(), true, "", map[string]any{"enabled": payload.Enabled})
	h.writePayload(w, http.StatusOK, map[string]any{"access_token": adminAccessTokenPayload(item, false)})
}

func (h Handler) DeleteProfileAccessToken(w http.ResponseWriter, r *http.Request) {
	if err := h.auth.DeleteAdminAccessToken(r.Context()); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "access_token_delete_failed", "failed to delete access token")
		return
	}
	h.recordAudit(r, currentAdminActor(r), "profile.access_token.delete", "admin_access_token", "", true, "", nil)
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) SetupProfileTOTP(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	result, err := h.auth.SetupTOTP(r.Context(), admin.ID)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "totp_setup_failed", "failed to setup TOTP")
		return
	}
	h.recordAudit(r, currentAdminActor(r), "profile.totp.setup", "admin", admin.ID.String(), true, "", nil)
	h.writePayload(w, http.StatusOK, map[string]any{
		"secret":      result.Secret,
		"otpauth_url": result.OtpauthURL,
		"admin":       adminPayload(result.Admin),
	})
}

func (h Handler) EnableProfileTOTP(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	var payload struct {
		Code string `json:"code"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	updated, err := h.auth.EnableTOTP(r.Context(), admin.ID, payload.Code)
	if err != nil {
		h.recordAudit(r, currentAdminActor(r), "profile.totp.enable", "admin", admin.ID.String(), false, "invalid_totp", nil)
		h.writeError(w, r, http.StatusBadRequest, "invalid_totp", err.Error())
		return
	}
	h.recordAudit(r, currentAdminActor(r), "profile.totp.enable", "admin", admin.ID.String(), true, "", nil)
	h.writePayload(w, http.StatusOK, map[string]any{"admin": adminPayload(updated)})
}

func (h Handler) DisableProfileTOTP(w http.ResponseWriter, r *http.Request) {
	admin, ok := h.currentAdmin(w, r)
	if !ok {
		return
	}
	var payload struct {
		CurrentPassword string `json:"current_password"`
		Code            string `json:"code"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	updated, err := h.auth.DisableTOTP(r.Context(), admin.ID, payload.CurrentPassword, payload.Code)
	if err != nil {
		h.recordAudit(r, currentAdminActor(r), "profile.totp.disable", "admin", admin.ID.String(), false, "totp_disable_failed", nil)
		h.writeError(w, r, http.StatusBadRequest, "totp_disable_failed", err.Error())
		return
	}
	h.recordAudit(r, currentAdminActor(r), "profile.totp.disable", "admin", admin.ID.String(), true, "", nil)
	h.writePayload(w, http.StatusOK, map[string]any{"admin": adminPayload(updated)})
}

func (h Handler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	filters, ok := h.auditLogFiltersFromRequest(w, r)
	if !ok {
		return
	}
	items, total, err := h.auth.ListAuditLogs(r.Context(), filters)
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "audit_logs_list_failed", "failed to list audit logs")
		return
	}
	payloadItems := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payloadItems = append(payloadItems, auditLogPayload(item))
	}
	h.writeItems(w, http.StatusOK, payloadItems, map[string]any{
		"count":     len(payloadItems),
		"total":     total,
		"page":      filters.Page,
		"page_size": filters.PageSize,
	})
}

func (h Handler) currentAdmin(w http.ResponseWriter, r *http.Request) (store.Admin, bool) {
	actor, ok := auth.AdminActorFromContext(r.Context())
	if !ok || actor.AdminID == uuid.Nil || h.auth == nil {
		h.writeError(w, r, http.StatusUnauthorized, "unauthorized", "admin session is required")
		return store.Admin{}, false
	}
	admin, err := h.auth.GetAdminByID(r.Context(), actor.AdminID)
	if err != nil {
		h.writeError(w, r, http.StatusUnauthorized, "unauthorized", "admin session is required")
		return store.Admin{}, false
	}
	return admin, true
}

func adminSessionPayload(session store.AdminSession, current bool) map[string]any {
	return map[string]any{
		"id":           session.ID.String(),
		"admin_id":     session.AdminID.String(),
		"current":      current,
		"expires_at":   timePtrValue(session.ExpiresAt),
		"last_seen_at": nullTimeValue(session.LastSeenAt),
		"ip_address":   ipAddressValue(session.IPAddress),
		"user_agent":   session.UserAgent,
		"created_at":   timeString(session.CreatedAt),
		"updated_at":   timeString(session.UpdatedAt),
	}
}

func adminAccessTokenPayload(item store.AdminAccessToken, includeOnce bool) map[string]any {
	return map[string]any{
		"id":                   item.ID.String(),
		"admin_id":             item.AdminID.String(),
		"masked_token":         item.MaskedToken,
		"enabled":              item.Enabled,
		"last_used_at":         nullTimeValue(item.LastUsedAt),
		"last_used_ip":         ipAddressValue(item.LastUsedIP),
		"last_used_user_agent": item.LastUsedUserAgent,
		"token_returned_once":  includeOnce,
		"created_at":           timeString(item.CreatedAt),
		"updated_at":           timeString(item.UpdatedAt),
	}
}

func auditLogPayload(item store.AdminAuditLog) map[string]any {
	return map[string]any{
		"id":               item.ID.String(),
		"actor_type":       item.ActorType,
		"admin_id":         nullUUIDValue(item.AdminID),
		"admin_session_id": nullUUIDValue(item.AdminSessionID),
		"access_token_id":  nullUUIDValue(item.AccessTokenID),
		"action":           item.Action,
		"resource_type":    item.ResourceType,
		"resource_id":      item.ResourceID,
		"ip_address":       ipAddressValue(item.IPAddress),
		"user_agent":       item.UserAgent,
		"request_id":       item.RequestID,
		"success":          item.Success,
		"error_code":       item.ErrorCode,
		"metadata":         jsonRaw(item.Metadata),
		"created_at":       timeString(item.CreatedAt),
	}
}

func (h Handler) auditLogFiltersFromRequest(w http.ResponseWriter, r *http.Request) (store.AdminAuditLogFilters, bool) {
	query := r.URL.Query()
	page, _ := strconv.Atoi(query.Get("page"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(query.Get("page_size"))
	if pageSize < 1 {
		pageSize = 50
	}
	filters := store.AdminAuditLogFilters{
		Action:    strings.TrimSpace(query.Get("action")),
		ActorType: strings.TrimSpace(query.Get("actor_type")),
		Page:      page,
		PageSize:  pageSize,
	}
	if raw := strings.TrimSpace(query.Get("success")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_success", "success must be a boolean")
			return filters, false
		}
		filters.Success = &value
	}
	if raw := strings.TrimSpace(query.Get("date_from")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_date_from", "date_from must be RFC3339")
			return filters, false
		}
		filters.DateFrom = &value
	}
	if raw := strings.TrimSpace(query.Get("date_to")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			h.writeError(w, r, http.StatusBadRequest, "invalid_date_to", "date_to must be RFC3339")
			return filters, false
		}
		filters.DateTo = &value
	}
	return filters, true
}

func ipAddressValue(value store.IPAddress) any {
	if value == nil {
		return nil
	}
	return value.NetIP().String()
}
