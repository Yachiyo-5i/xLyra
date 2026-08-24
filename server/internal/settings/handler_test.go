package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"xlyra/server/internal/backup"
	"xlyra/server/internal/config"
	"xlyra/server/internal/ratelimit"
	"xlyra/server/internal/store"
)

func TestGetSystemProxy_Defaults(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)
	rec := settingsPerform(h.GetSystemProxy, settingsTestRequest(http.MethodGet, "/settings/system-proxy", ""))

	settingsAssertStatus(t, rec, http.StatusOK)

	nc := settingsDecodeJSON[systemProxyConfig](t, rec)

	if len(nc.Proxies) != 0 {
		t.Fatalf("expected no default proxies, got %d", len(nc.Proxies))
	}
}

func TestUpdateSystemProxy_SetProxies(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)

	body := `{"proxies":[{"id":"local","name":"Local","url":"http://127.0.0.1:7890","type":"http"}]}`
	req := settingsRawJSONRequest(http.MethodPut, "/settings/system-proxy", body)
	rec := settingsPerform(h.UpdateSystemProxy, req)

	settingsAssertStatus(t, rec, http.StatusOK)

	nc := settingsDecodeJSON[systemProxyConfig](t, rec)

	if len(nc.Proxies) != 1 {
		t.Fatalf("expected one proxy, got %d", len(nc.Proxies))
	}
	if nc.Proxies[0].URL != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected proxy url: %s", nc.Proxies[0].URL)
	}
	if nc.Proxies[0].Type != "http" {
		t.Fatalf("unexpected proxy type: %s", nc.Proxies[0].Type)
	}

	// Verify persisted
	t.Run("persisted", func(t *testing.T) {
		rec := settingsPerform(h.GetSystemProxy, settingsTestRequest(http.MethodGet, "/settings/system-proxy", ""))
		settingsAssertStatus(t, rec, http.StatusOK)
		nc2 := settingsDecodeJSON[systemProxyConfig](t, rec)
		if len(nc2.Proxies) != 1 || nc2.Proxies[0].URL != "http://127.0.0.1:7890" {
			t.Fatal("proxy config was not persisted")
		}
	})
}

func TestUpdateSystemProxy_ClearProxies(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)

	// First set a proxy
	body := `{"proxies":[{"id":"local","name":"Local","url":"http://127.0.0.1:7890","type":"http"}]}`
	req := settingsRawJSONRequest(http.MethodPut, "/settings/system-proxy", body)
	_ = settingsPerform(h.UpdateSystemProxy, req)

	// Then clear it
	body = `{"proxies":[]}`
	req = settingsRawJSONRequest(http.MethodPut, "/settings/system-proxy", body)
	rec := settingsPerform(h.UpdateSystemProxy, req)

	settingsAssertStatus(t, rec, http.StatusOK)

	nc := settingsDecodeJSON[systemProxyConfig](t, rec)

	if len(nc.Proxies) != 0 {
		t.Fatalf("expected proxies to be cleared, got %d", len(nc.Proxies))
	}
}

func TestUpdateSystemProxy_InvalidBody(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)

	body := `not json`
	req := settingsRawJSONRequest(http.MethodPut, "/settings/system-proxy", body)
	rec := settingsPerform(h.UpdateSystemProxy, req)

	settingsAssertStatus(t, rec, http.StatusBadRequest)
}

func TestUpdateSystemProxy_InvalidConfig(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)
	body := `{"proxies":[{"id":"local","name":"Local","url":"socks5://127.0.0.1:1080","type":"http"}]}`
	req := settingsRawJSONRequest(http.MethodPut, "/settings/system-proxy", body)
	rec := settingsPerform(h.UpdateSystemProxy, req)

	assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_system_proxy_config")
}

func TestUpdateSystemProxy_NilConfFile(t *testing.T) {
	h := settingsHandlerWithoutConfig()

	body := `{"proxies":[{"id":"local","name":"Local","url":"http://127.0.0.1:7890","type":"http"}]}`
	req := settingsRawJSONRequest(http.MethodPut, "/settings/system-proxy", body)
	rec := settingsPerform(h.UpdateSystemProxy, req)

	settingsAssertStatus(t, rec, http.StatusServiceUnavailable)
}

