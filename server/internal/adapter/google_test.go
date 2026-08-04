package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGoogleListModelsUsesGeminiEndpointCapabilitiesAndHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "gemini-key" {
			t.Fatalf("x-goog-api-key = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != googleUserAgent {
			t.Fatalf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{
				{
					"name":                       "models/gemini-2.5-pro",
					"displayName":                "Gemini 2.5 Pro",
					"description":                "reasoning model",
					"inputTokenLimit":            1048576,
					"outputTokenLimit":           65536,
					"supportedGenerationMethods": []string{"generateContent"},
					"version":                    "2.5",
				},
				{
					"name":        "gemini-embedding-001",
					"displayName": "",
				},
				{
					"name": "   ",
				},
			},
		})
	}))
	defer server.Close()

	models, err := NewGoogle().ListModels(context.Background(), SiteConfig{BaseURL: server.URL + "/"}, " gemini-key ")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models length = %d, want 2", len(models))
	}
	first := models[0]
	if first.UpstreamName != "gemini-2.5-pro" || first.DisplayName != "Gemini 2.5 Pro" {
		t.Fatalf("unexpected first model: %#v", first)
	}
	endpoints, _ := first.Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 1 || endpoints[0] != "google-gemini" {
		t.Fatalf("supported_endpoint_types = %#v", endpoints)
	}
	if first.Capabilities["input_token_limit"] != 1048576 || first.Capabilities["output_token_limit"] != 65536 {
		t.Fatalf("unexpected token limits: %#v", first.Capabilities)
	}
	if first.Capabilities["version"] != "2.5" || first.Capabilities["description"] != "reasoning model" {
		t.Fatalf("unexpected metadata capabilities: %#v", first.Capabilities)
	}
	if models[1].UpstreamName != "gemini-embedding-001" || models[1].DisplayName != "gemini-embedding-001" {
		t.Fatalf("unexpected fallback display name model: %#v", models[1])
	}
}

func TestGoogleSupportedEndpointTypesIncludesImageRoutingCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		modelID string
		want    []string
	}{
		{modelID: "gemini-2.5-pro", want: []string{"google-gemini"}},
		{modelID: "gemini-image-3", want: []string{"google-gemini", "openai-image"}},
		{modelID: "nano_banana_2", want: []string{"google-gemini", "openai-image"}},
		{modelID: "nano-banana-pro", want: []string{"google-gemini", "openai-image"}},
	}
	for _, tt := range tests {
		if got := googleSupportedEndpointTypes(tt.modelID); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("googleSupportedEndpointTypes(%q) = %#v, want %#v", tt.modelID, got, tt.want)
		}
	}
}

func TestGoogleListModelsReportsStatusAndDecodeErrors(t *testing.T) {
	t.Parallel()

	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer statusServer.Close()
	if _, err := NewGoogle().ListModels(context.Background(), SiteConfig{BaseURL: statusServer.URL}, "key"); err == nil {
		t.Fatal("expected status error")
	}

	decodeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer decodeServer.Close()
	if _, err := NewGoogle().ListModels(context.Background(), SiteConfig{BaseURL: decodeServer.URL}, "key"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestGoogleValidateCredentialsUsesListModels(t *testing.T) {
	t.Parallel()

	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]any{{"name": "models/gemini-2.5-flash"}},
		})
	}))
	defer okServer.Close()

	if err := NewGoogle().ValidateCredentials(context.Background(), SiteConfig{BaseURL: okServer.URL}, "key"); err != nil {
		t.Fatalf("ValidateCredentials success returned error: %v", err)
	}

	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer failServer.Close()

	if err := NewGoogle().ValidateCredentials(context.Background(), SiteConfig{BaseURL: failServer.URL}, "key"); err == nil {
		t.Fatal("expected ValidateCredentials to propagate ListModels error")
	}
}

func TestGoogleHelpersExposeSiteTypeAndDefaults(t *testing.T) {
	t.Parallel()

	module := NewGoogle()
	if got := module.SiteTypes(); len(got) != 1 || got[0] != googleSiteType {
		t.Fatalf("SiteTypes = %#v", got)
	}
	if module.DefaultBaseURL() != googleDefaultBaseURL {
		t.Fatalf("DefaultBaseURL = %q", module.DefaultBaseURL())
	}
	caps := module.Capabilities()
	if len(caps) != 2 || caps[0] != CapabilityValidateCredential || caps[1] != CapabilityListModels {
		t.Fatalf("Capabilities = %#v", caps)
	}
	if got := googleBaseURL(" "); got != googleDefaultBaseURL {
		t.Fatalf("googleBaseURL empty = %q", got)
	}
	if got := googleBaseURL(" https://example.com/ "); got != "https://example.com" {
		t.Fatalf("googleBaseURL trim = %q", got)
	}
	if got := googleModelID("models/gemini-2.5-flash"); got != "gemini-2.5-flash" {
		t.Fatalf("googleModelID = %q", got)
	}
	if got := googleModelID("gemini-2.5-flash"); got != "gemini-2.5-flash" {
		t.Fatalf("googleModelID raw = %q", got)
	}
	if got := googleModelID(" "); got != "" {
		t.Fatalf("googleModelID empty = %q", got)
	}
}
