package site

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
)

func TestSetPluginSiteTypesAccountPool(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)

	spec := accountPoolSpec()
	headers := jsplugin.HeaderMap(map[string]string{"Authorization": "Bearer {{cred.access_token}}"})
	if err := SetPluginSiteTypes([]PluginSiteType{
		{
			Descriptor: SiteTypeDescriptor{
				SiteType:                     " ACME.EXAMPLE/Pool ",
				DisplayName:                  "账号池",
				Subtitle:                     "插件账号池",
				CredentialKey:                "pool",
				APICredentialType:            "oauth",
				CredentialCategory:           "oauth",
				RequiresBaseURL:              true,
				OfficialBaseURL:              "https://wrong.example",
				ShowInCreateDialog:           true,
				SupportsAPIKeyCostMultiplier: true,
				Accounts:                     &AccountCapabilities{LoginKind: "oauth2_device_code"},
			},
			PluginID:       " acme.example ",
			Enabled:        true,
			Hosts:          []string{"api.example.cn"},
			Request:        PluginSiteRequest{Headers: headers, StripDownstreamHeaders: true, UpstreamStreamOnly: true, BaseURLFrom: "region"},
			QuotaAggregate: "min",
			Pool:           &spec,
		},
		{
			Descriptor: SiteTypeDescriptor{
				SiteType:           "acme.example/chat",
				DisplayName:        "Key",
				APICredentialType:  "oauth",
				CredentialCategory: "oauth",
				Accounts:           &AccountCapabilities{LoginKind: "custom_poll", SupportsImport: true},
			},
			PluginID:       "acme.example",
			Enabled:        true,
			Hosts:          []string{"api.example.com"},
			QuotaAggregate: "none",
		},
	}); err != nil {
		t.Fatal(err)
	}
	spec.Regions[0].BaseURL = "https://evil.example"
	spec.Regions[0].Label["zh"] = "改了"
	spec.Login.SecretPaths["data.accessToken"] = "mutated"
	spec.Import.FieldMap["access_token"][0] = "mutated"
	spec.Request.Headers["Authorization"] = jsplugin.HeaderSpec{Value: "mutated"}
	headers["Authorization"] = jsplugin.HeaderSpec{Value: "mutated"}

	desc, ok := LookupSiteType("acme.example/pool")
	if !ok || desc.APICredentialType != "account" || desc.CredentialCategory != "account" || desc.SupportsAPIKeyCostMultiplier || desc.RequiresBaseURL {
		t.Fatalf("descriptor = %#v", desc)
	}
	if desc.OfficialBaseURL != "https://api.example.cn" || desc.Accounts == nil || desc.Accounts.LoginKind != "custom_poll" || !desc.Accounts.SupportsImport || !desc.Accounts.SupportsRegions {
		t.Fatalf("accounts = %#v official %q", desc.Accounts, desc.OfficialBaseURL)
	}
	item, ok := LookupPluginSiteType(" ACME.EXAMPLE/Pool ")
	if !ok || item.PluginID != "acme.example" || item.Pool == nil || item.Pool.DefaultBaseURL() != "https://api.example.cn" {
		t.Fatalf("lookup = %#v", item.Pool)
	}
	if item.Pool.Login.SecretPaths["data.accessToken"] != "access_token" || item.Pool.Regions[0].Label["zh"] != "中国" || item.Pool.Import.FieldMap["access_token"][0] != "accessToken" {
		t.Fatalf("pool leaked input: %#v", item.Pool)
	}
	if item.QuotaAggregate != "" || item.Pool.QuotaAggregate != "sum" {
		t.Fatalf("quota aggregate item %q pool %q", item.QuotaAggregate, item.Pool.QuotaAggregate)
	}
	if item.Request.Headers["Authorization"].Value != "Bearer {{cred.access_token}}" || !item.Request.UpstreamStreamOnly || item.Request.BaseURLFrom != "region" {
		t.Fatalf("request = %#v", item.Request)
	}
	item.Pool.Regions[0].BaseURL = "https://mutated.example"
	again, _ := LookupPluginSiteType("acme.example/pool")
	if again.Pool.DefaultBaseURL() != "https://api.example.cn" {
		t.Fatalf("snapshot base = %q", again.Pool.DefaultBaseURL())
	}

	key, ok := LookupSiteType("acme.example/chat")
	if !ok || key.APICredentialType != "api_key" || key.CredentialCategory != "api_key" || key.Accounts != nil || key.SupportsAPIKeyCostMultiplier {
		t.Fatalf("api_key descriptor = %#v", key)
	}
	keyItem, ok := LookupPluginSiteType("acme.example/chat")
	if !ok || keyItem.QuotaAggregate != "none" || keyItem.Pool != nil {
		t.Fatalf("api_key aggregate = %#v", keyItem)
	}
	if CredentialTypeForSiteType("acme.example/pool") != "account" {
		t.Fatalf("category = %q", CredentialTypeForSiteType("acme.example/pool"))
	}
	if SupportsMultipleAPIKeys("acme.example/pool") || SupportsMultipleAPIKeys("grok") {
		t.Fatal("SupportsMultipleAPIKeys")
	}
	if !SupportsMultipleAPIKeys("acme.example/chat") {
		t.Fatal("api_key plugin lost multiple keys")
	}
}

