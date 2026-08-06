package gateway

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	routeengine "xlyra/server/internal/router"
)

type mimoAudioSpeechProtocolAdapter struct {
	baseURL        string
	basePath       string
	path           string
	downstreamSSE  bool
	responseFormat string
	inputChars     int
}

func newMiMoAudioSpeechProtocolAdapter(request gatewayRequest, candidate routeengine.Candidate) mimoAudioSpeechProtocolAdapter {
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, candidate)
	responseFormat := strings.ToLower(strings.TrimSpace(stringFromMapAny(request.Payload, "response_format")))
	if responseFormat == "" {
		responseFormat = audioSpeechDefaultResponseFormat
	}
	input, _ := request.Payload["input"].(string)
	return mimoAudioSpeechProtocolAdapter{
		baseURL:        spec.OfficialBaseURL,
		basePath:       spec.BasePath,
		path:           spec.Path,
		downstreamSSE:  strings.EqualFold(strings.TrimSpace(stringFromMapAny(request.Payload, "stream_format")), "sse"),
		responseFormat: responseFormat,
		inputChars:     len([]rune(input)),
	}
}

func (mimoAudioSpeechProtocolAdapter) ProtocolName() string {
	return "mimo_tts_audio_speech_to_chat"
}

func (mimoAudioSpeechProtocolAdapter) usesRawBufferedResponse() bool {
	return true
}

func (a mimoAudioSpeechProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if a.responseFormat != "pcm" && a.responseFormat != "wav" {
		return nil, fmt.Errorf("mimo TTS audio/speech bridge supports response_format=pcm or wav, got %q", a.responseFormat)
	}
	if !mimoAudioSpeechSpeedIsDefault(request.Payload) {
		return nil, fmt.Errorf("mimo TTS audio/speech bridge only supports speed=1")
	}
	input, _ := request.Payload["input"].(string)
	instructions, _ := request.Payload["instructions"].(string)
	if rawInstructions, exists := request.Payload["instructions"]; exists {
		if _, ok := rawInstructions.(string); !ok {
			return nil, fmt.Errorf("mimo TTS instructions must be a string, got %T", rawInstructions)
		}
	}
	if strings.EqualFold(strings.TrimSpace(candidate.Model.UpstreamName), "mimo-v2.5-tts-voicedesign") && strings.TrimSpace(instructions) == "" {
		return nil, fmt.Errorf("mimo-v2.5-tts-voicedesign requires instructions")
	}
	voice, voiceOK := request.Payload["voice"].(string)
	if rawVoice, exists := request.Payload["voice"]; exists && !voiceOK {
		return nil, fmt.Errorf("mimo TTS voice must be a string, got %T", rawVoice)
	}
	model := strings.ToLower(strings.TrimSpace(candidate.Model.UpstreamName))
	if model == "mimo-v2.5-tts-voiceclone" && strings.TrimSpace(voice) == "" {
		return nil, fmt.Errorf("mimo-v2.5-tts-voiceclone requires voice")
	}
	messages := make([]any, 0, 2)
	if strings.TrimSpace(instructions) != "" {
		messages = append(messages, map[string]any{"role": "user", "content": instructions})
	}
	messages = append(messages, map[string]any{"role": "assistant", "content": input})
	audio := map[string]any{"format": "pcm16"}
	if voiceOK && strings.TrimSpace(voice) != "" && model != "mimo-v2.5-tts-voicedesign" {
		audio["voice"] = voice
	}
	return map[string]any{
		"model":    candidate.Model.UpstreamName,
		"messages": messages,
		"audio":    audio,
		"stream":   true,
	}, nil
}

func mimoAudioSpeechSpeedIsDefault(payload map[string]any) bool {
	raw, ok := payload["speed"]
	if !ok {
		return true
	}
	switch value := raw.(type) {
	case float64:
		return value == 1
	case float32:
		return value == 1
	case int:
		return value == 1
	case int64:
		return value == 1
	default:
		return false
	}
}

func (a mimoAudioSpeechProtocolAdapter) UpstreamPath(baseURL string) string {
	return upstreamPathFromSpec(baseURL, resolvedProtocolSpec{
		OfficialBaseURL: a.baseURL,
		BasePath:        a.basePath,
		Path:            a.path,
	}, gatewayEndpointChatCompletions)
}

func (a mimoAudioSpeechProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if statusCode < 200 || statusCode >= 300 {
		return gatewayBufferedResponse{StatusCode: statusCode, ContentType: contentType, Body: body}, nil
	}
	audio, usage, err := bufferMiMoTTSChatAudio(body, a.normalizedUpstreamResponseFormat())
	if err != nil {
		return gatewayBufferedResponse{}, err
	}
	return gatewayBufferedResponse{
		StatusCode:  statusCode,
		ContentType: audioSpeechContentType(a.responseFormat),
		Body:        audio,
		Usage:       usage,
	}, nil
}

