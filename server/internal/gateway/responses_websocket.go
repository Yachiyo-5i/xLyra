package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/store"
)

const (
	responsesWebSocketLifetime     = 60 * time.Minute
	responsesWebSocketPingInterval = 30 * time.Second
	responsesWebSocketWriteTimeout = 10 * time.Second
	responsesWebSocketControlRate  = 120
	responsesWebSocketControlBurst = 16
	responsesWebSocketMaxInvalid   = 5
	responsesWebSocketMaxWarmups   = 3
)

type responsesWebSocketConnection interface {
	responsesWebSocketEventWriter
	Read(context.Context) (websocket.MessageType, []byte, error)
	Ping(context.Context) error
	Close(websocket.StatusCode, string) error
}

type responsesWebSocketMessage struct {
	messageType websocket.MessageType
	data        []byte
}

type responsesWebSocketControlLimiter struct {
	tokens       float64
	lastRefill   time.Time
	invalidCount int
	warmupCount  int
}

func newResponsesWebSocketControlLimiter(now time.Time) *responsesWebSocketControlLimiter {
	return &responsesWebSocketControlLimiter{tokens: responsesWebSocketControlBurst, lastRefill: now}
}

func (l *responsesWebSocketControlLimiter) allow(now time.Time) bool {
	elapsed := now.Sub(l.lastRefill).Seconds()
	if elapsed > 0 {
		l.tokens += elapsed * float64(responsesWebSocketControlRate) / 60
		if l.tokens > responsesWebSocketControlBurst {
			l.tokens = responsesWebSocketControlBurst
		}
		l.lastRefill = now
	}
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

func (l *responsesWebSocketControlLimiter) invalid() bool {
	l.invalidCount++
	return l.invalidCount >= responsesWebSocketMaxInvalid
}

func (l *responsesWebSocketControlLimiter) valid() {
	l.invalidCount = 0
}

func (l *responsesWebSocketControlLimiter) warmup() bool {
	l.warmupCount++
	return l.warmupCount <= responsesWebSocketMaxWarmups
}

func (l *responsesWebSocketControlLimiter) generated() {
	l.warmupCount = 0
}

type responsesWebSocketMetadata struct {
	ConnectionID     string
	TurnIndex        int
	ConnectionReused bool
	StateMode        string
	HadPrevious      bool
	ConnectionAgeMS  int64
}

type responsesWebSocketMetadataContextKey struct{}

func withResponsesWebSocketMetadata(ctx context.Context, metadata responsesWebSocketMetadata) context.Context {
	return context.WithValue(ctx, responsesWebSocketMetadataContextKey{}, metadata)
}

func responsesWebSocketMetadataFromContext(ctx context.Context) (responsesWebSocketMetadata, bool) {
	metadata, ok := ctx.Value(responsesWebSocketMetadataContextKey{}).(responsesWebSocketMetadata)
	return metadata, ok
}

type responsesWebSocketTurnState struct {
	ResponseID string
	Input      []any
	Output     []any
	Config     map[string]any
}

type responsesWebSocketState struct {
	latest   *responsesWebSocketTurnState
	maxBytes int64
}

func (s *responsesWebSocketState) expand(payload map[string]any) ([]any, bool, string, error) {
	input, err := responsesWebSocketInputItems(payload["input"])
	if err != nil {
		return nil, false, "", err
	}
	previousID := strings.TrimSpace(anyString(payload["previous_response_id"]))
	if previousID == "" {
		return cloneResponsesWebSocketItems(input), false, "new_chain", nil
	}
	if s.latest == nil || previousID != s.latest.ResponseID {
		return nil, true, "previous_response_not_found", fmt.Errorf("previous response with id %q not found", previousID)
	}
	expanded := make([]any, 0, len(s.latest.Input)+len(s.latest.Output)+len(input))
	expanded = append(expanded, cloneResponsesWebSocketItems(s.latest.Input)...)
	expanded = append(expanded, cloneResponsesWebSocketItems(s.latest.Output)...)
	expanded = append(expanded, cloneResponsesWebSocketItems(input)...)
	return expanded, true, "previous_response_expanded", nil
}

func (s *responsesWebSocketState) update(responseID string, input []any, output []any, config map[string]any) bool {
	state := &responsesWebSocketTurnState{
		ResponseID: responseID,
		Input:      cloneResponsesWebSocketItems(input),
		Output:     cloneResponsesWebSocketItems(output),
		Config:     cloneResponsesWebSocketPayload(config),
	}
	if s.maxBytes > 0 {
		body, err := json.Marshal(state)
		if err != nil || int64(len(body)) > s.maxBytes {
			s.latest = nil
			return false
		}
	}
	s.latest = state
	return true
}

func (s *responsesWebSocketState) evict() {
	s.latest = nil
}

func responsesWebSocketInputItems(value any) ([]any, error) {
	switch input := value.(type) {
	case nil:
		return []any{}, nil
	case []any:
		return input, nil
	case string:
		return []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": input},
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("input must be a string or an array of response items")
	}
}

func cloneResponsesWebSocketItems(items []any) []any {
	if len(items) == 0 {
		return []any{}
	}
	body, err := json.Marshal(items)
	if err != nil {
		return append([]any(nil), items...)
	}
	var cloned []any
	if err := json.Unmarshal(body, &cloned); err != nil {
		return append([]any(nil), items...)
	}
	return cloned
}

type responsesWebSocketEventWriter interface {
	Write(context.Context, websocket.MessageType, []byte) error
}

type responsesWebSocketResponseWriter struct {
	ctx         context.Context
	conn        responsesWebSocketEventWriter
	header      http.Header
	statusCode  int
	pending     bytes.Buffer
	buffered    bytes.Buffer
	writeErr    error
	terminal    bool
	completed   bool
	responseID  string
	output      []any
	outputByID  map[string]struct{}
	writeMu     *sync.Mutex
	state       *responsesWebSocketState
	stateInput  []any
	stateConfig map[string]any
	requestID   string
}

type responsesWebSocketStateCommitError struct {
	message string
	usage   gatewayUsage
}

func (e *responsesWebSocketStateCommitError) Error() string {
	return e.message
}

type responsesWebSocketClientWriteError struct {
	err   error
	usage gatewayUsage
}

func (e *responsesWebSocketClientWriteError) Error() string {
	return e.err.Error()
}

func (e *responsesWebSocketClientWriteError) Unwrap() error {
	return e.err
}

func newResponsesWebSocketResponseWriter(ctx context.Context, conn responsesWebSocketEventWriter, writeMu *sync.Mutex, state *responsesWebSocketState, stateInput []any, stateConfig map[string]any, requestID string) *responsesWebSocketResponseWriter {
	return &responsesWebSocketResponseWriter{
		ctx:         ctx,
		conn:        conn,
		header:      http.Header{},
		statusCode:  http.StatusOK,
		outputByID:  map[string]struct{}{},
		writeMu:     writeMu,
		state:       state,
		stateInput:  cloneResponsesWebSocketItems(stateInput),
		stateConfig: cloneResponsesWebSocketPayload(stateConfig),
		requestID:   requestID,
	}
}

func (w *responsesWebSocketResponseWriter) Header() http.Header {
	return w.header
}

func (w *responsesWebSocketResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *responsesWebSocketResponseWriter) Flush() {}

func (w *responsesWebSocketResponseWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	if !strings.Contains(strings.ToLower(w.header.Get("Content-Type")), "text/event-stream") {
		w.buffered.Write(p)
		return len(p), nil
	}
	w.pending.Write(p)
	for {
		block, ok := takeResponsesWebSocketSSEBlock(&w.pending)
		if !ok {
			break
		}
		if err := w.forwardSSEBlock(block); err != nil {
			w.writeErr = err
			return 0, err
		}
	}
	return len(p), nil
}

func takeResponsesWebSocketSSEBlock(pending *bytes.Buffer) ([]byte, bool) {
	raw := pending.Bytes()
	lf := bytes.Index(raw, []byte("\n\n"))
	crlf := bytes.Index(raw, []byte("\r\n\r\n"))
	end := lf
	separator := 2
	if crlf >= 0 && (end < 0 || crlf < end) {
		end = crlf
		separator = 4
	}
	if end < 0 {
		return nil, false
	}
	block := append([]byte(nil), raw[:end]...)
	pending.Next(end + separator)
	return block, true
}

func (w *responsesWebSocketResponseWriter) forwardSSEBlock(block []byte) error {
	block = bytes.ReplaceAll(block, []byte("\r\n"), []byte("\n"))
	data := make([]byte, 0, len(block))
	for _, line := range bytes.Split(block, []byte("\n")) {
		value, ok := bytes.CutPrefix(line, []byte("data:"))
		if !ok {
			continue
		}
		value = bytes.TrimSpace(value)
		if len(data) > 0 {
			data = append(data, '\n')
		}
		data = append(data, value...)
	}
	if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
		return nil
	}
	return w.forwardJSONEvent(data)
}

