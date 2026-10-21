package gateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func providerClassifierPlugin(t *testing.T, id, body string) *jsplugin.Plugin {
	t.Helper()
	source := "export const meta = { apiVersion: 1, id: \"" + id + "\", kind: \"site_provider\" };\n" + body
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
		t.Fatal(err)
	}
	return plugin
}

func useGatewayPlugins(t *testing.T, plugins ...*jsplugin.Plugin) {
	t.Helper()
	catalog := jsplugin.DefaultCatalog()
	previous := catalog.Current()
	builtins, err := jsplugin.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	catalog.Replace(&jsplugin.Snapshot{Gen: 9101, Registry: jsplugin.NewRegistry(append(builtins.Plugins(), plugins...)...)})
	t.Cleanup(func() {
		if previous != nil {
			catalog.Replace(previous)
		} else {
			_ = catalog.InitBuiltins()
		}
	})
}

func installProviderTypes(t *testing.T, items ...site.PluginSiteType) {
	t.Helper()
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)
	if err := site.SetPluginSiteTypes(items); err != nil {
		t.Fatal(err)
	}
}

func providerType(plugin *jsplugin.Plugin, enabled bool) site.PluginSiteType {
	return site.PluginSiteType{
		Descriptor: site.SiteTypeDescriptor{SiteType: plugin.Manifest.SiteProvider.SiteType},
		PluginID:   plugin.Manifest.ID,
		Enabled:    enabled,
		Hosts:      []string{"api.example.com"},
	}
}

func providerCandidate(siteType, classifierID string) routeengine.Candidate {
	return routeengine.Candidate{
		Site: routeengine.CandidateSite{
			ID:                    uuid.New(),
			SiteType:              siteType,
			ErrorClassifierPlugin: classifierID,
		},
		Model: routeengine.CandidateModel{SiteModelID: uuid.New(), UpstreamName: "m"},
	}
}

func plainFailureResult(status int) gatewayAttemptResult {
	return gatewayAttemptResult{
		statusCode:         status,
		upstreamStatusCode: status,
		errorType:          "upstream_http_error",
		credentialID:       uuid.New(),
	}
}

const providerClassifySource = `
export function classify(ctx, input) {
  const ctxKeys = Object.keys(ctx).sort().join(",");
  const inKeys = Object.keys(input).sort().join(",");
  if (ctxKeys !== "siteType" || inKeys !== "code,message,status,type") return { class: "credential_invalid", reason: "shape" };
  if (input.code === "acme_slow") return { class: "limited", reason: "rate" };
  if (input.code === "acme_empty") return { class: "subscription_limit", reason: "quota" };
  return { class: "unknown" };
}`

const slowBody = `{"error":{"code":"acme_slow","type":"acme","message":"slow down"}}`
const quotaBody = `{"error":{"code":"acme_empty","type":"acme","message":"quota used up"}}`

func TestSiteProviderClassifyCorrectsRateLimitAndQuota(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	plugin := providerClassifierPlugin(t, "acme.example", providerClassifySource)
	useGatewayPlugins(t, plugin)
	installProviderTypes(t, providerType(plugin, true))
	candidate := providerCandidate("acme.example/chat", "")
	result := plainFailureResult(http.StatusBadRequest)

	baseline := classifyGatewayUpstreamError(providerCandidate("openai", ""), result, []byte(slowBody), now)
	quotaBaseline := classifyGatewayUpstreamError(providerCandidate("openai", ""), result, []byte(quotaBody), now)
	if baseline.errorType != "upstream_http_error" || baseline.cooldownReason != "" || quotaBaseline.errorType != "upstream_http_error" || quotaBaseline.cooldownReason != "" {
		t.Fatalf("body is already classified: slow %#v quota %#v", baseline, quotaBaseline)
	}
	limited := classifyGatewayUpstreamError(candidate, result, []byte(slowBody), now)
	if limited.errorType != "upstream_credential_limited" || limited.cooldownReason != store.CooldownReasonUpstreamCredentialLimited {
		t.Fatalf("rate limit = %#v", limited)
	}
	quota := classifyGatewayUpstreamError(candidate, result, []byte(quotaBody), now)
	if quota.errorType != "upstream_subscription_limit_exceeded" || quota.cooldownReason != store.CooldownReasonUpstreamSubscriptionLimitExceeded || quota.cooldownDuration <= 0 {
		t.Fatalf("quota = %#v", quota)
	}
}

