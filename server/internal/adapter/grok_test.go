package adapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func grokTestToken(t *testing.T, tier string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"at+jwt"}`))
	payloadBytes, _ := json.Marshal(map[string]any{"tier": tier, "email": "user@example.com"})
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	return header + "." + payload + ".sig"
}

type grokTestRoundTripper struct {
	target *url.URL
}

func (t grokTestRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	requestURL := *req.URL
	requestURL.Scheme = t.target.Scheme
	requestURL.Host = t.target.Host
	clone.URL = &requestURL
	clone.Host = ""
	return http.DefaultTransport.RoundTrip(clone)
}

func grokTestSite(t *testing.T, serverURL string) SiteConfig {
	t.Helper()
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	return SiteConfig{
		BaseURL: "https://untrusted.example",
		Client:  &http.Client{Transport: grokTestRoundTripper{target: target}},
	}
}

func TestGrokListModelsParsesOpenAIResponse(t *testing.T) {
	token := grokTestToken(t, "SuperGrok")
	var gotAuth, gotTokenAuth, gotSurface string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotTokenAuth = r.Header.Get("X-XAI-Token-Auth")
		gotSurface = r.Header.Get("x-grok-client-surface")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"grok-4.5","display_name":"Grok 4.5"},{"id":"grok-composer-2.5-fast"}]}`))
	}))
	defer server.Close()

	models, err := NewGrok().ListModels(context.Background(), grokTestSite(t, server.URL), token)
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("ListModels() returned %d models, want 2 catalog models (hidden models are tier-gated in SummarizeAPIKey)", len(models))
	}
	endpoints, _ := models[0].Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 1 || endpoints[0] != "openai-response" {
		t.Fatalf("unexpected Grok text endpoint types: %#v", models[0].Capabilities["supported_endpoint_types"])
	}
	if models[0].UpstreamName != "grok-4.5" || models[0].DisplayName != "Grok 4.5" {
		t.Fatalf("unexpected first model: %+v", models[0])
	}
	if models[1].DisplayName != "grok-composer-2.5-fast" {
		t.Fatalf("expected display name to fall back to id, got %q", models[1].DisplayName)
	}
	for _, model := range models {
		if model.UpstreamName == "grok-imagine-image-quality" {
			t.Fatal("ListModels must not inject the hidden imagine model")
		}
	}
	if gotAuth != "Bearer "+token {
		t.Fatalf("Authorization header = %q", gotAuth)
	}
	if gotTokenAuth != GrokTokenAuthHeader {
		t.Fatalf("X-XAI-Token-Auth = %q, want %q", gotTokenAuth, GrokTokenAuthHeader)
	}
	if gotSurface != GrokClientSurface {
		t.Fatalf("x-grok-client-surface = %q, want %q", gotSurface, GrokClientSurface)
	}
}

func TestGrokProbeHealthUsesModelsEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("models path = %q, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Fatal("health probe must send authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"grok-4.5"}]}`))
	}))
	t.Cleanup(server.Close)

	models, err := NewGrok().ProbeHealth(context.Background(), grokTestSite(t, server.URL), "access-token")
	if err != nil {
		t.Fatalf("ProbeHealth() error = %v", err)
	}
	if len(models) != 1 || models[0].UpstreamName != "grok-4.5" {
		t.Fatalf("ProbeHealth() models = %#v, want one grok model", models)
	}
}

func TestApplyGrokUpstreamHeadersUsesCLIIdentityOnlyForOfficialProxy(t *testing.T) {
	official, err := http.NewRequest(http.MethodGet, GrokChatBaseURL+"/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	ApplyGrokUpstreamHeaders(official, "token")
	if official.Header.Get("Authorization") != "Bearer token" || official.Header.Get("X-XAI-Token-Auth") != GrokTokenAuthHeader {
		t.Fatalf("official headers = %#v", official.Header)
	}

	custom, err := http.NewRequest(http.MethodGet, "https://relay.example/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	ApplyGrokUpstreamHeaders(custom, "token")
	if custom.Header.Get("Authorization") != "Bearer token" || custom.Header.Get("X-XAI-Token-Auth") != "" {
		t.Fatalf("custom headers = %#v", custom.Header)
	}
}

func TestGrokSupportedEndpointTypesUsesUpstreamDeclarations(t *testing.T) {
	endpoints := grokSupportedEndpointTypes(map[string]any{
		"supported_endpoint_types": []any{"chat_completions", "responses"},
	}, "grok-4.5")
	if len(endpoints) != 2 || endpoints[0] != "openai" || endpoints[1] != "openai-response" {
		t.Fatalf("declared endpoints = %#v", endpoints)
	}

	backend := grokSupportedEndpointTypes(map[string]any{"api_backend": "responses"}, "grok-build")
	if len(backend) != 1 || backend[0] != "openai-response" {
		t.Fatalf("backend endpoints = %#v", backend)
	}
}

func TestGrokSummaryDerivesTierFromToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/user") {
			_, _ = w.Write([]byte(`{"entitlements":{"image_generation":{"enabled":true}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"grok-4.5"}]}`))
	}))
	defer server.Close()

	summary, err := NewGrok().SummarizeAPIKey(context.Background(), grokTestSite(t, server.URL), grokTestToken(t, "SuperGrok"))
	if err != nil {
		t.Fatalf("SummarizeAPIKey() error = %v", err)
	}
	usage, _ := summary.Usage.(map[string]any)
	if usage["tier"] != "premium" {
		t.Fatalf("tier = %v, want premium", usage["tier"])
	}
	if len(summary.Models) != 2 {
		t.Fatalf("expected catalog + explicitly entitled model, got %d", len(summary.Models))
	}
	if usage["quota_status"] != "unknown" || usage["unlimited"] != false {
		t.Fatalf("usage = %#v, want unknown non-unlimited quota", usage)
	}
}

func TestGrokSummaryOmitsImageModelForFreeTier(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"grok-4.5"}]}`))
	}))
	defer server.Close()

	summary, err := NewGrok().SummarizeAPIKey(context.Background(), grokTestSite(t, server.URL), grokTestToken(t, "free"))
	if err != nil {
		t.Fatalf("SummarizeAPIKey() error = %v", err)
	}
	if usage, _ := summary.Usage.(map[string]any); usage["tier"] != "free" {
		t.Fatalf("tier = %v, want free", usage["tier"])
	}
	for _, model := range summary.Models {
		if model.UpstreamName == "grok-imagine-image-quality" {
			t.Fatal("free tier must not get the hidden imagine model")
		}
	}
	if len(summary.Models) != 1 {
		t.Fatalf("free tier models = %d, want 1 (catalog only)", len(summary.Models))
	}
}

func TestIsGrokImageModel(t *testing.T) {
	if !IsGrokImageModel("grok-imagine-image-quality") {
		t.Fatal("grok-imagine-image-quality should be recognized as an image model")
	}
	for _, name := range []string{"grok-4.5", "grok-composer-2.5-fast", "", "grok-image-unknown"} {
		if IsGrokImageModel(name) {
			t.Fatalf("%q should not be recognized as a hidden image model", name)
		}
	}
}

func TestAppendGrokEntitledModelsRequiresExplicitEntitlement(t *testing.T) {
	base := []Model{{UpstreamName: "grok-4.5"}}
	if got := appendGrokEntitledModels(base, map[string]any{"subscriptionTier": "SuperGrok Heavy"}); len(got) != 1 {
		t.Fatalf("tier-only profile must not add image: %#v", got)
	}
	got := appendGrokEntitledModels(base, map[string]any{"entitlements": map[string]any{"image_generation": map[string]any{"enabled": true}}})
	if len(got) != 2 || got[1].UpstreamName != "grok-imagine-image-quality" {
		t.Fatalf("explicit entitlement models = %#v", got)
	}
	if got := appendGrokEntitledModels(base, map[string]any{"image": true, "profile_image": true}); len(got) != 1 {
		t.Fatalf("unrelated image fields must not enable image models: %#v", got)
	}
}

func TestGrokModelItemsAcceptsCompatibleCatalogShapes(t *testing.T) {
	items := grokModelItems(map[string]any{"models": []any{
		map[string]any{"modelId": "grok-4.5"},
		"grok-build",
	}})
	if len(items) != 2 || items[0]["modelId"] != "grok-4.5" || items[1]["id"] != "grok-build" {
		t.Fatalf("items = %#v", items)
	}
}

func TestGrokUnauthorizedIsClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer server.Close()

	_, err := NewGrok().ListModels(context.Background(), grokTestSite(t, server.URL), grokTestToken(t, "free"))
	if err == nil {
		t.Fatal("ListModels() error = nil, want unauthorized")
	}
	if !IsGrokUnauthorizedError(err) {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}

func TestGrokForbiddenIsNotClassifiedAsInvalidCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"insufficient_plan"}`))
	}))
	defer server.Close()

	_, err := NewGrok().ListModels(context.Background(), grokTestSite(t, server.URL), grokTestToken(t, "free"))
	if err == nil {
		t.Fatal("ListModels() error = nil, want forbidden")
	}
	if IsGrokUnauthorizedError(err) {
		t.Fatalf("forbidden must not invalidate the credential: %v", err)
	}
}
