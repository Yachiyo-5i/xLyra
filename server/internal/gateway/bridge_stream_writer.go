package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type bridgeStreamWriter struct {
	mu          sync.Mutex
	dst         http.ResponseWriter
	innerHeader http.Header
	pending     bytes.Buffer
	writeErr    error

	started  bool
	finished bool

	seq             int
	nextOutputIndex int
	responseID      string
	model           string
	createdAt       int64

	indexMap        map[int]int
	suppressedItems map[string]bool
	suppressedIdx   map[int]bool
	pendingCalls    []bridgeFunctionCall
	heldTerminal    map[string]any

	emittedOutput  []any
	usage          bridgeUsageTotals
	contentFlushed bool
}

type bridgeUsageTotals struct {
	InputTokens     int64
	OutputTokens    int64
	TotalTokens     int64
	CachedTokens    int64
	ReasoningTokens int64
}

func newBridgeStreamWriter(dst http.ResponseWriter, model string) *bridgeStreamWriter {
	return &bridgeStreamWriter{
		dst:         dst,
		innerHeader: http.Header{},
		responseID:  "resp_bridge_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		model:       model,
		createdAt:   time.Now().Unix(),
	}
}

func (b *bridgeStreamWriter) StartEnvelope() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started {
		return
	}
	b.dst.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	b.dst.Header().Set("Cache-Control", "no-cache")
	b.dst.Header().Set("Connection", "keep-alive")
	b.dst.WriteHeader(http.StatusOK)
	b.started = true
	envelope := b.envelopePayload("in_progress")
	b.forwardEventLocked("response.created", map[string]any{"type": "response.created", "response": envelope})
	b.forwardEventLocked("response.in_progress", map[string]any{"type": "response.in_progress", "response": envelope})
}

func (b *bridgeStreamWriter) envelopePayload(status string) map[string]any {
	return map[string]any{
		"id":                 b.responseID,
		"object":             "response",
		"created_at":         b.createdAt,
		"status":             status,
		"background":         false,
		"error":              nil,
		"incomplete_details": nil,
		"model":              b.model,
		"output":             []any{},
	}
}

func (b *bridgeStreamWriter) beginRound() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.indexMap = map[int]int{}
	b.suppressedItems = map[string]bool{}
	b.suppressedIdx = map[int]bool{}
	b.pendingCalls = nil
	b.heldTerminal = nil
	b.pending.Reset()
}

func (b *bridgeStreamWriter) takeRound() ([]bridgeFunctionCall, []any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	calls := b.pendingCalls
	b.pendingCalls = nil
	var replay []any
	if b.heldTerminal != nil {
		if output, ok := b.heldTerminal["output"].([]any); ok {
			replay = bridgeRoundOutputForReplay(output)
		}
	}
	b.heldTerminal = nil
	return calls, replay
}

func (b *bridgeStreamWriter) ContentFlushed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.contentFlushed
}

func (b *bridgeStreamWriter) Started() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.started
}

func (b *bridgeStreamWriter) Finished() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.finished
}

func (b *bridgeStreamWriter) CompleteGracefully() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.forwardTerminalLocked("response.completed", "completed", nil)
}

func (b *bridgeStreamWriter) FailAll(code string, message string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.started {
		return false
	}
	if b.finished {
		return true
	}
	envelope := b.envelopePayload("failed")
	envelope["error"] = map[string]any{"code": code, "message": message}
	envelope["output"] = b.emittedOutput
	b.forwardEventLocked("response.failed", map[string]any{"type": "response.failed", "response": envelope})
	b.finished = true
	b.finishDownstreamLocked()
	return true
}

func (b *bridgeStreamWriter) Close() {}

func (b *bridgeStreamWriter) Header() http.Header { return b.innerHeader }

func (b *bridgeStreamWriter) WriteHeader(int) {}

func (b *bridgeStreamWriter) Flush() {}

func (b *bridgeStreamWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.writeErr != nil {
		return 0, b.writeErr
	}
	b.pending.Write(p)
	for {
		raw := b.pending.Bytes()
		end := bytes.Index(raw, []byte("\n\n"))
		if end < 0 {
			break
		}
		block := make([]byte, end)
		copy(block, raw[:end])
		b.pending.Next(end + 2)
		b.handleEventBlockLocked(block)
		if b.writeErr != nil {
			return 0, b.writeErr
		}
	}
	return len(p), nil
}

