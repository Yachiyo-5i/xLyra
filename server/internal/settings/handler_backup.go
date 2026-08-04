package settings

import (
	"io"
	"net/http"
	"os"
	"strings"

	"xlyra/server/internal/downloads"
	"xlyra/server/internal/httpx"
)

const maxBackupUploadBytes = 512 << 20

type backupPassphraseRequest struct {
	Passphrase string `json:"passphrase"`
}

func (h Handler) ExportBackup(w http.ResponseWriter, r *http.Request) {
	var req backupPassphraseRequest
	if err := httpx.DecodeJSONBody(r, &req); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}

	if h.downloads == nil {
		httpx.Error(w, r, http.StatusServiceUnavailable, "download_service_unavailable", "download service is not available")
		return
	}
	filePath, filename, err := h.backups.Export(r.Context(), req.Passphrase)
	if err != nil {
		h.writeBackupError(w, r, err)
		return
	}

	ticket, err := h.downloads.Register(r.Context(), downloads.RegisterInput{
		Filename:    filename,
		ContentType: "application/vnd.xlyra.backup",
		Scope:       "settings.backup.export",
		FilePath:    filePath,
		DeleteFile:  true,
	})
	if err != nil {
		_ = os.Remove(filePath)
		httpx.Error(w, r, http.StatusBadRequest, "download_register_failed", err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"download": ticket})
}

func (h Handler) ImportBackup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUploadBytes)
	if err := r.ParseMultipartForm(maxBackupUploadBytes); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_backup_upload", err.Error())
		return
	}

	passphrase := strings.TrimSpace(r.FormValue("passphrase"))
	file, _, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "backup_file_required", "backup file is required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "backup_file_read_failed", err.Error())
		return
	}
	summary, err := h.backups.Import(r.Context(), passphrase, data)
	if err != nil {
		h.writeBackupError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"backup": summary})
}

func (h Handler) writeBackupError(w http.ResponseWriter, r *http.Request, err error) {
	message := err.Error()
	status := http.StatusBadRequest
	code := "backup_failed"
	switch {
	case strings.Contains(message, "not available"):
		status = http.StatusServiceUnavailable
		code = "backup_unavailable"
	case strings.Contains(message, "passphrase"):
		code = "backup_passphrase_required"
	case strings.Contains(message, "decrypt backup payload"):
		code = "backup_passphrase_invalid"
	case strings.Contains(message, "unsupported backup"):
		code = "backup_unsupported"
	}
	h.logError("backup operation failed", "error", err)
	httpx.Error(w, r, status, code, message)
}
