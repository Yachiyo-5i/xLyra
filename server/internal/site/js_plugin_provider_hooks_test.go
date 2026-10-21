package site

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
)

func providerHookPlugin(t *testing.T, id, body string) *jsplugin.Plugin {
	t.Helper()
	source := header(id, jsplugin.KindSiteProvider) + body
	manifest := jsplugin.Manifest{
		ID: id, Name: id, Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType:   id + "/chat",
			Display:    jsplugin.SiteProviderDisplay{Name: jsplugin.LocalizedText{"en": "Example"}},
			Hosts:      []string{"api.example.com"},
			Credential: jsplugin.SiteProviderCredential{Model: "api_key"},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind:                 "openai_chat",
				Path:                 "/v1/chat/completions",
				OfficialBaseURL:      "https://api.example.com/v1",
				DefaultEndpointTypes: []string{"openai"},
			},
		},
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	plugin, err := jsplugin.NewPlugin(manifest, source, nil, 1)
	if err != nil {
		t.Fatalf("NewPlugin: %v", err)
	}
	return plugin
}

func installProviderSite(t *testing.T, plugin *jsplugin.Plugin, enabled bool, hosts []string) {
	t.Helper()
	installProviderSiteMode(t, plugin, enabled, hosts, "")
}

func installProviderSiteMode(t *testing.T, plugin *jsplugin.Plugin, enabled bool, hosts []string, aggregate string) {
	t.Helper()
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{
			SiteType:        plugin.Manifest.SiteProvider.SiteType,
			OfficialBaseURL: plugin.Manifest.SiteProvider.Protocol.OfficialBaseURL,
		},
		PluginID:       plugin.Manifest.ID,
		Enabled:        enabled,
		Hosts:          append([]string(nil), hosts...),
		QuotaAggregate: aggregate,
	}}); err != nil {
		t.Fatal(err)
	}
}

func hostDialClient(server *httptest.Server, hosts ...string) *http.Client {
	allowed := map[string]struct{}{}
	for _, host := range hosts {
		allowed[host] = struct{}{}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if _, ok := allowed[host]; ok {
			addr = server.Listener.Addr().String()
		}
		return dialer.DialContext(ctx, network, addr)
	}
	if server.TLS != nil {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Transport: transport}
}

type countingModule struct {
	siteType string
	calls    *int
}

func (m countingModule) SiteTypes() []string { return []string{m.siteType} }

func (countingModule) Scope() adapter.HealthProbeScope { return adapter.HealthProbeCredentialScope }

func (m countingModule) ProbeHealth(context.Context, adapter.SiteConfig, string) ([]adapter.Model, error) {
	*m.calls++
	return []adapter.Model{{UpstreamName: "generic-model"}}, nil
}

func (m countingModule) ListModels(context.Context, adapter.SiteConfig, string) ([]adapter.Model, error) {
	*m.calls++
	return []adapter.Model{{UpstreamName: "generic-model"}}, nil
}

func (m countingModule) ValidateCredentials(context.Context, adapter.SiteConfig, string) error {
	*m.calls++
	return nil
}

const providerListBody = `
export function listModels(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/models" } };
  const rows = steps[0].response.json.data || [];
  return { result: { models: rows.map((m) => ({ name: m.id, displayName: m.title })) } };
}
export function check(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/models" } };
  if (steps[0].response.status !== 200) return { result: { status: "invalid", message: "rejected" } };
  return { result: { status: "ok", message: "ready" } };
}
`

