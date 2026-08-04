package modelcapabilities

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeSource struct {
	name   string
	values map[string]any
	ok     bool
	err    error
}

func (f fakeSource) Name() string {
	return f.name
}

func (f fakeSource) Lookup(_ context.Context, _ Input) (map[string]any, bool, error) {
	return f.values, f.ok, f.err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestServiceConstructorsConfigureDefaultSourcesAndPriorities(t *testing.T) {
	t.Parallel()

	service := New()
	if service == nil {
		t.Fatal("New returned nil")
	}
	if len(service.sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(service.sources))
	}
	if _, ok := service.sources[0].(*modelsDevSource); !ok {
		t.Fatalf("first source = %T, want *modelsDevSource", service.sources[0])
	}
	if _, ok := service.sources[1].(curatedSource); !ok {
		t.Fatalf("second source = %T, want curatedSource", service.sources[1])
	}
	if service.priorityFor(sourceModelsDev) != 20 || service.priorityFor(sourceUpstream) != 60 || service.priorityFor(sourceCurated) != 100 {
		t.Fatalf("unexpected default priorities: %#v", service.priorities)
	}

	client := &http.Client{}
	configured := NewWithConfig(Config{
		HTTPClient: client,
		SourcePriority: map[string]int{
			" upstream ": 7,
			"custom":     9,
		},
	})
	modelsDev, ok := configured.sources[0].(*modelsDevSource)
	if !ok {
		t.Fatalf("first configured source = %T, want *modelsDevSource", configured.sources[0])
	}
	if modelsDev.client != client {
		t.Fatalf("models.dev client = %#v, want configured client", modelsDev.client)
	}
	if configured.priorityFor(sourceUpstream) != 7 || configured.priorityFor("custom") != 9 {
		t.Fatalf("configured priorities not applied: %#v", configured.priorities)
	}
	if got := (*Service)(nil).priorityFor(sourceUpstream); got != 0 {
		t.Fatalf("nil service priorityFor = %d, want 0", got)
	}
}

func TestEnrichUsesSourcePriorityAndNormalizesCapabilities(t *testing.T) {
	t.Parallel()

	service := &Service{
		sources: []Source{
			fakeSource{
				name: sourceModelsDev,
				ok:   true,
				values: map[string]any{
					"reasoning":         true,
					"tool_call":         true,
					"structured_output": true,
					"temperature":       false,
					"limit": map[string]any{
						"context": float64(400000),
						"output":  float64(128000),
					},
				},
			},
			fakeSource{
				name: sourceCurated,
				ok:   true,
				values: map[string]any{
					"supported_endpoint_types": []string{"openai-response"},
				},
			},
		},
		priorities: map[string]int{
			sourceModelsDev: 20,
			sourceUpstream:  60,
			sourceCurated:   100,
		},
	}

	result := service.Enrich(context.Background(), Input{
		Provider: "openai",
		ModelID:  "gpt-5-codex",
		BaseCapabilities: map[string]any{
			"tool_call": false,
		},
	})

	if got, _ := result.Capabilities["tool_call"].(bool); got {
		t.Fatalf("expected upstream tool_call=false to override lower-priority models.dev value")
	}
	if got, _ := result.Capabilities["supports_reasoning"].(bool); !got {
		t.Fatalf("expected supports_reasoning=true")
	}
	if got, _ := result.Capabilities["supports_structured_output"].(bool); !got {
		t.Fatalf("expected supports_structured_output=true")
	}
	if got, _ := result.Capabilities["supports_temperature"].(bool); got {
		t.Fatalf("expected supports_temperature=false")
	}
	if got, _ := result.Capabilities["supports_responses"].(bool); !got {
		t.Fatalf("expected supports_responses=true")
	}
	if got, _ := result.Capabilities["supports_chat_completions"].(bool); got {
		t.Fatalf("expected supports_chat_completions=false")
	}
	if got, _ := result.Capabilities["supports_embeddings"].(bool); got {
		t.Fatalf("expected supports_embeddings=false")
	}
	if got, _ := result.Capabilities["context_window"].(int); got != 400000 {
		t.Fatalf("expected context_window=400000, got %v", result.Capabilities["context_window"])
	}
	if got, _ := result.Capabilities["max_output_tokens"].(int); got != 128000 {
		t.Fatalf("expected max_output_tokens=128000, got %v", result.Capabilities["max_output_tokens"])
	}

	metadata, _ := result.Capabilities["_capability_merge"].(map[string]any)
	if metadata == nil {
		t.Fatalf("expected _capability_merge metadata")
	}
}

