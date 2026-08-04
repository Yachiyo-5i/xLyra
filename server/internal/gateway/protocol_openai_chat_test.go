package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestOpenAIChatProtocolBuildsPayloadPathAndStreamOptions(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.1"},
	}
	protocol := newOpenAIChatProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, candidate)
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Payload: map[string]any{
			"model":  "alias-chat",
			"stream": true,
			"messages": []any{
				map[string]any{"role": "user", "content": "hello"},
			},
		},
	}, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "gpt-5.1" {
		t.Fatalf("model = %#v, want upstream model", payload["model"])
	}
	streamOptions, ok := payload["stream_options"].(map[string]any)
	if !ok || streamOptions["include_usage"] != true {
		t.Fatalf("stream_options = %#v, want include_usage true", payload["stream_options"])
	}
	if got := protocol.UpstreamPath("https://api.example.test"); got != "https://api.example.test/v1/chat/completions" {
		t.Fatalf("UpstreamPath = %q, want chat completions path", got)
	}
	if got := protocol.ProtocolName(); got != "openai_chat_completions" {
		t.Fatalf("ProtocolName = %q, want openai_chat_completions", got)
	}
}

func TestOpenAIChatProtocolConvertsResponsesRequestAndResponse(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.1"}}
	protocol := newOpenAIChatProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointResponses}, candidate)

	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model":             "alias-responses",
			"input":             "hello from responses",
			"max_output_tokens": 64.0,
		},
	}, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "gpt-5.1" || payload["max_tokens"] != 64 {
		t.Fatalf("unexpected converted payload: %#v", payload)
	}
	if _, ok := payload["messages"].([]any); !ok {
		t.Fatalf("converted payload missing messages: %#v", payload)
	}
	if got := protocol.ProtocolName(); got != "openai_chat_completions_to_responses" {
		t.Fatalf("ProtocolName = %q, want responses conversion", got)
	}

	body := []byte(`{"id":"chatcmpl_1","object":"chat.completion","model":"gpt-5.1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatalf("decode transformed response: %v", err)
	}
	if out["object"] != "response" {
		t.Fatalf("object = %#v, want response; body=%s", out["object"], string(transformed.Body))
	}
	if transformed.ContentType != "application/json" || transformed.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected transformed metadata: %#v", transformed)
	}
}

func TestOpenAIChatProtocolTransformsNativeAndStreams(t *testing.T) {
	t.Parallel()

	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":4,"completion_tokens":6,"total_tokens":10}}`)
	transformed, err := (openAIChatProtocolAdapter{}).TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.ContentType != "application/json" || transformed.Usage.PromptTokens != 4 || transformed.Usage.TotalTokens != 10 || string(transformed.Body) != string(body) {
		t.Fatalf("unexpected native transform: %#v", transformed)
	}

	resp := gatewayStreamTestResponse(
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n" +
			"data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n" +
			"data: [DONE]\n\n")
	rec := httptest.NewRecorder()
	capture, started, err := (openAIChatProtocolAdapter{}).ProxyStream(t.Context(), rec, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted || capture.usage.TotalTokens != 3 {
		t.Fatalf("started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), `"delta":{"content":"Hi"}`)
}
