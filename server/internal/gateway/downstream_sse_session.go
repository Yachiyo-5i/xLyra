package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	downstreamSSEInitialDelay = 5 * time.Second
	downstreamSSEIdleInterval = 8 * time.Second
	downstreamSSERetryDelay   = time.Second
)

type downstreamSSESession struct {
	mu sync.Mutex

	dst       http.ResponseWriter
	header    http.Header
	path      string
	cancel    context.CancelFunc
	committed bool
	heartbeat bool
	terminal  bool
	writeErr  error
	boundary  bool
	tail      []byte
	nextWrite time.Time

	idleInterval time.Duration
	activity     chan struct{}
	stop         chan struct{}
	done         chan struct{}
	closeOnce    sync.Once
}

func newDownstreamSSESession(ctx context.Context, dst http.ResponseWriter, path string, cancel context.CancelFunc) *downstreamSSESession {
	return newDownstreamSSESessionWithIntervals(ctx, dst, path, cancel, downstreamSSEInitialDelay, downstreamSSEIdleInterval)
}

func newDownstreamSSESessionWithIntervals(
	ctx context.Context,
	dst http.ResponseWriter,
	path string,
	cancel context.CancelFunc,
	initialDelay time.Duration,
	idleInterval time.Duration,
) *downstreamSSESession {
	if initialDelay <= 0 {
		initialDelay = downstreamSSEInitialDelay
	}
	if idleInterval <= 0 {
		idleInterval = downstreamSSEIdleInterval
	}
	header := dst.Header().Clone()
	setDownstreamSSEHeaders(header)
	setDownstreamSSEHeaders(dst.Header())
	session := &downstreamSSESession{
		dst:          dst,
		header:       header,
		path:         path,
		cancel:       cancel,
		boundary:     true,
		nextWrite:    time.Now().Add(initialDelay),
		idleInterval: idleInterval,
		activity:     make(chan struct{}, 1),
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
	go session.run(ctx)
	return session
}

func requestUsesDownstreamSSE(request gatewayRequest) bool {
	if !request.Stream {
		return false
	}
	switch request.DownstreamPath {
	case gatewayEndpointChatCompletions, gatewayEndpointResponses, gatewayEndpointMessages,
		gatewayEndpointImagesGenerations, gatewayEndpointImagesEdits:
		return true
	case gatewayEndpointAudioSpeech:
		return strings.EqualFold(strings.TrimSpace(stringFromMapAny(request.Payload, "stream_format")), "sse")
	default:
		return false
	}
}

func setDownstreamSSEHeaders(header http.Header) {
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
}

func (s *downstreamSSESession) Header() http.Header {
	return s.header
}

func (s *downstreamSSESession) WriteHeader(statusCode int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.committed || s.writeErr != nil {
		return
	}
	if statusCode >= 200 && statusCode < 300 {
		setDownstreamSSEHeaders(s.header)
	}
	copyResponseHeaders(s.dst.Header(), s.header)
	s.dst.WriteHeader(statusCode)
	s.committed = true
}

func (s *downstreamSSESession) Write(body []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	if !s.committed {
		setDownstreamSSEHeaders(s.header)
		copyResponseHeaders(s.dst.Header(), s.header)
		s.dst.WriteHeader(http.StatusOK)
		s.committed = true
	}
	written, err := s.dst.Write(body)
	if err != nil {
		s.failLocked(err)
		return written, err
	}
	s.trackBoundaryLocked(body)
	s.nextWrite = time.Now().Add(s.idleInterval)
	s.signalActivityLocked()
	return written, nil
}

func (s *downstreamSSESession) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if flusher, ok := s.dst.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *downstreamSSESession) Unwrap() http.ResponseWriter {
	return s.dst
}

func (s *downstreamSSESession) WriteSSEFailure(failure downstreamSSEFailure) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.heartbeat {
		return false
	}
	if s.terminal || s.writeErr != nil {
		return true
	}
	body := downstreamSSEFailureBody(s.path, failure)
	if _, err := s.dst.Write(body); err != nil {
		s.failLocked(err)
		return true
	}
	if flusher, ok := s.dst.(http.Flusher); ok {
		flusher.Flush()
	}
	s.terminal = true
	s.boundary = true
	return true
}

func (s *downstreamSSESession) FinishSSE() {
	s.mu.Lock()
	s.terminal = true
	s.mu.Unlock()
}

func (s *downstreamSSESession) Close() {
	s.closeOnce.Do(func() {
		close(s.stop)
		<-s.done
	})
}