func TestGetSystemProxy_NilConfFile(t *testing.T) {
	h := settingsHandlerWithoutConfig()

	rec := settingsPerform(h.GetSystemProxy, settingsTestRequest(http.MethodGet, "/settings/system-proxy", ""))

	settingsAssertStatus(t, rec, http.StatusOK)

	nc := settingsDecodeJSON[systemProxyConfig](t, rec)
	if len(nc.Proxies) != 0 {
		t.Fatalf("expected no proxies without conf file, got %d", len(nc.Proxies))
	}
}

func TestGetGeneral_Defaults(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)
	rec := settingsPerform(h.GetGeneral, settingsTestRequest(http.MethodGet, "/settings/general", ""))

	settingsAssertStatus(t, rec, http.StatusOK)
	cfg := settingsDecodeJSON[generalConfig](t, rec)
	if cfg.Tasks.SiteRefreshCron != "0 */15 * * *" {
		t.Fatalf("unexpected site refresh cron: %s", cfg.Tasks.SiteRefreshCron)
	}
	if cfg.Log.Level != "info" || cfg.Log.RetentionDays != 30 {
		t.Fatalf("unexpected log defaults: %+v", cfg.Log)
	}
	if cfg.Security.SessionLifetimeHours != 24 {
		t.Fatalf("unexpected security defaults: %+v", cfg.Security)
	}
}

func TestGetGeneral_DefaultPayloadShape(t *testing.T) {
	h := settingsHandlerWithoutConfig()
	rec := settingsPerform(h.GetGeneral, settingsTestRequest(http.MethodGet, "/settings/general", ""))

	settingsAssertStatus(t, rec, http.StatusOK)
	body := settingsDecodeJSON[map[string]json.RawMessage](t, rec)
	for _, key := range []string{"tasks", "ip_whitelist", "log", "data", "security"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("expected %q in general payload: %#v", key, body)
		}
	}
	var data struct {
		RequestDetailCleanupEnabled bool `json:"request_detail_cleanup_enabled"`
		RequestDetailRetentionDays  int  `json:"request_detail_retention_days"`
	}
	if err := json.Unmarshal(body["data"], &data); err != nil {
		t.Fatalf("decode data section: %v", err)
	}
	if !data.RequestDetailCleanupEnabled || data.RequestDetailRetentionDays != 90 {
		t.Fatalf("unexpected data defaults: %+v", data)
	}
}

func TestUpdateGeneral_PersistsConfig(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)
	body := `{
		"tasks":{"site_refresh_cron":"*/15 * * * *","newapi_checkin_cron":"0 8 * * *"},
		"ip_whitelist":{"enabled":true,"entries":["192.168.1.10","10.0.0.0/24"]},
		"log":{"level":"debug","cleanup_enabled":true,"retention_days":14},
		"security":{"session_lifetime_hours":168}
	}`
	req := settingsRawJSONRequest(http.MethodPut, "/settings/general", body)
	rec := settingsPerform(h.UpdateGeneral, req)

	settingsAssertStatus(t, rec, http.StatusOK)
	cfg := settingsDecodeJSON[generalConfig](t, rec)
	if cfg.Tasks.SiteRefreshCron != "*/15 * * * *" {
		t.Fatalf("unexpected site refresh cron: %s", cfg.Tasks.SiteRefreshCron)
	}
	if !cfg.IPWhitelist.Enabled || len(cfg.IPWhitelist.Entries) != 2 {
		t.Fatalf("unexpected whitelist config: %+v", cfg.IPWhitelist)
	}
	if cfg.Security.SessionLifetimeHours != 168 {
		t.Fatalf("unexpected security config: %+v", cfg.Security)
	}

	rec = settingsPerform(h.GetGeneral, settingsTestRequest(http.MethodGet, "/settings/general", ""))
	settingsAssertStatus(t, rec, http.StatusOK)
	persisted := settingsDecodeJSON[generalConfig](t, rec)
	if persisted.Log.RetentionDays != 14 {
		t.Fatalf("general config was not persisted: %+v", persisted.Log)
	}
	if persisted.Security.SessionLifetimeHours != 168 {
		t.Fatalf("general security config was not persisted: %+v", persisted.Security)
	}
}

func TestUpdateGeneral_InvalidBody(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)
	assertSettingsInvalidJSON(t, h.UpdateGeneral, http.MethodPut, "/settings/general", `{"tasks":`)
}

