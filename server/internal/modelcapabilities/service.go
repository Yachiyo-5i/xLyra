package modelcapabilities

import (
	"context"
	"net/http"
	"sort"
	"strings"
)

const (
	sourceUpstream  = "upstream"
	sourceModelsDev = "models_dev"
	sourceCurated   = "curated"
)

type Input struct {
	Provider         string
	SiteType         string
	ModelID          string
	BaseCapabilities map[string]any
}

type Result struct {
	Capabilities map[string]any
	Applied      []string
	Errors       map[string]string
}

type Source interface {
	Name() string
	Lookup(ctx context.Context, input Input) (map[string]any, bool, error)
}

type Config struct {
	SourcePriority map[string]int
	HTTPClient     *http.Client
}

type Service struct {
	sources    []Source
	priorities map[string]int
}

func New() *Service {
	return NewWithConfig(Config{})
}

func NewWithConfig(cfg Config) *Service {
	priorities := defaultPriorities()
	for key, value := range cfg.SourcePriority {
		priorities[strings.TrimSpace(key)] = value
	}

	return &Service{
		sources: []Source{
			newModelsDevSource(cfg.HTTPClient),
			curatedSource{},
		},
		priorities: priorities,
	}
}

func defaultPriorities() map[string]int {
	return map[string]int{
		sourceModelsDev: 20,
		sourceUpstream:  60,
		sourceCurated:   100,
	}
}

func (s *Service) Enrich(ctx context.Context, input Input) Result {
	merged := cloneMap(input.BaseCapabilities)
	bestPriority := map[string]int{}
	applied := []string{}
	errorsBySource := map[string]string{}

	if len(merged) > 0 {
		applied = append(applied, sourceUpstream)
		basePriority := s.priorityFor(sourceUpstream)
		for key := range merged {
			bestPriority[key] = basePriority
		}
	}

	for _, source := range s.sources {
		values, ok, err := source.Lookup(ctx, input)
		if err != nil {
			errorsBySource[source.Name()] = err.Error()
			continue
		}
		if !ok || len(values) == 0 {
			continue
		}

		applied = appendUniqueString(applied, source.Name())
		priority := s.priorityFor(source.Name())
		for key, value := range values {
			if key == "" {
				continue
			}
			currentPriority, exists := bestPriority[key]
			if !exists || priority >= currentPriority {
				merged[key] = cloneAny(value)
				bestPriority[key] = priority
			}
		}
	}

	normalizeCapabilities(merged)
	attachMergeMetadata(merged, applied, errorsBySource, s.priorities)

	return Result{
		Capabilities: merged,
		Applied:      applied,
		Errors:       errorsBySource,
	}
}

func (s *Service) OfficialName(ctx context.Context, provider string, modelID string) string {
	provider = normalizeModelsDevProvider(provider)
	if provider == "" {
		return ""
	}
	for _, source := range s.sources {
		if md, ok := source.(*modelsDevSource); ok {
			if name, found := md.LookupName(ctx, provider, modelID); found {
				return name
			}
		}
	}
	return ""
}

func normalizeModelsDevProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "xiaomi_mimo", "xiaomi":
		return "xiaomi"
	case "zhipu":
		return "zhipuai"
	case "moonshot":
		return "moonshotai-cn"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func (s *Service) priorityFor(source string) int {
	if s == nil {
		return 0
	}
	return s.priorities[strings.TrimSpace(source)]
}

func normalizeCapabilities(values map[string]any) {
	if values == nil {
		return
	}

	endpointTypes := normalizeStringSlice(values["supported_endpoint_types"])
	if len(endpointTypes) > 0 {
		values["supported_endpoint_types"] = endpointTypes
		values["supports_chat_completions"] = stringSliceContains(endpointTypes, "openai")
		values["supports_responses"] = stringSliceContains(endpointTypes, "openai-response")
		values["supports_images"] = stringSliceContains(endpointTypes, "openai-image")
		values["supports_embeddings"] = stringSliceContains(endpointTypes, "openai-embedding")
		values["supports_audio_speech"] = stringSliceContains(endpointTypes, "openai-audio-speech")
		values["supports_anthropic_messages"] = stringSliceContains(endpointTypes, "anthropic-messages")
		values["supports_google_gemini"] = stringSliceContains(endpointTypes, "google-gemini")
	}

	if value, ok := values["reasoning"].(bool); ok {
		values["supports_reasoning"] = value
	}
	if value, ok := values["tool_call"].(bool); ok {
		values["supports_tools"] = value
	}
	if value, ok := values["structured_output"].(bool); ok {
		values["supports_structured_output"] = value
	}
	if value, ok := values["temperature"].(bool); ok {
		values["supports_temperature"] = value
	}

	if limit, ok := values["limit"].(map[string]any); ok {
		if contextWindow, ok := intFromAny(limit["context"]); ok {
			values["context_window"] = contextWindow
		}
		if maxOutput, ok := intFromAny(limit["output"]); ok {
			values["max_output_tokens"] = maxOutput
		}
	}
}

func attachMergeMetadata(values map[string]any, applied []string, errorsBySource map[string]string, priorities map[string]int) {
	if values == nil {
		return
	}

	sourcePriority := map[string]any{}
	for key, value := range priorities {
		sourcePriority[key] = value
	}

	metadata := map[string]any{
		"applied_sources": applied,
		"priority":        sourcePriority,
	}
	if len(errorsBySource) > 0 {
		errorMap := map[string]any{}
		for key, value := range errorsBySource {
			errorMap[key] = value
		}
		metadata["errors"] = errorMap
	}
	values["_capability_merge"] = metadata
}

func normalizeStringSlice(value any) []string {
	result := []string{}
	switch items := value.(type) {
	case []string:
		for _, item := range items {
			result = appendUniqueString(result, strings.TrimSpace(item))
		}
	case []any:
		for _, item := range items {
			text, _ := item.(string)
			result = appendUniqueString(result, strings.TrimSpace(text))
		}
	}
	sort.Strings(result)
	return result
}

func stringSliceContains(values []string, target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	for _, item := range values {
		if strings.TrimSpace(strings.ToLower(item)) == target {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = cloneAny(value)
	}
	return cloned
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		cloned := make([]any, 0, len(typed))
		for _, item := range typed {
			cloned = append(cloned, cloneAny(item))
		}
		return cloned
	case []string:
		cloned := make([]string, 0, len(typed))
		cloned = append(cloned, typed...)
		return cloned
	default:
		return typed
	}
}

func intFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	default:
		return 0, false
	}
}
