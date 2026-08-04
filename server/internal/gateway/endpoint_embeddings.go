package gateway

import (
	"net/http"
	"strings"

	"xlyra/server/internal/httpx"
)

type embeddingsEndpointAdapter struct{}

func (embeddingsEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointEmbeddings
}

func (embeddingsEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeOpenAIEmbedding
}

func (embeddingsEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
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

	return gatewayRequest{
		DownstreamPath: gatewayEndpointEmbeddings,
		RequestedModel: model,
		Stream:         false,
		Payload:        payload,
	}, nil
}