func TestSiteProviderHooksListModelsAndCheck(t *testing.T) {
	var hits atomic.Int32
	var gotAuth string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m-1","title":"Model One"}]}`))
	}))
	t.Cleanup(server.Close)
	plugin := providerHookPlugin(t, "acme.example", providerListBody)
	useCatalog(t, plugin)
	installProviderSite(t, plugin, true, []string{"api.example.com"})
	client := hostDialClient(server, "api.example.com")
	siteCfg := adapter.SiteConfig{SiteType: "acme.example/chat", BaseURL: "https://api.example.com", Client: client}
	service := &Service{}

	lister, ok := service.modelListerFor(store.Site{SiteType: "acme.example/chat"}, countingModule{siteType: "acme.example/chat"})
	if !ok {
		t.Fatal("model lister missing")
	}
	if got, isPlugin := lister.(pluginModelLister); !isPlugin || got.hook != "listModels" || got.resultKind != jsplugin.KindModelList {
		t.Fatalf("lister = %#v", lister)
	}
	models, err := lister.ListModels(context.Background(), siteCfg, "sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || gotAuth != "Bearer sk-secret" || len(models) != 1 || models[0].UpstreamName != "m-1" || models[0].DisplayName != "Model One" {
		t.Fatalf("hits=%d auth=%q models=%+v", hits.Load(), gotAuth, models)
	}

	validator, ok := service.credentialValidatorFor(store.Site{SiteType: "acme.example/chat"}, countingModule{siteType: "acme.example/chat"})
	if !ok {
		t.Fatal("validator missing")
	}
	if got, isPlugin := validator.(pluginCredentialValidator); !isPlugin || got.hook != "check" || got.resultKind != jsplugin.KindCredentialCheck {
		t.Fatalf("validator = %#v", validator)
	}
	if err := validator.ValidateCredentials(context.Background(), siteCfg, "sk-secret"); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestSiteProviderHookHostAllowList(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m-1","title":"Model One"}]}`))
	}))
	t.Cleanup(server.Close)
	plugin := providerHookPlugin(t, "acme.example", providerListBody)
	useCatalog(t, plugin)
	installProviderSite(t, plugin, true, []string{"api.example.com"})
	client := hostDialClient(server, "api.example.com", "evil.example.com")
	lister := pluginModelLister{plugin: plugin, hook: "listModels", resultKind: jsplugin.KindModelList}

	_, err := lister.ListModels(context.Background(), adapter.SiteConfig{
		SiteType: "acme.example/chat", BaseURL: "https://evil.example.com", Client: client,
	}, "sk-secret")
	if err == nil || !strings.Contains(err.Error(), plugin.Manifest.ID) || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("err = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d", hits.Load())
	}

	_, err = lister.ListModels(context.Background(), adapter.SiteConfig{
		SiteType: "acme.example/chat", BaseURL: "https://api.example.com", Client: client,
	}, "sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestSiteProviderHookBindingPriorityAndFallback(t *testing.T) {
	provider := providerHookPlugin(t, "acme.example", `
export function listModels() { return { result: { models: [{ name: "from-hook" }] } }; }
export function check() { return { result: { status: "invalid", message: "from-hook" } }; }
`)
	boundModels := kindsTestPlugin(t, jsplugin.KindModelList, "bound.models", header("bound.models", jsplugin.KindModelList)+`
export function listModels() { return { result: { models: [{ name: "from-binding" }] } }; }
`)
	boundCheck := kindsTestPlugin(t, jsplugin.KindCredentialCheck, "bound.check", header("bound.check", jsplugin.KindCredentialCheck)+`
export function check() { return { result: { status: "ok", message: "from-binding" } }; }
`)
	useCatalog(t, provider, boundModels, boundCheck)
	installProviderSite(t, provider, true, []string{"api.example.com"})
	var calls int
	module := countingModule{siteType: "acme.example/chat", calls: &calls}
	service := &Service{}
	cfg := adapter.SiteConfig{SiteType: "acme.example/chat", BaseURL: "https://api.example.com", Client: http.DefaultClient}

	lister, ok := service.modelListerFor(store.Site{SiteType: "acme.example/chat"}, module)
	if !ok {
		t.Fatal("hook lister missing")
	}
	models, err := lister.ListModels(context.Background(), cfg, "k")
	if err != nil || len(models) != 1 || models[0].UpstreamName != "from-hook" || calls != 0 {
		t.Fatalf("models=%+v err=%v calls=%d", models, err, calls)
	}
	boundSite := siteWithPlugins(map[string]string{"model_list": "bound.models"})
	boundSite.SiteType = "acme.example/chat"
	lister, ok = service.modelListerFor(boundSite, module)
	if !ok {
		t.Fatal("bound lister missing")
	}
	models, err = lister.ListModels(context.Background(), cfg, "k")
	if err != nil || len(models) != 1 || models[0].UpstreamName != "from-binding" || calls != 0 {
		t.Fatalf("bound models=%+v err=%v calls=%d", models, err, calls)
	}

	validator, ok := service.credentialValidatorFor(store.Site{SiteType: "acme.example/chat"}, module)
	if !ok {
		t.Fatal("hook validator missing")
	}
	err = validator.ValidateCredentials(context.Background(), cfg, "k")
	if err == nil || !strings.Contains(err.Error(), "from-hook") || calls != 0 {
		t.Fatalf("hook check err=%v calls=%d", err, calls)
	}
	checkSite := siteWithPlugins(map[string]string{"credential_check": "bound.check"})
	checkSite.SiteType = "acme.example/chat"
	validator, ok = service.credentialValidatorFor(checkSite, module)
	if !ok {
		t.Fatal("bound validator missing")
	}
	if err := validator.ValidateCredentials(context.Background(), cfg, "k"); err != nil || calls != 0 {
		t.Fatalf("bound check err=%v calls=%d", err, calls)
	}

	bare := providerHookPlugin(t, "bare.example", "")
	useCatalog(t, bare)
	installProviderSite(t, bare, true, []string{"api.example.com"})
	if _, ok := service.siteTypeHookPlugin("bare.example/chat", "listModels"); ok {
		t.Fatal("missing listModels was bound")
	}
	if _, ok := service.siteTypeHookPlugin("bare.example/chat", "check"); ok {
		t.Fatal("missing check was bound")
	}
	fallback := countingModule{siteType: "bare.example/chat", calls: &calls}
	lister, ok = service.modelListerFor(store.Site{SiteType: "bare.example/chat"}, fallback)
	if !ok {
		t.Fatal("fallback lister missing")
	}
	if _, isPlugin := lister.(pluginModelLister); isPlugin {
		t.Fatal("missing hook stayed on the plugin")
	}
	models, err = lister.ListModels(context.Background(), cfg, "k")
	if err != nil || models[0].UpstreamName != "generic-model" || calls != 1 {
		t.Fatalf("fallback models=%+v err=%v calls=%d", models, err, calls)
	}
	validator, ok = service.credentialValidatorFor(store.Site{SiteType: "bare.example/chat"}, fallback)
	if !ok {
		t.Fatal("fallback validator missing")
	}
	if _, isPlugin := validator.(pluginCredentialValidator); isPlugin {
		t.Fatal("missing check stayed on the plugin")
	}
	if err := validator.ValidateCredentials(context.Background(), cfg, "k"); err != nil || calls != 2 {
		t.Fatalf("fallback check err=%v calls=%d", err, calls)
	}

	installProviderSite(t, provider, false, []string{"api.example.com"})
	useCatalog(t, provider)
	if _, ok := service.siteTypeHookPlugin("acme.example/chat", "listModels"); ok {
		t.Fatal("disabled site type was bound")
	}
	openaiCalls := 0
	openai := countingModule{siteType: "openai", calls: &openaiCalls}
	lister, ok = service.modelListerFor(store.Site{SiteType: "openai"}, openai)
	if !ok {
		t.Fatal("builtin lister missing")
	}
	if _, isPlugin := lister.(pluginModelLister); isPlugin {
		t.Fatal("builtin site used a plugin hook")
	}
	if _, err := lister.ListModels(context.Background(), adapter.SiteConfig{SiteType: "openai", BaseURL: "https://api.openai.com"}, "k"); err != nil || openaiCalls != 1 {
		t.Fatalf("builtin list err=%v calls=%d", err, openaiCalls)
	}
}