func TestUpdateGeneral_InvalidDataRetentionMapsErrorCode(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)
	req := settingsRawJSONRequest(http.MethodPut, "/settings/general", `{
		"tasks":{"site_refresh_cron":"0 */6 * * *","newapi_checkin_cron":"0 9 * * *"},
		"ip_whitelist":{"enabled":false,"entries":[]},
		"log":{"level":"info","cleanup_enabled":true,"retention_days":30},
		"data":{"request_detail_cleanup_enabled":true,"request_detail_retention_days":-7},
		"security":{"session_lifetime_hours":24}
	}`)
	rec := settingsPerform(h.UpdateGeneral, req)

	assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_general_config")
}

func TestUpdateGeneral_NilConfFile(t *testing.T) {
	h := settingsHandlerWithoutConfig()
	rec := settingsPerform(h.UpdateGeneral, settingsRawJSONRequest(http.MethodPut, "/settings/general", `{}`))

	assertSettingsErrorCode(t, rec, http.StatusServiceUnavailable, "config_unavailable")
}

func TestUpdateAutomaticBackupMasksAndPreservesSecrets(t *testing.T) {
	confFile := settingsConfigFile(t)
	h := settingsBackupHandler(confFile, nil)

	body := `{
		"enabled":true,
		"cron":"0 3 * * *",
		"retention_count":7,
		"endpoint":"https://s3.example.com",
		"region":"us-east-1",
		"bucket":"xlyra",
		"prefix":"prod",
		"access_key":"access-key",
		"secret_key":"super-secret-key",
		"backup_passphrase":"backup-passphrase",
		"force_path_style":true,
		"use_ssl":true,
		"skip_tls_verify":false
	}`
	req := settingsRawJSONRequest(http.MethodPut, "/settings/backup/automatic", body)
	rec := settingsPerform(h.UpdateAutomaticBackup, req)

	settingsAssertStatus(t, rec, http.StatusOK)
	first := settingsDecodeJSON[struct {
		AutomaticBackup struct {
			SecretKeyMasked        string `json:"secret_key_masked"`
			BackupPassphraseMasked string `json:"backup_passphrase_masked"`
			HasSecretKey           bool   `json:"has_secret_key"`
			HasBackupPassphrase    bool   `json:"has_backup_passphrase"`
		} `json:"automatic_backup"`
	}](t, rec)
	if !first.AutomaticBackup.HasSecretKey || !first.AutomaticBackup.HasBackupPassphrase {
		t.Fatalf("expected saved secret flags: %#v", first.AutomaticBackup)
	}
	if strings.Contains(first.AutomaticBackup.SecretKeyMasked, "super-secret-key") || strings.Contains(first.AutomaticBackup.BackupPassphraseMasked, "backup-passphrase") {
		t.Fatalf("plain secrets leaked in response: %#v", first.AutomaticBackup)
	}

	raw, ok := confFile.Get(config.AutomaticBackupConfigPath)
	if !ok {
		t.Fatal("expected persisted automatic backup config")
	}
	persisted := config.AutomaticBackupConfigFromRaw(raw)
	secretEncrypted := persisted.Storage.SecretKeyEncrypted
	passphraseEncrypted := persisted.Storage.BackupPassphraseEncrypted
	if secretEncrypted == "" || passphraseEncrypted == "" {
		t.Fatalf("expected encrypted secrets to be stored: %#v", persisted.Storage)
	}

	body = `{
		"enabled":true,
		"cron":"0 4 * * *",
		"retention_count":9,
		"endpoint":"https://s3.example.com",
		"region":"us-east-1",
		"bucket":"xlyra",
		"prefix":"prod",
		"access_key":"access-key",
		"secret_key":"",
		"backup_passphrase":"",
		"force_path_style":true,
		"use_ssl":true,
		"skip_tls_verify":false
	}`
	req = settingsRawJSONRequest(http.MethodPut, "/settings/backup/automatic", body)
	rec = settingsPerform(h.UpdateAutomaticBackup, req)
	settingsAssertStatus(t, rec, http.StatusOK)
	raw, _ = confFile.Get(config.AutomaticBackupConfigPath)
	persisted = config.AutomaticBackupConfigFromRaw(raw)
	if persisted.Storage.SecretKeyEncrypted != secretEncrypted || persisted.Storage.BackupPassphraseEncrypted != passphraseEncrypted {
		t.Fatalf("expected empty update to preserve encrypted secrets")
	}
	if persisted.Cron != "0 4 * * *" || persisted.RetentionCount != 9 {
		t.Fatalf("expected non-secret settings to update: %#v", persisted)
	}
}

