package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	routeengine "xlyra/server/internal/router"
)

type openAIImagesResponse struct {
	Created           int64              `json:"created"`
	Data              []map[string]any   `json:"data"`
	OutputFormat      string             `json:"output_format"`
	Quality           string             `json:"quality"`
	Size              string             `json:"size"`
	Background        string             `json:"background"`
	Moderation        string             `json:"moderation"`
	Usage             *openAIImagesUsage `json:"usage"`
	PartialImages     int                `json:"partial_images"`
	OutputCompression int                `json:"output_compression"`
}

type openAIImagesUsage struct {
	InputTokens        int `json:"input_tokens"`
	OutputTokens       int `json:"output_tokens"`
	TotalTokens        int `json:"total_tokens"`
	InputTokensDetails struct {
		TextTokens  int `json:"text_tokens"`
		ImageTokens int `json:"image_tokens"`
	} `json:"input_tokens_details"`
	OutputTokensDetails struct {
		ImageTokens int `json:"image_tokens"`
	} `json:"output_tokens_details"`
}

type openAIImagesStreamEvent struct {
	Type     string                `json:"type"`
	B64JSON  string                `json:"b64_json"`
	Usage    *openAIImagesUsage    `json:"usage"`
	Data     []map[string]any      `json:"data"`
	Response *openAIImagesResponse `json:"response"`
	Error    *responsesAPIError    `json:"error"`
}

func newOpenAIImagesProtocolAdapter(request gatewayRequest, candidates ...routeengine.Candidate) openAIImagesProtocolAdapter {
	var candidate routeengine.Candidate
	if len(candidates) > 0 {
		candidate = candidates[0]
	}
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIImages, candidate)
	return openAIImagesProtocolAdapter{
		baseURL:            spec.OfficialBaseURL,
		basePath:           spec.BasePath,
		path:               spec.Path,
		downstreamPath:     request.DownstreamPath,
		downstreamProtocol: downstreamCanonicalProtocol(request.DownstreamPath),
	}
}

type openAIImagesProtocolAdapter struct {
	baseURL            string
	basePath           string
	path               string
	downstreamPath     string
	downstreamProtocol canonicalProtocol
}

func (a openAIImagesProtocolAdapter) ProtocolName() string {
	if a.downstreamProtocol == canonicalProtocolGoogleGemini {
		return "openai_images_to_gemini"
	}
	return "openai_images_generations"
}

func (openAIImagesProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if request.Canonical != nil {
		if err := validateGoogleGeminiConversion(*request.Canonical, canonicalProtocolOpenAIImages); err != nil {
			return nil, err
		}
		payload := encodeCanonicalRequestToOpenAIImages(*request.Canonical, candidate)
		if request.Stream {
			payload["stream"] = true
		}
		return applyRequestPolicyForCandidate(payload, canonicalProtocolOpenAIImages, candidate), nil
	}
	payload := clonePayload(request.Payload)
	payload["model"] = candidate.Model.UpstreamName
	return applyRequestPolicyForCandidate(payload, canonicalProtocolOpenAIImages, candidate), nil
}

func (a openAIImagesProtocolAdapter) BuildUpstreamBody(request gatewayRequest, payload map[string]any) ([]byte, string, error) {
	if a.downstreamPath == gatewayEndpointImagesEdits {
		if body, contentType, ok := encodeOpenAIImagesEditsMultipart(payload); ok {
			return body, contentType, nil
		}
	}
	encoded, err := json.Marshal(payload)
	return encoded, "application/json", err
}

func (a openAIImagesProtocolAdapter) UpstreamPath(baseURL string) string {
	path := a.path
	fallbackPath := gatewayEndpointImagesGenerations
	if a.downstreamPath == gatewayEndpointImagesEdits {
		path = gatewayEndpointImagesEdits
		fallbackPath = gatewayEndpointImagesEdits
	}
	return upstreamPathFromSpec(baseURL, resolvedProtocolSpec{
		OfficialBaseURL: a.baseURL,
		BasePath:        a.basePath,
		Path:            path,
	}, fallbackPath)
}

func (a openAIImagesProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if statusCode < 200 || statusCode >= 300 {
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: contentType,
			Body:        body,
		}, nil
	}

	if a.downstreamProtocol == canonicalProtocolGoogleGemini {
		convertedBody, usage, err := convertResponseBetweenProtocols(canonicalProtocolOpenAIImages, canonicalProtocolGoogleGemini, body, responseConversionOptions{RequireUsage: true})
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		return gatewayBufferedResponse{StatusCode: statusCode, ContentType: "application/json", Body: convertedBody, Usage: usage}, nil
	}
	return gatewayBufferedResponse{
		StatusCode:  statusCode,
		ContentType: stringValue(&contentType, "application/json"),
		Body:        body,
		Usage:       parseOpenAIImagesUsage(body),
	}, nil
}

