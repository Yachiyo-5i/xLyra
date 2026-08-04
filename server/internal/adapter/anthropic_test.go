package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAnthropicListModelsUsesMessagesEndpointCapabilities(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "sk-ant-test" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicAPIVersion {
			t.Fatalf("anthropic-version = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":           "claude-sonnet-4-20250514",
					"type":         "model",
					"display_name": "Claude Sonnet 4",
					"created_at":   "2025-05-14T00:00:00Z",
				},
			},
		})
	}))
	defer server.Close()

	models, err := NewAnthropic().ListModels(context.Background(), SiteConfig{BaseURL: server.URL + "/"}, " sk-ant-test ")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].UpstreamName != "claude-sonnet-4-20250514" || models[0].DisplayName != "Claude Sonnet 4" {
		t.Fatalf("unexpected model: %#v", models[0])
	}
	endpoints, _ := models[0].Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 1 || endpoints[0] != "anthropic-messages" {
		t.Fatalf("unexpected supported_endpoint_types: %#v", models[0].Capabilities["supported_endpoint_types"])
	}
	if models[0].Capabilities["tool_call"] != true {
		t.Fatalf("expected tool_call capability: %#v", models[0].Capabilities)
	}
}

func TestAnthropicListModelsReportsStatusAndDecodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    string
	}{
		{
			name:       "non-2xx",
			statusCode: http.StatusUnauthorized,
			body:       "invalid key",
			wantErr:    "anthropic models returned 401: invalid key",
		},
		{
			name:       "bad json",
			statusCode: http.StatusOK,
			body:       `{`,
			wantErr:    "decode anthropic models",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/models" {
					t.Fatalf("unexpected path: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			_, err := NewAnthropic().ListModels(context.Background(), SiteConfig{BaseURL: server.URL}, "sk-ant-test")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestAnthropicValidateCredentialsUsesDefaultBaseURL(t *testing.T) {
	t.Parallel()

	var requestedHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		requestedHost = r.Header.Get("X-Original-Host")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "claude-3-5-haiku-latest", "type": "model"},
			},
		})
	}))
	defer server.Close()

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	site := SiteConfig{
		BaseURL: " \t\n ",
		Client: &http.Client{
			Transport: anthropicTestRewriteTransport{target: target},
		},
	}

	if err := NewAnthropic().ValidateCredentials(context.Background(), site, "sk-ant-test"); err != nil {
		t.Fatalf("ValidateCredentials returned error: %v", err)
	}
	if requestedHost != "api.anthropic.com" {
		t.Fatalf("original host = %q, want api.anthropic.com", requestedHost)
	}
}

func TestAnthropicHelpersExposeSiteTypeAndDefaults(t *testing.T) {
	t.Parallel()

	module := NewAnthropic()
	if got := module.SiteTypes(); len(got) != 1 || got[0] != anthropicSiteType {
		t.Fatalf("SiteTypes = %#v", got)
	}
	if module.DefaultBaseURL() != anthropicDefaultBaseURL {
		t.Fatalf("DefaultBaseURL = %q", module.DefaultBaseURL())
	}
	caps := module.Capabilities()
	if len(caps) != 2 || caps[0] != CapabilityValidateCredential || caps[1] != CapabilityListModels {
		t.Fatalf("Capabilities = %#v", caps)
	}
	if got := anthropicBaseURL(" "); got != anthropicDefaultBaseURL {
		t.Fatalf("anthropicBaseURL empty = %q", got)
	}
	if got := anthropicBaseURL(" https://example.com/ "); got != "https://example.com" {
		t.Fatalf("anthropicBaseURL trim = %q", got)
	}
}

type anthropicTestRewriteTransport struct {
	target *url.URL
}

func (t anthropicTestRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL = new(url.URL)
	*cloned.URL = *req.URL
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	cloned.Host = t.target.Host
	cloned.Header.Set("X-Original-Host", req.URL.Host)

	return http.DefaultTransport.RoundTrip(cloned)
}

func TestAnthropicListModelsFollowsPagination(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "1000" {
			t.Fatalf("limit = %q, want 1000", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("after_id") {
		case "":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":     []map[string]any{{"id": "model-a", "type": "model"}},
				"has_more": true,
				"last_id":  "model-a",
			})
		case "model-a":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":     []map[string]any{{"id": "model-b", "type": "model"}},
				"has_more": false,
				"last_id":  "model-b",
			})
		default:
			t.Fatalf("unexpected after_id: %q", r.URL.Query().Get("after_id"))
		}
	}))
	defer server.Close()

	models, err := NewAnthropic().ListModels(context.Background(), SiteConfig{BaseURL: server.URL}, "sk-ant-test")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 2 || models[0].UpstreamName != "model-a" || models[1].UpstreamName != "model-b" {
		t.Fatalf("expected both pages, got %#v", models)
	}
}
