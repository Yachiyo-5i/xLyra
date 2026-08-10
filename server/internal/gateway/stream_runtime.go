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
)

type streamCaptureState struct {
	usage            completionUsage
	sawDone          bool
	streamCompleted  bool
	endReason        string
	errorDetail      string
	firstByteLatency int64
	malformedLines   int
	annotations      []any
}

func proxyUpstreamStream(
	ctx context.Context,
	w http.ResponseWriter,
	resp *http.Response,
	startedAt time.Time,
) (streamCaptureState, bool, error) {
	return proxyUpstreamStreamWithInspector(ctx, w, resp, startedAt, inspectOpenAIChatStreamLine)
}

func proxyUpstreamStreamWithInspector(
	ctx context.Context,
	w http.ResponseWriter,
	resp *http.Response,
	startedAt time.Time,
	inspectLine func([]byte, *streamCaptureState),
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
			if inspectLine != nil {
				inspectLine(line, &capture)
			}
		}

		if err == nil {
			continue
		}
		if err == io.EOF {
			if capture.streamCompleted || capture.sawDone {
				capture.streamCompleted = true
				if capture.endReason == "" {
					capture.endReason = "done"
				}
			} else if capture.endReason != "" {
				// Preserve semantic terminal states detected from stream events.
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
		if headersWritten {
			return capture, true, err
		}
		return capture, false, err
	}
}

func inspectOpenAIChatStreamLine(line []byte, capture *streamCaptureState) {
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

	var envelope struct {
		Usage completionUsage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return
	}
	if envelope.Usage.TotalTokens > 0 || envelope.Usage.PromptTokens > 0 || envelope.Usage.CompletionTokens > 0 {
		capture.usage = envelope.Usage.normalized()
	}
}

func copyStreamingHeaders(dst http.Header, src http.Header) {
	contentType := strings.TrimSpace(src.Get("Content-Type"))
	if contentType == "" {
		contentType = "text/event-stream; charset=utf-8"
	}
	dst.Set("Content-Type", contentType)
	dst.Set("Cache-Control", "no-cache, no-transform")
	dst.Set("Connection", "keep-alive")
	dst.Set("X-Accel-Buffering", "no")
	for _, key := range []string{
		"X-Request-Id",
		"Openai-Processing-Ms",
		"Anthropic-Request-Id",
	} {
		if value := strings.TrimSpace(src.Get(key)); value != "" {
			dst.Set(key, value)
		}
	}
}
