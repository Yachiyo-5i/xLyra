package gateway

import "testing"

func TestParseBufferedResponsesBodyParsesCompletedSSE(t *testing.T) {
	t.Parallel()

	body, parsed, err := parseBufferedResponsesBody([]byte(
		gatewaySSEEvent("", `{"type":"response.created","response":{"id":"resp_123","created_at":1710000000,"model":"gpt-5.4"}}`) +
			gatewaySSEEvent("", `{"type":"response.completed","response":{"id":"resp_123","created_at":1710000000,"model":"gpt-5.4","output":[{"id":"ig_123","type":"image_generation_call","status":"completed","result":"ZmFrZQ=="}],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}`),
	))
	if err != nil {
		t.Fatalf("parseBufferedResponsesBody returned error: %v", err)
	}
	if !parsed {
		t.Fatal("expected SSE body to be parsed")
	}
	assertGatewayBodyContainsAll(t, string(body), `"type":"image_generation_call"`)
}

func TestParseBufferedResponsesBodyParsesEventPrefixedSSE(t *testing.T) {
	t.Parallel()

	body, parsed, err := parseBufferedResponsesBody([]byte(
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_123","created_at":1710000000,"model":"gpt-5.4"}}`) +
			gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_123","created_at":1710000000,"model":"gpt-5.4","output":[{"id":"ig_123","type":"image_generation_call","status":"completed","result":"ZmFrZQ=="}],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}`),
	))
	if err != nil {
		t.Fatalf("parseBufferedResponsesBody returned error: %v", err)
	}
	if !parsed {
		t.Fatal("expected SSE body to be parsed")
	}
	assertGatewayBodyContainsAll(t, string(body), `"type":"image_generation_call"`)
}

func TestParseBufferedResponsesBodySynthesizesTextOutputFromDelta(t *testing.T) {
	t.Parallel()

	body, parsed, err := parseBufferedResponsesBody([]byte(
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_text","created_at":1710000002,"model":"gpt-5.4"}}`) +
			gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_text","output_index":0,"content_index":0,"delta":"hello "}`) +
			gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_text","output_index":0,"content_index":0,"delta":"world"}`) +
			gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_text","created_at":1710000002,"model":"gpt-5.4","output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`),
	))
	if err != nil {
		t.Fatalf("parseBufferedResponsesBody returned error: %v", err)
	}
	if !parsed {
		t.Fatal("expected SSE body to be parsed")
	}
	assertGatewayBodyContainsAll(t, string(body), `"text":"hello world"`)
}