func TestSiteProviderClassifyFailureKeepsDefault(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached"}}`)
	baseline := classifyGatewayUpstreamError(providerCandidate("acme.example/chat", ""), classifierResult(), body, now)
	cases := map[string]string{
		"unknown":     `export function classify() { return { class: "unknown" }; }`,
		"throws":      `export function classify() { throw new Error("boom"); }`,
		"bad class":   `export function classify() { return { class: "ban_forever" }; }`,
		"extra field": `export function classify() { return { class: "limited", cooldownSeconds: 99999 }; }`,
	}
	for name, source := range cases {
		plugin := providerClassifierPlugin(t, "acme.example", source)
		useGatewayPlugins(t, plugin)
		installProviderTypes(t, providerType(plugin, true))
		got := classifyGatewayUpstreamError(providerCandidate("acme.example/chat", ""), classifierResult(), body, now)
		if got.errorType != baseline.errorType || got.cooldownReason != baseline.cooldownReason || got.cooldownDuration != baseline.cooldownDuration {
			t.Fatalf("%s: outcome changed: got %#v, want %#v", name, got, baseline)
		}
	}
}

func TestSiteProviderClassifyTimeoutKeepsDefault(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached"}}`)
	plugin := providerClassifierPlugin(t, "acme.example", `export function classify() { for (;;) {} }`)
	useGatewayPlugins(t, plugin)
	installProviderTypes(t, providerType(plugin, true))
	baseline := classifyGatewayUpstreamError(providerCandidate("openai", ""), classifierResult(), body, now)
	started := time.Now()
	got := classifyGatewayUpstreamError(providerCandidate("acme.example/chat", ""), classifierResult(), body, now)
	elapsed := time.Since(started)
	if got.errorType != baseline.errorType || got.cooldownReason != baseline.cooldownReason || got.cooldownDuration != baseline.cooldownDuration {
		t.Fatalf("timeout changed the outcome: got %#v, want %#v", got, baseline)
	}
	if elapsed < 40*time.Millisecond || elapsed > 180*time.Millisecond {
		t.Fatalf("elapsed %s, want the 50ms classifier timeout inside the request budget", elapsed)
	}
}

func TestBoundErrorClassifierBeatsSiteProviderHook(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	provider := providerClassifierPlugin(t, "acme.example", providerClassifySource)
	bound := classifierPlugin(t, "acme-classify", `export function classify() { return { class: "credential_invalid", reason: "bound" }; }`)
	useGatewayPlugins(t, provider, bound)
	installProviderTypes(t, providerType(provider, true))

	got := classifyGatewayUpstreamError(providerCandidate("acme.example/chat", "acme-classify"), plainFailureResult(http.StatusBadRequest), []byte(slowBody), now)
	if got.errorType != "upstream_credential_invalid" || got.cooldownReason != store.CooldownReasonUpstreamCredentialUnauthorized || got.cooldownDuration != credentialUnauthorizedCooldownDuration {
		t.Fatalf("bound classifier lost: %#v", got)
	}
	fallback := classifyGatewayUpstreamError(providerCandidate("acme.example/chat", "missing-classifier"), plainFailureResult(http.StatusBadRequest), []byte(slowBody), now)
	if fallback.errorType != "upstream_credential_limited" || fallback.cooldownReason != store.CooldownReasonUpstreamCredentialLimited {
		t.Fatalf("missing binding did not fall through to the site hook: %#v", fallback)
	}
}

