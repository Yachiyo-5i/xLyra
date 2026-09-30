package gateway

import (
	"net/http"
	"strings"

	"xlyra/server/internal/httpx"
)

type typeSafeSystemOneEndpointAdapter struct{}

func (typeSafeSystemOneEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointTypeSafeSystemOne
}

func (typeSafeSystemOneEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeTypeSafeSystemOne
}

func (typeSafeSystemOneEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
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
		DownstreamPath: gatewayEndpointTypeSafeSystemOne,
		RequestedModel: model,
		Stream:         false,
		Payload:        payload,
	}, nil
}
