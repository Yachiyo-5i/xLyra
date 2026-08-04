package gateway

import (
	"context"
	"net/http"
	"strings"
	"time"

	routeengine "xlyra/server/internal/router"
)

type openAIChatProtocolAdapter struct {
	downstreamProtocol canonicalProtocol
	baseURL            string
	basePath           string
	path               string
}

func (a openAIChatProtocolAdapter) ProtocolName() string {
	switch a.downstreamProtocol {
	case canonicalProtocolOpenAIResponses:
		return "openai_chat_completions_to_responses"
	case canonicalProtocolAnthropicMessages:
		return "openai_chat_completions_to_messages"
	}
	return "openai_chat_completions"
}

func (a openAIChatProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if request.Canonical != nil && request.DownstreamPath != gatewayEndpointChatCompletions {
		return applyChatUpstreamPolicy(encodeCanonicalRequestToOpenAIChat(*request.Canonical, candidate), candidate), nil
	}
	if a.downstreamProtocol == canonicalProtocolOpenAIResponses || request.DownstreamPath == gatewayEndpointResponses {
		if request.Canonical != nil {
			return applyChatUpstreamPolicy(encodeCanonicalRequestToOpenAIChat(*request.Canonical, candidate), candidate), nil
		}
		payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, request.Payload, stringFromPayloadModel(request.Payload), candidate)
		if err != nil {
			return nil, err
		}
		return applyChatUpstreamPolicy(payload, candidate), nil
	}
	payload := clonePayload(request.Payload)
	payload["model"] = candidate.Model.UpstreamName
	return applyChatUpstreamPolicy(payload, candidate), nil
}

func newOpenAIChatProtocolAdapter(request gatewayRequest, candidate routeengine.Candidate) openAIChatProtocolAdapter {
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, candidate)
	return openAIChatProtocolAdapter{
		downstreamProtocol: downstreamCanonicalProtocol(request.DownstreamPath),
		baseURL:            spec.OfficialBaseURL,
		basePath:           spec.BasePath,
		path:               spec.Path,
	}
}

func (a openAIChatProtocolAdapter) UpstreamPath(baseURL string) string {
	return upstreamPathFromSpec(baseURL, resolvedProtocolSpec{
		OfficialBaseURL: a.baseURL,
		BasePath:        a.basePath,
		Path:            a.path,
	}, gatewayEndpointChatCompletions)
}

func (a openAIChatProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if a.downstreamProtocol != "" && a.downstreamProtocol != canonicalProtocolOpenAIChat && statusCode >= 200 && statusCode < 300 {
		convertedBody, usage, err := convertResponseBetweenProtocols(canonicalProtocolOpenAIChat, a.downstreamProtocol, body, responseConversionOptions{})
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: "application/json",
			Body:        convertedBody,
			Usage:       completionUsageFromGatewayUsage(usage),
		}, nil
	}
	return gatewayBufferedResponse{
		StatusCode:  statusCode,
		ContentType: contentType,
		Body:        body,
		Usage:       parseCompletionUsage(body),
	}, nil
}

func (a openAIChatProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	switch a.downstreamProtocol {
	case canonicalProtocolOpenAIResponses:
		return proxyChatCompletionsStreamAsResponses(ctx, w, resp, startedAt)
	case canonicalProtocolAnthropicMessages:
		return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolOpenAIChat, canonicalProtocolAnthropicMessages, canonicalStreamOptions{Candidate: candidate})
	}
	return proxyUpstreamStream(ctx, w, resp, startedAt)
}

type openAIChatUpstreamResolver struct{}

func (openAIChatUpstreamResolver) Resolve(_ context.Context, request gatewayRequest, candidate routeengine.Candidate) (gatewayProtocolAdapter, error) {
	return newOpenAIChatProtocolAdapter(request, candidate), nil
}

// applyChatUpstreamPolicy applies the request policy and injects stream_options
// with include_usage=true for streaming requests so that upstream providers return
// token usage data in SSE chunks.
func applyChatUpstreamPolicy(payload map[string]any, candidate routeengine.Candidate) map[string]any {
	payload = applyRequestPolicyForCandidate(payload, canonicalProtocolOpenAIChat, candidate)
	if boolFromMap(payload, "stream") {
		if _, exists := payload["stream_options"]; !exists {
			payload["stream_options"] = map[string]any{
				"include_usage": true,
			}
		}
	}
	return payload
}
