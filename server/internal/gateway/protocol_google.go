package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	routeengine "xlyra/server/internal/router"
)

const defaultGoogleGeminiBaseURL = "https://generativelanguage.googleapis.com"

type googleProtocolAdapter struct {
	stream             bool
	downstreamProtocol canonicalProtocol
	upstreamModel      string
	includeUsage       bool
}

func newGoogleProtocolAdapter(request gatewayRequest) gatewayProtocolAdapter {
	return &googleProtocolAdapter{
		stream:             request.Stream,
		downstreamProtocol: downstreamCanonicalProtocol(request.DownstreamPath),
		includeUsage:       chatStreamUsageEnabled(request.Payload),
	}
}

func (g *googleProtocolAdapter) ProtocolName() string {
	if g.stream {
		return "google_stream_generate_content"
	}
	return "google_generate_content"
}

func (g *googleProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	g.upstreamModel = candidate.Model.UpstreamName
	var canonical canonicalRequest
	if request.Canonical != nil {
		canonical = *request.Canonical
	} else if request.DownstreamPath == gatewayEndpointResponses {
		decoded, err := canonicalRequestFromOpenAIResponsesPayload(request.Payload, stringFromPayloadModel(request.Payload))
		if err != nil {
			return nil, err
		}
		canonical = decoded
	} else if request.DownstreamPath == gatewayEndpointMessages {
		decoded, err := canonicalRequestFromAnthropicMessagesPayload(request.Payload, stringFromPayloadModel(request.Payload))
		if err != nil {
			return nil, err
		}
		canonical = decoded
	} else {
		decoded, err := canonicalRequestFromOpenAIChatPayload(request.Payload, stringFromPayloadModel(request.Payload))
		if err != nil {
			return nil, err
		}
		canonical = decoded
	}
	payload := encodeCanonicalRequestToAntigravityGemini(canonical, candidate.Model.UpstreamName)
	delete(payload, "model")
	return payload, nil
}

func (g *googleProtocolAdapter) UpstreamPath(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultGoogleGeminiBaseURL
	}
	model := strings.TrimSpace(g.upstreamModel)
	if model == "" {
		model = "gemini-2.5-pro"
	}
	if g.stream {
		return base + "/v1beta/models/" + model + ":streamGenerateContent?alt=sse"
	}
	return base + "/v1beta/models/" + model + ":generateContent"
}

func (g *googleProtocolAdapter) ApplyUpstreamHeaders(req *http.Request, upstreamKey string, _ string, _ bool) {
	applyGoogleGatewayHeaders(req, upstreamKey)
}

func (g *googleProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if statusCode < 200 || statusCode >= 300 {
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: contentType,
			Body:        body,
		}, nil
	}
	target := g.downstreamProtocol
	if target == "" || target == canonicalProtocolOpenAIImages {
		target = canonicalProtocolOpenAIChat
	}
	convertedBody, convertedUsage, err := convertResponseBetweenProtocols(canonicalProtocolAntigravity, target, body, responseConversionOptions{})
	if err != nil {
		return gatewayBufferedResponse{}, err
	}
	return gatewayBufferedResponse{
		StatusCode:  statusCode,
		ContentType: "application/json",
		Body:        convertedBody,
		Usage:       convertedUsage,
	}, nil
}

func (g *googleProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	target := g.downstreamProtocol
	if target == "" || target == canonicalProtocolOpenAIImages {
		target = canonicalProtocolOpenAIChat
	}
	return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolAntigravity, target, canonicalStreamOptions{IncludeUsage: g.includeUsage, Candidate: candidate})
}
