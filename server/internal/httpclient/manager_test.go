package httpclient

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"xlyra/server/internal/config"
)

func TestNormalizeNetworkConfigTrimsAndLowercasesProxyFields(t *testing.T) {
	t.Parallel()

	got := NormalizeNetworkConfig(NetworkConfig{Proxies: []ProxyProfile{
		{ID: " proxy-1 ", Name: " Primary ", Type: "HTTP", URL: " http://127.0.0.1:8080 "},
	}})

	if len(got.Proxies) != 1 {
		t.Fatalf("expected one proxy, got %#v", got.Proxies)
	}
	proxy := got.Proxies[0]
	if proxy.ID != "proxy-1" || proxy.Name != "Primary" || proxy.Type != "http" || proxy.URL != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected normalized proxy: %#v", proxy)
	}
}

func TestValidateNetworkConfigRejectsDuplicateIDsAndInvalidSchemes(t *testing.T) {
	t.Parallel()

	if err := ValidateNetworkConfig(NetworkConfig{Proxies: []ProxyProfile{
		{ID: "proxy", Name: "Primary", Type: "http", URL: "http://127.0.0.1:8080"},
		{ID: "proxy", Name: "Secondary", Type: "http", URL: "http://127.0.0.1:8081"},
	}}); err == nil {
		t.Fatal("expected duplicate proxy ids to be rejected")
	}

	if err := ValidateNetworkConfig(NetworkConfig{Proxies: []ProxyProfile{
		{ID: "socks", Name: "Socks", Type: "socks5", URL: "http://127.0.0.1:1080"},
	}}); err == nil {
		t.Fatal("expected socks5 proxy with http URL to be rejected")
	}

	if err := ValidateNetworkConfig(NetworkConfig{Proxies: []ProxyProfile{
		{ID: "http", Name: "HTTP", Type: "http", URL: "socks5://127.0.0.1:1080"},
	}}); err == nil {
		t.Fatal("expected http proxy with socks5 URL to be rejected")
	}
}

func TestValidateNetworkConfigRejectsMissingFieldsDuplicateNamesAndBadURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  NetworkConfig
	}{
		{
			name: "missing id",
			cfg: NetworkConfig{Proxies: []ProxyProfile{
				{Name: "Primary", Type: "http", URL: "http://127.0.0.1:8080"},
			}},
		},
		{
			name: "missing name",
			cfg: NetworkConfig{Proxies: []ProxyProfile{
				{ID: "proxy", Type: "http", URL: "http://127.0.0.1:8080"},
			}},
		},
		{
			name: "duplicate names",
			cfg: NetworkConfig{Proxies: []ProxyProfile{
				{ID: "one", Name: "Primary", Type: "http", URL: "http://127.0.0.1:8080"},
				{ID: "two", Name: " primary ", Type: "https", URL: "https://127.0.0.1:8081"},
			}},
		},
		{
			name: "unsupported type",
			cfg: NetworkConfig{Proxies: []ProxyProfile{
				{ID: "proxy", Name: "Primary", Type: "ftp", URL: "http://127.0.0.1:8080"},
			}},
		},
		{
			name: "missing host",
			cfg: NetworkConfig{Proxies: []ProxyProfile{
				{ID: "proxy", Name: "Primary", Type: "http", URL: "http://"},
			}},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := ValidateNetworkConfig(tt.cfg); err == nil {
				t.Fatal("expected network config to be rejected")
			}
		})
	}
}

func TestReadNetworkConfigReadsProxyMapsFromConfigFile(t *testing.T) {
	t.Parallel()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	if err := confFile.Set("network", map[string]any{
		"proxies": []any{
			map[string]any{
				"id":   "proxy-1",
				"name": "Primary",
				"type": "HTTP",
				"url":  "http://127.0.0.1:8080",
			},
			"ignored",
		},
	}); err != nil {
		t.Fatalf("set network config: %v", err)
	}

	got := ReadNetworkConfig(confFile)

	if len(got.Proxies) != 1 {
		t.Fatalf("expected one parsed proxy, got %#v", got.Proxies)
	}
	if got.Proxies[0].ID != "proxy-1" || got.Proxies[0].Type != "http" {
		t.Fatalf("unexpected parsed proxy: %#v", got.Proxies[0])
	}
}

