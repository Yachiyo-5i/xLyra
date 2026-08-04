package gateway

import (
	"strings"

	routeengine "xlyra/server/internal/router"
)

const codexImageToolHostModel = "gpt-5.4"

func codexResponsesHostModel(payload map[string]any, candidate routeengine.Candidate) string {
	model := strings.TrimSpace(candidate.Model.UpstreamName)
	if !codexPayloadHasImageGenerationTool(payload) {
		return model
	}
	if codexImageModelID(model) {
		return codexImageToolHostModel
	}
	return model
}

func codexPayloadHasImageGenerationTool(payload map[string]any) bool {
	if payload == nil {
		return false
	}
	if codexToolChoiceIsImageGeneration(payload["tool_choice"]) {
		return true
	}
	tools, _ := payload["tools"].([]any)
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		if strings.EqualFold(strings.TrimSpace(anyString(tool["type"])), "image_generation") {
			return true
		}
	}
	return false
}

func isCodexImageGenerationRequest(candidate routeengine.Candidate, request gatewayRequest, payload map[string]any) bool {
	if !isCodexSite(candidate.Site.SiteType) {
		return false
	}
	if isOpenAIImagesEndpoint(request.DownstreamPath) {
		return true
	}
	return codexPayloadHasImageGenerationTool(payload)
}

func codexToolChoiceIsImageGeneration(value any) bool {
	choice, _ := value.(map[string]any)
	return strings.EqualFold(strings.TrimSpace(anyString(choice["type"])), "image_generation")
}

func codexImageModelID(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "gpt-image")
}