func TestEnrichRecordsSourceErrorsAndEndpointCapabilityFlags(t *testing.T) {
	t.Parallel()

	service := &Service{
		sources: []Source{
			fakeSource{name: "broken", err: errors.New("upstream unavailable")},
			fakeSource{
				name: sourceCurated,
				ok:   true,
				values: map[string]any{
					"": "ignored",
					"supported_endpoint_types": []string{
						"openai-image",
						"openai-embedding",
						"anthropic-messages",
						"google-gemini",
					},
				},
			},
		},
		priorities: defaultPriorities(),
	}

	result := service.Enrich(context.Background(), Input{})
	if result.Errors["broken"] != "upstream unavailable" {
		t.Fatalf("errors = %#v, want broken source error", result.Errors)
	}
	if _, ok := result.Capabilities[""]; ok {
		t.Fatalf("blank capability key should be skipped: %#v", result.Capabilities)
	}
	for key, want := range map[string]bool{
		"supports_images":             true,
		"supports_embeddings":         true,
		"supports_anthropic_messages": true,
		"supports_google_gemini":      true,
		"supports_chat_completions":   false,
		"supports_responses":          false,
	} {
		if got, _ := result.Capabilities[key].(bool); got != want {
			t.Fatalf("%s = %v, want %v in %#v", key, got, want, result.Capabilities)
		}
	}

	metadata, _ := result.Capabilities["_capability_merge"].(map[string]any)
	errorMap, _ := metadata["errors"].(map[string]any)
	if errorMap["broken"] != "upstream unavailable" {
		t.Fatalf("metadata errors = %#v, want broken source error", errorMap)
	}
}

func TestEnrichSkipsEmptySourcesAndAttachesMetadataForEmptyInput(t *testing.T) {
	t.Parallel()

	service := &Service{
		sources: []Source{
			fakeSource{name: "missing", ok: false, values: map[string]any{"ignored": true}},
			fakeSource{name: "empty", ok: true, values: map[string]any{}},
		},
		priorities: defaultPriorities(),
	}

	result := service.Enrich(context.Background(), Input{})

	if len(result.Applied) != 0 {
		t.Fatalf("applied sources = %#v, want none", result.Applied)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v, want none", result.Errors)
	}
	if _, ok := result.Capabilities["ignored"]; ok {
		t.Fatalf("source with ok=false should not merge values: %#v", result.Capabilities)
	}
	metadata, _ := result.Capabilities["_capability_merge"].(map[string]any)
	if metadata == nil {
		t.Fatalf("expected merge metadata on empty input")
	}
	applied, _ := metadata["applied_sources"].([]string)
	if len(applied) != 0 {
		t.Fatalf("metadata applied_sources = %#v, want empty", applied)
	}
}

func TestNilCapabilityHelpersAreNoops(t *testing.T) {
	t.Parallel()

	normalizeCapabilities(nil)
	attachMergeMetadata(nil, []string{sourceUpstream}, map[string]string{"source": "error"}, defaultPriorities())
}

func TestEnrichClonesInputAndNormalizesEndpointFlags(t *testing.T) {
	t.Parallel()

	base := map[string]any{
		"supported_endpoint_types": []any{"openai-response", " openai ", "openai-response", ""},
		"reasoning":                true,
		"limit": map[string]any{
			"context": int64(200000),
			"output":  int32(32000),
		},
	}
	service := &Service{priorities: defaultPriorities()}

	result := service.Enrich(context.Background(), Input{BaseCapabilities: base})

	endpoints, _ := result.Capabilities["supported_endpoint_types"].([]string)
	if len(endpoints) != 2 || endpoints[0] != "openai" || endpoints[1] != "openai-response" {
		t.Fatalf("supported_endpoint_types = %#v, want sorted unique openai/openai-response", endpoints)
	}
	if got, _ := result.Capabilities["supports_chat_completions"].(bool); !got {
		t.Fatal("expected supports_chat_completions")
	}
	if got, _ := result.Capabilities["supports_responses"].(bool); !got {
		t.Fatal("expected supports_responses")
	}
	if got, _ := result.Capabilities["supports_reasoning"].(bool); !got {
		t.Fatal("expected supports_reasoning")
	}
	if got, _ := result.Capabilities["context_window"].(int); got != 200000 {
		t.Fatalf("context_window = %v, want 200000", result.Capabilities["context_window"])
	}
	if got, _ := result.Capabilities["max_output_tokens"].(int); got != 32000 {
		t.Fatalf("max_output_tokens = %v, want 32000", result.Capabilities["max_output_tokens"])
	}

	if _, ok := base["supports_responses"]; ok {
		t.Fatalf("base capabilities should not be mutated: %#v", base)
	}
}

