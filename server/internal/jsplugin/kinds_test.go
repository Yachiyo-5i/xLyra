package jsplugin

import (
	"context"
	"strings"
	"testing"

	"xlyra/server/internal/upstream"
)

func kindPlugin(t *testing.T, kind, id, source string) *Plugin {
	t.Helper()
	manifest := Manifest{
		ID: id, Name: id, Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: kind,
		Site:   SiteSection{PricingPath: "/api/pricing"},
		SHA256: map[string]string{"plugin.js": hashSource(source)},
	}
	plugin, err := NewPlugin(manifest, source, nil, 1)
	if err != nil {
		t.Fatalf("NewPlugin(%s): %v", kind, err)
	}
	return plugin
}

func meta(id, kind string) string {
	return `export const meta = { apiVersion: 1, id: "` + id + `", kind: "` + kind + `" };` + "\n"
}

func TestEveryKindHasHooksAndTimeout(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range kindSpecs {
		if seen[spec.Name] {
			t.Fatalf("kind %s listed twice", spec.Name)
		}
		seen[spec.Name] = true
		if len(spec.Hooks) == 0 || spec.Timeout <= 0 {
			t.Fatalf("kind %s has no hooks or timeout", spec.Name)
		}
	}
	if len(seen) != 8 {
		t.Fatalf("got %d kinds, want 8", len(seen))
	}
}

func TestUnknownKindListsSupportedKinds(t *testing.T) {
	err := validateManifest(Manifest{ID: "acme-x", APIVersion: 1, HostAPI: 1, Kind: "mine"}, "")
	if err == nil {
		t.Fatal("unknown kind accepted")
	}
	// The sha256 check runs first, so build a manifest that passes it.
	source := "x"
	err = validateManifest(Manifest{ID: "acme-x", APIVersion: 1, HostAPI: 1, Kind: "mine", SHA256: map[string]string{"plugin.js": hashSource(source)}}, source)
	if err == nil || !strings.Contains(err.Error(), "model_list") {
		t.Fatalf("error = %v, want the supported kinds listed", err)
	}
}

// Every kind is wired into xLyra and says how it takes effect.
func TestEveryKindIsConnectedAndBound(t *testing.T) {
	bindings := map[string]string{
		KindQuotaProbe: "quota", KindProtocol: "slug", KindModelList: "site", KindCredentialCheck: "site",
		KindSiteDetect: "global", KindErrorClassifier: "site", KindModelMetadata: "global", KindPricingParse: "site",
	}
	for _, spec := range kindSpecs {
		if !spec.Connected {
			t.Errorf("kind %s is not connected", spec.Name)
		}
		if spec.Binding != bindings[spec.Name] {
			t.Errorf("kind %s Binding = %q, want %q", spec.Name, spec.Binding, bindings[spec.Name])
		}
	}
}

