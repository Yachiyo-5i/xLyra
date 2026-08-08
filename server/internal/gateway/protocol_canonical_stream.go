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

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

type canonicalStreamEventType string

const (
	canonicalStreamEventCreated         canonicalStreamEventType = "created"
	canonicalStreamEventTextDelta       canonicalStreamEventType = "text_delta"
	canonicalStreamEventToolCallDelta   canonicalStreamEventType = "tool_call_delta"
	canonicalStreamEventReasoningDelta  canonicalStreamEventType = "reasoning_delta"
	canonicalStreamEventAdvanced        canonicalStreamEventType = "advanced_event"
	canonicalStreamEventAnnotationAdded canonicalStreamEventType = "annotation_added"
	canonicalStreamEventUsage           canonicalStreamEventType = "usage"
	canonicalStreamEventCompleted       canonicalStreamEventType = "completed"
	canonicalStreamEventIncomplete      canonicalStreamEventType = "incomplete"
	canonicalStreamEventError           canonicalStreamEventType = "error"
)

type canonicalStreamEvent struct {
	Type                   canonicalStreamEventType
	ID                     string
	Model                  string
	CreatedAt              int64
	Delta                  string
	Usage                  completionUsage
	FinishReason           string
	ErrorMessage           string
	Annotation             any
	Refusal                bool
	ReasoningKind          string
	ReasoningEventName     string
	ReasoningIndex         int
	AdvancedKind           string
	AdvancedPhase          string
	AdvancedEventName      string
	AdvancedPayload        map[string]any
	ToolCallIndex          int
	ToolCallID             string
	ToolCallName           string
	ToolCallArgumentsDelta string
	ToolCallMetadata       map[string]any
}

type canonicalStreamOptions struct {
	IncludeUsage        bool
	Candidate           routeengine.Candidate
	UpstreamLineInspect func([]byte, *streamCaptureState)
	CustomTools         map[string]struct{}
	ResponseTools       map[string]responsesToolIdentity
}

type canonicalStreamDecoder interface {
	DecodeLine(line []byte) ([]canonicalStreamEvent, error)
}

type canonicalStreamDecoderFlusher interface {
	Flush() []canonicalStreamEvent
}

type canonicalStreamEncoder interface {
	EncodeEvent(event canonicalStreamEvent) error
}

type streamProtocolSpec struct {
	NewDecoder func() canonicalStreamDecoder
	NewEncoder func(canonicalStreamOptions, http.ResponseWriter, *streamCaptureState) canonicalStreamEncoder
}

// streamProtocolSpecsRegistry is built once; specs are read-only lookups.
var streamProtocolSpecsRegistry = buildStreamProtocolSpecs()

func streamProtocolSpecs() map[canonicalProtocol]streamProtocolSpec {
	return streamProtocolSpecsRegistry
}

func buildStreamProtocolSpecs() map[canonicalProtocol]streamProtocolSpec {
	return map[canonicalProtocol]streamProtocolSpec{
		canonicalProtocolOpenAIChat: {
			NewDecoder: func() canonicalStreamDecoder { return &openAIChatStreamDecoder{} },
			NewEncoder: func(options canonicalStreamOptions, w http.ResponseWriter, capture *streamCaptureState) canonicalStreamEncoder {
				return newOpenAIChatStreamEncoder(options, w, capture)
			},
		},
		canonicalProtocolOpenAIResponses: {
			NewDecoder: func() canonicalStreamDecoder { return &openAIResponsesStreamDecoder{} },
			NewEncoder: func(options canonicalStreamOptions, w http.ResponseWriter, capture *streamCaptureState) canonicalStreamEncoder {
				return newOpenAIResponsesStreamEncoder(options, w, capture)
			},
		},
		canonicalProtocolCodexResponses: {
			NewDecoder: func() canonicalStreamDecoder { return &openAIResponsesStreamDecoder{} },
			NewEncoder: func(options canonicalStreamOptions, w http.ResponseWriter, capture *streamCaptureState) canonicalStreamEncoder {
				return newOpenAIResponsesStreamEncoder(options, w, capture)
			},
		},
		canonicalProtocolAntigravity: {
			NewDecoder: func() canonicalStreamDecoder { return &antigravityStreamDecoder{} },
		},
		canonicalProtocolGoogleGemini: {
			NewDecoder: func() canonicalStreamDecoder { return &antigravityStreamDecoder{} },
		},
		canonicalProtocolAnthropicMessages: {
			NewDecoder: func() canonicalStreamDecoder { return &anthropicMessagesStreamDecoder{} },
			NewEncoder: func(options canonicalStreamOptions, w http.ResponseWriter, capture *streamCaptureState) canonicalStreamEncoder {
				return newAnthropicMessagesStreamEncoder(options, w, capture)
			},
		},
	}
}

func proxyCanonicalStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, from canonicalProtocol, to canonicalProtocol, options canonicalStreamOptions) (streamCaptureState, bool, error) {
	capture := streamCaptureState{}
	if resp == nil || resp.Body == nil {
		capture.endReason = "upstream_stream_missing_body"
		return capture, false, fmt.Errorf("upstream stream body is not available")
	}
	specs := streamProtocolSpecs()
	source, ok := specs[from]
	if !ok || source.NewDecoder == nil {
		capture.endReason = "stream_decoder_missing"
		return capture, false, fmt.Errorf("stream decoder is not registered for protocol %s", from)
	}
	target, ok := specs[to]
	if !ok || target.NewEncoder == nil {
		capture.endReason = "stream_encoder_missing"
		return capture, false, fmt.Errorf("stream encoder is not registered for protocol %s", to)
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	headersWritten := false
	writeHeaders := func() {
		if headersWritten {
			return
		}
		w.WriteHeader(http.StatusOK)
		headersWritten = true
		capture.firstByteLatency = firstNonZero(capture.firstByteLatency, time.Since(startedAt).Milliseconds())
	}

	decoder := source.NewDecoder()
	encoder := target.NewEncoder(options, w, &capture)
	reader := bufio.NewReader(resp.Body)
	for {
		if err := ctx.Err(); err != nil {
			capture.endReason = "downstream_client_cancelled"
			return capture, headersWritten, err
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if options.UpstreamLineInspect != nil {
				options.UpstreamLineInspect(line, &capture)
			}
			events, decodeErr := decoder.DecodeLine(line)
			if decodeErr != nil {
				// Once content is in flight, a single malformed or truncated line
				// (e.g. a half-line JSON on upstream EOF) must not abort the whole
				// stream and discard everything already sent. Skip it and let the
				// stream finish via the normal EOF/completion path. Before any bytes
				// are committed, keep failing so the gateway can fall over cleanly.
				if !headersWritten {
					capture.endReason = "upstream_stream_parse_failed"
					return capture, headersWritten, decodeErr
				}
				capture.malformedLines++
			} else {
				for _, event := range events {
					writeHeaders()
					if encodeErr := encoder.EncodeEvent(event); encodeErr != nil {
						if capture.endReason == "" {
							capture.endReason = "downstream_stream_write_failed"
						}
						return capture, headersWritten, encodeErr
					}
				}
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			if flusher, ok := decoder.(canonicalStreamDecoderFlusher); ok {
				for _, event := range flusher.Flush() {
					writeHeaders()
					if encodeErr := encoder.EncodeEvent(event); encodeErr != nil {
						if capture.endReason == "" {
							capture.endReason = "downstream_stream_write_failed"
						}
						return capture, headersWritten, encodeErr
					}
				}
			}
			if headersWritten && !capture.streamCompleted && !capture.sawDone && capture.endReason == "" {
				if shouldSynthesizeEOFCompletion(from) {
					if encodeErr := encoder.EncodeEvent(canonicalStreamEvent{Type: canonicalStreamEventCompleted}); encodeErr != nil {
						capture.endReason = "downstream_stream_write_failed"
						return capture, headersWritten, encodeErr
					}
				} else {
					capture.endReason = "upstream_stream_incomplete"
					return capture, headersWritten, nil
				}
			}
			if capture.streamCompleted {
				capture.endReason = "done"
			} else if capture.sawDone && capture.endReason == "" {
				capture.streamCompleted = true
				capture.endReason = "done"
			} else if capture.endReason != "" {
				// Preserve semantic terminal states such as response_incomplete.
			} else if headersWritten {
				capture.endReason = "upstream_stream_eof"
			} else {
				capture.endReason = "upstream_stream_empty"
			}
			return capture, headersWritten, nil
		}
		if capture.endReason == "done" || (capture.endReason == "" && (capture.streamCompleted || capture.sawDone)) {
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

func shouldSynthesizeEOFCompletion(protocol canonicalProtocol) bool {
	return protocol == canonicalProtocolAntigravity
}

func chatFinishReasonIsIncomplete(finishReason string) bool {
	switch strings.TrimSpace(finishReason) {
	case "length", "content_filter":
		return true
	default:
		return false
	}
}

type openAIChatStreamDecoder struct {
	createdSent         bool
	terminalEmitted     bool
	pendingTerminal     *canonicalStreamEvent
	toolCallIDByIndex   map[int]string
	toolCallNameByIndex map[int]string
}

func (d *openAIChatStreamDecoder) DecodeLine(line []byte) ([]canonicalStreamEvent, error) {
	data, done, ok := sseDataFromLine(line)
	if !ok {
		return nil, nil
	}
	if done {
		if d.terminalEmitted {
			return nil, nil
		}
		d.terminalEmitted = true
		if d.pendingTerminal != nil {
			terminal := *d.pendingTerminal
			d.pendingTerminal = nil
			return []canonicalStreamEvent{terminal}, nil
		}
		return []canonicalStreamEvent{{Type: canonicalStreamEventCompleted}}, nil
	}
	var chunk struct {
		ID      string          `json:"id"`
		Created int64           `json:"created"`
		Model   string          `json:"model"`
		Choices []chatChoice    `json:"choices"`
		Usage   completionUsage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, err
	}
	if d.toolCallIDByIndex == nil {
		d.toolCallIDByIndex = map[int]string{}
		d.toolCallNameByIndex = map[int]string{}
	}
	events := make([]canonicalStreamEvent, 0, len(chunk.Choices)+2)
	if !d.createdSent {
		d.createdSent = true
		events = append(events, canonicalStreamEvent{
			Type:      canonicalStreamEventCreated,
			ID:        chunk.ID,
			Model:     chunk.Model,
			CreatedAt: chunk.Created,
		})
	}
	for _, choice := range chunk.Choices {
		if finishReason := strings.TrimSpace(choice.FinishReason); finishReason != "" {
			eventType := canonicalStreamEventCompleted
			if chatFinishReasonIsIncomplete(finishReason) {
				eventType = canonicalStreamEventIncomplete
			}
			d.pendingTerminal = &canonicalStreamEvent{
				Type:         eventType,
				ID:           chunk.ID,
				Model:        chunk.Model,
				CreatedAt:    chunk.Created,
				FinishReason: finishReason,
			}
			continue
		}
		if delta, _ := choice.Delta.Content.(string); delta != "" {
			events = append(events, canonicalStreamEvent{
				Type:  canonicalStreamEventTextDelta,
				ID:    chunk.ID,
				Model: chunk.Model,
				Delta: delta,
			})
		}
		if reasoning := firstNonEmptyGatewayString(choice.Delta.ReasoningContent, choice.Delta.Thinking); strings.TrimSpace(reasoning) != "" {
			events = append(events, canonicalStreamEvent{
				Type:          canonicalStreamEventReasoningDelta,
				ID:            chunk.ID,
				Model:         chunk.Model,
				Delta:         reasoning,
				ReasoningKind: "thinking",
			})
		}
		if signature := choice.Delta.ThinkingSignature; signature != "" {
			events = append(events, canonicalStreamEvent{
				Type:          canonicalStreamEventReasoningDelta,
				ID:            chunk.ID,
				Model:         chunk.Model,
				Delta:         signature,
				ReasoningKind: "thinking_signature",
			})
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			index := toolCall.Index
			callID := strings.TrimSpace(toolCall.ID)
			if callID != "" {
				d.toolCallIDByIndex[index] = callID
			} else {
				callID = strings.TrimSpace(d.toolCallIDByIndex[index])
			}
			if callID == "" {
				callID = "call_" + uuid.NewString()
				d.toolCallIDByIndex[index] = callID
			}
			name := strings.TrimSpace(toolCall.Function.Name)
			if name != "" {
				d.toolCallNameByIndex[index] = name
			} else {
				name = strings.TrimSpace(d.toolCallNameByIndex[index])
			}
			events = append(events, canonicalStreamEvent{
				Type:                   canonicalStreamEventToolCallDelta,
				ID:                     chunk.ID,
				Model:                  chunk.Model,
				ToolCallIndex:          index,
				ToolCallID:             callID,
				ToolCallName:           name,
				ToolCallArgumentsDelta: toolCall.Function.Arguments,
			})
		}
	}
	if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
		events = append(events, canonicalStreamEvent{Type: canonicalStreamEventUsage, Usage: chunk.Usage.normalized()})
	}
	return events, nil
}

func (d *openAIChatStreamDecoder) Flush() []canonicalStreamEvent {
	if d.terminalEmitted || d.pendingTerminal == nil {
		return nil
	}
	terminal := *d.pendingTerminal
	d.pendingTerminal = nil
	d.terminalEmitted = true
	return []canonicalStreamEvent{terminal}
}

type openAIResponsesStreamDecoder struct {
	toolCallIDByItemID   map[string]string
	toolCallNameByCallID map[string]string
	toolCallArgsByCallID map[string]string
}

func (d *openAIResponsesStreamDecoder) DecodeLine(line []byte) ([]canonicalStreamEvent, error) {
	data, done, ok := sseDataFromLine(line)
	if !ok {
		return nil, nil
	}
	if done {
		return []canonicalStreamEvent{{Type: canonicalStreamEventCompleted}}, nil
	}
	if d.toolCallIDByItemID == nil {
		d.toolCallIDByItemID = map[string]string{}
		d.toolCallNameByCallID = map[string]string{}
		d.toolCallArgsByCallID = map[string]string{}
	}
	var event responsesStreamEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil, err
	}
	switch event.Type {
	case "response.created":
		if event.Response == nil {
			return nil, nil
		}
		return []canonicalStreamEvent{{
			Type:      canonicalStreamEventCreated,
			ID:        event.Response.ID,
			Model:     event.Response.Model,
			CreatedAt: event.Response.CreatedAt,
		}}, nil
	case "response.output_text.delta":
		return []canonicalStreamEvent{{Type: canonicalStreamEventTextDelta, Delta: event.Delta}}, nil
	case "response.refusal.delta":
		return []canonicalStreamEvent{{Type: canonicalStreamEventTextDelta, Delta: event.Delta, Refusal: true}}, nil
	case "response.refusal.done":
		if text := nonEmptyString(event.Text, event.Delta); text != "" {
			return []canonicalStreamEvent{{Type: canonicalStreamEventTextDelta, Delta: text, Refusal: true}}, nil
		}
		return nil, nil
	case "response.output_text.annotation.added":
		return []canonicalStreamEvent{{Type: canonicalStreamEventAnnotationAdded, Annotation: event.Annotation}}, nil
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning_delta", "response.reasoning_summary.delta",
		"response.reasoning.encrypted_content.delta", "response.reasoning.encrypted_content.done":
		return []canonicalStreamEvent{{
			Type:               canonicalStreamEventReasoningDelta,
			Delta:              event.Delta,
			ReasoningKind:      reasoningKindFromResponsesEvent(event.Type),
			ReasoningEventName: event.Type,
		}}, nil
	case "response.output_item.added", "response.output_item.done":
		if event.Item == nil || event.Item.Type != "function_call" {
			return responsesAdvancedStreamEvent(data, event)
		}
		callID := strings.TrimSpace(event.Item.CallID)
		if callID == "" {
			callID = strings.TrimSpace(event.Item.ID)
		}
		if event.Item.ID != "" && callID != "" {
			d.toolCallIDByItemID[event.Item.ID] = callID
		}
		name := strings.TrimSpace(event.Item.Name)
		if name != "" {
			d.toolCallNameByCallID[callID] = name
		}
		argsDelta := ""
		if nextArgs := event.Item.Arguments; nextArgs != "" {
			prevArgs := d.toolCallArgsByCallID[callID]
			if strings.HasPrefix(nextArgs, prevArgs) {
				argsDelta = nextArgs[len(prevArgs):]
			} else {
				argsDelta = nextArgs
			}
			d.toolCallArgsByCallID[callID] = nextArgs
		}
		return []canonicalStreamEvent{{
			Type:                   canonicalStreamEventToolCallDelta,
			ToolCallID:             callID,
			ToolCallName:           name,
			ToolCallArgumentsDelta: argsDelta,
		}}, nil
	case "response.function_call_arguments.delta":
		callID := strings.TrimSpace(d.toolCallIDByItemID[event.ItemID])
		if callID == "" {
			callID = strings.TrimSpace(event.ItemID)
		}
		d.toolCallArgsByCallID[callID] += event.Delta
		return []canonicalStreamEvent{{
			Type:                   canonicalStreamEventToolCallDelta,
			ToolCallID:             callID,
			ToolCallName:           d.toolCallNameByCallID[callID],
			ToolCallArgumentsDelta: event.Delta,
		}}, nil
	case "response.function_call_arguments.done":
		callID := strings.TrimSpace(d.toolCallIDByItemID[event.ItemID])
		if callID == "" {
			callID = strings.TrimSpace(event.ItemID)
		}
		nextArgs := strings.TrimSpace(string(event.Arguments))
		prevArgs := d.toolCallArgsByCallID[callID]
		if nextArgs == "" || nextArgs == prevArgs {
			return nil, nil
		}
		argsDelta := nextArgs
		if strings.HasPrefix(nextArgs, prevArgs) {
			argsDelta = nextArgs[len(prevArgs):]
		}
		d.toolCallArgsByCallID[callID] = nextArgs
		return []canonicalStreamEvent{{
			Type:                   canonicalStreamEventToolCallDelta,
			ToolCallID:             callID,
			ToolCallName:           d.toolCallNameByCallID[callID],
			ToolCallArgumentsDelta: argsDelta,
		}}, nil
	case "response.completed":
		out := []canonicalStreamEvent{}
		if event.Response != nil {
			if event.Response.Usage != nil {
				out = append(out, canonicalStreamEvent{Type: canonicalStreamEventUsage, Usage: completionUsageFromResponsesUsage(event.Response.Usage)})
			}
			out = append(out, canonicalStreamEvent{
				Type:      canonicalStreamEventCompleted,
				ID:        event.Response.ID,
				Model:     event.Response.Model,
				CreatedAt: event.Response.CreatedAt,
			})
			return out, nil
		}
		return []canonicalStreamEvent{{Type: canonicalStreamEventCompleted}}, nil
	case "response.incomplete":
		out := []canonicalStreamEvent{}
		if event.Response != nil && event.Response.Usage != nil {
			out = append(out, canonicalStreamEvent{Type: canonicalStreamEventUsage, Usage: completionUsageFromResponsesUsage(event.Response.Usage)})
		}
		out = append(out, canonicalStreamEvent{
			Type:         canonicalStreamEventIncomplete,
			ID:           responseIDFromResponsesStreamEvent(event),
			Model:        modelFromResponsesStreamEvent(event),
			CreatedAt:    createdAtFromResponsesStreamEvent(event),
			FinishReason: "length",
			ErrorMessage: "response incomplete",
		})
		return out, nil
	case "response.error", "response.failed":
		return []canonicalStreamEvent{{Type: canonicalStreamEventError, ErrorMessage: errorMessageFromResponsesStreamEvent(event)}}, nil
	case "response.in_progress":
		return nil, nil
	default:
		return responsesAdvancedStreamEvent(data, event)
	}
}

// responsesAdvancedStreamEvent decodes the untyped view of a Responses stream
// line lazily — only the advanced fallback branches need it, so common event
// types avoid a second json.Unmarshal per line.
func responsesAdvancedStreamEvent(data string, event responsesStreamEvent) ([]canonicalStreamEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return nil, err
	}
	if advanced, ok := advancedEventFromResponsesStreamEvent(event, raw); ok {
		return []canonicalStreamEvent{advanced}, nil
	}
	return nil, nil
}

type antigravityStreamDecoder struct {
	responseID           string
	createdAt            int64
	createdSent          bool
	textualToolUseBuffer strings.Builder
}

func (d *antigravityStreamDecoder) DecodeLine(line []byte) ([]canonicalStreamEvent, error) {
	data, done, ok := sseDataFromLine(line)
	if !ok {
		return nil, nil
	}
	if done {
		return []canonicalStreamEvent{{Type: canonicalStreamEventCompleted}}, nil
	}
	root := map[string]any{}
	if err := json.Unmarshal([]byte(data), &root); err != nil {
		return nil, nil
	}
	if response, ok := root["response"].(map[string]any); ok {
		root = response
	}
	if message := antigravityStreamErrorMessage(root); message != "" {
		return []canonicalStreamEvent{{Type: canonicalStreamEventError, ErrorMessage: message}}, nil
	}
	if d.responseID == "" {
		d.responseID = "chatcmpl-" + uuid.NewString()
	}
	if d.createdAt == 0 {
		d.createdAt = time.Now().Unix()
	}
	text, toolCalls := antigravityGeminiOutput(root)
	events := []canonicalStreamEvent{}
	usage := antigravityUsage(root)
	text, bufferedToolCalls, flushBufferedText := d.consumeTextualToolUseStreamText(text, antigravityRawFinishReason(root))
	if len(bufferedToolCalls) > 0 {
		toolCalls = append(toolCalls, bufferedToolCalls...)
	}
	if !d.createdSent && (text != "" || len(toolCalls) > 0 || usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
		d.createdSent = true
		events = append(events, canonicalStreamEvent{
			Type:      canonicalStreamEventCreated,
			ID:        d.responseID,
			CreatedAt: d.createdAt,
		})
	}
	if text != "" {
		events = append(events, canonicalStreamEvent{
			Type:      canonicalStreamEventTextDelta,
			ID:        d.responseID,
			CreatedAt: d.createdAt,
			Delta:     text,
		})
	}
	if flushBufferedText != "" {
		if !d.createdSent {
			d.createdSent = true
			events = append(events, canonicalStreamEvent{
				Type:      canonicalStreamEventCreated,
				ID:        d.responseID,
				CreatedAt: d.createdAt,
			})
		}
		events = append(events, canonicalStreamEvent{
			Type:      canonicalStreamEventTextDelta,
			ID:        d.responseID,
			CreatedAt: d.createdAt,
			Delta:     flushBufferedText,
		})
	}
	for _, toolCall := range toolCalls {
		function, _ := toolCall["function"].(map[string]any)
		events = append(events, canonicalStreamEvent{
			Type:                   canonicalStreamEventToolCallDelta,
			ID:                     d.responseID,
			CreatedAt:              d.createdAt,
			ToolCallID:             strings.TrimSpace(anyString(toolCall["id"])),
			ToolCallName:           strings.TrimSpace(anyString(function["name"])),
			ToolCallArgumentsDelta: strings.TrimSpace(anyString(function["arguments"])),
			ToolCallMetadata:       toolCallMetadataFromGatewayMap(toolCall),
		})
	}
	if usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		events = append(events, canonicalStreamEvent{Type: canonicalStreamEventUsage, Usage: usage})
	}
	if finishReason := antigravityFinishReason(root); finishReason != "" && finishReason != "stop" {
		eventType := canonicalStreamEventCompleted
		if chatFinishReasonIsIncomplete(finishReason) {
			eventType = canonicalStreamEventIncomplete
		}
		events = append(events, canonicalStreamEvent{
			Type:         eventType,
			ID:           d.responseID,
			CreatedAt:    d.createdAt,
			FinishReason: finishReason,
		})
	}
	return events, nil
}

func (d *antigravityStreamDecoder) consumeTextualToolUseStreamText(text string, finishReason string) (string, []map[string]any, string) {
	if d.textualToolUseBuffer.Len() > 0 || antigravityMaybeTextualToolUsePrefix(text) {
		if text != "" {
			d.textualToolUseBuffer.WriteString(text)
		}
		buffered := d.textualToolUseBuffer.String()
		if toolCalls := antigravityToolCallsFromText(buffered); len(toolCalls) > 0 {
			d.textualToolUseBuffer.Reset()
			return "", toolCalls, ""
		}
		if antigravityShouldKeepBufferingTextualToolUse(buffered, finishReason) {
			return "", nil, ""
		}
		d.textualToolUseBuffer.Reset()
		return "", nil, buffered
	}
	return text, nil, ""
}

func antigravityMaybeTextualToolUsePrefix(text string) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if strings.HasPrefix(trimmed, "```") {
		return true
	}
	return strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{")
}

func antigravityShouldKeepBufferingTextualToolUse(buffered string, finishReason string) bool {
	if strings.TrimSpace(finishReason) != "" {
		return false
	}
	trimmed := strings.TrimSpace(buffered)
	if len(trimmed) > 65536 {
		return false
	}
	return antigravityMaybeTextualToolUsePrefix(trimmed)
}

type anthropicMessagesStreamDecoder struct {
	responseID      string
	model           string
	stopReason      string
	usage           gatewayUsage
	toolIDByIndex   map[int]string
	toolNameByIndex map[int]string
	thinkingByIndex map[int]bool
}

// mergeAnthropicStreamUsage folds an incremental Anthropic usage snapshot into
// the running total. Anthropic reports input/cache tokens on message_start and
// output tokens on message_delta, so each snapshot only carries a subset of the
// fields; a plain overwrite would let the later output-only snapshot wipe the
// prompt/cache counts captured earlier (undercounting prompt/cache billing for
// every Anthropic-upstream stream converted to another downstream protocol).
// Non-zero input/cache fields replace the running input side, a non-zero output
// count replaces the running output, and TotalTokens is recomputed. Mirrors the
// same-source passthrough merge in inspectAnthropicMessagesStreamLine.
func mergeAnthropicStreamUsage(current, delta gatewayUsage) gatewayUsage {
	if delta.PromptTokens > 0 || delta.CachedPromptTokens > 0 || delta.CacheCreationInputTokens > 0 {
		current.PromptTokens = delta.PromptTokens
		current.CachedPromptTokens = delta.CachedPromptTokens
		current.CacheCreationInputTokens = delta.CacheCreationInputTokens
		current.CacheCreation5mInputTokens = delta.CacheCreation5mInputTokens
		current.CacheCreation1hInputTokens = delta.CacheCreation1hInputTokens
	}
	if delta.CompletionTokens > 0 {
		current.CompletionTokens = delta.CompletionTokens
	}
	current.TotalTokens = 0
	return current.normalized()
}

func (d *anthropicMessagesStreamDecoder) DecodeLine(line []byte) ([]canonicalStreamEvent, error) {
	data, _, ok := sseDataFromLine(line)
	if !ok {
		return nil, nil
	}
	var event struct {
		Type         string                    `json:"type"`
		Index        int                       `json:"index"`
		Message      map[string]any            `json:"message"`
		ContentBlock map[string]any            `json:"content_block"`
		Delta        map[string]any            `json:"delta"`
		Usage        anthropicMessageUsageBody `json:"usage"`
		Error        map[string]any            `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return nil, err
	}
	if d.toolIDByIndex == nil {
		d.toolIDByIndex = map[int]string{}
		d.toolNameByIndex = map[int]string{}
		d.thinkingByIndex = map[int]bool{}
	}
	switch event.Type {
	case "message_start":
		d.responseID = strings.TrimSpace(anyString(event.Message["id"]))
		d.model = strings.TrimSpace(anyString(event.Message["model"]))
		events := []canonicalStreamEvent{{Type: canonicalStreamEventCreated, ID: d.responseID, Model: d.model, CreatedAt: time.Now().Unix()}}
		if messageUsage, ok := event.Message["usage"]; ok {
			usageBody := anthropicMessageUsageBody{}
			if encoded, err := json.Marshal(messageUsage); err == nil && json.Unmarshal(encoded, &usageBody) == nil {
				if usageBody.InputTokens > 0 || usageBody.CacheReadInputTokens > 0 || usageBody.CacheCreationInputTokens > 0 {
					d.usage = mergeAnthropicStreamUsage(d.usage, gatewayUsageFromAnthropicUsage(usageBody))
					events = append(events, canonicalStreamEvent{Type: canonicalStreamEventUsage, Usage: completionUsageFromGatewayUsage(d.usage)})
				}
			}
		}
		return events, nil
	case "content_block_start":
		switch strings.TrimSpace(anyString(event.ContentBlock["type"])) {
		case "thinking":
			d.thinkingByIndex[event.Index] = true
			return nil, nil
		case "redacted_thinking":
			d.thinkingByIndex[event.Index] = true
			return []canonicalStreamEvent{{
				Type:               canonicalStreamEventReasoningDelta,
				ReasoningKind:      "thinking_redacted",
				ReasoningEventName: "content_block_start",
				ReasoningIndex:     event.Index,
			}}, nil
		case "tool_use":
			callID := strings.TrimSpace(anyString(event.ContentBlock["id"]))
			name := strings.TrimSpace(anyString(event.ContentBlock["name"]))
			d.toolIDByIndex[event.Index] = callID
			d.toolNameByIndex[event.Index] = name
			return []canonicalStreamEvent{{Type: canonicalStreamEventToolCallDelta, ToolCallIndex: event.Index, ToolCallID: callID, ToolCallName: name}}, nil
		default:
			return nil, nil
		}
	case "content_block_delta":
		switch strings.TrimSpace(anyString(event.Delta["type"])) {
		case "text_delta":
			delta, _ := event.Delta["text"].(string)
			return []canonicalStreamEvent{{Type: canonicalStreamEventTextDelta, Delta: delta}}, nil
		case "thinking_delta":
			d.thinkingByIndex[event.Index] = true
			thinking, _ := event.Delta["thinking"].(string)
			return []canonicalStreamEvent{{
				Type:               canonicalStreamEventReasoningDelta,
				Delta:              thinking,
				ReasoningKind:      "thinking",
				ReasoningEventName: event.Type,
				ReasoningIndex:     event.Index,
			}}, nil
		case "signature_delta":
			d.thinkingByIndex[event.Index] = true
			signature, _ := event.Delta["signature"].(string)
			return []canonicalStreamEvent{{
				Type:               canonicalStreamEventReasoningDelta,
				Delta:              signature,
				ReasoningKind:      "thinking_signature",
				ReasoningEventName: event.Type,
				ReasoningIndex:     event.Index,
			}}, nil
		case "input_json_delta":
			partialJSON, _ := event.Delta["partial_json"].(string)
			return []canonicalStreamEvent{{
				Type:                   canonicalStreamEventToolCallDelta,
				ToolCallIndex:          event.Index,
				ToolCallID:             d.toolIDByIndex[event.Index],
				ToolCallName:           d.toolNameByIndex[event.Index],
				ToolCallArgumentsDelta: partialJSON,
			}}, nil
		}
	case "message_delta":
		d.stopReason = strings.TrimSpace(anyString(event.Delta["stop_reason"]))
		if event.Usage.OutputTokens > 0 || event.Usage.InputTokens > 0 || event.Usage.CacheReadInputTokens > 0 || event.Usage.CacheCreationInputTokens > 0 {
			d.usage = mergeAnthropicStreamUsage(d.usage, gatewayUsageFromAnthropicUsage(event.Usage))
			return []canonicalStreamEvent{{Type: canonicalStreamEventUsage, Usage: completionUsageFromGatewayUsage(d.usage)}}, nil
		}
	case "message_stop":
		eventType := canonicalStreamEventCompleted
		finishReason := anthropicStopReasonToFinishReason(d.stopReason)
		if chatFinishReasonIsIncomplete(finishReason) {
			eventType = canonicalStreamEventIncomplete
		}
		return []canonicalStreamEvent{{Type: eventType, FinishReason: finishReason}}, nil
	case "error":
		return []canonicalStreamEvent{{Type: canonicalStreamEventError, ErrorMessage: anthropicStreamErrorMessage(event.Error)}}, nil
	case "ping":
		return nil, nil
	}
	return nil, nil
}

type openAIChatStreamEncoder struct {
	options          canonicalStreamOptions
	w                http.ResponseWriter
	capture          *streamCaptureState
	responseID       string
	model            string
	createdAt        int64
	startSent        bool
	stopSent         bool
	sawText          bool
	sawToolCall      bool
	toolCallIndex    map[string]int
	toolCallArgs     map[string]*strings.Builder
	toolCallNameSent map[string]bool
}

func newOpenAIChatStreamEncoder(options canonicalStreamOptions, w http.ResponseWriter, capture *streamCaptureState) *openAIChatStreamEncoder {
	return &openAIChatStreamEncoder{
		options:          options,
		w:                w,
		capture:          capture,
		createdAt:        time.Now().Unix(),
		toolCallIndex:    map[string]int{},
		toolCallArgs:     map[string]*strings.Builder{},
		toolCallNameSent: map[string]bool{},
	}
}

func (e *openAIChatStreamEncoder) EncodeEvent(event canonicalStreamEvent) error {
	e.mergeMeta(event)
	switch event.Type {
	case canonicalStreamEventCreated:
		return e.sendStartIfNeeded()
	case canonicalStreamEventTextDelta:
		if event.Delta == "" {
			return nil
		}
		if err := e.sendStartIfNeeded(); err != nil {
			return err
		}
		e.sawText = true
		return e.writeChunk(map[string]any{
			"id":      responseIDOrFallback(e.responseID),
			"object":  "chat.completion.chunk",
			"created": e.createdAt,
			"model":   e.model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": event.Delta}}},
		})
	case canonicalStreamEventToolCallDelta:
		return e.sendToolCallDelta(event)
	case canonicalStreamEventReasoningDelta:
		if reasoningPolicyForProtocol(canonicalProtocolOpenAIChat, event.ReasoningKind, e.options.Candidate) != "passthrough" {
			appendStreamAnnotation(e.capture, redactedReasoningAnnotation(event))
			return nil
		}
		if event.Delta == "" {
			return nil
		}
		if err := e.sendStartIfNeeded(); err != nil {
			return err
		}
		delta := map[string]any{"reasoning_content": event.Delta}
		if event.ReasoningKind == "thinking_signature" {
			delta = map[string]any{"thinking_signature": event.Delta}
		}
		return e.writeChunk(map[string]any{
			"id":      responseIDOrFallback(e.responseID),
			"object":  "chat.completion.chunk",
			"created": e.createdAt,
			"model":   e.model,
			"choices": []map[string]any{{"index": 0, "delta": delta}},
		})
	case canonicalStreamEventAdvanced:
		appendStreamAnnotation(e.capture, advancedStreamAnnotation(event))
		return nil
	case canonicalStreamEventAnnotationAdded:
		appendStreamAnnotation(e.capture, event.Annotation)
		if isGrokSite(e.options.Candidate.Site.SiteType) {
			if err := e.sendStartIfNeeded(); err != nil {
				return err
			}
			return e.writeChunk(map[string]any{
				"id":      responseIDOrFallback(e.responseID),
				"object":  "chat.completion.chunk",
				"created": e.createdAt,
				"model":   e.model,
				"choices": []map[string]any{{"index": 0, "delta": map[string]any{"citations": []any{event.Annotation}}}},
			})
		}
		return nil
	case canonicalStreamEventUsage:
		if event.Usage.TotalTokens > 0 || event.Usage.PromptTokens > 0 || event.Usage.CompletionTokens > 0 {
			e.capture.usage = event.Usage
		}
		return nil
	case canonicalStreamEventCompleted:
		return e.sendFinish()
	case canonicalStreamEventIncomplete:
		return e.sendIncomplete(event)
	case canonicalStreamEventError:
		e.capture.endReason = "upstream_stream_error"
		return fmt.Errorf("upstream stream failed: %s", nonEmptyString(event.ErrorMessage, "unknown error"))
	default:
		return nil
	}
}

func (e *openAIChatStreamEncoder) mergeMeta(event canonicalStreamEvent) {
	if event.ID != "" {
		e.responseID = event.ID
	}
	if event.Model != "" {
		e.model = event.Model
	}
	if event.CreatedAt > 0 {
		e.createdAt = event.CreatedAt
	}
}

func (e *openAIChatStreamEncoder) sendStartIfNeeded() error {
	if e.startSent {
		return nil
	}
	e.startSent = true
	return e.writeChunk(map[string]any{
		"id":      responseIDOrFallback(e.responseID),
		"object":  "chat.completion.chunk",
		"created": e.createdAt,
		"model":   e.model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant"}}},
	})
}

func (e *openAIChatStreamEncoder) sendToolCallDelta(event canonicalStreamEvent) error {
	callID := strings.TrimSpace(event.ToolCallID)
	if callID == "" {
		return nil
	}
	if err := e.sendStartIfNeeded(); err != nil {
		return err
	}
	e.sawToolCall = true
	index, ok := e.toolCallIndex[callID]
	if !ok {
		index = len(e.toolCallIndex)
		e.toolCallIndex[callID] = index
	}
	if event.ToolCallArgumentsDelta != "" {
		if e.toolCallArgs[callID] == nil {
			e.toolCallArgs[callID] = &strings.Builder{}
		}
		e.toolCallArgs[callID].WriteString(event.ToolCallArgumentsDelta)
	}
	tool := map[string]any{
		"index": index,
		"id":    callID,
		"type":  "function",
		"function": map[string]any{
			"arguments": event.ToolCallArgumentsDelta,
		},
	}
	if event.ToolCallName != "" && !e.toolCallNameSent[callID] {
		tool["function"].(map[string]any)["name"] = event.ToolCallName
		e.toolCallNameSent[callID] = true
	}
	return e.writeChunk(map[string]any{
		"id":      responseIDOrFallback(e.responseID),
		"object":  "chat.completion.chunk",
		"created": e.createdAt,
		"model":   e.model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"tool_calls": []map[string]any{tool}}}},
	})
}

func (e *openAIChatStreamEncoder) sendFinish() error {
	if err := e.validateToolCallArguments(); err != nil {
		return err
	}
	return e.sendTerminal("stop", true, "done")
}

func (e *openAIChatStreamEncoder) sendIncomplete(event canonicalStreamEvent) error {
	finishReason := nonEmptyString(event.FinishReason, "length")
	return e.sendTerminal(finishReason, false, "response_incomplete")
}

func (e *openAIChatStreamEncoder) sendTerminal(defaultFinishReason string, completed bool, endReason string) error {
	if e.stopSent {
		return nil
	}
	if err := e.sendStartIfNeeded(); err != nil {
		return err
	}
	finishReason := defaultFinishReason
	if e.sawToolCall && !e.sawText {
		finishReason = "tool_calls"
	}
	if err := e.writeChunk(map[string]any{
		"id":      responseIDOrFallback(e.responseID),
		"object":  "chat.completion.chunk",
		"created": e.createdAt,
		"model":   e.model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}},
	}); err != nil {
		return err
	}
	if e.options.IncludeUsage && (e.capture.usage.TotalTokens > 0 || e.capture.usage.PromptTokens > 0 || e.capture.usage.CompletionTokens > 0) {
		if err := e.writeChunk(map[string]any{
			"id":      responseIDOrFallback(e.responseID),
			"object":  "chat.completion.chunk",
			"created": e.createdAt,
			"model":   e.model,
			"choices": []any{},
			"usage":   e.capture.usage,
		}); err != nil {
			return err
		}
	}
	if _, err := e.w.Write([]byte("data: [DONE]\n\n")); err != nil {
		return err
	}
	e.stopSent = true
	e.capture.sawDone = true
	e.capture.streamCompleted = completed
	e.capture.endReason = endReason
	return nil
}

func (e *openAIChatStreamEncoder) validateToolCallArguments() error {
	for callID, args := range e.toolCallArgs {
		if args == nil {
			continue
		}
		if err := validateToolCallArguments(callID, args.String()); err != nil {
			e.capture.endReason = "tool_call_arguments_invalid_json"
			return err
		}
	}
	return nil
}

func (e *openAIChatStreamEncoder) writeChunk(chunk map[string]any) error {
	encoded, err := json.Marshal(chunk)
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

type openAIResponsesStreamEncoder struct {
	options       canonicalStreamOptions
	w             http.ResponseWriter
	capture       *streamCaptureState
	responseID    string
	model         string
	createdAt     int64
	responseSent  bool
	textStarted   bool
	textBuffer    strings.Builder
	completed     bool
	sequence      int
	textOutputIdx int
	contentIndex  int
	nextOutputIdx int
	toolCalls     map[string]*responsesStreamToolCallState
	toolCallOrder []string
}

type responsesStreamToolCallState struct {
	CallID      string
	ItemID      string
	Name        string
	Arguments   strings.Builder
	OutputIndex int
	Added       bool
}

func newOpenAIResponsesStreamEncoder(options canonicalStreamOptions, w http.ResponseWriter, capture *streamCaptureState) *openAIResponsesStreamEncoder {
	return &openAIResponsesStreamEncoder{
		options:       options,
		w:             w,
		capture:       capture,
		responseID:    synthesizedResponseID(),
		createdAt:     time.Now().Unix(),
		textOutputIdx: -1,
		toolCalls:     map[string]*responsesStreamToolCallState{},
	}
}

func (e *openAIResponsesStreamEncoder) EncodeEvent(event canonicalStreamEvent) error {
	e.mergeMeta(event)
	switch event.Type {
	case canonicalStreamEventCreated:
		return e.sendResponseStartIfNeeded()
	case canonicalStreamEventTextDelta:
		if event.Delta == "" {
			return nil
		}
		if err := e.sendTextStartIfNeeded(); err != nil {
			return err
		}
		e.textBuffer.WriteString(event.Delta)
		e.sequence++
		eventName := protocolEventNameDefault(canonicalProtocolOpenAIResponses, canonicalStreamEventTextDelta, "response.output_text.delta")
		return e.writeEvent(eventName, map[string]any{
			"type":            eventName,
			"sequence_number": e.sequence,
			"item_id":         "msg_" + e.responseID,
			"output_index":    e.textOutputIdx,
			"content_index":   e.contentIndex,
			"delta":           event.Delta,
		})
	case canonicalStreamEventToolCallDelta:
		return e.sendToolCallDelta(event)
	case canonicalStreamEventReasoningDelta:
		return e.sendReasoningEvent(event)
	case canonicalStreamEventAdvanced:
		return e.sendAdvancedEvent(event)
	case canonicalStreamEventAnnotationAdded:
		appendStreamAnnotation(e.capture, event.Annotation)
		if isGrokSite(e.options.Candidate.Site.SiteType) {
			if err := e.sendTextStartIfNeeded(); err != nil {
				return err
			}
			e.sequence++
			eventName := protocolEventNameDefault(canonicalProtocolOpenAIResponses, canonicalStreamEventAnnotationAdded, "response.output_text.annotation.added")
			return e.writeEvent(eventName, map[string]any{
				"type":             eventName,
				"sequence_number":  e.sequence,
				"item_id":          "msg_" + e.responseID,
				"output_index":     e.textOutputIdx,
				"content_index":    e.contentIndex,
				"annotation_index": len(e.capture.annotations) - 1,
				"annotation":       event.Annotation,
			})
		}
		return nil
	case canonicalStreamEventUsage:
		if event.Usage.TotalTokens > 0 || event.Usage.PromptTokens > 0 || event.Usage.CompletionTokens > 0 {
			e.capture.usage = event.Usage
		}
		return nil
	case canonicalStreamEventCompleted:
		return e.sendFinish()
	case canonicalStreamEventIncomplete:
		return e.sendIncomplete(event)
	case canonicalStreamEventError:
		e.capture.endReason = "upstream_stream_error"
		return fmt.Errorf("upstream stream failed: %s", nonEmptyString(event.ErrorMessage, "unknown error"))
	default:
		return nil
	}
}

func (e *openAIResponsesStreamEncoder) mergeMeta(event canonicalStreamEvent) {
	if event.ID != "" {
		e.responseID = event.ID
	}
	if event.Model != "" {
		e.model = event.Model
	}
	if event.CreatedAt > 0 {
		e.createdAt = event.CreatedAt
	}
}

func (e *openAIResponsesStreamEncoder) sendResponseStartIfNeeded() error {
	if e.responseSent {
		return nil
	}
	e.sequence++
	if err := e.writeEvent("response.created", map[string]any{
		"type":            "response.created",
		"sequence_number": e.sequence,
		"response":        e.responsePayload("in_progress", e.capture.usage),
	}); err != nil {
		return err
	}
	e.sequence++
	if err := e.writeEvent("response.in_progress", map[string]any{
		"type":            "response.in_progress",
		"sequence_number": e.sequence,
		"response":        e.responsePayload("in_progress", e.capture.usage),
	}); err != nil {
		return err
	}
	e.responseSent = true
	return nil
}

func (e *openAIResponsesStreamEncoder) sendTextStartIfNeeded() error {
	if e.textStarted {
		return nil
	}
	if err := e.sendResponseStartIfNeeded(); err != nil {
		return err
	}
	e.textOutputIdx = e.nextOutputIdx
	e.nextOutputIdx++
	e.sequence++
	if err := e.writeEvent("response.output_item.added", map[string]any{
		"type":            "response.output_item.added",
		"sequence_number": e.sequence,
		"output_index":    e.textOutputIdx,
		"item": map[string]any{
			"id":      "msg_" + e.responseID,
			"type":    "message",
			"status":  "in_progress",
			"role":    "assistant",
			"content": []any{},
		},
	}); err != nil {
		return err
	}
	e.sequence++
	if err := e.writeEvent("response.content_part.added", map[string]any{
		"type":            "response.content_part.added",
		"sequence_number": e.sequence,
		"item_id":         "msg_" + e.responseID,
		"output_index":    e.textOutputIdx,
		"content_index":   e.contentIndex,
		"part":            map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
	}); err != nil {
		return err
	}
	e.textStarted = true
	return nil
}

func (e *openAIResponsesStreamEncoder) sendToolCallDelta(event canonicalStreamEvent) error {
	callID := strings.TrimSpace(event.ToolCallID)
	if callID == "" {
		return nil
	}
	if err := e.sendResponseStartIfNeeded(); err != nil {
		return err
	}
	state := e.toolCalls[callID]
	if state == nil {
		state = &responsesStreamToolCallState{
			CallID:      callID,
			OutputIndex: e.nextOutputIdx,
		}
		e.nextOutputIdx++
		e.toolCalls[callID] = state
		e.toolCallOrder = append(e.toolCallOrder, callID)
	}
	if event.ToolCallName != "" && !state.Added {
		state.Name = event.ToolCallName
	}
	if !state.Added {
		if event.ToolCallArgumentsDelta != "" {
			state.Arguments.WriteString(event.ToolCallArgumentsDelta)
		}
		if state.Name == "" {
			return nil
		}
		if err := e.addToolCallState(state); err != nil {
			return err
		}
		return nil
	}
	if event.ToolCallArgumentsDelta == "" {
		return nil
	}
	state.Arguments.WriteString(event.ToolCallArgumentsDelta)
	if e.isCustomTool(state.Name) {
		return nil
	}
	e.sequence++
	eventName := protocolEventNameDefault(canonicalProtocolOpenAIResponses, canonicalStreamEventToolCallDelta, "response.function_call_arguments.delta")
	return e.writeEvent(eventName, map[string]any{
		"type":            eventName,
		"sequence_number": e.sequence,
		"item_id":         state.ItemID,
		"output_index":    state.OutputIndex,
		"delta":           event.ToolCallArgumentsDelta,
	})
}

func (e *openAIResponsesStreamEncoder) addToolCallState(state *responsesStreamToolCallState) error {
	itemType := "function_call"
	if e.isCustomTool(state.Name) {
		itemType = "custom_tool_call"
	}
	state.ItemID = responsesItemIDForType(itemType, state.ItemID, state.CallID)
	name, namespace := e.responseToolName(state.Name)
	item := map[string]any{
		"id":        state.ItemID,
		"type":      "function_call",
		"status":    "in_progress",
		"call_id":   state.CallID,
		"name":      name,
		"arguments": "",
	}
	if namespace != "" {
		item["namespace"] = namespace
	}
	if e.isCustomTool(state.Name) {
		item["type"] = "custom_tool_call"
		item["input"] = ""
		delete(item, "arguments")
	}
	e.sequence++
	if err := e.writeEvent("response.output_item.added", map[string]any{
		"type":            "response.output_item.added",
		"sequence_number": e.sequence,
		"output_index":    state.OutputIndex,
		"item":            item,
	}); err != nil {
		return err
	}
	state.Added = true
	if state.Arguments.Len() == 0 || e.isCustomTool(state.Name) {
		return nil
	}
	e.sequence++
	eventName := protocolEventNameDefault(canonicalProtocolOpenAIResponses, canonicalStreamEventToolCallDelta, "response.function_call_arguments.delta")
	return e.writeEvent(eventName, map[string]any{
		"type":            eventName,
		"sequence_number": e.sequence,
		"item_id":         state.ItemID,
		"output_index":    state.OutputIndex,
		"delta":           state.Arguments.String(),
	})
}

func (e *openAIResponsesStreamEncoder) sendReasoningEvent(event canonicalStreamEvent) error {
	if reasoningPolicyForProtocol(canonicalProtocolOpenAIResponses, event.ReasoningKind, e.options.Candidate) != "passthrough" {
		appendStreamAnnotation(e.capture, redactedReasoningAnnotation(event))
		return nil
	}
	if err := e.sendResponseStartIfNeeded(); err != nil {
		return err
	}
	eventName := strings.TrimSpace(event.ReasoningEventName)
	if eventName == "" {
		eventName = protocolEventNameDefault(canonicalProtocolOpenAIResponses, canonicalStreamEventReasoningDelta, "response.reasoning_delta")
	}
	e.sequence++
	payload := map[string]any{
		"type":            eventName,
		"sequence_number": e.sequence,
	}
	if event.Delta != "" {
		payload["delta"] = event.Delta
	}
	return e.writeEvent(eventName, payload)
}

func (e *openAIResponsesStreamEncoder) sendAdvancedEvent(event canonicalStreamEvent) error {
	if !advancedEventPassthroughAllowed(canonicalProtocolOpenAIResponses, event.AdvancedKind) {
		appendStreamAnnotation(e.capture, advancedStreamAnnotation(event))
		return nil
	}
	eventName := strings.TrimSpace(event.AdvancedEventName)
	if eventName == "" {
		appendStreamAnnotation(e.capture, advancedStreamAnnotation(event))
		return nil
	}
	if err := e.sendResponseStartIfNeeded(); err != nil {
		return err
	}
	e.sequence++
	payload := sanitizeAdvancedMap(event.AdvancedPayload)
	if payload == nil {
		payload = map[string]any{}
	}
	payload["type"] = eventName
	if _, ok := payload["sequence_number"]; !ok {
		payload["sequence_number"] = e.sequence
	}
	return e.writeEvent(eventName, payload)
}

func (e *openAIResponsesStreamEncoder) sendFinish() error {
	if err := e.validateToolCallArguments(); err != nil {
		return err
	}
	return e.sendTerminalResponse("completed", true, "done")
}

func (e *openAIResponsesStreamEncoder) sendIncomplete(event canonicalStreamEvent) error {
	return e.sendTerminalResponse("incomplete", false, "response_incomplete")
}

func (e *openAIResponsesStreamEncoder) sendTerminalResponse(status string, completed bool, endReason string) error {
	if e.completed {
		return nil
	}
	if err := e.sendResponseStartIfNeeded(); err != nil {
		return err
	}
	if e.textStarted {
		e.sequence++
		if err := e.writeEvent("response.output_text.done", map[string]any{
			"type":            "response.output_text.done",
			"sequence_number": e.sequence,
			"item_id":         "msg_" + e.responseID,
			"output_index":    e.textOutputIdx,
			"content_index":   e.contentIndex,
			"text":            e.textBuffer.String(),
		}); err != nil {
			return err
		}
		e.sequence++
		if err := e.writeEvent("response.content_part.done", map[string]any{
			"type":            "response.content_part.done",
			"sequence_number": e.sequence,
			"item_id":         "msg_" + e.responseID,
			"output_index":    e.textOutputIdx,
			"content_index":   e.contentIndex,
			"part":            map[string]any{"type": "output_text", "text": e.textBuffer.String(), "annotations": []any{}},
		}); err != nil {
			return err
		}
		e.sequence++
		if err := e.writeEvent("response.output_item.done", map[string]any{
			"type":            "response.output_item.done",
			"sequence_number": e.sequence,
			"output_index":    e.textOutputIdx,
			"item": map[string]any{
				"id":      "msg_" + e.responseID,
				"type":    "message",
				"status":  "completed",
				"role":    "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": e.textBuffer.String(), "annotations": []any{}}},
			},
		}); err != nil {
			return err
		}
	}
	for _, callID := range e.toolCallOrder {
		state := e.toolCalls[callID]
		if state == nil {
			continue
		}
		if !state.Added {
			if err := e.addToolCallState(state); err != nil {
				return err
			}
		}
		name, namespace := e.responseToolName(state.Name)
		item := map[string]any{
			"id":        state.ItemID,
			"type":      "function_call",
			"status":    "completed",
			"call_id":   state.CallID,
			"name":      name,
			"arguments": state.Arguments.String(),
		}
		if namespace != "" {
			item["namespace"] = namespace
		}
		if e.isCustomTool(state.Name) {
			input := bridgedCustomToolInputFromArguments(state.Arguments.String())
			e.sequence++
			if err := e.writeEvent("response.custom_tool_call_input.delta", map[string]any{
				"type":            "response.custom_tool_call_input.delta",
				"sequence_number": e.sequence,
				"item_id":         state.ItemID,
				"output_index":    state.OutputIndex,
				"delta":           input,
			}); err != nil {
				return err
			}
			e.sequence++
			if err := e.writeEvent("response.custom_tool_call_input.done", map[string]any{
				"type":            "response.custom_tool_call_input.done",
				"sequence_number": e.sequence,
				"item_id":         state.ItemID,
				"output_index":    state.OutputIndex,
				"input":           input,
			}); err != nil {
				return err
			}
			item["type"] = "custom_tool_call"
			item["input"] = input
			delete(item, "arguments")
		}
		e.sequence++
		if err := e.writeEvent("response.output_item.done", map[string]any{
			"type":            "response.output_item.done",
			"sequence_number": e.sequence,
			"output_index":    state.OutputIndex,
			"item":            item,
		}); err != nil {
			return err
		}
	}
	e.sequence++
	eventName := protocolEventNameDefault(canonicalProtocolOpenAIResponses, canonicalStreamEventCompleted, "response.completed")
	if status == "incomplete" {
		eventName = protocolEventNameDefault(canonicalProtocolOpenAIResponses, canonicalStreamEventIncomplete, "response.incomplete")
	}
	if err := e.writeEvent(eventName, map[string]any{
		"type":            eventName,
		"sequence_number": e.sequence,
		"response":        e.responsePayload(status, e.capture.usage),
	}); err != nil {
		return err
	}
	e.completed = true
	e.capture.sawDone = true
	e.capture.streamCompleted = completed
	e.capture.endReason = endReason
	return nil
}

func (e *openAIResponsesStreamEncoder) validateToolCallArguments() error {
	for _, callID := range e.toolCallOrder {
		state := e.toolCalls[callID]
		if state == nil {
			continue
		}
		if err := validateToolCallArguments(callID, state.Arguments.String()); err != nil {
			e.capture.endReason = "tool_call_arguments_invalid_json"
			return err
		}
	}
	return nil
}

func (e *openAIResponsesStreamEncoder) writeEvent(event string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := e.w.Write([]byte("event: " + event + "\n")); err != nil {
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

func (e *openAIResponsesStreamEncoder) responsePayload(status string, usage completionUsage) map[string]any {
	output := []any{}
	if (status == "completed" || status == "incomplete") && e.textStarted {
		output = append(output, map[string]any{
			"id":      "msg_" + e.responseID,
			"type":    "message",
			"status":  "completed",
			"role":    "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": e.textBuffer.String(), "annotations": []any{}}},
		})
	}
	if status == "completed" || status == "incomplete" {
		for _, callID := range e.toolCallOrder {
			state := e.toolCalls[callID]
			if state == nil {
				continue
			}
			name, namespace := e.responseToolName(state.Name)
			item := map[string]any{
				"id":        state.ItemID,
				"type":      "function_call",
				"status":    "completed",
				"call_id":   state.CallID,
				"name":      name,
				"arguments": state.Arguments.String(),
			}
			if namespace != "" {
				item["namespace"] = namespace
			}
			if e.isCustomTool(state.Name) {
				item["type"] = "custom_tool_call"
				item["input"] = bridgedCustomToolInputFromArguments(state.Arguments.String())
				delete(item, "arguments")
			}
			output = append(output, item)
		}
	}
	return map[string]any{
		"id":                 e.responseID,
		"object":             "response",
		"created_at":         e.createdAt,
		"status":             status,
		"background":         false,
		"error":              nil,
		"incomplete_details": incompleteDetailsPayload(status),
		"model":              e.model,
		"output":             output,
		"usage":              responsesUsagePayload(usage),
	}
}

func (e *openAIResponsesStreamEncoder) isCustomTool(name string) bool {
	_, ok := e.options.CustomTools[name]
	return ok
}

func (e *openAIResponsesStreamEncoder) responseToolName(name string) (string, string) {
	identity, ok := e.options.ResponseTools[name]
	if !ok {
		return name, ""
	}
	return identity.Name, identity.Namespace
}

func appendStreamAnnotation(capture *streamCaptureState, annotation any) {
	if capture == nil || annotation == nil {
		return
	}
	capture.annotations = append(capture.annotations, annotation)
}

type anthropicMessagesStreamEncoder struct {
	options           canonicalStreamOptions
	w                 http.ResponseWriter
	capture           *streamCaptureState
	responseID        string
	model             string
	createdAt         int64
	started           bool
	completed         bool
	nextIndex         int
	textIndex         int
	textStarted       bool
	textBuffer        strings.Builder
	thinkingIndex     int
	thinkingStarted   bool
	thinkingStopped   bool
	thinkingBuffer    strings.Builder
	thinkingSignature string
	toolCalls         map[string]*anthropicStreamToolCallState
	toolCallOrder     []string
}

type anthropicStreamToolCallState struct {
	Index     int
	CallID    string
	Name      string
	Arguments strings.Builder
	Started   bool
}

func newAnthropicMessagesStreamEncoder(options canonicalStreamOptions, w http.ResponseWriter, capture *streamCaptureState) *anthropicMessagesStreamEncoder {
	return &anthropicMessagesStreamEncoder{
		options:       options,
		w:             w,
		capture:       capture,
		responseID:    "msg_" + uuid.NewString(),
		createdAt:     time.Now().Unix(),
		textIndex:     -1,
		thinkingIndex: -1,
		toolCalls:     map[string]*anthropicStreamToolCallState{},
	}
}

func (e *anthropicMessagesStreamEncoder) EncodeEvent(event canonicalStreamEvent) error {
	e.mergeMeta(event)
	switch event.Type {
	case canonicalStreamEventCreated:
		return e.sendMessageStartIfNeeded()
	case canonicalStreamEventTextDelta:
		return e.sendTextDelta(event.Delta)
	case canonicalStreamEventToolCallDelta:
		return e.sendToolCallDelta(event)
	case canonicalStreamEventReasoningDelta:
		return e.sendReasoningEvent(event)
	case canonicalStreamEventAdvanced:
		appendStreamAnnotation(e.capture, advancedStreamAnnotation(event))
		return nil
	case canonicalStreamEventAnnotationAdded:
		appendStreamAnnotation(e.capture, event.Annotation)
		return nil
	case canonicalStreamEventUsage:
		if event.Usage.TotalTokens > 0 || event.Usage.PromptTokens > 0 || event.Usage.CompletionTokens > 0 {
			e.capture.usage = event.Usage
		}
		return nil
	case canonicalStreamEventCompleted:
		return e.sendFinish("end_turn", true, "done")
	case canonicalStreamEventIncomplete:
		return e.sendFinish("max_tokens", false, "response_incomplete")
	case canonicalStreamEventError:
		e.capture.endReason = "upstream_stream_error"
		return fmt.Errorf("upstream stream failed: %s", nonEmptyString(event.ErrorMessage, "unknown error"))
	default:
		return nil
	}
}

func (e *anthropicMessagesStreamEncoder) sendReasoningEvent(event canonicalStreamEvent) error {
	if reasoningPolicyForProtocol(canonicalProtocolAnthropicMessages, event.ReasoningKind, e.options.Candidate) != "passthrough" {
		appendStreamAnnotation(e.capture, redactedReasoningAnnotation(event))
		return nil
	}
	switch event.ReasoningKind {
	case "thinking":
		if event.Delta == "" {
			return nil
		}
		if err := e.sendThinkingStartIfNeeded(); err != nil {
			return err
		}
		e.thinkingBuffer.WriteString(event.Delta)
		return e.writeEvent(protocolEventNameDefault(canonicalProtocolAnthropicMessages, canonicalStreamEventReasoningDelta, "content_block_delta"), map[string]any{
			"type":  "content_block_delta",
			"index": e.thinkingIndex,
			"delta": map[string]any{"type": "thinking_delta", "thinking": event.Delta},
		})
	case "thinking_signature":
		if event.Delta == "" {
			return nil
		}
		if err := e.sendThinkingStartIfNeeded(); err != nil {
			return err
		}
		e.thinkingSignature += event.Delta
		return e.writeEvent(protocolEventNameDefault(canonicalProtocolAnthropicMessages, canonicalStreamEventReasoningDelta, "content_block_delta"), map[string]any{
			"type":  "content_block_delta",
			"index": e.thinkingIndex,
			"delta": map[string]any{"type": "signature_delta", "signature": event.Delta},
		})
	case "thinking_redacted":
		appendStreamAnnotation(e.capture, redactedReasoningAnnotation(event))
		return nil
	default:
		appendStreamAnnotation(e.capture, redactedReasoningAnnotation(event))
		return nil
	}
}

func (e *anthropicMessagesStreamEncoder) sendThinkingStartIfNeeded() error {
	if err := e.sendMessageStartIfNeeded(); err != nil {
		return err
	}
	if e.thinkingStarted && !e.thinkingStopped {
		return nil
	}
	e.thinkingIndex = e.nextIndex
	e.nextIndex++
	if err := e.writeEvent("content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         e.thinkingIndex,
		"content_block": map[string]any{"type": "thinking", "thinking": ""},
	}); err != nil {
		return err
	}
	e.thinkingStarted = true
	e.thinkingStopped = false
	return nil
}

func (e *anthropicMessagesStreamEncoder) stopThinkingIfOpen() error {
	if !e.thinkingStarted || e.thinkingStopped {
		return nil
	}
	if err := e.writeEvent("content_block_stop", map[string]any{"type": "content_block_stop", "index": e.thinkingIndex}); err != nil {
		return err
	}
	e.thinkingStopped = true
	return nil
}

func (e *anthropicMessagesStreamEncoder) mergeMeta(event canonicalStreamEvent) {
	if event.ID != "" {
		e.responseID = event.ID
	}
	if event.Model != "" {
		e.model = event.Model
	}
	if event.CreatedAt > 0 {
		e.createdAt = event.CreatedAt
	}
}

func (e *anthropicMessagesStreamEncoder) sendMessageStartIfNeeded() error {
	if e.started {
		return nil
	}
	e.started = true
	return e.writeEvent(protocolEventNameDefault(canonicalProtocolAnthropicMessages, canonicalStreamEventCreated, "message_start"), map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            responseIDOrFallback(e.responseID),
			"type":          "message",
			"role":          "assistant",
			"model":         e.model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         anthropicUsagePayload(gatewayUsage{PromptTokens: e.capture.usage.PromptTokens, CachedPromptTokens: e.capture.usage.CachedPromptTokens, CacheCreationInputTokens: e.capture.usage.CacheCreationInputTokens}),
		},
	})
}

func (e *anthropicMessagesStreamEncoder) sendTextDelta(delta string) error {
	if delta == "" {
		return nil
	}
	if err := e.sendMessageStartIfNeeded(); err != nil {
		return err
	}
	if err := e.stopThinkingIfOpen(); err != nil {
		return err
	}
	if !e.textStarted {
		e.textIndex = e.nextIndex
		e.nextIndex++
		if err := e.writeEvent("content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         e.textIndex,
			"content_block": map[string]any{"type": "text", "text": ""},
		}); err != nil {
			return err
		}
		e.textStarted = true
	}
	e.textBuffer.WriteString(delta)
	return e.writeEvent(protocolEventNameDefault(canonicalProtocolAnthropicMessages, canonicalStreamEventTextDelta, "content_block_delta"), map[string]any{
		"type":  "content_block_delta",
		"index": e.textIndex,
		"delta": map[string]any{"type": "text_delta", "text": delta},
	})
}

func (e *anthropicMessagesStreamEncoder) sendToolCallDelta(event canonicalStreamEvent) error {
	callID := strings.TrimSpace(event.ToolCallID)
	if callID == "" {
		return nil
	}
	if err := e.sendMessageStartIfNeeded(); err != nil {
		return err
	}
	if err := e.stopThinkingIfOpen(); err != nil {
		return err
	}
	state := e.toolCalls[callID]
	if state == nil {
		state = &anthropicStreamToolCallState{Index: e.nextIndex, CallID: callID}
		e.nextIndex++
		e.toolCalls[callID] = state
		e.toolCallOrder = append(e.toolCallOrder, callID)
	}
	if event.ToolCallName != "" {
		state.Name = event.ToolCallName
	}
	if !state.Started {
		contentBlock := map[string]any{
			"type":  "tool_use",
			"id":    state.CallID,
			"name":  state.Name,
			"input": map[string]any{},
		}
		addToolCallMetadataToAnthropicBlock(contentBlock, event.ToolCallMetadata)
		if err := e.writeEvent("content_block_start", map[string]any{
			"type":          "content_block_start",
			"index":         state.Index,
			"content_block": contentBlock,
		}); err != nil {
			return err
		}
		state.Started = true
	}
	if event.ToolCallArgumentsDelta == "" {
		return nil
	}
	state.Arguments.WriteString(event.ToolCallArgumentsDelta)
	return e.writeEvent(protocolEventNameDefault(canonicalProtocolAnthropicMessages, canonicalStreamEventToolCallDelta, "content_block_delta"), map[string]any{
		"type":  "content_block_delta",
		"index": state.Index,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": event.ToolCallArgumentsDelta},
	})
}

func (e *anthropicMessagesStreamEncoder) sendFinish(stopReason string, completed bool, endReason string) error {
	if e.completed {
		return nil
	}
	if err := e.validateToolCallArguments(); err != nil {
		return err
	}
	if err := e.sendMessageStartIfNeeded(); err != nil {
		return err
	}
	if e.textStarted {
		if err := e.writeEvent("content_block_stop", map[string]any{"type": "content_block_stop", "index": e.textIndex}); err != nil {
			return err
		}
	}
	if err := e.stopThinkingIfOpen(); err != nil {
		return err
	}
	hasToolCalls := false
	for _, callID := range e.toolCallOrder {
		state := e.toolCalls[callID]
		if state == nil {
			continue
		}
		hasToolCalls = true
		if err := e.writeEvent("content_block_stop", map[string]any{"type": "content_block_stop", "index": state.Index}); err != nil {
			return err
		}
	}
	if hasToolCalls && stopReason == "end_turn" {
		stopReason = "tool_use"
	}
	if err := e.writeEvent("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": e.capture.usage.CompletionTokens},
	}); err != nil {
		return err
	}
	messageStopEventName := protocolEventNameDefault(canonicalProtocolAnthropicMessages, canonicalStreamEventCompleted, "message_stop")
	if err := e.writeEvent(messageStopEventName, map[string]any{"type": messageStopEventName}); err != nil {
		return err
	}
	e.completed = true
	e.capture.sawDone = true
	e.capture.streamCompleted = completed
	e.capture.endReason = endReason
	return nil
}

func (e *anthropicMessagesStreamEncoder) validateToolCallArguments() error {
	for _, callID := range e.toolCallOrder {
		state := e.toolCalls[callID]
		if state == nil {
			continue
		}
		if err := validateToolCallArguments(callID, state.Arguments.String()); err != nil {
			e.capture.endReason = "tool_call_arguments_invalid_json"
			return err
		}
	}
	return nil
}

func (e *anthropicMessagesStreamEncoder) writeEvent(event string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := e.w.Write([]byte("event: " + event + "\n")); err != nil {
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

func validateToolCallArguments(callID string, arguments string) error {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return nil
	}
	if json.Valid([]byte(arguments)) {
		return nil
	}
	return fmt.Errorf("tool call %s arguments are not valid JSON", nonEmptyString(callID, "unknown"))
}

func reasoningKindFromResponsesEvent(eventName string) string {
	switch {
	case strings.Contains(eventName, "encrypted_content"):
		return "encrypted_reasoning"
	case strings.Contains(eventName, "summary"):
		return "reasoning_summary"
	default:
		return "reasoning"
	}
}

func reasoningPolicyForProtocol(protocol canonicalProtocol, kind string, candidate routeengine.Candidate) string {
	kind = strings.TrimSpace(kind)
	spec := effectiveProtocolSpec(protocol, candidate)
	if kind != "" {
		if policy := strings.TrimSpace(spec.Reasoning[kind]); policy != "" {
			return policy
		}
	}
	if policy := strings.TrimSpace(spec.Reasoning["default"]); policy != "" {
		return policy
	}
	return "metadata"
}

func redactedReasoningAnnotation(event canonicalStreamEvent) map[string]any {
	annotationType := nonEmptyString(event.ReasoningEventName, event.ReasoningKind, string(event.Type))
	return map[string]any{
		"type":  annotationType,
		"value": "[redacted]",
	}
}

func advancedEventFromResponsesStreamEvent(event responsesStreamEvent, raw map[string]any) (canonicalStreamEvent, bool) {
	eventName := strings.TrimSpace(event.Type)
	if eventName == "" || raw == nil {
		return canonicalStreamEvent{}, false
	}
	kind, ok := advancedKindFromResponsesStreamEvent(event, raw)
	if !ok {
		return canonicalStreamEvent{}, false
	}
	out := canonicalStreamEvent{
		Type:              canonicalStreamEventAdvanced,
		AdvancedKind:      kind,
		AdvancedPhase:     advancedPhaseFromResponsesStreamEvent(eventName),
		AdvancedEventName: eventName,
		AdvancedPayload:   sanitizeAdvancedMap(raw),
		ToolCallID:        nonEmptyString(event.ItemID, nestedStringFromMap(raw, "item", "call_id"), nestedStringFromMap(raw, "item", "id"), nestedStringFromMap(raw, "call_id")),
		ToolCallName:      nonEmptyString(nestedStringFromMap(raw, "item", "name"), nestedStringFromMap(raw, "name")),
	}
	return out, true
}

func advancedKindFromResponsesStreamEvent(event responsesStreamEvent, raw map[string]any) (string, bool) {
	eventName := strings.ToLower(strings.TrimSpace(event.Type))
	itemType := ""
	if event.Item != nil {
		itemType = strings.ToLower(strings.TrimSpace(event.Item.Type))
	}
	if itemType == "" {
		itemType = strings.ToLower(strings.TrimSpace(nestedStringFromMap(raw, "item", "type")))
	}
	partType := strings.ToLower(strings.TrimSpace(nestedStringFromMap(raw, "part", "type")))
	if strings.HasPrefix(eventName, "response.output_item.") {
		switch itemType {
		case "", "message", "function_call":
			return "", false
		default:
			return advancedKindFromTokens(eventName + " " + itemType), true
		}
	}
	if strings.HasPrefix(eventName, "response.content_part.") {
		switch partType {
		case "", "output_text", "refusal":
			return "", false
		default:
			return advancedKindFromTokens(eventName + " " + partType), true
		}
	}
	if isKnownResponsesStreamEvent(eventName) {
		return "", false
	}
	if strings.HasPrefix(eventName, "response.") {
		return advancedKindFromTokens(eventName + " " + itemType + " " + partType), true
	}
	return "", false
}

func isKnownResponsesStreamEvent(eventName string) bool {
	switch eventName {
	case "response.created", "response.in_progress", "response.completed", "response.incomplete", "response.error", "response.failed",
		"response.output_text.delta", "response.output_text.done", "response.refusal.delta", "response.refusal.done",
		"response.output_text.annotation.added", "response.reasoning_summary_text.delta", "response.reasoning_text.delta",
		"response.reasoning_delta", "response.reasoning_summary.delta", "response.reasoning.encrypted_content.delta",
		"response.reasoning.encrypted_content.done", "response.function_call_arguments.delta", "response.function_call_arguments.done":
		return true
	default:
		return false
	}
}

func advancedKindFromTokens(tokens string) string {
	switch {
	case strings.Contains(tokens, "function_call_output"), strings.Contains(tokens, "tool_result"):
		return "tool_result"
	case strings.Contains(tokens, "mcp"):
		return "mcp"
	case strings.Contains(tokens, "approval"), strings.Contains(tokens, "authorization"):
		return "approval"
	case strings.Contains(tokens, "computer"):
		return "computer_use"
	case strings.Contains(tokens, "web_search"):
		return "web_search"
	case strings.Contains(tokens, "shell"), strings.Contains(tokens, "terminal"), strings.Contains(tokens, "command"):
		return "shell"
	case strings.Contains(tokens, "apply_patch"), strings.Contains(tokens, "patch"):
		return "patch"
	case strings.Contains(tokens, "file"):
		return "file"
	case strings.Contains(tokens, "code_interpreter"), strings.Contains(tokens, "code_execution"):
		return "code_execution"
	case strings.Contains(tokens, "image_generation"), strings.Contains(tokens, "partial_image"), strings.Contains(tokens, "image"):
		return "image"
	case strings.Contains(tokens, "audio"):
		return "audio"
	case strings.Contains(tokens, "transcript"):
		return "transcript"
	case strings.Contains(tokens, "todo"):
		return "todo"
	case strings.Contains(tokens, "plan"):
		return "plan"
	case strings.Contains(tokens, "subagent"):
		return "subagent"
	default:
		return "provider_event"
	}
}

func advancedPhaseFromResponsesStreamEvent(eventName string) string {
	eventName = strings.ToLower(strings.TrimSpace(eventName))
	switch {
	case strings.HasSuffix(eventName, ".added"), strings.HasSuffix(eventName, ".created"), strings.HasSuffix(eventName, ".started"), strings.HasSuffix(eventName, "_started"):
		return "started"
	case strings.HasSuffix(eventName, ".delta"), strings.HasSuffix(eventName, "_delta"), strings.HasSuffix(eventName, ".partial_image"), strings.HasSuffix(eventName, ".in_progress"):
		return "delta"
	case strings.HasSuffix(eventName, ".done"), strings.HasSuffix(eventName, "_done"), strings.HasSuffix(eventName, ".completed"):
		return "done"
	case strings.HasSuffix(eventName, ".failed"), strings.HasSuffix(eventName, "_failed"), strings.HasSuffix(eventName, ".error"):
		return "failed"
	default:
		return "event"
	}
}

func advancedEventPassthroughAllowed(protocol canonicalProtocol, kind string) bool {
	switch protocol {
	case canonicalProtocolOpenAIResponses, canonicalProtocolCodexResponses:
		return true
	default:
		return false
	}
}

func advancedStreamAnnotation(event canonicalStreamEvent) map[string]any {
	annotationType := nonEmptyString(event.AdvancedEventName, event.AdvancedKind, string(event.Type))
	annotation := map[string]any{
		"type":  annotationType,
		"kind":  nonEmptyString(event.AdvancedKind, "provider_event"),
		"phase": nonEmptyString(event.AdvancedPhase, "event"),
		"value": "[advanced_event]",
	}
	if event.ToolCallID != "" {
		annotation["call_id"] = event.ToolCallID
	}
	if event.ToolCallName != "" {
		annotation["name"] = event.ToolCallName
	}
	if event.AdvancedPayload != nil {
		annotation["payload"] = sanitizeAdvancedMap(event.AdvancedPayload)
	}
	return annotation
}

func sanitizeAdvancedMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	sanitized, ok := sanitizeAdvancedPayload(value, 0).(map[string]any)
	if !ok {
		return nil
	}
	return sanitized
}

func sanitizeAdvancedPayload(value any, depth int) any {
	if depth > 6 {
		return "[truncated]"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		count := 0
		for key, child := range typed {
			if count >= 64 {
				out["_truncated"] = true
				break
			}
			count++
			if isSensitiveAdvancedKey(key) {
				out[key] = "[redacted]"
				continue
			}
			out[key] = sanitizeAdvancedPayload(child, depth+1)
		}
		return out
	case []any:
		limit := len(typed)
		truncated := false
		if limit > 64 {
			limit = 64
			truncated = true
		}
		out := make([]any, 0, limit+1)
		for i := 0; i < limit; i++ {
			out = append(out, sanitizeAdvancedPayload(typed[i], depth+1))
		}
		if truncated {
			out = append(out, "[truncated]")
		}
		return out
	case string:
		if len(typed) > 4096 {
			return typed[:4096] + "...[truncated]"
		}
		return typed
	default:
		return typed
	}
}

func isSensitiveAdvancedKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, token := range []string{"authorization", "api_key", "apikey", "access_token", "refresh_token", "secret", "password", "credential"} {
		if strings.Contains(key, token) {
			return true
		}
	}
	return key == "key" || key == "token"
}

func nestedStringFromMap(root map[string]any, path ...string) string {
	var current any = root
	for _, part := range path {
		next, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = next[part]
	}
	switch value := current.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func incompleteDetailsPayload(status string) any {
	if status != "incomplete" {
		return nil
	}
	return map[string]any{"reason": "stream_incomplete"}
}

func responseIDFromResponsesStreamEvent(event responsesStreamEvent) string {
	if event.Response == nil {
		return ""
	}
	return event.Response.ID
}

func modelFromResponsesStreamEvent(event responsesStreamEvent) string {
	if event.Response == nil {
		return ""
	}
	return event.Response.Model
}

func createdAtFromResponsesStreamEvent(event responsesStreamEvent) int64 {
	if event.Response == nil {
		return 0
	}
	return event.Response.CreatedAt
}

func errorMessageFromResponsesStreamEvent(event responsesStreamEvent) string {
	if event.Error != nil {
		return nonEmptyString(event.Error.Message, event.Error.Code, event.Error.Type, event.Type)
	}
	if event.Response != nil && event.Response.Error != nil {
		encoded, err := json.Marshal(event.Response.Error)
		if err == nil && len(encoded) > 0 {
			return string(encoded)
		}
	}
	return nonEmptyString(event.Type, "upstream stream failed")
}

func anthropicStreamErrorMessage(value map[string]any) string {
	if value == nil {
		return "anthropic stream returned an error"
	}
	return nonEmptyString(
		stringFromMapAny(value, "message"),
		stringFromMapAny(value, "type"),
		"anthropic stream returned an error",
	)
}

func antigravityStreamErrorMessage(root map[string]any) string {
	if root == nil {
		return ""
	}
	switch value := root["error"].(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		return nonEmptyString(
			stringFromMapAny(value, "message"),
			stringFromMapAny(value, "code"),
			stringFromMapAny(value, "status"),
			"antigravity stream returned an error",
		)
	}
	return ""
}

func sseDataFromLine(line []byte) (string, bool, bool) {
	text := strings.TrimSpace(string(line))
	if !strings.HasPrefix(text, "data:") {
		return "", false, false
	}
	data := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
	if data == "" {
		return "", false, false
	}
	if data == "[DONE]" {
		return "", true, true
	}
	return data, false, true
}
