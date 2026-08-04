package gateway

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type synchronizedStreamRecorder struct {
	mu      sync.Mutex
	header  http.Header
	status  int
	body    bytes.Buffer
	flushes int
}

func newSynchronizedStreamRecorder() *synchronizedStreamRecorder {
	return &synchronizedStreamRecorder{header: http.Header{}}
}

func (r *synchronizedStreamRecorder) Header() http.Header {
	return r.header
}

func (r *synchronizedStreamRecorder) WriteHeader(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = status
	}
}

func (r *synchronizedStreamRecorder) Write(body []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(body)
}

func (r *synchronizedStreamRecorder) Flush() {
	r.mu.Lock()
	r.flushes++
	r.mu.Unlock()
}

func (r *synchronizedStreamRecorder) snapshot() (int, string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status, r.body.String(), r.flushes
}

func TestRequestUsesDownstreamSSE(t *testing.T) {
	tests := []struct {
		name    string
		request gatewayRequest
		want    bool
	}{
		{name: "chat stream", request: gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Stream: true}, want: true},
		{name: "responses stream", request: gatewayRequest{DownstreamPath: gatewayEndpointResponses, Stream: true}, want: true},
		{name: "messages stream", request: gatewayRequest{DownstreamPath: gatewayEndpointMessages, Stream: true}, want: true},
		{name: "images stream", request: gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations, Stream: true}, want: true},
		{name: "audio sse", request: gatewayRequest{DownstreamPath: gatewayEndpointAudioSpeech, Stream: true, Payload: map[string]any{"stream_format": " SSE "}}, want: true},
		{name: "raw audio", request: gatewayRequest{DownstreamPath: gatewayEndpointAudioSpeech, Stream: true, Payload: map[string]any{"stream_format": "audio"}}},
		{name: "buffered chat", request: gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := requestUsesDownstreamSSE(test.request); got != test.want {
				t.Fatalf("requestUsesDownstreamSSE() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestDownstreamSSESessionSendsInitialHeartbeat(t *testing.T) {
	recorder := newSynchronizedStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	session := newDownstreamSSESessionWithIntervals(ctx, recorder, gatewayEndpointResponses, cancel, 20*time.Millisecond, 30*time.Millisecond)

	waitForStreamBody(t, recorder, ": xlyra-keepalive\n\n")
	session.Close()
	status, body, flushes := recorder.snapshot()
	if status != http.StatusOK || body != ": xlyra-keepalive\n\n" || flushes == 0 {
		t.Fatalf("status=%d body=%q flushes=%d", status, body, flushes)
	}
	if got := recorder.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q", got)
	}
}

func TestDownstreamSSESessionResetsIdleDeadlineAfterBusinessEvent(t *testing.T) {
	recorder := newSynchronizedStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	session := newDownstreamSSESessionWithIntervals(ctx, recorder, gatewayEndpointChatCompletions, cancel, 100*time.Millisecond, 35*time.Millisecond)
	if _, err := session.Write([]byte("data: {\"id\":\"one\"}\n\n")); err != nil {
		t.Fatalf("write business event: %v", err)
	}
	session.Flush()
	time.Sleep(15 * time.Millisecond)
	_, body, _ := recorder.snapshot()
	if strings.Contains(body, "xlyra-keepalive") {
		t.Fatalf("heartbeat arrived before idle deadline: %q", body)
	}
	waitForStreamBody(t, recorder, "xlyra-keepalive")
	session.Close()
}

func TestDownstreamSSESessionDoesNotInterruptPartialFrame(t *testing.T) {
	recorder := newSynchronizedStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	session := newDownstreamSSESessionWithIntervals(ctx, recorder, gatewayEndpointChatCompletions, cancel, 20*time.Millisecond, 20*time.Millisecond)
	if _, err := session.Write([]byte("data: ")); err != nil {
		t.Fatalf("write partial frame: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	_, body, _ := recorder.snapshot()
	if strings.Contains(body, "xlyra-keepalive") {
		t.Fatalf("heartbeat interrupted partial frame: %q", body)
	}
	if _, err := session.Write([]byte("{\"id\":\"one\"}\n\n")); err != nil {
		t.Fatalf("finish frame: %v", err)
	}
	waitForStreamBody(t, recorder, "xlyra-keepalive")
	session.Close()
}

func TestDownstreamSSESessionWritesProtocolFailureAfterHeartbeat(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: gatewayEndpointChatCompletions, want: "data: [DONE]"},
		{path: gatewayEndpointResponses, want: "event: error"},
		{path: gatewayEndpointMessages, want: `"code":"upstream_failed"`},
		{path: gatewayEndpointImagesGenerations, want: "event: error"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			recorder := newSynchronizedStreamRecorder()
			ctx, cancel := context.WithCancel(context.Background())
			session := newDownstreamSSESessionWithIntervals(ctx, recorder, test.path, cancel, 10*time.Millisecond, time.Second)
			waitForStreamBody(t, recorder, "xlyra-keepalive")
			if !session.WriteSSEFailure(downstreamSSEFailure{Code: "upstream_failed", Message: "all candidates failed"}) {
				t.Fatal("expected committed SSE failure")
			}
			session.Close()
			_, body, _ := recorder.snapshot()
			if !strings.Contains(body, test.want) {
				t.Fatalf("body %q does not contain %q", body, test.want)
			}
		})
	}
}

