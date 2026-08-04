package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAntigravityFetchPricingCombinesQuotaAndModelsDevCatalog(t *testing.T) {
	t.Parallel()

	var quotaProject string
	var modelsDevRequested bool
	site := SiteConfig{
		BaseURL: "https://antigravity.example.test",
		Client: &http.Client{Transport: adapterRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case "https://antigravity.example.test/v1internal:fetchAvailableModels":
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Fatalf("decode quota request body: %v", err)
				}
				quotaProject, _ = body["project"].(string)
				return adapterTestResponse(http.StatusOK, `{
						"models": {
							"gemini-2.5-pro": {
								"displayName": "Gemini 2.5 Pro",
								"quotaInfo": {"remainingFraction": 0.8}
							},
							"internal-preview": {
								"quotaInfo": {"remainingFraction": 1}
							}
						}
					}`), nil
			case antigravityModelsDevSourceURL:
				modelsDevRequested = true
				return adapterTestResponse(http.StatusOK, `{
						"google": {
							"models": {
								"gemini-2.5-pro": {
									"id": "gemini-2.5-pro",
									"name": "Gemini 2.5 Pro",
									"cost": {"input": 1.25, "output": 10, "cache_read": 0.125},
									"modalities": {"input": ["text"], "output": ["text"]},
									"limit": {"context": 1048576}
								}
							}
						}
					}`), nil
			default:
				t.Fatalf("unexpected request URL: %s", req.URL.String())
				return nil, nil
			}
		})},
	}

	snapshot, err := NewAntigravity().FetchPricing(context.Background(), site, SystemAuth{
		AccessToken: "token",
		Metadata: map[string]any{
			"project_id": "project-123",
		},
	})
	if err != nil {
		t.Fatalf("FetchPricing returned error: %v", err)
	}
	if quotaProject != "project-123" {
		t.Fatalf("quota project = %q, want project-123", quotaProject)
	}
	if !modelsDevRequested {
		t.Fatal("expected FetchPricing to request models.dev catalog after quota")
	}
	if len(snapshot.Groups) != 1 || snapshot.Groups[0].GroupName != "default" {
		t.Fatalf("pricing groups = %#v, want default group", snapshot.Groups)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("pricing items = %#v, want one models.dev matched item", snapshot.Items)
	}
	item := snapshot.Items[0]
	if item.ModelName != "gemini-2.5-pro" || item.InputValue != 1.25 || item.OutputValue != 10 {
		t.Fatalf("unexpected pricing item: %#v", item)
	}
	if !item.HasCacheRatio || item.CacheRatio != 0.1 {
		t.Fatalf("cache ratio = %#v, want 0.1", item.CacheRatio)
	}
}

func TestAntigravityModelsDevCatalogHandlesHTTPRequestsAndFailures(t *testing.T) {
	t.Run("success sends expected headers", func(t *testing.T) {
		var gotAccept string
		var gotUserAgent string
		site := SiteConfig{
			Client: &http.Client{Transport: adapterRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotAccept = req.Header.Get("Accept")
				gotUserAgent = req.Header.Get("User-Agent")
				return adapterTestResponse(http.StatusOK, `{
						"google": {
							"models": {
								"gemini-2.5-pro": {
									"id": "gemini-2.5-pro",
									"name": "Gemini 2.5 Pro",
									"cost": {"input": 1.25}
								}
							}
						}
					}`), nil
			})},
		}

		catalog, err := NewAntigravity().fetchModelsDevCatalog(context.Background(), site)
		if err != nil {
			t.Fatalf("fetchModelsDevCatalog returned error: %v", err)
		}
		if gotAccept != "application/json" || gotUserAgent != "xLyra/1.0" {
			t.Fatalf("request headers Accept=%q User-Agent=%q", gotAccept, gotUserAgent)
		}
		model := catalog["google"].Models["gemini-2.5-pro"]
		if model.Name != "Gemini 2.5 Pro" || model.Cost["input"] != float64(1.25) {
			t.Fatalf("decoded model = %#v", model)
		}
	})

	t.Run("transport error is wrapped", func(t *testing.T) {
		wantErr := errors.New("models.dev offline")
		site := SiteConfig{
			Client: &http.Client{Transport: adapterRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, wantErr
			})},
		}

		_, err := NewAntigravity().fetchModelsDevCatalog(context.Background(), site)
		if err == nil || !strings.Contains(err.Error(), "fetch models.dev pricing") || !errors.Is(err, wantErr) {
			t.Fatalf("fetchModelsDevCatalog error = %v, want wrapped transport error", err)
		}
	})

	t.Run("non success status includes code", func(t *testing.T) {
		site := SiteConfig{
			Client: &http.Client{Transport: adapterRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return adapterTestResponse(http.StatusBadGateway, "bad gateway"), nil
			})},
		}

		_, err := NewAntigravity().fetchModelsDevCatalog(context.Background(), site)
		if err == nil || !strings.Contains(err.Error(), "models.dev pricing returned 502") {
			t.Fatalf("fetchModelsDevCatalog error = %v, want status code", err)
		}
	})

	t.Run("invalid json is rejected", func(t *testing.T) {
		site := SiteConfig{
			Client: &http.Client{Transport: adapterRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return adapterTestResponse(http.StatusOK, `{`), nil
			})},
		}

		_, err := NewAntigravity().fetchModelsDevCatalog(context.Background(), site)
		if err == nil || !strings.Contains(err.Error(), "decode models.dev pricing") {
			t.Fatalf("fetchModelsDevCatalog error = %v, want decode error", err)
		}
	})
}

func TestXLyraValidateCredentialsDelegatesToModelsEndpoint(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var gotPath string
		var gotAuth string
		site := SiteConfig{
			BaseURL: "https://xlyra.example.test/base/",
			Client: &http.Client{Transport: adapterRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				gotPath = req.URL.Path
				gotAuth = req.Header.Get("Authorization")
				return adapterTestResponse(http.StatusOK, `{"data":[{"id":"gpt-xlyra"}]}`), nil
			})},
		}

		if err := NewXLyra().ValidateCredentials(context.Background(), site, " sk-test "); err != nil {
			t.Fatalf("ValidateCredentials returned error: %v", err)
		}
		if gotPath != "/base/v1/models" {
			t.Fatalf("models path = %q, want /base/v1/models", gotPath)
		}
		if gotAuth != "Bearer  sk-test " {
			t.Fatalf("authorization header = %q, want api key delegated unchanged", gotAuth)
		}
	})

	t.Run("upstream error is propagated", func(t *testing.T) {
		site := SiteConfig{
			BaseURL: "https://xlyra.example.test",
			Client: &http.Client{Transport: adapterRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return adapterTestResponse(http.StatusUnauthorized, "bad key"), nil
			})},
		}

		err := NewXLyra().ValidateCredentials(context.Background(), site, "sk-bad")
		if err == nil || !strings.Contains(err.Error(), "upstream returned 401: bad key") {
			t.Fatalf("ValidateCredentials error = %v, want upstream status", err)
		}
	})
}

func adapterTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