func TestUpdateGeneral_InvalidWhitelist(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)
	body := `{
		"tasks":{"site_refresh_cron":"*/15 * * * *","newapi_checkin_cron":"0 8 * * *"},
		"ip_whitelist":{"enabled":true,"entries":["not-an-ip"]},
		"log":{"level":"info","cleanup_enabled":true,"retention_days":14}
	}`
	req := settingsRawJSONRequest(http.MethodPut, "/settings/general", body)
	rec := settingsPerform(h.UpdateGeneral, req)

	settingsAssertStatus(t, rec, http.StatusBadRequest)
}

func TestUpdateGeneral_ZeroSessionLifetimeMeansNeverExpires(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)
	body := `{
		"tasks":{"site_refresh_cron":"*/15 * * * *","newapi_checkin_cron":"0 8 * * *"},
		"ip_whitelist":{"enabled":false,"entries":[]},
		"log":{"level":"info","cleanup_enabled":true,"retention_days":14},
		"security":{"session_lifetime_hours":0}
	}`
	req := settingsRawJSONRequest(http.MethodPut, "/settings/general", body)
	rec := settingsPerform(h.UpdateGeneral, req)

	settingsAssertStatus(t, rec, http.StatusOK)
	cfg := settingsDecodeJSON[generalConfig](t, rec)
	if cfg.Security.SessionLifetimeHours != 0 {
		t.Fatalf("unexpected security config: %+v", cfg.Security)
	}
}

func TestUpdateGeneral_InvalidSessionLifetime(t *testing.T) {
	h, _ := settingsHandlerWithConfig(t)
	for _, lifetime := range []int{-1, 721} {
		body := fmt.Sprintf(`{
			"tasks":{"site_refresh_cron":"*/15 * * * *","newapi_checkin_cron":"0 8 * * *"},
			"ip_whitelist":{"enabled":false,"entries":[]},
			"log":{"level":"info","cleanup_enabled":true,"retention_days":14},
			"security":{"session_lifetime_hours":%d}
		}`, lifetime)
		rec := settingsPerform(h.UpdateGeneral, settingsRawJSONRequest(http.MethodPut, "/settings/general", body))

		settingsAssertStatus(t, rec, http.StatusBadRequest)
	}
}

func TestGetRateLimitsDefaultsWithoutService(t *testing.T) {
	h := settingsHandlerWithoutConfig()
	rec := settingsPerform(h.GetRateLimits, settingsTestRequest(http.MethodGet, "/settings/rate-limits", ""))

	settingsAssertStatus(t, rec, http.StatusOK)
	body := settingsDecodeJSON[rateLimitConfigEnvelope](t, rec)
	if body.RateLimit.Status != store.RateLimitStatusDisabled {
		t.Fatalf("expected disabled rate limits, got %+v", body.RateLimit)
	}
	if body.RateLimit.RPMLimit != nil || body.RateLimit.TPMLimit != nil {
		t.Fatalf("expected empty rate limits, got %+v", body.RateLimit)
	}
}

func TestUpdateRateLimitsRequiresService(t *testing.T) {
	h := settingsHandlerWithoutConfig()
	req := settingsRawJSONRequest(http.MethodPut, "/settings/rate-limits", `{"rate_limit":{"status":"enabled"}}`)
	rec := settingsPerform(h.UpdateRateLimits, req)

	assertSettingsErrorCode(t, rec, http.StatusServiceUnavailable, "rate_limit_unavailable")
}

func TestRateLimitConfigPayloadDefaultsStatusAndKeepsLimits(t *testing.T) {
	rpm := int64(60)
	tpm := int64(1000)

	payload := rateLimitConfigPayload(ratelimit.Config{RPMLimit: &rpm, TPMLimit: &tpm})

	if payload.Status != store.RateLimitStatusDisabled {
		t.Fatalf("expected disabled fallback, got %q", payload.Status)
	}
	if payload.RPMLimit == nil || *payload.RPMLimit != rpm {
		t.Fatalf("unexpected rpm limit: %+v", payload.RPMLimit)
	}
	if payload.TPMLimit == nil || *payload.TPMLimit != tpm {
		t.Fatalf("unexpected tpm limit: %+v", payload.TPMLimit)
	}
}

