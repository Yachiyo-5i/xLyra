package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

type bridgeTestEvent struct {
	Name    string
	Payload map[string]any
}

func parseBridgeSSE(t *testing.T, body string) []bridgeTestEvent {
	t.Helper()
	events := []bridgeTestEvent{}
	for _, block := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(block) == "" || strings.HasPrefix(strings.TrimSpace(block), ":") {
			continue
		}
		event := bridgeTestEvent{}
		for _, line := range strings.Split(block, "\n") {
			if value, ok := strings.CutPrefix(line, "event: "); ok {
				event.Name = value
			}
			if value, ok := strings.CutPrefix(line, "data: "); ok {
				if err := json.Unmarshal([]byte(value), &event.Payload); err != nil {
					t.Fatalf("invalid event payload %q: %v", value, err)
				}
			}
		}
		if event.Name != "" {
			events = append(events, event)
		}
	}
	return events
}

func writeBridgeUpstreamEvent(t *testing.T, w *bridgeStreamWriter, payload map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal upstream event: %v", err)
	}
	name := anyString(payload["type"])
	if _, err := w.Write([]byte("event: " + name + "\ndata: " + string(encoded) + "\n\n")); err != nil {
		t.Fatalf("write upstream event: %v", err)
	}
}

func TestBridgeStreamWriterMultiRound(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := newBridgeStreamWriter(recorder, "gpt-5.5")
	defer writer.Close()
	writer.StartEnvelope()

	writer.beginRound()
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.created", "sequence_number": 1, "response": map[string]any{"id": "resp_up1", "model": "claude-opus"}})
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.in_progress", "sequence_number": 2, "response": map[string]any{"id": "resp_up1"}})
	messageItem := map[string]any{"id": "msg_up1", "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.output_item.added", "sequence_number": 3, "output_index": 0, "item": messageItem})
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.output_text.delta", "sequence_number": 4, "item_id": "msg_up1", "output_index": 0, "delta": "好的，我来画"})
	doneMessage := map[string]any{"id": "msg_up1", "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "好的，我来画"}}}
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.output_item.done", "sequence_number": 5, "output_index": 0, "item": doneMessage})
	callItem := map[string]any{"id": "fc_1", "type": "function_call", "status": "in_progress", "call_id": "call_1", "name": bridgeImageFunctionName, "arguments": ""}
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.output_item.added", "sequence_number": 6, "output_index": 1, "item": callItem})
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.function_call_arguments.delta", "sequence_number": 7, "item_id": "fc_1", "output_index": 1, "delta": `{"prompt":"a cat"}`})
	callDone := map[string]any{"id": "fc_1", "type": "function_call", "status": "completed", "call_id": "call_1", "name": bridgeImageFunctionName, "arguments": `{"prompt":"a cat"}`}
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.output_item.done", "sequence_number": 8, "output_index": 1, "item": callDone})
	writeBridgeUpstreamEvent(t, writer, map[string]any{
		"type": "response.completed", "sequence_number": 9,
		"response": map[string]any{
			"id":     "resp_up1",
			"output": []any{doneMessage, callDone},
			"usage":  map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15},
		},
	})

	calls, replay := writer.takeRound()
	if len(calls) != 1 || calls[0].CallID != "call_1" {
		t.Fatalf("expected one captured bridged call, got %+v", calls)
	}
	if calls[0].Prompt() != "a cat" {
		t.Fatalf("expected prompt extracted, got %q", calls[0].Prompt())
	}
	if len(replay) != 2 {
		t.Fatalf("expected message + function_call replay items, got %d", len(replay))
	}

	itemID, index := writer.InjectImageStart()
	if index != 1 {
		t.Fatalf("expected image item at downstream index 1 (function call suppressed), got %d", index)
	}
	writer.InjectImageResult(itemID, index, bridgeImageToolSpec{Size: "1024x1024"}, bridgeImageOutcome{OK: true, B64: "aGk=", RevisedPrompt: "a fluffy cat"})

	writer.beginRound()
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.created", "sequence_number": 1, "response": map[string]any{"id": "resp_up2"}})
	closing := map[string]any{"id": "msg_up2", "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "画好了"}}}
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.output_item.added", "sequence_number": 2, "output_index": 0, "item": map[string]any{"id": "msg_up2", "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.output_text.delta", "sequence_number": 3, "item_id": "msg_up2", "output_index": 0, "delta": "画好了"})
	writeBridgeUpstreamEvent(t, writer, map[string]any{"type": "response.output_item.done", "sequence_number": 4, "output_index": 0, "item": closing})
	writeBridgeUpstreamEvent(t, writer, map[string]any{
		"type": "response.completed", "sequence_number": 5,
		"response": map[string]any{
			"id":     "resp_up2",
			"output": []any{closing},
			"usage":  map[string]any{"input_tokens": 20, "output_tokens": 7, "total_tokens": 27},
		},
	})

	if calls, _ := writer.takeRound(); len(calls) != 0 {
		t.Fatalf("expected no bridged calls in round 2, got %+v", calls)
	}
	if !writer.Finished() {
		t.Fatal("expected writer finished after terminal round")
	}

	events := parseBridgeSSE(t, recorder.Body.String())

	if events[0].Name != "response.created" || events[1].Name != "response.in_progress" {
		t.Fatalf("expected synthetic envelope first, got %s/%s", events[0].Name, events[1].Name)
	}
	syntheticID := anyString(events[0].Payload["response"].(map[string]any)["id"])
	if !strings.HasPrefix(syntheticID, "resp_bridge_") {
		t.Fatalf("expected synthetic response id, got %q", syntheticID)
	}

	lastSeq := 0
	sawFunctionEvent := false
	createdCount := 0
	var terminal map[string]any
	for _, event := range events {
		seq, ok := payloadInt(event.Payload["sequence_number"])
		if !ok || seq != lastSeq+1 {
			t.Fatalf("expected contiguous sequence numbers, got %d after %d (%s)", seq, lastSeq, event.Name)
		}
		lastSeq = seq
		if strings.Contains(event.Name, "function_call") {
			sawFunctionEvent = true
		}
		if item, ok := event.Payload["item"].(map[string]any); ok {
			if anyString(item["type"]) == "function_call" {
				sawFunctionEvent = true
			}
		}
		if event.Name == "response.created" {
			createdCount++
		}
		if event.Name == "response.completed" {
			terminal, _ = event.Payload["response"].(map[string]any)
		}
	}
	if sawFunctionEvent {
		t.Fatal("bridged function_call events must not reach downstream")
	}
	if createdCount != 1 {
		t.Fatalf("expected exactly one response.created, got %d", createdCount)
	}
	if terminal == nil {
		t.Fatal("expected terminal response.completed")
	}
	if anyString(terminal["id"]) != syntheticID {
		t.Fatalf("expected terminal id %q, got %q", syntheticID, anyString(terminal["id"]))
	}
	output, _ := terminal["output"].([]any)
	if len(output) != 3 {
		t.Fatalf("expected merged output message+image+message, got %d items", len(output))
	}
	imageItem, _ := output[1].(map[string]any)
	if anyString(imageItem["type"]) != "image_generation_call" || anyString(imageItem["result"]) != "aGk=" {
		t.Fatalf("expected image item with result, got %+v", imageItem)
	}
	usage, _ := terminal["usage"].(map[string]any)
	if payloadInt64(usage["input_tokens"]) != 30 || payloadInt64(usage["output_tokens"]) != 12 || payloadInt64(usage["total_tokens"]) != 42 {
		t.Fatalf("expected aggregated usage 30/12/42, got %+v", usage)
	}

	foundClosingIdx := false
	for _, event := range events {
		if event.Name != "response.output_item.done" {
			continue
		}
		item, _ := event.Payload["item"].(map[string]any)
		if anyString(item["id"]) == "msg_up2" {
			idx, _ := payloadInt(event.Payload["output_index"])
			if idx != 2 {
				t.Fatalf("expected closing message at downstream index 2, got %d", idx)
			}
			foundClosingIdx = true
		}
	}
	if !foundClosingIdx {
		t.Fatal("expected closing message output_item.done forwarded")
	}
}

