package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/store"
)

func (h Handler) BootstrapStatus(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "auth service is not available")
		return
	}

	status, err := h.auth.BootstrapStatus(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "bootstrap_status_failed", "failed to load bootstrap status")
		return
	}

	h.writePayload(w, http.StatusOK, map[string]any{
		"initialized":  status.Initialized,
		"can_register": !status.Initialized,
		"admin_count":  status.AdminCount,
	})
}

func (h Handler) BootstrapRegister(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "auth service is not available")
		return
	}

	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
		Avatar   string `json:"avatar"`
	}

	if !h.decodeJSON(w, r, &payload) {
		return
	}

	result, err := h.auth.BootstrapAdmin(r.Context(), payload.Username, payload.Password, payload.Nickname, payload.Avatar, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		switch {
		case err == auth.ErrAlreadyInitialized:
			h.writeError(w, r, http.StatusConflict, "bootstrap_completed", "admin bootstrap has already been completed")
		case strings.Contains(err.Error(), "username is required"),
			strings.Contains(err.Error(), "password"):
			h.writeError(w, r, http.StatusBadRequest, "invalid_registration", err.Error())
		default:
			h.writeError(w, r, http.StatusInternalServerError, "bootstrap_register_failed", "failed to create initial admin")
		}
		return
	}

	h.logInfo("admin bootstrap completed", "admin_id", result.Admin.ID, "username", result.Admin.Username)
	h.recordAudit(r, auth.AdminActor{Type: "session", AdminID: result.Admin.ID, SessionID: result.SessionID}, "auth.bootstrap", "", "", true, "", nil)
	writeSessionCookie(w, r, result)
	h.writePayload(w, http.StatusCreated, sessionPayload(result))
}

func (h Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "auth service is not available")
		return
	}

	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}

	if !h.decodeJSON(w, r, &payload) {
		return
	}

	result, err := h.auth.Login(r.Context(), payload.Username, payload.Password, payload.TOTPCode, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		var lockErr *auth.LockoutError
		switch {
		case errors.As(err, &lockErr):
			retryAfter := int64(lockErr.RetryAfter/time.Second) + 1
			w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
			h.writeError(w, r, http.StatusTooManyRequests, "too_many_attempts", "too many failed login attempts, please try again later")
		case errors.Is(err, auth.ErrTOTPRequired):
			h.writeError(w, r, http.StatusUnauthorized, "totp_required", "TOTP code is required")
		case errors.Is(err, auth.ErrTOTPInvalid):
			h.writeError(w, r, http.StatusUnauthorized, "invalid_totp", "TOTP code is invalid")
		default:
			h.writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		}
		h.recordAudit(r, auth.AdminActor{Type: "system"}, "auth.login", "", "", false, "invalid_credentials", map[string]any{"username": payload.Username})
		return
	}

	h.logInfo("admin session created", "admin_id", result.Admin.ID, "username", result.Admin.Username)
	h.recordAudit(r, auth.AdminActor{Type: "session", AdminID: result.Admin.ID, SessionID: result.SessionID}, "auth.login", "", "", true, "", nil)
	writeSessionCookie(w, r, result)
	h.writePayload(w, http.StatusCreated, sessionPayload(result))
}

func (h Handler) CurrentSession(w http.ResponseWriter, r *http.Request) {
	actor, ok := auth.AdminActorFromContext(r.Context())
	if !ok || h.auth == nil || actor.AdminID == uuid.Nil {
		h.writeError(w, r, http.StatusUnauthorized, "unauthorized", "admin session is required")
		return
	}

	admin, err := h.auth.GetAdminByID(r.Context(), actor.AdminID)
	if err != nil {
		h.writeError(w, r, http.StatusUnauthorized, "unauthorized", "admin session is required")
		return
	}

	payload := map[string]any{"admin": adminPayload(admin), "auth_type": actor.Type}
	if adminSession, ok := auth.AdminSessionFromContext(r.Context()); ok {
		payload["expires_at"] = timePtrValue(adminSession.ExpiresAt)
		if token := auth.AdminSessionTokenFromRequest(r); token != "" {
			payload["csrf_token"] = h.auth.CSRFTokenForSession(token)
		}
	} else {
		payload["expires_at"] = nil
	}
	h.writePayload(w, http.StatusOK, payload)
}

func (h Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "auth service is not available")
		return
	}

	token := auth.AdminSessionTokenFromRequest(r)

	if err := h.auth.Logout(r.Context(), token); err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "logout_failed", "failed to delete session")
		return
	}

	h.logInfo("admin session deleted")
	h.recordAudit(r, currentAdminActor(r), "auth.logout", "", "", true, "", nil)
	http.SetCookie(w, &http.Cookie{
		Name:     "xlyra_admin_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   sessionCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	})

	w.WriteHeader(http.StatusNoContent)
}

func writeSessionCookie(w http.ResponseWriter, r *http.Request, result auth.LoginResult) {
	cookie := &http.Cookie{
		Name:     "xlyra_admin_session",
		Value:    result.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   sessionCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	}
	if result.ExpiresAt != nil {
		cookie.Expires = *result.ExpiresAt
	} else {
		cookie.Expires = time.Now().AddDate(20, 0, 0)
	}
	http.SetCookie(w, cookie)
}

func sessionPayload(result auth.LoginResult) map[string]any {
	return map[string]any{
		"expires_at": timePtrValue(result.ExpiresAt),
		"csrf_token": result.CSRFToken,
		"admin":      adminPayload(result.Admin),
	}
}

func adminPayload(admin store.Admin) map[string]any {
	return map[string]any{
		"id":            admin.ID.String(),
		"username":      admin.Username,
		"nickname":      admin.Nickname,
		"avatar":        admin.Avatar,
		"role":          admin.Role,
		"status":        admin.Status,
		"totp_enabled":  admin.TOTPEnabled,
		"last_login_at": nullTimeValue(admin.LastLoginAt),
		"created_at":    timeString(admin.CreatedAt),
		"updated_at":    timeString(admin.UpdatedAt),
	}
}

func currentAdminActor(r *http.Request) auth.AdminActor {
	actor, _ := auth.AdminActorFromContext(r.Context())
	return actor
}

func (h Handler) recordAudit(r *http.Request, actor auth.AdminActor, action string, resourceType string, resourceID string, success bool, errorCode string, metadata any) {
	if h.auth == nil {
		return
	}
	h.auth.RecordAudit(r.Context(), auth.AuditInput{
		Actor:        actor,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		RemoteAddr:   r.RemoteAddr,
		UserAgent:    r.UserAgent(),
		RequestID:    middleware.GetReqID(r.Context()),
		Success:      success,
		ErrorCode:    errorCode,
		Metadata:     metadata,
	})
}

func sessionCookieSecure(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r != nil && r.TLS != nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Ssl")), "on")
}
