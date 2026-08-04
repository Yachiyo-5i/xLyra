package adapter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAntigravityQuotaFromRawBuildsModelQuotaAndFiltersUnsupportedNames(t *testing.T) {
	t.Parallel()

	resetTime := "2026-06-22T12:34:56Z"
	raw := map[string]any{
		"models": map[string]any{
			"gemini-3.1-pro": map[string]any{
				"displayName":      "Gemini 3.1 Pro",
				"supportsImages":   false,
				"supportsThinking": true,
				"thinkingBudget":   float64(32768),
				"recommended":      true,
				"maxTokens":        float64(1048576),
				"maxOutputTokens":  float64(65536),
				"supportedMimeTypes": map[string]any{
					"text/plain": true,
				},
				"quotaInfo": map[string]any{
					"remainingFraction": 0.42,
					"resetTime":         resetTime,
				},
			},
			"image-3.0-generate": map[string]any{
				"quotaInfo": map[string]any{
					"remainingFraction": 1.5,
				},
			},
			"internal-preview": map[string]any{
				"quotaInfo": map[string]any{
					"remainingFraction": 1,
				},
			},
		},
	}

	snapshot := antigravityQuotaFromRaw(raw)
	if len(snapshot.Models) != 2 {
		t.Fatalf("models length = %d, want 2: %#v", len(snapshot.Models), snapshot.Models)
	}

	pro := antigravityQuotaByName(snapshot.Models, "gemini-3.1-pro")
	if pro == nil {
		t.Fatal("missing gemini quota model")
	}
	if pro.DisplayName != "Gemini 3.1 Pro" || pro.RemainingPercent != 42 || pro.UsedPercent != 58 {
		t.Fatalf("unexpected gemini quota model: %#v", pro)
	}
	reset, _ := time.Parse(time.RFC3339, resetTime)
	if pro.ResetAt != reset.Unix() || pro.ResetTime != resetTime {
		t.Fatalf("reset fields = %#v/%q, want %d/%q", pro.ResetAt, pro.ResetTime, reset.Unix(), resetTime)
	}
	if pro.SupportsImages != false || pro.SupportsThinking != true || pro.ThinkingBudget != float64(32768) {
		t.Fatalf("unexpected model capabilities copied from raw: %#v", pro)
	}
	if pro.SupportedMimeTypes["text/plain"] != true {
		t.Fatalf("mime types = %#v, want text/plain", pro.SupportedMimeTypes)
	}

	image := antigravityQuotaByName(snapshot.Models, "image-3.0-generate")
	if image == nil {
		t.Fatal("missing image quota model")
	}
	if image.DisplayName != "image-3.0-generate" || image.RemainingPercent != 100 || image.UsedPercent != 0 {
		t.Fatalf("unexpected image quota model: %#v", image)
	}

	quotaModels, ok := snapshot.Quota["models"].([]map[string]any)
	if !ok || len(quotaModels) != 2 {
		t.Fatalf("quota models payload = %#v, want 2 models", snapshot.Quota["models"])
	}
	if snapshot.Quota["is_forbidden"] != false {
		t.Fatalf("is_forbidden = %#v, want false", snapshot.Quota["is_forbidden"])
	}
}

