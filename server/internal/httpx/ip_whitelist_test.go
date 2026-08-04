package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xlyra/server/internal/config"
)

func TestIPWhitelistAllowsConfiguredCIDR(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.DefaultGeneralConfig()
	cfg.IPWhitelist.Enabled = true
	cfg.IPWhitelist.Entries = []string{"10.0.0.0/24"}
	if err := confFile.Set(config.GeneralConfigPath, config.GeneralConfigToMap(cfg)); err != nil {
		t.Fatalf("set config: %v", err)
	}

	handler := IPWhitelist(confFile)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.42:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIPWhitelistRejectsOutsideCIDR(t *testing.T) {
	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg := config.DefaultGeneralConfig()
	cfg.IPWhitelist.Enabled = true
	cfg.IPWhitelist.Entries = []string{"10.0.0.0/24"}
	if err := confFile.Set(config.GeneralConfigPath, config.GeneralConfigToMap(cfg)); err != nil {
		t.Fatalf("set config: %v", err)
	}

	handler := IPWhitelist(confFile)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.10:12345"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ip_not_allowed") {
		t.Fatalf("expected ip_not_allowed response, got %s", rec.Body.String())
	}
}
