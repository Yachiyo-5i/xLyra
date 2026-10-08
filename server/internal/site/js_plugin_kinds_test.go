package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

func kindsTestPlugin(t *testing.T, kind, id, source string) *jsplugin.Plugin {
	t.Helper()
	manifest := jsplugin.Manifest{
		ID: id, Name: id, Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: kind,
		Site:   jsplugin.SiteSection{BaseURLMode: "as_is", PricingPath: "/api/pricing"},
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}
	plugin, err := jsplugin.NewPlugin(manifest, source, nil, 1)
	if err != nil {
		t.Fatalf("NewPlugin(%s): %v", kind, err)
	}
	return plugin
}

func header(id, kind string) string {
	return `export const meta = { apiVersion: 1, id: "` + id + `", kind: "` + kind + `" };` + "\n"
}

// useCatalog makes the given plugins the active generation for one test.
func useCatalog(t *testing.T, plugins ...*jsplugin.Plugin) {
	t.Helper()
	catalog := jsplugin.DefaultCatalog()
	previous := catalog.Current()
	builtins, err := jsplugin.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	// Keep the builtins so tests running alongside still find their probes.
	catalog.Replace(&jsplugin.Snapshot{Gen: 9000, Registry: jsplugin.NewRegistry(append(builtins.Plugins(), plugins...)...)})
	t.Cleanup(func() {
		if previous != nil {
			catalog.Replace(previous)
		} else {
			_ = catalog.InitBuiltins()
		}
	})
}

func siteWithPlugins(plugins map[string]string) store.Site {
	meta, _ := json.Marshal(map[string]any{"gateway": map[string]any{"plugins": plugins}})
	return store.Site{Meta: meta}
}

const modelListSource = `
export function listModels(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/models" } };
  const rows = steps[0].response.json.data || [];
  return { result: { models: rows.map((m) => ({ name: m.id, displayName: m.title, capabilities: { vision: !!m.vision } })) } };
}`

func TestPluginModelListerSendsKeyAndConvertsModels(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"m-1","title":"Model One","vision":true},{"id":"m-2"}]}`))
	}))
	defer server.Close()
	plugin := kindsTestPlugin(t, jsplugin.KindModelList, "acme-models", header("acme-models", jsplugin.KindModelList)+modelListSource)

	models, err := pluginModelLister{plugin: plugin}.ListModels(context.Background(),
		adapter.SiteConfig{SiteType: "custom", BaseURL: server.URL, Client: server.Client()}, "sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if len(models) != 2 || models[0].UpstreamName != "m-1" || models[0].DisplayName != "Model One" || models[0].Capabilities["vision"] != true {
		t.Fatalf("models = %+v", models)
	}
	if models[1].DisplayName != "m-2" {
		t.Fatalf("display name should default to the model name, got %q", models[1].DisplayName)
	}
}

func TestModelListerForPrefersBoundPlugin(t *testing.T) {
	plugin := kindsTestPlugin(t, jsplugin.KindModelList, "acme-models", header("acme-models", jsplugin.KindModelList)+modelListSource)
	useCatalog(t, plugin)
	s := &Service{}
	lister, ok := s.modelListerFor(siteWithPlugins(map[string]string{"model_list": "acme-models"}), nil)
	if !ok {
		t.Fatal("bound plugin was not used")
	}
	if _, isPlugin := lister.(pluginModelLister); !isPlugin {
		t.Fatalf("lister = %T, want the plugin", lister)
	}
	// A binding to a plugin that is not enabled falls back to the adapter.
	if _, ok := s.boundSitePlugin(siteWithPlugins(map[string]string{"model_list": "gone"}), jsplugin.KindModelList); ok {
		t.Fatal("a missing plugin was treated as bound")
	}
	// A binding must point at a plugin of the right kind.
	if _, ok := s.boundSitePlugin(siteWithPlugins(map[string]string{"credential_check": "acme-models"}), jsplugin.KindCredentialCheck); ok {
		t.Fatal("a plugin of another kind was accepted")
	}
}

func TestCredentialCheckMapsStatusToFailureClass(t *testing.T) {
	source := func(status string) string {
		return header("acme-check", jsplugin.KindCredentialCheck) +
			`export function check() { return { result: { status: "` + status + `", message: "note" } }; }`
	}
	cases := map[string]func(upstream.Failure) bool{
		"ok":          nil,
		"invalid":     func(f upstream.Failure) bool { return f.CredentialInvalid() },
		"unavailable": func(f upstream.Failure) bool { return f.Transient() && !f.CredentialInvalid() },
	}
	for status, want := range cases {
		plugin := kindsTestPlugin(t, jsplugin.KindCredentialCheck, "acme-check", source(status))
		err := pluginCredentialValidator{plugin: plugin}.ValidateCredentials(context.Background(),
			adapter.SiteConfig{BaseURL: "https://x.example", Client: http.DefaultClient}, "k")
		if want == nil {
			if err != nil {
				t.Fatalf("status ok returned %v", err)
			}
			continue
		}
		if err == nil || !want(upstream.ClassifyError(err)) {
			t.Fatalf("status %s: err = %v, class = %v", status, err, upstream.ClassifyError(err).Class)
		}
	}
}

func TestSiteDetectRequestHasNoAuthorization(t *testing.T) {
	var sawAuth, called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		sawAuth = r.Header.Get("Authorization") != ""
		_, _ = w.Write([]byte(`{"system_name":"Acme"}`))
	}))
	defer server.Close()
	plugin := kindsTestPlugin(t, jsplugin.KindSiteDetect, "acme-detect", header("acme-detect", jsplugin.KindSiteDetect)+`
