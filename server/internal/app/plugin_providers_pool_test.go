package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestConvertAccountPoolSiteProvider(t *testing.T) {
	plugin := &jsplugin.Plugin{Manifest: jsplugin.Manifest{
		ID:   "acme.example",
		Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType: "acme.example/pool",
			Display:  jsplugin.SiteProviderDisplay{Name: jsplugin.LocalizedText{"zh": "账号池"}, Subtitle: jsplugin.LocalizedText{"zh": "插件"}},
			Hosts:    []string{"api.example.cn", "api.example.com"},
			Credential: jsplugin.SiteProviderCredential{
				Model: "account_pool",
				Fields: []jsplugin.SiteProviderField{
					{Name: "access_token", Secret: true},
					{Name: "realm"},
				},
				Identity: jsplugin.Sources("field.realm"),
			},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://api.example.com/v1",
				DefaultEndpointTypes: []string{"openai"},
				RequestParams:        json.RawMessage(`{"forced":{"stream":true}}`),
			},
			Regions:       []jsplugin.SiteProviderRegion{{ID: "cn", BaseURL: "https://api.example.cn/v1"}},
			DefaultRegion: "cn",
			Login:         &jsplugin.SiteProviderLogin{Kind: "custom_poll"},
			Refresh:       &jsplugin.SiteProviderRefresh{Kind: "none"},
			Import:        &jsplugin.SiteProviderImport{Enabled: true},
			Request: jsplugin.SiteProviderRequest{
				Headers: jsplugin.HeaderMap(map[string]string{"X-Realm": "{{cred.realm}}"}), StripDownstreamHeaders: true,
				UpstreamStreamOnly: true, BaseURLFrom: "region",
			},
		},
	}}
	prepared, err := convertSiteProvider(plugin)
	if err != nil {
		t.Fatal(err)
	}
	plugin.Manifest.SiteProvider.Regions[0].BaseURL = "https://evil.example"
	if prepared.item.Pool == nil || prepared.item.Pool.DefaultBaseURL() != "https://api.example.cn/v1" || prepared.item.Pool.Login.Kind != "custom_poll" {
		t.Fatalf("pool = %#v", prepared.item.Pool)
	}
	if prepared.item.QuotaAggregate != "" || prepared.item.Pool.QuotaAggregate != "sum" {
		t.Fatalf("quota aggregate item %q pool %q", prepared.item.QuotaAggregate, prepared.item.Pool.QuotaAggregate)
	}
	if prepared.item.Descriptor.OfficialBaseURL != "https://api.example.cn/v1" || prepared.item.Descriptor.RequiresBaseURL {
		t.Fatalf("descriptor = %#v", prepared.item.Descriptor)
	}
	if !prepared.item.Request.UpstreamStreamOnly || prepared.item.Request.BaseURLFrom != "region" || prepared.item.Request.Headers["X-Realm"].Value != "{{cred.realm}}" {
		t.Fatalf("request = %#v", prepared.item.Request)
	}
	var spec struct {
		Protocol string          `json:"protocol"`
		Official string          `json:"official_base_url"`
		Path     string          `json:"path"`
		Params   json.RawMessage `json:"request_params"`
	}
	if err := json.Unmarshal(prepared.spec, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Protocol != "openai_chat" || spec.Official != "https://api.example.com/v1" || spec.Path != "/v1/chat/completions" || string(spec.Params) != `{"forced":{"stream":true}}` {
		t.Fatalf("spec = %s", prepared.spec)
	}
	if types := prepared.module.SiteTypes(); len(types) != 1 || types[0] != "acme.example/pool" {
		t.Fatalf("site types = %#v", types)
	}
	provider, ok := adapter.AsDefaultBaseURLProvider(prepared.module)
	if !ok || provider.DefaultBaseURL() != "https://api.example.cn/v1" {
		t.Fatalf("default base = %v %q", ok, provider.DefaultBaseURL())
	}
	if caps := adapter.ModuleCapabilities(prepared.module); len(caps) != 0 {
		t.Fatalf("capabilities = %#v", caps)
	}
	if _, ok := prepared.module.(interface {
		ListModels(context.Context, adapter.SiteConfig, string) ([]adapter.Model, error)
	}); ok {
		t.Fatal("placeholder lists models")
	}
	if _, ok := prepared.module.(adapter.CredentialValidator); ok {
		t.Fatal("placeholder validates credentials")
	}

	apiKey, err := convertSiteProvider(testProviderPlugin("good.example", "good.example/chat", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Example"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if apiKey.item.Pool != nil || len(adapter.ModuleCapabilities(apiKey.module)) == 0 {
		t.Fatalf("api_key module = pool %v caps %#v", apiKey.item.Pool != nil, adapter.ModuleCapabilities(apiKey.module))
	}

	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{prepared.item}); err != nil {
		t.Fatal(err)
	}
	prepared.item.Pool.Regions[0].BaseURL = "https://mutated.example"
	got, ok := site.LookupSiteType("acme.example/pool")
	if !ok || got.CredentialCategory != "account" || got.APICredentialType != "account" || got.OfficialBaseURL != "https://api.example.cn/v1" || got.Accounts == nil || got.Accounts.LoginKind != "custom_poll" || !got.Accounts.SupportsImport || !got.Accounts.SupportsRegions {
		t.Fatalf("installed = %#v accounts %#v", got, got.Accounts)
	}

	svc := site.NewService(&store.Store{}, "test-master-key")
	if err := svc.SetPluginAdapterModules([]adapter.Module{prepared.module}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.SetPluginAdapterModules(nil) })
	_, _, err = svc.Create(t.Context(), site.CreateSiteParams{
		Name: "Pool", Slug: "pool", SiteType: "acme.example/pool", BaseURL: "https://evil.example",
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") || strings.Contains(err.Error(), "unsupported site_type") || strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("create = %v", err)
	}
	_, _, err = svc.Update(t.Context(), site.UpdateSiteParams{
		ID: uuid.New(), Name: "Pool", Slug: "pool", SiteType: "acme.example/pool", BaseURL: "https://api.example.com/v1",
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") || strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("update = %v", err)
	}
}