func (b *bridgeStreamWriter) handleEventBlockLocked(block []byte) {
	var data []byte
	for _, line := range bytes.Split(block, []byte("\n")) {
		if value, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
			data = append(data, value...)
		}
	}
	if len(data) == 0 {
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}
	eventType := strings.TrimSpace(anyString(payload["type"]))
	if eventType == "" {
		return
	}

	switch eventType {
	case "response.created", "response.in_progress":
		return
	case "response.output_item.added":
		if b.itemIsBridgedCall(payload) {
			b.markSuppressedLocked(payload)
			return
		}
	case "response.output_item.done":
		if b.eventIsSuppressedLocked(payload) {
			b.captureBridgedCallLocked(payload)
			return
		}
	case "response.completed":
		response, _ := payload["response"].(map[string]any)
		b.accumulateUsageLocked(response)
		if len(b.pendingCalls) > 0 {
			b.heldTerminal = response
			return
		}
		b.forwardTerminalLocked("response.completed", "completed", response)
		return
	case "response.incomplete":
		response, _ := payload["response"].(map[string]any)
		b.accumulateUsageLocked(response)
		b.forwardTerminalLocked("response.incomplete", "incomplete", response)
		return
	case "response.failed", "response.error", "error":
		b.forwardEventLocked(eventType, payload)
		b.finished = true
		b.finishDownstreamLocked()
		return
	}

	if b.eventIsSuppressedLocked(payload) {
		return
	}
	b.remapOutputIndexLocked(payload)
	b.forwardEventLocked(eventType, payload)
	b.contentFlushed = true
	if eventType == "response.output_item.done" {
		if item, ok := payload["item"].(map[string]any); ok {
			b.emittedOutput = append(b.emittedOutput, item)
		}
	}
}

func (b *bridgeStreamWriter) itemIsBridgedCall(payload map[string]any) bool {
	item, _ := payload["item"].(map[string]any)
	if item == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(anyString(item["type"])), "function_call") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(anyString(item["name"])), bridgeImageFunctionName)
}

func (b *bridgeStreamWriter) markSuppressedLocked(payload map[string]any) {
	item, _ := payload["item"].(map[string]any)
	if id := strings.TrimSpace(anyString(item["id"])); id != "" {
		b.suppressedItems[id] = true
	}
	if idx, ok := payloadInt(payload["output_index"]); ok {
		b.suppressedIdx[idx] = true
	}
}

func (b *bridgeStreamWriter) eventIsSuppressedLocked(payload map[string]any) bool {
	if id := strings.TrimSpace(anyString(payload["item_id"])); id != "" && b.suppressedItems[id] {
		return true
	}
	if item, ok := payload["item"].(map[string]any); ok {
		if id := strings.TrimSpace(anyString(item["id"])); id != "" && b.suppressedItems[id] {
			return true
		}
	}
	if idx, ok := payloadInt(payload["output_index"]); ok && b.suppressedIdx[idx] {
		return true
	}
	return false
}

func (b *bridgeStreamWriter) captureBridgedCallLocked(payload map[string]any) {
	item, _ := payload["item"].(map[string]any)
	if item == nil {
		return
	}
	callID := strings.TrimSpace(anyString(item["call_id"]))
	if callID == "" {
		callID = strings.TrimSpace(anyString(item["id"]))
	}
	b.pendingCalls = append(b.pendingCalls, bridgeFunctionCall{
		CallID:    callID,
		Arguments: anyString(item["arguments"]),
	})
}

func (b *bridgeStreamWriter) remapOutputIndexLocked(payload map[string]any) {
	idx, ok := payloadInt(payload["output_index"])
	if !ok {
		return
	}
	mapped, seen := b.indexMap[idx]
	if !seen {
		mapped = b.nextOutputIndex
		b.nextOutputIndex++
		b.indexMap[idx] = mapped
	}
	payload["output_index"] = mapped
}

func (b *bridgeStreamWriter) forwardTerminalLocked(eventType string, status string, upstream map[string]any) {
	if b.finished {
		return
	}
	envelope := b.envelopePayload(status)
	if upstream != nil {
		if details, ok := upstream["incomplete_details"]; ok {
			envelope["incomplete_details"] = details
		}
	}
	envelope["output"] = b.emittedOutput
	envelope["usage"] = b.usagePayloadLocked()
	b.forwardEventLocked(eventType, map[string]any{"type": eventType, "response": envelope})
	b.finished = true
	b.finishDownstreamLocked()
}

