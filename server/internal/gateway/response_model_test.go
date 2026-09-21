package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	routeengine "xlyra/server/internal/router"
)

func TestUpstreamResponseModelEnvelope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body, want string }{
		{"chat", `{"model":" returned-model ","choices":[]}`, "returned-model"},
		{"responses", `{"response":{"model":"returned-model"}}`, "returned-model"},
		{"anthropic start", `{"type":"message_start","message":{"model":"returned-model"}}`, "returned-model"},
		{"gemini", `{"modelVersion":"gemini-version"}`, "gemini-version"},
		{"antigravity", `{"response":{"modelVersion":"gemini-version"}}`, "gemini-version"},
		{"missing", `{"usage":{"total_tokens":3}}`, ""},
		{"generated content", `{"choices":[{"message":{"content":"model: fake","model":"fake"}}]}`, ""},
		{"invalid", `{"model":`, ""},
		{"binary", "RIFF\x00\x01", ""},
		{"SSE retains model", "data: {\"model\":\"returned-model\"}\n\ndata: {\"usage\":{}}\n\ndata: [DONE]\n", "returned-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := upstreamResponseModel([]byte(tc.body)); got != tc.want {
				t.Fatalf("model = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResponseModelCapturedAcrossStreamProtocols(t *testing.T) {
	t.Parallel()
	chat := "data: {\"id\":\"c1\",\"model\":\"returned-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	responses := gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"r1","model":"returned-model"}}`) +
		gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"m1","delta":"hi"}`) +
		gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`)
	anthropic := gatewaySSEEvent("message_start", `{"type":"message_start","message":{"id":"m1","model":"returned-model","role":"assistant","content":[]}}`) +
		gatewaySSEEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`) +
		gatewaySSEEvent("message_stop", `{"type":"message_stop"}`)
	gemini := "data: {\"modelVersion\":\"returned-model\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n"
	for _, tc := range []struct {
		name     string
		protocol gatewayProtocolAdapter
		body     string
	}{
		{"chat passthrough", openAIChatProtocolAdapter{}, chat},
		{"responses passthrough", openAIResponsesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}, responses},
		{"anthropic passthrough", newAnthropicMessagesProtocolAdapter(canonicalProtocolAnthropicMessages), anthropic},
		{"provider anthropic passthrough", newProviderAnthropicMessagesProtocolAdapter("deepseek", alternateProtocolDefinition{}, canonicalProtocolAnthropicMessages), anthropic},
		{"chat to responses", openAIChatProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}, chat},
		{"responses to chat", openAIResponsesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIChat}, responses},
		{"anthropic to chat", newAnthropicMessagesProtocolAdapter(canonicalProtocolOpenAIChat), anthropic},
		{"gemini to chat", &googleProtocolAdapter{}, gemini},
		{"codex images", codexProtocolAdapter{downstreamImages: true}, responses},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := (Handler{logger: gatewayDiscardLogger()}).handleStreamResponse(
				context.Background(), httptest.NewRecorder(), "req-model", uuid.New(), uuid.New(),
				routeengine.Candidate{}, tc.protocol, gatewayStreamTestResponse(tc.body),
				gatewayAttemptResult{stream: true}, time.Now(),
			)
			if result.upstreamResponseModel != "returned-model" {
				t.Fatalf("response model = %q, want returned-model", result.upstreamResponseModel)
			}
		})
	}
}

func TestBufferedResponseModelSurvivesStreamBuffering(t *testing.T) {
	t.Parallel()
	body := gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"r1","model":"returned-model"}}`) +
		gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`)
	result := (Handler{logger: gatewayDiscardLogger()}).handleBufferedResponse(
		context.Background(), "req-model", uuid.New(), uuid.New(), routeengine.Candidate{},
		openAIResponsesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses},
		gatewayStreamTestResponse(body), gatewayAttemptResult{}, time.Now(),
	)
	if !result.success || result.upstreamResponseModel != "returned-model" {
		t.Fatalf("success = %v, model = %q, error = %s", result.success, result.upstreamResponseModel, result.errorMessage)
	}
}

func TestRequestModelMetadataPersistsOriginalAndObservedModels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, rules    string
		originalRoute  bool
		originalStatus int
	}{
		{"direct", `[]`, true, 0},
		{"hard mapping", `[{"pattern":"orig-model","target":"fallback-model","mode":"hard"}]`, true, 0},
		{"soft planning", `[{"pattern":"orig-model","target":"fallback-model","mode":"soft"}]`, false, 0},
		{"soft retry", `[{"pattern":"orig-model","target":"fallback-model","mode":"soft"}]`, true, 500},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", tc.name, stream), func(t *testing.T) {
				fixture := newSoftMappingFixture(t, tc.rules, tc.originalRoute, tc.originalStatus)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasPrefix(r.URL.Path, "/orig") && tc.originalStatus >= 400 {
						w.WriteHeader(tc.originalStatus)
						_, _ = w.Write([]byte(`{"error":{"message":"unavailable"}}`))
						return
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = w.Write([]byte("data: {\"model\":\"returned-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"model":"returned-model","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`))
				}))
				defer upstream.Close()
				for id, site := range fixture.harness.sites {
					path := "/fallback"
					if site.Slug == "orig-site" {
						path = "/orig"
					}
					site.BaseURL = upstream.URL + path
					fixture.harness.sites[id] = site
				}
				body := fmt.Sprintf(`{"model":"orig-model","messages":[{"role":"user","content":"hi"}],"stream":%v}`, stream)
				req := httptest.NewRequest(http.MethodPost, gatewayEndpointChatCompletions, strings.NewReader(body))
				req = req.WithContext(auth.WithAPIKey(req.Context(), fixture.apiKey))
				rec := httptest.NewRecorder()
				fixture.handler.ChatCompletions(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
				logs := fixture.harness.recordedLogs()
				if len(logs) == 0 {
					t.Fatal("no request logs persisted")
				}
				for _, log := range logs {
					meta := softMappingLogMetadata(t, log)
					if meta["requested_model"] != "orig-model" {
						t.Fatalf("lost original requested model: %#v", meta["requested_model"])
					}
					if log.Success && meta["upstream_response_model"] != "returned-model" {
						t.Fatalf("lost response model: %#v", meta["upstream_response_model"])
					}
					if !log.Success && meta["upstream_response_model"] != nil {
						t.Fatalf("unexpected model on failed attempt: %#v", meta["upstream_response_model"])
					}
				}
				if !logs[len(logs)-1].Success {
					t.Fatal("expected final successful log")
				}
			})
		}
	}
}
