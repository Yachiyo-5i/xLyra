package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewAPIAdapterGatewayAPIKeyMethodsUseGatewaySummary(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertNewAPIGatewayAuth(t, r, "Bearer sk-test")

		switch r.URL.Path {
		case "/api/usage/token/":
			writeAdapterJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"object":          "token_usage",
					"total_available": 900,
				},
			})
		case "/v1/models":
			writeAdapterJSON(t, w, map[string]any{
				"object": "list",
				"data": []map[string]any{
					{
						"id":                       "gpt-4o-mini",
						"object":                   "model",
						"owned_by":                 "openai",
						"supported_endpoint_types": []string{"openai", "openai-response"},
					},
					{"id": " \t\n "},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	module := NewNewAPI()
	site := SiteConfig{BaseURL: server.URL, Client: server.Client()}
	if module.serviceForSite(site) == nil || module.serviceForSite(SiteConfig{}) == nil {
		t.Fatal("expected serviceForSite to return a service")
	}
	if err := module.ValidateCredentials(context.Background(), site, "sk-test"); err != nil {
		t.Fatalf("ValidateCredentials returned error: %v", err)
	}

	models, err := module.ListModels(context.Background(), site, "sk-test")
	if err != nil {
		t.Fatalf("ListModels returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models length = %d, want 1", len(models))
	}
	model := models[0]
	if model.UpstreamName != "gpt-4o-mini" || model.DisplayName != "gpt-4o-mini" {
		t.Fatalf("unexpected model = %#v", model)
	}
	endpoints, _ := model.Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 2 || endpoints[0] != "openai" || endpoints[1] != "openai-response" {
		t.Fatalf("supported endpoints = %#v", model.Capabilities["supported_endpoint_types"])
	}
	if model.Capabilities["source"] != "newapi" || model.Capabilities["owned_by"] != "openai" {
		t.Fatalf("unexpected model capabilities = %#v", model.Capabilities)
	}

	summary, err := module.SummarizeAPIKey(context.Background(), site, "sk-test")
	if err != nil {
		t.Fatalf("SummarizeAPIKey returned error: %v", err)
	}
	if len(summary.Models) != 1 || summary.Models[0].UpstreamName != "gpt-4o-mini" {
		t.Fatalf("summary models = %#v", summary.Models)
	}
	usage, _ := summary.Usage.(map[string]any)
	if usage["success"] != true {
		t.Fatalf("summary usage = %#v", summary.Usage)
	}
}

func TestNewAPIAdapterUserAuthMethodsWrapUserSummaryAndCheckin(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/user/self":
			assertNewAPIUserAuth(t, r)
			writeAdapterJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"id":              42,
					"quota":           1000,
					"used_quota":      100,
					"request_count":   12,
					"available_quota": 900,
				},
			})
		case "/api/token/":
			assertNewAPIUserAuth(t, r)
			writeAdapterJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"items": []map[string]any{
						{"id": 7, "name": " default ", "key": "sk-***", "status": "enabled"},
					},
				},
			})
		case "/api/token/batch/keys":
			assertNewAPIUserAuth(t, r)
			if r.Method != http.MethodPost {
				t.Fatalf("batch key method = %s, want POST", r.Method)
			}
			writeAdapterJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"keys": map[string]any{"7": "full-newapi"},
				},
			})
		case "/api/user/models":
			assertNewAPIUserAuth(t, r)
			writeAdapterJSON(t, w, map[string]any{
				"success": true,
				"data": []map[string]any{
					{"id": "gpt-4o-mini"},
				},
			})
		case "/api/pricing":
			assertNewAPIUserAuth(t, r)
			writeAdapterJSON(t, w, map[string]any{
				"success":        true,
				"quota_per_unit": 500000,
				"group_ratio": map[string]any{
					"default": 1,
				},
				"data": map[string]any{
					"gpt-4o-mini": map[string]any{
						"model_ratio":      0.3,
						"completion_ratio": 4.0,
					},
				},
			})
		case "/api/user/checkin":
			assertNewAPIUserAuth(t, r)
			writeAdapterJSON(t, w, map[string]any{
				"success": true,
				"message": "checked in",
			})
		case "/api/usage/token/":
			assertNewAPIGatewayAuth(t, r, "Bearer sk-full-newapi")
			writeAdapterJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"total_available": 900,
				},
			})
		case "/v1/models":
			assertNewAPIGatewayAuth(t, r, "Bearer sk-full-newapi")
			writeAdapterJSON(t, w, map[string]any{
				"data": []map[string]any{
					{"id": "gpt-4o-mini", "object": "model"},
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	module := NewNewAPI()
	site := SiteConfig{BaseURL: server.URL, Client: server.Client()}
	auth := SystemAuth{AccessToken: "Bearer access-token", UserID: 42}

	keys, err := module.ListAPIKeys(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 || keys[0].ID != 7 || keys[0].Name != "default" || keys[0].Key != "sk-full-newapi" {
		t.Fatalf("unexpected api keys = %#v", keys)
	}
	if keys[0].MaskedKey == "" || keys[0].Status != "enabled" {
		t.Fatalf("api key metadata = %#v", keys[0])
	}

	summary, err := module.FetchUserSummary(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("FetchUserSummary returned error: %v", err)
	}
	if !summary.CheckinReady || summary.User == nil || summary.APIKeys == nil || summary.UserModels == nil || summary.Pricing == nil {
		t.Fatalf("unexpected user summary = %#v", summary)
	}

	balance, err := module.FetchBalance(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("FetchBalance returned error: %v", err)
	}
	if balance.Raw == nil {
		t.Fatal("expected balance raw user payload")
	}

	metadata, err := module.FetchMetadata(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("FetchMetadata returned error: %v", err)
	}
	metadataRaw, _ := metadata.Raw.(map[string]any)
	if metadataRaw["user"] == nil || metadataRaw["user_models"] == nil || metadataRaw["checkin"] == nil {
		t.Fatalf("metadata raw = %#v", metadata.Raw)
	}

	pricing, err := module.FetchPricing(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("FetchPricing returned error: %v", err)
	}
	if len(pricing.Items) != 1 || pricing.Items[0].ModelName != "gpt-4o-mini" {
		t.Fatalf("pricing items = %#v", pricing.Items)
	}

	checkin, err := module.ExecuteCheckin(context.Background(), site, auth)
	if err != nil {
		t.Fatalf("ExecuteCheckin returned error: %v", err)
	}
	checkinRaw, _ := checkin.Raw.(map[string]any)
	if checkinRaw["success"] != true {
		t.Fatalf("checkin raw = %#v", checkin.Raw)
	}
}

func TestNewAPIAdapterDetectAndModelsFromPayload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		writeAdapterJSON(t, w, map[string]any{
			"success": true,
			"data": map[string]any{
				"version":                "v0.8.0",
				"quota_per_unit":         500000,
				"quota_display_type":     "quota",
				"checkin_enabled":        true,
				"default_use_auto_group": false,
			},
		})
	}))
	defer server.Close()

	result, err := NewNewAPI().Detect(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if !result.Matched || result.SiteType != "newapi" || result.Confidence <= 0 {
		t.Fatalf("unexpected detect result = %#v", result)
	}

	if got := modelsFromPayload(nil); got != nil {
		t.Fatalf("nil payload models = %#v, want nil", got)
	}
	models := modelsFromPayload(map[string]any{
		"data": []any{
			map[string]any{
				"id":                       " image-model ",
				"object":                   "model",
				"owned_by":                 "newapi",
				"supported_endpoint_types": "openai, openai-image",
			},
			map[string]any{"id": ""},
			"ignored",
		},
	})
	if len(models) != 1 {
		t.Fatalf("models length = %d, want 1", len(models))
	}
	if models[0].UpstreamName != "image-model" {
		t.Fatalf("upstream name = %q, want image-model", models[0].UpstreamName)
	}
	endpoints, _ := models[0].Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 2 || endpoints[1] != "openai-image" {
		t.Fatalf("endpoint types = %#v", models[0].Capabilities["supported_endpoint_types"])
	}
}