func TestCuratedSourceLookupAndEndpointCopies(t *testing.T) {
	t.Parallel()

	source := curatedSource{}
	if got := source.Name(); got != sourceCurated {
		t.Fatalf("curated Name = %q, want %q", got, sourceCurated)
	}
	values, ok, err := source.Lookup(context.Background(), Input{ModelID: " GPT-4.1 "})
	if err != nil {
		t.Fatalf("curated Lookup: %v", err)
	}
	if !ok {
		t.Fatal("expected curated source to match gpt-4.1")
	}
	endpoints, _ := values["supported_endpoint_types"].([]string)
	if len(endpoints) != 2 || endpoints[0] != "openai" || endpoints[1] != "openai-response" {
		t.Fatalf("gpt-4.1 endpoints = %#v", endpoints)
	}

	values, ok, err = source.Lookup(context.Background(), Input{Provider: "unknown", ModelID: "unknown-model"})
	if err != nil || ok || values != nil {
		t.Fatalf("unknown curated lookup = values %#v ok %v err %v, want no match", values, ok, err)
	}

	copied := EndpointTypesForModel("gpt-4.1")
	copied[0] = "mutated"
	again := EndpointTypesForModel("gpt-4.1")
	if again[0] != "openai" {
		t.Fatalf("EndpointTypesForModel should return a copy, got %#v after mutation", again)
	}
	if got := EndpointTypesForModel("unknown-model"); got != nil {
		t.Fatalf("unknown EndpointTypesForModel = %#v, want nil", got)
	}
}

func TestOfficialNameUsesModelsDevProviderAliasesAndCachesPayload(t *testing.T) {
	t.Parallel()

	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if req.Header.Get("Accept") != "application/json" {
			t.Fatalf("expected JSON Accept header, got %q", req.Header.Get("Accept"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"zhipuai": {
					"models": {
						"glm-4.5": {"name": "GLM 4.5"}
					}
				}
			}`)),
		}, nil
	})}
	service := &Service{
		sources:    []Source{newModelsDevSource(client)},
		priorities: defaultPriorities(),
	}

	if got := service.OfficialName(context.Background(), " zhipu ", " GLM-4.5 "); got != "GLM 4.5" {
		t.Fatalf("OfficialName = %q, want GLM 4.5", got)
	}
	if got := service.OfficialName(context.Background(), "zhipuai", "missing-model"); got != "" {
		t.Fatalf("missing OfficialName = %q, want empty", got)
	}
	if requests != 1 {
		t.Fatalf("models.dev payload should be cached, requests = %d", requests)
	}
}

func TestModelsDevSourceLookupNormalizesAndClonesCachedPayload(t *testing.T) {
	t.Parallel()

	requests := 0
	source := newModelsDevSource(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"openai": {
					"models": {
						"gpt-5": {
							"name": "GPT-5",
							"supported_endpoint_types": ["openai"]
						}
					}
				}
			}`)),
		}, nil
	})})
	if got := source.Name(); got != sourceModelsDev {
		t.Fatalf("models.dev Name = %q, want %q", got, sourceModelsDev)
	}

	values, ok, err := source.Lookup(context.Background(), Input{Provider: " OpenAI ", ModelID: " GPT-5 "})
	if err != nil {
		t.Fatalf("models.dev Lookup: %v", err)
	}
	if !ok || values["name"] != "GPT-5" {
		t.Fatalf("models.dev lookup = values %#v ok %v, want GPT-5 match", values, ok)
	}
	values["name"] = "mutated"

	values, ok, err = source.Lookup(context.Background(), Input{Provider: "openai", ModelID: "gpt-5"})
	if err != nil || !ok || values["name"] != "GPT-5" {
		t.Fatalf("cached models.dev lookup = values %#v ok %v err %v, want cloned GPT-5 match", values, ok, err)
	}
	if requests != 1 {
		t.Fatalf("models.dev payload should be cached, requests = %d", requests)
	}

	values, ok, err = source.Lookup(context.Background(), Input{Provider: "openai", ModelID: "missing"})
	if err != nil || ok || values != nil {
		t.Fatalf("missing model lookup = values %#v ok %v err %v, want no match", values, ok, err)
	}
	values, ok, err = source.Lookup(context.Background(), Input{Provider: "missing", ModelID: "gpt-5"})
	if err != nil || ok || values != nil {
		t.Fatalf("missing provider lookup = values %#v ok %v err %v, want no match", values, ok, err)
	}
	values, ok, err = source.Lookup(context.Background(), Input{Provider: "", ModelID: "gpt-5"})
	if err != nil || ok || values != nil {
		t.Fatalf("blank provider lookup = values %#v ok %v err %v, want no match", values, ok, err)
	}
}