func (w *responsesWebSocketResponseWriter) forwardJSONEvent(data []byte) error {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("encode responses websocket event: %w", err)
	}
	eventType := strings.TrimSpace(anyString(payload["type"]))
	eventUsage := gatewayUsage{}
	if eventType == "response.completed" {
		var event responsesStreamEvent
		_ = json.Unmarshal(data, &event)
		if event.Response != nil && event.Response.Usage != nil {
			eventUsage = completionUsageFromResponsesUsage(event.Response.Usage)
		}
	}
	switch eventType {
	case "response.output_item.done":
		if item, ok := payload["item"].(map[string]any); ok {
			w.captureOutputItem(item)
		}
	case "response.completed":
		w.terminal = true
		w.completed = true
		if response, ok := payload["response"].(map[string]any); ok {
			w.responseID = strings.TrimSpace(anyString(response["id"]))
			if output, ok := response["output"].([]any); ok && len(output) > 0 {
				w.output = cloneResponsesWebSocketItems(output)
			}
		}
		if w.state != nil && w.responseID != "" && !w.state.update(w.responseID, w.stateInput, w.output, w.stateConfig) {
			message := "websocket response state exceeds the maximum allowed size"
			if err := w.writeError(http.StatusRequestEntityTooLarge, "websocket_state_too_large", message, w.requestID); err != nil {
				return err
			}
			return &responsesWebSocketStateCommitError{message: message, usage: eventUsage}
		}
	case "response.failed", "response.incomplete", "response.error", "error":
		w.terminal = true
	}
	if err := w.writeEvent(data); err != nil {
		var clientWriteErr *responsesWebSocketClientWriteError
		if errors.As(err, &clientWriteErr) {
			clientWriteErr.usage = eventUsage
		}
		return err
	}
	return nil
}

