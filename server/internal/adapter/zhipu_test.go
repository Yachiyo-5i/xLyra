package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestZhipuAdaptersExposeSeparateSiteTypesAndDefaults(t *testing.T) {
	t.Parallel()

	general := NewZhipu()
	if got := general.SiteTypes(); len(got) != 1 || got[0] != "zhipu" {
		t.Fatalf("zhipu site types = %#v", got)
	}
	if general.DefaultBaseURL() != zhipuDefaultBaseURL {
		t.Fatalf("zhipu default base URL = %q", general.DefaultBaseURL())
	}

	coding := NewGLMCode()
	if got := coding.SiteTypes(); len(got) != 1 || got[0] != "glm_code" {
		t.Fatalf("glm code site types = %#v", got)
	}
	if coding.DefaultBaseURL() != glmCodeDefaultBaseURL {
		t.Fatalf("glm code default base URL = %q", coding.DefaultBaseURL())
	}
}

func TestZhipuListModelsReturnsCuratedEndpointTypes(t *testing.T) {
	t.Parallel()

	models, err := NewZhipu().ListModels(context.Background(), SiteConfig{SiteType: "zhipu"}, "sk-test")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected curated GLM models")
	}

	var foundText bool
	var foundEmbedding bool
	var foundImage bool
	for _, model := range models {
		endpoints, _ := model.Capabilities["supported_endpoint_types"].([]string)
		switch model.UpstreamName {
		case "glm-5.1":
			foundText = true
			if len(endpoints) != 2 || endpoints[0] != "openai" || endpoints[1] != "anthropic-messages" {
				t.Fatalf("glm-5.1 endpoint types = %#v", endpoints)
			}
		case "embedding-3":
			foundEmbedding = true
			if len(endpoints) != 1 || endpoints[0] != "openai-embedding" {
				t.Fatalf("embedding-3 endpoint types = %#v", endpoints)
			}
		case "glm-image":
			foundImage = true
			if len(endpoints) != 1 || endpoints[0] != "openai-image" {
				t.Fatalf("glm-image endpoint types = %#v", endpoints)
			}
		}
	}

	if !foundText || !foundEmbedding || !foundImage {
		t.Fatalf("expected text, embedding, and image models, got %#v", models)
	}
}

func TestGLMCodeListModelsUsesCodingPlanSubset(t *testing.T) {
	t.Parallel()

	models, err := NewGLMCode().ListModels(context.Background(), SiteConfig{SiteType: "glm_code"}, "sk-test")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 4 {
		t.Fatalf("expected coding model subset, got %d models: %#v", len(models), models)
	}
	if models[0].UpstreamName != "glm-5.1" {
		t.Fatalf("first coding model = %q", models[0].UpstreamName)
	}
}

func TestZhipuValidateCredentialsRejectsEmptyAPIKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		module Zhipu
		apiKey string
	}{
		{name: "zhipu empty", module: NewZhipu(), apiKey: ""},
		{name: "zhipu whitespace", module: NewZhipu(), apiKey: " \t\n"},
		{name: "glm code empty", module: NewGLMCode(), apiKey: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.module.ValidateCredentials(context.Background(), SiteConfig{}, tt.apiKey)
			if err == nil {
				t.Fatal("expected empty api key error")
			}
			if !strings.Contains(err.Error(), "api key is required") {
				t.Fatalf("error = %q", err.Error())
			}
		})
	}
}

