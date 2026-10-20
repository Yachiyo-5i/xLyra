package adapter

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGenericProviderListsModelsWithBearerAndHeaderAuth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		auth       string
		header     string
		wantHeader string
		wantValue  string
	}{
		{name: "bearer", auth: "", header: "Authorization", wantValue: "Bearer sk-test"},
		{name: "bearer word", auth: "Bearer", header: "Authorization", wantValue: "Bearer sk-test"},
		{name: "header", auth: "header:X-Api-Key", header: "X-Api-Key", wantValue: "sk-test"},
		{name: "header scheme case", auth: "Header: X-Api-Key", header: "X-Api-Key", wantValue: "sk-test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotPath string
			var gotHeader http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.RequestURI()
				gotHeader = r.Header.Clone()
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"id":"","object":"model"},{"id":"claude-sonnet-4","object":"model","owned_by":"acme"}]}`)
			}))
			t.Cleanup(server.Close)
			client := genericDialClient(map[string]string{"a.test": server.Listener.Addr().String()})

			module, err := NewGenericProvider(GenericProviderConfig{
				SiteType:        " ACME.EXAMPLE/Chat ",
				DisplayName:     " Acme ",
				OfficialBaseURL: "http://a.test/",
				Auth:            tc.auth,
				EndpointTypes:   []string{" openai ", "anthropic-messages"},
				AllowedHosts:    []string{"a.test"},
			})
			if err != nil {
				t.Fatal(err)
			}
			provider := module.(GenericProvider)
			if provider.displayName != "Acme" || provider.DefaultBaseURL() != "http://a.test" {
				t.Fatalf("provider = %#v", provider)
			}
			if got := provider.SiteTypes(); len(got) != 1 || got[0] != "acme.example/chat" {
				t.Fatalf("site types = %#v", got)
			}
			if provider.Scope() != HealthProbeCredentialScope {
				t.Fatalf("scope = %q", provider.Scope())
			}
			wantCapabilities := []Capability{CapabilityValidateCredential, CapabilityListModels, CapabilityHealthProbe}
			if got := provider.Capabilities(); !sameCapabilities(got, wantCapabilities) {
				t.Fatalf("capabilities = %#v", got)
			}

			models, err := provider.ListModels(context.Background(), SiteConfig{BaseURL: "http://a.test/", Client: client}, "sk-test")
			if err != nil {
				t.Fatal(err)
			}
			if gotPath != "/v1/models" {
				t.Fatalf("path = %q", gotPath)
			}
			if gotHeader.Get(tc.header) != tc.wantValue {
				t.Fatalf("%s = %q", tc.header, gotHeader.Get(tc.header))
			}
			if tc.header != "Authorization" && gotHeader.Get("Authorization") != "" {
				t.Fatalf("authorization = %q", gotHeader.Get("Authorization"))
			}
			if len(models) != 1 || models[0].UpstreamName != "claude-sonnet-4" {
				t.Fatalf("models = %#v", models)
			}
			gotTypes, _ := models[0].Capabilities["supported_endpoint_types"].([]string)
			if len(gotTypes) != 2 || gotTypes[0] != "openai" || gotTypes[1] != "anthropic-messages" {
				t.Fatalf("endpoint types = %#v", gotTypes)
			}
			gotTypes[0] = "google-gemini"
			again, err := provider.ProbeHealth(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: client}, "sk-test")
			if err != nil {
				t.Fatal(err)
			}
			againTypes, _ := again[0].Capabilities["supported_endpoint_types"].([]string)
			if len(againTypes) != 2 || againTypes[0] != "openai" {
				t.Fatalf("endpoint types changed to %#v", againTypes)
			}
			if err := provider.ValidateCredentials(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: client}, "sk-test"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGenericProviderUsesConfiguredModelsPath(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"m"}]}`)
	}))
	t.Cleanup(server.Close)
	client := genericDialClient(map[string]string{"a.test": server.Listener.Addr().String()})

	module, err := NewGenericProvider(GenericProviderConfig{
		SiteType:        "acme.example/chat",
		OfficialBaseURL: "http://a.test",
		ModelsPath:      "/api/v1/models?limit=1",
		EndpointTypes:   []string{"google-gemini"},
		AllowedHosts:    []string{"a.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	models, err := module.(GenericProvider).ListModels(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: client}, "")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/models?limit=1" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(models) != 1 {
		t.Fatalf("models = %#v", models)
	}
	var seenAuth bool
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Key") != ""
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[]}`)
	})
	models, err = module.(GenericProvider).ListModels(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: client}, "   ")
	if err != nil {
		t.Fatal(err)
	}
	if seenAuth {
		t.Fatal("empty api key set an auth header")
	}
	if len(models) != 0 {
		t.Fatalf("empty data = %#v", models)
	}
	if err := module.(GenericProvider).ValidateCredentials(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: client}, ""); err != nil {
		t.Fatal(err)
	}
}

func TestGenericProviderUpstreamErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: "nope", want: "upstream returned 401"},
		{name: "not json", status: http.StatusOK, body: "not-json", want: "decode upstream models"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := tc.body
			if tc.name == "unauthorized" {
				body = strings.Repeat("x", 600)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(server.Close)
			client := genericDialClient(map[string]string{"a.test": server.Listener.Addr().String()})
			provider := mustGenericProvider(t, "https://a.test")
			_, err := provider.ListModels(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: client}, "sk-test")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if tc.name == "unauthorized" && strings.Contains(err.Error(), strings.Repeat("x", 513)) {
				t.Fatalf("error body was not limited: %s", err)
			}
			if err := provider.ValidateCredentials(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: client}, "sk-test"); err == nil {
				t.Fatal("validate succeeded")
			}
			if _, err := provider.ProbeHealth(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: client}, "sk-test"); err == nil {
				t.Fatal("probe succeeded")
			}
		})
	}
}

func TestNewGenericProviderRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	base := GenericProviderConfig{
		SiteType:        "acme.example/chat",
		OfficialBaseURL: "https://api.example.com",
		EndpointTypes:   []string{"openai"},
		AllowedHosts:    []string{"api.example.com"},
	}
	cases := []struct {
		name string
		edit func(*GenericProviderConfig)
		want string
	}{
		{name: "site type required", edit: func(cfg *GenericProviderConfig) { cfg.SiteType = " " }, want: "site type is required"},
		{name: "site type slash", edit: func(cfg *GenericProviderConfig) { cfg.SiteType = "openai" }, want: `must contain "/"`},
		{name: "official url empty", edit: func(cfg *GenericProviderConfig) { cfg.OfficialBaseURL = "" }, want: "official base url"},
		{name: "official url scheme", edit: func(cfg *GenericProviderConfig) { cfg.OfficialBaseURL = "ftp://api.example.com" }, want: "official base url"},
		{name: "official url host", edit: func(cfg *GenericProviderConfig) { cfg.OfficialBaseURL = "https://" }, want: "official base url"},
		{name: "endpoint empty", edit: func(cfg *GenericProviderConfig) { cfg.EndpointTypes = nil }, want: "endpoint types are required"},
		{name: "endpoint blank slice", edit: func(cfg *GenericProviderConfig) { cfg.EndpointTypes = []string{} }, want: "endpoint types are required"},
		{name: "endpoint invalid", edit: func(cfg *GenericProviderConfig) { cfg.EndpointTypes = []string{"OpenAI"} }, want: "not supported"},
		{name: "endpoint unknown", edit: func(cfg *GenericProviderConfig) { cfg.EndpointTypes = []string{"openai-chat"} }, want: "not supported"},
		{name: "auth scheme", edit: func(cfg *GenericProviderConfig) { cfg.Auth = "basic" }, want: "bearer or header"},
		{name: "auth bearer extra", edit: func(cfg *GenericProviderConfig) { cfg.Auth = "bearer token" }, want: "bearer or header"},
		{name: "auth header empty", edit: func(cfg *GenericProviderConfig) { cfg.Auth = "header:" }, want: "not valid"},
		{name: "auth header space", edit: func(cfg *GenericProviderConfig) { cfg.Auth = "header:Bad Name" }, want: "not valid"},
		{name: "auth header colon", edit: func(cfg *GenericProviderConfig) { cfg.Auth = "header:Bad:Name" }, want: "not valid"},
		{name: "hosts empty", edit: func(cfg *GenericProviderConfig) { cfg.AllowedHosts = nil }, want: "allowed hosts"},
		{name: "hosts blank slice", edit: func(cfg *GenericProviderConfig) { cfg.AllowedHosts = []string{} }, want: "allowed hosts"},
		{name: "hosts too many", edit: func(cfg *GenericProviderConfig) {
			hosts := make([]string, 17)
			for i := range hosts {
				hosts[i] = strings.Repeat("a", i+1) + ".example"
			}
			cfg.AllowedHosts = hosts
		}, want: "allowed hosts"},
		{name: "hosts duplicate", edit: func(cfg *GenericProviderConfig) { cfg.AllowedHosts = []string{"api.example.com", "api.example.com"} }, want: "duplicate host"},
		{name: "hosts ip", edit: func(cfg *GenericProviderConfig) { cfg.AllowedHosts = []string{"127.0.0.1"} }, want: "must not be an IP"},
		{name: "hosts uppercase", edit: func(cfg *GenericProviderConfig) { cfg.AllowedHosts = []string{"API.Example.com"} }, want: "lowercase"},
		{name: "hosts port", edit: func(cfg *GenericProviderConfig) { cfg.AllowedHosts = []string{"api.example.com:443"} }, want: "without a scheme, port, or path"},
		{name: "hosts scheme", edit: func(cfg *GenericProviderConfig) { cfg.AllowedHosts = []string{"https://api.example.com"} }, want: "without a scheme, port, or path"},
		{name: "hosts no dot", edit: func(cfg *GenericProviderConfig) { cfg.AllowedHosts = []string{"localhost"} }, want: "must be a DNS hostname"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.edit(&cfg)
			_, err := NewGenericProvider(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}

	cfg := base
	cfg.EndpointTypes = []string{"openai"}
	module, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.EndpointTypes[0] = "anthropic-messages"
	if module.(GenericProvider).endpointTypes[0] != "openai" {
		t.Fatal("caller mutated provider endpoint types")
	}
}

func TestGenericModelsPathMatchesPluginAndRuntimeRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		ok   bool
	}{
		{name: "chat completions", path: "/v1/chat/completions", ok: true},
		{name: "paas chat completions", path: "/api/paas/v4/chat/completions", ok: true},
		{name: "models", path: "/v1/models", ok: true},
		{name: "query", path: "/v1/models?limit=1", ok: true},
		{name: "encoded dotdot", path: "/%2e%2e/x", ok: false},
		{name: "encoded dot", path: "/%2E/x", ok: false},
		{name: "encoded dotdot upper", path: "/%2E%2E/x", ok: false},
		{name: "dot segment", path: "/a/./b", ok: false},
		{name: "dotdot segment", path: "/a/../b", ok: false},
		{name: "double slash", path: "//x", ok: false},
		{name: "backslash", path: "/a\\b", ok: false},
		{name: "hash", path: "/a#b", ok: false},
		{name: "scheme", path: "/a://b", ok: false},
		{name: "encoded slash", path: "/a%2fb", ok: false},
		{name: "encoded slash upper", path: "/a%2Fb", ok: false},
		{name: "encoded backslash", path: "/a%5cb", ok: false},
		{name: "encoded backslash upper", path: "/a%5Cb", ok: false},
		{name: "empty", path: "", ok: false},
		{name: "relative", path: "v1/models", ok: false},
		{name: "bad escape", path: "/a%zz", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGenericModelsPath(tc.path)
			if tc.ok != (err == nil) {
				t.Fatalf("validateGenericModelsPath(%q) = %v", tc.path, err)
			}
			_, err = NewGenericProvider(GenericProviderConfig{
				SiteType:        "acme.example/chat",
				OfficialBaseURL: "https://api.example.com",
				ModelsPath:      tc.path,
				EndpointTypes:   []string{"openai"},
				AllowedHosts:    []string{"api.example.com"},
			})
			if tc.path == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if tc.ok != (err == nil) {
				t.Fatalf("NewGenericProvider path %q error = %v", tc.path, err)
			}
		})
	}

	needles := []string{
		"strings.HasPrefix(",
		`"//"`,
		`"#"`,
		`"://"`,
		"strings.Cut(",
		"\"?\"",
		`== "."`,
		`== ".."`,
		"url.PathUnescape",
		"strings.ContainsAny(",
		"\\",
	}
	bodies := []string{
		sourceFuncBody(t, "generic_provider.go", "validateGenericModelsPath"),
		sourceFuncBody(t, "../jsplugin/urlcheck.go", "validatePluginPath"),
		sourceFuncBody(t, "../gateway/protocol_spec_runtime.go", "validateRuntimeProviderPath"),
	}
	for _, body := range bodies {
		for _, needle := range needles {
			if !strings.Contains(body, needle) {
				t.Fatalf("path rule missing %q in:\n%s", needle, body)
			}
		}
	}
}

func mustGenericProvider(t *testing.T, baseURL string) GenericProvider {
	t.Helper()
	module, err := NewGenericProvider(GenericProviderConfig{
		SiteType:        "acme.example/chat",
		OfficialBaseURL: baseURL,
		EndpointTypes:   []string{"openai"},
		AllowedHosts:    []string{"a.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return module.(GenericProvider)
}

func genericDialClient(routes map[string]string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if mapped, ok := routes[host]; ok {
			addr = mapped
		}
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

func sameCapabilities(got, want []Capability) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func sourceFuncBody(t *testing.T, path, name string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src := string(raw)
	start := strings.Index(src, "func "+name+"(")
	if start < 0 {
		t.Fatalf("%s missing %s", path, name)
	}
	rel := strings.Index(src[start:], "{")
	if rel < 0 {
		t.Fatalf("%s %s missing body", path, name)
	}
	depth := 0
	for i := start + rel; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	t.Fatalf("%s %s body is not closed", path, name)
	return ""
}
