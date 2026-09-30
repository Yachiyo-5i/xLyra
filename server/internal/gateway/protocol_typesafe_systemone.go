package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/adapter"
	routeengine "xlyra/server/internal/router"
)

type typeSafeSystemOneResponse struct {
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type typeSafeSystemOneProtocolAdapter struct{}

func (typeSafeSystemOneProtocolAdapter) ProtocolName() string {
	return "typesafe_systemone"
}

func (typeSafeSystemOneProtocolAdapter) CredentialEndpointTypes() []string {
	return []string{upstreamEndpointTypeTypeSafeSystemOne}
}

func (typeSafeSystemOneProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	payload := clonePayload(request.Payload)
	payload["model"] = candidate.Model.UpstreamName
	return payload, nil
}

func (typeSafeSystemOneProtocolAdapter) UpstreamPath(baseURL string) string {
	return upstreamPathFromSpec(baseURL, resolvedProtocolSpec{OfficialBaseURL: adapter.TypeSafeDefaultBaseURL}, gatewayEndpointTypeSafeSystemOne)
}

func (typeSafeSystemOneProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
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
		Usage:       parseTypeSafeSystemOneUsage(body),
	}, nil
}

func (typeSafeSystemOneProtocolAdapter) ProxyStream(_ context.Context, _ http.ResponseWriter, _ *http.Response, _ time.Time, _ routeengine.Candidate) (streamCaptureState, bool, error) {
	return streamCaptureState{}, false, nil
}

func parseTypeSafeSystemOneUsage(body []byte) gatewayUsage {
	var envelope typeSafeSystemOneResponse
	_ = json.Unmarshal(body, &envelope)

	return gatewayUsage{
		PromptTokens:     envelope.Usage.InputTokens,
		CompletionTokens: envelope.Usage.OutputTokens,
		TotalTokens:      envelope.Usage.InputTokens + envelope.Usage.OutputTokens,
	}
}