func TestBridgeStreamWriterFailedImage(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := newBridgeStreamWriter(recorder, "gpt-5.5")
	defer writer.Close()
	writer.StartEnvelope()
	writer.beginRound()

	itemID, index := writer.InjectImageStart()
	writer.InjectImageResult(itemID, index, bridgeImageToolSpec{}, bridgeImageOutcome{ErrorMessage: "boom"})
	writer.CompleteGracefully()

	events := parseBridgeSSE(t, recorder.Body.String())
	var failedItem map[string]any
	for _, event := range events {
		if event.Name == "response.output_item.done" {
			failedItem, _ = event.Payload["item"].(map[string]any)
		}
	}
	if failedItem == nil || anyString(failedItem["status"]) != "failed" {
		t.Fatalf("expected failed image item, got %+v", failedItem)
	}
	if _, hasResult := failedItem["result"]; hasResult {
		t.Fatal("failed image item must not carry a result")
	}
}

func TestBridgeStreamWriterFailAll(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := newBridgeStreamWriter(recorder, "gpt-5.5")
	defer writer.Close()

	if writer.FailAll("upstream_failed", "nope") {
		t.Fatal("FailAll before start must report unhandled")
	}

	writer.StartEnvelope()
	if !writer.FailAll("upstream_failed", "all candidates failed") {
		t.Fatal("FailAll after start must handle the failure")
	}
	events := parseBridgeSSE(t, recorder.Body.String())
	last := events[len(events)-1]
	if last.Name != "response.failed" {
		t.Fatalf("expected response.failed terminal, got %s", last.Name)
	}
	response, _ := last.Payload["response"].(map[string]any)
	errorPayload, _ := response["error"].(map[string]any)
	if anyString(errorPayload["code"]) != "upstream_failed" {
		t.Fatalf("expected error code in terminal, got %+v", errorPayload)
	}
}

