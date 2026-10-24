package jsplugin

import (
	"context"
	"strings"
	"testing"
)

const siteProviderHookSource = `
export function probe(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/usage" } };
  const total = Number(steps[0].response.json.total);
  const used = Number(steps[0].response.json.used);
  return { result: { kind: "balance", entries: [{ label: "balance", unit: "usd", limit: total, used, remaining: total - used }] } };
}
export function listModels(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/models" } };
  const rows = (steps[0].response.json && steps[0].response.json.data) || [];
  return { result: { models: rows.map((row) => ({ name: row.id })) } };
}
export function check(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/me" } };
  if (steps[0].response.status === 200) return { result: { status: "ok" } };
  return { result: { status: "invalid", message: "rejected" } };
}
export function classify(ctx, input) {
  if (input.status === 429) return { class: "limited", reason: input.code || "limited" };
  return { class: "unknown" };
}
export function rewritePayload(ctx, payload) {
  if (payload.boom) throw new Error("bad payload");
  const next = Object.assign({}, payload);
  next.model = ctx.model;
  next.stream = ctx.stream;
  next.protocol = ctx.protocol;
  next.extra = "yes";
  return next;
}
`

func providerWith(t *testing.T, extra string, fixtures []Fixture) *Plugin {
	t.Helper()
	source := meta("acme.example", KindSiteProvider) + extra
	plugin, err := NewPlugin(siteProviderManifest(source), source, fixtures, 1)
	if err != nil {
		t.Fatalf("NewPlugin: %v", err)
	}
	return plugin
}

func TestSiteProviderSelfTestDispatchesHooks(t *testing.T) {
	pass := []Fixture{
		probeFixture(6),
		{
			Name: "list models", File: "fixtures/list-models.json", Hook: "listModels",
			Ctx: fixtureContext{SiteType: "acme.example/example", BaseURL: "https://api.example.com", CredentialType: "api_key"},
			Responses: []FixtureResponse{{
				Match: FixtureMatch{Method: "GET", Path: "/v1/models"}, Status: 200,
				JSON: map[string]any{"data": []any{map[string]any{"id": "demo"}}},
			}},
			Expect: fixtureExpectation{
				Requests: []FixtureMatch{{Path: "/v1/models"}},
				Result:   map[string]any{"models": []any{map[string]any{"name": "demo"}}},
			},
		},
		{
			Name: "check ok", File: "fixtures/check.json", Hook: "check",
			Ctx: fixtureContext{SiteType: "acme.example/example", BaseURL: "https://api.example.com", CredentialType: "api_key"},
			Responses: []FixtureResponse{{
				Match: FixtureMatch{Path: "/api/me"}, Status: 200, JSON: map[string]any{},
			}},
			Expect: fixtureExpectation{
				Requests: []FixtureMatch{{Path: "/api/me"}},
				Result:   map[string]any{"status": "ok"},
			},
		},
		{
			Name: "classify limited", File: "fixtures/classify.json", Hook: "classify",
			Ctx:    fixtureContext{SiteType: "acme.example/example"},
			Input:  map[string]any{"status": 429, "code": "rate_limit"},
			Expect: fixtureExpectation{Result: map[string]any{"class": "limited"}},
		},
		rewriteFixture(false),
	}
	plugin := providerWith(t, siteProviderHookSource, pass)
	if err := plugin.SelfTest(context.Background()); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		fixture Fixture
		want    string
	}{
		{"probe", probeFixture(1), "remaining"},
		{"listModels", Fixture{
			Name: "list models", File: "fixtures/list-models.json", Hook: "listModels",
			Ctx: fixtureContext{BaseURL: "https://api.example.com", CredentialType: "api_key"},
			Responses: []FixtureResponse{{
				Match: FixtureMatch{Path: "/v1/models"}, Status: 200,
				JSON: map[string]any{"data": []any{map[string]any{"id": "demo"}}},
			}},
			Expect: fixtureExpectation{
				Requests: []FixtureMatch{{Path: "/v1/models"}},
				Result:   map[string]any{"models": []any{map[string]any{"name": "other"}}},
			},
		}, "name"},
		{"check", Fixture{
			Name: "check ok", File: "fixtures/check.json", Hook: "check",
			Ctx:       fixtureContext{BaseURL: "https://api.example.com", CredentialType: "api_key"},
			Responses: []FixtureResponse{{Match: FixtureMatch{Path: "/api/me"}, Status: 200, JSON: map[string]any{}}},
			Expect: fixtureExpectation{
				Requests: []FixtureMatch{{Path: "/api/me"}},
				Result:   map[string]any{"status": "invalid"},
			},
		}, "status"},
		{"classify", Fixture{
			Name: "classify limited", File: "fixtures/classify.json", Hook: "classify",
			Input:  map[string]any{"status": 429, "code": "rate_limit"},
			Expect: fixtureExpectation{Result: map[string]any{"class": "unknown"}},
		}, "class"},
		{"rewritePayload", rewriteFixture(true), "extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := plugin.RunFixture(context.Background(), tc.fixture)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSiteProviderRewritePayloadFixtureError(t *testing.T) {
	plugin := providerWith(t, siteProviderHookSource, nil)
	ok := Fixture{
		Name: "boom", File: "fixtures/rewrite-error.json", Hook: "rewritePayload",
		Ctx:    fixtureContext{SiteType: "acme.example/example", Model: "m1", Protocol: "openai_chat"},
		Input:  map[string]any{"boom": true, "model": "m1"},
		Expect: fixtureExpectation{Error: "bad payload"},
	}
	if err := plugin.RunFixture(context.Background(), ok); err != nil {
		t.Fatal(err)
	}
	ok.Expect = fixtureExpectation{Error: "not this"}
	if err := plugin.RunFixture(context.Background(), ok); err == nil || !strings.Contains(err.Error(), "bad payload") {
		t.Fatalf("error = %v, want the thrown message", err)
	}
	ok.Input = map[string]any{"model": "m1"}
	ok.Expect = fixtureExpectation{Error: "bad payload"}
	if err := plugin.RunFixture(context.Background(), ok); err == nil || !strings.Contains(err.Error(), "want error") {
		t.Fatalf("error = %v, want a result-versus-error mismatch", err)
	}
	ok.Input = []any{"nope"}
	if err := plugin.RunFixture(context.Background(), ok); err == nil || !strings.Contains(err.Error(), "input must be an object") {
		t.Fatalf("error = %v, want an object input", err)
	}
}

func TestSiteProviderFixtureHookRules(t *testing.T) {
	if err := providerWith(t, "", nil).SelfTest(context.Background()); err != nil {
		t.Fatal(err)
	}
	hooks := providerWith(t, siteProviderHookSource, nil)
	cases := []struct {
		name    string
		plugin  *Plugin
		fixture Fixture
		want    string
	}{
		{
			name: "missing", plugin: hooks,
			fixture: Fixture{Name: "bare", File: "fixtures/bare.json"},
			want:    "fixtures/bare.json: hook is required",
		},
		{
			name: "unknown", plugin: hooks,
			fixture: Fixture{Name: "weird", File: "fixtures/weird.json", Hook: "detect"},
			want:    "fixtures/weird.json: unknown hook \"detect\"",
		},
		{
			name: "not exported", plugin: providerWith(t, "", nil),
			fixture: Fixture{Name: "missing", File: "fixtures/missing.json", Hook: "classify"},
			want:    "fixtures/missing.json: hook \"classify\" is not exported",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := tc.plugin
			plugin.fixtures = []Fixture{tc.fixture}
			err := plugin.SelfTest(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), tc.fixture.Name) {
				t.Fatalf("error = %v, want %q and name %q", err, tc.want, tc.fixture.Name)
			}
		})
	}
}