func TestNewAPIModelsFromPayloadCleansEndpointListAndSkipsMalformedRows(t *testing.T) {
	t.Parallel()

	models := modelsFromPayload(map[string]any{
		"data": []any{
			"not a model map",
			map[string]any{"id": "   "},
			map[string]any{
				"id":                       " model-a ",
				"object":                   "model",
				"owned_by":                 "team-a",
				"supported_endpoint_types": " chat , , embeddings ",
			},
		},
	})

	if len(models) != 1 {
		t.Fatalf("models length = %d, want 1: %#v", len(models), models)
	}
	model := models[0]
	if model.UpstreamName != "model-a" || model.DisplayName != "model-a" {
		t.Fatalf("unexpected model identity: %#v", model)
	}
	endpoints, ok := model.Capabilities["supported_endpoint_types"].([]string)
	if !ok || len(endpoints) != 2 || endpoints[0] != "chat" || endpoints[1] != "embeddings" {
		t.Fatalf("supported endpoints = %#v, want cleaned endpoint list", model.Capabilities["supported_endpoint_types"])
	}
	if model.Capabilities["object"] != "model" || model.Capabilities["owned_by"] != "team-a" {
		t.Fatalf("unexpected model capabilities: %#v", model.Capabilities)
	}
}

func assertNewAPIUserAuth(t *testing.T, r *http.Request) {
	t.Helper()

	if got := r.Header.Get("Authorization"); got != "access-token" {
		t.Fatalf("Authorization = %q, want access-token", got)
	}
	if got := r.Header.Get("New-Api-User"); got != "42" {
		t.Fatalf("New-Api-User = %q, want 42", got)
	}
}

func assertNewAPIGatewayAuth(t *testing.T, r *http.Request, want string) {
	t.Helper()

	if got := r.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func writeAdapterJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
