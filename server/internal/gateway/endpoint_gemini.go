package gateway

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"xlyra/server/internal/httpx"
)

type geminiGenerateContentEndpointAdapter struct {
	stream bool
}

func (a geminiGenerateContentEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointGeminiGenerate
}

func (a geminiGenerateContentEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeGoogleGemini
}

func (a geminiGenerateContentEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
	var payload map[string]any
	if err := httpx.DecodeJSONBody(r, &payload); err != nil {
		return gatewayRequest{}, decodeRequestFailure(err)
	}

	model := strings.TrimSpace(chi.URLParam(r, "model"))
	if model == "" {
		return gatewayRequest{}, &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_model",
			message: "model path parameter is required",
			stage:   "validate",
		}
	}
	if failure := normalizeClientRequestOptions(payload); failure != nil {
		failure.requestedModel = model
		failure.stream = a.stream
		return gatewayRequest{}, failure
	}

	canonical, err := canonicalRequestFromGoogleGeminiPayload(payload, model, a.stream)
	if err != nil {
		return gatewayRequest{}, &chatFailure{
			status:         http.StatusBadRequest,
			code:           "invalid_gemini_request",
			message:        err.Error(),
			requestedModel: model,
			stream:         a.stream,
			stage:          "validate",
		}
	}
	return gatewayRequest{
		DownstreamPath:    gatewayEndpointGeminiGenerate,
		DownstreamHeaders: r.Header.Clone(),
		RequestedModel:    model,
		Stream:            a.stream,
		Payload:           payload,
		ContentType:       r.Header.Get("Content-Type"),
		Canonical:         &canonical,
	}, nil
}
