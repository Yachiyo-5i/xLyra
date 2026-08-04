package settings

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xlyra/server/internal/config"
	"xlyra/server/internal/downloads"
	"xlyra/server/internal/store"
)

const settingsTestMasterKey = "test-master-key"

func settingsTestRequest(method string, target string, body string) *http.Request {
	return httptest.NewRequest(method, target, strings.NewReader(body))
}

func settingsRawJSONRequest(method string, target string, body string) *http.Request {
	req := settingsTestRequest(method, target, body)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func settingsMaybeJSONRequest(method string, target string, body string) *http.Request {
	if body == "" {
		return settingsTestRequest(method, target, body)
	}
	return settingsRawJSONRequest(method, target, body)
}

func settingsJSONRequest(t *testing.T, method string, target string, payload any) *http.Request {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request payload: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func settingsPerform(handler func(http.ResponseWriter, *http.Request), req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func settingsAssertStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()

	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, status, rec.Body.String())
	}
}

func assertSettingsInvalidJSON(t *testing.T, handler func(http.ResponseWriter, *http.Request), method string, target string, body string) {
	t.Helper()

	rec := settingsPerform(handler, settingsRawJSONRequest(method, target, body))
	assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_body")
}

func assertSettingsErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	settingsAssertStatus(t, rec, status)
	body := settingsDecodeJSON[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, rec)
	if body.Error.Code != code {
		t.Fatalf("error code = %q, want %q", body.Error.Code, code)
	}
}

func assertSettingsErrorWriterCode(t *testing.T, target string, writeError func(http.ResponseWriter, *http.Request), status int, code string) {
	t.Helper()

	req := settingsTestRequest(http.MethodPost, target, "")
	rec := settingsPerform(writeError, req)
	assertSettingsErrorCode(t, rec, status, code)
}

func settingsDecodeJSON[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var body T
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func settingsConfigFile(t *testing.T) *config.ConfigFile {
	t.Helper()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	return confFile
}

func settingsHandlerWithConfig(t *testing.T) (Handler, *config.ConfigFile) {
	t.Helper()

	confFile := settingsConfigFile(t)
	return NewHandler(slog.Default(), confFile), confFile
}

func settingsHandlerWithoutConfig() Handler {
	return NewHandler(slog.Default(), nil)
}

func settingsHandlerWithStore() Handler {
	return NewHandler(slog.Default(), nil, &store.Store{})
}

func settingsBackupHandler(confFile *config.ConfigFile, downloadService *downloads.Service) Handler {
	return NewHandlerWithBackup(slog.Default(), confFile, &store.Store{}, settingsTestMasterKey, downloadService)
}
