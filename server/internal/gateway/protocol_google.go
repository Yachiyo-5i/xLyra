package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		includeUsage:       true,
	}
}

func (g *googleProtocolAdapter) ProtocolName() string {
	if g.stream {
		return "google_stream_generate_content"
	}
	return "google_generate_content"
}

func (*googleProtocolAdapter) CredentialEndpointTypes() []string {
	return textCredentialEndpointTypes()
}

func (g *googleProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	g.upstreamModel = candidate.Model.UpstreamName
	var canonical canonicalRequest
	if request.Canonical != nil && request.Canonical.SourceProtocol == canonicalProtocolGoogleGemini {
		return clonePayload(request.Payload), nil
	}
	if request.Canonical != nil && request.Canonical.Image != nil {
		return encodeCanonicalImageRequestToGoogleGemini(*request.Canonical, candidate), nil
	}
	if request.Canonical != nil {
		if err := validateGoogleGeminiConversion(*request.Canonical, canonicalProtocolGoogleGemini); err != nil {
			return nil, err
		}
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
	if target == canonicalProtocolGoogleGemini {
		canonical, err := decodeCanonicalResponseFromAntigravityBody(body)
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		if !gatewayUsageAvailable(canonical.Usage) {
			return gatewayBufferedResponse{}, fmt.Errorf("Gemini response usage is unavailable")
		}
		return gatewayBufferedResponse{StatusCode: statusCode, ContentType: contentType, Body: body, Usage: canonical.Usage}, nil
	}
	if target == "" {
		target = canonicalProtocolOpenAIChat
	}
	convertedBody, convertedUsage, err := convertResponseBetweenProtocols(canonicalProtocolAntigravity, target, body, responseConversionOptions{RequireUsage: true})
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
	if target == canonicalProtocolOpenAIImages {
		return proxyGoogleGeminiStreamAsOpenAIImages(ctx, w, resp, startedAt)
	}
	if target == canonicalProtocolGoogleGemini {
		return proxyUpstreamStreamWithInspector(ctx, w, resp, startedAt, func(line []byte, capture *streamCaptureState) {
			data, _, ok := sseDataFromLine(line)
			if !ok || strings.TrimSpace(data) == "" {
				return
			}
			root := map[string]any{}
			if json.Unmarshal([]byte(data), &root) != nil {
				return
			}
			if response, ok := root["response"].(map[string]any); ok {
				root = response
			}
			usage := antigravityUsage(root)
			if usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
				capture.usage = usage
			}
			if antigravityRawFinishReason(root) != "" {
				capture.streamCompleted = true
				capture.sawDone = true
			}
		})
	}
	if target == "" {
		target = canonicalProtocolOpenAIChat
	}
	return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolAntigravity, target, canonicalStreamOptions{IncludeUsage: true, RequireUsage: true, Candidate: candidate})
}

func proxyGoogleGeminiStreamAsOpenAIImages(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time) (streamCaptureState, bool, error) {
	capture := streamCaptureState{}
	if resp == nil || resp.Body == nil {
		capture.endReason = "upstream_stream_missing_body"
		return capture, false, fmt.Errorf("upstream stream body is not available")
	}
	if err := ctx.Err(); err != nil {
		capture.endReason = "downstream_client_cancelled"
		return capture, false, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		capture.endReason = "upstream_stream_read_failed"
		return capture, false, err
	}
	images := make([]map[string]any, 0)
	for _, line := range strings.Split(string(body), "\n") {
		data, done, ok := sseDataFromLine([]byte(line))
		if !ok || done || strings.TrimSpace(data) == "" {
			continue
		}
		root := map[string]any{}
		if json.Unmarshal([]byte(data), &root) != nil {
			continue
		}
		if response, ok := root["response"].(map[string]any); ok {
			root = response
		}
		usage := antigravityUsage(root)
		if usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
			capture.usage = usage
		}
		images = append(images, antigravityGeminiImages(root)...)
	}
	if len(images) == 0 {
		root := map[string]any{}
		if json.Unmarshal(body, &root) == nil {
			if response, ok := root["response"].(map[string]any); ok {
				root = response
			}
			images = append(images, antigravityGeminiImages(root)...)
			capture.usage = antigravityUsage(root)
		}
	}
	if len(images) == 0 {
		capture.endReason = "upstream_stream_no_image"
		return capture, false, fmt.Errorf("Gemini stream did not contain image data")
	}
	if !gatewayUsageAvailable(capture.usage) {
		capture.endReason = "usage_missing"
		return capture, false, fmt.Errorf("Gemini stream completed without usage metadata")
	}
	if capture.usage.ImageCount == 0 {
		capture.usage.ImageCount = len(images)
	}
	writeOpenAIImagesSSEHeaders(w)
	data := make([]map[string]any, 0, len(images))
	for _, image := range images {
		encoded := strings.TrimSpace(stringFromMapAny(image, "data"))
		if encoded == "" {
			continue
		}
		data = append(data, map[string]any{"b64_json": encoded})
	}
	payload := map[string]any{
		"type":  "image_generation.completed",
		"data":  data,
		"usage": openAIImagesUsageFromGatewayUsage(capture.usage),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		capture.endReason = "downstream_stream_write_failed"
		return capture, true, err
	}
	if _, err := w.Write(append(append([]byte("data: "), encoded...), []byte("\n\n")...)); err != nil {
		capture.endReason = "downstream_stream_write_failed"
		return capture, true, err
	}
	if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
		capture.endReason = "downstream_stream_write_failed"
		return capture, true, err
	}
	if flusher, _ := w.(http.Flusher); flusher != nil {
		flusher.Flush()
	}
	capture.streamCompleted = true
	capture.sawDone = true
	capture.endReason = "done"
	return capture, true, nil
}

func writeOpenAIImagesSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
}