func (w *responsesWebSocketResponseWriter) captureOutputItem(item map[string]any) {
	id := strings.TrimSpace(anyString(item["id"]))
	if id != "" {
		if _, exists := w.outputByID[id]; exists {
			return
		}
		w.outputByID[id] = struct{}{}
	}
	cloned := cloneResponsesWebSocketItems([]any{item})
	if len(cloned) == 1 {
		w.output = append(w.output, cloned[0])
	}
}

func (w *responsesWebSocketResponseWriter) finalize(requestID string) error {
	if w.writeErr != nil {
		return w.writeErr
	}
	if w.pending.Len() > 0 {
		block := append([]byte(nil), w.pending.Bytes()...)
		w.pending.Reset()
		if err := w.forwardSSEBlock(block); err != nil {
			return err
		}
	}
	if w.terminal {
		return nil
	}
	if w.buffered.Len() > 0 {
		return w.forwardBufferedError(requestID)
	}
	return w.writeError(http.StatusBadGateway, "upstream_stream_incomplete", "upstream stream ended before a terminal response event", requestID)
}

func (w *responsesWebSocketResponseWriter) forwardBufferedError(requestID string) error {
	var payload map[string]any
	if err := json.Unmarshal(w.buffered.Bytes(), &payload); err != nil {
		return w.writeError(w.statusCode, "gateway_error", strings.TrimSpace(w.buffered.String()), requestID)
	}
	errorPayload, _ := payload["error"].(map[string]any)
	code := strings.TrimSpace(anyString(errorPayload["code"]))
	message := strings.TrimSpace(anyString(errorPayload["message"]))
	if code == "" {
		code = "gateway_error"
	}
	if message == "" {
		message = http.StatusText(w.statusCode)
	}
	return w.writeError(w.statusCode, code, message, requestID)
}

