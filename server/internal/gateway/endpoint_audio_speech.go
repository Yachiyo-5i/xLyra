package gateway

import (
	"net/http"
	"strings"

	"xlyra/server/internal/httpx"
)

type audioSpeechEndpointAdapter struct{}

func (audioSpeechEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointAudioSpeech
}

func (audioSpeechEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeOpenAIAudioSpeech
}

func (audioSpeechEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
	var payload map[string]any
	if err := httpx.DecodeJSONBody(r, &payload); err != nil {
		return gatewayRequest{}, &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_json",
			message: "request body must be valid JSON",
			stage:   "decode",
		}
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

	input, _ := payload["input"].(string)
	if strings.TrimSpace(input) == "" {
		return gatewayRequest{}, &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_input",
			message: "input is required",
			stage:   "validate",
		}
	}
	if rawStreamFormat, exists := payload["stream_format"]; exists {
		streamFormat, ok := rawStreamFormat.(string)
		normalized := strings.ToLower(strings.TrimSpace(streamFormat))
		if !ok || (normalized != "audio" && normalized != "sse") {
			return gatewayRequest{}, &chatFailure{
				status:  http.StatusBadRequest,
				code:    "invalid_stream_format",
				message: "stream_format must be audio or sse",
				stage:   "validate",
			}
		}
	}

	return gatewayRequest{
		DownstreamPath: gatewayEndpointAudioSpeech,
		RequestedModel: model,
		Stream:         true,
		Payload:        payload,
		ContentType:    r.Header.Get("Content-Type"),
	}, nil
}
