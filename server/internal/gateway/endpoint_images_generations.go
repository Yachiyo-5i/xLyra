package gateway

import (
	"net/http"
	"strings"

	"xlyra/server/internal/httpx"
)

type imagesGenerationsEndpointAdapter struct{}

func (imagesGenerationsEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointImagesGenerations
}

func (imagesGenerationsEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeOpenAIImage
}

func (imagesGenerationsEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
	var payload map[string]any
	var failure *chatFailure

	if isMultipartRequest(r) {
		payload, failure = decodeImageMultipartPayload(r)
	} else {
		payload, failure = decodeImageJSONPayload(r)
	}
	if failure != nil {
		return gatewayRequest{}, failure
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

	canonical := canonicalRequestFromOpenAIImagesPayload(payload, model)
	stream, _ := payload["stream"].(bool)
	return gatewayRequest{
		DownstreamPath: gatewayEndpointImagesGenerations,
		RequestedModel: model,
		Stream:         stream,
		Payload:        payload,
		ContentType:    r.Header.Get("Content-Type"),
		Canonical:      &canonical,
	}, nil
}

func decodeImageJSONPayload(r *http.Request) (map[string]any, *chatFailure) {
	var payload map[string]any
	if err := httpx.DecodeJSONBody(r, &payload); err != nil {
		return nil, &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_json",
			message: "request body must be valid JSON",
			stage:   "decode",
		}
	}
	return payload, nil
}
