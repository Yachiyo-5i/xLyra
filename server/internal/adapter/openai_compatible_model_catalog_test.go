package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPClientForSitePrefersSiteThenFallbackThenDefault(t *testing.T) {
	t.Parallel()

	siteClient := &http.Client{}
	fallbackClient := &http.Client{}

	if got := httpClientForSite(SiteConfig{Client: siteClient}, fallbackClient); got != siteClient {
		t.Fatalf("httpClientForSite with site client = %p, want %p", got, siteClient)
	}
	if got := httpClientForSite(SiteConfig{}, fallbackClient); got != fallbackClient {
		t.Fatalf("httpClientForSite with fallback client = %p, want %p", got, fallbackClient)
	}
	if got := httpClientForSite(SiteConfig{}, nil); got != http.DefaultClient {
		t.Fatalf("httpClientForSite default = %p, want %p", got, http.DefaultClient)
	}
}

func TestOpenAICompatibleListModelsPreservesMetadataAndAllowsEmptyAPIKey(t *testing.T) {
	t.Parallel()

	authorization := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		authorization <- r.Header.Get("Authorization")
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q, want application/json", got)
		}
		writeJSONResponse(t, w, map[string]any{
			"data": []map[string]any{
				{
					"id":       "metadata-model",
					"object":   "model",
					"metadata": map[string]any{"tier": "preview"},
				},
				{
					"id":       "",
					"object":   "model",
					"owned_by": "ignored",
				},
			},
		})
	}))
	defer server.Close()

	models, err := NewOpenAICompatible().ListModels(context.Background(), SiteConfig{
		BaseURL: server.URL,
	}, " \t\n ")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if got := <-authorization; got != "" {
		t.Fatalf("Authorization = %q, want empty header for blank api key", got)
	}
	if len(models) != 1 {
		t.Fatalf("models length = %d, want 1: %#v", len(models), models)
	}
	model := models[0]
	if model.UpstreamName != "metadata-model" || model.DisplayName != "metadata-model" {
		t.Fatalf("unexpected model = %#v", model)
	}
	if _, ok := model.Capabilities["owned_by"]; ok {
		t.Fatalf("owned_by should be omitted when upstream value is empty: %#v", model.Capabilities)
	}
	metadata, ok := model.Capabilities["metadata"].(map[string]any)
	if !ok || metadata["tier"] != "preview" {
		t.Fatalf("metadata capability = %#v, want preview tier", model.Capabilities["metadata"])
	}
}

func TestOpenAICompatibleFetchPricingFallsBackToCatalogWhenModelListFails(t *testing.T) {
	t.Parallel()

	var seenModelsDev bool
	var seenModelList bool
	client := &http.Client{Transport: adapterRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		switch {
		case r.URL.Scheme == "https" && r.URL.Host == "models.dev" && r.URL.Path == "/api.json":
			seenModelsDev = true
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Fatalf("models.dev Accept = %q, want application/json", got)
			}
			if got := r.Header.Get("User-Agent"); got != "xLyra/1.0" {
				t.Fatalf("models.dev User-Agent = %q, want xLyra/1.0", got)
			}
			writeJSONResponse(t, recorder, map[string]any{
				"openai": map[string]any{
					"models": map[string]any{
						"gpt-catalog-fallback": map[string]any{
							"id":   "gpt-catalog-fallback",
							"name": "GPT Catalog Fallback",
							"cost": map[string]any{
								"input":  0.1,
								"output": 0.4,
							},
						},
					},
				},
			})
		case r.URL.Host == "api.example.test" && r.URL.Path == "/v1/models":
			seenModelList = true
			http.Error(recorder, "model list unavailable", http.StatusBadGateway)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		return recorder.Result(), nil
	})}

	snapshot, err := NewOpenAICompatible().FetchPricing(context.Background(), SiteConfig{
		Name:     "OpenAI mirror",
		SiteType: "openai",
		BaseURL:  "https://api.example.test",
		Client:   client,
	}, SystemAuth{AccessToken: "sk-test"})
	if err != nil {
		t.Fatalf("FetchPricing returned error: %v", err)
	}
	if !seenModelsDev || !seenModelList {
		t.Fatalf("seenModelsDev=%v seenModelList=%v, want both requests", seenModelsDev, seenModelList)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("pricing items length = %d, want 1: %#v", len(snapshot.Items), snapshot.Items)
	}
	row := snapshot.Items[0]
	if row.ModelName != "gpt-catalog-fallback" || row.DisplayName != "GPT Catalog Fallback" {
		t.Fatalf("unexpected fallback pricing row: %#v", row)
	}
	if !row.HasInputValue || row.InputValue != 0.1 || !row.HasOutputValue || row.OutputValue != 0.4 {
		t.Fatalf("unexpected pricing values: %#v", row)
	}
	raw, ok := snapshot.Raw.(map[string]any)
	if !ok || raw["provider"] != "openai" || raw["source"] != "models_dev" {
		t.Fatalf("unexpected pricing raw snapshot: %#v", snapshot.Raw)
	}
}

func TestOpenAICompatibleModelsDevProviderNormalizesKnownAliases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		site SiteConfig
		want string
	}{
		{
			name: "xiaomi mimo site type",
			site: SiteConfig{SiteType: " xiaomi_mimo "},
			want: "xiaomi",
		},
		{
			name: "glm name maps to zhipuai",
			site: SiteConfig{Name: "GLM Code", SiteType: "openai"},
			want: "zhipuai",
		},
		{
			name: "moonshot base url",
			site: SiteConfig{BaseURL: "https://api.moonshot.cn", SiteType: "openai"},
			want: "moonshotai-cn",
		},
		{
			name: "custom site type lowercased",
			site: SiteConfig{SiteType: "  CustomProvider  "},
			want: "customprovider",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := openAICompatibleModelsDevProvider(tt.site); got != tt.want {
				t.Fatalf("openAICompatibleModelsDevProvider = %q, want %q", got, tt.want)
			}
		})
	}
}

type adapterRoundTripFunc func(*http.Request) (*http.Response, error)

func (f adapterRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