func (s *downstreamSSESession) run(ctx context.Context) {
	defer close(s.done)
	timer := time.NewTimer(s.delayUntilNextWrite())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-s.activity:
			resetTimer(timer, s.delayUntilNextWrite())
		case <-timer.C:
			s.writeHeartbeat()
			resetTimer(timer, s.delayUntilNextWrite())
		}
	}
}

func (s *downstreamSSESession) writeHeartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal || s.writeErr != nil {
		s.nextWrite = time.Now().Add(s.idleInterval)
		return
	}
	if time.Now().Before(s.nextWrite) {
		return
	}
	if !s.boundary {
		s.nextWrite = time.Now().Add(downstreamSSERetryDelay)
		return
	}
	if !s.committed {
		setDownstreamSSEHeaders(s.dst.Header())
		s.dst.WriteHeader(http.StatusOK)
		s.committed = true
	}
	if _, err := s.dst.Write([]byte(": xlyra-keepalive\n\n")); err != nil {
		s.failLocked(err)
		return
	}
	if flusher, ok := s.dst.(http.Flusher); ok {
		flusher.Flush()
	}
	s.heartbeat = true
	s.nextWrite = time.Now().Add(s.idleInterval)
}

func (s *downstreamSSESession) delayUntilNextWrite() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	delay := time.Until(s.nextWrite)
	if delay < 0 {
		return 0
	}
	return delay
}

func (s *downstreamSSESession) trackBoundaryLocked(body []byte) {
	combined := make([]byte, 0, len(s.tail)+len(body))
	combined = append(combined, s.tail...)
	combined = append(combined, body...)
	lastEnd := -1
	for _, delimiter := range [][]byte{[]byte("\n\n"), []byte("\r\n\r\n"), []byte("\r\r")} {
		if index := bytes.LastIndex(combined, delimiter); index >= 0 {
			end := index + len(delimiter)
			if end > lastEnd {
				lastEnd = end
			}
		}
	}
	s.boundary = lastEnd == len(combined)
	if len(combined) > 3 {
		s.tail = append(s.tail[:0], combined[len(combined)-3:]...)
	} else {
		s.tail = append(s.tail[:0], combined...)
	}
}

func (s *downstreamSSESession) signalActivityLocked() {
	select {
	case s.activity <- struct{}{}:
	default:
	}
}

func (s *downstreamSSESession) failLocked(err error) {
	s.writeErr = err
	if s.cancel != nil {
		s.cancel()
	}
}

func copyResponseHeaders(dst http.Header, src http.Header) {
	for key := range dst {
		dst.Del(key)
	}
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func resetTimer(timer *time.Timer, delay time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func downstreamSSEFailureBody(path string, failure downstreamSSEFailure) []byte {
	errorPayload := map[string]any{"code": failure.Code, "message": failure.Message}
	if failure.RequestID != "" {
		errorPayload["request_id"] = failure.RequestID
	}
	if failure.RetryAfterSeconds > 0 {
		errorPayload["retry_after_seconds"] = failure.RetryAfterSeconds
	}
	var event string
	switch path {
	case gatewayEndpointResponses:
		event = "event: error\ndata: " + marshalSSEPayload(map[string]any{"type": "error", "error": errorPayload}) + "\n\n"
	case gatewayEndpointMessages:
		anthropicError := make(map[string]any, len(errorPayload)+1)
		for key, value := range errorPayload {
			anthropicError[key] = value
		}
		anthropicError["type"] = "api_error"
		event = "event: error\ndata: " + marshalSSEPayload(map[string]any{
			"type":  "error",
			"error": anthropicError,
		}) + "\n\n"
	case gatewayEndpointImagesGenerations, gatewayEndpointImagesEdits, gatewayEndpointAudioSpeech:
		event = "event: error\ndata: " + marshalSSEPayload(map[string]any{"type": "error", "error": errorPayload}) + "\n\n"
	default:
		event = "data: " + marshalSSEPayload(map[string]any{"error": errorPayload}) + "\n\ndata: [DONE]\n\n"
	}
	return []byte(event)
}

func marshalSSEPayload(payload map[string]any) string {
	encoded, _ := json.Marshal(payload)
	return string(encoded)
}

type downstreamSSEFailure struct {
	Code              string
	Message           string
	RequestID         string
	RetryAfterSeconds int64
}

type downstreamSSEFailureWriter interface {
	WriteSSEFailure(failure downstreamSSEFailure) bool
}

type downstreamSSELifecycleWriter interface {
	FinishSSE()
}