func TestSystemProxyAuth(t *testing.T) {
	parsed, err := url.Parse("socks5://alice:secret@127.0.0.1:1080")
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}
	auth := systemProxyAuth(parsed)
	if auth == nil || auth.User != "alice" || auth.Password != "secret" {
		t.Fatalf("unexpected proxy auth: %#v", auth)
	}

	parsed, err = url.Parse("socks5://127.0.0.1:1080")
	if err != nil {
		t.Fatalf("parse proxy url without auth: %v", err)
	}
	if auth := systemProxyAuth(parsed); auth != nil {
		t.Fatalf("expected nil auth, got %#v", auth)
	}
	if auth := systemProxyAuth(nil); auth != nil {
		t.Fatalf("expected nil auth for nil url, got %#v", auth)
	}
}

func TestSystemProxyTestHTTPClientRejectsUnsupportedType(t *testing.T) {
	parsed, err := url.Parse("http://127.0.0.1:7890")
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	client, err := systemProxyTestHTTPClient(proxyConfig{Type: "ssh"}, parsed)

	if err == nil {
		t.Fatalf("expected unsupported proxy type error, client=%#v", client)
	}
	if !strings.Contains(err.Error(), "unsupported proxy type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSystemProxyTestHTTPClientConfiguresHTTPProxy(t *testing.T) {
	parsed, err := url.Parse("http://proxy.local:8080")
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	client, err := systemProxyTestHTTPClient(proxyConfig{Type: "HTTP"}, parsed)
	if err != nil {
		t.Fatalf("system proxy http client: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	req := settingsTestRequest(http.MethodGet, systemProxyTestURL, "")
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("proxy func: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != parsed.String() {
		t.Fatalf("proxy url = %v, want %s", proxyURL, parsed)
	}
	if client.Timeout != systemProxyTestTimeout {
		t.Fatalf("client timeout = %s, want %s", client.Timeout, systemProxyTestTimeout)
	}
}

func TestTestSystemProxyRejectsInvalidProxyWithoutNetwork(t *testing.T) {
	h := settingsHandlerWithoutConfig()
	req := settingsRawJSONRequest(http.MethodPost, "/settings/system-proxy/test", `{"proxy":{"id":"","name":"Local","type":"http","url":"http://127.0.0.1:7890"}}`)
	rec := settingsPerform(h.TestSystemProxy, req)

	settingsAssertStatus(t, rec, http.StatusOK)
	body := settingsDecodeJSON[systemProxyTestResponse](t, rec)
	if body.OK || body.Stage != "validate" {
		t.Fatalf("expected validation failure, got %+v", body)
	}
}

func TestTestSystemProxyInvalidBody(t *testing.T) {
	h := settingsHandlerWithoutConfig()
	assertSettingsInvalidJSON(t, h.TestSystemProxy, http.MethodPost, "/settings/system-proxy/test", `{"proxy":`)
}

func TestAutomaticBackupHandlersRequireService(t *testing.T) {
	h := settingsHandlerWithoutConfig()
	cases := []struct {
		name   string
		method string
		target string
		body   string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{name: "get", method: http.MethodGet, target: "/settings/backup/automatic", handle: h.GetAutomaticBackup},
		{name: "update", method: http.MethodPut, target: "/settings/backup/automatic", body: `{}`, handle: h.UpdateAutomaticBackup},
		{name: "test", method: http.MethodPost, target: "/settings/backup/automatic/test", handle: h.TestAutomaticBackup},
		{name: "list files", method: http.MethodGet, target: "/settings/backup/automatic/files", handle: h.ListAutomaticBackupFiles},
		{name: "run", method: http.MethodPost, target: "/settings/backup/automatic/run", handle: h.RunAutomaticBackup},
		{name: "restore", method: http.MethodPost, target: "/settings/backup/automatic/files/restore", body: `{"key":"backup.xlyra"}`, handle: h.RestoreAutomaticBackupFile},
		{name: "active restore", method: http.MethodGet, target: "/settings/backup/automatic/files/restore/active", handle: h.GetActiveAutomaticBackupRestoreTask},
		{name: "restore status", method: http.MethodGet, target: "/settings/backup/automatic/files/restore/task-id", handle: h.GetAutomaticBackupRestoreTask},
		{name: "cancel restore", method: http.MethodDelete, target: "/settings/backup/automatic/files/restore/task-id", handle: h.CancelAutomaticBackupRestoreTask},
		{name: "delete", method: http.MethodDelete, target: "/settings/backup/automatic/files", body: `{"key":"backup.xlyra"}`, handle: h.DeleteAutomaticBackupFile},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := settingsMaybeJSONRequest(tc.method, tc.target, tc.body)
			rec := settingsPerform(tc.handle, req)

			assertSettingsErrorCode(t, rec, http.StatusServiceUnavailable, "backup_unavailable")
		})
	}
}

func TestExportBackupRequiresDownloadService(t *testing.T) {
	h := settingsBackupHandler(nil, nil)
	req := settingsRawJSONRequest(http.MethodPost, "/settings/backup/export", `{"passphrase":"secret"}`)
	rec := settingsPerform(h.ExportBackup, req)

	assertSettingsErrorCode(t, rec, http.StatusServiceUnavailable, "download_service_unavailable")
}

func TestImportBackupRejectsNonMultipartUpload(t *testing.T) {
	h := settingsBackupHandler(nil, nil)
	req := settingsTestRequest(http.MethodPost, "/settings/backup/import", "not multipart")
	rec := settingsPerform(h.ImportBackup, req)

	assertSettingsErrorCode(t, rec, http.StatusBadRequest, "invalid_backup_upload")
}

func TestWriteBackupErrorMapsMessages(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "unavailable", err: errors.New("backup service is not available"), status: http.StatusServiceUnavailable, code: "backup_unavailable"},
		{name: "passphrase required", err: errors.New("passphrase is required"), status: http.StatusBadRequest, code: "backup_passphrase_required"},
		{name: "invalid passphrase", err: errors.New("decrypt backup payload: authentication failed"), status: http.StatusBadRequest, code: "backup_passphrase_invalid"},
		{name: "unsupported", err: errors.New("unsupported backup version"), status: http.StatusBadRequest, code: "backup_unsupported"},
		{name: "default", err: errors.New("archive failed"), status: http.StatusBadRequest, code: "backup_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Handler{}

			assertSettingsErrorWriterCode(t, "/settings/backup/export", func(w http.ResponseWriter, r *http.Request) {
				h.writeBackupError(w, r, tc.err)
			}, tc.status, tc.code)
		})
	}
}

