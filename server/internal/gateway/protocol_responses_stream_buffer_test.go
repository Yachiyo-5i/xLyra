package gateway

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"
)

func TestReadBufferedResponsesStreamBodyRejectsNilReader(t *testing.T) {
	t.Parallel()

	_, err := readBufferedResponsesStreamBody(nil)
	if err == nil {
		t.Fatal("expected nil reader to return an error")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadBufferedResponsesStreamBodySkipsPartialImagesAndReturnsCompleted(t *testing.T) {
	t.Parallel()

	body, err := readBufferedResponsesStreamBody(strings.NewReader(
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_123","created_at":1710000000,"model":"gpt-5.4"}}`) +
			gatewaySSEEvent("keepalive", `{"type":"keepalive","sequence_number":1}`) +
			gatewaySSEEvent("response.image_generation_call.partial_image", `{"type":"response.image_generation_call.partial_image","partial_image_b64":"AAAA"}`) +
			gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_123","created_at":1710000000,"model":"gpt-5.4","output":[{"id":"ig_123","type":"image_generation_call","status":"completed","result":"ZmFrZQ=="}],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}`),
	))
	if err != nil {
		t.Fatalf("readBufferedResponsesStreamBody returned error: %v", err)
	}
	assertGatewayBodyContainsAll(t, string(body), `"type":"image_generation_call"`, `"result":"ZmFrZQ=="`)
}

func TestReadBufferedResponsesStreamBodySynthesizesImageOutputFromPartialImage(t *testing.T) {
	t.Parallel()

	body, err := readBufferedResponsesStreamBody(strings.NewReader(
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_456","created_at":1710000001,"model":"gpt-5.4"}}`) +
			gatewaySSEEvent("response.image_generation_call.partial_image", `{"type":"response.image_generation_call.partial_image","item_id":"ig_456","partial_image_b64":"cGFydGlhbA==","revised_prompt":"safe prompt","output_format":"png","quality":"high","size":"1024x1024","background":"opaque"}`) +
			gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_456","created_at":1710000001,"model":"gpt-5.4","output":[],"usage":{"input_tokens":21,"output_tokens":9,"total_tokens":30}}}`),
	))
	if err != nil {
		t.Fatalf("readBufferedResponsesStreamBody returned error: %v", err)
	}
	assertGatewayBodyContainsAll(t, string(body),
		`"id":"ig_456"`,
		`"type":"image_generation_call"`,
		`"result":"cGFydGlhbA=="`,
		`"revised_prompt":"safe prompt"`,
	)
}

func TestReadBufferedResponsesStreamBodySynthesizesTextOutputFromDelta(t *testing.T) {
	t.Parallel()

	body, err := readBufferedResponsesStreamBody(strings.NewReader(
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_text","created_at":1710000002,"model":"gpt-5.4"}}`) +
			gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_text","output_index":0,"content_index":0,"delta":"hello "}`) +
			gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_text","output_index":0,"content_index":0,"delta":"world"}`) +
			gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_text","created_at":1710000002,"model":"gpt-5.4","output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`),
	))
	if err != nil {
		t.Fatalf("readBufferedResponsesStreamBody returned error: %v", err)
	}
	assertGatewayBodyContainsAll(t, string(body), `"type":"message"`, `"text":"hello world"`)
}

func TestReadBufferedResponsesStreamBodyFallsBackToLastResponseAtEOFAndMergesText(t *testing.T) {
	t.Parallel()

	body, err := readBufferedResponsesStreamBody(strings.NewReader(
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_eof_text","created_at":1710000003,"model":"gpt-5.4"}}`) +
			gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","output_index":2,"content_index":0,"delta":"partial "}`) +
			gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","output_index":2,"content_index":0,"delta":"answer"}`),
	))
	if err != nil {
		t.Fatalf("readBufferedResponsesStreamBody returned error: %v", err)
	}

	var response responsesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(body))
	}
	if response.ID != "resp_eof_text" {
		t.Fatalf("response ID = %q, want resp_eof_text", response.ID)
	}
	if len(response.Output) != 1 {
		t.Fatalf("output len = %d, want synthesized text output: %#v", len(response.Output), response.Output)
	}
	item := response.Output[0]
	if item.ID != "output-2" || item.Type != "message" || item.Role != "assistant" || item.Status != "completed" {
		t.Fatalf("synthesized item = %#v, want output-2 assistant message", item)
	}
	if len(item.Content) != 1 || item.Content[0].Text != "partial answer" {
		t.Fatalf("synthesized content = %#v, want partial answer", item.Content)
	}
}

func TestReadBufferedResponsesStreamBodyFallsBackToPartialImageAtEOF(t *testing.T) {
	t.Parallel()

	body, err := readBufferedResponsesStreamBody(strings.NewReader(
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_eof_image","created_at":1710000004,"model":"gpt-5.4"}}`) +
			gatewaySSEEvent("response.image_generation_call.partial_image", `{"type":"response.image_generation_call.partial_image","partial_image_b64":"aW1hZ2U=","revised_prompt":"fallback prompt","quality":"medium"}`),
	))
	if err != nil {
		t.Fatalf("readBufferedResponsesStreamBody returned error: %v", err)
	}

	var response responsesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, string(body))
	}
	if len(response.Output) != 1 {
		t.Fatalf("output len = %d, want synthesized image output: %#v", len(response.Output), response.Output)
	}
	item := response.Output[0]
	if item.ID != "partial-image-1" || item.Type != "image_generation_call" || item.Status != "completed" {
		t.Fatalf("synthesized image item = %#v, want default partial image item", item)
	}
	if item.Result != "aW1hZ2U=" || item.RevisedPrompt != "fallback prompt" || item.Quality != "medium" {
		t.Fatalf("synthesized image metadata = %#v, want partial image fields", item)
	}
}

func TestReadBufferedResponsesStreamBodyReturnsUpstreamErrorEvent(t *testing.T) {
	t.Parallel()

	_, err := readBufferedResponsesStreamBody(strings.NewReader(
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_123","created_at":1710000000,"model":"gpt-5.4"}}`) +
			gatewaySSEEvent("error", `{"type":"error","error":{"type":"image_generation_user_error","code":"moderation_blocked","message":"blocked by safety"}}`),
	))
	if err == nil {
		t.Fatal("expected upstream error event to be returned")
	}
	if !strings.Contains(err.Error(), "moderation_blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestShouldBufferAsResponsesStreamByContentType(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader(`{"ok":true}`))
	if !shouldBufferAsResponsesStream("text/event-stream; charset=utf-8", reader) {
		t.Fatal("expected event-stream content type to force buffering")
	}
}

func TestShouldBufferAsResponsesStreamByBodyPrefix(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("event: response.created\ndata: {}\n\n"))
	if !shouldBufferAsResponsesStream("", reader) {
		t.Fatal("expected SSE body prefix to force buffering")
	}
}
