package admin

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestSitePayloadProviderStatus(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{
		{
			Descriptor: site.SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
			PluginID:   "acme.example",
			Enabled:    true,
			Hosts:      []string{"api.example.com"},
		},
		{
			Descriptor: site.SiteTypeDescriptor{SiteType: "paused.example/chat", DisplayName: "Paused"},
			PluginID:   "paused.example",
			Enabled:    false,
			Hosts:      []string{"api.paused.example"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	builtin := sitePayload(store.Site{
		ID: uuid.New(), Name: "OpenAI", Slug: "openai", SiteType: "openai",
		BaseURL: "https://api.openai.com/v1", Status: "active", Enabled: true,
		RoutingPriority: 1, CreatedAt: now, UpdatedAt: now,
	})
	raw, err := json.Marshal(builtin)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := builtin["provider_status"]; ok || strings.Contains(string(raw), "provider_status") {
		t.Fatalf("builtin payload changed: %s", raw)
	}

	enabled := Handler{}.sitePayloadWithCapabilities(store.Site{
		ID: uuid.New(), Name: "Acme", Slug: "acme", SiteType: " ACME.EXAMPLE/Chat ",
		BaseURL: "https://api.example.com/v1", Status: "active", Enabled: true,
		RoutingPriority: 1, CreatedAt: now, UpdatedAt: now,
	})
	if enabled["provider_status"] != "enabled" {
		t.Fatalf("enabled provider_status = %#v", enabled["provider_status"])
	}

	paused := Handler{}.sitePayloadWithState(store.Site{
		ID: uuid.New(), Name: "Paused", Slug: "paused", SiteType: "paused.example/chat",
		BaseURL: "https://api.paused.example/v1", Status: "active", Enabled: true,
		RoutingPriority: 1, CreatedAt: now, UpdatedAt: now,
	}, store.SiteState{})
	if paused["provider_status"] != "unavailable" {
		t.Fatalf("disabled provider_status = %#v", paused["provider_status"])
	}

	unknown := sitePayload(store.Site{
		ID: uuid.New(), Name: "Missing", Slug: "missing", SiteType: "missing.example/chat",
		BaseURL: "https://api.example.com/v1", Status: "active", Enabled: true,
		RoutingPriority: 1, CreatedAt: now, UpdatedAt: now,
	})
	if unknown["provider_status"] != "unavailable" {
		t.Fatalf("unknown provider_status = %#v", unknown["provider_status"])
	}
}