func (a openAIImagesProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	if a.downstreamProtocol == canonicalProtocolGoogleGemini {
		return proxyOpenAIImagesStreamAsGemini(ctx, w, resp, startedAt)
	}
	return proxyOpenAIImagesStream(ctx, w, resp, startedAt)
}

func proxyOpenAIImagesStreamAsGemini(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time) (streamCaptureState, bool, error) {
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
		var event openAIImagesStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if event.Usage != nil {
			capture.usage = usageFromOpenAIImagesUsage(event.Usage, capture.usage.ImageCount)
		}
		if event.Response != nil {
			if event.Response.Usage != nil {
				capture.usage = usageFromOpenAIImagesUsage(event.Response.Usage, capture.usage.ImageCount)
			}
			images = append(images, event.Response.Data...)
		}
		images = append(images, event.Data...)
		if strings.TrimSpace(event.B64JSON) != "" {
			images = append(images, map[string]any{"b64_json": event.B64JSON})
		}
	}
	if len(images) == 0 {
		var envelope openAIImagesResponse
		if json.Unmarshal(body, &envelope) == nil {
			images = append(images, envelope.Data...)
			capture.usage = parseOpenAIImagesUsageFromEnvelope(envelope)
		}
	}
	if len(images) == 0 {
		capture.endReason = "upstream_stream_no_image"
		return capture, false, fmt.Errorf("OpenAI Images stream did not contain image data")
	}
	if !gatewayUsageAvailable(capture.usage) {
		capture.endReason = "usage_missing"
		return capture, false, fmt.Errorf("OpenAI Images stream completed without usage metadata")
	}
	if capture.usage.ImageCount == 0 {
		capture.usage.ImageCount = len(images)
	}
	writeGeminiSSEHeaders(w)
	partList := make([]any, 0, len(images))
	for _, image := range images {
		encoded := strings.TrimSpace(anyString(image["b64_json"]))
		if encoded == "" {
			continue
		}
		partList = append(partList, map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": encoded}})
	}
	payload := map[string]any{
		"candidates":    []any{map[string]any{"content": map[string]any{"role": "model", "parts": partList}, "finishReason": "STOP"}},
		"usageMetadata": geminiUsageMetadata(capture.usage),
	}
	if err := writeGeminiSSEPayload(w, payload); err != nil {
		capture.endReason = "downstream_stream_write_failed"
		return capture, true, err
	}
	capture.streamCompleted = true
	capture.sawDone = true
	capture.endReason = "done"
	return capture, true, nil
}

func proxyOpenAIImagesStream(
	ctx context.Context,
	w http.ResponseWriter,
	resp *http.Response,
	startedAt time.Time,
) (streamCaptureState, bool, error) {
	capture := streamCaptureState{}
	if resp == nil || resp.Body == nil {
		capture.endReason = "upstream_stream_missing_body"
		return capture, false, fmt.Errorf("upstream stream body is not available")
	}

	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)
	headersWritten := false

	writeHeaders := func() {
		if headersWritten {
			return
		}
		copyStreamingHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		headersWritten = true
	}

	for {
		if err := ctx.Err(); err != nil {
			capture.endReason = "downstream_client_cancelled"
			if headersWritten {
				return capture, true, err
			}
			return capture, false, err
		}

		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			observeUpstreamStreamModel(line, &capture)
			if !headersWritten {
				capture.firstByteLatency = time.Since(startedAt).Milliseconds()
				writeHeaders()
			}
			if _, writeErr := w.Write(line); writeErr != nil {
				capture.endReason = "downstream_stream_write_failed"
				return capture, headersWritten, writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
			inspectOpenAIImagesStreamLine(line, &capture)
		}

		if err == nil {
			continue
		}
		if err == io.EOF {
			if capture.streamCompleted {
				capture.endReason = "done"
			} else if capture.sawDone {
				capture.streamCompleted = true
				capture.endReason = "done"
			} else if headersWritten {
				capture.endReason = "upstream_stream_eof"
			} else {
				capture.endReason = "upstream_stream_empty"
			}
			return capture, headersWritten, nil
		}
		capture.endReason = "upstream_stream_read_failed"
		if headersWritten {
			return capture, true, err
		}
		return capture, false, err
	}
}

