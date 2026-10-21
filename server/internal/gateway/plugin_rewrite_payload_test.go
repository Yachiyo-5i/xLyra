package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
)

const rewriteEchoSource = `
export function rewritePayload(ctx, payload) {
  const keys = Object.keys(ctx).sort().join(",");
  if (keys !== "model,protocol,siteType,stream") throw new Error("ctx " + keys);
  if (ctx.siteType !== "acme.example/chat") throw new Error("site " + ctx.siteType);
  if (ctx.protocol !== "test_protocol") throw new Error("protocol " + ctx.protocol);
  if (typeof ctx.stream !== "boolean") throw new Error("stream type");
  if (payload.stream === true && ctx.stream !== true) throw new Error("stream mismatch");
  if (payload.stream !== true && ctx.stream !== false) throw new Error("stream default");
  if (typeof payload.model === "string" && ctx.model !== payload.model) throw new Error("model " + ctx.model);
  if (JSON.stringify(payload).indexOf("account_uuid") >= 0) throw new Error("saw account");
  delete payload.dropme;
  payload.added = "yes";
  return payload;
}`

func TestRewritePayloadRewritesChatAndOtherEndpoints(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)
	plugin := providerClassifierPlugin(t, "acme.example", rewriteEchoSource)
	useGatewayPlugins(t, plugin)
	capture := newRewriteCapture()
	server := httptest.NewServer(capture.handler())
	t.Cleanup(server.Close)

	paths := []string{
		gatewayEndpointChatCompletions,
		gatewayEndpointEmbeddings,
		gatewayEndpointImagesGenerations,
		gatewayEndpointImagesEdits,
		gatewayEndpointAudioSpeech,
		gatewayEndpointMessages,
		gatewayEndpointResponses,
	}
	handler, candidate := pluginUpstreamFixture(t, "acme.example/chat", server.URL, []string{"127.0.0.1"}, true, &http.Client{Timeout: 5 * time.Second}, 1)
	for _, path := range paths {
		result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-rewrite", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: path}, nil, &testGatewayProtocol{})
		if !result.success || result.errorType != "" {
			t.Fatalf("%s success=%v type=%q message=%q", path, result.success, result.errorType, result.errorMessage)
		}
	}
	handler, candidate = pluginUpstreamFixture(t, "acme.example/chat", server.URL, []string{"127.0.0.1"}, true, nil, 1)
	result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-rewrite-stream", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Stream: true}, nil, &testGatewayProtocol{streamStarted: true, streamCapture: streamCaptureState{streamCompleted: true, endReason: "done"}})
	if !result.success || result.errorType != "" {
		t.Fatalf("stream success=%v type=%q message=%q", result.success, result.errorType, result.errorMessage)
	}
	bodies := capture.snapshot()
	if len(bodies) != len(paths)+1 {
		t.Fatalf("bodies = %d", len(bodies))
	}
	for i, body := range bodies {
		assertRewrittenBody(t, body, map[string]any{"model": "test-upstream", "added": "yes"})
		if strings.Contains(body, "account_uuid") {
			t.Fatalf("body %d contains account_uuid: %s", i, body)
		}
	}

	streamPayload := map[string]any{"model": "m", "stream": true, "dropme": "x"}
	handler, candidate = pluginUpstreamFixture(t, "acme.example/chat", server.URL, []string{"127.0.0.1"}, true, &http.Client{Timeout: 5 * time.Second}, 1)
	result = handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-rewrite-flag", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, nil, rewriteBodyProtocol{testGatewayProtocol: &testGatewayProtocol{}, payload: streamPayload})
	if !result.success || result.errorType != "" {
		t.Fatalf("stream flag success=%v type=%q message=%q", result.success, result.errorType, result.errorMessage)
	}
	last := capture.snapshot()
	assertRewrittenBody(t, last[len(last)-1], map[string]any{"model": "m", "stream": true, "added": "yes"})
	if strings.Contains(last[len(last)-1], "dropme") {
		t.Fatalf("dropme survived: %s", last[len(last)-1])
	}
}

