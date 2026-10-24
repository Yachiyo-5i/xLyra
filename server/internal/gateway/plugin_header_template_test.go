package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
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

func TestPluginHeaderTemplateApplied(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	headers, base := capturePluginHeaders(t, "acme.example/chat", gatewayEndpointMessages, downstreamPluginHeaders(), site.PluginSiteRequest{
		Headers: jsplugin.HeaderMap(map[string]string{
			"Authorization": "Bearer {{cred.api_key}}",
			"X-Client":      "xlyra",
			"X-Base":        "{{site.baseURL}}",
		}),
	}, &headerSnapshotProtocol{})
	if headers.Get("Authorization") != "Bearer sk-test" || headers.Get("X-Client") != "xlyra" || headers.Get("X-Base") != base {
		t.Fatalf("headers:\n%s", formatHeaders(headers))
	}
	if headers.Get("X-Stainless-Lang") != "js" || headers.Get("Anthropic-Beta") != "context-1m" {
		t.Fatalf("passthrough dropped:\n%s", formatHeaders(headers))
	}
	if headers.Get("Cookie") != "" {
		t.Fatalf("cookie = %q", headers.Get("Cookie"))
	}
}

func TestPluginHeaderTemplateStrip(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	headers, _ := capturePluginHeaders(t, "acme.example/chat", gatewayEndpointMessages, downstreamPluginHeaders(), site.PluginSiteRequest{
		Headers: jsplugin.HeaderMap(map[string]string{
			"Authorization": "Bearer {{cred.api_key}}",
			"X-Client":      "xlyra",
		}),
		StripDownstreamHeaders: true,
	}, &headerSnapshotProtocol{})
	if headers.Get("Authorization") != "Bearer sk-test" || headers.Get("X-Client") != "xlyra" {
		t.Fatalf("template headers:\n%s", formatHeaders(headers))
	}
	if headers.Get("Content-Type") != "application/json" || headers.Get("Accept") != "application/json" {
		t.Fatalf("kept headers:\n%s", formatHeaders(headers))
	}
	for _, name := range []string{"X-Stainless-Lang", "Anthropic-Beta", "Cookie", "X-From-Adapter"} {
		if headers.Get(name) != "" {
			t.Fatalf("%s = %q", name, headers.Get(name))
		}
	}

	strippedAuth, _ := capturePluginHeaders(t, "acme.example/chat", gatewayEndpointMessages, downstreamPluginHeaders(), site.PluginSiteRequest{
		Headers:                jsplugin.HeaderMap(map[string]string{"X-Client": "xlyra"}),
		StripDownstreamHeaders: true,
	}, &headerSnapshotProtocol{})
	if strippedAuth.Get("Authorization") != "" || strippedAuth.Get("X-Client") != "xlyra" || strippedAuth.Get("X-From-Adapter") != "" {
		t.Fatalf("undeclared auth:\n%s", formatHeaders(strippedAuth))
	}
	if strippedAuth.Get("Content-Type") == "" || strippedAuth.Get("Accept") == "" {
		t.Fatalf("content headers removed:\n%s", formatHeaders(strippedAuth))
	}
}

func TestPluginHeaderTemplateOrder(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	protocol := &headerSnapshotProtocol{}
	headers, _ := capturePluginHeaders(t, "acme.example/chat", gatewayEndpointMessages, downstreamPluginHeaders(), site.PluginSiteRequest{
		Headers: jsplugin.HeaderMap(map[string]string{
			"X-Client":      "from-template",
			"Authorization": "Bearer {{cred.api_key}}",
		}),
	}, protocol)
	if headers.Get("X-Client") != "from-template" {
		t.Fatalf("upstream X-Client = %q", headers.Get("X-Client"))
	}
	if protocol.signed.Get("X-Client") != "from-template" || protocol.signed.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("signed before template:\n%s", formatHeaders(protocol.signed))
	}
	if protocol.signed.Get("X-Stainless-Lang") != "js" {
		t.Fatalf("template ran before passthrough:\n%s", formatHeaders(protocol.signed))
	}
}