func TestCreateUpdateAccountPoolBaseURL(t *testing.T) {
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	spec := accountPoolSpec()
	if err := SetPluginSiteTypes([]PluginSiteType{
		{
			Descriptor: SiteTypeDescriptor{SiteType: "acme.example/pool", DisplayName: "账号池"},
			PluginID:   "acme.example",
			Enabled:    true,
			Hosts:      []string{"api.example.cn"},
			Pool:       &spec,
		},
		{
			Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Key"},
			PluginID:   "acme.example",
			Enabled:    true,
			Hosts:      []string{"api.example.com"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	service := siteServiceWithUninitializedStore()
	poolModule, err := adapter.NewAccountPoolModule("acme.example/pool", "https://api.example.cn")
	if err != nil {
		t.Fatal(err)
	}
	keyModule, err := adapter.NewGenericProvider(adapter.GenericProviderConfig{
		SiteType: "acme.example/chat", OfficialBaseURL: "https://api.example.com",
		EndpointTypes: []string{"openai"}, AllowedHosts: []string{"api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetPluginAdapterModules([]adapter.Module{poolModule, keyModule}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.SetPluginAdapterModules(nil) })

	_, _, err = service.Create(t.Context(), CreateSiteParams{
		Name: "Pool", Slug: "pool", SiteType: "acme.example/pool", BaseURL: "https://evil.example/v1",
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") || strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("create pool = %v", err)
	}
	_, _, err = service.Update(t.Context(), UpdateSiteParams{
		ID: uuid.New(), Name: "Pool", Slug: "pool", SiteType: " ACME.EXAMPLE/Pool ", BaseURL: "https://api.example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") || strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("update pool = %v", err)
	}
	_, _, err = service.Create(t.Context(), CreateSiteParams{
		Name: "Key", Slug: "key", SiteType: "acme.example/chat", BaseURL: "https://evil.example/v1",
	})
	if err == nil || !strings.Contains(err.Error(), `base_url host "evil.example" is not allowed`) {
		t.Fatalf("create api_key = %v", err)
	}
}

func accountPoolSpec() jsplugin.AccountPoolSpec {
	return jsplugin.AccountPoolSpec{
		Credential: jsplugin.SiteProviderCredential{
			Model:            "account_pool",
			AccessTokenField: "access_token",
			Fields:           []jsplugin.SiteProviderField{{Name: "access_token", Secret: true, Type: "string"}, {Name: "realm", Type: "string"}},
		},
		Regions: []jsplugin.SiteProviderRegion{{
			ID: "cn", Label: jsplugin.LocalizedText{"zh": "中国"}, BaseURL: "https://api.example.cn",
		}},
		DefaultRegion: "cn",
		Login: jsplugin.SiteProviderLogin{
			Kind:        "custom_poll",
			SecretPaths: map[string]string{"data.accessToken": "access_token"},
		},
		Refresh:        jsplugin.SiteProviderRefresh{Kind: "custom_steps"},
		Import:         jsplugin.SiteProviderImport{Enabled: true, FieldMap: map[string][]string{"access_token": {"accessToken"}}},
		Selection:      "ordered",
		QuotaAggregate: "sum",
		Request:        jsplugin.SiteProviderRequest{Headers: jsplugin.HeaderMap(map[string]string{"Authorization": "Bearer {{cred.access_token}}"}), BaseURLFrom: "region"},
	}
}
