package settings

import (
	"errors"
	"net/http"
	"strings"

	"xlyra/server/internal/backup"
	"xlyra/server/internal/httpx"
)

type automaticBackupKeyRequest struct {
	Key string `json:"key"`
}

func (h Handler) GetAutomaticBackup(w http.ResponseWriter, r *http.Request) {
	if h.autoBackups == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "backup_unavailable", "automatic backup service is not available")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"automatic_backup": h.autoBackups.GetConfig()})
}

func (h Handler) UpdateAutomaticBackup(w http.ResponseWriter, r *http.Request) {
	if h.autoBackups == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "backup_unavailable", "automatic backup service is not available")
		return
	}
	var req backup.AutomaticConfigInput
	if err := httpx.DecodeJSONBody(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	payload, err := h.autoBackups.UpdateConfig(req)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "automatic_backup_config_invalid", err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"automatic_backup": payload})
}

func (h Handler) TestAutomaticBackup(w http.ResponseWriter, r *http.Request) {
	if h.autoBackups == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "backup_unavailable", "automatic backup service is not available")
		return
	}
	result, err := h.autoBackups.Test(r.Context())
	if err != nil {
		h.writeAutomaticBackupError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"test": result})
}

func (h Handler) ListAutomaticBackupFiles(w http.ResponseWriter, r *http.Request) {
	if h.autoBackups == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "backup_unavailable", "automatic backup service is not available")
		return
	}
	files, err := h.autoBackups.ListFiles(r.Context())
	if err != nil {
		h.writeAutomaticBackupError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"files": files})
}

func (h Handler) RunAutomaticBackup(w http.ResponseWriter, r *http.Request) {
	if h.autoBackups == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "backup_unavailable", "automatic backup service is not available")
		return
	}
	task, err := h.autoBackups.RunNow()
	if err != nil {
		h.writeAutomaticBackupError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"backup": task})
}

func (h Handler) RestoreAutomaticBackupFile(w http.ResponseWriter, r *http.Request) {
	if h.autoBackups == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "backup_unavailable", "automatic backup service is not available")
		return
	}
	var req automaticBackupKeyRequest
	if err := httpx.DecodeJSONBody(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	summary, err := h.autoBackups.Restore(r.Context(), req.Key)
	if err != nil {
		h.writeAutomaticBackupError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"backup": summary})
}

func (h Handler) DeleteAutomaticBackupFile(w http.ResponseWriter, r *http.Request) {
	if h.autoBackups == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "backup_unavailable", "automatic backup service is not available")
		return
	}
	var req automaticBackupKeyRequest
	if err := httpx.DecodeJSONBody(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if err := h.autoBackups.Delete(r.Context(), req.Key); err != nil {
		h.writeAutomaticBackupError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) writeAutomaticBackupError(w http.ResponseWriter, r *http.Request, err error) {
	message := err.Error()
	status := http.StatusBadRequest
	code := "automatic_backup_failed"
	switch {
	case errors.Is(err, backup.ErrAutomaticAlreadyRunning):
		status = http.StatusConflict
		code = "automatic_backup_running"
	case strings.Contains(message, "not available"):
		status = http.StatusServiceUnavailable
		code = "backup_unavailable"
	case strings.Contains(message, "disabled"):
		code = "automatic_backup_disabled"
	case strings.Contains(message, "required"):
		code = "automatic_backup_config_required"
	case strings.Contains(message, "decrypt backup payload"):
		code = "backup_passphrase_invalid"
	case strings.Contains(message, "unsupported backup"):
		code = "backup_unsupported"
	}
	h.logError("automatic backup operation failed", "error", err)
	httpx.Error(w, r, status, code, message)
}
