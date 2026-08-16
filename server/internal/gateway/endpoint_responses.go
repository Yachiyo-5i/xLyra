package gateway

import (
	"net/http"
	"strings"

	"xlyra/server/internal/httpx"
)

type responsesEndpointAdapter struct{}

func (responsesEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointResponses
}

func (responsesEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeOpenAIResponse
}

func (responsesEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
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
	if failure := normalizeClientRequestOptions(payload); failure != nil {
		failure.requestedModel = model
		failure.stream = stream
		return gatewayRequest{}, failure
	}

	request := gatewayRequest{
		DownstreamPath:    gatewayEndpointResponses,
		DownstreamHeaders: r.Header.Clone(),
		RequestedModel:    model,
		Stream:            stream,
		Payload:           payload,
	}
	if canonical, err := canonicalRequestFromOpenAIResponsesPayload(payload, model); err == nil {
		request.Canonical = &canonical
	}
	return request, nil
}