func TestPluginHeaderTemplateUnconfiguredMatchesBuiltin(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	var mu sync.Mutex
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)
	downstream := downstreamPluginHeaders()
	forward := func(siteType, path string, request site.PluginSiteRequest) http.Header {
		t.Helper()
		handler, candidate := pluginUpstreamFixture(t, siteType, server.URL, []string{"127.0.0.1"}, true, &http.Client{Timeout: 5 * time.Second}, 1)
		if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
			Descriptor: site.SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
			PluginID:   "acme.example",
			Enabled:    true,
			Hosts:      []string{"127.0.0.1"},
			Request:    request,
		}}); err != nil {
			t.Fatal(err)
		}
		result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-snapshot", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{
			DownstreamPath:    path,
			DownstreamHeaders: downstream,
		}, nil, &testGatewayProtocol{})
		if !result.success || result.errorType != "" {
			t.Fatalf("%s %s type=%q message=%q", siteType, path, result.errorType, result.errorMessage)
		}
		mu.Lock()
		defer mu.Unlock()
		return got.Clone()
	}
	for _, path := range []string{gatewayEndpointChatCompletions, gatewayEndpointMessages} {
		builtin := forward("openai", path, site.PluginSiteRequest{})
		plugin := forward("acme.example/chat", path, site.PluginSiteRequest{})
		if !reflect.DeepEqual(builtin, plugin) {
			t.Fatalf("%s builtin:\n%splugin:\n%s", path, formatHeaders(builtin), formatHeaders(plugin))
		}
		if builtin.Get("Authorization") != "Bearer sk-test" || builtin.Get("Content-Type") != "application/json" {
			t.Fatalf("%s snapshot:\n%s", path, formatHeaders(builtin))
		}
		if path == gatewayEndpointMessages {
			if builtin.Get("X-Stainless-Lang") != "js" || builtin.Get("Anthropic-Beta") != "context-1m" || builtin.Get("Cookie") != "" {
				t.Fatalf("messages snapshot:\n%s", formatHeaders(builtin))
			}
		} else if builtin.Get("X-Stainless-Lang") != "" {
			t.Fatalf("chat snapshot forwarded stainless:\n%s", formatHeaders(builtin))
		}
	}
	templated := site.PluginSiteRequest{
		Headers:                jsplugin.HeaderMap(map[string]string{"X-Client": "from-template", "Authorization": "Bearer {{cred.api_key}}"}),
		StripDownstreamHeaders: true,
	}
	builtin := forward("openai", gatewayEndpointMessages, site.PluginSiteRequest{})
	withTemplate := forward("openai", gatewayEndpointMessages, templated)
	if !reflect.DeepEqual(builtin, withTemplate) {
		t.Fatalf("builtin changed by a plugin template:\n%s\n%s", formatHeaders(builtin), formatHeaders(withTemplate))
	}
}

func TestPluginHeaderTemplateRenderFailure(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	secret := "sk-super-secret-value"
	hits := &atomic.Int32{}
	assertHeaderTemplateFailure(t, secret, site.PluginSiteRequest{
		Headers: jsplugin.HeaderMap(map[string]string{"X-Domain": "{{cred.domain}}"}),
	}, hits, "X-Domain", "missing variable cred.domain")

	hits = &atomic.Int32{}
	assertHeaderTemplateFailure(t, "super-secret-key\r\nX-Evil: 1", site.PluginSiteRequest{
		Headers: jsplugin.HeaderMap(map[string]string{"Authorization": "Bearer {{cred.api_key}}"}),
	}, hits, "Authorization", "control character")
}

func TestPluginHeaderTemplateDiagnostic(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	var mu sync.Mutex
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)
	baseURL := server.URL + "/"
	handler, candidate, credential := pluginUpstreamFixtureWithSecret(t, "acme.example/chat", baseURL, []string{"127.0.0.1"}, true, &http.Client{Timeout: 5 * time.Second}, 1, "sk-test")
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"127.0.0.1"},
		Request: site.PluginSiteRequest{
			Headers: jsplugin.HeaderMap(map[string]string{
				"Authorization": "Bearer {{cred.api_key}}",
				"X-Client":      "xlyra",
				"X-Base":        "{{site.baseURL}}",
			}),
			StripDownstreamHeaders: true,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	protocol := &headerSnapshotProtocol{}
	result := handler.forwardSiteModelTestRequest(context.Background(), "site-model-test/headers", uuid.New(), candidate, gatewayRequest{
		DownstreamPath:    gatewayEndpointMessages,
		DownstreamHeaders: downstreamPluginHeaders(),
	}, protocol, credential)
	if !result.success || result.errorType != "" {
		t.Fatalf("diagnostic success=%v type=%q message=%q", result.success, result.errorType, result.errorMessage)
	}
	mu.Lock()
	headers := got.Clone()
	mu.Unlock()
	if headers.Get("Authorization") != "Bearer sk-test" || headers.Get("X-Client") != "xlyra" || headers.Get("X-Base") != strings.TrimRight(baseURL, "/") {
		t.Fatalf("diagnostic headers:\n%s", formatHeaders(headers))
	}
	if headers.Get("X-From-Adapter") != "" || headers.Get("X-Stainless-Lang") != "" {
		t.Fatalf("diagnostic strip:\n%s", formatHeaders(headers))
	}
	if protocol.signed.Get("X-Client") != "xlyra" {
		t.Fatalf("diagnostic signed:\n%s", formatHeaders(protocol.signed))
	}

	hits := &atomic.Int32{}
	handler, candidate, credential = pluginUpstreamFixtureWithSecret(t, "acme.example/chat", "http://127.0.0.1:1", []string{"127.0.0.1"}, true, pluginCountingClient(hits), 1, "sk-super-secret-value")
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"127.0.0.1"},
		Request:    site.PluginSiteRequest{Headers: jsplugin.HeaderMap(map[string]string{"X-Domain": "{{cred.domain}}"})},
	}}); err != nil {
		t.Fatal(err)
	}
	result = handler.forwardSiteModelTestRequest(context.Background(), "site-model-test/header-fail", uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, &testGatewayProtocol{}, credential)
	if result.statusCode != http.StatusBadGateway || result.errorType != "plugin_header_template_failed" || hits.Load() != 0 {
		t.Fatalf("diagnostic fail status=%d type=%q message=%q hits=%d", result.statusCode, result.errorType, result.errorMessage, hits.Load())
	}
	if !strings.Contains(result.errorMessage, "X-Domain") || strings.Contains(result.errorMessage, "sk-super-secret-value") {
		t.Fatalf("diagnostic message = %q", result.errorMessage)
	}
}