func TestReadNetworkConfigFallbacksAndTypedProxySlices(t *testing.T) {
	t.Parallel()

	if got := ReadNetworkConfig(nil); len(got.Proxies) != 0 {
		t.Fatalf("nil config proxies = %#v, want empty", got.Proxies)
	}

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	if err := confFile.Set("network", "invalid"); err != nil {
		t.Fatalf("set invalid network config: %v", err)
	}
	if got := ReadNetworkConfig(confFile); len(got.Proxies) != 0 {
		t.Fatalf("invalid raw network proxies = %#v, want empty", got.Proxies)
	}

	typed := NetworkConfig{Proxies: []ProxyProfile{{ID: "typed", Name: "Typed", Type: "http", URL: "http://127.0.0.1:8080"}}}
	if err := confFile.Set("network", typed); err != nil {
		t.Fatalf("set typed network config: %v", err)
	}
	got := ReadNetworkConfig(confFile)
	if len(got.Proxies) != 1 || got.Proxies[0].ID != "typed" {
		t.Fatalf("typed network config = %#v, want typed proxy", got.Proxies)
	}

	if err := confFile.Set("network", map[string]any{
		"proxies": []map[string]any{
			{"id": "map", "name": "Map", "type": "HTTPS", "url": "https://127.0.0.1:8443"},
		},
	}); err != nil {
		t.Fatalf("set map proxy config: %v", err)
	}
	got = ReadNetworkConfig(confFile)
	if len(got.Proxies) != 1 || got.Proxies[0].ID != "map" || got.Proxies[0].Type != "https" {
		t.Fatalf("map proxy config = %#v, want normalized map proxy", got.Proxies)
	}

	if err := confFile.Set("network", map[string]any{}); err != nil {
		t.Fatalf("set empty network config: %v", err)
	}
	if got := ReadNetworkConfig(confFile); len(got.Proxies) != 0 {
		t.Fatalf("empty network config proxies = %#v, want empty", got.Proxies)
	}
}

func TestManagerClientCachesByProfileAndCanCloseIdleConnections(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil)
	profile := Profile{RequestTimeout: 5 * time.Second}

	first, err := manager.Client(profile)
	if err != nil {
		t.Fatalf("first client: %v", err)
	}
	second, err := manager.Client(profile)
	if err != nil {
		t.Fatalf("second client: %v", err)
	}
	if first != second {
		t.Fatal("expected manager to cache identical profile clients")
	}
	if first.Timeout != 5*time.Second {
		t.Fatalf("client timeout = %s, want 5s", first.Timeout)
	}

	manager.CloseIdleConnections()
	third, err := manager.Client(profile)
	if err != nil {
		t.Fatalf("third client: %v", err)
	}
	if third == first {
		t.Fatal("expected CloseIdleConnections to clear cached clients")
	}
}

func TestManagerClientRejectsMissingProxyID(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil)
	if _, err := manager.Client(Profile{ProxyID: "missing"}); err == nil {
		t.Fatal("expected missing proxy id to return an error")
	}
}

func TestManagerClientUsesConfiguredProxyAndInvalidatesCacheOnConfigChange(t *testing.T) {
	t.Parallel()

	confFile, err := config.LoadConfigFile(t.TempDir())
	if err != nil {
		t.Fatalf("load config file: %v", err)
	}
	if err := confFile.Set("network", map[string]any{
		"proxies": []any{
			map[string]any{
				"id":   "primary",
				"name": "Primary",
				"type": "https",
				"url":  "https://127.0.0.1:8443",
			},
		},
	}); err != nil {
		t.Fatalf("set network config: %v", err)
	}

	manager := NewManager(confFile)
	first, err := manager.Client(Profile{ProxyID: " primary "})
	if err != nil {
		t.Fatalf("first client: %v", err)
	}
	second, err := manager.Client(Profile{ProxyID: "primary"})
	if err != nil {
		t.Fatalf("second client: %v", err)
	}
	if first != second {
		t.Fatal("expected configured proxy client to be cached")
	}

	transport, ok := first.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", first.Transport)
	}
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}
	gotProxy, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if gotProxy.String() != "https://127.0.0.1:8443" {
		t.Fatalf("proxy URL = %s, want configured https proxy", gotProxy)
	}

	if err := confFile.Set("network", map[string]any{
		"proxies": []any{
			map[string]any{
				"id":   "primary",
				"name": "Primary",
				"type": "https",
				"url":  "https://127.0.0.1:9443",
			},
		},
	}); err != nil {
		t.Fatalf("update network config: %v", err)
	}
	third, err := manager.Client(Profile{ProxyID: "primary"})
	if err != nil {
		t.Fatalf("third client: %v", err)
	}
	if third == first {
		t.Fatal("expected config change to clear cached client")
	}
}

