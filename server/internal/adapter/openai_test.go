package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAICompatibleListModelsAddsChatEndpointSupport(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":       "deepseek-v4-flash",
					"object":   "model",
					"owned_by": "deepseek",
				},
				{
					"id":       "text-embedding-3-small",
					"object":   "model",
					"owned_by": "openai",
				},
				{
					"id":       "claude-3-5-sonnet",
					"object":   "model",
					"owned_by": "anthropic",
				},
			},
		})
	}))
	defer server.Close()

	models, err := NewOpenAICompatible().ListModels(context.Background(), SiteConfig{BaseURL: server.URL + "/"}, "sk-test")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}

	endpoints, _ := models[0].Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 2 || endpoints[0] != "openai" || endpoints[1] != "anthropic-messages" {
		t.Fatalf("unexpected supported_endpoint_types for chat model: %#v", models[0].Capabilities["supported_endpoint_types"])
	}

	embeddingEndpoints, _ := models[1].Capabilities["supported_endpoint_types"].([]string)
	if len(embeddingEndpoints) != 1 || embeddingEndpoints[0] != "openai-embedding" {
		t.Fatalf("unexpected supported_endpoint_types for embedding model: %#v", models[1].Capabilities["supported_endpoint_types"])
	}

	claudeEndpoints, _ := models[2].Capabilities["supported_endpoint_types"].([]string)
	if len(claudeEndpoints) != 1 || claudeEndpoints[0] != "anthropic-messages" {
		t.Fatalf("unexpected supported_endpoint_types for claude model: %#v", models[2].Capabilities["supported_endpoint_types"])
	}
}

func TestOpenAICompatibleEndpointTypesUseModelNamesForGenericChannels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		modelID  string
		siteType string
		baseURL  string
		want     []string
	}{
		{modelID: "grok-4.5", siteType: "openai", want: []string{"openai", "openai-response"}},
		{modelID: "grok-imagine-image-quality", siteType: "openai", want: []string{"openai-image"}},
		{modelID: "gemini-image-3", siteType: "openai", want: []string{"openai-image"}},
		{modelID: "nano_banana_2", siteType: "openai", want: []string{"openai-image"}},
		{modelID: "nano-banana-pro", siteType: "openai", want: []string{"openai-image"}},
		{modelID: "gemini-image-3", siteType: "xlyra", want: []string{"google-gemini"}},
		{modelID: "grok-4.5", siteType: "deepseek", want: []string{"openai", "anthropic-messages"}},
		{modelID: "deepseek-chat", siteType: "openai", baseURL: "https://relay.example.com/anthropic", want: []string{"anthropic-messages"}},
		{modelID: "glm-4.7", siteType: "openai", baseURL: "https://relay.example.com/api/anthropic/", want: []string{"anthropic-messages"}},
		{modelID: "deepseek-chat", siteType: "openai", baseURL: "https://relay.example.com", want: []string{"openai", "anthropic-messages"}},
		{modelID: "kimi-k2", siteType: "openai", baseURL: "https://anthropic.example.com/v1", want: []string{"openai", "anthropic-messages"}},
	}
	for _, tc := range cases {
		if got := openAICompatibleEndpointTypes(tc.modelID, tc.siteType, tc.baseURL); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("openAICompatibleEndpointTypes(%q, %q, %q) = %#v, want %#v", tc.modelID, tc.siteType, tc.baseURL, got, tc.want)
		}
	}
}

