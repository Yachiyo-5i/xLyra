package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"xlyra/server/internal/auth"
)

type capturedResponsesWebSocket struct {
	messages [][]byte
}

func (c *capturedResponsesWebSocket) Write(_ context.Context, messageType websocket.MessageType, payload []byte) error {
	if messageType != websocket.MessageText {
		return nil
	}
	c.messages = append(c.messages, append([]byte(nil), payload...))
	return nil
}

type scriptedResponsesWebSocket struct {
	reads      chan responsesWebSocketMessage
	readErr    error
	messagesMu sync.Mutex
	messages   [][]byte
	closed     chan struct{}
	closeOnce  sync.Once
}

func (c *scriptedResponsesWebSocket) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case message, ok := <-c.reads:
		if !ok {
			return 0, nil, c.readErr
		}
		return message.messageType, message.data, nil
	}
}

func (c *scriptedResponsesWebSocket) Write(_ context.Context, messageType websocket.MessageType, payload []byte) error {
	if messageType == websocket.MessageText {
		c.messagesMu.Lock()
		c.messages = append(c.messages, append([]byte(nil), payload...))
		c.messagesMu.Unlock()
	}
	return nil
}

func (c *scriptedResponsesWebSocket) Ping(context.Context) error {
	return nil
}

func (c *scriptedResponsesWebSocket) Close(websocket.StatusCode, string) error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func TestResponsesWebSocketStateExpandsPreviousResponse(t *testing.T) {
	state := responsesWebSocketState{}
	initialInput := []any{map[string]any{"type": "message", "role": "user", "content": "first"}}
	initialOutput := []any{map[string]any{"type": "message", "role": "assistant", "content": "answer"}}
	state.update("resp_1", initialInput, initialOutput, map[string]any{"tools": []any{map[string]any{"type": "function", "name": "lookup"}}})

	expanded, hadPrevious, mode, err := state.expand(map[string]any{
		"previous_response_id": "resp_1",
		"input": []any{
			map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "done"},
		},
	})
	if err != nil {
		t.Fatalf("expand previous response: %v", err)
	}
	if !hadPrevious || mode != "previous_response_expanded" {
		t.Fatalf("hadPrevious=%v mode=%q", hadPrevious, mode)
	}
	if len(expanded) != 3 {
		t.Fatalf("expanded item count = %d, want 3", len(expanded))
	}
}

func TestResponsesWebSocketStateRejectsUnknownPreviousResponse(t *testing.T) {
	state := responsesWebSocketState{}
	state.update("resp_1", []any{}, []any{}, nil)

	_, hadPrevious, mode, err := state.expand(map[string]any{
		"previous_response_id": "resp_missing",
		"input":                []any{},
	})
	if err == nil {
		t.Fatal("expected missing previous response error")
	}
	if !hadPrevious || mode != "previous_response_not_found" {
		t.Fatalf("hadPrevious=%v mode=%q", hadPrevious, mode)
	}
}

func TestResponsesWebSocketResponseWriterForwardsSSEEvents(t *testing.T) {
	conn := &capturedResponsesWebSocket{}
	state := responsesWebSocketState{maxBytes: 1 << 20}
	writer := newResponsesWebSocketResponseWriter(context.Background(), conn, &sync.Mutex{}, &state, []any{"input"}, map[string]any{"instructions": "be concise"}, "req_1")
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.WriteHeader(http.StatusOK)

	first := "event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}"
	second := "\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}}\n\n"
	if _, err := writer.Write([]byte(first)); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}
	if _, err := writer.Write([]byte(second)); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}
	if err := writer.finalize("req_1"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if len(conn.messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(conn.messages))
	}
	if !writer.completed || writer.responseID != "resp_1" || len(writer.output) != 1 {
		t.Fatalf("completed=%v responseID=%q output=%d", writer.completed, writer.responseID, len(writer.output))
	}
	if state.latest == nil || state.latest.Config["instructions"] != "be concise" {
		t.Fatalf("state = %#v", state.latest)
	}
}

func TestResponsesWebSocketResponseWriterRejectsCompletedWhenStateCommitFails(t *testing.T) {
	conn := &capturedResponsesWebSocket{}
	state := responsesWebSocketState{maxBytes: 1}
	writer := newResponsesWebSocketResponseWriter(context.Background(), conn, &sync.Mutex{}, &state, []any{"input"}, nil, "req_1")
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	completed := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[],\"usage\":{\"input_tokens\":11,\"output_tokens\":7}}}\n\n"
	if _, err := writer.Write([]byte(completed)); err == nil {
		t.Fatal("expected state commit error")
	} else {
		var stateErr *responsesWebSocketStateCommitError
		if !errors.As(err, &stateErr) {
			t.Fatalf("error = %T %v", err, err)
		}
		if stateErr.usage.PromptTokens != 11 || stateErr.usage.CompletionTokens != 7 {
			t.Fatalf("usage = %#v", stateErr.usage)
		}
	}
	if len(conn.messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(conn.messages))
	}
	var event map[string]any
	if err := json.Unmarshal(conn.messages[0], &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event["type"] != "error" {
		t.Fatalf("event = %#v", event)
	}
}

