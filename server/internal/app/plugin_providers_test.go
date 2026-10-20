package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"xlyra/server/internal/gateway"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/site"
)

func TestSiteProviderText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text jsplugin.LocalizedText
		want string
	}{
		{name: "zh over en", text: jsplugin.LocalizedText{"en": "Example", "zh": "示例"}, want: "示例"},
		{name: "en over other", text: jsplugin.LocalizedText{"ja": "例", "en": "Example"}, want: "Example"},
		{name: "sorted key", text: jsplugin.LocalizedText{"ja": "例", "fr": "Exemple"}, want: "Exemple"},
		{name: "blank zh", text: jsplugin.LocalizedText{"zh": "  ", "en": "Example"}, want: "Example"},
		{name: "blank zh and en", text: jsplugin.LocalizedText{"zh": "", "en": "", "ja": "例"}, want: "例"},
		{name: "plain", text: jsplugin.LocalizedText{"": "Plain"}, want: "Plain"},
		{name: "empty", text: nil, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := siteProviderText(tc.text); got != tc.want {
				t.Fatalf("siteProviderText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSiteProviderSpecJSON(t *testing.T) {
	t.Parallel()
	raw, err := siteProviderSpecJSON(jsplugin.SiteProviderSection{
		Protocol: jsplugin.SiteProviderProtocol{
			Kind:            "openai_chat",
			Path:            "/v1/chat/completions",
			OfficialBaseURL: "https://api.example.com/v1",
			RequestParams:   json.RawMessage(`{"unsupported":["logit_bias"],"defaults":{"temperature":0}}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Protocol string          `json:"protocol"`
		Official string          `json:"official_base_url"`
		Path     string          `json:"path"`
		Params   json.RawMessage `json:"request_params"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Protocol != "openai_chat" || payload.Official != "https://api.example.com/v1" || payload.Path != "/v1/chat/completions" {
		t.Fatalf("spec = %s", raw)
	}
	if compactJSON(t, payload.Params) != `{"unsupported":["logit_bias"],"defaults":{"temperature":0}}` {
		t.Fatalf("request_params = %s", payload.Params)
	}

	omitted, err := siteProviderSpecJSON(jsplugin.SiteProviderSection{
		Protocol: jsplugin.SiteProviderProtocol{Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(omitted, []byte("request_params")) {
		t.Fatalf("empty params were kept: %s", omitted)
	}
	if _, err := siteProviderSpecJSON(jsplugin.SiteProviderSection{
		Protocol: jsplugin.SiteProviderProtocol{RequestParams: json.RawMessage(`{"temperature"`)},
	}); err == nil {
		t.Fatal("invalid request_params was accepted")
	}
}

func TestApplySiteProviders(t *testing.T) {
	svc := site.NewService(nil, "test-master-key")
	logger, logs := discardLogger()
	t.Cleanup(func() {
		siteProviderCurrent = nil
		applySiteProviders(nil, svc, logger)
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
	})
	openaiURL := gateway.OfficialBaseURLForProvider("openai")

	hosts := []string{"api.example.com", "files.example.com"}
	good := testProviderPlugin("good.example", "good.example/chat", hosts, "https://api.example.com/v1", json.RawMessage(`{"unsupported":["logit_bias"]}`), jsplugin.LocalizedText{"zh": "示例", "en": "Example"}, jsplugin.LocalizedText{"de": "Untertitel", "ja": "字幕"})
	other := testProviderPlugin("beta.example", "beta.example/embed", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Beta"}, nil)
	quota := &jsplugin.Plugin{Manifest: jsplugin.Manifest{ID: "acme.quota", Kind: jsplugin.KindQuotaProbe}}
	applySiteProviders(jsplugin.NewRegistry(other, quota, good), svc, logger)

	assertProviderInstalled(t, svc, "good.example/chat", "示例", "Untertitel", "good.example", hosts, "https://api.example.com/v1")
	assertProviderInstalled(t, svc, "beta.example/embed", "Beta", "", "beta.example", []string{"api.example.com"}, "https://api.example.com")
	if gateway.OfficialBaseURLForProvider("openai") != openaiURL {
		t.Fatalf("builtin official url = %q", gateway.OfficialBaseURLForProvider("openai"))
	}
	hosts[0] = "mutated.example"
	item, ok := site.LookupPluginSiteType("good.example/chat")
	if !ok || item.Hosts[0] != "api.example.com" {
		t.Fatalf("hosts aliased the manifest: %#v", item.Hosts)
	}
	good = testProviderPlugin("good.example", "good.example/chat", []string{"api.example.com", "files.example.com"}, "https://api.example.com/v1", json.RawMessage(`{"unsupported":["logit_bias"]}`), jsplugin.LocalizedText{"zh": "示例", "en": "Example"}, jsplugin.LocalizedText{"de": "Untertitel", "ja": "字幕"})

	logs.Reset()
	badHost := testProviderPlugin("bad.example", "bad.example/chat", []string{"api.example.com"}, "https://evil.example.com", nil, jsplugin.LocalizedText{"en": "Bad"}, nil)
	builtinName := testProviderPlugin("named.example", "openai", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Hijack"}, nil)
	badParams := testProviderPlugin("params.example", "params.example/chat", []string{"api.example.com"}, "https://api.example.com", json.RawMessage(`{"temperature":0}`), jsplugin.LocalizedText{"en": "Params"}, nil)
	applySiteProviders(jsplugin.NewRegistry(good, badHost, builtinName, badParams), svc, logger)
	assertProviderInstalled(t, svc, "good.example/chat", "示例", "Untertitel", "good.example", []string{"api.example.com", "files.example.com"}, "https://api.example.com/v1")
	if _, ok := site.LookupSiteType("bad.example/chat"); ok {
		t.Fatal("host mismatch was installed")
	}
	if _, ok := site.LookupSiteType("params.example/chat"); ok {
		t.Fatal("invalid request params were installed")
	}
	if desc, ok := site.LookupSiteType("openai"); !ok || desc.DisplayName != "OpenAI" {
		t.Fatalf("builtin openai = %#v, %v", desc, ok)
	}
	if _, ok := site.LookupPluginSiteType("beta.example/embed"); ok {
		t.Fatal("previous registry was kept")
	}
	logged := logs.String()
	for _, id := range []string{"bad.example", "named.example", "params.example"} {
		if !strings.Contains(logged, id) {
			t.Fatalf("log missing %s: %s", id, logged)
		}
	}

	first := testProviderPlugin("aaa.example", "aaa.example/chat", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "First"}, nil)
	second := testProviderPlugin("mmm.example", "aaa.example/chat", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Second"}, nil)
	logs.Reset()
	applySiteProviders(jsplugin.NewRegistry(second, first), svc, logger)
	assertProviderInstalled(t, svc, "aaa.example/chat", "First", "", "aaa.example", []string{"api.example.com"}, "https://api.example.com")
	if !strings.Contains(logs.String(), "mmm.example") {
		t.Fatalf("duplicate was not skipped: %s", logs.String())
	}

	applySiteProviders(jsplugin.NewRegistry(), svc, logger)
	assertProviderCleared(t, svc, "aaa.example/chat")
	applySiteProviders(nil, svc, logger)
	assertProviderCleared(t, svc, "good.example/chat")
	if _, ok := site.LookupSiteType("openai"); !ok {
		t.Fatal("clear dropped builtin site types")
	}

	applySiteProviders(jsplugin.NewRegistry(good), svc, logger)
	applySiteProviders(nil, nil, logger)
	assertProviderInstalled(t, svc, "good.example/chat", "示例", "Untertitel", "good.example", []string{"api.example.com", "files.example.com"}, "https://api.example.com/v1")
	applySiteProviders(nil, svc, nil)
	assertProviderCleared(t, svc, "good.example/chat")
}

func TestApplySiteProvidersSkipsStaleRegistry(t *testing.T) {
	svc := site.NewService(nil, "test-master-key")
	logger, _ := discardLogger()
	fresh := jsplugin.NewRegistry(testProviderPlugin("good.example", "good.example/chat", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Good"}, nil))
	stale := jsplugin.NewRegistry()
	previous := siteProviderCurrent
	t.Cleanup(func() {
		siteProviderCurrent = previous
		applySiteProviders(nil, svc, logger)
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
	})
	siteProviderCurrent = func() *jsplugin.Registry { return fresh }
	applySiteProviders(stale, svc, logger)
	if _, ok := site.LookupSiteType("good.example/chat"); ok {
		t.Fatal("stale registry was applied")
	}
	applySiteProviders(fresh, svc, logger)
	if _, ok := site.LookupSiteType("good.example/chat"); !ok {
		t.Fatal("current registry was skipped")
	}
}

func TestApplySiteProvidersConcurrent(t *testing.T) {
	svc := site.NewService(nil, "test-master-key")
	logger, _ := discardLogger()
	t.Cleanup(func() {
		applySiteProviders(nil, svc, logger)
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
	})
	registry := jsplugin.NewRegistry(testProviderPlugin("good.example", "good.example/chat", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"zh": "示例"}, nil))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			applySiteProviders(registry, svc, logger)
			applySiteProviders(nil, svc, logger)
		}()
	}
	wg.Wait()
	desc, ok := site.LookupSiteType("good.example/chat")
	if ok && desc.DisplayName != "示例" {
		t.Fatalf("descriptor = %#v", desc)
	}
	if ok && len(svc.Capabilities("good.example/chat")) == 0 {
		t.Fatal("site type is visible without an adapter")
	}
	if !ok && len(svc.Capabilities("good.example/chat")) != 0 {
		t.Fatal("adapter left behind after clear")
	}
}

func testProviderPlugin(id, siteType string, hosts []string, official string, params json.RawMessage, name, subtitle jsplugin.LocalizedText) *jsplugin.Plugin {
	return &jsplugin.Plugin{Manifest: jsplugin.Manifest{
		ID:   id,
		Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType: siteType,
			Display: jsplugin.SiteProviderDisplay{
				Name:     name,
				Subtitle: subtitle,
			},
			Hosts:      hosts,
			Credential: jsplugin.SiteProviderCredential{Model: "api_key"},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind:                 "openai_chat",
				Path:                 "/v1/chat/completions",
				OfficialBaseURL:      official,
				DefaultEndpointTypes: []string{"openai"},
				RequestParams:        params,
			},
			Models: jsplugin.SiteProviderModels{Path: "/v1/models", Auth: "bearer"},
		},
	}}
}

func assertProviderInstalled(t *testing.T, svc *site.Service, siteType, displayName, subtitle, pluginID string, hosts []string, official string) {
	t.Helper()
	desc, ok := site.LookupSiteType(siteType)
	if !ok {
		t.Fatalf("LookupSiteType(%q) missing", siteType)
	}
	if desc.DisplayName != displayName || desc.Subtitle != subtitle || desc.CredentialKey != siteType || desc.RequiresBaseURL || !desc.ShowInCreateDialog || desc.IconURL != "" || desc.OfficialBaseURL != official {
		t.Fatalf("descriptor = %#v", desc)
	}
	item, ok := site.LookupPluginSiteType(siteType)
	if !ok || item.PluginID != pluginID || !item.Enabled || strings.Join(item.Hosts, ",") != strings.Join(hosts, ",") {
		t.Fatalf("plugin site type = %#v, %v", item, ok)
	}
	if got := gateway.OfficialBaseURLForProvider(siteType); got != official {
		t.Fatalf("OfficialBaseURLForProvider = %q, want %q", got, official)
	}
	wantCapabilities := []string{"validate_credential", "list_models", "health_probe"}
	if got := svc.Capabilities(siteType); strings.Join(got, ",") != strings.Join(wantCapabilities, ",") {
		t.Fatalf("capabilities = %#v", got)
	}
}

func assertProviderCleared(t *testing.T, svc *site.Service, siteType string) {
	t.Helper()
	if _, ok := site.LookupSiteType(siteType); ok {
		t.Fatalf("%s still in the site type registry", siteType)
	}
	if _, ok := site.LookupPluginSiteType(siteType); ok {
		t.Fatalf("%s still in the plugin registry", siteType)
	}
	if got := svc.Capabilities(siteType); len(got) != 0 {
		t.Fatalf("capabilities = %#v", got)
	}
	if got := gateway.OfficialBaseURLForProvider(siteType); got != "" {
		t.Fatalf("official url = %q", got)
	}
}

func discardLogger() (*slog.Logger, *bytes.Buffer) {
	var logs bytes.Buffer
	return slog.New(slog.NewTextHandler(&logs, nil)), &logs
}

func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact %s: %v", raw, err)
	}
	return buf.String()
}