func TestRewritePayloadFailureDoesNotSend(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)
	cases := []struct {
		name    string
		source  string
		payload map[string]any
		want    string
		alt     string
		creds   int
	}{
		{name: "throw", source: `export function rewritePayload() { throw new Error("boom"); }`, want: "boom", creds: 2},
		{name: "null", source: `export function rewritePayload() { return null; }`, want: "js_shape_error"},
		{name: "array", source: `export function rewritePayload() { return []; }`, want: "js_shape_error"},
		{name: "change model", source: `export function rewritePayload(ctx, payload) { payload.model = "other"; return payload; }`, want: `"model"`},
		{name: "delete model", source: `export function rewritePayload(ctx, payload) { delete payload.model; return payload; }`, want: `"model"`},
		{name: "add model", source: `export function rewritePayload(ctx, payload) { payload.model = "m"; return payload; }`, payload: map[string]any{"input": "x"}, want: `"model"`},
		{name: "add stream", source: `export function rewritePayload(ctx, payload) { payload.stream = false; return payload; }`, want: `"stream"`},
		{name: "change stream", source: `export function rewritePayload(ctx, payload) { payload.stream = false; return payload; }`, payload: map[string]any{"model": "m", "stream": true}, want: `"stream"`},
		{name: "delete stream", source: `export function rewritePayload(ctx, payload) { delete payload.stream; return payload; }`, payload: map[string]any{"model": "m", "stream": true}, want: `"stream"`},
		{name: "timeout", source: `export function rewritePayload() { for (;;) {} }`, want: "js_timeout"},
		// race 检测器下 50 ms 预算可能先耗尽。
		{name: "output too large", source: `export function rewritePayload() { return { blob: "x".repeat(2 * 1024 * 1024) }; }`, want: "js_plugin_response_too_large", alt: "js_timeout"},
		{name: "input too large", source: `export function rewritePayload(ctx, payload) { payload.added = "yes"; return payload; }`, payload: map[string]any{"model": "m", "blob": strings.Repeat("a", int(jsplugin.MaxResultBytes))}, want: "payload exceeds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := providerClassifierPlugin(t, "acme.example", tc.source)
			useGatewayPlugins(t, plugin)
			hits := &atomic.Int32{}
			creds := tc.creds
			if creds == 0 {
				creds = 1
			}
			handler, candidate := pluginUpstreamFixture(t, "acme.example/chat", "http://127.0.0.1:1", []string{"a.test"}, true, pluginCountingClient(hits), creds)
			protocol := gatewayProtocolAdapter(&testGatewayProtocol{})
			if tc.payload != nil {
				protocol = rewriteBodyProtocol{testGatewayProtocol: &testGatewayProtocol{}, payload: tc.payload}
			}
			started := time.Now()
			result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-rewrite-fail", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, nil, protocol)
			elapsed := time.Since(started)
			kindOK := strings.Contains(result.errorMessage, tc.want) || (tc.alt != "" && strings.Contains(result.errorMessage, tc.alt))
			if result.statusCode != http.StatusBadGateway || result.errorType != "plugin_rewrite_failed" || !strings.Contains(result.errorMessage, "acme.example") || !kindOK {
				t.Fatalf("status=%d type=%q message=%q", result.statusCode, result.errorType, result.errorMessage)
			}
			if result.credentialDecision.ShouldTryNextCredential || hits.Load() != 0 {
				t.Fatalf("sent or failed over: hits=%d decision=%#v", hits.Load(), result.credentialDecision)
			}
			if tc.creds == 2 && (result.credentialAttempt != 1 || result.credentialTotal != 2 || result.credentialDecision.Action != "stop") {
				t.Fatalf("credential decision = attempt %d total %d %#v", result.credentialAttempt, result.credentialTotal, result.credentialDecision)
			}
			if tc.name == "timeout" && (elapsed < 40*time.Millisecond || elapsed > 200*time.Millisecond) {
				t.Fatalf("elapsed %s, want the 50ms hook timeout", elapsed)
			}
		})
	}
}

