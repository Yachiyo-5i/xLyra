package httpclient

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestNodeClientHelloSpecIsAppliedToHandshake(t *testing.T) {
	t.Parallel()

	hellos := make(chan *tls.ClientHelloInfo, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.TLS = &tls.Config{
		GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			select {
			case hellos <- info:
			default:
			}
			return nil, nil
		},
	}
	server.StartTLS()
	defer server.Close()

	client, err := newHTTPClient(Profile{TLSFingerprint: TLSFingerprintClaudeCode, RequestTimeout: 5 * time.Second}, nil)
	if err != nil {
		t.Fatalf("newHTTPClient returned error: %v", err)
	}
	// The test server certificate is self-signed, so the request itself fails;
	// the ClientHello has been captured by then.
	_, _ = client.Get(server.URL)

	select {
	case info := <-hellos:
		if len(info.SupportedProtos) != 1 || info.SupportedProtos[0] != "http/1.1" {
			t.Fatalf("ALPN = %v, want only http/1.1", info.SupportedProtos)
		}
		want := make([]tls.CurveID, 0, len(nodeClientHelloCurves))
		for _, curve := range nodeClientHelloCurves {
			want = append(want, tls.CurveID(curve))
		}
		if !slices.Equal(info.SupportedCurves, want) {
			t.Fatalf("curves = %v, want %v", info.SupportedCurves, want)
		}
		got := info.CipherSuites
		if len(got) != len(nodeClientHelloCipherSuites) || got[0] != 0x1301 || got[len(got)-1] != 0x0035 {
			t.Fatalf("cipher suites = %v, want the Node.js order %v", got, nodeClientHelloCipherSuites)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not observe a ClientHello")
	}
}

func TestCacheKeySeparatesTLSFingerprint(t *testing.T) {
	t.Parallel()

	plain := cacheKey(normalizeProfile(Profile{}), nil)
	fingerprinted := cacheKey(normalizeProfile(Profile{TLSFingerprint: " Claude_Code "}), nil)
	if plain == fingerprinted {
		t.Fatal("fingerprinted clients must not share a cache entry with default clients")
	}
}

func TestFingerprintedHandshakeThroughHTTPConnectProxy(t *testing.T) {
	t.Parallel()

	hellos := make(chan *tls.ClientHelloInfo, 1)
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	target.TLS = &tls.Config{
		GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			select {
			case hellos <- info:
			default:
			}
			return nil, nil
		},
	}
	target.StartTLS()
	defer target.Close()

	var mu sync.Mutex
	var connectHost, proxyAuth string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusMethodNotAllowed)
			return
		}
		mu.Lock()
		connectHost, proxyAuth = r.Host, r.Header.Get("Proxy-Authorization")
		mu.Unlock()
		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("proxy response writer cannot hijack")
			return
		}
		client, _, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("Hijack returned error: %v", err)
			return
		}
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
		go func() { _, _ = io.Copy(client, upstream); _ = client.Close() }()
	}))
	defer proxy.Close()

	client, err := newHTTPClient(
		Profile{TLSFingerprint: TLSFingerprintClaudeCode, RequestTimeout: 5 * time.Second},
		&ProxyProfile{ID: "p", Type: "http", URL: "http://user:pass@" + proxy.Listener.Addr().String()},
	)
	if err != nil {
		t.Fatalf("newHTTPClient returned error: %v", err)
	}
	_, _ = client.Get(target.URL)

	select {
	case info := <-hellos:
		if len(info.SupportedProtos) != 1 || info.SupportedProtos[0] != "http/1.1" {
			t.Fatalf("ALPN through proxy = %v, want only http/1.1", info.SupportedProtos)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("target did not observe a ClientHello through the proxy")
	}
	mu.Lock()
	defer mu.Unlock()
	if connectHost != target.Listener.Addr().String() {
		t.Fatalf("CONNECT host = %q, want %q", connectHost, target.Listener.Addr().String())
	}
	if proxyAuth != "Basic dXNlcjpwYXNz" {
		t.Fatalf("Proxy-Authorization = %q", proxyAuth)
	}
}

func TestHTTPConnectDialerReportsProxyRejection(t *testing.T) {
	t.Parallel()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer proxy.Close()

	client, err := newHTTPClient(
		Profile{TLSFingerprint: TLSFingerprintClaudeCode, RequestTimeout: 5 * time.Second},
		&ProxyProfile{ID: "p", Type: "http", URL: proxy.URL},
	)
	if err != nil {
		t.Fatalf("newHTTPClient returned error: %v", err)
	}
	if _, err := client.Get("https://example.invalid/"); err == nil {
		t.Fatal("request through a rejecting proxy succeeded, want an error")
	}
}