func assertHeaderTemplateFailure(t *testing.T, secret string, request site.PluginSiteRequest, hits *atomic.Int32, headerName, reason string) {
	t.Helper()
	handler, candidate := pluginHeaderFailureFixture(t, secret, request, hits)
	result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-header-fail", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, nil, &testGatewayProtocol{})
	if result.statusCode != http.StatusBadGateway || result.errorType != "plugin_header_template_failed" || hits.Load() != 0 {
		t.Fatalf("status=%d type=%q message=%q hits=%d", result.statusCode, result.errorType, result.errorMessage, hits.Load())
	}
	if result.credentialDecision.ShouldTryNextCredential || result.credentialDecision.Action != "stop" || result.credentialAttempt != 1 || result.credentialTotal != 2 {
		t.Fatalf("decision attempt %d/%d %#v", result.credentialAttempt, result.credentialTotal, result.credentialDecision)
	}
	if !strings.Contains(result.errorMessage, headerName) || !strings.Contains(result.errorMessage, reason) || strings.Contains(result.errorMessage, secret) {
		t.Fatalf("message = %q", result.errorMessage)
	}
}

func pluginHeaderFailureFixture(t *testing.T, secret string, request site.PluginSiteRequest, hits *atomic.Int32) (Handler, routeengine.Candidate) {
	t.Helper()
	handler, candidate, _ := pluginUpstreamFixtureWithSecret(t, "acme.example/chat", "http://127.0.0.1:1", []string{"127.0.0.1"}, true, pluginCountingClient(hits), 2, secret)
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"127.0.0.1"},
		Request:    request,
	}}); err != nil {
		t.Fatal(err)
	}
	return handler, candidate
}

func capturePluginHeaders(t *testing.T, siteType, path string, downstream http.Header, request site.PluginSiteRequest, protocol gatewayProtocolAdapter) (http.Header, string) {
	t.Helper()
	var mu sync.Mutex
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)
	baseURL := server.URL + "/"
	handler, candidate := pluginUpstreamFixture(t, siteType, baseURL, []string{"127.0.0.1"}, true, &http.Client{Timeout: 5 * time.Second}, 1)
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"127.0.0.1"},
		Request:    request,
	}}); err != nil {
		t.Fatal(err)
	}
	result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-headers", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{
		DownstreamPath:    path,
		DownstreamHeaders: downstream,
	}, nil, protocol)
	if !result.success || result.errorType != "" {
		t.Fatalf("%s %s success=%v type=%q message=%q", siteType, path, result.success, result.errorType, result.errorMessage)
	}
	mu.Lock()
	defer mu.Unlock()
	return got.Clone(), strings.TrimRight(baseURL, "/")
}

func downstreamPluginHeaders() http.Header {
	return http.Header{
		"X-Stainless-Lang": {"js"},
		"Anthropic-Beta":   {"context-1m"},
		"Cookie":           {"session=secret"},
		"X-Client":         {"from-downstream"},
	}
}

func formatHeaders(h http.Header) string {
	if h == nil {
		return "<nil>\n"
	}
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s: %q\n", key, h.Values(key))
	}
	return b.String()
}

type headerSnapshotProtocol struct {
	testGatewayProtocol
	signed http.Header
}

func (p *headerSnapshotProtocol) ApplyUpstreamHeaders(req *http.Request, _, _ string, _ bool) {
	req.Header.Set("X-From-Adapter", "adapter")
	req.Header.Set("X-Client", "from-adapter")
}

func (p *headerSnapshotProtocol) SignUpstreamRequest(req *http.Request, _ []byte, _ string) error {
	if req != nil {
		p.signed = req.Header.Clone()
	}
	return nil
}
