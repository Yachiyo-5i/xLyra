package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiModelsPayloadUsesNativeShapeAndFiltersOtherProtocols(t *testing.T) {
	payload := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"id": "gemini-2.5-pro",
				"metadata": map[string]any{
					"display_name":             "Gemini 2.5 Pro",
					"description":              "reasoning model",
					"version":                  "2.5",
					"input_token_limit":        float64(1048576),
					"output_token_limit":       65536,
					"supported_endpoint_types": []any{"google-gemini"},
				},
			},
			{
				"id": "gpt-5",
				"metadata": map[string]any{
					"supported_endpoint_types": []string{"openai"},
				},
			},
		},
	}

	got := geminiModelsPayload(payload)
	models, ok := got["models"].([]map[string]any)
	if !ok || len(models) != 1 {
		t.Fatalf("models = %#v, want one Gemini model", got["models"])
	}
	model := models[0]
	if model["name"] != "models/gemini-2.5-pro" || model["displayName"] != "Gemini 2.5 Pro" {
		t.Fatalf("native model identity = %#v", model)
	}
	if model["version"] != "2.5" || model["description"] != "reasoning model" {
		t.Fatalf("native model metadata = %#v", model)
	}
	if model["inputTokenLimit"] != 1048576 || model["outputTokenLimit"] != 65536 {
		t.Fatalf("native token limits = %#v", model)
	}
	methods, ok := model["supportedGenerationMethods"].([]string)
	if !ok || len(methods) != 1 || methods[0] != "generateContent" {
		t.Fatalf("supported generation methods = %#v", model["supportedGenerationMethods"])
	}
}

func TestGeminiModelsPayloadHandlesMissingAndPrefixedIDs(t *testing.T) {
	got := geminiModelsPayload(map[string]any{
		"data": []map[string]any{
			{"id": "models/gemini-flash", "metadata": map[string]any{"supported_endpoint_types": []string{"google-gemini"}}},
			{"id": "", "metadata": map[string]any{"supported_endpoint_types": []string{"google-gemini"}}},
		},
	})
	models := got["models"].([]map[string]any)
	if len(models) != 1 || models[0]["name"] != "models/gemini-flash" || models[0]["displayName"] != "models/gemini-flash" {
		t.Fatalf("models = %#v", models)
	}

	empty := geminiModelsPayload(map[string]any{"data": "invalid"})
	if models, ok := empty["models"].([]map[string]any); !ok || len(models) != 0 {
		t.Fatalf("invalid payload models = %#v", empty["models"])
	}
}

func TestGeminiModelsReturnsGeminiErrorWhenUnavailable(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	req.Header.Set("X-Request-ID", "req-gemini-models")
	rec := httptest.NewRecorder()

	handler.GeminiModels(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rec.Body.String(), `"status":"GATEWAY_UNAVAILABLE"`) {
		t.Fatalf("response = %s", rec.Body.String())
	}
}
