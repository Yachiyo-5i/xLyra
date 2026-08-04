package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	routeengine "xlyra/server/internal/router"
)

const (
	audioSpeechDefaultResponseFormat = "mp3"
	audioSpeechMaxBytes              = 64 << 20
)

type openAIAudioSpeechProtocolAdapter struct {
	baseURL        string
	basePath       string
	path           string
	sseMode        bool
	downstreamSSE  bool
	inputChars     int
	responseFormat string
}

func newOpenAIAudioSpeechProtocolAdapter(request gatewayRequest, candidate routeengine.Candidate) openAIAudioSpeechProtocolAdapter {
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIAudioSpeech, candidate)
	input, _ := request.Payload["input"].(string)
	effectivePayload := applyResolvedRequestPolicy(clonePayload(request.Payload), spec)
	format, _ := effectivePayload["response_format"].(string)
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = audioSpeechDefaultResponseFormat
	}
	sseMode := audioSpeechModelSupportsSSE(candidate.Model.UpstreamName) && audioSpeechSpecAllowsSSE(spec)
	return openAIAudioSpeechProtocolAdapter{
		baseURL:        spec.OfficialBaseURL,
		basePath:       spec.BasePath,
		path:           spec.Path,
		sseMode:        sseMode,
		downstreamSSE:  strings.EqualFold(stringFromMapAny(request.Payload, "stream_format"), "sse"),
		inputChars:     len([]rune(input)),
		responseFormat: format,
	}
}

func audioSpeechModelSupportsSSE(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	return !strings.Contains(normalized, "tts-1")
}

func audioSpeechSpecAllowsSSE(spec resolvedProtocolSpec) bool {
	payload := applyResolvedRequestPolicy(map[string]any{"stream_format": "sse"}, spec)
	return strings.EqualFold(stringFromMapAny(payload, "stream_format"), "sse")
}

func (openAIAudioSpeechProtocolAdapter) ProtocolName() string {
	return "openai_audio_speech"
}

func (openAIAudioSpeechProtocolAdapter) usesRawBufferedResponse() bool {
	return true
}

type rawBufferedResponseProtocol interface {
	usesRawBufferedResponse() bool
}

func protocolUsesRawBufferedResponse(protocol gatewayProtocolAdapter) bool {
	raw, ok := protocol.(rawBufferedResponseProtocol)
	return ok && raw.usesRawBufferedResponse()
}

func (a openAIAudioSpeechProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if a.downstreamSSE && !a.sseMode {
		return nil, fmt.Errorf("stream_format=sse is not supported by model %q", candidate.Model.UpstreamName)
	}
	payload := clonePayload(request.Payload)
	payload["model"] = candidate.Model.UpstreamName
	if a.sseMode {
		payload["stream_format"] = "sse"
	} else {
		delete(payload, "stream_format")
	}
	payload = applyRequestPolicyForCandidate(payload, canonicalProtocolOpenAIAudioSpeech, candidate)
	if a.downstreamSSE && !strings.EqualFold(stringFromMapAny(payload, "stream_format"), "sse") {
		return nil, fmt.Errorf("stream_format=sse is not supported by model %q", candidate.Model.UpstreamName)
	}
	return payload, nil
}

func (a openAIAudioSpeechProtocolAdapter) ApplyUpstreamHeaders(req *http.Request, _ string, _ string, _ bool) {
	if a.sseMode {
		req.Header.Set("Accept", "text/event-stream")
		return
	}
	req.Header.Set("Accept", "application/octet-stream")
}

func (a openAIAudioSpeechProtocolAdapter) UpstreamPath(baseURL string) string {
	return upstreamPathFromSpec(baseURL, resolvedProtocolSpec{
		OfficialBaseURL: a.baseURL,
		BasePath:        a.basePath,
		Path:            a.path,
	}, gatewayEndpointAudioSpeech)
}

func (a openAIAudioSpeechProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if statusCode < 200 || statusCode >= 300 {
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: contentType,
			Body:        body,
		}, nil
	}

	if isEventStreamContentType(contentType) || (a.sseMode && bodyLooksLikeAudioSpeechSSE(body)) {
		audio, usage := collectAudioSpeechFromSSE(body)
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: audioSpeechContentType(a.responseFormat),
			Body:        audio,
			Usage:       usage,
		}, nil
	}

	return gatewayBufferedResponse{
		StatusCode:  statusCode,
		ContentType: stringValue(&contentType, audioSpeechContentType(a.responseFormat)),
		Body:        body,
		Usage:       gatewayUsage{PromptTokens: a.inputChars},
	}, nil
}

func (a openAIAudioSpeechProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, _ routeengine.Candidate) (streamCaptureState, bool, error) {
	if resp == nil || resp.Body == nil {
		return streamCaptureState{endReason: "upstream_stream_missing_body"}, false, fmt.Errorf("upstream stream body is not available")
	}
	upstreamSSE := isEventStreamContentType(resp.Header.Get("Content-Type"))
	if a.downstreamSSE {
		if !upstreamSSE {
			return streamCaptureState{endReason: "upstream_stream_invalid_content_type"}, false, fmt.Errorf("upstream returned %q for stream_format=sse", resp.Header.Get("Content-Type"))
		}
		return proxyUpstreamStreamWithInspector(ctx, w, resp, startedAt, inspectAudioSpeechStreamLine)
	}
	if upstreamSSE {
		return a.proxySSEAsAudio(ctx, w, resp, startedAt)
	}
	return a.proxyRawAudio(ctx, w, resp, startedAt)
}

