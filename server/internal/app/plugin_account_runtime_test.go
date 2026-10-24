package app

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"

	"xlyra/server/internal/gateway"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/site"
)

func TestApplyAccountPoolRuntime(t *testing.T) {
	svc := site.NewService(nil, "test-master-key")
	logger, logs := discardLogger()
	t.Cleanup(func() {
		applySiteProviders(nil, svc, logger)
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
	})

	compiled := compileAccountPoolPlugin(t)
	skipped := uncompiledAccountPoolPlugin()
	apiKey := testProviderPlugin("good.example", "good.example/chat", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Example"}, nil)
	applySiteProviders(jsplugin.NewRegistry(compiled, skipped, apiKey), svc, logger)

	provider, ok := svc.AccountProviderFor("acme.example/p38pool")
	if !ok || provider == nil {
		t.Fatal("compiled account pool has no account provider")
	}
	if _, ok := site.LookupSiteType("acme.skip/pool"); ok {
		t.Fatal("uncompiled account pool was published")
	}
	if !strings.Contains(logs.String(), "acme.skip") {
		t.Fatalf("skip was not logged: %s", logs.String())
	}
	if _, ok := svc.AccountProviderFor("good.example/chat"); ok {
		t.Fatal("api-key plugin exposed an account provider")
	}

	applySiteProviders(nil, svc, logger)
	if _, ok := svc.AccountProviderFor("acme.example/p38pool"); ok {
		t.Fatal("cleared snapshot still serves the account pool")
	}
	if _, ok := site.LookupSiteType("acme.example/p38pool"); ok {
		t.Fatal("cleared snapshot still publishes the site type")
	}
}

func TestAccountPoolRuntimeAdapter(t *testing.T) {
	plugin := compileAccountPoolPlugin(t)
	prepared, err := convertSiteProvider(plugin)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newAccountPoolRuntime(plugin, prepared.item.Pool)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	started, err := runtime.StartLogin(ctx, map[string]string{"region": "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != "pending" || started.ID == "" || started.VerificationURL != "https://api.example.cn/login" || started.Credential != nil {
		t.Fatalf("start = %#v", started)
	}
	poll, err := runtime.PollLogin(ctx, started.ID)
	if err != nil || poll.Status != "pending" || poll.Credential != nil {
		t.Fatalf("poll = %#v %v", poll, err)
	}
	if _, err := runtime.PollLogin(ctx, "other-process"); !errors.Is(err, jsplugin.ErrLoginSessionNotFound) {
		t.Fatalf("foreign session = %v", err)
	}

	const token = "ACCESS-TOKEN-plugin-9f3c"
	raw := []byte(`{"access_token":"` + token + `","refresh_token":"REFRESH-TOKEN-plugin-9f3c","subject":"user-1","realm":"cn"}`)
	imported, err := runtime.ParseImport(raw)
	if err != nil || len(imported) != 1 || imported[0].Fields["access_token"] != token || imported[0].Fields["subject"] != "user-1" {
		t.Fatalf("import = %#v %v", imported, err)
	}
	if _, err := runtime.ParseImport([]byte(`{"access_token":"` + token + `"}`)); err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("missing secret error = %v", err)
	}

	disabled := prepared.item.Pool.Clone()
	disabled.Import.Enabled = false
	closed, err := newAccountPoolRuntime(plugin, &disabled)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ParseImport(raw); !errors.Is(err, site.ErrAccountImportUnsupported) || strings.Contains(err.Error(), token) {
		t.Fatalf("disabled import = %v", err)
	}
}

func TestAccountPoolRuntimeBlocksMetadataDial(t *testing.T) {
	plugin := compileAccountPoolPlugin(t)
	runtime, err := newAccountPoolRuntime(plugin, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := accountPoolRuntimeClient(t, runtime)
	if client == nil || client.Transport == nil {
		t.Fatal("account pool client has no guarded transport")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.DialContext == nil {
		t.Fatal("account pool transport has no dial")
	}
	const token = "METADATA_TOKEN_9f3c"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := transport.DialContext(ctx, "tcp", "169.254.169.254:80")
	if conn != nil {
		_ = conn.Close()
		t.Fatal("metadata dial connected")
	}
	if err == nil || !strings.Contains(err.Error(), "blocked connection") {
		t.Fatalf("dial = %v", err)
	}
	if strings.Contains(err.Error(), "no route to host") || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "http://") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("dial error = %v", err)
	}
	if client.Timeout != 0 {
		t.Fatalf("client timeout = %s", client.Timeout)
	}
}

func accountPoolRuntimeClient(t *testing.T, runtime site.PluginAccountRuntime) *http.Client {
	t.Helper()
	adapter, ok := runtime.(accountPoolRuntimeAdapter)
	if !ok {
		t.Fatalf("runtime type %T", runtime)
	}
	field := reflect.ValueOf(adapter.runtime).Elem().FieldByName("client")
	if !field.IsValid() || field.IsNil() {
		return nil
	}
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface().(*http.Client)
}

func compileAccountPoolPlugin(t *testing.T) *jsplugin.Plugin {
	t.Helper()
	source := `export const meta = { apiVersion: 1, id: "acme.example", kind: "site_provider" };
export function startLogin() {
  return { result: { kind: "account_login", status: "pending", verificationURL: "https://api.example.cn/login", userCode: "USER-1", interval: 3 } };
}
export function pollLogin() {
  return { result: { kind: "account_login", status: "pending", interval: 3 } };
}
export function summarizeAccount() {
  return { result: { kind: "account_summary", status: "ok", remark: "pro", entries: [{ label: "credits", remaining: 1, limit: 2 }] } };
}
`
	section := accountPoolSection("acme.example/p38pool")
	manifest := jsplugin.Manifest{
		ID: "acme.example", Name: "Example", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindSiteProvider,
		SiteProvider: section,
		SHA256:       map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	plugin, err := jsplugin.NewPlugin(manifest, source, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	return plugin
}

func uncompiledAccountPoolPlugin() *jsplugin.Plugin {
	return &jsplugin.Plugin{Manifest: jsplugin.Manifest{
		ID: "acme.skip", Name: "Skip", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindSiteProvider,
		SiteProvider: accountPoolSection("acme.skip/pool"),
	}}
}

func accountPoolSection(siteType string) jsplugin.SiteProviderSection {
	return jsplugin.SiteProviderSection{
		SiteType: siteType,
		Display:  jsplugin.SiteProviderDisplay{Name: jsplugin.LocalizedText{"zh": "账号池", "en": "Pool"}},
		Hosts:    []string{"api.example.cn"},
		Credential: jsplugin.SiteProviderCredential{
			Model: "account_pool",
			Fields: []jsplugin.SiteProviderField{
				{Name: "access_token", Secret: true},
				{Name: "refresh_token", Secret: true},
				{Name: "subject"},
				{Name: "realm"},
			},
			AccessTokenField:  "access_token",
			RefreshTokenField: "refresh_token",
			Identity:          jsplugin.Sources("field.subject"),
		},
		Protocol: jsplugin.SiteProviderProtocol{
			Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://api.example.cn",
			DefaultEndpointTypes: []string{"openai"},
		},
		Regions:       []jsplugin.SiteProviderRegion{{ID: "cn", BaseURL: "https://api.example.cn"}},
		DefaultRegion: "cn",
		Login: &jsplugin.SiteProviderLogin{
			Kind:        "custom_poll",
			SecretPaths: map[string]string{"token": "access_token"},
		},
		Refresh: &jsplugin.SiteProviderRefresh{Kind: "none"},
		Import: &jsplugin.SiteProviderImport{
			Enabled: true,
			FieldMap: map[string][]string{
				"access_token":  {"access_token"},
				"refresh_token": {"refresh_token"},
				"subject":       {"subject"},
				"realm":         {"realm"},
			},
		},
	}
}
