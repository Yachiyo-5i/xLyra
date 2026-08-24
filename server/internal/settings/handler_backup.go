package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/backup"
	"xlyra/server/internal/downloads"
	"xlyra/server/internal/httpx"
)

const (
	maxBackupUploadBytes     = backup.MaxImportBytes
	maxBackupUploadBodyBytes = maxBackupUploadBytes + 1<<20
	maxBackupUploadMemory    = 32 << 20
)

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
	r.Body = http.MaxBytesReader(w, r.Body, maxBackupUploadBodyBytes)
	if err := r.ParseMultipartForm(maxBackupUploadMemory); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_backup_upload", err.Error())
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	passphrase := strings.TrimSpace(r.FormValue("passphrase"))
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "backup_file_required", "backup file is required")
		return
	}
	defer file.Close()
	if header.Size > maxBackupUploadBytes {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_backup_upload", fmt.Sprintf("backup file size %d exceeds maximum %d", header.Size, maxBackupUploadBytes))
		return
	}

	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		h.importBackupSSE(w, r, passphrase, file)
		return
	}

	summary, err := h.backups.ImportReader(r.Context(), passphrase, io.LimitReader(file, maxBackupUploadBytes+1), backup.ImportOptions{AdminID: adminIDFromRequest(r)})
	if err != nil {
		h.writeBackupError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"backup": summary})
}

func (h Handler) importBackupSSE(w http.ResponseWriter, r *http.Request, passphrase string, file io.Reader) {
	streamBackupRestore(w, r, "decrypt", func(ctx context.Context, progress backup.ProgressFunc) (backup.ImportSummary, error) {
		return h.backups.ImportReader(ctx, passphrase, io.LimitReader(file, maxBackupUploadBytes+1), backup.ImportOptions{AdminID: adminIDFromRequest(r)}, progress)
	})
}

func adminIDFromRequest(r *http.Request) uuid.UUID {
	adminID, _ := auth.AdminIDFromContext(r.Context())
	return adminID
}

func streamBackupRestore(w http.ResponseWriter, r *http.Request, initialStep string, run func(context.Context, backup.ProgressFunc) (backup.ImportSummary, error)) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, r, http.StatusInternalServerError, "streaming_unsupported", "streaming is not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	currentStep := initialStep
	var writeErr error
	progress := func(event backup.ProgressEvent) {
		if event.Step != "complete" {
			currentStep = event.Step
		}
		if writeErr != nil {
			return
		}
		writeErr = writeBackupProgressEvent(w, event)
		if writeErr != nil {
			cancel()
			return
		}
		flusher.Flush()
	}
	_, err := run(ctx, progress)
	if writeErr != nil {
		return
	}
	if err != nil {
		_ = writeBackupProgressEvent(w, backup.ProgressEvent{Step: currentStep, Status: "error", Message: err.Error()})
		flusher.Flush()
	}
}

func writeBackupProgressEvent(w http.ResponseWriter, event backup.ProgressEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func (h Handler) writeBackupError(w http.ResponseWriter, r *http.Request, err error) {
	message := err.Error()
	status := http.StatusBadRequest
	code := "backup_failed"
	switch {
	case errors.Is(err, backup.ErrOperationInProgress):
		status = http.StatusConflict
		code = "backup_restore_in_progress"
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
