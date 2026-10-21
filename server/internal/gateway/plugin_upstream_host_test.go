package gateway

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/credential"
	"xlyra/server/internal/httpclient"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

func TestPluginUpstreamHostNotAllowedDoesNotSend(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	paths := []string{
		gatewayEndpointChatCompletions,
		gatewayEndpointEmbeddings,
		gatewayEndpointImagesGenerations,
		gatewayEndpointImagesEdits,
		gatewayEndpointAudioSpeech,
		gatewayEndpointMessages,
		gatewayEndpointResponses,
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			hits := &atomic.Int32{}
			handler, candidate := pluginUpstreamFixture(t, "acme.example/chat", "http://127.0.0.1:1", []string{"a.test"}, true, pluginCountingClient(hits), 2)
			result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-host", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: path}, nil, &testGatewayProtocol{})
			if result.statusCode != http.StatusBadGateway || result.errorType != "upstream_host_not_allowed" {
				t.Fatalf("status=%d type=%q message=%q", result.statusCode, result.errorType, result.errorMessage)
			}
			if !strings.Contains(result.errorMessage, "127.0.0.1") {
				t.Fatalf("message = %q", result.errorMessage)
			}
			if result.credentialAttempt != 1 || result.credentialTotal != 2 || result.credentialDecision.ShouldTryNextCredential || result.credentialDecision.Action != "stop" {
				t.Fatalf("credential decision = attempt %d total %d %#v", result.credentialAttempt, result.credentialTotal, result.credentialDecision)
			}
			if hits.Load() != 0 {
				t.Fatalf("hits = %d", hits.Load())
			}
		})
	}

	t.Run("stream", func(t *testing.T) {
		handler, candidate := pluginUpstreamFixture(t, "acme.example/chat", "http://127.0.0.1:1", []string{"a.test"}, true, nil, 2)
		result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-host-stream", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Stream: true}, nil, &testGatewayProtocol{})
		if result.statusCode != http.StatusBadGateway || result.errorType != "upstream_host_not_allowed" || result.credentialDecision.ShouldTryNextCredential {
			t.Fatalf("stream deny = status %d type %q decision %#v message %q", result.statusCode, result.errorType, result.credentialDecision, result.errorMessage)
		}
		if result.credentialAttempt != 1 || result.credentialTotal != 2 {
			t.Fatalf("stream credential attempt = %d/%d", result.credentialAttempt, result.credentialTotal)
		}
	})
}

func TestPluginUpstreamAllowedHost(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)

	handler, candidate := pluginUpstreamFixture(t, "acme.example/chat", server.URL, []string{"127.0.0.1"}, true, &http.Client{Timeout: 5 * time.Second}, 1)
	result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-allow", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, nil, &testGatewayProtocol{})
	if !result.success || result.statusCode != http.StatusOK || result.errorType != "" {
		t.Fatalf("allow = success %v status %d type %q message %q", result.success, result.statusCode, result.errorType, result.errorMessage)
	}

	streamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: ok\n\n")
	}))
	t.Cleanup(streamServer.Close)
	handler, candidate = pluginUpstreamFixture(t, "acme.example/chat", streamServer.URL, []string{"127.0.0.1"}, true, nil, 1)
	result = handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-allow-stream", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Stream: true}, nil, &testGatewayProtocol{streamStarted: true, streamCapture: streamCaptureState{streamCompleted: true, endReason: "done"}})
	if !result.success || result.errorType != "" {
		t.Fatalf("stream allow = success %v type %q message %q", result.success, result.errorType, result.errorMessage)
	}
}

