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

const (
	codexGatewayResponsesPath = "/codex/responses"
	codexOriginator           = "codex_cli_rs"
)

type codexProtocolAdapter struct {
	responses        openAIResponsesProtocolAdapter
	downstreamImages bool
	downstreamPath   string
}

func newCodexProtocolAdapter(request gatewayRequest) codexProtocolAdapter {
	return codexProtocolAdapter{
		responses:        newOpenAIResponsesProtocolAdapter(request).(openAIResponsesProtocolAdapter),
		downstreamImages: isOpenAIImagesEndpoint(request.DownstreamPath),
		downstreamPath:   request.DownstreamPath,
	}
}

func (a codexProtocolAdapter) ProtocolName() string {
	return "codex_responses"
}

func (a codexProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	payload, err := a.buildUpstreamPayload(request, candidate)
	if err != nil {
		return nil, err
	}
	return applyCodexResponsesLitePolicy(payload, request), nil
}

func (a codexProtocolAdapter) buildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if a.downstreamImages {
		if request.Canonical != nil {
			payload := encodeCanonicalImageRequestToCodexResponses(*request.Canonical, candidate)
			return applyCodexRequestPolicyForRequest(payload, candidate, request), nil
		}
		payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIImages, canonicalProtocolCodexResponses, request.Payload, stringFromPayloadModel(request.Payload), candidate)
		if err != nil {
			return nil, err
		}
		return applyCodexRequestPolicyForRequest(payload, candidate, request), nil
	}
	if request.DownstreamPath == gatewayEndpointResponses {
		payload := clonePayload(request.Payload)
		payload["model"] = codexResponsesHostModel(payload, candidate)
		if codexPayloadHasImageGenerationTool(payload) {
			payload["stream"] = true
		}
		return applyCodexRequestPolicyForRequest(payload, candidate, request), nil
	}
	if request.Canonical != nil {
		payload, err := encodeCanonicalRequestToOpenAIResponses(*request.Canonical, candidate)
		if err != nil {
			return nil, err
		}
		return applyCodexRequestPolicyForRequest(payload, candidate, request), nil
	}
	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, request.Payload, stringFromPayloadModel(request.Payload), candidate)
	if err != nil {
		return nil, err
	}
	return applyCodexRequestPolicyForRequest(payload, candidate, request), nil
}

func (a codexProtocolAdapter) UpstreamPath(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://chatgpt.com/backend-api"
	}
	if strings.HasSuffix(base, "/codex") {
		return base + "/responses"
	}
	if strings.HasSuffix(base, "/backend-api") {
		return base + codexGatewayResponsesPath
	}
	return base + "/backend-api" + codexGatewayResponsesPath
}

func (a codexProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	if a.downstreamImages {
		contentType := strings.TrimSpace(headers.Get("Content-Type"))
		if statusCode < 200 || statusCode >= 300 {
			return gatewayBufferedResponse{
				StatusCode:  statusCode,
				ContentType: contentType,
				Body:        body,
			}, nil
		}
		normalizedBody, _, err := parseBufferedResponsesBody(body)
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		if len(normalizedBody) > 0 {
			body = normalizedBody
		}
		imagesBody, usage, err := convertResponseBetweenProtocols(canonicalProtocolCodexResponses, canonicalProtocolOpenAIImages, body, responseConversionOptions{})
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: "application/json",
			Body:        imagesBody,
			Usage:       usage,
		}, nil
	}
	return a.responses.TransformBufferedResponse(statusCode, headers, body)
}

func (a codexProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	if a.downstreamImages {
		prefix := "image_generation"
		if a.downstreamPath == gatewayEndpointImagesEdits {
			prefix = "image_edit"
		}
		return proxyCodexResponsesImageStream(ctx, w, resp, startedAt, prefix)
	}
	if a.responses.downstreamProtocol == canonicalProtocolOpenAIResponses {
		return proxyResponsesStreamPassthrough(ctx, w, resp, startedAt)
	}
	return a.responses.ProxyStream(ctx, w, resp, startedAt, candidate)
}

