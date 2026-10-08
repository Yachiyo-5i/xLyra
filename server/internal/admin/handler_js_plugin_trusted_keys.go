package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"xlyra/server/internal/httpx"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func (h Handler) ListJSPluginTrustedKeys(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	rows, err := h.jsPlugins.ListTrustedKeys(r.Context())
	if err != nil {
		h.logError("list js plugin trusted keys failed", "error", err)
		h.writeError(w, r, http.StatusInternalServerError, "js_plugin_trusted_keys_failed", "failed to list trusted keys")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, jsPluginTrustedKeyPayload(row))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h Handler) CreateJSPluginTrustedKey(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	var body struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if !h.decodeJSON(w, r, &body) {
		return
	}
	adminID := adminIDFromContext(r.Context())
	row, err := h.jsPlugins.AddTrustedKey(r.Context(), adminID, body.Name, body.PublicKey)
	if err != nil {
		status, code, message := http.StatusInternalServerError, "js_plugin_trusted_key_create_failed", "failed to add trusted key"
		var invalid *jsplugin.InvalidTrustedKeyError
		switch {
		case errors.As(err, &invalid):
			status, code, message = http.StatusBadRequest, "js_plugin_trusted_key_invalid", invalid.Error()
		case errors.Is(err, jsplugin.ErrTrustedKeyExists):
			status, code, message = http.StatusConflict, "js_plugin_trusted_key_exists", err.Error()
		default:
			h.logError("create js plugin trusted key failed", "error", err)
		}
		h.recordJSPluginAudit(r, "js_plugin.trusted_key.create", "", false, code, map[string]any{"name": strings.TrimSpace(body.Name)})
		h.writeError(w, r, status, code, message)
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.trusted_key.create", row.ID.String(), true, "", map[string]any{"name": row.Name, "fingerprint": row.Fingerprint})
	httpx.JSON(w, http.StatusOK, jsPluginTrustedKeyPayload(row))
}

func (h Handler) DeleteJSPluginTrustedKey(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.jsPlugins.DeleteTrustedKey(r.Context(), id); err != nil {
		status, code, message := http.StatusInternalServerError, "js_plugin_trusted_key_delete_failed", "failed to delete trusted key"
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status, code, message = http.StatusNotFound, "not_found", "trusted key not found"
		} else {
			h.logError("delete js plugin trusted key failed", "error", err)
		}
		h.recordJSPluginAudit(r, "js_plugin.trusted_key.delete", id, false, code, nil)
		h.writeError(w, r, status, code, message)
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.trusted_key.delete", id, true, "", nil)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func jsPluginTrustedKeyPayload(row store.JSPluginTrustedKey) map[string]any {
	return map[string]any{
		"id":          row.ID.String(),
		"name":        row.Name,
		"public_key":  row.PublicKey,
		"fingerprint": row.Fingerprint,
		"created_at":  row.CreatedAt,
	}
}
