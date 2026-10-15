package gateway

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"xlyra/server/internal/httpx"
	"xlyra/server/internal/jsplugin"
)

// PluginProtocol serves POST /v1/plugins/{slug} for enabled third-party protocol bindings.
func (h Handler) PluginProtocol(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	registry := jsplugin.DefaultCatalog().Registry()
	if registry == nil {
		writePluginDisabled(w, r)
		return
	}
	plugin, ok := registry.ProtocolBySlug(slug)
	if !ok {
		writePluginDisabled(w, r)
		return
	}
	endpoint := newJSEndpointAdapter(plugin, nil)
	h.serveEndpoint(w, r, endpoint, openAIProtocolResolver{db: h.db})
}

func writePluginDisabled(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusNotFound, "plugin_disabled", "plugin is not enabled for this endpoint")
}