func TestResponsesWebSocketExecutionBodyChecksCompletePayloadSize(t *testing.T) {
	payload := map[string]any{
		"type":         "response.create",
		"model":        "gpt-5",
		"input":        []any{},
		"instructions": "this field makes the complete payload larger than the expanded input",
	}
	executionPayload := responsesWebSocketExecutionPayload(payload, nil, []any{})
	if _, err := responsesWebSocketExecutionBody(executionPayload, 40); err == nil {
		t.Fatal("expected complete payload size error")
	}
}

func TestResponsesWebSocketExecutionPayloadInheritsWarmupConfig(t *testing.T) {
	inherited := map[string]any{
		"model":        "gpt-5",
		"instructions": "use the prepared tools",
		"tools":        []any{map[string]any{"type": "function", "name": "lookup"}},
	}
	payload := map[string]any{
		"type":                 "response.create",
		"model":                "gpt-5",
		"previous_response_id": "resp_warmup",
		"input":                []any{map[string]any{"type": "message", "role": "user", "content": "hello"}},
	}
	executionPayload := responsesWebSocketExecutionPayload(payload, inherited, payload["input"].([]any))
	if executionPayload["instructions"] != "use the prepared tools" {
		t.Fatalf("instructions = %#v", executionPayload["instructions"])
	}
	tools, ok := executionPayload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", executionPayload["tools"])
	}
	if _, exists := executionPayload["previous_response_id"]; exists {
		t.Fatalf("execution payload = %#v", executionPayload)
	}
}

func TestResponsesWebSocketControlLimiterThresholds(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newResponsesWebSocketControlLimiter(now)
	for i := 0; i < responsesWebSocketControlBurst; i++ {
		if !limiter.allow(now) {
			t.Fatalf("burst request %d rejected", i+1)
		}
	}
	if limiter.allow(now) {
		t.Fatal("expected burst limit rejection")
	}
	if !limiter.allow(now.Add(time.Second)) || !limiter.allow(now.Add(time.Second)) || limiter.allow(now.Add(time.Second)) {
		t.Fatal("unexpected token refill behavior")
	}
	for i := 1; i < responsesWebSocketMaxInvalid; i++ {
		if limiter.invalid() {
			t.Fatalf("invalid event %d closed too early", i)
		}
	}
	if !limiter.invalid() {
		t.Fatal("expected invalid event threshold")
	}
	limiter.valid()
	for i := 0; i < responsesWebSocketMaxWarmups; i++ {
		if !limiter.warmup() {
			t.Fatalf("warmup %d rejected", i+1)
		}
	}
	if limiter.warmup() {
		t.Fatal("expected consecutive warmup rejection")
	}
	limiter.generated()
	if !limiter.warmup() {
		t.Fatal("expected generated turn to reset warmups")
	}
}

func TestResponsesWebSocketReadLoopCancelsOnClientClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &scriptedResponsesWebSocket{
		reads:   make(chan responsesWebSocketMessage),
		readErr: errors.New("client closed"),
		closed:  make(chan struct{}),
	}
	close(conn.reads)
	turnSlots := make(chan struct{}, 2)
	turnSlots <- struct{}{}
	turnSlots <- struct{}{}
	go responsesWebSocketReadLoop(ctx, cancel, conn, make(chan responsesWebSocketMessage, 2), turnSlots, &sync.Mutex{})
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("connection context was not canceled")
	}
}

func TestResponsesWebSocketReadLoopRejectsQueueOverflow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &scriptedResponsesWebSocket{
		reads:  make(chan responsesWebSocketMessage, 3),
		closed: make(chan struct{}),
	}
	conn.reads <- responsesWebSocketMessage{messageType: websocket.MessageText, data: []byte(`{"type":"response.create"}`)}
	conn.reads <- responsesWebSocketMessage{messageType: websocket.MessageText, data: []byte(`{"type":"response.create"}`)}
	conn.reads <- responsesWebSocketMessage{messageType: websocket.MessageText, data: []byte(`{"type":"response.create"}`)}
	turnSlots := make(chan struct{}, 2)
	turnSlots <- struct{}{}
	turnSlots <- struct{}{}
	go responsesWebSocketReadLoop(ctx, cancel, conn, make(chan responsesWebSocketMessage, 2), turnSlots, &sync.Mutex{})
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("connection was not closed after queue overflow")
	}
	conn.messagesMu.Lock()
	defer conn.messagesMu.Unlock()
	if len(conn.messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(conn.messages))
	}
	var event map[string]any
	if err := json.Unmarshal(conn.messages[0], &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	errorPayload, _ := event["error"].(map[string]any)
	if errorPayload["code"] != "too_many_pending_requests" {
		t.Fatalf("event = %#v", event)
	}
}

