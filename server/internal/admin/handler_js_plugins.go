package admin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func (h Handler) ListBuiltinJSPlugins(w http.ResponseWriter, r *http.Request) {
	registry, err := jsplugin.LoadBuiltins()
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "js_plugin_builtins_failed", err.Error())
		return
	}
	items := make([]map[string]any, 0, len(registry.Plugins()))
	for _, plugin := range registry.Plugins() {
		items = append(items, jsPluginBuiltinPayload(plugin))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h Handler) ListJSPlugins(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil || h.trafficDB == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	repo := store.NewJSPluginRepository(h.trafficDB.DB())
	plugins, err := repo.ListPlugins(r.Context())
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "js_plugin_list_failed", "failed to list plugins")
		return
	}
	items := make([]map[string]any, 0, len(plugins))
	for _, plugin := range plugins {
		versions, _ := repo.ListVersions(r.Context(), plugin.ID)
		enabled := ""
		for _, row := range versions {
			if row.Status == store.JSPluginStatusEnabled {
				enabled = row.Version
				break
			}
		}
		items = append(items, map[string]any{
			"id":              plugin.ID,
			"source":          plugin.Source,
			"enabled_version": enabled,
			"version_count":   len(versions),
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h Handler) UploadJSPlugin(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	adminID := adminIDFromContext(r.Context())
	raw, err := io.ReadAll(io.LimitReader(r.Body, jsplugin.MaxPackageBytes+1))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_body", "failed to read upload body")
		return
	}
	if len(raw) > jsplugin.MaxPackageBytes {
		h.writeError(w, r, http.StatusBadRequest, "package_too_large", "package exceeds size limit")
		return
	}
	row, err := h.jsPlugins.Upload(r.Context(), adminID, raw)
	if err != nil {
		h.recordJSPluginAudit(r, "js_plugin.upload", "", false, "js_plugin_upload_failed", nil)
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_upload_failed", err.Error())
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.upload", row.PluginID, true, "", jsPluginAuditMeta(row.PluginID, row.Version, row.PackageSHA256))
	httpx.JSON(w, http.StatusOK, jsPluginVersionPayload(row))
}

func (h Handler) GetJSPlugin(w http.ResponseWriter, r *http.Request) {
	if h.trafficDB == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "database is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	repo := store.NewJSPluginRepository(h.trafficDB.DB())
	versions, err := repo.ListVersions(r.Context(), pluginID)
	if err != nil || len(versions) == 0 {
		h.writeError(w, r, http.StatusNotFound, "not_found", "plugin not found")
		return
	}
	items := make([]map[string]any, 0, len(versions))
	for _, row := range versions {
		items = append(items, jsPluginVersionPayload(row))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": pluginID, "versions": items})
}

func (h Handler) EnableJSPluginVersion(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	version := strings.TrimSpace(chi.URLParam(r, "version"))
	if err := h.jsPlugins.Enable(r.Context(), pluginID, version); err != nil {
		h.recordJSPluginAudit(r, "js_plugin.enable", pluginID, false, "js_plugin_enable_failed", map[string]any{"version": version})
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_enable_failed", err.Error())
		return
	}
	gen := h.jsPlugins.Catalog().Generation()
	h.recordJSPluginAudit(r, "js_plugin.enable", pluginID, true, "", map[string]any{"version": version, "generation": gen})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "generation": gen})
}

func (h Handler) DisableJSPlugin(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	if err := h.jsPlugins.Disable(r.Context(), pluginID); err != nil {
		h.recordJSPluginAudit(r, "js_plugin.disable", pluginID, false, "js_plugin_disable_failed", nil)
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_disable_failed", err.Error())
		return
	}
	gen := h.jsPlugins.Catalog().Generation()
	h.recordJSPluginAudit(r, "js_plugin.disable", pluginID, true, "", map[string]any{"generation": gen})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "generation": gen})
}

func (h Handler) DeleteJSPluginVersion(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	version := strings.TrimSpace(chi.URLParam(r, "version"))
	meta := map[string]any{"version": version}
	if err := h.jsPlugins.DeleteVersion(r.Context(), pluginID, version); err != nil {
		code := "js_plugin_delete_failed"
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, jsplugin.ErrVersionEnabled):
			code = "js_plugin_version_enabled"
		case errors.Is(err, gorm.ErrRecordNotFound):
			code = "not_found"
			status = http.StatusNotFound
		}
		h.recordJSPluginAudit(r, "js_plugin.delete", pluginID, false, code, meta)
		h.writeError(w, r, status, code, err.Error())
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.delete", pluginID, true, "", meta)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h Handler) BindJSPluginProtocolSlug(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	version := strings.TrimSpace(chi.URLParam(r, "version"))
	var body struct {
		Slug string `json:"slug"`
	}
	if err := httpx.DecodeJSONBody(r, &body); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	slug := strings.TrimSpace(body.Slug)
	if err := h.jsPlugins.BindProtocolSlug(r.Context(), pluginID, version, slug); err != nil {
		h.recordJSPluginAudit(r, "js_plugin.bind_protocol", pluginID, false, "js_plugin_bind_failed", map[string]any{"version": version, "slug": slug})
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_bind_failed", err.Error())
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.bind_protocol", pluginID, true, "", map[string]any{"version": version, "slug": slug})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func jsPluginBuiltinPayload(plugin *jsplugin.Plugin) map[string]any {
	if plugin == nil {
		return map[string]any{}
	}
	manifest := plugin.Manifest
	payload := map[string]any{
		"id":          manifest.ID,
		"name":        manifest.Name,
		"description": manifest.Description,
		"version":     manifest.Version,
		"kind":        manifest.Kind,
		"source":      "builtin",
	}
	switch manifest.Kind {
	case jsplugin.KindQuotaProbe:
		if replaces := plugin.ProbeType(); replaces != "" {
			payload["replaces"] = replaces
		}
	case jsplugin.KindProtocol:
		if name := plugin.ProtocolName(); name != "" {
			payload["protocol"] = name
		}
	}
	return payload
}

func jsPluginVersionPayload(row store.JSPluginVersion) map[string]any {
	return map[string]any{
		"plugin_id":      row.PluginID,
		"version":        row.Version,
		"status":         row.Status,
		"package_sha256": row.PackageSHA256,
		"manifest":       row.Manifest,
		"selftest":       row.SelfTest,
		"created_at":     row.CreatedAt,
	}
}

func jsPluginAuditMeta(pluginID, version, sha string) map[string]any {
	meta := map[string]any{"version": version}
	if sha != "" {
		meta["package_sha256"] = sha
	}
	if pluginID != "" {
		meta["plugin_id"] = pluginID
	}
	return meta
}

func (h Handler) recordJSPluginAudit(r *http.Request, action, pluginID string, success bool, errorCode string, metadata any) {
	h.recordAudit(r, currentAdminActor(r), action, "js_plugin", pluginID, success, errorCode, metadata)
}

func adminIDFromContext(ctx context.Context) string {
	id, ok := auth.AdminIDFromContext(ctx)
	if !ok {
		return ""
	}
	return id.String()
}