func TestRewritePayloadSkipsBuiltinAndHookless(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)
	hooked := providerClassifierPlugin(t, "acme.example", rewriteEchoSource)
	bare := providerClassifierPlugin(t, "bare.example", `export function listModels() { return { result: { models: [] } }; }`)
	useGatewayPlugins(t, hooked, bare)
	capture := newRewriteCapture()
	server := httptest.NewServer(capture.handler())
	t.Cleanup(server.Close)
	client := &http.Client{Timeout: 5 * time.Second}
	breaker := jsplugin.DefaultCatalog().Breaker()
	hookedBefore := breaker.Metrics(hooked.Manifest.ID, hooked.Manifest.Version)
	bareBefore := breaker.Metrics(bare.Manifest.ID, bare.Manifest.Version)

	handler, candidate := pluginUpstreamFixture(t, "openai", server.URL, []string{"127.0.0.1"}, true, client, 1)
	builtin := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-builtin", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, nil, &testGatewayProtocol{})
	if !builtin.success {
		t.Fatalf("builtin type=%q message=%q", builtin.errorType, builtin.errorMessage)
	}

	handler, candidate = pluginUpstreamFixture(t, "bare.example/chat", server.URL, []string{"127.0.0.1"}, true, client, 1)
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: "bare.example/chat", DisplayName: "Bare"},
		PluginID:   bare.Manifest.ID,
		Enabled:    true,
		Hosts:      []string{"127.0.0.1"},
	}}); err != nil {
		t.Fatal(err)
	}
	hookless := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-hookless", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, nil, &testGatewayProtocol{})
	if !hookless.success {
		t.Fatalf("hookless type=%q message=%q", hookless.errorType, hookless.errorMessage)
	}
	bodies := capture.snapshot()
	if len(bodies) != 2 {
		t.Fatalf("bodies = %d", len(bodies))
	}
	for _, body := range bodies {
		if body != `{"model":"test-upstream"}` {
			t.Fatalf("body = %s", body)
		}
	}
	if got := breaker.Metrics(hooked.Manifest.ID, hooked.Manifest.Version); got.Calls != hookedBefore.Calls {
		t.Fatalf("builtin called the hook: %+v -> %+v", hookedBefore, got)
	}
	if got := breaker.Metrics(bare.Manifest.ID, bare.Manifest.Version); got.Calls != bareBefore.Calls {
		t.Fatalf("hookless plugin was called: %+v -> %+v", bareBefore, got)
	}

	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: "acme.example/chat"},
		PluginID:   hooked.Manifest.ID,
		Enabled:    false,
		Hosts:      []string{"127.0.0.1"},
	}}); err != nil {
		t.Fatal(err)
	}
	disabledBefore := breaker.Metrics(hooked.Manifest.ID, hooked.Manifest.Version)
	payload := map[string]any{"model": "test-upstream"}
	got, err := applyProviderRewrite(context.Background(), routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "acme.example/chat"},
		Model: routeengine.CandidateModel{UpstreamName: "test-upstream"},
	}, &testGatewayProtocol{}, payload)
	if err != nil || got["added"] != nil || got["model"] != "test-upstream" {
		t.Fatalf("disabled rewrite = %#v err = %v", got, err)
	}
	if after := breaker.Metrics(hooked.Manifest.ID, hooked.Manifest.Version); after.Calls != disabledBefore.Calls {
		t.Fatalf("disabled type called the hook: %+v -> %+v", disabledBefore, after)
	}
}