func TestProviderHookPlugin(t *testing.T) {
	provider := providerHookPlugin(t, "acme.example", `export function classify() { return { class: "limited" }; }`)
	bare := providerHookPlugin(t, "bare.example", `export function listModels() { return { result: { models: [] } }; }`)
	other := kindsTestPlugin(t, jsplugin.KindErrorClassifier, "other.example", header("other.example", jsplugin.KindErrorClassifier)+`
export function classify() { return { class: "limited" }; }`)
	useCatalog(t, provider, bare, other)
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{
		{Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat"}, PluginID: "acme.example", Enabled: true, Hosts: []string{"api.example.com"}},
		{Descriptor: SiteTypeDescriptor{SiteType: "paused.example/chat"}, PluginID: "acme.example", Enabled: false, Hosts: []string{"api.example.com"}},
		{Descriptor: SiteTypeDescriptor{SiteType: "bare.example/chat"}, PluginID: "bare.example", Enabled: true, Hosts: []string{"api.example.com"}},
		{Descriptor: SiteTypeDescriptor{SiteType: "other.example/chat"}, PluginID: "other.example", Enabled: true, Hosts: []string{"api.example.com"}},
		{Descriptor: SiteTypeDescriptor{SiteType: "gone.example/chat"}, PluginID: "gone.example", Enabled: true, Hosts: []string{"api.example.com"}},
	}); err != nil {
		t.Fatal(err)
	}
	registry := jsplugin.DefaultCatalog().Registry()
	service := &Service{}
	cases := []struct {
		name     string
		siteType string
		hook     string
		wantID   string
	}{
		{name: "enabled classify", siteType: "acme.example/chat", hook: "classify", wantID: "acme.example"},
		{name: "disabled", siteType: "paused.example/chat", hook: "classify"},
		{name: "other kind", siteType: "other.example/chat", hook: "classify"},
		{name: "missing hook", siteType: "bare.example/chat", hook: "classify"},
		{name: "other export", siteType: "acme.example/chat", hook: "rewritePayload"},
		{name: "builtin", siteType: "openai", hook: "classify"},
		{name: "not registered", siteType: "gone.example/chat", hook: "classify"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plugin, ok := ProviderHookPlugin(registry, tc.siteType, tc.hook)
			if tc.wantID == "" {
				if ok || plugin != nil {
					t.Fatalf("ProviderHookPlugin = %v, %v", pluginIDOf(plugin), ok)
				}
			} else if !ok || plugin == nil || plugin.Manifest.ID != tc.wantID || !plugin.HasHook(tc.hook) {
				t.Fatalf("ProviderHookPlugin = %v, %v", pluginIDOf(plugin), ok)
			}
			got, gotOK := service.siteTypeHookPlugin(tc.siteType, tc.hook)
			if gotOK != ok || pluginIDOf(got) != pluginIDOf(plugin) {
				t.Fatalf("siteTypeHookPlugin = %v, %v; ProviderHookPlugin = %v, %v", pluginIDOf(got), gotOK, pluginIDOf(plugin), ok)
			}
		})
	}
	if plugin, ok := ProviderHookPlugin(nil, "acme.example/chat", "classify"); ok || plugin != nil {
		t.Fatal("nil registry returned a plugin")
	}
}

