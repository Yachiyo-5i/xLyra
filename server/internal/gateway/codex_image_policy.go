package gateway

import (
	"strings"

	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
)

type codexImageGenerationIntent int

const (
	codexImageIntentNone codexImageGenerationIntent = iota
	// codexImageIntentModelEndpoint marks a dedicated image request: a gpt-image-* model
	// or the native images generations/edits endpoints. These are served through the
	// upstream's own image pipeline and do not depend on the Responses hosted
	// image_generation tool, so the "strip image tool" config must not gate their routing.
	codexImageIntentModelEndpoint
	codexImageIntentToolAdvertised
	// codexImageIntentExplicit marks a request that depends on the hosted image_generation
	// tool being available upstream (a forced tool_choice or an $imagegen directive). Sites
	// that strip that tool cannot satisfy these requests, so they are excluded during routing.
	codexImageIntentExplicit
)

func codexImageIntentForRequest(request gatewayRequest) codexImageGenerationIntent {
	if request.DownstreamPath == gatewayEndpointImagesGenerations || request.DownstreamPath == gatewayEndpointImagesEdits {
		return codexImageIntentModelEndpoint
	}
	if codexToolChoiceIsImageGeneration(request.Payload["tool_choice"]) {
		return codexImageIntentExplicit
	}
	if codexImageModelID(request.RequestedModel) || codexImageModelID(stringFromPayloadModel(request.Payload)) {
		return codexImageIntentModelEndpoint
	}
	if requestPayloadContainsImagegenDirective(request.Payload) {
		return codexImageIntentExplicit
	}
	if codexPayloadHasImageGenerationTool(request.Payload) {
		return codexImageIntentToolAdvertised
	}
	return codexImageIntentNone
}

func requestForCodexImagePolicy(request gatewayRequest, candidate routeengine.Candidate, intent codexImageGenerationIntent) (gatewayRequest, bool) {
	if isCodexSite(candidate.Site.SiteType) && request.DownstreamPath == gatewayEndpointImagesEdits {
		return request, false
	}
	cfg := &sitepkg.GatewayConfig{
		ResponsesToolPolicy:    candidate.Site.ResponsesToolPolicy,
		DisabledResponsesTools: candidate.Site.DisabledResponsesTools,
	}
	if !sitepkg.ResponsesToolDisabled(cfg, sitepkg.ResponsesHostedToolImageGeneration) {
		return request, true
	}
	if intent == codexImageIntentExplicit {
		return request, false
	}
	if intent == codexImageIntentToolAdvertised {
		return stripImageGenerationToolFromRequest(request), true
	}
	return request, true
}

func stripImageGenerationToolFromRequest(request gatewayRequest) gatewayRequest {
	if !codexPayloadHasImageGenerationTool(request.Payload) {
		return request
	}
	payload := clonePayload(request.Payload)
	if tools, ok := payload["tools"].([]any); ok {
		filtered := make([]any, 0, len(tools))
		for _, item := range tools {
			tool, _ := item.(map[string]any)
			if strings.EqualFold(strings.TrimSpace(anyString(tool["type"])), "image_generation") {
				continue
			}
			filtered = append(filtered, item)
		}
		if len(filtered) == 0 {
			delete(payload, "tools")
		} else {
			payload["tools"] = filtered
		}
	}
	if codexToolChoiceIsImageGeneration(payload["tool_choice"]) {
		payload["tool_choice"] = "auto"
	}
	request.Payload = payload
	switch request.DownstreamPath {
	case gatewayEndpointResponses:
		if canonical, err := canonicalRequestFromOpenAIResponsesPayload(payload, request.RequestedModel); err == nil {
			request.Canonical = &canonical
		}
	case gatewayEndpointChatCompletions:
		if canonical, err := canonicalRequestFromOpenAIChatPayload(payload, request.RequestedModel); err == nil {
			request.Canonical = &canonical
		}
	case gatewayEndpointMessages:
		if canonical, err := canonicalRequestFromAnthropicMessagesPayload(payload, request.RequestedModel); err == nil {
			request.Canonical = &canonical
		}
	}
	return request
}

func requestPayloadContainsImagegenDirective(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(strings.ToLower(typed), "$imagegen")
	case map[string]any:
		for _, child := range typed {
			if requestPayloadContainsImagegenDirective(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if requestPayloadContainsImagegenDirective(child) {
				return true
			}
		}
	}
	return false
}