func inspectOpenAIImagesStreamLine(line []byte, capture *streamCaptureState) {
	if capture == nil {
		return
	}
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return
	}
	data := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	if data == "" {
		return
	}
	if strings.HasPrefix(data, "[DONE]") {
		capture.sawDone = true
		return
	}

	var event openAIImagesStreamEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}
	if event.Error != nil {
		capture.endReason = "upstream_stream_error"
		return
	}
	eventType := strings.TrimSpace(event.Type)
	if strings.HasSuffix(eventType, ".partial_image") && strings.TrimSpace(event.B64JSON) != "" && capture.usage.ImageCount == 0 {
		capture.usage.ImageCount = 1
	}
	if event.Usage != nil {
		capture.usage = usageFromOpenAIImagesUsage(event.Usage, imageCountFromImageStreamEvent(event, capture.usage.ImageCount))
	}
	if event.Response != nil {
		capture.usage = parseOpenAIImagesUsageFromEnvelope(*event.Response)
		if strings.Contains(strings.ToLower(eventType), "completed") {
			capture.streamCompleted = true
			capture.sawDone = true
		}
	}
	if strings.Contains(strings.ToLower(eventType), "completed") {
		capture.streamCompleted = true
		capture.sawDone = true
		if capture.usage.ImageCount == 0 {
			capture.usage.ImageCount = imageCountFromImageStreamEvent(event, 1)
		}
	}
}

func imageCountFromImageStreamEvent(event openAIImagesStreamEvent, fallback int) int {
	if len(event.Data) > 0 {
		return len(event.Data)
	}
	if event.Response != nil && len(event.Response.Data) > 0 {
		return len(event.Response.Data)
	}
	if strings.TrimSpace(event.B64JSON) != "" {
		return 1
	}
	return fallback
}

func parseOpenAIImagesUsage(body []byte) gatewayUsage {
	var envelope openAIImagesResponse
	_ = json.Unmarshal(body, &envelope)

	return parseOpenAIImagesUsageFromEnvelope(envelope)
}

func canonicalResponseFromOpenAIImagesBody(body []byte) (canonicalResponse, error) {
	var envelope openAIImagesResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return canonicalResponse{}, fmt.Errorf("decode OpenAI Images response: %w", err)
	}
	response := canonicalResponse{CreatedAt: envelope.Created, Usage: parseOpenAIImagesUsageFromEnvelope(envelope)}
	for _, image := range envelope.Data {
		data := strings.TrimSpace(anyString(image["b64_json"]))
		format := strings.TrimSpace(anyString(image["output_format"]))
		if data == "" {
			if url := strings.TrimSpace(anyString(image["url"])); strings.HasPrefix(url, "data:") {
				if comma := strings.Index(url, ","); comma > 0 {
					data = url[comma+1:]
				}
				if semi := strings.Index(url, ";"); strings.HasPrefix(url, "data:image/") && semi > 5 {
					format = strings.TrimPrefix(url[5:semi], "image/")
				}
			}
		}
		if data == "" {
			continue
		}
		response.Output = append(response.Output, canonicalOutputItem{Type: "image_generation_call", Status: "completed", Result: data, OutputFormat: format})
	}
	if len(response.Output) == 0 {
		return response, fmt.Errorf("OpenAI Images response did not contain image data")
	}
	return response, nil
}

func parseOpenAIImagesUsageFromEnvelope(envelope openAIImagesResponse) gatewayUsage {
	usage := gatewayUsage{
		ImageCount: len(envelope.Data),
	}
	if envelope.Usage == nil {
		return usage
	}

	usage.PromptTokens = envelope.Usage.InputTokens
	usage.CompletionTokens = envelope.Usage.OutputTokens
	usage.TotalTokens = envelope.Usage.TotalTokens
	usage.InputTextTokens = envelope.Usage.InputTokensDetails.TextTokens
	usage.InputImageTokens = envelope.Usage.InputTokensDetails.ImageTokens
	usage.OutputImageTokens = envelope.Usage.OutputTokensDetails.ImageTokens
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

func usageFromOpenAIImagesUsage(envelope *openAIImagesUsage, imageCount int) gatewayUsage {
	if envelope == nil {
		return gatewayUsage{ImageCount: imageCount}
	}
	usage := gatewayUsage{
		PromptTokens:      envelope.InputTokens,
		CompletionTokens:  envelope.OutputTokens,
		TotalTokens:       envelope.TotalTokens,
		ImageCount:        imageCount,
		InputTextTokens:   envelope.InputTokensDetails.TextTokens,
		InputImageTokens:  envelope.InputTokensDetails.ImageTokens,
		OutputImageTokens: envelope.OutputTokensDetails.ImageTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

func openAIImagesUsageFromGatewayUsage(usage gatewayUsage) openAIImagesUsage {
	out := openAIImagesUsage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}
	out.InputTokensDetails.TextTokens = usage.InputTextTokens
	if out.InputTokensDetails.TextTokens == 0 {
		out.InputTokensDetails.TextTokens = usage.PromptTokens - usage.InputImageTokens
	}
	out.InputTokensDetails.ImageTokens = usage.InputImageTokens
	out.OutputTokensDetails.ImageTokens = usage.outputImageTokensOrCompletion()
	return out
}