func TestAntigravityModelsFromQuotaCopiesQuotaAndEndpointCapabilities(t *testing.T) {
	t.Parallel()

	snapshot := antigravityQuotaSnapshot{
		Models: []antigravityModelQuota{
			{
				Name:             "gemini-3.1-pro",
				DisplayName:      "Gemini 3.1 Pro",
				RemainingPercent: 20,
				UsedPercent:      80,
				SupportsThinking: true,
				MaxOutputTokens:  float64(65536),
				Raw:              map[string]any{"source": "quota"},
			},
			{
				Name:             "image-3.0-generate",
				RemainingPercent: 0,
				UsedPercent:      100,
			},
		},
	}

	models := antigravityModelsFromQuota(snapshot)
	if len(models) != 2 {
		t.Fatalf("models length = %d, want 2", len(models))
	}

	pro := adapterModelByName(models, "gemini-3.1-pro")
	if pro == nil {
		t.Fatal("missing gemini model")
	}
	if pro.DisplayName != "Gemini 3.1 Pro" {
		t.Fatalf("display name = %q, want Gemini 3.1 Pro", pro.DisplayName)
	}
	if pro.Capabilities["source"] != antigravitySiteType || pro.Capabilities["available"] != true {
		t.Fatalf("unexpected pro capabilities: %#v", pro.Capabilities)
	}
	endpoints, ok := pro.Capabilities["supported_endpoint_types"].([]string)
	if !ok || len(endpoints) != 3 || endpoints[0] != "openai" || endpoints[2] != "google-gemini" {
		t.Fatalf("pro endpoint types = %#v", pro.Capabilities["supported_endpoint_types"])
	}
	quota, ok := pro.Capabilities["quota"].(map[string]any)
	if !ok || quota["remaining_percent"] != 20 || quota["used_percent"] != 80 {
		t.Fatalf("pro quota capability = %#v", pro.Capabilities["quota"])
	}

	image := adapterModelByName(models, "image-3.0-generate")
	if image == nil {
		t.Fatal("missing image model")
	}
	if image.Capabilities["available"] != false {
		t.Fatalf("image available = %#v, want false", image.Capabilities["available"])
	}
	imageEndpoints, _ := image.Capabilities["supported_endpoint_types"].([]string)
	if len(imageEndpoints) != 4 || imageEndpoints[3] != "openai-image" {
		t.Fatalf("image endpoint types = %#v, want openai-image included", imageEndpoints)
	}

	rawModels := antigravityRawModels(snapshot.Models)
	if len(rawModels) != 2 {
		t.Fatalf("raw models length = %d, want 2", len(rawModels))
	}
	rawImage := antigravityRawModelByName(rawModels, "image-3.0-generate")
	if rawImage == nil || rawImage["id"] != "image-3.0-generate" || rawImage["display"] != "image-3.0-generate" {
		t.Fatalf("raw image model = %#v", rawImage)
	}
}

func TestAntigravityImageGenerationModelRecognizesNanoBananaAliases(t *testing.T) {
	t.Parallel()

	for _, modelID := range []string{"nano_banana_2", "nano-banana-pro"} {
		if !antigravityImageGenerationModel(modelID) {
			t.Errorf("antigravityImageGenerationModel(%q) = false, want true", modelID)
		}
	}
}

func TestAntigravityProjectFromRawChoosesTierFallbacks(t *testing.T) {
	t.Parallel()

	paid := antigravityProjectFromRaw(map[string]any{
		"cloudaicompanionProject": "project-a",
		"paidTier": map[string]any{
			"name": "pro",
		},
	})
	if paid.ProjectID != "project-a" || paid.SubscriptionTier != "pro" {
		t.Fatalf("paid project snapshot = %#v", paid)
	}

	defaultTier := antigravityProjectFromRaw(map[string]any{
		"currentTier": map[string]any{},
		"allowedTiers": []any{
			map[string]any{"id": "free", "is_default": true},
			map[string]any{"id": "pro"},
		},
	})
	if defaultTier.SubscriptionTier != "free" {
		t.Fatalf("default tier = %q, want free", defaultTier.SubscriptionTier)
	}
}

