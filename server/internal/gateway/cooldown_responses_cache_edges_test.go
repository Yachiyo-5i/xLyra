package gateway

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

func TestCooldownInputCoversCodexModelCredentialAndNoResponse(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			ID:       uuid.New(),
			SiteType: "codex",
		},
		Model: routeengine.CandidateModel{
			SiteModelID:  uuid.New(),
			UpstreamName: "gpt-upstream",
		},
	}
	credentialID := uuid.New()

	modelInput, ok := cooldownInputForFailure(candidate, gatewayAttemptResult{
		statusCode:         http.StatusBadGateway,
		errorType:          "upstream_model_error",
		cooldownScope:      "model",
		cooldownReason:     "model_busy",
		cooldownDuration:   7 * time.Second,
		upstreamStatusCode: http.StatusServiceUnavailable,
		retryAfterSeconds:  3,
		credentialMasked:   "sk-...123",
		credentialAttempt:  2,
		credentialTotal:    4,
	})
	if !ok {
		t.Fatal("expected codex model cooldown input")
	}
	if modelInput.Scope != "model" || modelInput.SiteModelID == nil || *modelInput.SiteModelID != candidate.Model.SiteModelID {
		t.Fatalf("model cooldown identity = %#v", modelInput)
	}
	if modelInput.Reason != "model_busy" || modelInput.Duration != 7*time.Second {
		t.Fatalf("model cooldown reason/duration = %q/%s", modelInput.Reason, modelInput.Duration)
	}
	if modelInput.Metadata["upstream_status_code"] != http.StatusServiceUnavailable || modelInput.Metadata["retry_after_seconds"] != int64(3) {
		t.Fatalf("model cooldown metadata = %#v", modelInput.Metadata)
	}

	credentialInput, ok := cooldownInputForFailure(candidate, gatewayAttemptResult{
		statusCode:       http.StatusBadGateway,
		errorType:        "upstream_credential_error",
		cooldownScope:    "credential",
		credentialID:     credentialID,
		cooldownDuration: time.Second,
	})
	if !ok {
		t.Fatal("expected codex credential cooldown input")
	}
	if credentialInput.Scope != "credential" || credentialInput.SiteCredentialID == nil || *credentialInput.SiteCredentialID != credentialID {
		t.Fatalf("credential cooldown identity = %#v", credentialInput)
	}
	if credentialInput.Reason != "upstream_credential_error" || credentialInput.Duration != time.Second {
		t.Fatalf("credential cooldown reason/duration = %q/%s", credentialInput.Reason, credentialInput.Duration)
	}

	noResponseInput, ok := cooldownInputForFailure(candidate, gatewayAttemptResult{
		statusCode: http.StatusGatewayTimeout,
		errorType:  "upstream_timeout",
	})
	if !ok {
		t.Fatal("expected upstream no-response cooldown input")
	}
	if noResponseInput.Scope != "model" || noResponseInput.Reason != "upstream_no_response" || noResponseInput.Duration != upstreamNoResponseCooldownDuration {
		t.Fatalf("no-response cooldown = %#v", noResponseInput)
	}
	if noResponseInput.Metadata["no_upstream_response"] != true {
		t.Fatalf("no-response metadata = %#v", noResponseInput.Metadata)
	}
}

func TestResponsesStreamPassthroughEmptyAndIncompleteEvents(t *testing.T) {
	t.Parallel()

	emptyCapture, started, err := proxyResponsesStreamPassthrough(
		context.Background(),
		httptest.NewRecorder(),
		gatewayStreamTestResponse(""),
		time.Now(),
	)
	if err != nil {
		t.Fatalf("empty passthrough returned error: %v", err)
	}
	if started {
		t.Fatal("empty stream should not write headers")
	}
	if emptyCapture.endReason != "upstream_stream_empty" {
		t.Fatalf("empty stream endReason = %q", emptyCapture.endReason)
	}

	rec := httptest.NewRecorder()
	body := gatewaySSEEvent("", `{"type":"response.incomplete"}`)
	capture, started, err := proxyResponsesStreamPassthrough(
		context.Background(),
		rec,
		&http.Response{
			StatusCode: http.StatusAccepted,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       gatewayStreamTestResponse(body).Body,
		},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("incomplete passthrough returned error: %v", err)
	}
	if !started || rec.Code != http.StatusAccepted {
		t.Fatalf("incomplete stream started=%v status=%d, want started 202", started, rec.Code)
	}
	if capture.endReason != "response_incomplete" {
		t.Fatalf("incomplete stream endReason = %q", capture.endReason)
	}
	if got := rec.Body.String(); got != body {
		t.Fatalf("passthrough body = %q, want %q", got, body)
	}
}

func TestBufferedResponsesStreamMergesFallbackTextAndPartialImage(t *testing.T) {
	t.Parallel()

	stream := gatewaySSEEvent("", `{"type":"response.output_text.delta","delta":"hello ","item_id":"msg_1"}`) +
		gatewaySSEEvent("", `{"type":"response.output_text.delta","delta":"world","item_id":"msg_1"}`) +
		gatewaySSEEvent("", `{"type":"response.image_generation_call.partial_image","partial_image_b64":"abc123","revised_prompt":"cat","output_index":1}`) +
		gatewaySSEEvent("", `{"type":"response.completed","response":{"id":"resp_1","output":[]}}`)

	body, err := readBufferedResponsesStreamBody(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("readBufferedResponsesStreamBody returned error: %v", err)
	}
	if !bytes.Contains(body, []byte(`"text":"hello world"`)) {
		t.Fatalf("buffered body missing merged text output: %s", body)
	}
	if bytes.Contains(body, []byte("abc123")) {
		t.Fatalf("partial image should not replace existing text output: %s", body)
	}
}

func TestModelsETagEmptyAndDefaultStringFallbacks(t *testing.T) {
	t.Parallel()

	if modelsETagMatches([]string{"*"}, "") {
		t.Fatal("empty etag should never match")
	}
	if got := defaultString("", "fallback"); got != "fallback" {
		t.Fatalf("defaultString empty = %q, want fallback", got)
	}
	if got := defaultString("value", "fallback"); got != "value" {
		t.Fatalf("defaultString value = %q, want value", got)
	}
	if shouldBufferAsResponsesStream("application/json", nil) {
		t.Fatal("nil reader with non-SSE content type should not buffer as responses stream")
	}
	if !shouldBufferAsResponsesStream("application/json", bufio.NewReader(strings.NewReader("data: {\"type\":"))) {
		t.Fatal("SSE-looking body should buffer as responses stream")
	}
}
