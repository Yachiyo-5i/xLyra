package settings

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestDefaultSystemProxyConfigEncodesEmptyProxyList(t *testing.T) {
	t.Parallel()

	cfg := defaultSystemProxyConfig()
	if cfg.Proxies == nil {
		t.Fatal("default proxy list should be an empty slice, not nil")
	}
	if len(cfg.Proxies) != 0 {
		t.Fatalf("default proxy count = %d, want 0", len(cfg.Proxies))
	}

	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal default proxy config: %v", err)
	}
	if !strings.Contains(string(encoded), `"proxies":[]`) {
		t.Fatalf("default proxy config JSON = %s, want empty proxies array", encoded)
	}
}

func TestTestSystemProxyValidationRejectsUnsupportedTypeAndInvalidURLWithoutDialing(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil)
	for _, tc := range []struct {
		name        string
		body        string
		wantMessage string
	}{
		{
			name:        "unsupported proxy type",
			body:        `{"proxy":{"id":"local","name":"Local","type":"ssh","url":"http://127.0.0.1:9"}}`,
			wantMessage: "type must be one of",
		},
		{
			name:        "malformed proxy url",
			body:        `{"proxy":{"id":"local","name":"Local","type":"http","url":":bad"}}`,
			wantMessage: "url is invalid",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := settingsPerform(handler.TestSystemProxy, systemProxyTestJSONRequest(tc.body))

			settingsAssertStatus(t, rec, http.StatusOK)
			body := settingsDecodeJSON[systemProxyTestResponse](t, rec)
			if body.OK || body.Stage != "validate" || !strings.Contains(body.Message, tc.wantMessage) {
				t.Fatalf("validation response = %+v, want failure containing %q", body, tc.wantMessage)
			}
		})
	}
}

func TestTestSystemProxyBlocksCloudMetadataAddress(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil)
	rec := settingsPerform(handler.TestSystemProxy, systemProxyTestJSONRequest(`{"proxy":{"id":"meta","name":"Meta","type":"http","url":"http://169.254.169.254:80"}}`))

	settingsAssertStatus(t, rec, http.StatusOK)
	body := settingsDecodeJSON[systemProxyTestResponse](t, rec)
	if body.OK || body.Stage != "tcp_connect" || !strings.Contains(body.Message, "blocked") {
		t.Fatalf("metadata proxy response = %+v, want tcp_connect failure blocked by SSRF guard", body)
	}
}

func TestSystemProxyTestHTTPClientConfiguresHTTPSProxy(t *testing.T) {
	t.Parallel()

	parsed, err := url.Parse("https://proxy.example:8443")
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	client, err := systemProxyTestHTTPClient(proxyConfig{Type: " https "}, parsed)
	if err != nil {
		t.Fatalf("system proxy https client: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	req := settingsTestRequest(http.MethodGet, systemProxyTestURL, "")
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("proxy func: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != parsed.String() {
		t.Fatalf("proxy url = %v, want %s", proxyURL, parsed)
	}
}

func TestSystemProxyTestHTTPClientBuildsSOCKS5TransportWithoutDialing(t *testing.T) {
	t.Parallel()

	parsed, err := url.Parse("socks5://alice@127.0.0.1:1080")
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	client, err := systemProxyTestHTTPClient(proxyConfig{Type: "SOCKS5"}, parsed)
	if err != nil {
		t.Fatalf("system proxy socks5 client: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("socks5 transport should use DialContext instead of HTTP proxy func")
	}
	if transport.DialContext == nil {
		t.Fatal("socks5 transport should install a DialContext")
	}
	if client.Timeout != systemProxyTestTimeout {
		t.Fatalf("client timeout = %s, want %s", client.Timeout, systemProxyTestTimeout)
	}
}

func TestSystemProxyAuthUsernameWithoutPassword(t *testing.T) {
	t.Parallel()

	parsed, err := url.Parse("socks5://alice@127.0.0.1:1080")
	if err != nil {
		t.Fatalf("parse proxy url: %v", err)
	}

	auth := systemProxyAuth(parsed)
	if auth == nil || auth.User != "alice" || auth.Password != "" {
		t.Fatalf("auth = %#v, want username with empty password", auth)
	}
}

func systemProxyTestJSONRequest(body string) *http.Request {
	req := settingsTestRequest(http.MethodPost, "/settings/system-proxy/test", body)
	req.Header.Set("Content-Type", "application/json")
	return req
}
