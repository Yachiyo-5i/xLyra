package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"xlyra/server/internal/jsplugin"
)

func (h Handler) GetPluginIcon(w http.ResponseWriter, r *http.Request) {
	if h.jsPlugins == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "js_plugin_unavailable", "js plugin manager is not available")
		return
	}
	icon, err := h.jsPlugins.SiteProviderIcon(r.Context(), chi.URLParam(r, "pluginID"))
	if errors.Is(err, jsplugin.ErrPluginIconNotFound) {
		h.writeError(w, r, http.StatusNotFound, "not_found", "plugin icon not found")
		return
	}
	if err != nil {
		h.writeError(w, r, http.StatusInternalServerError, "plugin_icon_failed", "failed to load plugin icon")
		return
	}
	etag := `"` + icon.SHA256 + `"`
	header := w.Header()
	header.Set("Content-Type", icon.ContentType)
	header.Set("ETag", etag)
	header.Set("Cache-Control", "private, max-age=86400")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'")
	if ifNoneMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(icon.Bytes)
}

func ifNoneMatch(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" || etag == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "W/")
		if part == etag {
			return true
		}
	}
	return false
}
