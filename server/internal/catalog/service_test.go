package catalog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

type catalogRoundTripFunc func(*http.Request) (*http.Response, error)

func (f catalogRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNormalizeModelKey(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		" GPT_4o mini ":                 "gpt-4o-mini",
		"deepseek-v3.1":                 "deepseek-v3.1",
		"anthropic/Claude 3.5 Sonnet":   "claude-3.5-sonnet",
		"google/gemini-2.5-pro":         "gemini-2.5-pro",
		"gemini/gemini-2.5-pro":         "gemini-2.5-pro",
		"xai/Grok 4 Image+":             "grok-4-image-plus",
		"models/text-embedding-3-large": "text-embedding-3-large",
		"model/openai/gpt.5++preview":   "gpt.5-plus-plus-preview",
		"gpt-5.4":                       "gpt-5.4",
		"gpt-5.3-code":                  "gpt-5.3-code",
	}

	for input, want := range cases {
		if got := NormalizeModelKey(input); got != want {
			t.Fatalf("NormalizeModelKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCanonicalModelKeyFromUpstream(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"0.2-官方-兼容-gpt-4o-mini":                      "gpt-4o-mini",
		"1.5-流式-1M上下文-Gemini-2.5-Pro-思考":             "gemini-2.5-pro",
		"0.6-兼容-急速-Qwen3-235b":                       "qwen3-235b",
		"20-官方图生视频-流式-veo_3_1-横屏":                    "veo-3-1",
		"0.1-官方-嵌入-text-embedding-3-small-inference": "text-embedding-3-small",
		"官方-1-兼容-gemini-2.5-pro-Business":            "gemini-2.5-pro",
		"route-openai-gpt-4o-search":                 "gpt-4o",
		"route-openai-gpt-4o-business-search":        "gpt-4o",
		"0.5-兼容-官方-grok-4-image生图":                   "grok-4-image",
		"0.2-兼容-模型尝鲜-hermes":                         "hermes",
		"plain-custom-model":                         "plain-custom-model",
		"gpt-5.4":                                    "gpt-5.4",
		"gpt-5.3-code":                               "gpt-5.3-code",
	}

	for input, want := range cases {
		if got := CanonicalModelKeyFromUpstream(input); got != want {
			t.Fatalf("CanonicalModelKeyFromUpstream(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInferProvider(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"gpt-4o-mini":                          "openai",
		"o3-mini":                              "openai",
		"dall-e-3":                             "openai",
		"sora-2-official":                      "openai",
		"claude-3-5-sonnet":                    "anthropic",
		"gemini-2.5-pro":                       "google",
		"gemma-3-27b":                          "google",
		"nano-banana-pro":                      "google",
		"veo3.1-fast":                          "google",
		"deepseek-v3.1":                        "deepseek",
		"grok-4":                               "xai",
		"grok":                                 "xai",
		"qwen3-235b":                           "qwen",
		"qwen3.7-max":                          "qwen",
		"glm-4.5":                              "zhipuai",
		"kimi-k2":                              "moonshotai-cn",
		"moonshot-v1":                          "moonshotai-cn",
		"moonshotai-kimi-k2.6":                 "moonshotai-cn",
		"mimo-vl":                              "xiaomi",
		"minimax-m1":                           "minimax",
		"k3-256k":                              "moonshotai-cn",
		"bytedance-seed-seed-oss-36b-instruct": "bytedance",
		"viduq3-fast":                          "vidu",
		"step-3.7-flash":                       "stepfun",
		"stepaudio-2.5-asr":                    "stepfun",
		"wan2.6-flash":                         "alibaba",
		"tongyi-mai-z-image":                   "alibaba",
		"kling-v3":                             "kuaishou",
		"kwai-kolors-kolors":                   "kuaishou",
		"bge-m3":                               "baai",
		"flux-kontext-pro":                     "flux",
		"happyhorse-1.1":                       "alibaba",
		"hy3":                                  "hunyuan",
		"hy3-preview":                          "hunyuan",
		"tencent-hunyuan-a13b-instruct":        "hunyuan",
		"sensenova-6.7-flash-lite":             "sensenova",
		"sensenova-u1-fast":                    "sensenova",
		"perplexity-sonar":                     "perplexity",
		"unknown-model":                        "unknown",
	}

	for input, want := range cases {
		if got := InferProvider(input); got != want {
			t.Fatalf("InferProvider(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInferCategory(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"text-embedding-3-large": "embedding",
		"bge-reranker-v2":        "rerank",
		"whisper-1":              "audio",
		"dall-e-3":               "image",
		"flux-kontext-pro":       "image",
		"nano_banana_2":          "image",
		"nano-banana-pro":        "image",
		"sora-2":                 "video",
		"veo-3.1":                "video",
		"vendor-video-model":     "video",
		"gpt-4o-mini":            "chat",
	}

	for input, want := range cases {
		if got := InferCategory(input); got != want {
			t.Fatalf("InferCategory(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCategoryFromSiteModelProtocols(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		modelKey string
		models   []store.SiteModel
		want     string
	}{
		{
			name:     "image protocol overrides stale canonical category",
			modelKey: "nano-banana-pro",
			models: []store.SiteModel{{
				Capabilities: store.JSON(`{"supported_endpoint_types":["openai-image"]}`),
			}},
			want: "image",
		},
		{
			name:     "embedding protocol has dedicated category",
			modelKey: "custom-model",
			models: []store.SiteModel{{
				Capabilities: store.JSON(`{"supported_endpoint_types":["openai-embedding"]}`),
			}},
			want: "embedding",
		},
		{
			name:     "audio protocol has dedicated category",
			modelKey: "custom-model",
			models: []store.SiteModel{{
				Capabilities: store.JSON(`{"supported_endpoint_types":["openai-audio-speech"]}`),
			}},
			want: "audio",
		},
		{
			name:     "fallback keeps model name classification",
			modelKey: "nano-banana-pro",
			models:   []store.SiteModel{{Capabilities: store.JSON(`{"supported_endpoint_types":["openai"]}`)}},
			want:     "image",
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := categoryFromSiteModelProtocols(tt.modelKey, tt.models); got != tt.want {
				t.Fatalf("categoryFromSiteModelProtocols() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferEndpointTypesUsesStandardModelRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		provider string
		modelKey string
		category string
		want     []string
	}{
		{
			name:     "claude on openai compatible",
			provider: "openai",
			modelKey: "claude-3-5-sonnet",
			category: "chat",
			want:     []string{"anthropic-messages"},
		},
		{
			name:     "gpt 5.3 codex dual stack",
			provider: "openai",
			modelKey: "gpt-5.3-codex",
			category: "chat",
			want:     []string{"openai", "openai-response"},
		},
		{
			name:     "embedding",
			provider: "openai",
			modelKey: "text-embedding-3-small",
			category: "embedding",
			want:     []string{"openai-embedding"},
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := inferEndpointTypes(tt.provider, tt.modelKey, tt.category)
			if len(got) != len(tt.want) {
				t.Fatalf("inferEndpointTypes returned %#v, want %#v", got, tt.want)
			}
			for index := range got {
				if got[index] != tt.want[index] {
					t.Fatalf("inferEndpointTypes returned %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}

func TestInferEndpointTypesFallsBackByCategoryProviderAndModelName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		provider string
		modelKey string
		category string
		want     []string
	}{
		{
			name:     "image category",
			provider: "custom",
			modelKey: "vendor-art-model",
			category: "image",
			want:     []string{"openai-image"},
		},
		{
			name:     "embedding category",
			provider: "custom",
			modelKey: "vendor-vector-model",
			category: "embedding",
			want:     []string{"openai"},
		},
		{
			name:     "audio category",
			provider: "custom",
			modelKey: "vendor-speech-model",
			category: "audio",
			want:     []string{"openai"},
		},
		{
			name:     "codex name",
			provider: "custom",
			modelKey: "vendor-codex-runner",
			category: "chat",
			want:     []string{"openai-response"},
		},
		{
			name:     "anthropic provider",
			provider: "anthropic",
			modelKey: "vendor-chat-model",
			category: "chat",
			want:     []string{"anthropic-messages"},
		},
		{
			name:     "default chat",
			provider: "custom",
			modelKey: "vendor-chat-model",
			category: "chat",
			want:     []string{"openai"},
		},
	}

	for _, tt := range cases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertStringSliceEqual(t, inferEndpointTypes(tt.provider, tt.modelKey, tt.category), tt.want)
		})
	}
}

func TestCanonicalModelParamsInfersDefaultsFromModelKey(t *testing.T) {
	t.Parallel()

	params, err := canonicalModelParams(UpsertCanonicalModelInput{
		ModelKey: "openai/GPT_4o mini",
	})
	if err != nil {
		t.Fatalf("canonicalModelParams: %v", err)
	}

	if params.ModelKey != "gpt-4o-mini" {
		t.Fatalf("ModelKey = %q, want gpt-4o-mini", params.ModelKey)
	}
	if params.DisplayName != "gpt-4o-mini" {
		t.Fatalf("DisplayName = %q, want gpt-4o-mini", params.DisplayName)
	}
	if params.Provider != "openai" {
		t.Fatalf("Provider = %q, want openai", params.Provider)
	}
	if params.Category != "chat" {
		t.Fatalf("Category = %q, want chat", params.Category)
	}
	if params.Status != "active" {
		t.Fatalf("Status = %q, want active", params.Status)
	}
	var capabilities map[string]any
	if err := json.Unmarshal(params.Capabilities, &capabilities); err != nil {
		t.Fatalf("capabilities should be JSON: %v", err)
	}
	if len(capabilities) != 0 {
		t.Fatalf("default capabilities should be empty object, got %#v", capabilities)
	}
}

func TestCanonicalModelParamsPreservesExplicitFieldsAndCapabilities(t *testing.T) {
	t.Parallel()

	params, err := canonicalModelParams(UpsertCanonicalModelInput{
		ModelKey:    "vendor-model",
		DisplayName: "Vendor Model",
		Provider:    "vendor",
		Category:    "embedding",
		Status:      "disabled",
		Capabilities: map[string]any{
			"manual": true,
			"tags":   []string{"private"},
		},
	})
	if err != nil {
		t.Fatalf("canonicalModelParams: %v", err)
	}

	if params.ModelKey != "vendor-model" || params.DisplayName != "Vendor Model" || params.Provider != "vendor" || params.Category != "embedding" || params.Status != "disabled" {
		t.Fatalf("unexpected params: %#v", params)
	}
	var capabilities map[string]any
	if err := json.Unmarshal(params.Capabilities, &capabilities); err != nil {
		t.Fatalf("capabilities should be JSON: %v", err)
	}
	if capabilities["manual"] != true {
		t.Fatalf("manual capability missing: %#v", capabilities)
	}
}

func TestCanonicalModelParamsRejectsMissingModelKeyAndUnmarshalableCapabilities(t *testing.T) {
	t.Parallel()

	if _, err := canonicalModelParams(UpsertCanonicalModelInput{ModelKey: "   "}); err == nil {
		t.Fatal("expected empty model key error")
	}

	_, err := canonicalModelParams(UpsertCanonicalModelInput{
		ModelKey: "gpt-4o",
		Capabilities: map[string]any{
			"bad": func() {},
		},
	})
	assertCatalogErrorContains(t, "canonicalModelParams capabilities", err, "marshal canonical model capabilities")
}

func TestDisplayNameFromModelPrefersExplicitDisplayName(t *testing.T) {
	t.Parallel()

	if got := displayNameFromModel(store.SiteModel{DisplayName: " GPT 4o ", UpstreamName: "upstream"}); got != "GPT 4o" {
		t.Fatalf("displayNameFromModel explicit = %q, want GPT 4o", got)
	}
	if got := displayNameFromModel(store.SiteModel{UpstreamName: " upstream "}); got != "upstream" {
		t.Fatalf("displayNameFromModel upstream = %q, want upstream", got)
	}
}

func TestArchiveValidationErrorsAvoidRepositoryCalls(t *testing.T) {
	t.Parallel()

	service := &Service{}
	ctx := context.Background()

	if _, err := service.Archive(ctx, uuid.Nil); err == nil {
		t.Fatal("expected missing archive id error")
	}
}

func TestSyncHelpersExtractNumericValues(t *testing.T) {
	t.Parallel()

	cost := map[string]any{
		"input":       float64(2.5),
		"output":      float32(3.5),
		"cache_read":  int(1),
		"cache_write": int64(2),
		"free":        float64(0),
		"bad":         "1.2",
	}

	assertCatalogFloat(t, cost, "input", 2.5)
	assertCatalogFloat(t, cost, "output", float64(float32(3.5)))
	assertCatalogFloat(t, cost, "cache_read", 1)
	assertCatalogFloat(t, cost, "cache_write", 2)
	assertCatalogFloat(t, cost, "bad", 0)
	assertCatalogFloat(t, nil, "input", 0)

	assertCatalogCost(t, cost, "input", 2.5, true)
	assertCatalogCost(t, cost, "free", 0, false)
	assertCatalogCost(t, nil, "input", 0, false)

	limits := map[string]any{
		"context": float64(128000),
		"output":  float32(4096),
		"batch":   int(12),
		"tokens":  int64(64),
		"bad":     "4096",
	}
	assertCatalogInt(t, limits, "context", 128000, true)
	assertCatalogInt(t, limits, "output", 4096, true)
	assertCatalogInt(t, limits, "batch", 12, true)
	assertCatalogInt(t, limits, "tokens", 64, true)
	assertCatalogInt(t, limits, "bad", 0, false)
	assertCatalogInt(t, nil, "context", 0, false)
}

func TestFetchCatalogUsesModelsDevHeadersAndDecodesPayload(t *testing.T) {
	t.Parallel()

	requests := 0
	service := &SyncService{client: &http.Client{Transport: catalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.String() != modelsDevSyncURL {
			t.Fatalf("url = %s, want %s", req.URL.String(), modelsDevSyncURL)
		}
		if req.Header.Get("Accept") != "application/json" {
			t.Fatalf("Accept = %q, want application/json", req.Header.Get("Accept"))
		}
		if req.Header.Get("User-Agent") != "xLyra/1.0" {
			t.Fatalf("User-Agent = %q, want xLyra/1.0", req.Header.Get("User-Agent"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"openai": {
					"models": {
						"gpt-4o": {
							"id": "gpt-4o",
							"name": "GPT-4o",
							"cost": {"input": 2.5},
							"modalities": {"input": ["text"], "output": ["text"]},
							"limit": {"context": 128000}
						}
					}
				}
			}`)),
		}, nil
	})}}

	catalog, err := service.fetchCatalog(context.Background())
	if err != nil {
		t.Fatalf("fetchCatalog: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	model := catalog["openai"].Models["gpt-4o"]
	if model.ID != "gpt-4o" || model.Name != "GPT-4o" {
		t.Fatalf("decoded model = %#v", model)
	}
	if got := extractFloat(model.Cost, "input"); got != 2.5 {
		t.Fatalf("decoded cost input = %v, want 2.5", got)
	}
}

func TestFetchCatalogReportsHTTPAndDecodeErrors(t *testing.T) {
	t.Parallel()

	statusService := &SyncService{client: &http.Client{Transport: catalogRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`bad gateway`)),
		}, nil
	})}}
	_, err := statusService.fetchCatalog(context.Background())
	assertCatalogErrorContains(t, "fetchCatalog status", err, "models.dev returned 502")

	decodeService := &SyncService{client: &http.Client{Transport: catalogRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{`)),
		}, nil
	})}}
	if _, err := decodeService.fetchCatalog(context.Background()); err == nil {
		t.Fatal("expected decode error")
	}
}

func assertStringSliceEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}
