package gateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func classifierPlugin(t *testing.T, id, body string) *jsplugin.Plugin {
	t.Helper()
	source := `export const meta = { apiVersion: 1, id: "` + id + `", kind: "error_classifier" };` + "\n" + body
	plugin, err := jsplugin.NewPlugin(jsplugin.Manifest{
		ID: id, Name: id, Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: jsplugin.KindErrorClassifier,
		SHA256: map[string]string{"plugin.js": jsplugin.HashSource(source)},
	}, source, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	return plugin
}

// The catalog is process-wide, so tests here run one after another.
func withClassifier(t *testing.T, plugin *jsplugin.Plugin) {
	t.Helper()
	catalog := jsplugin.DefaultCatalog()
	previous := catalog.Current()
	builtins, err := jsplugin.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	// Keep the builtins so tests running alongside still find their protocols.
	catalog.Replace(&jsplugin.Snapshot{Gen: 9100, Registry: jsplugin.NewRegistry(append(builtins.Plugins(), plugin)...)})
	t.Cleanup(func() {
		if previous != nil {
			catalog.Replace(previous)
		} else {
			_ = catalog.InitBuiltins()
		}
	})
}

func classifierCandidate(pluginID string) routeengine.Candidate {
	return routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.New(), SiteType: "custom", ErrorClassifierPlugin: pluginID},
		Model: routeengine.CandidateModel{SiteModelID: uuid.New(), UpstreamName: "m"},
	}
}

func classifierResult() gatewayAttemptResult {
	return gatewayAttemptResult{
		statusCode:         http.StatusTooManyRequests,
		upstreamStatusCode: http.StatusTooManyRequests,
		errorType:          "upstream_http_error",
		credentialID:       uuid.New(),
	}
}

const weirdBody = `{"error":{"code":"acme_wallet_empty","message":"wallet is empty"}}`

func TestErrorClassifierPluginCorrectsTheClass(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	withClassifier(t, classifierPlugin(t, "acme-classify", `
export function classify(ctx, input) {
  return input.code === "acme_wallet_empty" ? { class: "credential_invalid", reason: "wallet empty" } : { class: "unknown" };
}`))

	without := classifyGatewayUpstreamError(classifierCandidate(""), classifierResult(), []byte(weirdBody), now)
	with := classifyGatewayUpstreamError(classifierCandidate("acme-classify"), classifierResult(), []byte(weirdBody), now)

	if without.cooldownReason == store.CooldownReasonUpstreamCredentialUnauthorized {
		t.Fatalf("test body is already classified without the plugin: %#v", without)
	}
	if with.cooldownReason != store.CooldownReasonUpstreamCredentialUnauthorized || with.errorType != "upstream_credential_invalid" {
		t.Fatalf("plugin verdict was not applied: %#v", with)
	}
	// The plugin picked the class; the cooldown length is still ours.
	if with.cooldownDuration != credentialUnauthorizedCooldownDuration {
		t.Fatalf("cooldown = %s, want the built-in %s", with.cooldownDuration, credentialUnauthorizedCooldownDuration)
	}
}

func TestErrorClassifierPluginUnknownKeepsDefault(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached"}}`)
	baseline := classifyGatewayUpstreamError(classifierCandidate(""), classifierResult(), body, now)
	withClassifier(t, classifierPlugin(t, "acme-classify", `export function classify() { return { class: "unknown" }; }`))
	got := classifyGatewayUpstreamError(classifierCandidate("acme-classify"), classifierResult(), body, now)
	if got.errorType != baseline.errorType || got.cooldownReason != baseline.cooldownReason || got.cooldownDuration != baseline.cooldownDuration {
		t.Fatalf("unknown changed the outcome: got %#v, want %#v", got, baseline)
	}
}

func TestErrorClassifierPluginFailureKeepsDefault(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	body := []byte(`{"error":{"code":"rate_limit_exceeded","message":"Rate limit reached"}}`)
	baseline := classifyGatewayUpstreamError(classifierCandidate(""), classifierResult(), body, now)
	for name, source := range map[string]string{
		"throws":      `export function classify() { throw new Error("boom"); }`,
		"bad class":   `export function classify() { return { class: "ban_forever" }; }`,
		"extra field": `export function classify() { return { class: "limited", cooldownSeconds: 99999 }; }`,
	} {
		withClassifier(t, classifierPlugin(t, "acme-classify", source))
		got := classifyGatewayUpstreamError(classifierCandidate("acme-classify"), classifierResult(), body, now)
		if got.errorType != baseline.errorType || got.cooldownDuration != baseline.cooldownDuration {
			t.Fatalf("%s: outcome changed: got %#v, want %#v", name, got, baseline)
		}
	}
}

func TestErrorClassifierPluginMissingIsIgnored(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	withClassifier(t, classifierPlugin(t, "some-other", `export function classify() { return { class: "credential_invalid" }; }`))
	got := classifyGatewayUpstreamError(classifierCandidate("not-enabled"), classifierResult(), []byte(weirdBody), now)
	baseline := classifyGatewayUpstreamError(classifierCandidate(""), classifierResult(), []byte(weirdBody), now)
	if got.errorType != baseline.errorType {
		t.Fatalf("a binding to a plugin that is not enabled changed the outcome: %#v", got)
	}
}