func TestOtherKindRejectsFixtureHook(t *testing.T) {
	source := meta("acme.probe", KindQuotaProbe) + "export function probe() { return { error: \"stop\" }; }\n"
	manifest := Manifest{
		ID: "acme.probe", Name: "acme.probe", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: KindQuotaProbe,
		SHA256: map[string]string{"plugin.js": hashSource(source)},
	}
	fixture := Fixture{
		Name: "stop", File: "fixtures/stop.json", Hook: "probe",
		Expect: fixtureExpectation{Error: "stop"},
	}
	plugin, err := NewPlugin(manifest, source, []Fixture{fixture}, 1)
	if err != nil {
		t.Fatal(err)
	}
	err = plugin.SelfTest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fixtures/stop.json") || !strings.Contains(err.Error(), "hook is not allowed") {
		t.Fatalf("error = %v", err)
	}
	fixture.Hook = ""
	if err := plugin.RunFixture(context.Background(), fixture); err != nil {
		t.Fatal(err)
	}
}

func probeFixture(remaining float64) Fixture {
	return Fixture{
		Name: "probe ok", File: "fixtures/probe.json", Hook: "probe",
		Ctx: fixtureContext{SiteType: "acme.example/example", BaseURL: "https://api.example.com", CredentialType: "api_key", Now: 1},
		Responses: []FixtureResponse{{
			Match: FixtureMatch{Method: "GET", Path: "/api/usage"}, Status: 200,
			JSON: map[string]any{"total": 10.0, "used": 4.0},
		}},
		Expect: fixtureExpectation{
			Requests: []FixtureMatch{{Path: "/api/usage"}},
			Result: map[string]any{
				"kind":    "balance",
				"entries": []any{map[string]any{"label": "balance", "remaining": remaining}},
			},
		},
	}
}

func rewriteFixture(wrong bool) Fixture {
	extra := "yes"
	if wrong {
		extra = "no"
	}
	return Fixture{
		Name: "rewrite payload", File: "fixtures/rewrite.json", Hook: "rewritePayload",
		Ctx: fixtureContext{SiteType: "acme.example/example", Model: "m1", Stream: true, Protocol: "openai_chat"},
		Input: map[string]any{
			"model": "m1", "stream": true, "messages": []any{map[string]any{"role": "user"}},
		},
		Expect: fixtureExpectation{Result: map[string]any{
			"model": "m1", "stream": true, "protocol": "openai_chat", "extra": extra,
			"messages": []any{map[string]any{"role": "user"}},
		}},
	}
}
