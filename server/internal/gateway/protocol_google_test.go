package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestGoogleProtocolBuildsGenerateContentPayloadFromChatRequest(t *testing.T) {
	t.Parallel()

	protocol := newGoogleProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions})
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Payload: map[string]any{
			"model": "alias",
			"messages": []any{
				map[string]any{"role": "system", "content": "Be concise."},
				map[string]any{"role": "user", "content": "Hello"},
			},
			"temperature": 0.2,
			"max_tokens":  128,
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-pro"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if _, ok := payload["model"]; ok {
		t.Fatalf("Google payload should not include model in body: %#v", payload["model"])
	}
	contents, ok := payload["contents"].([]any)
	if !ok || len(contents) != 1 {
		t.Fatalf("contents = %#v, want one user content", payload["contents"])
	}
	gen, ok := payload["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("generationConfig missing: %#v", payload)
	}
	if gen["maxOutputTokens"] != 128 {
		t.Fatalf("maxOutputTokens = %#v, want 128", gen["maxOutputTokens"])
	}
	if got := protocol.UpstreamPath("https://gemini.example.test/"); got != "https://gemini.example.test/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("UpstreamPath = %q", got)
	}
	if got := protocol.ProtocolName(); got != "google_generate_content" {
		t.Fatalf("ProtocolName = %q, want google_generate_content", got)
	}
}

func TestGoogleProtocolBuildsPayloadFromResponsesAndMessagesRequests(t *testing.T) {
	t.Parallel()

	responsesProtocol := newGoogleProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointResponses})
	responsesPayload, err := responsesProtocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model": "alias",
			"input": "hello responses",
		},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-flash"}})
	if err != nil {
		t.Fatalf("responses BuildUpstreamPayload returned error: %v", err)
	}
	if _, ok := responsesPayload["contents"].([]any); !ok {
		t.Fatalf("responses payload missing contents: %#v", responsesPayload)
	}
	if _, ok := responsesPayload["model"]; ok {
		t.Fatalf("responses payload should not include model: %#v", responsesPayload["model"])
	}

	messagesProtocol := newGoogleProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointMessages})
	messagesPayload, err := messagesProtocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointMessages,
		Payload: map[string]any{
			"model":      "alias",
			"max_tokens": 64,
			"messages": []any{
				map[string]any{"role": "user", "content": "hello messages"},
			},
		},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-flash"}})
	if err != nil {
		t.Fatalf("messages BuildUpstreamPayload returned error: %v", err)
	}
	if _, ok := messagesPayload["contents"].([]any); !ok {
		t.Fatalf("messages payload missing contents: %#v", messagesPayload)
	}
}

func TestGoogleProtocolStreamNameAndPathDefaults(t *testing.T) {
	t.Parallel()

	protocol := newGoogleProtocolAdapter(gatewayRequest{Stream: true}).(*googleProtocolAdapter)
	if got := protocol.ProtocolName(); got != "google_stream_generate_content" {
		t.Fatalf("ProtocolName = %q, want google_stream_generate_content", got)
	}
	if got := protocol.UpstreamPath(""); got != defaultGoogleGeminiBaseURL+"/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse" {
		t.Fatalf("default stream UpstreamPath = %q", got)
	}
	protocol.upstreamModel = "gemini-3-pro"
	if got := protocol.UpstreamPath(" https://gemini.example.test "); got != "https://gemini.example.test/v1beta/models/gemini-3-pro:streamGenerateContent?alt=sse" {
		t.Fatalf("custom stream UpstreamPath = %q", got)
	}
}

func TestGoogleProtocolAppliesHeaders(t *testing.T) {
	t.Parallel()

	protocol := newGoogleProtocolAdapter(gatewayRequest{})
	req, err := http.NewRequest(http.MethodPost, "https://gemini.example.test/v1beta/models/gemini:generateContent", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer old")

	protocol.(interface {
		ApplyUpstreamHeaders(*http.Request, string, string, bool)
	}).ApplyUpstreamHeaders(req, " gemini-key ", "", false)

	if got := req.Header.Get("x-goog-api-key"); got != "gemini-key" {
		t.Fatalf("x-goog-api-key = %q, want gemini-key", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization should be removed, got %q", got)
	}
}

func TestGoogleProtocolTransformsGeminiResponseToDownstreamProtocols(t *testing.T) {
	t.Parallel()

	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"Hello from Gemini"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}`)

	chatProtocol := &googleProtocolAdapter{}
	chatResponse, err := chatProtocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("chat TransformBufferedResponse returned error: %v", err)
	}
	var chatBody map[string]any
	if err := json.Unmarshal(chatResponse.Body, &chatBody); err != nil {
		t.Fatalf("decode chat body: %v", err)
	}
	choices := chatBody["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "Hello from Gemini" {
		t.Fatalf("chat content = %#v", message["content"])
	}
	if chatResponse.ContentType != "application/json" || chatResponse.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected chat response metadata: %#v", chatResponse)
	}

	responsesProtocol := &googleProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}
	responsesBody, err := responsesProtocol.TransformBufferedResponse(http.StatusOK, http.Header{}, body)
	if err != nil {
		t.Fatalf("responses TransformBufferedResponse returned error: %v", err)
	}
	var responses map[string]any
	if err := json.Unmarshal(responsesBody.Body, &responses); err != nil {
		t.Fatalf("decode responses body: %v", err)
	}
	if responses["object"] != "response" {
		t.Fatalf("responses object = %#v, want response", responses["object"])
	}

	messagesProtocol := &googleProtocolAdapter{downstreamProtocol: canonicalProtocolAnthropicMessages}
	messagesBody, err := messagesProtocol.TransformBufferedResponse(http.StatusOK, http.Header{}, body)
	if err != nil {
		t.Fatalf("messages TransformBufferedResponse returned error: %v", err)
	}
	var messages map[string]any
	if err := json.Unmarshal(messagesBody.Body, &messages); err != nil {
		t.Fatalf("decode messages body: %v", err)
	}
	if messages["type"] != "message" || messages["role"] != "assistant" {
		t.Fatalf("unexpected messages body: %#v", messages)
	}
}

func TestGoogleProtocolPassesThroughNonSuccessBufferedResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error":{"message":"quota exhausted"}}`)
	transformed, err := (&googleProtocolAdapter{}).TransformBufferedResponse(http.StatusTooManyRequests, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.StatusCode != http.StatusTooManyRequests || transformed.ContentType != "application/json" || string(transformed.Body) != string(body) {
		t.Fatalf("unexpected passthrough response: %#v", transformed)
	}
}

func TestGoogleProtocolProxyStreamConvertsGeminiStreamToChat(t *testing.T) {
	t.Parallel()

	resp := gatewayStreamTestResponse(
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hi\"}]}}]}\n\n" +
			"data: {\"candidates\":[{\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":2,\"totalTokenCount\":3}}\n\n")
	rec := httptest.NewRecorder()

	capture, started, err := (&googleProtocolAdapter{}).ProxyStream(context.Background(), rec, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), `"delta":{"content":"Hi"}`)
	if capture.usage.TotalTokens != 3 {
		t.Fatalf("usage total = %d, want 3", capture.usage.TotalTokens)
	}
}