func TestSiteProviderClassifySkipsBuiltinAndHookless(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	hooked := providerClassifierPlugin(t, "acme.example", `export function classify() { return { class: "credential_invalid", reason: "called" }; }`)
	bare := providerClassifierPlugin(t, "bare.example", `export function listModels() { return { result: { models: [] } }; }`)
	useGatewayPlugins(t, hooked, bare)
	installProviderTypes(t, providerType(hooked, true), providerType(bare, true))
	breaker := jsplugin.DefaultCatalog().Breaker()
	body := []byte(slowBody)
	result := plainFailureResult(http.StatusBadRequest)

	hookedBefore := breaker.Metrics(hooked.Manifest.ID, hooked.Manifest.Version)
	bareBefore := breaker.Metrics(bare.Manifest.ID, bare.Manifest.Version)
	builtin := classifyGatewayUpstreamError(providerCandidate("openai", ""), result, body, now)
	hookless := classifyGatewayUpstreamError(providerCandidate("bare.example/chat", ""), result, body, now)
	if builtin.errorType != "upstream_http_error" || builtin.cooldownReason != "" || hookless.errorType != "upstream_http_error" || hookless.cooldownReason != "" {
		t.Fatalf("builtin %#v hookless %#v", builtin, hookless)
	}
	if got := breaker.Metrics(hooked.Manifest.ID, hooked.Manifest.Version); got.Calls != hookedBefore.Calls || got.Errors != hookedBefore.Errors {
		t.Fatalf("builtin or hookless called the classify plugin: before %+v after %+v", hookedBefore, got)
	}
	if got := breaker.Metrics(bare.Manifest.ID, bare.Manifest.Version); got.Calls != bareBefore.Calls || got.Errors != bareBefore.Errors {
		t.Fatalf("hookless plugin was called: before %+v after %+v", bareBefore, got)
	}

	called := classifyGatewayUpstreamError(providerCandidate("acme.example/chat", ""), result, body, now)
	if called.errorType != "upstream_credential_invalid" {
		t.Fatalf("enabled classify hook was not applied: %#v", called)
	}
	disabled := providerClassifierPlugin(t, "paused.example", `export function classify() { return { class: "credential_invalid", reason: "called" }; }`)
	useGatewayPlugins(t, disabled)
	installProviderTypes(t, providerType(disabled, false))
	before := breaker.Metrics(disabled.Manifest.ID, disabled.Manifest.Version)
	got := classifyGatewayUpstreamError(providerCandidate("paused.example/chat", ""), result, body, now)
	after := breaker.Metrics(disabled.Manifest.ID, disabled.Manifest.Version)
	if got.errorType != "upstream_http_error" || got.cooldownReason != "" || after.Calls != before.Calls {
		t.Fatalf("disabled type changed the outcome or called the plugin: %#v metrics %+v -> %+v", got, before, after)
	}
}

func TestSiteProviderClassifyBreakerDoesNotDisable(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	plugin := providerClassifierPlugin(t, "classifybreaker.example", `
export function classify(ctx, input) {
  if (input.code === "boom") throw new Error("boom");
  if (input.code === "acme_slow") return { class: "limited", reason: "rate" };
  return { class: "unknown" };
}`)
	useGatewayPlugins(t, plugin)
	installProviderTypes(t, providerType(plugin, true))
	breaker := jsplugin.DefaultCatalog().Breaker()
	before := breaker.Metrics(plugin.Manifest.ID, plugin.Manifest.Version)
	gen := jsplugin.DefaultCatalog().Generation()
	candidate := providerCandidate("classifybreaker.example/chat", "")
	boom := []byte(`{"error":{"code":"boom","type":"acme","message":"nope"}}`)
	for i := 0; i < 21; i++ {
		got := classifyGatewayUpstreamError(candidate, plainFailureResult(http.StatusBadRequest), boom, now)
		if got.errorType != "upstream_http_error" || got.cooldownReason != "" {
			t.Fatalf("call %d = %#v", i, got)
		}
	}
	mid := breaker.Metrics(plugin.Manifest.ID, plugin.Manifest.Version)
	if mid.Calls < before.Calls+21 || mid.Errors < before.Errors+21 {
		t.Fatalf("metrics after failures = %+v, before %+v", mid, before)
	}
	got := classifyGatewayUpstreamError(candidate, plainFailureResult(http.StatusBadRequest), []byte(slowBody), now)
	if got.errorType != "upstream_credential_limited" || got.cooldownReason != store.CooldownReasonUpstreamCredentialLimited {
		t.Fatalf("recovered classify = %#v", got)
	}
	after := breaker.Metrics(plugin.Manifest.ID, plugin.Manifest.Version)
	if after.Calls < before.Calls+22 || after.Errors != mid.Errors {
		t.Fatalf("metrics after recovery = %+v, mid %+v", after, mid)
	}
	if jsplugin.DefaultCatalog().Generation() != gen {
		t.Fatalf("generation = %d, want %d", jsplugin.DefaultCatalog().Generation(), gen)
	}
	found, ok := site.ProviderHookPlugin(jsplugin.DefaultCatalog().Registry(), "classifybreaker.example/chat", "classify")
	if !ok || found.Manifest.ID != plugin.Manifest.ID || !found.HasHook("classify") {
		t.Fatalf("classify hook after failures = %v", ok)
	}
}