func (w *responsesWebSocketResponseWriter) writeError(status int, code string, message string, requestID string) error {
	payload, _ := json.Marshal(responsesWebSocketErrorPayload(status, code, message, requestID))
	w.terminal = true
	return w.writeEvent(payload)
}

func (w *responsesWebSocketResponseWriter) writeEvent(payload []byte) error {
	writeCtx, cancel := context.WithTimeout(w.ctx, responsesWebSocketWriteTimeout)
	defer cancel()
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if err := w.conn.Write(writeCtx, websocket.MessageText, payload); err != nil {
		return &responsesWebSocketClientWriteError{err: err}
	}
	return nil
}

func responsesWebSocketErrorPayload(status int, code string, message string, requestID string) map[string]any {
	errorType := "invalid_request_error"
	if status == http.StatusTooManyRequests {
		errorType = "rate_limit_error"
	}
	errorPayload := map[string]any{
		"type":    errorType,
		"code":    code,
		"message": message,
	}
	if code == "previous_response_not_found" {
		errorPayload["param"] = "previous_response_id"
	}
	if requestID != "" {
		errorPayload["request_id"] = requestID
	}
	return map[string]any{
		"type":   "error",
		"status": status,
		"error":  errorPayload,
	}
}

func responsesWebSocketQuotaErrorPayload(failure auth.APIKeyQuotaFailure, requestID string) map[string]any {
	return map[string]any{
		"type":   "error",
		"status": http.StatusUnauthorized,
		"error":  failure.Payload(requestID),
	}
}

