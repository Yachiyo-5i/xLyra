package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"
)

func TestNewGuardedTransportBlocksMetadataAndAllowsLoopback(t *testing.T) {
	orig := ssrfBlockPrivateNetworks
	ssrfBlockPrivateNetworks = false
	t.Cleanup(func() { ssrfBlockPrivateNetworks = orig })

	transport := NewGuardedTransport(time.Second)
	if transport.Proxy == nil || transport.DialContext == nil {
		t.Fatal("guarded transport lost the default proxy or dial")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	const token = "METADATA_TOKEN_9f3c"
	conn, err := transport.DialContext(ctx, "tcp", "169.254.169.254:80")
	if conn != nil {
		_ = conn.Close()
		t.Fatal("metadata dial connected")
	}
	if err == nil || !strings.Contains(err.Error(), "blocked connection") {
		t.Fatalf("metadata dial = %v", err)
	}
	if strings.Contains(err.Error(), "no route to host") || strings.Contains(err.Error(), token) || strings.Contains(err.Error(), "http://") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("metadata dial error = %v", err)
	}
	conn, err = transport.DialContext(ctx, "tcp", "127.0.0.1:1")
	if conn != nil {
		_ = conn.Close()
	}
	if err != nil && strings.Contains(err.Error(), "blocked connection") {
		t.Fatalf("loopback dial blocked: %v", err)
	}
}

func TestNewGuardedDialerAppliesSSRFGuard(t *testing.T) {
	dialer := NewGuardedDialer(time.Second)
	if dialer.Control == nil {
		t.Fatal("NewGuardedDialer must install a Control hook")
	}
	if err := dialer.Control("tcp", "169.254.169.254:80", nil); err == nil {
		t.Fatal("guarded dialer must block cloud metadata address")
	}
	if err := dialer.Control("tcp", "8.8.8.8:443", nil); err != nil {
		t.Fatalf("guarded dialer must allow public address, got %v", err)
	}
}

func TestSSRFGuardControlBlocksNonPublicTargets(t *testing.T) {
	cases := []struct {
		address     string
		blockPriv   bool
		wantBlocked bool
	}{
		{"169.254.169.254:80", false, true}, // cloud metadata — always blocked
		{"[fe80::1]:443", false, true},      // link-local v6 — always blocked
		{"0.0.0.0:80", false, true},         // unspecified — always blocked
		{"8.8.8.8:443", false, false},       // public — allowed
		{"93.184.216.34:443", false, false}, // public — allowed
		{"127.0.0.1:8080", false, false},    // loopback allowed by default
		{"10.0.0.5:80", false, false},       // private allowed by default
		{"127.0.0.1:8080", true, true},      // loopback blocked when strict
		{"10.0.0.5:80", true, true},         // private blocked when strict
		{"192.168.1.10:80", true, true},     // private blocked when strict
		{"nonsense:80", false, true},        // non-ip host — blocked
	}

	orig := ssrfBlockPrivateNetworks
	defer func() { ssrfBlockPrivateNetworks = orig }()

	for _, tc := range cases {
		ssrfBlockPrivateNetworks = tc.blockPriv
		err := ssrfGuardControl("tcp", tc.address, nil)
		if (err != nil) != tc.wantBlocked {
			t.Fatalf("ssrfGuardControl(%q, blockPriv=%v) err=%v, wantBlocked=%v", tc.address, tc.blockPriv, err, tc.wantBlocked)
		}
	}
}

// clearProxyEnv isolates proxy selection. t.Setenv restores the process
// environment when the test ends. Empty values are treated as unset.
func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"HTTP_PROXY", "http_proxy",
		"HTTPS_PROXY", "https_proxy",
		"ALL_PROXY", "all_proxy",
		"NO_PROXY", "no_proxy",
		"REQUEST_METHOD",
	} {
		t.Setenv(key, "")
	}
}

func setBlockPrivateNetworks(t *testing.T, block bool) {
	t.Helper()
	previous := ssrfBlockPrivateNetworks
	ssrfBlockPrivateNetworks = block
	t.Cleanup(func() { ssrfBlockPrivateNetworks = previous })
}

func TestGuardedTransportRoundTripBlocksMetadataWithoutProxy(t *testing.T) {
	// Prime net/http's process-wide proxy cache while the ambient proxy is
	// still visible. The guarded transport must not keep that cached proxy
	// after the environment is cleared.
	probe, err := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := http.ProxyFromEnvironment(probe); err != nil {
		t.Fatalf("prime proxy cache: %v", err)
	}
	clearProxyEnv(t)
	setBlockPrivateNetworks(t, false)

	transport := NewGuardedTransport(time.Second)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		t.Fatal(err)
	}
	connected := false
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotConn: func(httptrace.GotConnInfo) { connected = true },
	}))
	resp, err := client.Do(req)
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		t.Fatal("metadata request succeeded")
	}
	if connected {
		t.Fatal("metadata request was connected")
	}
	if err == nil || !strings.Contains(err.Error(), "blocked connection") {
		t.Fatalf("metadata round trip = %v", err)
	}
	if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no route to host") {
		t.Fatalf("metadata dialed before the guard: %v", err)
	}
}

func TestGuardedTransportRoundTripAllowsEnvProxyAndBlocksDirectPrivate(t *testing.T) {
	setBlockPrivateNetworks(t, true)
	t.Setenv("HTTP_CLIENT_BLOCK_PRIVATE_NETWORKS", "true")

	t.Run("http proxy", func(t *testing.T) {
		proxyHit := make(chan string, 1)
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proxyHit <- r.Host
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(proxy.Close)
		loopback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(loopback.Close)

		clearProxyEnv(t)
		t.Setenv("HTTP_PROXY", proxy.URL)
		t.Setenv("http_proxy", proxy.URL)
		t.Setenv("HTTPS_PROXY", proxy.URL)
		t.Setenv("https_proxy", proxy.URL)
		t.Setenv("NO_PROXY", "10.1.2.3")

		client := &http.Client{Transport: NewGuardedTransport(time.Second), Timeout: 3 * time.Second}
		resp, err := client.Get("http://api.example.com/health")
		if err != nil {
			t.Fatalf("proxied public name: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("proxied status = %d", resp.StatusCode)
		}
		select {
		case host := <-proxyHit:
			if host != "api.example.com" {
				t.Fatalf("proxy host = %q", host)
			}
		default:
			t.Fatal("public name was not sent through the proxy")
		}

		resp, err = client.Get(loopback.URL)
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "blocked connection") {
			t.Fatalf("direct loopback = %v", err)
		}

		resp, err = client.Get("http://10.1.2.3/latest")
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "blocked connection") {
			t.Fatalf("direct private = %v", err)
		}
	})

	t.Run("all_proxy", func(t *testing.T) {
		allowed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(allowed.Close)
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(other.Close)

		clearProxyEnv(t)
		t.Setenv("all_proxy", allowed.URL)

		client := &http.Client{Transport: NewGuardedTransport(time.Second), Timeout: 3 * time.Second}
		resp, err := client.Get(allowed.URL)
		if err != nil {
			t.Fatalf("configured proxy address: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("proxy address status = %d", resp.StatusCode)
		}

		resp, err = client.Get(other.URL)
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "blocked connection") {
			t.Fatalf("other loopback = %v", err)
		}

		resp, err = client.Get("http://10.1.2.3/latest")
		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "blocked connection") {
			t.Fatalf("direct private = %v", err)
		}
	})
}