func TestNormalizeModelsDevProviderAliases(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"xiaomi_mimo": "xiaomi",
		"Xiaomi":      "xiaomi",
		"zhipu":       "zhipuai",
		"Moonshot":    "moonshotai-cn",
		" openai ":    "openai",
	}
	for input, want := range cases {
		if got := normalizeModelsDevProvider(input); got != want {
			t.Fatalf("normalizeModelsDevProvider(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCuratedOpenAIModelAddsEndpointTypes(t *testing.T) {
	t.Parallel()

	values := curatedOpenAIModel("gpt-5-codex")
	if values == nil {
		t.Fatalf("expected curated values for gpt-5-codex")
	}

	items, _ := values["supported_endpoint_types"].([]string)
	if len(items) != 1 || items[0] != "openai-response" {
		t.Fatalf("unexpected supported_endpoint_types: %#v", values["supported_endpoint_types"])
	}

	values = curatedOpenAIModel("codex-auto-review")
	if values == nil {
		t.Fatalf("expected curated values for codex-auto-review")
	}
	items, _ = values["supported_endpoint_types"].([]string)
	if len(items) != 1 || items[0] != "openai-response" {
		t.Fatalf("unexpected codex-auto-review supported_endpoint_types: %#v", values["supported_endpoint_types"])
	}

	values = curatedOpenAIModel("gpt-5.3-codex")
	if values == nil {
		t.Fatalf("expected curated values for gpt-5.3-codex")
	}
	items, _ = values["supported_endpoint_types"].([]string)
	if len(items) != 2 || items[0] != "openai" || items[1] != "openai-response" {
		t.Fatalf("unexpected gpt-5.3-codex supported_endpoint_types: %#v", values["supported_endpoint_types"])
	}

	values = curatedOpenAIModel("gpt-5.5")
	if values == nil {
		t.Fatalf("expected curated values for gpt-5.5")
	}
	items, _ = values["supported_endpoint_types"].([]string)
	if len(items) != 2 || items[0] != "openai" || items[1] != "openai-response" {
		t.Fatalf("unexpected gpt-5.5 supported_endpoint_types: %#v", values["supported_endpoint_types"])
	}

	values = curatedOpenAIModel("text-embedding-3-small")
	if values == nil {
		t.Fatalf("expected curated values for text-embedding-3-small")
	}
	items, _ = values["supported_endpoint_types"].([]string)
	if len(items) != 1 || items[0] != "openai-embedding" {
		t.Fatalf("unexpected text-embedding-3-small supported_endpoint_types: %#v", values["supported_endpoint_types"])
	}

	values = curatedOpenAIModel("text-embedding-ada-002")
	if values == nil {
		t.Fatalf("expected curated values for text-embedding-ada-002")
	}
	items, _ = values["supported_endpoint_types"].([]string)
	if len(items) != 1 || items[0] != "openai-embedding" {
		t.Fatalf("unexpected text-embedding-ada-002 supported_endpoint_types: %#v", values["supported_endpoint_types"])
	}
}

func TestCuratedStandardModelAddsAnthropicEndpointForClaude(t *testing.T) {
	t.Parallel()

	values := curatedStandardModel("claude-3-5-sonnet")
	if values == nil {
		t.Fatalf("expected curated values for claude-3-5-sonnet")
	}
	items, _ := values["supported_endpoint_types"].([]string)
	if len(items) != 1 || items[0] != "anthropic-messages" {
		t.Fatalf("unexpected claude supported_endpoint_types: %#v", values["supported_endpoint_types"])
	}

	endpoints := EndpointTypesForModel("claude-sonnet-4-20250514")
	if len(endpoints) != 1 || endpoints[0] != "anthropic-messages" {
		t.Fatalf("EndpointTypesForModel returned %#v", endpoints)
	}
}

func TestIntFromAnyCoversSupportedNumericTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input any
		want  int
		ok    bool
	}{
		{name: "int", input: 12, want: 12, ok: true},
		{name: "int32", input: int32(13), want: 13, ok: true},
		{name: "int64", input: int64(14), want: 14, ok: true},
		{name: "float64 truncates", input: float64(15.9), want: 15, ok: true},
		{name: "float32 truncates", input: float32(16.9), want: 16, ok: true},
		{name: "string rejected", input: "17", ok: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := intFromAny(tt.input)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("intFromAny(%#v) = (%d, %v), want (%d, %v)", tt.input, got, ok, tt.want, tt.ok)
			}
		})
	}
}