type bridgeLifecycleRecorder struct {
	*httptest.ResponseRecorder
	finished bool
}

func (r *bridgeLifecycleRecorder) FinishSSE() {
	r.finished = true
}

func TestBridgeStreamWriterErrorFinishesDownstream(t *testing.T) {
	recorder := &bridgeLifecycleRecorder{ResponseRecorder: httptest.NewRecorder()}
	writer := newBridgeStreamWriter(recorder, "gpt-5.5")
	writer.StartEnvelope()
	writer.beginRound()
	writeBridgeUpstreamEvent(t, writer, map[string]any{
		"type":  "response.failed",
		"error": map[string]any{"code": "upstream_failed", "message": "boom"},
	})
	if !recorder.finished {
		t.Fatal("expected terminal bridge error to finish downstream SSE")
	}
}

func TestBridgeStreamWriterChunkedWrites(t *testing.T) {
	recorder := httptest.NewRecorder()
	writer := newBridgeStreamWriter(recorder, "gpt-5.5")
	defer writer.Close()
	writer.StartEnvelope()
	writer.beginRound()

	full := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":4,\"output_index\":0,\"delta\":\"hi\"}\n\n"
	for _, chunk := range []string{full[:10], full[10:40], full[40:]} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatalf("chunked write failed: %v", err)
		}
	}

	events := parseBridgeSSE(t, recorder.Body.String())
	last := events[len(events)-1]
	if last.Name != "response.output_text.delta" || anyString(last.Payload["delta"]) != "hi" {
		t.Fatalf("expected reassembled delta event, got %+v", last)
	}
	if !writer.ContentFlushed() {
		t.Fatal("expected content flushed after text delta")
	}
}