func TestPluginUpstreamRedirectOffAllowlist(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	var bHits atomic.Int32
	var leaked atomic.Bool
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://b.test/secret")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(a.Close)
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		if r.Header.Get("X-Plugin-Auth") != "" {
			leaked.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(b.Close)
	client := pluginHostDialClient(map[string]string{
		"a.test": a.Listener.Addr().String(),
		"b.test": b.Listener.Addr().String(),
	})
	handler, candidate := pluginUpstreamFixture(t, "acme.example/chat", "http://a.test", []string{"a.test"}, true, client, 1)
	result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-redirect", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, nil, &headerGatewayProtocol{testGatewayProtocol: &testGatewayProtocol{}})
	if result.success || result.statusCode != http.StatusBadGateway || !strings.Contains(result.errorMessage, `redirect host "b.test" is not allowed`) {
		t.Fatalf("redirect = success %v status %d type %q message %q", result.success, result.statusCode, result.errorType, result.errorMessage)
	}
	if bHits.Load() != 0 || leaked.Load() {
		t.Fatalf("b hits = %d leaked = %v", bHits.Load(), leaked.Load())
	}
	if result.credentialDecision.ShouldTryNextCredential {
		t.Fatalf("redirect switched credentials: %#v", result.credentialDecision)
	}
}

func TestBuiltinUpstreamIgnoresPluginHostPolicy(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	var bHits atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://b.test/ok")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(a.Close)
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(b.Close)
	client := pluginHostDialClient(map[string]string{
		"a.test": a.Listener.Addr().String(),
		"b.test": b.Listener.Addr().String(),
	})
	handler, candidate := pluginUpstreamFixture(t, "openai", "http://a.test", []string{"a.test"}, true, client, 1)
	result := handler.forwardGatewayRequest(context.Background(), httptest.NewRecorder(), "req-builtin", 1, uuid.New(), uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, nil, &testGatewayProtocol{})
	if !result.success || bHits.Load() != 1 {
		t.Fatalf("builtin redirect success=%v hits=%d type=%q message=%q", result.success, bHits.Load(), result.errorType, result.errorMessage)
	}
}

func TestSiteModelTestPluginHostPolicy(t *testing.T) {
	site.ClearPluginSiteTypes()
	t.Cleanup(site.ClearPluginSiteTypes)

	hits := &atomic.Int32{}
	handler, candidate, credential := pluginUpstreamModelTestFixture(t, "acme.example/chat", "http://127.0.0.1:1", []string{"a.test"}, pluginCountingClient(hits))
	result := handler.forwardSiteModelTestRequest(context.Background(), "site-model-test/deny", uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, &testGatewayProtocol{}, credential)
	if result.statusCode != http.StatusBadGateway || result.errorType != "upstream_host_not_allowed" || !strings.Contains(result.errorMessage, "127.0.0.1") {
		t.Fatalf("diagnostic deny = status %d type %q message %q", result.statusCode, result.errorType, result.errorMessage)
	}
	if hits.Load() != 0 {
		t.Fatalf("diagnostic hits = %d", hits.Load())
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(server.Close)
	handler, candidate, credential = pluginUpstreamModelTestFixture(t, "acme.example/chat", server.URL, []string{"127.0.0.1"}, &http.Client{Timeout: 5 * time.Second})
	result = handler.forwardSiteModelTestRequest(context.Background(), "site-model-test/allow", uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, &testGatewayProtocol{}, credential)
	if !result.success || result.errorType != "" {
		t.Fatalf("diagnostic allow = success %v type %q message %q", result.success, result.errorType, result.errorMessage)
	}

	var bHits atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://b.test/secret")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(a.Close)
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(b.Close)
	handler, candidate, credential = pluginUpstreamModelTestFixture(t, "acme.example/chat", "http://a.test", []string{"a.test"}, pluginHostDialClient(map[string]string{
		"a.test": a.Listener.Addr().String(),
		"b.test": b.Listener.Addr().String(),
	}))
	result = handler.forwardSiteModelTestRequest(context.Background(), "site-model-test/redirect", uuid.New(), candidate, gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, &testGatewayProtocol{}, credential)
	if result.success || !strings.Contains(result.errorMessage, `redirect host "b.test" is not allowed`) || bHits.Load() != 0 {
		t.Fatalf("diagnostic redirect success=%v hits=%d message=%q", result.success, bHits.Load(), result.errorMessage)
	}
}

func pluginUpstreamFixture(t *testing.T, siteType, baseURL string, hosts []string, enabled bool, client *http.Client, credentialCount int) (Handler, routeengine.Candidate) {
	t.Helper()
	handler, candidate, _ := pluginUpstreamModelTestFixtureWithHosts(t, siteType, baseURL, hosts, enabled, client, credentialCount)
	return handler, candidate
}

func pluginUpstreamModelTestFixture(t *testing.T, siteType, baseURL string, hosts []string, client *http.Client) (Handler, routeengine.Candidate, *store.GatewayCredential) {
	t.Helper()
	return pluginUpstreamModelTestFixtureWithHosts(t, siteType, baseURL, hosts, true, client, 1)
}

func pluginUpstreamModelTestFixtureWithHosts(t *testing.T, siteType, baseURL string, hosts []string, enabled bool, client *http.Client, credentialCount int) (Handler, routeengine.Candidate, *store.GatewayCredential) {
	t.Helper()
	return pluginUpstreamFixtureWithSecret(t, siteType, baseURL, hosts, enabled, client, credentialCount, "sk-test")
}

func pluginUpstreamFixtureWithSecret(t *testing.T, siteType, baseURL string, hosts []string, enabled bool, client *http.Client, credentialCount int, secret string) (Handler, routeengine.Candidate, *store.GatewayCredential) {
	t.Helper()
	if credentialCount < 1 {
		credentialCount = 1
	}
	if err := site.SetPluginSiteTypes([]site.PluginSiteType{{
		Descriptor: site.SiteTypeDescriptor{SiteType: "acme.example/chat", DisplayName: "Acme"},
		PluginID:   "acme.example",
		Enabled:    enabled,
		Hosts:      append([]string(nil), hosts...),
	}}); err != nil {
		t.Fatal(err)
	}

	siteID := uuid.New()
	siteModelID := uuid.New()
	secrets := credential.NewService("test-master")
	encrypted, masked, err := secrets.Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	credentials := make([]store.SiteCredential, 0, credentialCount)
	states := make([]store.SiteAPIKeyState, 0, credentialCount)
	bindings := make([]store.SiteAPIKeyModel, 0, credentialCount)
	for i := 0; i < credentialCount; i++ {
		id := uuid.New()
		credentials = append(credentials, store.SiteCredential{
			ID:              id,
			SiteID:          siteID,
			CredentialType:  "api_key",
			EncryptedSecret: encrypted,
			MaskedSecret:    masked,
			Meta:            store.JSON(`{"enabled":true}`),
			CreatedAt:       time.Unix(int64(10+i), 0),
		})
		states = append(states, store.SiteAPIKeyState{SiteCredentialID: id, SiteID: siteID, Enabled: true, SyncStatus: "synced"})
		bindings = append(bindings, store.SiteAPIKeyModel{
			SiteID:            siteID,
			SiteCredentialID:  id,
			SiteModelID:       uuid.NullUUID{UUID: siteModelID, Valid: true},
			UpstreamModelName: "test-upstream",
			Available:         true,
			Enabled:           true,
			UpdatedAt:         time.Unix(20, 0),
		})
	}
	db := gatewayGormWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.Site:
			*dest = store.Site{ID: siteID, SiteType: siteType, BaseURL: baseURL}
			tx.Statement.RowsAffected = 1
		case *[]store.SiteAPIKeyModel:
			*dest = bindings
			tx.Statement.RowsAffected = int64(len(bindings))
		case *[]store.SiteCredential:
			*dest = credentials
			tx.Statement.RowsAffected = int64(len(credentials))
		case *[]store.SiteAPIKeyState:
			*dest = states
			tx.Statement.RowsAffected = int64(len(states))
		case *[]store.RouteCooldown, *[]store.SiteModelPricing:
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(gorm.ErrRecordNotFound)
		}
	})
	handler := Handler{
		logger:      gatewayDiscardLogger(),
		db:          gatewayStoreWithGorm(t, db),
		credentials: secrets,
		recorder:    Recorder{},
		httpClient:  client,
		clients:     httpclient.NewManager(nil),
	}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: siteID, Name: "Acme", SiteType: siteType, BaseURL: baseURL},
		Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "test-upstream"},
	}
	selected := &store.GatewayCredential{Credential: credentials[0], State: states[0]}
	return handler, candidate, selected
}

func pluginCountingClient(hits *atomic.Int32) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		hits.Add(1)
		return nil, context.Canceled
	}), Timeout: time.Second}
}

func pluginHostDialClient(routes map[string]string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 2 * time.Second}
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

type headerGatewayProtocol struct {
	*testGatewayProtocol
}

func (p *headerGatewayProtocol) ApplyUpstreamHeaders(req *http.Request, _, _ string, _ bool) {
	req.Header.Set("X-Plugin-Auth", "secret-value")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
