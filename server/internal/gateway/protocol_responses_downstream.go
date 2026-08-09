package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

var synthesizedResponseCounter uint64

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	Delta        chatMessage `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type chatMessage struct {
	Role              string         `json:"role"`
	Content           any            `json:"content"`
	ReasoningContent  string         `json:"reasoning_content,omitempty"`
	Thinking          string         `json:"thinking,omitempty"`
	ThinkingSignature string         `json:"thinking_signature,omitempty"`
	ToolCalls         []chatToolCall `json:"tool_calls"`
}

type chatToolCall struct {
	Index                 int    `json:"index"`
	ID                    string `json:"id"`
	Type                  string `json:"type"`
	ThoughtSignature      string `json:"thought_signature,omitempty"`
	ThoughtSignatureCamel string `json:"thoughtSignature,omitempty"`
	Function              struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func proxyChatCompletionsStreamAsResponses(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time) (streamCaptureState, bool, error) {
	return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
}

func proxyResponsesStreamPassthrough(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time) (streamCaptureState, bool, error) {
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
			return capture, headersWritten, err
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if normalizedLine, changed := normalizeResponsesStreamDataLine(line); changed {
				line = normalizedLine
			}
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
			inspectResponsesStreamLine(line, &capture)
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			if capture.streamCompleted {
				capture.endReason = "done"
			} else if capture.endReason != "" {
				// Preserve semantic terminal states such as response_incomplete or upstream_stream_error.
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
		if capture.endReason == "" && (capture.streamCompleted || capture.sawDone) {
			capture.streamCompleted = true
			capture.endReason = "done"
			return capture, headersWritten, nil
		}
		if capture.endReason == "" {
			capture.endReason = "upstream_stream_read_failed"
		}
		return capture, headersWritten, err
	}
}

func inspectResponsesStreamLine(line []byte, capture *streamCaptureState) {
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
	if data == "[DONE]" {
		capture.sawDone = true
		return
	}
	var event responsesStreamEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}
	switch event.Type {
	case "response.completed":
		capture.streamCompleted = true
		capture.sawDone = true
	case "response.incomplete":
		capture.endReason = "response_incomplete"
	case "response.error", "response.failed":
		capture.endReason = "upstream_stream_error"
		capture.errorDetail = truncatedStreamErrorDetail(data)
	case "response.output_text.annotation.added":
		appendStreamAnnotation(capture, event.Annotation)
	}
	if event.Response != nil && event.Response.Usage != nil {
		capture.usage = completionUsageFromResponsesUsage(event.Response.Usage)
	}
}

func parseResponsesUsage(body []byte) completionUsage {
	var response responsesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return completionUsage{}
	}
	return completionUsageFromResponsesUsage(response.Usage)
}

func responsesUsagePayload(usage completionUsage) map[string]any {
	return map[string]any{
		"input_tokens":  usage.PromptTokens,
		"output_tokens": usage.CompletionTokens,
		"total_tokens":  usage.TotalTokens,
		"input_tokens_details": map[string]any{
			"cached_tokens":      usage.CachedPromptTokens,
			"cache_write_tokens": usage.CacheWriteTokens,
			"image_tokens":       usage.InputImageTokens,
		},
		"output_tokens_details": map[string]any{
			"reasoning_tokens": usage.ReasoningTokens,
		},
	}
}

func normalizeResponsesStreamDataLine(line []byte) ([]byte, bool) {
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return line, false
	}
	data := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	if data == "" || data == "[DONE]" {
		return line, false
	}
	normalized, changed, err := normalizeResponsesJSONUsage([]byte(data))
	if err != nil || !changed {
		return line, false
	}
	suffix := []byte{}
	if bytes.HasSuffix(line, []byte("\r\n")) {
		suffix = []byte("\r\n")
	} else if bytes.HasSuffix(line, []byte("\n")) {
		suffix = []byte("\n")
	}
	rewritten := make([]byte, 0, len("data: ")+len(normalized)+len(suffix))
	rewritten = append(rewritten, "data: "...)
	rewritten = append(rewritten, normalized...)
	rewritten = append(rewritten, suffix...)
	return rewritten, true
}

func normalizeResponsesBufferedUsage(body []byte) []byte {
	normalized, changed, err := normalizeResponsesJSONUsage(body)
	if err != nil || !changed {
		return body
	}
	return normalized
}

func normalizeResponsesJSONUsage(data []byte) ([]byte, bool, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, false, err
	}
	if !normalizeResponsesEnvelopeUsage(root) {
		return data, false, nil
	}
	normalized, err := json.Marshal(root)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

func normalizeResponsesEnvelopeUsage(root map[string]any) bool {
	changed := false
	if usage, ok := root["usage"].(map[string]any); ok {
		changed = normalizeResponsesUsageMap(usage) || changed
	}
	if response, ok := root["response"].(map[string]any); ok {
		if usage, ok := response["usage"].(map[string]any); ok {
			changed = normalizeResponsesUsageMap(usage) || changed
		}
	}
	return changed
}

func normalizeResponsesUsageMap(usage map[string]any) bool {
	changed := false
	reasoningTokens := int64(0)
	if completionDetails, ok := usage["completion_tokens_details"].(map[string]any); ok {
		reasoningTokens = payloadInt64(completionDetails["reasoning_tokens"])
	}
	inputDetails, ok := usage["input_tokens_details"].(map[string]any)
	if !ok {
		inputDetails = map[string]any{}
		usage["input_tokens_details"] = inputDetails
		changed = true
	}
	if _, ok := inputDetails["cached_tokens"]; !ok {
		inputDetails["cached_tokens"] = int64(0)
		changed = true
	}
	outputDetails, ok := usage["output_tokens_details"].(map[string]any)
	if !ok {
		outputDetails = map[string]any{}
		usage["output_tokens_details"] = outputDetails
		changed = true
	}
	if _, ok := outputDetails["reasoning_tokens"]; !ok {
		outputDetails["reasoning_tokens"] = reasoningTokens
		changed = true
	}
	return changed
}

func synthesizedResponseID() string {
	return fmt.Sprintf("resp_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&synthesizedResponseCounter, 1))
}

func firstNonZero(value int64, fallback int64) int64 {
	if value != 0 {
		return value
	}
	return fallback
}
