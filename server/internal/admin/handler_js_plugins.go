package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/auth"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/jsplugin"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

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
		var enabledRow store.JSPluginVersion
		for _, row := range versions {
			if row.Status == store.JSPluginStatusEnabled {
				enabled = row.Version
				enabledRow = row
				break
			}
		}
		if enabled == "" && len(versions) > 0 {
			enabledRow = versions[0]
		}
		item := map[string]any{
			"id":              plugin.ID,
			"source":          plugin.Source,
			"enabled_version": enabled,
			"version_count":   len(versions),
		}
		if meta := jsPluginManifestSummary(enabledRow.Manifest); meta != nil {
			for key, value := range meta {
				item[key] = value
			}
			if kind, ok := meta["kind"].(string); ok {
				// Where the kind takes effect, so the admin UI need not hard-code kinds.
				item["scope"] = string(jsplugin.ScopeOf(kind))
			}
		}
		if enabled != "" && h.jsPlugins != nil {
			metrics := h.jsPlugins.Metrics(plugin.ID, enabled)
			item["metrics_24h"] = map[string]any{
				"calls":       metrics.Calls,
				"errors":      metrics.Errors,
				"error_rate":  metrics.ErrorRate,
				"window_ends": metrics.WindowEnds,
			}
		}
		items = append(items, item)
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
		h.recordJSPluginAudit(r, "js_plugin.upload", "", false, "request_body_too_large", nil)
		h.writeError(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "package exceeds size limit")
		return
	}
	row, err := h.jsPlugins.Upload(r.Context(), adminID, raw)
	if err != nil {
		status, code, message := http.StatusInternalServerError, "js_plugin_upload_failed", "failed to store plugin package"
		var invalid *jsplugin.InvalidPackageError
		switch {
		case errors.As(err, &invalid):
			status, code, message = http.StatusBadRequest, "js_plugin_package_invalid", invalid.Error()
		case errors.Is(err, jsplugin.ErrVersionContentMismatch):
			status, code, message = http.StatusConflict, "js_plugin_version_conflict", err.Error()
		default:
			h.logError("js plugin upload failed", "error", err)
		}
		h.recordJSPluginAudit(r, "js_plugin.upload", "", false, code, nil)
		h.writeError(w, r, status, code, message)
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.upload", row.PluginID, true, "", jsPluginAuditMeta(row.PluginID, row.Version, row.PackageSHA256))
	httpx.JSON(w, http.StatusOK, h.enrichJSPluginVersion(r.Context(), row))
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
		items = append(items, h.enrichJSPluginVersion(r.Context(), row))
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
	var body struct {
		ConfirmUntrusted bool `json:"confirm_untrusted"`
		ConfirmUnsigned  bool `json:"confirm_unsigned"`
		// GrantPermissions are the automation actions the admin approved after
		// seeing the list the version declares.
		GrantPermissions []string `json:"grant_permissions"`
	}
	_ = httpx.DecodeJSONBody(r, &body)
	confirm := body.ConfirmUntrusted || body.ConfirmUnsigned
	if err := h.jsPlugins.Enable(r.Context(), pluginID, version, jsplugin.EnableOptions{ConfirmUntrusted: confirm, GrantPermissions: body.GrantPermissions}); err != nil {
		code := "js_plugin_enable_failed"
		status := http.StatusBadRequest
		var permissionsErr *jsplugin.PermissionsRequiredError
		if errors.As(err, &permissionsErr) {
			code = "js_plugin_permissions_required"
			h.recordJSPluginAudit(r, "js_plugin.enable", pluginID, false, code, map[string]any{"version": version, "required": permissionsErr.Required})
			h.writeError(w, r, status, code, err.Error())
			return
		}
		var confirmErr *jsplugin.ConfirmRequiredError
		if errors.As(err, &confirmErr) {
			code = "js_plugin_confirm_required"
			h.recordJSPluginAudit(r, "js_plugin.enable", pluginID, false, code, map[string]any{"version": version, "trust": confirmErr.Trust})
			h.writeError(w, r, status, code, err.Error())
			return
		}
		var notConnected *jsplugin.KindNotConnectedError
		if errors.As(err, &notConnected) {
			code = "js_plugin_kind_not_connected"
		}
		h.recordJSPluginAudit(r, "js_plugin.enable", pluginID, false, code, map[string]any{"version": version})
		h.writeError(w, r, status, code, err.Error())
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

func (h Handler) TryJSPlugin(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	var body struct {
		Version string `json:"version"`
	}
	if err := httpx.DecodeJSONBody(r, &body); err != nil && r.ContentLength > 0 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	version := strings.TrimSpace(body.Version)
	if version == "" && h.trafficDB != nil {
		repo := store.NewJSPluginRepository(h.trafficDB.DB())
		versions, err := repo.ListVersions(r.Context(), pluginID)
		if err == nil {
			for _, row := range versions {
				if row.Status == store.JSPluginStatusEnabled {
					version = row.Version
					break
				}
			}
		}
	}
	if version == "" {
		h.writeError(w, r, http.StatusBadRequest, "version_required", "version is required when no enabled version exists")
		return
	}
	result, err := h.jsPlugins.TryVersion(r.Context(), pluginID, version)
	if err != nil {
		h.recordJSPluginAudit(r, "js_plugin.try", pluginID, false, "js_plugin_try_failed", map[string]any{"version": version})
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_try_failed", err.Error())
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.try", pluginID, true, "", map[string]any{"version": version, "ok": result["ok"]})
	httpx.JSON(w, http.StatusOK, result)
}

func jsPluginManifestSummary(raw store.JSON) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil
	}
	out := map[string]any{}
	if name, ok := manifest["name"].(string); ok && strings.TrimSpace(name) != "" {
		out["name"] = strings.TrimSpace(name)
	}
	if kind, ok := manifest["kind"].(string); ok && strings.TrimSpace(kind) != "" {
		out["kind"] = strings.TrimSpace(kind)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func jsPluginVersionPayload(row store.JSPluginVersion) map[string]any {
	payload := map[string]any{
		"plugin_id":      row.PluginID,
		"version":        row.Version,
		"status":         row.Status,
		"package_sha256": row.PackageSHA256,
		"manifest":       rawJSON(row.Manifest),
		"selftest":       rawJSON(row.SelfTest),
		"created_at":     row.CreatedAt,
	}
	if len(row.GrantedPermissions) > 0 {
		payload["granted_permissions"] = rawJSON(row.GrantedPermissions)
	}
	if strings.TrimSpace(row.Signer) != "" {
		payload["signer"] = row.Signer
		payload["signed"] = true
	} else {
		payload["signed"] = false
	}
	return payload
}

func (h Handler) enrichJSPluginVersion(ctx context.Context, row store.JSPluginVersion) map[string]any {
	payload := jsPluginVersionPayload(row)
	if h.jsPlugins != nil {
		trust, err := h.jsPlugins.TrustStatus(ctx, row.Signer)
		if err == nil {
			payload["trust"] = string(trust)
		}
	}
	return payload
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

// BindJSPluginSite binds an enabled plugin to one site, for every kind with
// site scope: quota_probe, model_list, credential_check, error_classifier and
// pricing_parse.
// Binding a pricing_parse plugin needs confirm_pricing_reviewed, because it
// changes the prices xLyra shows for that site.
func (h Handler) BindJSPluginSite(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	version := strings.TrimSpace(chi.URLParam(r, "version"))
	var body struct {
		SiteID                 string `json:"site_id"`
		ConfirmPricingReviewed bool   `json:"confirm_pricing_reviewed"`
	}
	if err := httpx.DecodeJSONBody(r, &body); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	siteUUID, err := uuid.Parse(strings.TrimSpace(body.SiteID))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_site_id", "site_id must be a uuid")
		return
	}
	meta := map[string]any{"version": version, "site_id": siteUUID.String()}
	kind, err := h.jsPlugins.CheckSiteBinding(r.Context(), pluginID, version)
	if err != nil {
		h.recordJSPluginAudit(r, "js_plugin.bind_site", pluginID, false, "js_plugin_bind_site_failed", meta)
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_bind_site_failed", err.Error())
		return
	}
	meta["kind"] = kind
	if kind == jsplugin.KindPricingParse && !body.ConfirmPricingReviewed {
		h.recordJSPluginAudit(r, "js_plugin.bind_site", pluginID, false, "js_plugin_pricing_review_required", meta)
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_pricing_review_required", "preview the parsed prices and confirm them before binding a pricing plugin")
		return
	}
	// quota_probe keeps its binding in the site's quota probe setting, which can
	// also name a built-in probe; every other kind lives under gateway plugins.
	var patchErr error
	if kind == jsplugin.KindQuotaProbe {
		_, patchErr = h.sites.PatchGatewayQuotaProbe(r.Context(), siteUUID, sitepkg.QuotaProbePluginPrefix+pluginID)
	} else {
		_, patchErr = h.sites.PatchGatewayPlugin(r.Context(), siteUUID, kind, pluginID)
	}
	if err := patchErr; err != nil {
		h.recordJSPluginAudit(r, "js_plugin.bind_site", pluginID, false, "site_plugin_patch_failed", meta)
		h.writeError(w, r, http.StatusBadRequest, "site_plugin_patch_failed", err.Error())
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.bind_site", pluginID, true, "", meta)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "kind": kind})
}

// UnbindJSPluginSite removes a site's plugin of one kind, returning the site to
// the default behavior.
func (h Handler) UnbindJSPluginSite(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	var body struct {
		SiteID string `json:"site_id"`
		Kind   string `json:"kind"`
	}
	if err := httpx.DecodeJSONBody(r, &body); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	siteUUID, err := uuid.Parse(strings.TrimSpace(body.SiteID))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_site_id", "site_id must be a uuid")
		return
	}
	kind := strings.TrimSpace(body.Kind)
	meta := map[string]any{"kind": kind, "site_id": siteUUID.String()}
	site, err := h.sites.Get(r.Context(), siteUUID)
	if err != nil {
		h.writeError(w, r, http.StatusNotFound, "not_found", "site was not found")
		return
	}
	// A plugin may only remove its own binding; otherwise unbinding plugin A
	// from a site would silently remove plugin B's.
	cfg := sitepkg.GatewayConfigFromSiteMeta(site.Meta)
	if kind == jsplugin.KindQuotaProbe {
		if sitepkg.QuotaProbeTypeFromConfig(cfg) != sitepkg.QuotaProbePluginPrefix+pluginID {
			h.writeError(w, r, http.StatusConflict, "js_plugin_not_bound", "this site is not bound to this plugin")
			return
		}
		if _, err := h.sites.PatchGatewayQuotaProbe(r.Context(), siteUUID, ""); err != nil {
			h.recordJSPluginAudit(r, "js_plugin.unbind_site", pluginID, false, "site_plugin_patch_failed", meta)
			h.writeError(w, r, http.StatusBadRequest, "site_plugin_patch_failed", err.Error())
			return
		}
		h.recordJSPluginAudit(r, "js_plugin.unbind_site", pluginID, true, "", meta)
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if sitepkg.SitePluginID(cfg, kind) != pluginID {
		h.writeError(w, r, http.StatusConflict, "js_plugin_not_bound", "this site is not bound to this plugin")
		return
	}
	if _, err := h.sites.PatchGatewayPlugin(r.Context(), siteUUID, kind, ""); err != nil {
		h.recordJSPluginAudit(r, "js_plugin.unbind_site", pluginID, false, "site_plugin_patch_failed", meta)
		h.writeError(w, r, http.StatusBadRequest, "site_plugin_patch_failed", err.Error())
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.unbind_site", pluginID, true, "", meta)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// PreviewJSPluginPricing fetches a site's price table and shows how an enabled
// pricing_parse plugin reads it, without saving anything.
func (h Handler) PreviewJSPluginPricing(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil || h.sites == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	var body struct {
		SiteID string `json:"site_id"`
	}
	if err := httpx.DecodeJSONBody(r, &body); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	siteUUID, err := uuid.Parse(strings.TrimSpace(body.SiteID))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_site_id", "site_id must be a uuid")
		return
	}
	registry := h.jsPlugins.Catalog().Registry()
	plugin, ok := registry.ByPluginID(pluginID)
	if !ok || plugin.Manifest.Kind != jsplugin.KindPricingParse {
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_not_pricing", "an enabled pricing_parse plugin is required")
		return
	}
	snapshot, err := h.sites.PreviewPluginPricing(r.Context(), siteUUID, plugin)
	if err != nil {
		h.recordJSPluginAudit(r, "js_plugin.preview_pricing", pluginID, false, "js_plugin_preview_failed", map[string]any{"site_id": siteUUID.String()})
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_preview_failed", err.Error())
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.preview_pricing", pluginID, true, "", map[string]any{"site_id": siteUUID.String()})
	httpx.JSON(w, http.StatusOK, map[string]any{
		"groups": pricingPreviewGroups(snapshot.Groups),
		"items":  pricingPreviewItems(snapshot.Items),
	})
}

func pricingPreviewGroups(groups []adapter.PricingGroup) []map[string]any {
	out := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		out = append(out, map[string]any{
			"name": group.GroupName, "display_name": group.DisplayName, "ratio": group.Ratio, "auto": group.IsAuto,
		})
	}
	return out
}

// pricingPreviewItems lists only the values the plugin actually supplied, so an
// admin reviewing the preview does not mistake a missing value for a zero.
func pricingPreviewItems(items []adapter.ModelPricing) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"model": item.ModelName, "display_name": item.DisplayName, "group": item.GroupName,
			"billing_type": item.BillingType, "currency": item.Currency, "group_ratio": item.GroupRatio,
		}
		for _, value := range []struct {
			key string
			has bool
			val float64
		}{
			{"model_ratio", item.HasModelRatio, item.ModelRatio},
			{"completion_ratio", item.HasCompletionRatio, item.CompletionRatio},
			{"cache_ratio", item.HasCacheRatio, item.CacheRatio},
			{"create_cache_ratio", item.HasCreateCacheRatio, item.CreateCacheRatio},
			{"create_cache_1h_ratio", item.HasCreateCache1hRatio, item.CreateCache1hRatio},
			{"image_ratio", item.HasImageRatio, item.ImageRatio},
			{"audio_ratio", item.HasAudioRatio, item.AudioRatio},
			{"audio_completion_ratio", item.HasAudioCompletionRatio, item.AudioCompletionRatio},
			{"model_price", item.HasModelPrice, item.ModelPrice},
			{"input_value", item.HasInputValue, item.InputValue},
			{"output_value", item.HasOutputValue, item.OutputValue},
			{"per_request_value", item.HasPerRequestValue, item.PerRequestValue},
		} {
			if value.has {
				entry[value.key] = value.val
			}
		}
		out = append(out, entry)
	}
	return out
}

// ListJSPluginAutomations returns the bindings of an automation plugin.
func (h Handler) ListJSPluginAutomations(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	items, err := h.jsPlugins.ListAutomations(r.Context(), strings.TrimSpace(chi.URLParam(r, "id")))
	if err != nil {
		if abandonIfClientGone(w, r) {
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "js_plugin_automation_list_failed", "failed to list automations")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// jsPluginAutomationBody carries one value per input the plugin declares: the id
// (or ids) of the picked objects, or the parameter value.
type jsPluginAutomationBody struct {
	Inputs map[string]any `json:"inputs"`
}

func (b jsPluginAutomationBody) input() jsplugin.AutomationBindingInput {
	return jsplugin.AutomationBindingInput{Inputs: b.Inputs}
}

// writeAutomationError maps what the manager returns onto a response.
func (h Handler) writeAutomationError(w http.ResponseWriter, r *http.Request, action, pluginID string, meta map[string]any, err error) {
	var inputErr *jsplugin.AutomationInputError
	switch {
	case errors.As(err, &inputErr):
		h.recordJSPluginAudit(r, action, pluginID, false, "js_plugin_automation_invalid", meta)
		h.writeError(w, r, http.StatusBadRequest, "js_plugin_automation_invalid", inputErr.Message)
	case errors.Is(err, gorm.ErrRecordNotFound):
		h.recordJSPluginAudit(r, action, pluginID, false, "not_found", meta)
		h.writeError(w, r, http.StatusNotFound, "not_found", "automation was not found")
	default:
		h.recordJSPluginAudit(r, action, pluginID, false, "js_plugin_automation_failed", meta)
		h.writeError(w, r, http.StatusInternalServerError, "js_plugin_automation_failed", "failed to save the automation")
	}
}

// CreateJSPluginAutomation binds an enabled automation plugin to an OAuth
// account and the objects it may act on.
func (h Handler) CreateJSPluginAutomation(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	var body jsPluginAutomationBody
	if err := httpx.DecodeJSONBody(r, &body); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	meta := map[string]any{"inputs": len(body.Inputs)}
	binding, err := h.jsPlugins.CreateAutomation(r.Context(), pluginID, body.input())
	if err != nil {
		h.writeAutomationError(w, r, "js_plugin.bind_automation", pluginID, meta, err)
		return
	}
	meta["binding_id"] = binding.ID.String()
	h.recordJSPluginAudit(r, "js_plugin.bind_automation", pluginID, true, "", meta)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "id": binding.ID.String()})
}

// UpdateJSPluginAutomation changes the parameters and targets of a binding.
func (h Handler) UpdateJSPluginAutomation(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	bindingID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "binding_id")))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_binding_id", "binding_id must be a uuid")
		return
	}
	var body jsPluginAutomationBody
	if err := httpx.DecodeJSONBody(r, &body); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	meta := map[string]any{"binding_id": bindingID.String(), "inputs": len(body.Inputs)}
	if err := h.jsPlugins.UpdateAutomation(r.Context(), pluginID, bindingID, body.input()); err != nil {
		h.writeAutomationError(w, r, "js_plugin.update_automation", pluginID, meta, err)
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.update_automation", pluginID, true, "", meta)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DeleteJSPluginAutomation removes a binding.
func (h Handler) DeleteJSPluginAutomation(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	pluginID := strings.TrimSpace(chi.URLParam(r, "id"))
	bindingID, err := uuid.Parse(strings.TrimSpace(chi.URLParam(r, "binding_id")))
	if err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_binding_id", "binding_id must be a uuid")
		return
	}
	meta := map[string]any{"binding_id": bindingID.String()}
	if err := h.jsPlugins.DeleteAutomation(r.Context(), pluginID, bindingID); err != nil {
		h.writeAutomationError(w, r, "js_plugin.unbind_automation", pluginID, meta, err)
		return
	}
	h.recordJSPluginAudit(r, "js_plugin.unbind_automation", pluginID, true, "", meta)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ListJSPluginActionLog returns what a plugin recently asked xLyra to do.
func (h Handler) ListJSPluginActionLog(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := h.jsPlugins.ActionLog(r.Context(), strings.TrimSpace(chi.URLParam(r, "id")), limit)
	if err != nil {
		if abandonIfClientGone(w, r) {
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "js_plugin_action_log_failed", "failed to read the action log")
		return
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, map[string]any{
			"id":         row.ID,
			"version":    row.Version,
			"event_type": row.EventType,
			"action":     rawJSON(row.Action),
			"target_id":  row.TargetID,
			"status":     row.Status,
			"detail":     row.Detail,
			"created_at": row.CreatedAt,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// rawJSON sends a stored JSON column as the JSON it holds. store.JSON is a byte
// slice, which encoding/json would otherwise write as a base64 string.
func rawJSON(raw store.JSON) any {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	return json.RawMessage(raw)
}

// ListJSPluginAutomationOptions lists the objects an admin may pick for one
// input of an automation plugin, with the reason for any that cannot be picked.
func (h Handler) ListJSPluginAutomationOptions(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	items, err := h.jsPlugins.AutomationOptions(r.Context(), strings.TrimSpace(chi.URLParam(r, "id")), strings.TrimSpace(chi.URLParam(r, "name")))
	if err != nil {
		if abandonIfClientGone(w, r) {
			return
		}
		var inputErr *jsplugin.AutomationInputError
		if errors.As(err, &inputErr) {
			h.writeError(w, r, http.StatusBadRequest, "js_plugin_automation_invalid", inputErr.Message)
			return
		}
		h.writeError(w, r, http.StatusInternalServerError, "js_plugin_automation_failed", "failed to list the options")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

// statusClientClosedRequest is the conventional status for a request the client
// gave up on before the answer was ready.
const statusClientClosedRequest = 499

// abandonIfClientGone ends a request whose caller has already left. The page
// aborts the lists it no longer needs (closing a dialog, switching plugins), and
// the cancelled database call is not a server fault: answering it with a 500
// only puts a false error in the log.
func abandonIfClientGone(w http.ResponseWriter, r *http.Request) bool {
	if r.Context().Err() == nil {
		return false
	}
	w.WriteHeader(statusClientClosedRequest)
	return true
}
