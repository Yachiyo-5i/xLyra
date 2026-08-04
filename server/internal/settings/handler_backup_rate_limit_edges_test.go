package settings

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/downloads"
	"xlyra/server/internal/store"
)

func backupMultipartBody(t *testing.T, fileContent string) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("passphrase", "secret"); err != nil {
		t.Fatalf("write passphrase field: %v", err)
	}
	if fileContent != "" {
		part, err := writer.CreateFormFile("file", "backup.xlyra")
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := part.Write([]byte(fileContent)); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func backupMultipartRequest(t *testing.T, fileContent string) *http.Request {
	t.Helper()

	body, contentType := backupMultipartBody(t, fileContent)
	req := httptest.NewRequest(http.MethodPost, "/settings/backup/import", body)
	req.Header.Set("Content-Type", contentType)
	return req
}

func TestExportBackupRejectsInvalidBodyAndUnavailableService(t *testing.T) {
	t.Parallel()

	t.Run("invalid json", func(t *testing.T) {
		t.Parallel()

		h := settingsHandlerWithoutConfig()
		assertSettingsInvalidJSON(t, h.ExportBackup, http.MethodPost, "/settings/backup/export", `{"passphrase":`)
	})

	t.Run("backup unavailable after download dependency check", func(t *testing.T) {
		t.Parallel()

		h := settingsBackupHandler(nil, downloads.NewService())
		rec := settingsPerform(h.ExportBackup, settingsRawJSONRequest(http.MethodPost, "/settings/backup/export", `{"passphrase":"secret"}`))

		assertSettingsErrorCode(t, rec, http.StatusServiceUnavailable, "backup_unavailable")
	})
}

func TestImportBackupValidatesMultipartFileAndServiceAvailability(t *testing.T) {
	t.Parallel()

	t.Run("missing file part", func(t *testing.T) {
		t.Parallel()

		h := settingsBackupHandler(nil, nil)
		rec := settingsPerform(h.ImportBackup, backupMultipartRequest(t, ""))

		assertSettingsErrorCode(t, rec, http.StatusBadRequest, "backup_file_required")
	})

	t.Run("backup service unavailable after reading file", func(t *testing.T) {
		t.Parallel()

		h := settingsBackupHandler(nil, nil)
		rec := settingsPerform(h.ImportBackup, backupMultipartRequest(t, "encrypted"))

		assertSettingsErrorCode(t, rec, http.StatusServiceUnavailable, "backup_unavailable")
	})
}

func TestAutomaticBackupActionsReportUnavailableWithoutConfiguredService(t *testing.T) {
	t.Parallel()

	h := settingsBackupHandler(nil, nil)
	cases := []struct {
		name   string
		method string
		target string
		body   string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{name: "test", method: http.MethodPost, target: "/settings/backup/automatic/test", handle: h.TestAutomaticBackup},
		{name: "list files", method: http.MethodGet, target: "/settings/backup/automatic/files", handle: h.ListAutomaticBackupFiles},
		{name: "run", method: http.MethodPost, target: "/settings/backup/automatic/run", handle: h.RunAutomaticBackup},
		{name: "restore", method: http.MethodPost, target: "/settings/backup/automatic/files/restore", body: `{"key":"xlyra/xlyra-backup-20260621-030000.zip.xlyra"}`, handle: h.RestoreAutomaticBackupFile},
		{name: "delete", method: http.MethodDelete, target: "/settings/backup/automatic/files", body: `{"key":"xlyra/xlyra-backup-20260621-030000.zip.xlyra"}`, handle: h.DeleteAutomaticBackupFile},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := settingsMaybeJSONRequest(tc.method, tc.target, tc.body)
			rec := settingsPerform(tc.handle, req)

			assertSettingsErrorCode(t, rec, http.StatusServiceUnavailable, "backup_unavailable")
		})
	}
}

func TestAutomaticBackupConfigHandlersValidateInputAndMissingConfig(t *testing.T) {
	t.Parallel()

	h := settingsBackupHandler(nil, nil)

	t.Run("get returns default payload when config is absent", func(t *testing.T) {
		t.Parallel()

		rec := settingsPerform(h.GetAutomaticBackup, settingsTestRequest(http.MethodGet, "/settings/backup/automatic", ""))

		settingsAssertStatus(t, rec, http.StatusOK)
		body := settingsDecodeJSON[struct {
			AutomaticBackup struct {
				Ready bool `json:"ready"`
			} `json:"automatic_backup"`
		}](t, rec)
		if body.AutomaticBackup.Ready {
			t.Fatalf("default automatic backup payload should not be ready: %#v", body.AutomaticBackup)
		}
	})

	t.Run("update invalid json", func(t *testing.T) {
		t.Parallel()

		assertSettingsInvalidJSON(t, h.UpdateAutomaticBackup, http.MethodPut, "/settings/backup/automatic", `{"enabled":`)
	})

	t.Run("update missing config persistence", func(t *testing.T) {
		t.Parallel()

		rec := settingsPerform(h.UpdateAutomaticBackup, settingsRawJSONRequest(http.MethodPut, "/settings/backup/automatic", `{"enabled":false}`))

		assertSettingsErrorCode(t, rec, http.StatusBadRequest, "automatic_backup_config_invalid")
	})

	for _, tc := range []struct {
		name   string
		method string
		target string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{name: "restore invalid json", method: http.MethodPost, target: "/settings/backup/automatic/files/restore", handle: h.RestoreAutomaticBackupFile},
		{name: "delete invalid json", method: http.MethodDelete, target: "/settings/backup/automatic/files", handle: h.DeleteAutomaticBackupFile},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assertSettingsInvalidJSON(t, tc.handle, tc.method, tc.target, `{"key":`)
		})
	}
}

func TestRateLimitHandlersUseDefaultsAndMapEarlyErrors(t *testing.T) {
	t.Parallel()

	t.Run("get uses service default when store is uninitialized", func(t *testing.T) {
		t.Parallel()

		h := settingsHandlerWithStore()
		rec := settingsPerform(h.GetRateLimits, settingsTestRequest(http.MethodGet, "/settings/rate-limits", ""))

		settingsAssertStatus(t, rec, http.StatusOK)
		body := settingsDecodeJSON[rateLimitConfigEnvelope](t, rec)
		if body.RateLimit.Status != store.RateLimitStatusDisabled {
			t.Fatalf("rate limit status = %q, want disabled", body.RateLimit.Status)
		}
	})

	t.Run("update invalid json before persistence", func(t *testing.T) {
		t.Parallel()

		h := settingsHandlerWithStore()
		assertSettingsInvalidJSON(t, h.UpdateRateLimits, http.MethodPut, "/settings/rate-limits", `{"rate_limit":`)
	})

	t.Run("update maps uninitialized store error", func(t *testing.T) {
		t.Parallel()

		h := settingsHandlerWithStore()
		rec := settingsPerform(h.UpdateRateLimits, settingsRawJSONRequest(http.MethodPut, "/settings/rate-limits", `{"rate_limit":{"status":"enabled"}}`))

		assertSettingsErrorCode(t, rec, http.StatusBadRequest, "rate_limit_update_failed")
	})
}