func TestResponsesWebSocketLifetimeLoopWritesLimitEvent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	conn := &scriptedResponsesWebSocket{
		reads:  make(chan responsesWebSocketMessage),
		closed: make(chan struct{}),
	}
	done := make(chan struct{})
	go responsesWebSocketLifetimeLoop(ctx, conn, &sync.Mutex{}, done)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lifetime loop did not finish")
	}
	conn.messagesMu.Lock()
	defer conn.messagesMu.Unlock()
	if len(conn.messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(conn.messages))
	}
	var event map[string]any
	if err := json.Unmarshal(conn.messages[0], &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	errorPayload, _ := event["error"].(map[string]any)
	if errorPayload["code"] != "websocket_connection_limit_reached" {
		t.Fatalf("event = %#v", event)
	}
}

func TestResponsesWebSocketResponseWriterWrapsBufferedError(t *testing.T) {
	conn := &capturedResponsesWebSocket{}
	writer := newResponsesWebSocketResponseWriter(context.Background(), conn, &sync.Mutex{}, nil, nil, nil, "req_1")
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusBadRequest)
	if _, err := writer.Write([]byte(`{"error":{"code":"invalid_model","message":"bad model"}}`)); err != nil {
		t.Fatalf("write error: %v", err)
	}
	if err := writer.finalize("req_1"); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if len(conn.messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(conn.messages))
	}
	var event map[string]any
	if err := json.Unmarshal(conn.messages[0], &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event["type"] != "error" || int(event["status"].(float64)) != http.StatusBadRequest {
		t.Fatalf("event = %#v", event)
	}
}

func TestResponsesWebSocketQuotaErrorPayload(t *testing.T) {
	resetAt := time.Date(2026, 7, 23, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	event := responsesWebSocketQuotaErrorPayload(auth.APIKeyQuotaFailure{
		Code:    "api_key_daily_quota_exhausted",
		Message: "API key daily quota has been exhausted.",
		Scope:   "daily",
		ResetAt: &resetAt,
	}, "req-quota")
	if event["type"] != "error" || event["status"] != http.StatusUnauthorized {
		t.Fatalf("event = %#v", event)
	}
	errorPayload, ok := event["error"].(map[string]any)
	if !ok || errorPayload["type"] != "authentication_error" || errorPayload["code"] != "api_key_daily_quota_exhausted" || errorPayload["scope"] != "daily" {
		t.Fatalf("error payload = %#v", event["error"])
	}
	if errorPayload["reset_at"] != resetAt.Format(time.RFC3339) || errorPayload["request_id"] != "req-quota" || errorPayload["param"] != nil {
		t.Fatalf("error metadata = %#v", errorPayload)
	}
}

func TestAttemptMetadataIncludesResponsesWebSocketFields(t *testing.T) {
	ctx := withResponsesWebSocketMetadata(context.Background(), responsesWebSocketMetadata{
		ConnectionID:     "ws_1",
		TurnIndex:        2,
		ConnectionReused: true,
		StateMode:        "previous_response_expanded",
		HadPrevious:      true,
		ConnectionAgeMS:  1250,
	})
	metadata := map[string]any{}
	applyResponsesWebSocketMetadata(metadata, ctx)
	if metadata["downstream_transport"] != "websocket" || metadata["websocket_connection_id"] != "ws_1" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestRequestFailureMetadataIncludesTransport(t *testing.T) {
	metadata := requestFailureMetadata(context.Background(), "req_1", uuid.Nil, http.StatusBadRequest, "bad_request", "bad request", "gpt-5", false, "validate", gatewayEndpointResponses)
	if metadata["downstream_transport"] != "http" || metadata["upstream_transport"] != nil {
		t.Fatalf("metadata = %#v", metadata)
	}

	ctx := withResponsesWebSocketMetadata(context.Background(), responsesWebSocketMetadata{ConnectionID: "ws_1", TurnIndex: 1})
	metadata = requestFailureMetadata(ctx, "req_2", uuid.Nil, http.StatusBadRequest, "bad_request", "bad request", "gpt-5", true, "validate", gatewayEndpointResponses)
	if metadata["downstream_transport"] != "websocket" || metadata["upstream_transport"] != nil {
		t.Fatalf("metadata = %#v", metadata)
	}
}