func (h Handler) ResponsesWebSocket(maxMessageBytes int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKey, ok := auth.APIKeyFromContext(r.Context())
		if !ok {
			h.writeGatewayError(w, r, http.StatusUnauthorized, "unauthorized", "valid api key is required")
			return
		}
		key := responsesWebSocketAPIKey(r)
		connectionID := "ws_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		connectionStarted := time.Now()
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			CompressionMode: websocket.CompressionContextTakeover,
		})
		if err != nil {
			return
		}
		if maxMessageBytes <= 0 {
			maxMessageBytes = 32 << 20
		}
		conn.SetReadLimit(maxMessageBytes)
		connectionCtx, cancel := context.WithTimeout(r.Context(), responsesWebSocketLifetime)

		var writeMu sync.Mutex
		lifetimeNoticeDone := make(chan struct{})
		go responsesWebSocketLifetimeLoop(connectionCtx, conn, &writeMu, lifetimeNoticeDone)
		defer func() {
			cancel()
			<-lifetimeNoticeDone
			_ = conn.Close(websocket.StatusNormalClosure, "")
		}()
		messages := make(chan responsesWebSocketMessage, 2)
		turnSlots := make(chan struct{}, 2)
		turnSlots <- struct{}{}
		turnSlots <- struct{}{}
		go responsesWebSocketReadLoop(connectionCtx, cancel, conn, messages, turnSlots, &writeMu)
		go responsesWebSocketPingLoop(connectionCtx, cancel, conn)

		state := responsesWebSocketState{maxBytes: maxMessageBytes}
		controlLimiter := newResponsesWebSocketControlLimiter(connectionStarted)
		turnIndex := 0
		for {
			var message responsesWebSocketMessage
			select {
			case <-connectionCtx.Done():
				return
			case message = <-messages:
				if connectionCtx.Err() != nil {
					return
				}
			}
			if !controlLimiter.allow(time.Now()) {
				_ = writeResponsesWebSocketEvent(connectionCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusTooManyRequests, "websocket_control_rate_limit", "too many response.create events on this websocket connection", ""))
				_ = conn.Close(websocket.StatusPolicyViolation, "control rate limit exceeded")
				return
			}
			if message.messageType != websocket.MessageText {
				_ = writeResponsesWebSocketEvent(connectionCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusBadRequest, "invalid_websocket_message", "responses websocket messages must be JSON text frames", ""))
				_ = conn.Close(websocket.StatusUnsupportedData, "text frames required")
				return
			}

			turnIndex++
			turnStarted := time.Now()
			turnRequestID := uuid.NewString()
			metadata := responsesWebSocketMetadata{
				ConnectionID:     connectionID,
				TurnIndex:        turnIndex,
				ConnectionReused: turnIndex > 1,
				ConnectionAgeMS:  time.Since(connectionStarted).Milliseconds(),
			}
			turnCtx := withResponsesWebSocketMetadata(connectionCtx, metadata)
			turnCtx = context.WithValue(turnCtx, middleware.RequestIDKey, turnRequestID)

			freshKey, validateErr := h.auth.ValidateAPIKey(turnCtx, key)
			if validateErr != nil {
				if failure, ok := auth.APIKeyQuotaFailureFromError(validateErr); ok {
					h.recordRequestFailure(turnCtx, turnRequestID, apiKey.ID, turnStarted, http.StatusUnauthorized, failure.Code, failure.Message, "", true, "auth", gatewayEndpointResponses)
					_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketQuotaErrorPayload(failure, turnRequestID))
					return
				}
				h.recordRequestFailure(turnCtx, turnRequestID, apiKey.ID, turnStarted, http.StatusUnauthorized, "unauthorized", "valid api key is required", "", true, "auth", gatewayEndpointResponses)
				_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusUnauthorized, "unauthorized", "valid api key is required", turnRequestID))
				return
			}

			var payload map[string]any
			if err := json.Unmarshal(message.data, &payload); err != nil {
				h.recordRequestFailure(turnCtx, turnRequestID, freshKey.ID, turnStarted, http.StatusBadRequest, "invalid_json", "websocket message must be valid JSON", "", true, "ws_decode", gatewayEndpointResponses)
				_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusBadRequest, "invalid_json", "websocket message must be valid JSON", turnRequestID))
				if controlLimiter.invalid() {
					_ = conn.Close(websocket.StatusPolicyViolation, "too many invalid events")
					return
				}
				turnSlots <- struct{}{}
				continue
			}
			if strings.TrimSpace(anyString(payload["type"])) != "response.create" {
				h.recordRequestFailure(turnCtx, turnRequestID, freshKey.ID, turnStarted, http.StatusBadRequest, "invalid_websocket_event", "only response.create events are supported", "", true, "ws_decode", gatewayEndpointResponses)
				_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusBadRequest, "invalid_websocket_event", "only response.create events are supported", turnRequestID))
				if controlLimiter.invalid() {
					_ = conn.Close(websocket.StatusPolicyViolation, "too many invalid events")
					return
				}
				turnSlots <- struct{}{}
				continue
			}

			model := strings.TrimSpace(anyString(payload["model"]))
			if model == "" {
				if strings.TrimSpace(anyString(payload["previous_response_id"])) != "" {
					state.evict()
				}
				h.recordRequestFailure(turnCtx, turnRequestID, freshKey.ID, turnStarted, http.StatusBadRequest, "invalid_model", "model is required", "", true, "validate", gatewayEndpointResponses)
				_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusBadRequest, "invalid_model", "model is required", turnRequestID))
				if controlLimiter.invalid() {
					_ = conn.Close(websocket.StatusPolicyViolation, "too many invalid events")
					return
				}
				turnSlots <- struct{}{}
				continue
			}

			expandedInput, hadPrevious, stateMode, expandErr := state.expand(payload)
			metadata.HadPrevious = hadPrevious
			metadata.StateMode = stateMode
			turnCtx = withResponsesWebSocketMetadata(turnCtx, metadata)
			if expandErr != nil {
				state.evict()
				code := "invalid_input"
				stage := "validate"
				if stateMode == "previous_response_not_found" {
					code = "previous_response_not_found"
					stage = "ws_state"
				}
				h.recordRequestFailure(turnCtx, turnRequestID, freshKey.ID, turnStarted, http.StatusBadRequest, code, expandErr.Error(), model, true, stage, gatewayEndpointResponses)
				_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusBadRequest, code, expandErr.Error(), turnRequestID))
				if controlLimiter.invalid() {
					_ = conn.Close(websocket.StatusPolicyViolation, "too many invalid events")
					return
				}
				turnSlots <- struct{}{}
				continue
			}

			generate := true
			if rawGenerate, exists := payload["generate"]; exists {
				value, valid := rawGenerate.(bool)
				if !valid {
					if hadPrevious {
						state.evict()
					}
					h.recordRequestFailure(turnCtx, turnRequestID, freshKey.ID, turnStarted, http.StatusBadRequest, "invalid_generate", "generate must be a boolean", model, true, "validate", gatewayEndpointResponses)
					_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusBadRequest, "invalid_generate", "generate must be a boolean", turnRequestID))
					if controlLimiter.invalid() {
						_ = conn.Close(websocket.StatusPolicyViolation, "too many invalid events")
						return
					}
					turnSlots <- struct{}{}
					continue
				}
				generate = value
			}
			var inheritedConfig map[string]any
			if hadPrevious && state.latest != nil {
				inheritedConfig = state.latest.Config
			}
			executionPayload := responsesWebSocketExecutionPayload(payload, inheritedConfig, expandedInput)
			executionBody, bodyErr := responsesWebSocketExecutionBody(executionPayload, maxMessageBytes)
			stateConfig := responsesWebSocketRequestConfig(executionPayload)
			if !generate {
				controlLimiter.valid()
				if !controlLimiter.warmup() {
					_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusTooManyRequests, "too_many_consecutive_warmups", "too many consecutive generate:false events", turnRequestID))
					_ = conn.Close(websocket.StatusPolicyViolation, "too many consecutive warmups")
					return
				}
				if bodyErr != nil {
					state.evict()
					_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusRequestEntityTooLarge, "request_body_too_large", "websocket request exceeds the maximum allowed size", turnRequestID))
					turnSlots <- struct{}{}
					continue
				}
				responseID := "resp_warmup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
				if !state.update(responseID, expandedInput, nil, stateConfig) {
					_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusRequestEntityTooLarge, "websocket_state_too_large", "websocket response state exceeds the maximum allowed size", turnRequestID))
					turnSlots <- struct{}{}
					continue
				}
				if err := writeResponsesWebSocketWarmup(turnCtx, conn, &writeMu, responseID, model); err != nil {
					return
				}
				turnSlots <- struct{}{}
				continue
			}

			if bodyErr != nil {
				state.evict()
				h.recordRequestFailure(turnCtx, turnRequestID, freshKey.ID, turnStarted, http.StatusRequestEntityTooLarge, "request_body_too_large", "websocket request exceeds the maximum allowed size", model, true, "ws_state", gatewayEndpointResponses)
				_ = writeResponsesWebSocketEvent(turnCtx, conn, &writeMu, responsesWebSocketErrorPayload(http.StatusRequestEntityTooLarge, "request_body_too_large", "websocket request exceeds the maximum allowed size", turnRequestID))
				if controlLimiter.invalid() {
					_ = conn.Close(websocket.StatusPolicyViolation, "too many invalid events")
					return
				}
				turnSlots <- struct{}{}
				continue
			}
			controlLimiter.valid()

			turnRequest := responsesWebSocketHTTPRequest(r, turnCtx, freshKey, executionBody)
			responseWriter := newResponsesWebSocketResponseWriter(turnCtx, conn, &writeMu, &state, expandedInput, stateConfig, turnRequestID)
			h.serveEndpoint(responseWriter, turnRequest, responsesEndpointAdapter{}, openAIProtocolResolver{db: h.db})
			if err := responseWriter.finalize(turnRequestID); err != nil {
				return
			}
			if responseWriter.completed && responseWriter.responseID != "" {
				controlLimiter.generated()
			} else {
				state.evict()
			}
			turnSlots <- struct{}{}
		}
	}
}

