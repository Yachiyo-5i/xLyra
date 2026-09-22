package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type googleGeminiStreamEncoder struct {
	options    canonicalStreamOptions
	w          http.ResponseWriter
	capture    *streamCaptureState
	responseID string
	model      string
	createdAt  int64
	completed  bool
	toolArgs   map[string]*strings.Builder
}

func newGoogleGeminiStreamEncoder(options canonicalStreamOptions, w http.ResponseWriter, capture *streamCaptureState) canonicalStreamEncoder {
	return &googleGeminiStreamEncoder{
		options:   options,
		w:         w,
		capture:   capture,
		createdAt: time.Now().Unix(),
		toolArgs:  map[string]*strings.Builder{},
	}
}

func (e *googleGeminiStreamEncoder) EncodeEvent(event canonicalStreamEvent) error {
	if event.ID != "" {
		e.responseID = event.ID
	}
	if event.Model != "" {
		e.model = event.Model
	}
	if event.CreatedAt > 0 {
		e.createdAt = event.CreatedAt
	}
	switch event.Type {
	case canonicalStreamEventCreated:
		return nil
	case canonicalStreamEventTextDelta:
		if event.Delta == "" {
			return nil
		}
		return e.writeCandidate(map[string]any{"text": event.Delta})
	case canonicalStreamEventReasoningDelta:
		if event.Delta == "" {
			return nil
		}
		return e.writeCandidate(map[string]any{"text": event.Delta, "thought": true})
	case canonicalStreamEventToolCallDelta:
		callID := strings.TrimSpace(event.ToolCallID)
		if callID == "" {
			callID = fmt.Sprintf("call_%d", len(e.toolArgs)+1)
		}
		if event.ToolCallArgumentsDelta != "" {
			if e.toolArgs[callID] == nil {
				e.toolArgs[callID] = &strings.Builder{}
			}
			e.toolArgs[callID].WriteString(event.ToolCallArgumentsDelta)
		}
		arguments := ""
		if builder := e.toolArgs[callID]; builder != nil {
			arguments = builder.String()
		}
		call := map[string]any{
			"name": event.ToolCallName,
			"args": unmarshalJSONObjectOrEmpty(arguments),
		}
		if callID != "" {
			call["id"] = callID
		}
		part := map[string]any{"functionCall": call}
		addAntigravityThoughtSignature(part, event.ToolCallMetadata)
		return e.writeCandidate(part)
	case canonicalStreamEventUsage:
		if event.Usage.TotalTokens > 0 || event.Usage.PromptTokens > 0 || event.Usage.CompletionTokens > 0 {
			e.capture.usage = event.Usage.normalized()
		}
		return nil
	case canonicalStreamEventCompleted:
		return e.writeTerminal(event.FinishReason, true)
	case canonicalStreamEventIncomplete:
		return e.writeTerminal(event.FinishReason, false)
	case canonicalStreamEventError:
		e.capture.endReason = "upstream_stream_error"
		return fmt.Errorf("upstream stream failed: %s", nonEmptyString(event.ErrorMessage, "unknown error"))
	default:
		return nil
	}
}

func (e *googleGeminiStreamEncoder) writeCandidate(part map[string]any) error {
	return e.writePayload(map[string]any{
		"responseId":   e.responseID,
		"modelVersion": e.model,
		"candidates": []any{map[string]any{
			"content": map[string]any{"role": "model", "parts": []any{part}},
		}},
	})
}

func (e *googleGeminiStreamEncoder) writeTerminal(reason string, completed bool) error {
	if e.completed {
		return nil
	}
	if !gatewayUsageAvailable(e.capture.usage) {
		e.capture.endReason = "usage_missing"
		return fmt.Errorf("gemini stream completed without usage metadata")
	}
	payload := map[string]any{
		"responseId":   e.responseID,
		"modelVersion": e.model,
		"candidates": []any{map[string]any{
			"content":      map[string]any{"role": "model", "parts": []any{}},
			"finishReason": geminiFinishReason(reason),
		}},
		"usageMetadata": geminiUsageMetadata(e.capture.usage),
	}
	if err := e.writePayload(payload); err != nil {
		return err
	}
	e.completed = true
	e.capture.streamCompleted = completed
	e.capture.endReason = "done"
	return nil
}

func (e *googleGeminiStreamEncoder) writePayload(payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := e.w.Write([]byte("data: ")); err != nil {
		return err
	}
	if _, err := e.w.Write(encoded); err != nil {
		return err
	}
	if _, err := e.w.Write([]byte("\n\n")); err != nil {
		return err
	}
	if flusher, _ := e.w.(http.Flusher); flusher != nil {
		flusher.Flush()
	}
	return nil
}

func writeGeminiSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
}

func writeGeminiSSEPayload(w http.ResponseWriter, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
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
