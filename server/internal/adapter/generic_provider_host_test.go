package adapter

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGenericProviderRejectsDisallowedBaseHostWithoutRequest(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	provider := mustGenericProvider(t, "https://a.test")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		hits.Add(1)
		return nil, context.Canceled
	})}
	_, err := provider.ListModels(context.Background(), SiteConfig{BaseURL: "http://evil.test/v1", Client: client}, "sk-test")
	if err == nil || !strings.Contains(err.Error(), `base url host "evil.test" is not allowed`) {
		t.Fatalf("error = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits = %d", hits.Load())
	}
	if _, err := provider.ListModels(context.Background(), SiteConfig{BaseURL: "http://", Client: client}, "sk-test"); err == nil || !strings.Contains(err.Error(), "base url host is not allowed") {
		t.Fatalf("empty host error = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("hits after empty host = %d", hits.Load())
	}
}

func TestGenericProviderRedirectStaysInsideAllowedHosts(t *testing.T) {
	t.Parallel()

	var aHits, bHits, cHits atomic.Int32
	var leaked atomic.Bool
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aHits.Add(1)
		w.Header().Set("Location", "http://b.test/v1/models")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(a.Close)
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		if r.Header.Get("X-Api-Key") != "" {
			leaked.Store(true)
		}
		http.Error(w, "leaked", http.StatusOK)
	}))
	t.Cleanup(b.Close)
	c := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cHits.Add(1)
		if r.Header.Get("X-Api-Key") != "sk-test" {
			http.Error(w, "missing key", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"followed","object":"model"}]}`)
	}))
	t.Cleanup(c.Close)

	provider := mustHeaderGenericProvider(t, []string{"a.test"})
	base := genericDialClient(map[string]string{
		"a.test": a.Listener.Addr().String(),
		"b.test": b.Listener.Addr().String(),
		"c.test": c.Listener.Addr().String(),
	})
	if base.CheckRedirect != nil {
		t.Fatal("dial client already has CheckRedirect")
	}
	_, err := provider.ListModels(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: base}, "sk-test")
	if err == nil || !strings.Contains(err.Error(), `redirect host "b.test" is not allowed`) {
		t.Fatalf("off-list redirect error = %v", err)
	}
	if bHits.Load() != 0 || leaked.Load() {
		t.Fatalf("b hits = %d leaked = %v", bHits.Load(), leaked.Load())
	}
	if base.CheckRedirect != nil {
		t.Fatal("allowed-host client mutated the base CheckRedirect")
	}
	if aHits.Load() != 1 {
		t.Fatalf("a hits = %d", aHits.Load())
	}

	followed := mustHeaderGenericProvider(t, []string{"a.test", "c.test"})
	a.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aHits.Add(1)
		w.Header().Set("Location", "http://c.test/v1/models")
		w.WriteHeader(http.StatusFound)
	})
	models, err := followed.ListModels(context.Background(), SiteConfig{BaseURL: "http://a.test", Client: base}, "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].UpstreamName != "followed" {
		t.Fatalf("models = %#v", models)
	}
	if cHits.Load() != 1 {
		t.Fatalf("c hits = %d", cHits.Load())
	}
	if bHits.Load() != 0 {
		t.Fatalf("b hits after allowed redirect = %d", bHits.Load())
	}
}

func TestGenericProviderRejectsSixthRedirect(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 6 {
			w.Header().Set("Location", "http://a.test/hop")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"late","object":"model"}]}`)
	}))
	t.Cleanup(server.Close)
	client := genericDialClient(map[string]string{"a.test": server.Listener.Addr().String()})
	provider := mustGenericProvider(t, "http://a.test")
	_, err := provider.ListModels(context.Background(), SiteConfig{BaseURL: "http://a.test/start", Client: client}, "sk-test")
	if err == nil || !strings.Contains(err.Error(), "stopped after 5 redirects") {
		t.Fatalf("error = %v", err)
	}
	if hits.Load() != 6 {
		t.Fatalf("hits = %d, want 6", hits.Load())
	}

	hits.Store(0)
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 5 {
			w.Header().Set("Location", "http://a.test/hop")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"fifth","object":"model"}]}`)
	})
	models, err := provider.ListModels(context.Background(), SiteConfig{BaseURL: "http://a.test/start", Client: client}, "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].UpstreamName != "fifth" {
		t.Fatalf("models = %#v", models)
	}
	if hits.Load() != 6 {
		t.Fatalf("followed hits = %d, want 6", hits.Load())
	}
}

func TestGenericProviderRejectsHTTPSDowngrade(t *testing.T) {
	t.Parallel()

	var bHits atomic.Int32
	a := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://b.test/v1/models")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(a.Close)
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		http.Error(w, "downgraded", http.StatusOK)
	}))
	t.Cleanup(b.Close)

	client := genericDialClient(map[string]string{
		"a.test": a.Listener.Addr().String(),
		"b.test": b.Listener.Addr().String(),
	})
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	provider := mustGenericProvider(t, "https://a.test")
	_, err := provider.ListModels(context.Background(), SiteConfig{BaseURL: "https://a.test", Client: client}, "sk-test")
	if err == nil || !strings.Contains(err.Error(), "redirect from https to http is not allowed") {
		t.Fatalf("error = %v", err)
	}
	if bHits.Load() != 0 {
		t.Fatalf("b hits = %d", bHits.Load())
	}
}

func mustHeaderGenericProvider(t *testing.T, hosts []string) GenericProvider {
	t.Helper()
	module, err := NewGenericProvider(GenericProviderConfig{
		SiteType:        "acme.example/chat",
		OfficialBaseURL: "https://a.test",
		Auth:            "header:X-Api-Key",
		EndpointTypes:   []string{"openai"},
		AllowedHosts:    hosts,
	})
	if err != nil {
		t.Fatal(err)
	}
	return module.(GenericProvider)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