export function detect(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/status", headers: { Authorization: "Bearer stolen" } } };
  return { result: { matched: steps[0].response.json.system_name === "Acme", siteType: "newapi", confidence: 0.9 } };
}`)
	result, err := driveStepped(context.Background(), server.Client(), plugin, jsplugin.ProbeContext{BaseURL: server.URL}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !called || sawAuth {
		t.Fatalf("called = %v, Authorization sent = %v; a detector must send none, not even one it sets itself", called, sawAuth)
	}
	if detected := result.(*jsplugin.SiteDetectResult); !detected.Matched || detected.SiteType != "newapi" {
		t.Fatalf("result = %+v", detected)
	}
}

func TestSteppedPluginCannotLeaveTheSiteOrigin(t *testing.T) {
	plugin := kindsTestPlugin(t, jsplugin.KindModelList, "acme-models", header("acme-models", jsplugin.KindModelList)+`
export function listModels() { return { request: { method: "GET", path: "//evil.example/x" } }; }`)
	_, err := driveStepped(context.Background(), http.DefaultClient, plugin, jsplugin.ProbeContext{BaseURL: "https://good.example"}, "k")
	if err == nil || !strings.Contains(err.Error(), jsplugin.KindURLRejected) {
		t.Fatalf("err = %v, want %s", err, jsplugin.KindURLRejected)
	}
}

func TestModelMetadataOnlyFillsGaps(t *testing.T) {
	plugin := kindsTestPlugin(t, jsplugin.KindModelMetadata, "acme-meta", header("acme-meta", jsplugin.KindModelMetadata)+`
export function describeModels(ctx, input) {
  return { models: input.models.map((id) => ({ id, name: "Acme Vision", capabilities: { vision: true, reasoning: true } })) };
}`)
	useCatalog(t, plugin)
	s := &Service{}
	model := s.applyModelMetadataPlugins(context.Background(), store.Site{SiteType: "custom"}, adapter.Model{
		UpstreamName: "acme-vision-1",
		DisplayName:  "acme-vision-1",
		Capabilities: map[string]any{"vision": false},
	})
	if model.Capabilities["vision"] != false {
		t.Fatalf("plugin overrode a value the upstream gave: %+v", model.Capabilities)
	}
	if model.Capabilities["reasoning"] != true {
		t.Fatalf("plugin did not fill a missing capability: %+v", model.Capabilities)
	}
	if model.DisplayName != "Acme Vision" {
		t.Fatalf("display name = %q", model.DisplayName)
	}
	named := s.applyModelMetadataPlugins(context.Background(), store.Site{SiteType: "custom"}, adapter.Model{UpstreamName: "x", DisplayName: "Chosen name"})
	if named.DisplayName != "Chosen name" {
		t.Fatalf("a display name that was already set was replaced: %q", named.DisplayName)
	}
}

func TestDetectWithPluginsRequiresSupportedSiteType(t *testing.T) {
	// The adapters registry is empty in this test, so no site type is supported.
	plugin := kindsTestPlugin(t, jsplugin.KindSiteDetect, "acme-detect", header("acme-detect", jsplugin.KindSiteDetect)+`
export function detect() { return { result: { matched: true, siteType: "invented-type", confidence: 1 } }; }`)
	useCatalog(t, plugin)
	if _, ok := (&Service{}).detectWithPlugins(context.Background(), "https://x.example"); ok {
		t.Fatal("a plugin created a site type")
	}
}

func TestPricingSnapshotFromPlugin(t *testing.T) {
	ratio, price := 2.5, 0.01
	snapshot := pricingSnapshotFromPlugin(jsplugin.PricingParseResult{
		Groups: []jsplugin.PricingGroupEntry{{Name: "default", Ratio: &ratio}, {Name: "plain"}},
		Items:  []jsplugin.PricingItemEntry{{Model: "m", Group: "default", ModelRatio: &ratio, InputValue: &price}},
	}, map[string]any{"raw": true})
	if len(snapshot.Groups) != 2 || snapshot.Groups[0].Ratio != 2.5 || snapshot.Groups[1].Ratio != 1 {
		t.Fatalf("groups = %+v", snapshot.Groups)
	}
	item := snapshot.Items[0]
	if !item.HasModelRatio || item.ModelRatio != 2.5 || !item.HasInputValue || item.HasOutputValue || item.GroupRatio != 1 {
		t.Fatalf("item = %+v", item)
	}
}

func TestSitePluginsGatewayConfig(t *testing.T) {
	merged, err := MergeSiteGatewayConfig(nil, &GatewayConfig{Plugins: map[string]string{"model_list": "a", "bogus": "x", "credential_check": " b "}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := GatewayConfigFromSiteMeta(merged)
	if cfg.Plugins["model_list"] != "a" || cfg.Plugins["credential_check"] != "b" || len(cfg.Plugins) != 2 {
		t.Fatalf("plugins = %v; unknown kinds must be dropped and ids trimmed", cfg.Plugins)
	}
	// A patch without plugins leaves them alone.
	timeout := 1000
	merged, err = MergeSiteGatewayConfig(merged, &GatewayConfig{RequestTimeoutMS: &timeout})
	if err != nil {
		t.Fatal(err)
	}
	if got := GatewayConfigFromSiteMeta(merged).Plugins["model_list"]; got != "a" {
		t.Fatalf("an unrelated patch changed the plugins: %q", got)
	}
	// An empty id removes one kind and keeps the others.
	merged, err = MergeSiteGatewayConfig(merged, &GatewayConfig{Plugins: map[string]string{"model_list": ""}})
	if err != nil {
		t.Fatal(err)
	}
	cfg = GatewayConfigFromSiteMeta(merged)
	if _, present := cfg.Plugins["model_list"]; present || cfg.Plugins["credential_check"] != "b" {
		t.Fatalf("plugins after unbind = %v", cfg.Plugins)
	}
}
