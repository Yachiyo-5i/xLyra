package app

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"maps"
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

func TestPluginSiteTypeDisplayI18n(t *testing.T) {
	svc := site.NewService(nil, "test-master-key")
	logger, _ := discardLogger()
	t.Cleanup(func() {
		applySiteProviders(nil, svc, logger)
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
	})

	poolName := jsplugin.LocalizedText{"zh": "池", "en": " Pool ", "jp": "プール", "fr": "  ", "de": ""}
	poolSubtitle := jsplugin.LocalizedText{"zh": " \t ", "en": "Account pool", "ja": ""}
	chatName := jsplugin.LocalizedText{"en": "Sample", "zh": "示例", "fr": ""}
	chatSubtitle := jsplugin.LocalizedText{"zh": "对话", "en": "Chat", "jp": "チャット", "de": " "}
	applySiteProviders(jsplugin.NewRegistry(
		testProviderPlugin("acme.example", "acme.example/pool", []string{"api.example.com"}, "https://api.example.com", nil, poolName, poolSubtitle),
		testProviderPlugin("acme.sample", "acme.sample/chat", []string{"api.example.com"}, "https://api.example.com", nil, chatName, chatSubtitle),
	), svc, logger)
	poolName["zh"] = "changed"
	poolSubtitle["en"] = "changed"
	chatName["en"] = "changed"

	gotPool, ok := site.LookupSiteType("acme.example/pool")
	if !ok {
		t.Fatal("pool site type missing")
	}
	if gotPool.DisplayName != "池" || gotPool.Subtitle != "Account pool" {
		t.Fatalf("flattened pool text = %q / %q", gotPool.DisplayName, gotPool.Subtitle)
	}
	if !maps.Equal(gotPool.DisplayNameI18n, map[string]string{"zh": "池", "en": "Pool", "jp": "プール"}) {
		t.Fatalf("pool display_name_i18n = %#v", gotPool.DisplayNameI18n)
	}
	if !maps.Equal(gotPool.SubtitleI18n, map[string]string{"en": "Account pool"}) {
		t.Fatalf("pool subtitle_i18n = %#v", gotPool.SubtitleI18n)
	}

	gotChat, ok := site.LookupSiteType("acme.sample/chat")
	if !ok {
		t.Fatal("chat site type missing")
	}
	if gotChat.DisplayName != "示例" || gotChat.Subtitle != "对话" {
		t.Fatalf("flattened chat text = %q / %q", gotChat.DisplayName, gotChat.Subtitle)
	}
	if !maps.Equal(gotChat.DisplayNameI18n, map[string]string{"zh": "示例", "en": "Sample"}) {
		t.Fatalf("chat display_name_i18n = %#v", gotChat.DisplayNameI18n)
	}
	if !maps.Equal(gotChat.SubtitleI18n, map[string]string{"zh": "对话", "en": "Chat", "jp": "チャット"}) {
		t.Fatalf("chat subtitle_i18n = %#v", gotChat.SubtitleI18n)
	}

	for _, desc := range site.SiteTypeDescriptors() {
		if strings.Contains(desc.SiteType, "/") {
			continue
		}
		if desc.DisplayNameI18n != nil || desc.SubtitleI18n != nil {
			t.Fatalf("builtin %s has i18n maps: %#v %#v", desc.SiteType, desc.DisplayNameI18n, desc.SubtitleI18n)
		}
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

func TestConvertSiteProviderIconURL(t *testing.T) {
	svc := site.NewService(nil, "test-master-key")
	logger, _ := discardLogger()
	t.Cleanup(func() {
		siteProviderCurrent = nil
		applySiteProviders(nil, svc, logger)
		site.ClearPluginSiteTypes()
		gateway.ClearRuntimeProviderSpecs()
	})

	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1}
	with := compileIconProvider(t, "good.example", "assets/icon.png", png, "")
	without := compileIconProvider(t, "plain.example", "", nil, "")

	prepared, err := convertSiteProvider(with)
	if err != nil {
		t.Fatal(err)
	}
	sum := with.PackageSHA256()
	if len(sum) != 64 {
		t.Fatalf("package sha = %q", sum)
	}
	want := "/api/v1/plugin-icons/good.example?v=" + sum[:16]
	if prepared.item.Descriptor.IconURL != want {
		t.Fatalf("IconURL = %q, want %q", prepared.item.Descriptor.IconURL, want)
	}
	plain, err := convertSiteProvider(without)
	if err != nil {
		t.Fatal(err)
	}
	if plain.item.Descriptor.IconURL != "" {
		t.Fatalf("IconURL without icon = %q", plain.item.Descriptor.IconURL)
	}

	applySiteProviders(jsplugin.NewRegistry(with), svc, logger)
	desc, ok := site.LookupSiteType("good.example/chat")
	if !ok || desc.IconURL != want {
		t.Fatalf("LookupSiteType IconURL = %q, %v", desc.IconURL, ok)
	}
	if builtin, ok := site.LookupSiteType("openai"); !ok || builtin.IconURL != "/brand-icons/openai.png" {
		t.Fatalf("builtin icon = %#v, %v", builtin, ok)
	}

	next := compileIconProvider(t, "good.example", "assets/icon.png", png, "// next\n")
	if next.PackageSHA256() == sum {
		t.Fatal("package sha did not change")
	}
	nextWant := "/api/v1/plugin-icons/good.example?v=" + next.PackageSHA256()[:16]
	applySiteProviders(jsplugin.NewRegistry(next), svc, logger)
	desc, ok = site.LookupSiteType("good.example/chat")
	if !ok || desc.IconURL != nextWant {
		t.Fatalf("IconURL after repackage = %q, want %q", desc.IconURL, nextWant)
	}
}

func compileIconProvider(t *testing.T, id, iconName string, icon []byte, extra string) *jsplugin.Plugin {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"" + id + "\", kind: \"site_provider\" };\n" + extra
	manifest := jsplugin.Manifest{
		ID: id, Name: id, Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindSiteProvider,
		SiteProvider: jsplugin.SiteProviderSection{
			SiteType: id + "/chat",
			Display: jsplugin.SiteProviderDisplay{
				Name: jsplugin.LocalizedText{"zh": "示例"},
				Icon: iconName,
			},
			Hosts:      []string{"api.example.com"},
			Credential: jsplugin.SiteProviderCredential{Model: "api_key"},
			Protocol: jsplugin.SiteProviderProtocol{
				Kind: "openai_chat", Path: "/v1/chat/completions", OfficialBaseURL: "https://api.example.com/v1",
				DefaultEndpointTypes: []string{"openai"},
			},
		},
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"manifest.json": encoded,
		"plugin.js":     []byte(source),
	}
	if len(icon) > 0 {
		files[iconName] = icon
	}
	raw, err := jsplugin.BuildPackage(files)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := jsplugin.ReadPackage(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := jsplugin.CompilePackage(pkg)
	if err != nil {
		t.Fatal(err)
	}
	return plugin
}

func TestConvertSiteProviderCopiesRequestHeaders(t *testing.T) {
	plugin := testProviderPlugin("good.example", "good.example/chat", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Example"}, nil)
	plugin.Manifest.SiteProvider.Request = jsplugin.SiteProviderRequest{
		Headers:                jsplugin.HeaderMap(map[string]string{"Authorization": "Bearer {{cred.api_key}}", "X-Client": "xlyra"}),
		StripDownstreamHeaders: true,
	}
	prepared, err := convertSiteProvider(plugin)
	if err != nil {
		t.Fatal(err)
	}
	plugin.Manifest.SiteProvider.Request.Headers["X-Client"] = jsplugin.HeaderSpec{Value: "changed"}
	if prepared.item.Request.Headers["X-Client"].Value != "xlyra" || prepared.item.Request.Headers["Authorization"].Value != "Bearer {{cred.api_key}}" || !prepared.item.Request.StripDownstreamHeaders {
		t.Fatalf("request = %#v", prepared.item.Request)
	}
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{prepared.item}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(site.ClearPluginSiteTypes)
	prepared.item.Request.Headers["Authorization"] = jsplugin.HeaderSpec{Value: "mutated"}
	item, ok := site.LookupPluginSiteType("good.example/chat")
	if !ok || item.Request.Headers["Authorization"].Value != "Bearer {{cred.api_key}}" || item.Request.Headers["X-Client"].Value != "xlyra" {
		t.Fatalf("lookup request = %#v, %v", item.Request, ok)
	}
}

func TestConvertAPIKeyQuotaAggregate(t *testing.T) {
	plugin := testProviderPlugin("good.example", "good.example/chat", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Example"}, nil)
	plugin.Manifest.SiteProvider.Quota = &jsplugin.SiteProviderQuota{Aggregate: " min "}
	prepared, err := convertSiteProvider(plugin)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.item.Pool != nil || prepared.item.QuotaAggregate != "min" {
		t.Fatalf("aggregate = %q pool %#v", prepared.item.QuotaAggregate, prepared.item.Pool)
	}
	omitted := testProviderPlugin("good.example", "good.example/chat", []string{"api.example.com"}, "https://api.example.com", nil, jsplugin.LocalizedText{"en": "Example"}, nil)
	prepared, err = convertSiteProvider(omitted)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.item.QuotaAggregate != "" {
		t.Fatalf("omitted aggregate = %q", prepared.item.QuotaAggregate)
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