func TestRewritePayloadDiagnostic(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)
	plugin := providerClassifierPlugin(t, "acme.example", rewriteEchoSource)
	useGatewayPlugins(t, plugin)
	capture := newRewriteCapture()
	server := httptest.NewServer(capture.handler())
	t.Cleanup(server.Close)
	handler, candidate, credential := pluginUpstreamModelTestFixture(t, "acme.example/chat", server.URL, []string{"127.0.0.1"}, &http.Client{Timeout: 5 * time.Second})
	result := handler.forwardSiteModelTestRequest(context.Background(), "site-model-test/rewrite", uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, &testGatewayProtocol{}, credential)
	if !result.success || result.errorType != "" {
		t.Fatalf("diagnostic success=%v type=%q message=%q", result.success, result.errorType, result.errorMessage)
	}
	bodies := capture.snapshot()
	if len(bodies) != 1 {
		t.Fatalf("bodies = %d", len(bodies))
	}
	assertRewrittenBody(t, bodies[0], map[string]any{"model": "test-upstream", "added": "yes"})

	hits := &atomic.Int32{}
	broken := providerClassifierPlugin(t, "acme.example", `export function rewritePayload() { throw new Error("boom"); }`)
	useGatewayPlugins(t, broken)
	handler, candidate, credential = pluginUpstreamModelTestFixture(t, "acme.example/chat", "http://127.0.0.1:1", []string{"a.test"}, pluginCountingClient(hits))
	result = handler.forwardSiteModelTestRequest(context.Background(), "site-model-test/rewrite-fail", uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, &testGatewayProtocol{}, credential)
	if result.statusCode != http.StatusBadGateway || result.errorType != "plugin_rewrite_failed" || !strings.Contains(result.errorMessage, "acme.example") || !strings.Contains(result.errorMessage, "boom") || hits.Load() != 0 {
		t.Fatalf("diagnostic fail status=%d type=%q message=%q hits=%d", result.statusCode, result.errorType, result.errorMessage, hits.Load())
	}
}

func TestRewritePayloadBreakerDoesNotDisable(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)
	plugin := providerClassifierPlugin(t, "rewritebreaker.example", `
export function rewritePayload(ctx, payload) {
  if (payload.boom) throw new Error("boom");
  payload.added = "yes";
  return payload;
}`)
	useGatewayPlugins(t, plugin)
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: "rewritebreaker.example/chat"},
		PluginID:   plugin.Manifest.ID,
		Enabled:    true,
		Hosts:      []string{"a.test"},
	}}); err != nil {
		t.Fatal(err)
	}
	breaker := jsplugin.DefaultCatalog().Breaker()
	before := breaker.Metrics(plugin.Manifest.ID, plugin.Manifest.Version)
	gen := jsplugin.DefaultCatalog().Generation()
	candidate := routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "rewritebreaker.example/chat"}, Model: routeengine.CandidateModel{UpstreamName: "m"}}
	for i := 0; i < 21; i++ {
		_, err := applyProviderRewrite(context.Background(), candidate, &testGatewayProtocol{}, map[string]any{"model": "m", "boom": true})
		if err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("call %d error = %v", i, err)
		}
	}
	rewritten, err := applyProviderRewrite(context.Background(), candidate, &testGatewayProtocol{}, map[string]any{"model": "m"})
	if err != nil || rewritten["added"] != "yes" || rewritten["model"] != "m" {
		t.Fatalf("recovered = %#v err = %v", rewritten, err)
	}
	after := breaker.Metrics(plugin.Manifest.ID, plugin.Manifest.Version)
	if after.Calls < before.Calls+22 || after.Errors < before.Errors+21 {
		t.Fatalf("metrics = %+v, before %+v", after, before)
	}
	if jsplugin.DefaultCatalog().Generation() != gen {
		t.Fatalf("generation = %d, want %d", jsplugin.DefaultCatalog().Generation(), gen)
	}
	found, ok := site.ProviderHookPlugin(jsplugin.DefaultCatalog().Registry(), "rewritebreaker.example/chat", "rewritePayload")
	if !ok || found.Manifest.ID != plugin.Manifest.ID {
		t.Fatalf("hook after failures = %v", ok)
	}
}