func TestStreamingProfileDisablesRequestTimeout(t *testing.T) {
	t.Parallel()

	profile := StreamingProfile(DefaultProfile())
	if !profile.NoRequestTimeout || profile.RequestTimeout != 0 {
		t.Fatalf("unexpected streaming profile: %#v", profile)
	}
}

func TestNilManagerClientAndHTTPProxyClientBranches(t *testing.T) {
	t.Parallel()

	direct, err := (*Manager)(nil).Client(Profile{NoRequestTimeout: true})
	if err != nil {
		t.Fatalf("nil manager direct client: %v", err)
	}
	if direct.Timeout != 0 {
		t.Fatalf("direct client timeout = %s, want 0", direct.Timeout)
	}
	transport, ok := direct.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("direct transport = %T, want *http.Transport", direct.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("direct transport should not configure a proxy")
	}

	proxyClient, err := newHTTPClient(Profile{RequestTimeout: time.Second}, &ProxyProfile{
		ID:   "proxy",
		Name: "Proxy",
		Type: " HTTP ",
		URL:  " http://user:pass@127.0.0.1:8080 ",
	})
	if err != nil {
		t.Fatalf("http proxy client: %v", err)
	}
	if proxyClient.Timeout != time.Second {
		t.Fatalf("proxy client timeout = %s, want 1s", proxyClient.Timeout)
	}
	transport, ok = proxyClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("proxy transport = %T, want *http.Transport", proxyClient.Transport)
	}
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}
	gotProxy, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("resolve proxy: %v", err)
	}
	if gotProxy.String() != "http://user:pass@127.0.0.1:8080" {
		t.Fatalf("proxy URL = %s, want configured proxy", gotProxy)
	}
}

func TestNewHTTPClientSocksAndUnsupportedProxyBranches(t *testing.T) {
	t.Parallel()

	socksClient, err := newHTTPClient(Profile{}, &ProxyProfile{
		ID:   "socks",
		Name: "Socks",
		Type: "socks5",
		URL:  "socks5://user:pass@127.0.0.1:1080",
	})
	if err != nil {
		t.Fatalf("socks client: %v", err)
	}
	transport, ok := socksClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("socks transport = %T, want *http.Transport", socksClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("socks transport should use DialContext instead of HTTP Proxy")
	}
	if _, err := newHTTPClient(Profile{}, &ProxyProfile{Type: "ftp", URL: "http://127.0.0.1:8080"}); err == nil {
		t.Fatal("expected unsupported proxy type to fail")
	}
	if _, err := newHTTPClient(Profile{}, &ProxyProfile{Type: "http", URL: "://bad"}); err == nil {
		t.Fatal("expected invalid proxy URL to fail")
	}
}

func TestProxyAuthAndCacheKeyIncludeProxyIdentity(t *testing.T) {
	t.Parallel()

	if got := proxyAuth(nil); got != nil {
		t.Fatalf("proxyAuth(nil) = %#v, want nil", got)
	}
	noUser, _ := url.Parse("http://127.0.0.1:8080")
	if got := proxyAuth(noUser); got != nil {
		t.Fatalf("proxyAuth(no user) = %#v, want nil", got)
	}
	withUser, _ := url.Parse("socks5://user:pass@127.0.0.1:1080")
	auth := proxyAuth(withUser)
	if auth == nil || auth.User != "user" || auth.Password != "pass" {
		t.Fatalf("proxyAuth(with user) = %#v, want user/pass", auth)
	}

	profile := normalizeProfile(Profile{RequestTimeout: time.Second, ProxyID: " proxy "})
	directKey := cacheKey(profile, nil)
	proxyKey := cacheKey(profile, &ProxyProfile{ID: "proxy", Type: "http", URL: "http://127.0.0.1:8080"})
	if directKey == proxyKey {
		t.Fatalf("cache keys should differ for direct and proxy clients: %q", directKey)
	}
	if !containsAll(proxyKey, "proxy", "http", "http://127.0.0.1:8080") {
		t.Fatalf("proxy cache key %q should include proxy identity", proxyKey)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