func proxyCodexResponsesImageStream(
	ctx context.Context,
	w http.ResponseWriter,
	resp *http.Response,
	startedAt time.Time,
	eventPrefix string,
) (streamCaptureState, bool, error) {
	capture := streamCaptureState{}
	if resp == nil || resp.Body == nil {
		capture.endReason = "upstream_stream_missing_body"
		return capture, false, fmt.Errorf("upstream stream body is not available")
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	headersWritten := false
	writeHeaders := func() {
		if headersWritten {
			return
		}
		w.WriteHeader(http.StatusOK)
		headersWritten = true
		capture.firstByteLatency = firstNonZero(capture.firstByteLatency, time.Since(startedAt).Milliseconds())
	}

	reader := bufio.NewReader(resp.Body)
	for {
		if err := ctx.Err(); err != nil {
			capture.endReason = "downstream_client_cancelled"
			return capture, headersWritten, err
		}

		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			data, done, ok := sseDataFromLine(line)
			if ok && done {
				capture.sawDone = true
			}
			if ok && !done && strings.TrimSpace(data) != "" {
				var event responsesStreamBufferEvent
				if unmarshalErr := json.Unmarshal([]byte(data), &event); unmarshalErr != nil {
					capture.endReason = "upstream_stream_parse_failed"
					return capture, headersWritten, unmarshalErr
				}
				if event.Error != nil {
					capture.endReason = "upstream_stream_error"
					writeHeaders()
					if writeErr := writeImageStreamEvent(w, "error", map[string]any{"type": "error", "error": event.Error}); writeErr != nil {
						return capture, headersWritten, writeErr
					}
					return capture, headersWritten, nil
				}
				if writeErr := writeCodexImageStreamEvents(w, &capture, eventPrefix, event, writeHeaders); writeErr != nil {
					return capture, headersWritten, writeErr
				}
			}
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
		return capture, headersWritten, err
	}
}

func writeCodexImageStreamEvents(w http.ResponseWriter, capture *streamCaptureState, eventPrefix string, event responsesStreamBufferEvent, writeHeaders func()) error {
	eventType := strings.TrimSpace(event.Type)
	if eventType == "response.image_generation_call.partial_image" && strings.TrimSpace(event.PartialImageB64) != "" {
		writeHeaders()
		if capture.usage.ImageCount == 0 {
			capture.usage.ImageCount = 1
		}
		return writeImageStreamEvent(w, eventPrefix+".partial_image", map[string]any{
			"type":                eventPrefix + ".partial_image",
			"b64_json":            strings.TrimSpace(event.PartialImageB64),
			"partial_image_index": event.PartialImageIndex,
		})
	}
	if eventType != "response.completed" || event.Response.ID == "" && len(event.Response.Output) == 0 && event.Response.Usage == nil {
		return nil
	}
	if event.Response.Usage != nil {
		capture.usage = completionUsageFromResponsesUsage(event.Response.Usage)
	}
	outputs := imageGenerationOutputsFromResponses(event.Response)
	if len(outputs) == 0 {
		return nil
	}
	writeHeaders()
	for index, item := range outputs {
		payload := map[string]any{
			"type":     eventPrefix + ".completed",
			"b64_json": strings.TrimSpace(item.Result),
		}
		if revisedPrompt := strings.TrimSpace(item.RevisedPrompt); revisedPrompt != "" {
			payload["revised_prompt"] = revisedPrompt
		}
		if index == len(outputs)-1 && event.Response.Usage != nil {
			payload["usage"] = openAIImagesUsageFromGatewayUsage(capture.usage)
		}
		if err := writeImageStreamEvent(w, eventPrefix+".completed", payload); err != nil {
			return err
		}
	}
	capture.usage.ImageCount = len(outputs)
	capture.streamCompleted = true
	capture.sawDone = true
	return nil
}

func imageGenerationOutputsFromResponses(response responsesResponse) []responsesOutputItem {
	outputs := make([]responsesOutputItem, 0)
	for _, item := range response.Output {
		if strings.EqualFold(strings.TrimSpace(item.Type), "image_generation_call") && strings.TrimSpace(item.Result) != "" {
			outputs = append(outputs, item)
		}
	}
	return outputs
}

func writeImageStreamEvent(w http.ResponseWriter, eventName string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte("event: " + eventName + "\n")); err != nil {
		return err
	}
	if _, err := w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := w.Write(encoded); err != nil {
		return err
	}
	if _, err := w.Write([]byte("\n\n")); err != nil {
		return err
	}
	if flusher, _ := w.(http.Flusher); flusher != nil {
		flusher.Flush()
	}
	return nil
}