func responsesWebSocketReadLoop(ctx context.Context, cancel context.CancelFunc, conn responsesWebSocketConnection, messages chan<- responsesWebSocketMessage, turnSlots <-chan struct{}, writeMu *sync.Mutex) {
	for {
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			cancel()
			return
		}
		message := responsesWebSocketMessage{messageType: messageType, data: data}
		select {
		case <-turnSlots:
			select {
			case messages <- message:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		default:
			cancel()
			_ = writeResponsesWebSocketEvent(context.Background(), conn, writeMu, responsesWebSocketErrorPayload(http.StatusTooManyRequests, "too_many_pending_requests", "only one pending response.create event is allowed", ""))
			_ = conn.Close(websocket.StatusPolicyViolation, "too many pending requests")
			return
		}
	}
}

func responsesWebSocketAPIKey(r *http.Request) string {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(authorization, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

func cloneResponsesWebSocketPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return clonePayload(payload)
	}
	var cloned map[string]any
	if err := json.Unmarshal(body, &cloned); err != nil {
		return clonePayload(payload)
	}
	return cloned
}

func responsesWebSocketExecutionPayload(payload map[string]any, inheritedConfig map[string]any, expandedInput []any) map[string]any {
	executionPayload := cloneResponsesWebSocketPayload(inheritedConfig)
	for key, value := range cloneResponsesWebSocketPayload(payload) {
		executionPayload[key] = value
	}
	executionPayload["input"] = cloneResponsesWebSocketItems(expandedInput)
	executionPayload["stream"] = true
	delete(executionPayload, "type")
	delete(executionPayload, "generate")
	delete(executionPayload, "background")
	delete(executionPayload, "previous_response_id")
	delete(executionPayload, "client_metadata")
	return executionPayload
}