func TestModelListSteps(t *testing.T) {
	source := meta("acme-models", KindModelList) + `
export function listModels(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/models" } };
  const rows = steps[0].response.json.data || [];
  return { result: { models: rows.map((m) => ({ name: m.id, displayName: m.title })) } };
}`
	plugin := kindPlugin(t, KindModelList, "acme-models", source)
	fixture := Fixture{
		Name: "ok",
		Ctx:  fixtureContext{SiteType: "custom", BaseURL: "https://x.example", CredentialType: "api_key", Now: 1},
		Responses: []FixtureResponse{{
			Match: FixtureMatch{Path: "/api/models"}, Status: 200,
			JSON: map[string]any{"data": []any{map[string]any{"id": "m1", "title": "Model 1"}}},
		}},
		Expect: fixtureExpectation{
			Requests: []FixtureMatch{{Path: "/api/models"}},
			Result:   map[string]any{"models": []any{map[string]any{"name": "m1", "displayName": "Model 1"}}},
		},
	}
	if err := plugin.RunFixture(context.Background(), fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Expect.Result = map[string]any{"models": []any{map[string]any{"name": "other"}}}
	if err := plugin.RunFixture(context.Background(), fixture); err == nil {
		t.Fatal("wrong expectation passed")
	}
}

func TestCredentialCheckRejectsUnknownStatus(t *testing.T) {
	source := meta("acme-check", KindCredentialCheck) + `
export function check() { return { result: { status: "maybe" } }; }`
	plugin := kindPlugin(t, KindCredentialCheck, "acme-check", source)
	_, err := plugin.CallStepped(context.Background(), ProbeContext{}, nil)
	if err == nil || !strings.Contains(err.Error(), KindShape) || !strings.Contains(err.Error(), "result.status") {
		t.Fatalf("error = %v, want a shape error naming result.status", err)
	}
}

func TestSteppedDecisionNeedsExactlyOne(t *testing.T) {
	source := meta("acme-detect", KindSiteDetect) + `
export function detect() { return { error: "a", result: { matched: true } }; }`
	plugin := kindPlugin(t, KindSiteDetect, "acme-detect", source)
	if _, err := plugin.CallStepped(context.Background(), ProbeContext{BaseURL: "https://x.example"}, nil); err == nil {
		t.Fatal("two outcomes accepted")
	}
}

func TestSiteDetectContextHasNoCredentialFields(t *testing.T) {
	source := meta("acme-detect", KindSiteDetect) + `
export function detect(ctx) {
  const keys = Object.keys(ctx).sort().join(",");
  return { result: { matched: keys === "baseURL,now", siteType: "newapi", confidence: 0.9 } };
}`
	plugin := kindPlugin(t, KindSiteDetect, "acme-detect", source)
	decision, err := plugin.CallStepped(context.Background(), ProbeContext{SiteType: "x", BaseURL: "https://x.example", CredentialType: "api_key", Now: 5}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result := decision.Result.(*SiteDetectResult); !result.Matched {
		t.Fatalf("ctx had extra keys: %+v", result)
	}
}

func TestSiteDetectConfidenceBounds(t *testing.T) {
	source := meta("acme-detect", KindSiteDetect) + `
export function detect() { return { result: { matched: true, siteType: "newapi", confidence: 2 } }; }`
	plugin := kindPlugin(t, KindSiteDetect, "acme-detect", source)
	if _, err := plugin.CallStepped(context.Background(), ProbeContext{}, nil); err == nil {
		t.Fatal("confidence 2 accepted")
	}
}

func TestErrorClassifierOnlyPicksFromEnum(t *testing.T) {
	good := kindPlugin(t, KindErrorClassifier, "acme-classify", meta("acme-classify", KindErrorClassifier)+`
export function classify(ctx, input) {
  return { class: input.status === 429 ? "limited" : "unknown", reason: input.code };
}`)
	out, err := good.CallErrorClassify(context.Background(), ErrorClassifyContext{SiteType: "x"}, ErrorClassifyInput{Status: 429, Code: "rate_limit"})
	if err != nil || out.Class != "limited" || out.Reason != "rate_limit" {
		t.Fatalf("out = %+v err = %v", out, err)
	}
	bad := kindPlugin(t, KindErrorClassifier, "acme-classify2", meta("acme-classify2", KindErrorClassifier)+`
export function classify() { return { class: "cooldown_forever" }; }`)
	if _, err := bad.CallErrorClassify(context.Background(), ErrorClassifyContext{}, ErrorClassifyInput{Status: 500}); err == nil {
		t.Fatal("class outside the enum accepted")
	}
	extra := kindPlugin(t, KindErrorClassifier, "acme-classify3", meta("acme-classify3", KindErrorClassifier)+`
export function classify() { return { class: "transient", cooldownSeconds: 3600 }; }`)
	if _, err := extra.CallErrorClassify(context.Background(), ErrorClassifyContext{}, ErrorClassifyInput{Status: 500}); err == nil {
		t.Fatal("a cooldown field was accepted; the classifier must not set cooldown")
	}
}

// The enum copied into this package must not drift from the one xLyra uses.
func TestFailureClassesMatchUpstream(t *testing.T) {
	want := []upstream.FailureClass{
		upstream.FailureUnknown, upstream.FailureLimited, upstream.FailureSubscriptionLimit,
		upstream.FailureTransient, upstream.FailureCredentialInvalid,
	}
	if len(FailureClasses) != len(want) {
		t.Fatalf("FailureClasses = %v", FailureClasses)
	}
	for i, class := range want {
		if FailureClasses[i] != string(class) {
			t.Errorf("FailureClasses[%d] = %q, want %q", i, FailureClasses[i], class)
		}
	}
}

func TestModelMetadataRejectsUnknownIDs(t *testing.T) {
	plugin := kindPlugin(t, KindModelMetadata, "acme-meta", meta("acme-meta", KindModelMetadata)+`
export function describeModels(ctx, input) {
  return { models: input.models.map((id) => ({ id, name: id.toUpperCase(), capabilities: { vision: true } })).concat([{ id: "invented" }]) };
}`)
	_, err := plugin.CallModelMetadata(context.Background(), ModelMetadataContext{}, ModelMetadataInput{Models: []string{"a"}})
	if err == nil || !strings.Contains(err.Error(), "invented") {
		t.Fatalf("error = %v, want the invented id named", err)
	}
	ok := kindPlugin(t, KindModelMetadata, "acme-meta2", meta("acme-meta2", KindModelMetadata)+`
export function describeModels(ctx, input) {
  return { models: input.models.map((id) => ({ id, name: id.toUpperCase(), capabilities: { vision: true } })) };
}`)
	out, err := ok.CallModelMetadata(context.Background(), ModelMetadataContext{}, ModelMetadataInput{Models: []string{"a", "b"}})
	if err != nil || len(out.Models) != 2 || out.Models[0].Name != "A" || out.Models[0].Capabilities["vision"] != true {
		t.Fatalf("out = %+v err = %v", out, err)
	}
}

func TestPricingParse(t *testing.T) {
	plugin := kindPlugin(t, KindPricingParse, "acme-price", meta("acme-price", KindPricingParse)+`
export function parsePricing(ctx, payload) {
  return {
    groups: [{ name: "default", ratio: 1 }],
    items: payload.models.map((m) => ({ model: m.name, modelRatio: m.ratio, currency: "usd" })),
  };
}`)
	fixture := Fixture{
		Name:   "ok",
		Input:  map[string]any{"models": []any{map[string]any{"name": "gpt", "ratio": 2.5}}},
		Expect: fixtureExpectation{Result: map[string]any{"items": []any{map[string]any{"model": "gpt", "modelRatio": 2.5}}}},
	}
	if err := plugin.RunFixture(context.Background(), fixture); err != nil {
		t.Fatal(err)
	}
	bad := kindPlugin(t, KindPricingParse, "acme-price2", meta("acme-price2", KindPricingParse)+`
export function parsePricing() { return { items: [{ model: "x", modelRatio: "2" }] }; }`)
	if _, err := bad.CallPricingParse(context.Background(), PricingParseContext{}, map[string]any{}); err == nil {
		t.Fatal("a string ratio was accepted")
	}
}

func TestNewKindsNeedFixtureInput(t *testing.T) {
	plugin := kindPlugin(t, KindErrorClassifier, "acme-classify", meta("acme-classify", KindErrorClassifier)+`
export function classify() { return { class: "unknown" }; }`)
	err := plugin.RunFixture(context.Background(), Fixture{Name: "no input", Expect: fixtureExpectation{Result: map[string]any{"class": "unknown"}}})
	if err == nil || !strings.Contains(err.Error(), "input is required") {
		t.Fatalf("error = %v", err)
	}
}
