package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

type requestedModelContextKey struct{}

func withRequestedModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, requestedModelContextKey{}, strings.TrimSpace(model))
}

func requestedModelFromContext(ctx context.Context, fallback string) string {
	model, _ := ctx.Value(requestedModelContextKey{}).(string)
	if model != "" {
		return model
	}
	return strings.TrimSpace(fallback)
}

// Observe only protocol envelope fields, never model names embedded in generated content.
func upstreamResponseModel(body []byte) string {
	if looksLikeSSEBody(body) {
		capture := streamCaptureState{}
		for len(body) > 0 {
			line, remaining, _ := bytes.Cut(body, []byte{'\n'})
			observeUpstreamStreamModel(line, &capture)
			body = remaining
		}
		return capture.responseModel
	}
	return responseEnvelopeModel(body, 0)
}

func responseEnvelopeModel(body []byte, depth int) string {
	if depth > 2 {
		return ""
	}
	var envelope struct {
		Model        string          `json:"model"`
		ModelVersion string          `json:"modelVersion"`
		Response     json.RawMessage `json:"response"`
		Message      json.RawMessage `json:"message"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	for _, model := range []string{envelope.Model, envelope.ModelVersion} {
		if model = strings.TrimSpace(model); model != "" {
			return model
		}
	}
	for _, nested := range []json.RawMessage{envelope.Response, envelope.Message} {
		if model := responseEnvelopeModel(nested, depth+1); model != "" {
			return model
		}
	}
	return ""
}

func observeUpstreamStreamModel(line []byte, capture *streamCaptureState) {
	data, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
	if !ok {
		return
	}
	if model := responseEnvelopeModel(bytes.TrimSpace(data), 0); model != "" {
		capture.responseModel = model
	}
}
