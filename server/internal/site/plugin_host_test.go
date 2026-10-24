package site

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
)

func TestCheckPluginHost(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{
		{
			Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
			PluginID:   "acme.example",
			Enabled:    true,
			Hosts:      []string{"api.example.com", "files.example.com"},
		},
		{
			Descriptor: SiteTypeDescriptor{SiteType: "paused.example/chat", DisplayName: "Paused"},
			PluginID:   "paused.example",
			Enabled:    false,
			Hosts:      []string{"api.paused.example"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	hosts, ok := PluginSiteTypeHosts(" ACME.EXAMPLE/Chat ")
	if !ok || len(hosts) != 2 || hosts[0] != "api.example.com" || hosts[1] != "files.example.com" {
		t.Fatalf("hosts = %#v ok=%v", hosts, ok)
	}
	hosts[0] = "mutated.example"
	again, ok := PluginSiteTypeHosts("acme.example/chat")
	if !ok || again[0] != "api.example.com" {
		t.Fatalf("hosts copy = %#v", again)
	}
	if _, ok := PluginSiteTypeHosts("openai"); ok {
		t.Fatal("builtin returned hosts")
	}
	if _, ok := PluginSiteTypeHosts("paused.example/chat"); ok {
		t.Fatal("disabled plugin returned hosts")
	}
	if _, ok := PluginSiteTypeHosts("missing.example/chat"); ok {
		t.Fatal("unknown plugin returned hosts")
	}

	cases := []struct {
		name     string
		siteType string
		rawURL   string
		wantErr  bool
		host     string
		reason   string
	}{
		{name: "builtin", siteType: "openai", rawURL: "http://evil.test/v1"},
		{name: "builtin garbage url", siteType: "anthropic", rawURL: "not a url"},
		{name: "builtin empty", siteType: "deepseek", rawURL: ""},
		{name: "exact", siteType: "acme.example/chat", rawURL: "https://api.example.com/v1"},
		{name: "case and port", siteType: " ACME.EXAMPLE/Chat ", rawURL: "https://API.Example.COM:8443/v1/models"},
		{name: "userinfo ignored when host matches", siteType: "acme.example/chat", rawURL: "https://user:secret@api.example.com/v1"},
		{name: "second host", siteType: "acme.example/chat", rawURL: "http://files.example.com"},
		{name: "other subdomain", siteType: "acme.example/chat", rawURL: "https://evil.api.example.com/v1", wantErr: true, host: "evil.api.example.com"},
		{name: "sibling", siteType: "acme.example/chat", rawURL: "https://www.api.example.com/v1", wantErr: true, host: "www.api.example.com"},
		{name: "ip literal", siteType: "acme.example/chat", rawURL: "http://127.0.0.1/v1", wantErr: true, host: "127.0.0.1"},
		{name: "userinfo does not change host", siteType: "acme.example/chat", rawURL: "https://api.example.com@evil.test/v1", wantErr: true, host: "evil.test"},
		{name: "disabled", siteType: "paused.example/chat", rawURL: "https://api.paused.example/v1", wantErr: true, host: "api.paused.example", reason: "provider unavailable"},
		{name: "unknown slash", siteType: "missing.example/chat", rawURL: "https://api.example.com/v1", wantErr: true, host: "api.example.com", reason: "provider unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPluginHost(tc.siteType, tc.rawURL)
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("CheckPluginHost = %v", err)
				}
				return
			}
			var hostErr *HostNotAllowedError
			if !errors.As(err, &hostErr) {
				t.Fatalf("error = %v", err)
			}
			if hostErr.Host != tc.host || hostErr.Reason != tc.reason || !strings.Contains(hostErr.Error(), tc.host) {
				t.Fatalf("error = %#v", hostErr)
			}
		})
	}
}

func TestCreateUpdatePluginBaseURLHost(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"api.example.com", "files.example.com"},
	}}); err != nil {
		t.Fatal(err)
	}

	service := siteServiceWithUninitializedStore()
	module, err := adapter.NewGenericProvider(adapter.GenericProviderConfig{
		SiteType:        "acme.example/chat",
		OfficialBaseURL: "https://api.example.com",
		EndpointTypes:   []string{"openai"},
		AllowedHosts:    []string{"api.example.com", "files.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPluginAdapterModules([]adapter.Module{module}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.SetPluginAdapterModules(nil) })

	_, _, err = service.Create(t.Context(), CreateSiteParams{
		Name: "Acme", Slug: "acme", SiteType: "acme.example/chat", BaseURL: "https://evil.example/v1",
	})
	if err == nil || err.Error() != `base_url host "evil.example" is not allowed by plugin acme.example; allowed hosts: api.example.com, files.example.com` {
		t.Fatalf("create reject = %v", err)
	}
	_, _, err = service.Update(t.Context(), UpdateSiteParams{
		ID: uuid.New(), Name: "Acme", Slug: "acme", SiteType: " ACME.EXAMPLE/Chat ", BaseURL: "https://API.Evil.Example:443/v1",
	})
	if err == nil || err.Error() != `base_url host "api.evil.example" is not allowed by plugin acme.example; allowed hosts: api.example.com, files.example.com` {
		t.Fatalf("update reject = %v", err)
	}

	_, _, err = service.Create(t.Context(), CreateSiteParams{
		Name: "Acme", Slug: "acme", SiteType: "acme.example/chat", BaseURL: "https://API.Example.COM:8443/tenant",
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("create allow = %v", err)
	}
	_, _, err = service.Update(t.Context(), UpdateSiteParams{
		ID: uuid.New(), Name: "Acme", Slug: "acme", SiteType: "acme.example/chat", BaseURL: "https://user:secret@files.example.com/v1",
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("update allow = %v", err)
	}

	_, _, err = service.Create(t.Context(), CreateSiteParams{
		Name: "OpenAI", Slug: "openai-kept", SiteType: "openai", BaseURL: "http://127.0.0.1:9/v1",
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") || strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("builtin create = %v", err)
	}
	_, _, err = service.Update(t.Context(), UpdateSiteParams{
		ID: uuid.New(), Name: "OpenAI", Slug: "openai-kept", SiteType: "openai", BaseURL: "https://evil.example/v1",
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") || strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("builtin update = %v", err)
	}

	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    false,
		Hosts:      []string{"api.example.com"},
	}}); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Create(t.Context(), CreateSiteParams{
		Name: "Acme", Slug: "acme", SiteType: "acme.example/chat", BaseURL: "https://api.example.com",
	})
	if err == nil || err.Error() != `base_url host "api.example.com" is not allowed: plugin site type "acme.example/chat" is unavailable` {
		t.Fatalf("disabled create = %v", err)
	}

	other, err := adapter.NewGenericProvider(adapter.GenericProviderConfig{
		SiteType:        "missing.example/chat",
		OfficialBaseURL: "https://api.example.com",
		EndpointTypes:   []string{"openai"},
		AllowedHosts:    []string{"api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPluginAdapterModules([]adapter.Module{other}); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Update(t.Context(), UpdateSiteParams{
		ID: uuid.New(), Name: "Missing", Slug: "missing", SiteType: "missing.example/chat", BaseURL: "https://api.example.com",
	})
	if err == nil || err.Error() != `base_url host "api.example.com" is not allowed: plugin site type "missing.example/chat" is unavailable` {
		t.Fatalf("unknown slash update = %v", err)
	}
}
