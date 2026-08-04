package settings

import (
	"net/http"

	"xlyra/server/internal/config"
	"xlyra/server/internal/httpx"
)

func (h Handler) GetPortal(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, config.ReadPortalConfig(h.confFile))
}

func (h Handler) UpdatePortal(w http.ResponseWriter, r *http.Request) {
	if h.confFile == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "config_unavailable", "config persistence is not available")
		return
	}
	var req map[string]any
	if err := httpx.DecodeJSONBody(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	next := config.PortalConfigFromRaw(req)
	if err := config.ValidatePortalConfig(next); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_portal_config", err.Error())
		return
	}

	if err := h.confFile.Set(config.PortalConfigPath, config.PortalConfigToMap(next)); err != nil {
		h.logError("update portal config", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save config")
		return
	}

	h.logInfo("portal config updated", "enabled", next.Enabled, "show_requests", next.ShowRequests)
	httpx.JSON(w, http.StatusOK, next)
}