func TestWriteUpstreamFailurePreservesStructuredDetailsAfterHeartbeat(t *testing.T) {
	recorder := newSynchronizedStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	session := newDownstreamSSESessionWithIntervals(ctx, recorder, gatewayEndpointResponses, cancel, 10*time.Millisecond, time.Second)
	waitForStreamBody(t, recorder, "xlyra-keepalive")
	writeUpstreamFailure(session, gatewayAttemptResult{
		statusCode:        http.StatusTooManyRequests,
		contentType:       "application/json",
		body:              []byte(`{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"quota exhausted"}}`),
		errorType:         "upstream_http_error",
		errorMessage:      "upstream returned HTTP 429",
		retryAfterSeconds: 17,
	}, "req-structured")
	session.Close()
	_, body, _ := recorder.snapshot()
	for _, expected := range []string{"rate_limit_exceeded", "quota exhausted", "req-structured", `"retry_after_seconds":17`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("body %q does not contain %q", body, expected)
		}
	}
}

func TestDownstreamSSEHeartbeatDoesNotMarkEmptyUpstreamAsStarted(t *testing.T) {
	recorder := newSynchronizedStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	session := newDownstreamSSESessionWithIntervals(ctx, recorder, gatewayEndpointChatCompletions, cancel, 10*time.Millisecond, time.Second)
	waitForStreamBody(t, recorder, "xlyra-keepalive")

	capture, started, err := proxyUpstreamStream(ctx, session, gatewayStreamTestResponse(""), time.Now())
	if err != nil {
		t.Fatalf("proxy empty upstream: %v", err)
	}
	if started || capture.endReason != "upstream_stream_empty" {
		t.Fatalf("empty upstream started=%t endReason=%q", started, capture.endReason)
	}

	capture, started, err = proxyUpstreamStream(ctx, session, gatewayStreamTestResponse("data: [DONE]\n\n"), time.Now())
	if err != nil {
		t.Fatalf("proxy fallback upstream: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("fallback upstream started=%t completed=%t", started, capture.streamCompleted)
	}
	session.Close()
}

func TestDownstreamSSESessionStopsHeartbeatAfterFinish(t *testing.T) {
	recorder := newSynchronizedStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	session := newDownstreamSSESessionWithIntervals(ctx, recorder, gatewayEndpointChatCompletions, cancel, 100*time.Millisecond, 20*time.Millisecond)
	if _, err := session.Write([]byte("data: [DONE]\n\n")); err != nil {
		t.Fatalf("write terminal event: %v", err)
	}
	session.FinishSSE()
	time.Sleep(60 * time.Millisecond)
	session.Close()
	_, body, _ := recorder.snapshot()
	if strings.Contains(body, "xlyra-keepalive") {
		t.Fatalf("heartbeat followed terminal event: %q", body)
	}
}

func waitForStreamBody(t *testing.T, recorder *synchronizedStreamRecorder, target string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, body, _ := recorder.snapshot()
		if strings.Contains(body, target) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	_, body, _ := recorder.snapshot()
	t.Fatalf("stream body %q did not contain %q", body, target)
}