func responsesWebSocketRequestConfig(executionPayload map[string]any) map[string]any {
	config := cloneResponsesWebSocketPayload(executionPayload)
	delete(config, "input")
	delete(config, "stream")
	return config
}

func responsesWebSocketExecutionBody(executionPayload map[string]any, maxBytes int64) ([]byte, error) {
	body, err := json.Marshal(executionPayload)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("websocket request exceeds the maximum allowed size")
	}
	return body, nil
}

func responsesWebSocketHTTPRequest(r *http.Request, ctx context.Context, apiKey store.APIKey, body []byte) *http.Request {
	request := r.Clone(auth.WithAPIKey(ctx, apiKey))
	request.Method = http.MethodPost
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.Header = r.Header.Clone()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Del("Connection")
	request.Header.Del("Upgrade")
	request.Header.Del("Sec-WebSocket-Key")
	request.Header.Del("Sec-WebSocket-Version")
	request.Header.Del("Sec-WebSocket-Extensions")
	request.Header.Del("Sec-WebSocket-Protocol")
	request.Header.Del("OpenAI-Beta")
	request.Header.Del("Origin")
	return request
}

func writeResponsesWebSocketWarmup(ctx context.Context, conn responsesWebSocketEventWriter, writeMu *sync.Mutex, responseID string, model string) error {
	createdAt := time.Now().Unix()
	created := map[string]any{
		"type":            "response.created",
		"sequence_number": 0,
		"response": map[string]any{
			"id": responseID, "object": "response", "created_at": createdAt, "status": "in_progress", "model": model, "output": []any{},
		},
	}
	completed := map[string]any{
		"type":            "response.completed",
		"sequence_number": 1,
		"response": map[string]any{
			"id": responseID, "object": "response", "created_at": createdAt, "status": "completed", "model": model, "output": []any{},
		},
	}
	if err := writeResponsesWebSocketEvent(ctx, conn, writeMu, created); err != nil {
		return err
	}
	return writeResponsesWebSocketEvent(ctx, conn, writeMu, completed)
}

func writeResponsesWebSocketEvent(ctx context.Context, conn responsesWebSocketEventWriter, writeMu *sync.Mutex, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, responsesWebSocketWriteTimeout)
	defer cancel()
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.Write(writeCtx, websocket.MessageText, body)
}

func responsesWebSocketPingLoop(ctx context.Context, cancel context.CancelFunc, conn responsesWebSocketConnection) {
	ticker := time.NewTicker(responsesWebSocketPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, responsesWebSocketWriteTimeout)
			err := conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}

func responsesWebSocketLifetimeLoop(ctx context.Context, conn responsesWebSocketConnection, writeMu *sync.Mutex, done chan<- struct{}) {
	defer close(done)
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return
	}
	_ = writeResponsesWebSocketConnectionLimit(conn, writeMu)
	_ = conn.Close(websocket.StatusNormalClosure, "connection limit reached")
}

func writeResponsesWebSocketConnectionLimit(conn responsesWebSocketEventWriter, writeMu *sync.Mutex) error {
	ctx, cancel := context.WithTimeout(context.Background(), responsesWebSocketWriteTimeout)
	defer cancel()
	return writeResponsesWebSocketEvent(ctx, conn, writeMu, responsesWebSocketErrorPayload(http.StatusBadRequest, "websocket_connection_limit_reached", "Responses websocket connection limit reached (60 minutes). Create a new websocket connection to continue.", ""))
}