func TestOpenAICompatibleListModelsReportsStatusAndDecodeErrors(t *testing.T) {
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
			wantErr:    "upstream returned 401: invalid key",
		},
		{
			name:       "bad json",
			statusCode: http.StatusOK,
			body:       `{`,
			wantErr:    "decode upstream models",
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

			_, err := NewOpenAICompatible().ListModels(context.Background(), SiteConfig{BaseURL: server.URL}, "sk-test")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestOpenAICompatibleValidateCredentialsPropagatesListModelsErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()

	err := NewOpenAICompatible().ValidateCredentials(context.Background(), SiteConfig{BaseURL: server.URL + "/"}, "sk-test")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "upstream returned 403: denied") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestOpenAICompatiblePricingFromModelsDevUsesDeepSeekProvider(t *testing.T) {
	t.Parallel()

	provider := openAICompatibleModelsDevProvider(SiteConfig{
		Name:     "DeepSeek",
		SiteType: "openai",
		BaseURL:  "https://api.deepseek.com",
	})
	if provider != "deepseek" {
		t.Fatalf("unexpected provider: %q", provider)
	}

	snapshot := openAICompatiblePricingFromModelsDev(provider, []Model{
		{UpstreamName: "deepseek-v4-flash", DisplayName: "deepseek-v4-flash"},
	}, antigravityModelsDevCatalog{
		"deepseek": {
			Models: map[string]antigravityModelsDevModel{
				"deepseek-v4-flash": {
					ID:   "deepseek-v4-flash",
					Name: "DeepSeek V4 Flash",
					Cost: map[string]any{
						"input":      0.14,
						"output":     0.28,
						"cache_read": 0.028,
					},
				},
			},
		},
	})

	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one pricing row, got %d", len(snapshot.Items))
	}
	row := snapshot.Items[0]
	if !row.HasInputValue || row.InputValue != 0.14 {
		t.Fatalf("unexpected input price: %#v", row)
	}
	if !row.HasOutputValue || row.OutputValue != 0.28 {
		t.Fatalf("unexpected output price: %#v", row)
	}
}

func TestOpenAICompatiblePricingEndpointTypesUseProviderRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		provider string
		modelID  string
		want     []string
	}{
		{provider: "openai", modelID: "grok-4.5", want: []string{"openai", "openai-response"}},
		{provider: "openai", modelID: "grok-imagine-image-quality", want: []string{"openai-image"}},
		{provider: "deepseek", modelID: "grok-4.5", want: []string{"openai", "anthropic-messages"}},
		{provider: "zhipuai", modelID: "glm-5", want: []string{"openai", "anthropic-messages"}},
	}
	for _, tc := range cases {
		pricing := openAICompatibleModelPricingFromModelsDev(
			Model{UpstreamName: tc.modelID},
			tc.provider,
			tc.modelID,
			antigravityModelsDevModel{},
		)
		endpoints, _ := pricing.Raw["supported_endpoint_types"].([]string)
		if !reflect.DeepEqual(endpoints, tc.want) {
			t.Errorf("pricing endpoint types for provider=%q model=%q = %#v, want %#v", tc.provider, tc.modelID, endpoints, tc.want)
		}
	}
}

func TestOpenAICompatibleModelsDevProviderNormalization(t *testing.T) {
	t.Parallel()

	cases := map[string]SiteConfig{
		"zhipuai": {
			SiteType: " zhipu ",
		},
		"moonshotai-cn": {
			SiteType: "kimi_code",
		},
		"xiaomi": {
			SiteType: "xiaomi_mimo",
		},
		"minimax": {
			Name: "MiniMax Open Platform",
		},
		"openai": {
			SiteType: "openai",
		},
		"custom_provider": {
			SiteType: " Custom_Provider ",
		},
	}

	for want, site := range cases {
		site := site
		want := want
		t.Run(want, func(t *testing.T) {
			t.Parallel()

			if got := openAICompatibleModelsDevProvider(site); got != want {
				t.Fatalf("provider = %q, want %q", got, want)
			}
		})
	}
}

func TestOpenAICompatiblePricingFromModelsDevOnlySortsCatalogItems(t *testing.T) {
	t.Parallel()

	snapshot := openAICompatiblePricingFromModelsDevOnly("openai", antigravityModelsDevCatalog{
		"openai": {
			Models: map[string]antigravityModelsDevModel{
				"z-model": {Name: "Zed", Cost: map[string]any{"input": 1.2, "output": 2.4}},
				"a-model": {Name: "Alpha", Cost: map[string]any{"input": 0.1, "output": 0.2}},
			},
		},
	})

	if len(snapshot.Items) != 2 {
		t.Fatalf("pricing item count = %d, want 2", len(snapshot.Items))
	}
	if snapshot.Items[0].ModelName != "a-model" || snapshot.Items[1].ModelName != "z-model" {
		t.Fatalf("items not sorted by model name: %#v", snapshot.Items)
	}
	raw, ok := snapshot.Raw.(map[string]any)
	if !ok {
		t.Fatalf("raw snapshot = %#v, want map", snapshot.Raw)
	}
	rawItems, ok := raw["items"].([]map[string]any)
	if !ok || len(rawItems) != 2 {
		t.Fatalf("raw items = %#v, want two item maps", raw["items"])
	}
	if rawItems[0]["model_name"] != "a-model" || rawItems[1]["model_name"] != "z-model" {
		t.Fatalf("raw items not sorted: %#v", rawItems)
	}
}
