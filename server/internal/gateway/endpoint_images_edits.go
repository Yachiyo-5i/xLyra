package gateway

import (
	"net/http"
	"strings"
)

const defaultImageEditModel = "gpt-image-2"

type imagesEditsEndpointAdapter struct{}

func (imagesEditsEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointImagesEdits
}

func (imagesEditsEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeOpenAIImage
}

func (imagesEditsEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
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
		model = defaultImageEditModel
		payload["model"] = model
	}

	canonical := canonicalRequestFromOpenAIImagesPayloadForPath(payload, model, gatewayEndpointImagesEdits)
	stream, _ := payload["stream"].(bool)
	return gatewayRequest{
		DownstreamPath: gatewayEndpointImagesEdits,
		RequestedModel: model,
		Stream:         stream,
		Payload:        payload,
		ContentType:    r.Header.Get("Content-Type"),
		Canonical:      &canonical,
	}, nil
}