func TestAntigravityHTTPResponseAndHeaderHelpers(t *testing.T) {
	t.Parallel()

	forbidden, err := readAntigravityQuotaResponse(&http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(strings.NewReader("forbidden")),
	})
	if err != nil {
		t.Fatalf("forbidden quota response should be converted to empty snapshot: %v", err)
	}
	if forbidden["is_forbidden"] != true {
		t.Fatalf("forbidden payload = %#v, want is_forbidden", forbidden)
	}

	okPayload, err := readAntigravityQuotaResponse(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"models":{}}`)),
	})
	if err != nil || okPayload["models"] == nil {
		t.Fatalf("ok quota response = %#v, err=%v", okPayload, err)
	}

	if _, err := readAntigravityQuotaResponse(&http.Response{
		StatusCode: http.StatusTooManyRequests,
		Body:       io.NopCloser(strings.NewReader("slow down")),
	}); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("429 response error = %v, want status error", err)
	}
	if _, err := readAntigravityQuotaResponse(&http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader("bad request")),
	}); err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("400 response error = %v, want status error", err)
	}

	decoded, err := decodeJSONMap(strings.NewReader(`{"ok":true}`))
	if err != nil || decoded["ok"] != true {
		t.Fatalf("decoded map = %#v, err=%v", decoded, err)
	}
	if _, err := decodeJSONMap(strings.NewReader(`{`)); err == nil {
		t.Fatal("expected invalid JSON to fail")
	}

	req, err := http.NewRequest(http.MethodPost, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	antigravitySetHeaders(req, " token ")
	if req.Header.Get("Authorization") != "Bearer token" || req.Header.Get("x-client-name") != "antigravity" {
		t.Fatalf("unexpected antigravity headers: %#v", req.Header)
	}
}

func TestAntigravityEndpointAndSmallValueHelpers(t *testing.T) {
	t.Parallel()

	if got := antigravityBaseURL(" "); got != antigravityDefaultBaseURL {
		t.Fatalf("empty base URL = %q, want default", got)
	}
	if got := antigravityBaseURL(" https://example.com/ "); got != "https://example.com" {
		t.Fatalf("trimmed base URL = %q, want https://example.com", got)
	}
	customEndpoints := antigravityQuotaEndpointURLs("https://example.com/")
	if len(customEndpoints) != len(antigravityQuotaEndpoints)+1 || customEndpoints[0] != "https://example.com/v1internal:fetchAvailableModels" {
		t.Fatalf("custom endpoints = %#v", customEndpoints)
	}
	defaultEndpoints := antigravityQuotaEndpointURLs(antigravityDefaultBaseURL)
	if len(defaultEndpoints) != len(antigravityQuotaEndpoints) || defaultEndpoints[0] != antigravityQuotaEndpoints[0] {
		t.Fatalf("default endpoints = %#v", defaultEndpoints)
	}

	if !antigravityKeepModel(" Gemini-Pro ") || !antigravityKeepModel("imagen-3") || antigravityKeepModel("internal-preview") {
		t.Fatal("unexpected antigravity model keep rules")
	}
	if got := antigravityResetAt("not-a-time"); got != "not-a-time" {
		t.Fatalf("invalid reset time = %#v, want original string", got)
	}
	if got := antigravityResetAt(" "); got != nil {
		t.Fatalf("blank reset time = %#v, want nil", got)
	}
	if got := antigravityFloat(int64(12)); got != 12 {
		t.Fatalf("int64 float = %v, want 12", got)
	}
	if got := antigravityFloat(float32(1.5)); got != 1.5 {
		t.Fatalf("float32 value = %v, want 1.5", got)
	}
	if got := antigravityFloat(7); got != 7 {
		t.Fatalf("int value = %v, want 7", got)
	}
	if got := antigravityFloat("12"); got != 0 {
		t.Fatalf("string float = %v, want 0", got)
	}
	if !boolFromAnyValue(true) || boolFromAnyValue("true") {
		t.Fatal("unexpected bool parsing")
	}
	if mapFromAny(map[string]any{"ok": true})["ok"] != true {
		t.Fatal("expected mapFromAny to return map values")
	}
	if len(sliceFromAny([]any{"one"})) != 1 {
		t.Fatal("expected sliceFromAny to return slice values")
	}
	if mapStringAny(map[string]any{"mime": true})["mime"] != true {
		t.Fatal("expected mapStringAny to return map values")
	}
	if got := defaultAntigravityAPIKeyName(" user@example.com "); got != "user@example.com" {
		t.Fatalf("api key name = %q, want trimmed email", got)
	}
	if got := defaultAntigravityAPIKeyName(" "); got != "Antigravity OAuth" {
		t.Fatalf("blank api key name = %q, want fallback", got)
	}
}

func TestAntigravityListAPIKeysBuildsOAuthBearerRecord(t *testing.T) {
	t.Parallel()

	keys, err := NewAntigravity().ListAPIKeys(context.Background(), SiteConfig{}, SystemAuth{
		AccessToken: "token",
		AccountID:   "acct-1",
		Email:       "user@example.com",
		Metadata: map[string]any{
			"project_id":        "project-a",
			"subscription_tier": "pro",
		},
	})
	if err != nil {
		t.Fatalf("ListAPIKeys returned error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("keys length = %d, want 1", len(keys))
	}
	key := keys[0]
	if key.Name != "user@example.com" || key.Status != "connected" || key.Key != "token" {
		t.Fatalf("unexpected key summary: %#v", key)
	}
	if key.Raw["provider"] != antigravitySiteType || key.Raw["project_id"] != "project-a" || key.Raw["subscription_tier"] != "pro" {
		t.Fatalf("unexpected key raw metadata: %#v", key.Raw)
	}
}

func TestAntigravityFetchQuotaPostsProjectAndParsesModels(t *testing.T) {
	t.Parallel()

	var seenPath string
	var seenBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer access-token" || r.Header.Get("User-Agent") != antigravityUserAgent || r.Header.Get("x-client-name") != "antigravity" {
			t.Fatalf("unexpected antigravity quota headers: %#v", r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode quota request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"models": {
				"gemini-3.1-pro": {
					"displayName": "Gemini 3.1 Pro",
					"quotaInfo": {
						"remainingFraction": 0.25,
						"resetTime": "2026-06-22T12:00:00Z"
					}
				}
			}
		}`))
	}))
	defer server.Close()

	snapshot, err := NewAntigravity().fetchQuota(context.Background(), SiteConfig{
		BaseURL: server.URL,
		Client:  server.Client(),
	}, " access-token ", " project-a ")

	if err != nil {
		t.Fatalf("fetchQuota: %v", err)
	}
	if seenPath != "/v1internal:fetchAvailableModels" {
		t.Fatalf("quota path = %q, want fetchAvailableModels", seenPath)
	}
	if seenBody["project"] != "project-a" {
		t.Fatalf("quota request body = %#v, want trimmed project", seenBody)
	}
	if snapshot.ProjectID != "project-a" || len(snapshot.Models) != 1 {
		t.Fatalf("unexpected quota snapshot: %#v", snapshot)
	}
	model := snapshot.Models[0]
	if model.Name != "gemini-3.1-pro" || model.DisplayName != "Gemini 3.1 Pro" || model.RemainingPercent != 25 || model.UsedPercent != 75 {
		t.Fatalf("unexpected quota model: %#v", model)
	}
}

