package settings

import (
	"net/http"

	"xlyra/server/internal/config"
	"xlyra/server/internal/httpx"
)

type generalConfig = config.GeneralConfig

func (h Handler) GetGeneral(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, config.ReadGeneralConfig(h.confFile))
}

func (h Handler) UpdateGeneral(w http.ResponseWriter, r *http.Request) {
	if h.confFile == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "config_unavailable", "config persistence is not available")
		return
	}
	var req map[string]any
	if err := httpx.DecodeJSONBody(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	next := config.GeneralConfigFromRaw(req)
	if err := config.ValidateGeneralConfig(next); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_general_config", err.Error())
		return
	}

	if err := h.confFile.Set(config.GeneralConfigPath, config.GeneralConfigToMap(next)); err != nil {
		h.logError("update general config", "error", err)
		httpx.Error(w, r, http.StatusInternalServerError, "config_write_error", "failed to save config")
		return
	}

	h.logInfo("general config updated", "log_level", next.Log.Level, "ip_whitelist_enabled", next.IPWhitelist.Enabled)
	httpx.JSON(w, http.StatusOK, next)
}
