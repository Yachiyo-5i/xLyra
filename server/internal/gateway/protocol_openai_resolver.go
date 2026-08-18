package gateway

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

const (
	upstreamEndpointTypeOpenAI            = "openai"
	upstreamEndpointTypeOpenAIResponse    = "openai-response"
	upstreamEndpointTypeOpenAIImage       = "openai-image"
	upstreamEndpointTypeOpenAIEmbedding   = "openai-embedding"
	upstreamEndpointTypeOpenAIAudioSpeech = "openai-audio-speech"
	upstreamEndpointTypeGoogleGemini      = "google-gemini"
	upstreamEndpointTypeAnthropicMessages = "anthropic-messages"
)

func isAnthropicSite(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), "anthropic")
}

type openAIProtocolResolver struct {
	db *store.Store
}

func (r openAIProtocolResolver) Resolve(ctx context.Context, request gatewayRequest, candidate routeengine.Candidate) (gatewayProtocolAdapter, error) {
	if request.DownstreamPath == gatewayEndpointEmbeddings {
		return newOpenAIEmbeddingsProtocolAdapter(request, candidate), nil
	}

	if request.DownstreamPath == gatewayEndpointAudioSpeech && isMiMoV25TTSModel(candidate.Model.UpstreamName) {
		return newMiMoAudioSpeechProtocolAdapter(request, candidate), nil
	}
	if request.DownstreamPath == gatewayEndpointAudioSpeech {
		return newOpenAIAudioSpeechProtocolAdapter(request, candidate), nil
	}

	if isOpenAIImagesEndpoint(request.DownstreamPath) {
		if isCodexSite(candidate.Site.SiteType) {
			return newCodexProtocolAdapter(request), nil
		}
		if isAntigravitySite(candidate.Site.SiteType) {
			return newAntigravityProtocolAdapter(request, r.db), nil
		}
		if isGoogleSite(candidate.Site.SiteType) {
			return newGoogleProtocolAdapter(request), nil
		}
		if isGrokSite(candidate.Site.SiteType) {
			return newGrokImagesProtocolAdapter(request, candidate), nil
		}
		return newOpenAIImagesProtocolAdapter(request, candidate), nil
	}

	if isCodexSite(candidate.Site.SiteType) {
		return newCodexProtocolAdapter(request), nil
	}
	if isAntigravitySite(candidate.Site.SiteType) {
		return newAntigravityProtocolAdapter(request, r.db), nil
	}
	if isGoogleSite(candidate.Site.SiteType) {
		return newGoogleProtocolAdapter(request), nil
	}
	if isAnthropicSite(candidate.Site.SiteType) || isClaudeCodeSite(candidate.Site.SiteType) {
		return newAnthropicMessagesProtocolAdapter(downstreamCanonicalProtocol(request.DownstreamPath)), nil
	}
	if isGrokSite(candidate.Site.SiteType) {
		return newGrokResponsesProtocolAdapterWithReasoningEfforts(request, r.grokModelReasoningEfforts(ctx, candidate.Model.SiteModelID)), nil
	}
	if isMiMoV25TTSModel(candidate.Model.UpstreamName) && request.DownstreamPath == gatewayEndpointChatCompletions {
		return newOpenAIChatProtocolAdapter(request, candidate), nil
	}
	if alt, ok := alternateProtocolForCandidate(downstreamCanonicalProtocol(request.DownstreamPath), candidate); ok {
		switch canonicalProtocol(normalizeSpecKey(alt.Protocol)) {
		case canonicalProtocolAnthropicMessages:
			return newProviderAnthropicMessagesProtocolAdapter(providerNameForCandidate(candidate), alt, downstreamCanonicalProtocol(request.DownstreamPath)), nil
		}
	}

	endpointTypes, err := r.supportedEndpointTypes(ctx, candidate.Model.SiteModelID)
	if err != nil {
		return nil, err
	}

	if containsEndpointType(endpointTypes, upstreamEndpointTypeGoogleGemini) {
		return newGoogleProtocolAdapter(request), nil
	}

	if containsEndpointType(endpointTypes, upstreamEndpointTypeAnthropicMessages) {
		return anthropicMessagesProtocolForCandidate(request, candidate), nil
	}

	if shouldUseOpenAIResponses(request, candidate, endpointTypes) {
		return newOpenAIResponsesProtocolAdapter(request), nil
	}
	return newOpenAIChatProtocolAdapter(request, candidate), nil
}

func anthropicMessagesProtocolForCandidate(request gatewayRequest, candidate routeengine.Candidate) gatewayProtocolAdapter {
	downstream := downstreamCanonicalProtocol(request.DownstreamPath)
	if alt, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, candidate); ok {
		if canonicalProtocol(normalizeSpecKey(alt.Protocol)) == canonicalProtocolAnthropicMessages {
			return newProviderAnthropicMessagesProtocolAdapter(providerNameForCandidate(candidate), alt, downstream)
		}
	}
	return newAnthropicMessagesProtocolAdapter(downstream)
}

