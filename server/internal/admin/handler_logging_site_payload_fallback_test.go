package admin

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestHandlerLogWrappersAddAdminScope(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := Handler{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}

	handler.logInfo("created site", "site_id", "site-1")
	handler.logWarn("refresh failed", "error", "boom")

	logs := buf.String()
	for _, want := range []string{
		`level=INFO`,
		`msg="created site"`,
		`scope=admin`,
		`site_id=site-1`,
		`level=WARN`,
		`msg="refresh failed"`,
		`error=boom`,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs did not contain %q:\n%s", want, logs)
		}
	}
}

func TestHandlerLogWrappersIgnoreNilLogger(t *testing.T) {
	t.Parallel()

	handler := Handler{}

	handler.logInfo("ignored", "key", "value")
	handler.logWarn("ignored", "key", "value")
}

func sitePayloadFallbackFixture(t *testing.T, name, slug, siteType, baseURL, pathSuffix string) (*http.Request, store.Site) {
	t.Helper()

	siteID := uuid.New()
	return adminTestRequest(http.MethodGet, "/api/v1/sites/"+siteID.String()+pathSuffix, ""), store.Site{
		ID:       siteID,
		Name:     name,
		Slug:     slug,
		SiteType: siteType,
		BaseURL:  baseURL,
		Enabled:  true,
		Status:   "active",
	}
}

func TestSitePayloadWithStatsFallsBackWhenSitesServiceMissing(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req, site := sitePayloadFallbackFixture(t, "OpenAI Gateway", "openai-gateway", "openai", "https://api.example.test", "")

	payload := handler.sitePayloadWithStats(req, site)

	if payload["id"] != site.ID.String() || payload["name"] != "OpenAI Gateway" {
		t.Fatalf("base site fields missing from payload: %#v", payload)
	}
	if _, ok := payload["capabilities"]; ok {
		t.Fatalf("nil site service should not add capabilities: %#v", payload["capabilities"])
	}
	if _, ok := payload["model_count"]; ok {
		t.Fatalf("nil site service should not add model_count: %#v", payload["model_count"])
	}
	if _, ok := payload["api_key_count"]; ok {
		t.Fatalf("nil site service should not add api_key_count: %#v", payload["api_key_count"])
	}
}

func TestSitePayloadWithEditConfigFallsBackWhenSitesServiceMissing(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req, site := sitePayloadFallbackFixture(t, "Codex", "codex", "codex", "https://codex.example.test", "/edit")

	payload := handler.sitePayloadWithEditConfig(req, site)

	if payload["id"] != site.ID.String() || payload["site_type"] != "codex" {
		t.Fatalf("base site fields missing from edit payload: %#v", payload)
	}
	if _, ok := payload["auth_config"]; ok {
		t.Fatalf("nil site service should not add auth_config: %#v", payload["auth_config"])
	}
	if _, ok := payload["capabilities"]; ok {
		t.Fatalf("nil site service should not add capabilities: %#v", payload["capabilities"])
	}
}