func pluginIDOf(plugin *jsplugin.Plugin) string {
	if plugin == nil {
		return ""
	}
	return plugin.Manifest.ID
}

func TestSiteProviderHookFailureDoesNotFallBack(t *testing.T) {
	plugin := providerHookPlugin(t, "acme.example", `
export function listModels() { return { error: "broken list" }; }
export function check() { throw new Error("broken check"); }
`)
	useCatalog(t, plugin)
	installProviderSite(t, plugin, true, []string{"api.example.com"})
	var calls int
	module := countingModule{siteType: "acme.example/chat", calls: &calls}
	service := &Service{}
	cfg := adapter.SiteConfig{SiteType: "acme.example/chat", BaseURL: "https://api.example.com", Client: http.DefaultClient}

	lister, ok := service.modelListerFor(store.Site{SiteType: "acme.example/chat"}, module)
	if !ok {
		t.Fatal("lister missing")
	}
	if _, err := lister.ListModels(context.Background(), cfg, "k"); err == nil || !strings.Contains(err.Error(), "plugin "+plugin.Manifest.ID) || !strings.Contains(err.Error(), "broken list") || calls != 0 {
		t.Fatalf("list err=%v calls=%d", err, calls)
	}
	validator, ok := service.credentialValidatorFor(store.Site{SiteType: "acme.example/chat"}, module)
	if !ok {
		t.Fatal("validator missing")
	}
	if err := validator.ValidateCredentials(context.Background(), cfg, "k"); err == nil || !strings.Contains(err.Error(), "broken check") || calls != 0 {
		t.Fatalf("check err=%v calls=%d", err, calls)
	}
}

