package gateway

import (
	"net/http"
	"strings"

	"xlyra/server/internal/httpx"
)

type chatCompletionsEndpointAdapter struct{}

func (chatCompletionsEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointChatCompletions
}

func (chatCompletionsEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeOpenAI
}

func (chatCompletionsEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
	var payload map[string]any
	if err := httpx.DecodeJSONBody(r, &payload); err != nil {
		return gatewayRequest{}, decodeRequestFailure(err)
	}

	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return gatewayRequest{}, &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_model",
			message: "model is required",
			stage:   "validate",
		}
	}

	stream, _ := payload["stream"].(bool)
	request := gatewayRequest{
		DownstreamPath:    gatewayEndpointChatCompletions,
		DownstreamHeaders: r.Header.Clone(),
		RequestedModel:    model,
		Stream:            stream,
		Payload:           payload,
	}
	if canonical, err := canonicalRequestFromOpenAIChatPayload(payload, model); err == nil {
		request.Canonical = &canonical
	}
	return request, nil
}
