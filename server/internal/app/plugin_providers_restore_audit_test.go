package app

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/gateway"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/site"
)

// A static module whose site type contains "/" makes the next snapshot fail
// inside SetPluginAdapterModules, after the protocol table has already been
// replaced. restoreSiteProviders has to put every published snapshot back.
func TestApplySiteProvidersRestoresThePreviousSnapshot(t *testing.T) {
	svc := site.NewService(nil, "test-master-key")
	logger, logs := discardLogger()
	previous := siteProviderCurrent
	t.Cleanup(func() {
		siteProviderCurrent = previous
		applySiteProviders(nil, svc, logger)
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
	})
	siteProviderCurrent = nil

	const (
		poolType = "acme.example/pool"
		chatType = "good.example/chat"
		clash    = "clash.example/chat"
	)
	pool := auditAccountPoolPlugin(t, "acme.example", poolType, "")
	chat := testProviderPlugin("good.example", chatType, []string{"api.example.com"}, "https://api.example.com/v1", nil, jsplugin.LocalizedText{"en": "Example"}, nil)
	applySiteProviders(jsplugin.NewRegistry(pool, chat), svc, logger)
	if _, ok := site.LookupPluginSiteType(poolType); !ok {
		t.Fatalf("pool site type was not installed: %s", logs.String())
	}
	if _, ok := site.LookupPluginSiteType(chatType); !ok {
		t.Fatal("api key site type was not installed")
	}
	if gateway.OfficialBaseURLForProvider(chatType) != "https://api.example.com/v1" {
		t.Fatalf("chat spec = %q", gateway.OfficialBaseURLForProvider(chatType))
	}
	if caps := svc.Capabilities(chatType); len(caps) == 0 {
		t.Fatal("api key module was not installed")
	}
	if errText := auditRuntimeProbe(svc, poolType); errText != "site id is required" {
		t.Fatalf("pool runtime probe = %q, log %s", errText, logs.String())
	}

	pinStaticModule(svc, clashModule{})
	logs.Reset()
	failed := testProviderPlugin("clash.example", clash, []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Clash"}, nil)
	applySiteProviders(jsplugin.NewRegistry(failed), svc, logger)

	if _, ok := site.LookupPluginSiteType(clash); ok {
		t.Fatal("failed snapshot published a site type")
	}
	if gateway.OfficialBaseURLForProvider(clash) != "" {
		t.Fatalf("failed snapshot left a protocol spec: %q", gateway.OfficialBaseURLForProvider(clash))
	}
	if caps := svc.Capabilities(clash); len(caps) != 0 {
		t.Fatalf("failed snapshot left a module: %#v", caps)
	}
	if _, ok := site.LookupPluginSiteType(poolType); !ok {
		t.Fatal("pool site type was not restored")
	}
	if _, ok := site.LookupPluginSiteType(chatType); !ok {
		t.Fatal("api key site type was not restored")
	}
	if gateway.OfficialBaseURLForProvider(chatType) != "https://api.example.com/v1" {
		t.Fatalf("restored chat spec = %q", gateway.OfficialBaseURLForProvider(chatType))
	}
	if gateway.OfficialBaseURLForProvider(poolType) == "" {
		t.Fatal("restored pool spec is missing")
	}
	if caps := svc.Capabilities(chatType); len(caps) == 0 {
		t.Fatal("api key module was not restored")
	}
	if errText := auditRuntimeProbe(svc, poolType); errText != "site id is required" {
		t.Fatalf("pool runtime was not restored: %q", errText)
	}
	if errText := auditRuntimeProbe(svc, clash); errText != "plugin account pool is not configured" {
		t.Fatalf("clash runtime = %q", errText)
	}
	if !strings.Contains(logs.String(), "site provider snapshot was not updated") {
		t.Fatalf("apply did not fail closed: %s", logs.String())
	}
}

type clashModule struct{}

func (clashModule) SiteTypes() []string { return []string{"clash.example/chat"} }

func pinStaticModule(svc *site.Service, module adapter.Module) {
	field := reflect.ValueOf(svc).Elem().FieldByName("adapters")
	registry := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Interface().(*adapter.Registry)
	registry.Register(module)
}

func auditRuntimeProbe(svc *site.Service, siteType string) string {
	_, err := svc.RefreshPluginAccountForGateway(context.Background(), uuid.Nil, uuid.Nil, siteType, nil)
	if err == nil {
		return ""
	}
	return err.Error()
}

func auditAccountPoolPlugin(t *testing.T, id, siteType, listModels string) *jsplugin.Plugin {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"" + id + "\", kind: \"site_provider\" };\n" +
		"export function summarizeAccount() { return { result: { status: \"ok\" } }; }\n" +
		"export function startLogin() { return { result: { status: \"pending\" } }; }\n" +
		"export function pollLogin() { return { result: { status: \"pending\" } }; }\n" +
		listModels
	section := jsplugin.SiteProviderSection{
		SiteType: siteType,
		Display:  jsplugin.SiteProviderDisplay{Name: jsplugin.LocalizedText{"en": "Pool"}},
		Hosts:    []string{"api.example.com"},
		Credential: jsplugin.SiteProviderCredential{
			Model: "account_pool",
			Fields: []jsplugin.SiteProviderField{
				{Name: "access_token", Secret: true},
				{Name: "subject"},
			},
			Identity: jsplugin.Sources("field.subject"),
		},
		Protocol: jsplugin.SiteProviderProtocol{
			Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://api.example.com",
			DefaultEndpointTypes: []string{"openai"},
		},
		Regions:       []jsplugin.SiteProviderRegion{{ID: "intl", BaseURL: "https://api.example.com"}},
		DefaultRegion: "intl",
		Login: &jsplugin.SiteProviderLogin{
			Kind:        "custom_poll",
			SecretPaths: map[string]string{"data.accessToken": "access_token"},
		},
		Refresh: &jsplugin.SiteProviderRefresh{Kind: "none"},
	}
	if listModels != "" {
		section.Models.PerAccount = true
	}
	manifest := jsplugin.Manifest{
		ID: id, Name: "Pool", Version: "1.0.0", APIVersion: 1, HostAPI: 1,
		Kind: jsplugin.KindSiteProvider, SiteProvider: section,
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	plugin, err := jsplugin.NewPlugin(manifest, source, nil, 1)
	if err != nil {
		t.Fatalf("plugin: %v", err)
	}
	return plugin
}
