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
		baseURL:        spec.OfficialBaseURL,
		basePath:       spec.BasePath,
		path:           spec.Path,
		downstreamPath: request.DownstreamPath,
	}
}

type openAIImagesProtocolAdapter struct {
	baseURL        string
	basePath       string
	path           string
	downstreamPath string
}

func (a openAIImagesProtocolAdapter) ProtocolName() string {
	return "openai_images_generations"
}

func (openAIImagesProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if request.Canonical != nil {
		return applyRequestPolicyForCandidate(encodeCanonicalRequestToOpenAIImages(*request.Canonical, candidate), canonicalProtocolOpenAIImages, candidate), nil
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

func (openAIImagesProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
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
		Usage:       parseOpenAIImagesUsage(body),
	}, nil
}

func (openAIImagesProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	return proxyOpenAIImagesStream(ctx, w, resp, startedAt)
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
