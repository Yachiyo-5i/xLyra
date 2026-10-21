package jsplugin

import (
	"context"
	"strings"
	"testing"
	"time"
)

func providerPlugin(t *testing.T, id, extra string) *Plugin {
	t.Helper()
	source := meta(id, KindSiteProvider) + extra
	manifest := Manifest{
		ID: id, Name: id, Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: KindSiteProvider,
		SiteProvider: SiteProviderSection{
			SiteType:   id + "/chat",
			Display:    SiteProviderDisplay{Name: LocalizedText{"en": "Example"}},
			Hosts:      []string{"api.example.com"},
			Credential: SiteProviderCredential{Model: "api_key"},
			Protocol: SiteProviderProtocol{
				Kind:                 "openai_chat",
				Path:                 "/v1/chat/completions",
				OfficialBaseURL:      "https://api.example.com/v1",
				DefaultEndpointTypes: []string{"openai"},
			},
		},
		SHA256: map[string]string{"plugin.js": hashSource(source)},
	}
	plugin, err := NewPlugin(manifest, source, nil, 1)
	if err != nil {
		t.Fatalf("NewPlugin: %v", err)
	}
	return plugin
}

func TestSiteProviderBaseURLSettings(t *testing.T) {
	t.Parallel()
	mode, base := (Manifest{
		Kind: KindSiteProvider,
		SiteProvider: SiteProviderSection{Protocol: SiteProviderProtocol{
			OfficialBaseURL: "https://api.example.com/v1",
		}},
	}).BaseURLSettings()
	if mode != "as_is" || base != "https://api.example.com/v1" {
		t.Fatalf("site_provider settings = %q %q", mode, base)
	}
	mode, base = (Manifest{Kind: KindModelList, Site: SiteSection{BaseURLMode: "trim_v1", DefaultBaseURL: "https://models.example"}}).BaseURLSettings()
	if mode != "trim_v1" || base != "https://models.example" {
		t.Fatalf("model_list settings = %q %q", mode, base)
	}
	mode, base = (Manifest{Kind: KindQuotaProbe, QuotaProbe: QuotaProbeSection{BaseURLMode: "origin", DefaultBaseURL: "https://quota.example"}}).BaseURLSettings()
	if mode != "origin" || base != "https://quota.example" {
		t.Fatalf("quota_probe settings = %q %q", mode, base)
	}
	mode, base = (Manifest{Kind: KindQuotaProbe}).BaseURLSettings()
	if mode != "" || base != "" {
		t.Fatalf("empty quota settings = %q %q", mode, base)
	}
}

func TestCallSteppedHookDecodesSiteProviderHooks(t *testing.T) {
	t.Parallel()
	plugin := providerPlugin(t, "acme.example", `
export function listModels(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/models" } };
  return { result: { models: [{ name: "m1", displayName: "Model 1", capabilities: { vision: true } }] } };
}
export function check() { return { result: { status: "ok", message: "ready" } }; }
`)
	ctx := ProbeContext{SiteType: "acme.example/chat", BaseURL: "https://api.example.com", CredentialType: "api_key", Now: 5}
	decision, err := plugin.CallSteppedHook(context.Background(), "listModels", KindModelList, ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Request == nil || decision.Request.Method != "GET" || decision.Request.Path != "/v1/models" || decision.Result != nil || decision.Error != "" {
		t.Fatalf("request decision = %+v", decision)
	}
	decision, err = plugin.CallSteppedHook(context.Background(), "listModels", KindModelList, ctx, []ProbeStep{{
		Response: ProbeResponse{Status: 200, JSON: map[string]any{"data": []any{map[string]any{"id": "m1"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := decision.Result.(*ModelListResult)
	if !ok || len(list.Models) != 1 || list.Models[0].Name != "m1" || list.Models[0].DisplayName != "Model 1" || list.Models[0].Capabilities["vision"] != true {
		t.Fatalf("result = %#v", decision.Result)
	}
	decision, err = plugin.CallSteppedHook(context.Background(), "check", KindCredentialCheck, ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	check, ok := decision.Result.(*CredentialCheckResult)
	if !ok || check.Status != "ok" || check.Message != "ready" {
		t.Fatalf("check = %#v", decision.Result)
	}
}

func TestCallSteppedHookReportsErrorAndBadShape(t *testing.T) {
	t.Parallel()
	plugin := providerPlugin(t, "acme.example", `
export function listModels(ctx, steps) {
  if (steps.length === 0) return { error: "nope" };
  return { result: { models: [{ displayName: "missing name" }] }, request: { method: "GET", path: "/v1/models" } };
}
export function check() { return { result: { status: "maybe" } }; }
`)
	decision, err := plugin.CallSteppedHook(context.Background(), "listModels", KindModelList, ProbeContext{}, nil)
	if err != nil || decision.Error != "nope" || decision.Request != nil || decision.Result != nil {
		t.Fatalf("error decision = %+v, err = %v", decision, err)
	}
	if _, err := plugin.CallSteppedHook(context.Background(), "listModels", KindModelList, ProbeContext{}, []ProbeStep{{}}); err == nil || !strings.Contains(err.Error(), KindShape) {
		t.Fatalf("two outcomes error = %v", err)
	}
	if _, err := plugin.CallSteppedHook(context.Background(), "check", KindCredentialCheck, ProbeContext{}, nil); err == nil || !strings.Contains(err.Error(), KindShape) || !strings.Contains(err.Error(), "result.status") {
		t.Fatalf("bad status error = %v", err)
	}
	broken := providerPlugin(t, "acme.broken", `export function listModels() { throw new Error("boom"); }`)
	if _, err := broken.CallSteppedHook(context.Background(), "listModels", KindModelList, ProbeContext{}, nil); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("throw error = %v", err)
	}
}

func TestCallSteppedHookMissingAndKindGuard(t *testing.T) {
	t.Parallel()
	plugin := providerPlugin(t, "acme.example", `export function check() { return { result: { status: "ok" } }; }`)
	if plugin.HasHook("listModels") {
		t.Fatal("listModels was invented")
	}
	if _, err := plugin.CallSteppedHook(context.Background(), "listModels", KindModelList, ProbeContext{}, nil); err == nil || !strings.Contains(err.Error(), "listModels hook is missing") {
		t.Fatalf("missing hook error = %v", err)
	}
	if _, err := plugin.CallStepped(context.Background(), ProbeContext{}, nil); err == nil || !strings.Contains(err.Error(), "not a stepped kind") {
		t.Fatalf("CallStepped error = %v", err)
	}
	if _, err := plugin.CallSteppedHook(context.Background(), "check", KindQuotaProbe, ProbeContext{}, nil); err == nil || !strings.Contains(err.Error(), "not a stepped result kind") {
		t.Fatalf("result kind error = %v", err)
	}
}

func TestCallSteppedHookUsesProbeTimeout(t *testing.T) {
	t.Parallel()
	plugin := providerPlugin(t, "acme.example", `export function listModels() { for (;;) {} }`)
	started := time.Now()
	_, err := plugin.CallSteppedHook(context.Background(), "listModels", KindModelList, ProbeContext{}, nil)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), KindTimeout) {
		t.Fatalf("error = %v", err)
	}
	if elapsed < 100*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("elapsed %s, want the 200ms probe timeout", elapsed)
	}
}