func TestRewritePayloadCallOrder(t *testing.T) {
	forward := functionSource(t, "execution.go", "func (h Handler) forwardGatewayRequest(")
	assertCallOrder(t, forward,
		"buildGatewayUpstreamPayload(",
		"applyProviderRewrite(",
		"applyRequestBillingMetadata(",
		"validatePayloadParams(",
		"payload[\"stream\"]",
		"applyGatewayClientImpersonationPayloadForAccount(",
	)
	diagnostic := functionSource(t, "site_model_diagnostic.go", "func (h Handler) forwardSiteModelTestRequest(")
	assertCallOrder(t, diagnostic,
		"buildGatewayUpstreamPayload(",
		"applyProviderRewrite(",
		"applyRequestBillingMetadata(",
		"payload[\"stream\"]",
		"applyGatewayClientImpersonationPayloadForAccount(",
		"json.Marshal(payload)",
	)
}

func TestRewriteProtocolName(t *testing.T) {
	cases := []struct {
		protocol gatewayProtocolAdapter
		want     string
	}{
		{openAIChatProtocolAdapter{}, "openai_chat"},
		{openAIResponsesProtocolAdapter{}, "openai_responses"},
		{openAIImagesProtocolAdapter{}, "openai_images"},
		{openAIEmbeddingsProtocolAdapter{}, "openai_embeddings"},
		{openAIAudioSpeechProtocolAdapter{}, "openai_audio_speech"},
		{mimoAudioSpeechProtocolAdapter{}, "openai_chat"},
		{anthropicMessagesProtocolAdapter{}, "anthropic_messages"},
		{&providerAnthropicMessagesProtocolAdapter{}, "anthropic_messages"},
		{&googleProtocolAdapter{}, "google_gemini"},
		{codexProtocolAdapter{}, "codex_responses"},
		{antigravityProtocolAdapter{}, "antigravity_gemini"},
		{&grokResponsesProtocolAdapter{}, "grok"},
		{grokImagesProtocolAdapter{}, "openai_images"},
		{&testGatewayProtocol{name: "custom_proto"}, "custom_proto"},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := rewriteProtocolName(tc.protocol); got != tc.want {
			t.Fatalf("%T = %q, want %q", tc.protocol, got, tc.want)
		}
	}
}

type rewriteBodyProtocol struct {
	*testGatewayProtocol
	payload map[string]any
}

func (p rewriteBodyProtocol) BuildUpstreamPayload(gatewayRequest, routeengine.Candidate) (map[string]any, error) {
	return clonePayload(p.payload), nil
}

type rewriteCapture struct {
	mu     sync.Mutex
	bodies []string
}

func newRewriteCapture() *rewriteCapture {
	return &rewriteCapture{}
}

func (c *rewriteCapture) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.bodies = append(c.bodies, string(raw))
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
}

func (c *rewriteCapture) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.bodies))
	copy(out, c.bodies)
	return out
}

func assertRewrittenBody(t *testing.T, body string, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("body %s key %s = %#v, want %#v", body, key, got[key], value)
		}
	}
}

func functionSource(t *testing.T, file, signature string) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(here), file))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start := strings.Index(src, signature)
	if start < 0 {
		t.Fatalf("%s missing %s", file, signature)
	}
	rest := src[start:]
	next := strings.Index(rest[1:], "\nfunc ")
	if next < 0 {
		return rest
	}
	return rest[:next+1]
}

func assertCallOrder(t *testing.T, body string, markers ...string) {
	t.Helper()
	prev := -1
	for _, marker := range markers {
		at := strings.Index(body, marker)
		if at < 0 || at <= prev {
			t.Fatalf("marker %s at %d, previous %d", marker, at, prev)
		}
		prev = at
	}
}