func (a openAIAudioSpeechProtocolAdapter) proxySSEAsAudio(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time) (streamCaptureState, bool, error) {
	capture := streamCaptureState{}
	reader := bufio.NewReader(resp.Body)
	flusher, _ := w.(http.Flusher)
	headersWritten := false
	writeHeaders := func() {
		if headersWritten {
			return
		}
		copyAudioSpeechHeaders(w.Header(), resp.Header, audioSpeechContentType(a.responseFormat))
		w.WriteHeader(resp.StatusCode)
		headersWritten = true
	}

	for {
		if err := ctx.Err(); err != nil {
			capture.endReason = "downstream_client_cancelled"
			return capture, headersWritten, err
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			chunk, usage, done, ok := parseAudioSpeechSSELine(line)
			if ok {
				if usage != nil {
					capture.usage = *usage
				}
				if len(chunk) > 0 {
					if !headersWritten {
						capture.firstByteLatency = time.Since(startedAt).Milliseconds()
						writeHeaders()
					}
					if _, writeErr := w.Write(chunk); writeErr != nil {
						capture.endReason = "downstream_stream_write_failed"
						return capture, headersWritten, writeErr
					}
					if flusher != nil {
						flusher.Flush()
					}
				}
				if done {
					capture.sawDone = true
					capture.streamCompleted = true
					capture.endReason = "done"
					if !headersWritten {
						capture.firstByteLatency = time.Since(startedAt).Milliseconds()
						writeHeaders()
					}
				}
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			if capture.streamCompleted {
				return capture, headersWritten, nil
			}
			if headersWritten {
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

func (a openAIAudioSpeechProtocolAdapter) proxyRawAudio(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time) (streamCaptureState, bool, error) {
	capture := streamCaptureState{usage: gatewayUsage{PromptTokens: a.inputChars}}
	flusher, _ := w.(http.Flusher)
	headersWritten := false
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			capture.endReason = "downstream_client_cancelled"
			return capture, headersWritten, err
		}
		read, err := resp.Body.Read(buffer)
		if read > 0 {
			if !headersWritten {
				capture.firstByteLatency = time.Since(startedAt).Milliseconds()
				contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
				if contentType == "" {
					contentType = audioSpeechContentType(a.responseFormat)
				}
				copyAudioSpeechHeaders(w.Header(), resp.Header, contentType)
				w.WriteHeader(resp.StatusCode)
				headersWritten = true
			}
			if _, writeErr := w.Write(buffer[:read]); writeErr != nil {
				capture.endReason = "downstream_stream_write_failed"
				return capture, headersWritten, writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			if !headersWritten {
				capture.endReason = "upstream_stream_empty"
				return capture, false, nil
			}
			capture.sawDone = true
			capture.streamCompleted = true
			capture.endReason = "done"
			return capture, true, nil
		}
		capture.endReason = "upstream_stream_read_failed"
		return capture, headersWritten, err
	}
}

func copyAudioSpeechHeaders(dst http.Header, src http.Header, contentType string) {
	dst.Set("Content-Type", contentType)
	for _, key := range []string{"Content-Disposition", "X-Request-Id", "Openai-Processing-Ms"} {
		if value := strings.TrimSpace(src.Get(key)); value != "" {
			dst.Set(key, value)
		}
	}
}

func inspectAudioSpeechStreamLine(line []byte, capture *streamCaptureState) {
	if capture == nil {
		return
	}
	_, usage, done, ok := parseAudioSpeechSSELine(line)
	if !ok {
		return
	}
	if usage != nil {
		capture.usage = *usage
	}
	if done {
		capture.sawDone = true
		capture.streamCompleted = true
	}
}

func collectAudioSpeechFromSSE(body []byte) ([]byte, gatewayUsage) {
	var audio bytes.Buffer
	usage := gatewayUsage{}
	reader := bufio.NewReader(bytes.NewReader(body))
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if chunk, parsedUsage, _, ok := parseAudioSpeechSSELine(line); ok {
				if len(chunk) > 0 {
					audio.Write(chunk)
				}
				if parsedUsage != nil {
					usage = *parsedUsage
				}
			}
		}
		if err != nil {
			break
		}
	}
	return audio.Bytes(), usage
}

func parseAudioSpeechSSELine(line []byte) ([]byte, *gatewayUsage, bool, bool) {
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return nil, nil, false, false
	}
	data := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	if data == "" {
		return nil, nil, false, false
	}
	if data == "[DONE]" {
		return nil, nil, true, true
	}

	var event struct {
		Type  string `json:"type"`
		Audio string `json:"audio"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil, nil, false, false
	}

	done := event.Type == "speech.audio.done"
	var chunk []byte
	if event.Audio != "" {
		chunk, _ = base64.StdEncoding.DecodeString(event.Audio)
	}
	var usage *gatewayUsage
	if event.Usage != nil {
		total := event.Usage.TotalTokens
		if total == 0 {
			total = event.Usage.InputTokens + event.Usage.OutputTokens
		}
		usage = &gatewayUsage{
			PromptTokens:      event.Usage.InputTokens,
			AudioOutputTokens: event.Usage.OutputTokens,
			TotalTokens:       total,
		}
	}
	return chunk, usage, done, true
}

func audioSpeechContentType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "opus":
		return "audio/ogg"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	case "wav":
		return "audio/wav"
	case "pcm":
		return "audio/pcm"
	default:
		return "audio/mpeg"
	}
}

func isEventStreamContentType(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

func bodyLooksLikeAudioSpeechSSE(body []byte) bool {
	return strings.HasPrefix(strings.TrimLeft(string(body), " \t\r\n"), "data:")
}