func TestAntigravityLoadCodeAssistPostsMetadataAndParsesProject(t *testing.T) {
	t.Parallel()

	var seenBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1internal:loadCodeAssist" {
			t.Fatalf("path = %q, want loadCodeAssist", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer access-token" || r.Header.Get("x-client-version") != "1.0.0" {
			t.Fatalf("unexpected loadCodeAssist headers: %#v", r.Header)
		}
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode loadCodeAssist request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"cloudaicompanionProject": "project-a",
			"currentTier": {"quotaTier": "free"},
			"paidTier": {"name": "pro"}
		}`))
	}))
	defer server.Close()

	project, err := NewAntigravity().loadCodeAssist(context.Background(), SiteConfig{
		BaseURL: server.URL + "/",
		Client:  server.Client(),
	}, " access-token ")

	if err != nil {
		t.Fatalf("loadCodeAssist: %v", err)
	}
	metadata, ok := seenBody["metadata"].(map[string]any)
	if !ok || metadata["ideType"] != "ANTIGRAVITY" {
		t.Fatalf("loadCodeAssist body = %#v", seenBody)
	}
	if project.ProjectID != "project-a" || project.SubscriptionTier != "pro" {
		t.Fatalf("unexpected project snapshot: %#v", project)
	}
}

func antigravityQuotaByName(models []antigravityModelQuota, name string) *antigravityModelQuota {
	for index := range models {
		if models[index].Name == name {
			return &models[index]
		}
	}
	return nil
}

func adapterModelByName(models []Model, name string) *Model {
	for index := range models {
		if models[index].UpstreamName == name {
			return &models[index]
		}
	}
	return nil
}

func antigravityRawModelByName(models []map[string]any, name string) map[string]any {
	for _, model := range models {
		if model["name"] == name {
			return model
		}
	}
	return nil
}
