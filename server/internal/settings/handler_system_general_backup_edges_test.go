package settings

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"xlyra/server/internal/backup"
	"xlyra/server/internal/config"
)

func TestUpdateSystemProxy_NormalizesBeforePersisting(t *testing.T) {
	t.Parallel()

	h, confFile := settingsHandlerWithConfig(t)
	req := settingsRawJSONRequest(http.MethodPut, "/settings/system-proxy", `{
		"proxies":[{"id":" local ","name":" Local Proxy ","type":" HTTPS ","url":" https://proxy.example:8443 "}]
	}`)
	rec := settingsPerform(h.UpdateSystemProxy, req)

	settingsAssertStatus(t, rec, http.StatusOK)
	raw, ok := confFile.Get("network")
	if !ok {
		t.Fatal("expected network config to be persisted")
	}
	persisted, ok := raw.(systemProxyConfig)
	if !ok {
		t.Fatalf("persisted network config = %T, want systemProxyConfig", raw)
	}
	if len(persisted.Proxies) != 1 {
		t.Fatalf("proxy count = %d, want 1", len(persisted.Proxies))
	}
	if got := persisted.Proxies[0]; got.ID != "local" || got.Name != "Local Proxy" || got.Type != "https" || got.URL != "https://proxy.example:8443" {
		t.Fatalf("persisted proxy was not normalized: %+v", got)
	}
}

func TestUpdateGeneral_NormalizesDefaultsBeforePersisting(t *testing.T) {
	t.Parallel()

	h, confFile := settingsHandlerWithConfig(t)
	req := settingsRawJSONRequest(http.MethodPut, "/settings/general", `{
		"tasks":{"site_refresh_cron":"  ","newapi_checkin_cron":" 0 9 * * * "},
		"ip_whitelist":{"enabled":true,"entries":[" 127.0.0.1 "," "," 10.0.0.0/8 "]},
		"log":{"level":" WARN ","cleanup_enabled":false,"retention_days":0},
		"data":{"request_detail_cleanup_enabled":false,"request_detail_retention_days":0},
		"security":{"session_lifetime_hours":24}
	}`)
	rec := settingsPerform(h.UpdateGeneral, req)

	settingsAssertStatus(t, rec, http.StatusOK)
	body := settingsDecodeJSON[generalConfig](t, rec)
	defaults := config.DefaultGeneralConfig()
	if body.Tasks.SiteRefreshCron != defaults.Tasks.SiteRefreshCron || body.Tasks.NewAPICheckinCron != "0 9 * * *" {
		t.Fatalf("unexpected normalized task config: %+v", body.Tasks)
	}
	if body.Log.Level != "warn" || body.Log.RetentionDays != defaults.Log.RetentionDays {
		t.Fatalf("unexpected normalized log config: %+v", body.Log)
	}
	if body.Data.RequestDetailRetentionDays != defaults.Data.RequestDetailRetentionDays {
		t.Fatalf("unexpected normalized data config: %+v", body.Data)
	}
	if got := strings.Join(body.IPWhitelist.Entries, ","); got != "127.0.0.1,10.0.0.0/8" {
		t.Fatalf("whitelist entries = %q, want trimmed non-empty entries", got)
	}

	raw, ok := confFile.Get(config.GeneralConfigPath)
	if !ok {
		t.Fatal("expected general config to be persisted")
	}
	persisted := config.GeneralConfigFromRaw(raw)
	if persisted.Log.Level != body.Log.Level || persisted.Log.RetentionDays != body.Log.RetentionDays {
		t.Fatalf("persisted log config = %+v, want %+v", persisted.Log, body.Log)
	}
}

func TestWriteAutomaticBackupError_MapsWrappedRunningError(t *testing.T) {
	t.Parallel()

	h := Handler{}
	assertSettingsErrorWriterCode(t, "/settings/backup/automatic/run", func(w http.ResponseWriter, r *http.Request) {
		h.writeAutomaticBackupError(w, r, errors.Join(errors.New("scheduler rejected task"), backup.ErrAutomaticAlreadyRunning))
	}, http.StatusConflict, "automatic_backup_running")
}