func TestWriteAutomaticBackupErrorMapsMessages(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "running", err: backup.ErrAutomaticAlreadyRunning, status: http.StatusConflict, code: "automatic_backup_running"},
		{name: "restore task not found", err: backup.ErrRestoreTaskNotFound, status: http.StatusNotFound, code: "automatic_restore_task_not_found"},
		{name: "restore cannot cancel", err: backup.ErrRestoreCannotCancel, status: http.StatusConflict, code: "automatic_restore_cannot_cancel"},
		{name: "unavailable", err: errors.New("storage is not available"), status: http.StatusServiceUnavailable, code: "backup_unavailable"},
		{name: "disabled", err: errors.New("automatic backup is disabled"), status: http.StatusBadRequest, code: "automatic_backup_disabled"},
		{name: "required", err: errors.New("bucket is required"), status: http.StatusBadRequest, code: "automatic_backup_config_required"},
		{name: "invalid passphrase", err: errors.New("decrypt backup payload: authentication failed"), status: http.StatusBadRequest, code: "backup_passphrase_invalid"},
		{name: "unsupported", err: errors.New("unsupported backup version"), status: http.StatusBadRequest, code: "backup_unsupported"},
		{name: "default", err: errors.New("upload failed"), status: http.StatusBadRequest, code: "automatic_backup_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Handler{}

			assertSettingsErrorWriterCode(t, "/settings/backup/automatic/test", func(w http.ResponseWriter, r *http.Request) {
				h.writeAutomaticBackupError(w, r, tc.err)
			}, tc.status, tc.code)
		})
	}
}
