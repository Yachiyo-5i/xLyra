package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	routeengine "xlyra/server/internal/router"
)

type openAIEmbeddingsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIEmbeddingsProtocolAdapter struct {
	baseURL  string
	basePath string
	path     string
}

func newOpenAIEmbeddingsProtocolAdapter(_ gatewayRequest, candidate routeengine.Candidate) openAIEmbeddingsProtocolAdapter {
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIEmbeddings, candidate)
	return openAIEmbeddingsProtocolAdapter{
		baseURL:  spec.OfficialBaseURL,
		basePath: spec.BasePath,
		path:     spec.Path,
	}
}

func (openAIEmbeddingsProtocolAdapter) ProtocolName() string {
	return "openai_embeddings"
}

func (openAIEmbeddingsProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	payload := clonePayload(request.Payload)
	payload["model"] = candidate.Model.UpstreamName
	return payload, nil
}

func (a openAIEmbeddingsProtocolAdapter) UpstreamPath(baseURL string) string {
	return upstreamPathFromSpec(baseURL, resolvedProtocolSpec{
		OfficialBaseURL: a.baseURL,
		BasePath:        a.basePath,
		Path:            a.path,
	}, gatewayEndpointEmbeddings)
}

func (openAIEmbeddingsProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if statusCode < 200 || statusCode >= 300 {
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: contentType,
			Body:        body,
		}, nil
	}

	return gatewayBufferedResponse{
		StatusCode:  statusCode,
		ContentType: stringValue(&contentType, "application/json"),
		Body:        body,
		Usage:       parseOpenAIEmbeddingsUsage(body),
	}, nil
}

func (openAIEmbeddingsProtocolAdapter) ProxyStream(_ context.Context, _ http.ResponseWriter, _ *http.Response, _ time.Time, _ routeengine.Candidate) (streamCaptureState, bool, error) {
	return streamCaptureState{}, false, nil
}

func parseOpenAIEmbeddingsUsage(body []byte) gatewayUsage {
	var envelope openAIEmbeddingsResponse
	_ = json.Unmarshal(body, &envelope)

	return gatewayUsage{
		PromptTokens: envelope.Usage.PromptTokens,
		TotalTokens:  envelope.Usage.TotalTokens,
	}
}