func (a mimoAudioSpeechProtocolAdapter) normalizedUpstreamResponseFormat() string {
	if a.responseFormat == "wav" {
		return "wav"
	}
	return "pcm16"
}

func (a mimoAudioSpeechProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, _ routeengine.Candidate) (streamCaptureState, bool, error) {
	if resp == nil || resp.Body == nil {
		return streamCaptureState{endReason: "upstream_stream_missing_body"}, false, fmt.Errorf("upstream stream body is not available")
	}
	if !isEventStreamContentType(resp.Header.Get("Content-Type")) {
		return streamCaptureState{endReason: "upstream_stream_invalid_content_type"}, false, fmt.Errorf("mimo TTS upstream must return text/event-stream, got %q", resp.Header.Get("Content-Type"))
	}
	collectAudio := a.responseFormat == "wav" || a.downstreamSSE
	decoder := newMiMoTTSChatStreamDecoder(collectAudio)
	capture := streamCaptureState{usage: gatewayUsage{PromptTokens: a.inputChars}}
	reader := bufio.NewReader(resp.Body)
	flusher, _ := w.(http.Flusher)
	headersWritten := false
	writeHeaders := func() {
		if headersWritten {
			return
		}
		if a.downstreamSSE {
			setDownstreamSSEHeaders(w.Header())
		} else {
			copyAudioSpeechHeaders(w.Header(), resp.Header, audioSpeechContentType(a.responseFormat))
		}
		w.WriteHeader(resp.StatusCode)
		headersWritten = true
	}
	writeBody := func(body []byte) error {
		if len(body) == 0 {
			return nil
		}
		if !headersWritten {
			capture.firstByteLatency = time.Since(startedAt).Milliseconds()
			writeHeaders()
		}
		if _, err := w.Write(body); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	streamDone := false
	for !streamDone {
		if err := ctx.Err(); err != nil {
			capture.endReason = "downstream_client_cancelled"
			return capture, headersWritten, err
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			chunk, consumeErr := decoder.ConsumeLine(line)
			if consumeErr != nil {
				capture.endReason = "upstream_stream_error"
				return capture, headersWritten, consumeErr
			}
			if chunk.Usage != nil {
				capture.usage = *chunk.Usage
			}
			if len(chunk.Audio) > 0 && a.responseFormat != "wav" {
				if a.downstreamSSE {
					encoded := base64.StdEncoding.EncodeToString(chunk.Audio)
					if writeErr := writeBody([]byte("data: " + marshalSSEPayload(map[string]any{"type": "speech.audio.delta", "audio": encoded}) + "\n\n")); writeErr != nil {
						capture.endReason = "downstream_stream_write_failed"
						return capture, headersWritten, writeErr
					}
				} else if writeErr := writeBody(chunk.Audio); writeErr != nil {
					capture.endReason = "downstream_stream_write_failed"
					return capture, headersWritten, writeErr
				}
			}
			streamDone = chunk.Done
		}
		if streamDone {
			break
		}
		if err == nil {
			continue
		}
		if err != io.EOF {
			capture.endReason = "upstream_stream_read_failed"
			return capture, headersWritten, err
		}
		break
	}

	audio, usage, finalizeErr := decoder.Finalize()
	if finalizeErr != nil {
		capture.endReason = "upstream_stream_incomplete"
		return capture, headersWritten, finalizeErr
	}
	if usage != (completionUsage{}) {
		capture.usage = usage
	}
	if a.responseFormat == "wav" {
		audio = mimoTTSWAV(audio)
		if a.downstreamSSE {
			encoded := base64.StdEncoding.EncodeToString(audio)
			if writeErr := writeBody([]byte("data: " + marshalSSEPayload(map[string]any{"type": "speech.audio.delta", "audio": encoded}) + "\n\n")); writeErr != nil {
				capture.endReason = "downstream_stream_write_failed"
				return capture, headersWritten, writeErr
			}
		} else if writeErr := writeBody(audio); writeErr != nil {
			capture.endReason = "downstream_stream_write_failed"
			return capture, headersWritten, writeErr
		}
	}
	if a.downstreamSSE {
		usagePayload := map[string]any{
			"input_tokens":  capture.usage.PromptTokens,
			"output_tokens": capture.usage.CompletionTokens,
			"total_tokens":  capture.usage.TotalTokens,
		}
		if writeErr := writeBody([]byte("data: " + marshalSSEPayload(map[string]any{"type": "speech.audio.done", "usage": usagePayload}) + "\n\n")); writeErr != nil {
			capture.endReason = "downstream_stream_write_failed"
			return capture, headersWritten, writeErr
		}
	}
	capture.streamCompleted = true
	capture.sawDone = decoder.receivedDone
	capture.endReason = "done"
	return capture, headersWritten, nil
}