func providerNameForCandidate(candidate routeengine.Candidate) string {
	if spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, candidate); strings.TrimSpace(spec.Provider) != "" {
		return normalizeSpecKey(spec.Provider)
	}
	return normalizeSpecKey(candidate.Site.SiteType)
}

func (r openAIProtocolResolver) supportedEndpointTypes(ctx context.Context, siteModelID uuid.UUID) ([]string, error) {
	if r.db == nil || siteModelID == uuid.Nil {
		return nil, nil
	}
	capabilityValues, err := r.supportedEndpointTypesFromCapabilities(ctx, siteModelID)
	if err != nil {
		return nil, err
	}

	combined := map[string]struct{}{}
	for _, value := range capabilityValues {
		text := normalizeEndpointType(value)
		if text != "" {
			combined[text] = struct{}{}
		}
	}

	result := make([]string, 0, len(combined))
	for value := range combined {
		result = append(result, value)
	}
	return result, nil
}

func (r openAIProtocolResolver) grokModelReasoningEfforts(ctx context.Context, siteModelID uuid.UUID) []string {
	if r.db == nil || siteModelID == uuid.Nil {
		return nil
	}
	model, err := store.NewSiteModelRepository(r.db.DB()).GetByID(ctx, siteModelID)
	if err != nil || len(model.Capabilities) == 0 {
		return nil
	}
	capabilities := map[string]any{}
	if err := json.Unmarshal(model.Capabilities, &capabilities); err != nil {
		return nil
	}
	raw, ok := capabilities["raw"].(map[string]any)
	if !ok {
		return nil
	}
	supported, _ := raw["supports_reasoning_effort"].(bool)
	if !supported {
		return nil
	}
	result := make([]string, 0)
	if efforts, ok := raw["reasoning_efforts"].([]any); ok {
		for _, rawEffort := range efforts {
			value := anyString(rawEffort)
			if effort, ok := rawEffort.(map[string]any); ok {
				value = firstNonEmptyGatewayString(anyString(effort["value"]), anyString(effort["id"]))
			}
			if value != "" {
				result = append(result, value)
			}
		}
	}
	if len(result) == 0 {
		return []string{"low", "medium", "high"}
	}
	return result
}

func (r openAIProtocolResolver) supportedEndpointTypesFromCapabilities(ctx context.Context, siteModelID uuid.UUID) ([]string, error) {
	model, err := store.NewSiteModelRepository(r.db.DB()).GetByID(ctx, siteModelID)
	if err != nil {
		return nil, err
	}

	capabilities := map[string]any{}
	if len(model.Capabilities) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(model.Capabilities, &capabilities); err != nil {
		return nil, err
	}

	raw, ok := capabilities["supported_endpoint_types"]
	if !ok {
		return nil, nil
	}

	values := []string{}
	switch items := raw.(type) {
	case []string:
		values = append(values, items...)
	case []any:
		for _, item := range items {
			text, _ := item.(string)
			values = append(values, text)
		}
	}
	return values, nil
}

func shouldUseOpenAIResponses(request gatewayRequest, candidate routeengine.Candidate, endpointTypes []string) bool {
	supportsResponses := false
	supportsChat := false
	for _, item := range endpointTypes {
		switch strings.TrimSpace(strings.ToLower(item)) {
		case upstreamEndpointTypeOpenAIResponse:
			supportsResponses = true
		case upstreamEndpointTypeOpenAI:
			supportsChat = true
		}
	}
	if !supportsResponses {
		return false
	}

	// Responses-only models must route through /v1/responses.
	if !supportsChat {
		return true
	}

	// Codex family is documented as Responses-oriented / Responses-only.
	model := strings.ToLower(strings.TrimSpace(candidate.Model.UpstreamName))
	if strings.Contains(model, "codex") {
		return true
	}

	// Responses and Messages downstream endpoints prefer Responses upstream when available.
	return request.DownstreamPath == gatewayEndpointResponses || request.DownstreamPath == gatewayEndpointMessages
}

func normalizeEndpointType(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func containsEndpointType(types []string, target string) bool {
	target = normalizeEndpointType(target)
	for _, item := range types {
		if normalizeEndpointType(item) == target {
			return true
		}
	}
	return false
}

func downstreamCanonicalProtocol(path string) canonicalProtocol {
	switch strings.TrimSpace(path) {
	case gatewayEndpointResponses:
		return canonicalProtocolOpenAIResponses
	case gatewayEndpointMessages:
		return canonicalProtocolAnthropicMessages
	case gatewayEndpointImagesGenerations, gatewayEndpointImagesEdits:
		return canonicalProtocolOpenAIImages
	default:
		return canonicalProtocolOpenAIChat
	}
}

func isOpenAIImagesEndpoint(path string) bool {
	switch strings.TrimSpace(path) {
	case gatewayEndpointImagesGenerations, gatewayEndpointImagesEdits:
		return true
	default:
		return false
	}
}