func (b *bridgeStreamWriter) finishDownstreamLocked() {
	if writer, ok := b.dst.(downstreamSSELifecycleWriter); ok {
		writer.FinishSSE()
	}
}

func (b *bridgeStreamWriter) accumulateUsageLocked(response map[string]any) {
	if response == nil {
		return
	}
	usage, _ := response["usage"].(map[string]any)
	if usage == nil {
		return
	}
	b.usage.InputTokens += payloadInt64(usage["input_tokens"])
	b.usage.OutputTokens += payloadInt64(usage["output_tokens"])
	b.usage.TotalTokens += payloadInt64(usage["total_tokens"])
	if details, ok := usage["input_tokens_details"].(map[string]any); ok {
		b.usage.CachedTokens += payloadInt64(details["cached_tokens"])
	}
	if details, ok := usage["output_tokens_details"].(map[string]any); ok {
		b.usage.ReasoningTokens += payloadInt64(details["reasoning_tokens"])
	}
}

func (b *bridgeStreamWriter) usagePayloadLocked() map[string]any {
	return map[string]any{
		"input_tokens":  b.usage.InputTokens,
		"output_tokens": b.usage.OutputTokens,
		"total_tokens":  b.usage.TotalTokens,
		"input_tokens_details": map[string]any{
			"cached_tokens": b.usage.CachedTokens,
		},
		"output_tokens_details": map[string]any{
			"reasoning_tokens": b.usage.ReasoningTokens,
		},
	}
}

func (b *bridgeStreamWriter) InjectImageStart() (string, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	itemID := "ig_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	index := b.nextOutputIndex
	b.nextOutputIndex++
	b.forwardEventLocked("response.output_item.added", map[string]any{
		"type":         "response.output_item.added",
		"output_index": index,
		"item": map[string]any{
			"id":     itemID,
			"type":   "image_generation_call",
			"status": "in_progress",
		},
	})
	b.forwardEventLocked("response.image_generation_call.in_progress", map[string]any{
		"type":         "response.image_generation_call.in_progress",
		"item_id":      itemID,
		"output_index": index,
	})
	b.forwardEventLocked("response.image_generation_call.generating", map[string]any{
		"type":         "response.image_generation_call.generating",
		"item_id":      itemID,
		"output_index": index,
	})
	b.contentFlushed = true
	return itemID, index
}

func (b *bridgeStreamWriter) InjectImageResult(itemID string, index int, spec bridgeImageToolSpec, outcome bridgeImageOutcome) {
	b.mu.Lock()
	defer b.mu.Unlock()
	item := map[string]any{
		"id":   itemID,
		"type": "image_generation_call",
	}
	if outcome.OK {
		b.forwardEventLocked("response.image_generation_call.completed", map[string]any{
			"type":         "response.image_generation_call.completed",
			"item_id":      itemID,
			"output_index": index,
		})
		item["status"] = "completed"
		item["result"] = outcome.B64
		if outcome.RevisedPrompt != "" {
			item["revised_prompt"] = outcome.RevisedPrompt
		}
		if spec.Size != "" {
			item["size"] = spec.Size
		}
		if spec.Quality != "" {
			item["quality"] = spec.Quality
		}
		if spec.OutputFormat != "" {
			item["output_format"] = spec.OutputFormat
		}
	} else {
		item["status"] = "failed"
	}
	b.forwardEventLocked("response.output_item.done", map[string]any{
		"type":         "response.output_item.done",
		"output_index": index,
		"item":         item,
	})
	b.emittedOutput = append(b.emittedOutput, item)
}

func (b *bridgeStreamWriter) forwardEventLocked(eventType string, payload map[string]any) {
	if b.writeErr != nil || !b.started {
		return
	}
	b.seq++
	payload["sequence_number"] = b.seq
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if _, err := b.dst.Write([]byte("event: " + eventType + "\ndata: ")); err != nil {
		b.writeErr = err
		return
	}
	if _, err := b.dst.Write(encoded); err != nil {
		b.writeErr = err
		return
	}
	if _, err := b.dst.Write([]byte("\n\n")); err != nil {
		b.writeErr = err
		return
	}
	b.flushLocked()
}

func (b *bridgeStreamWriter) flushLocked() {
	if flusher, ok := b.dst.(http.Flusher); ok {
		flusher.Flush()
	}
}

func payloadInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed), true
		}
	}
	return 0, false
}

func payloadInt64(value any) int64 {
	if parsed, ok := payloadInt(value); ok {
		return int64(parsed)
	}
	return 0
}