func TestZhipuFetchPricingReturnsModelsDevSnapshot(t *testing.T) {
	t.Parallel()

	calls := 0
	client := zhipuModelsDevClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "xLyra/1.0" {
			t.Fatalf("User-Agent = %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"zhipuai": map[string]any{
				"models": map[string]any{
					"glm-5.1": map[string]any{
						"name": "GLM-5.1",
						"cost": map[string]any{
							"input":       1.0,
							"output":      4.0,
							"cache_read":  0.25,
							"cache_write": 0.5,
						},
					},
					"embedding-3": map[string]any{
						"name": "Embedding 3",
						"cost": map[string]any{
							"input": 0.5,
						},
					},
				},
			},
		})
	})

	snapshot, err := NewZhipu().FetchPricing(context.Background(), SiteConfig{
		SiteType: zhipuSiteType,
		Client:   client,
	}, SystemAuth{})
	if err != nil {
		t.Fatalf("FetchPricing returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one models.dev request, got %d", calls)
	}
	if len(snapshot.Groups) != 1 || snapshot.Groups[0].GroupName != "default" {
		t.Fatalf("unexpected pricing groups: %#v", snapshot.Groups)
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("expected two pricing rows, got %d: %#v", len(snapshot.Items), snapshot.Items)
	}

	glm := zhipuPricingByModel(t, snapshot.Items, "glm-5.1")
	if !glm.HasInputValue || glm.InputValue != 1 {
		t.Fatalf("unexpected GLM input price: %#v", glm)
	}
	if !glm.HasOutputValue || glm.OutputValue != 4 {
		t.Fatalf("unexpected GLM output price: %#v", glm)
	}
	if !glm.HasCacheRatio || glm.CacheRatio != 0.25 {
		t.Fatalf("unexpected GLM cache ratio: %#v", glm)
	}
	if !glm.HasCreateCacheRatio || glm.CreateCacheRatio != 0.5 {
		t.Fatalf("unexpected GLM create cache ratio: %#v", glm)
	}

	embedding := zhipuPricingByModel(t, snapshot.Items, "embedding-3")
	if !embedding.HasInputValue || embedding.InputValue != 0.5 {
		t.Fatalf("unexpected embedding input price: %#v", embedding)
	}
	if embedding.HasOutputValue {
		t.Fatalf("expected embedding output price to be empty, got %#v", embedding)
	}

	raw, ok := snapshot.Raw.(map[string]any)
	if !ok {
		t.Fatalf("unexpected raw payload: %#v", snapshot.Raw)
	}
	if raw["provider"] != zhipuModelsDevProvider {
		t.Fatalf("raw provider = %#v", raw["provider"])
	}
}

func TestZhipuFetchPricingReportsModelsDevFailure(t *testing.T) {
	t.Parallel()

	client := zhipuModelsDevClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
	})

	_, err := NewZhipu().FetchPricing(context.Background(), SiteConfig{
		SiteType: zhipuSiteType,
		Client:   client,
	}, SystemAuth{})
	if err == nil {
		t.Fatal("expected FetchPricing error")
	}
	if !strings.Contains(err.Error(), "models.dev pricing returned 503") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestOpenAICompatiblePricingFromModelsDevOnlyBuildsProviderSnapshot(t *testing.T) {
	t.Parallel()

	snapshot := openAICompatiblePricingFromModelsDevOnly("zhipuai", antigravityModelsDevCatalog{
		"zhipuai": {
			Models: map[string]antigravityModelsDevModel{
				"glm-a": {
					Name: "GLM A",
					Cost: map[string]any{
						"input":  0.75,
						"output": 2.5,
					},
				},
				"glm-z": {
					Name: "GLM Z",
					Cost: map[string]any{
						"input": 1.25,
					},
				},
			},
		},
	})

	if len(snapshot.Groups) != 1 || snapshot.Groups[0].GroupName != "default" {
		t.Fatalf("unexpected pricing groups: %#v", snapshot.Groups)
	}
	if len(snapshot.Items) != 2 {
		t.Fatalf("expected two pricing rows, got %d: %#v", len(snapshot.Items), snapshot.Items)
	}

	first := snapshot.Items[0]
	if first.ModelName != "glm-a" || first.DisplayName != "GLM A" {
		t.Fatalf("expected sorted GLM A row first, got %#v", first)
	}
	if !first.HasInputValue || first.InputValue != 0.75 {
		t.Fatalf("unexpected GLM A input price: %#v", first)
	}
	if !first.HasOutputValue || first.OutputValue != 2.5 {
		t.Fatalf("unexpected GLM A output price: %#v", first)
	}

	raw, ok := snapshot.Raw.(map[string]any)
	if !ok {
		t.Fatalf("unexpected raw payload: %#v", snapshot.Raw)
	}
	if raw["provider"] != "zhipuai" {
		t.Fatalf("raw provider = %#v", raw["provider"])
	}
}

func zhipuModelsDevClient(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()

	return &http.Client{
		Transport: adapterRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet {
				t.Fatalf("method = %s", r.Method)
			}
			if r.URL.Scheme != "https" || r.URL.Host != "models.dev" || r.URL.Path != "/api.json" {
				t.Fatalf("unexpected request URL: %s", r.URL.String())
			}

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			return recorder.Result(), nil
		}),
	}
}

func zhipuPricingByModel(t *testing.T, items []ModelPricing, name string) ModelPricing {
	t.Helper()

	for _, item := range items {
		if item.ModelName == name {
			return item
		}
	}
	t.Fatalf("pricing row %q not found in %#v", name, items)
	return ModelPricing{}
}