func TestSiteProviderCheckHookHealth(t *testing.T) {
	t.Cleanup(ClearPluginSiteTypes)
	plugin := providerHookPlugin(t, "acme.example", `
export function check(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/models" } };
  return { result: { status: "ok", message: "ready" } };
}
`)
	okPlugin := providerHookPlugin(t, "acme.example", `
export function check() { return { result: { status: "ok", message: "ready" } }; }
`)
	badPlugin := providerHookPlugin(t, "acme.example", `
export function check() { return { error: "broken check" }; }
`)
	bound := kindsTestPlugin(t, jsplugin.KindCredentialCheck, "bound.check", header("bound.check", jsplugin.KindCredentialCheck)+`
export function check() { return { result: { status: "ok", message: "from-binding" } }; }
`)
	item := store.Site{ID: uuid.New(), SiteType: "acme.example/chat", BaseURL: "https://api.example.com"}
	credential := siteEncryptedCredential(t, uuid.New(), item.ID, defaultCredentialType, "sk-secret", nil)

	run := func(t *testing.T, plugins []*jsplugin.Plugin, site store.Site, enabled bool, hosts []string) (siteHealthCheck, int) {
		t.Helper()
		useCatalog(t, plugins...)
		ClearPluginSiteTypes()
		if err := SetPluginSiteTypes([]PluginSiteType{{
			Descriptor: SiteTypeDescriptor{SiteType: "acme.example/chat"},
			PluginID:   "acme.example",
			Enabled:    enabled,
			Hosts:      hosts,
		}}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		service := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
			switch destination := tx.Statement.Dest.(type) {
			case *store.Site:
				*destination = site
				tx.RowsAffected = 1
			case *[]store.SiteCredential:
				*destination = []store.SiteCredential{credential}
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		})
		module := countingModule{siteType: "acme.example/chat", calls: &calls}
		if err := service.SetPluginAdapterModules([]adapter.Module{module}); err != nil {
			t.Fatal(err)
		}
		return service.runSiteHealthCheck(context.Background(), site), calls
	}

	t.Run("hook", func(t *testing.T) {
		check, calls := run(t, []*jsplugin.Plugin{okPlugin}, item, true, []string{"api.example.com"})
		if !check.success || calls != 0 || check.endpoint != "credential_check acme.example" || check.metadata["plugin"] != "acme.example" {
			t.Fatalf("check=%#v calls=%d", check, calls)
		}
	})
	t.Run("hook failure", func(t *testing.T) {
		check, calls := run(t, []*jsplugin.Plugin{badPlugin}, item, true, []string{"api.example.com"})
		if check.success || calls != 0 || check.endpoint != "credential_check acme.example" || !strings.Contains(check.message, "broken check") {
			t.Fatalf("check=%#v calls=%d", check, calls)
		}
	})
	t.Run("binding wins", func(t *testing.T) {
		site := siteWithPlugins(map[string]string{"credential_check": "bound.check"})
		site.ID = item.ID
		site.SiteType = item.SiteType
		site.BaseURL = item.BaseURL
		check, calls := run(t, []*jsplugin.Plugin{badPlugin, bound}, site, true, []string{"api.example.com"})
		if !check.success || calls != 0 || check.endpoint != "credential_check bound.check" {
			t.Fatalf("check=%#v calls=%d", check, calls)
		}
	})
	t.Run("host outside allow list", func(t *testing.T) {
		blocked := item
		blocked.BaseURL = "https://evil.example.com"
		check, calls := run(t, []*jsplugin.Plugin{plugin}, blocked, true, []string{"api.example.com"})
		if check.success || calls != 0 || !strings.Contains(check.message, "plugin acme.example") || !strings.Contains(check.message, "not allowed") {
			t.Fatalf("check=%#v calls=%d", check, calls)
		}
	})
	t.Run("no hook uses probe", func(t *testing.T) {
		bare := providerHookPlugin(t, "acme.example", "")
		check, calls := run(t, []*jsplugin.Plugin{bare}, item, true, []string{"api.example.com"})
		if !check.success || calls != 1 || check.endpoint == "credential_check acme.example" {
			t.Fatalf("check=%#v calls=%d", check, calls)
		}
	})
	t.Run("builtin unchanged", func(t *testing.T) {
		ClearPluginSiteTypes()
		service := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
			tx.AddError(gorm.ErrRecordNotFound)
		})
		check := service.runSiteHealthCheck(context.Background(), store.Site{
			ID: uuid.New(), SiteType: "openai", BaseURL: "https://api.openai.com",
		})
		if check.success || strings.Contains(check.endpoint, "credential_check") {
			t.Fatalf("builtin check = %#v", check)
		}
	})
}
