package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestChatCompletionsGatewayUnavailableUsesSharedFailureEnvelope(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, nil, nil, "")
	req := gatewayRequestWithID(http.MethodPost, "/v1/chat/completions", `{"model":"gpt-4o-mini"}`, "req-123")
	rec := httptest.NewRecorder()

	handler.ChatCompletions(rec, req)

	assertGatewayErrorEnvelope(t, rec, http.StatusServiceUnavailable, "gateway_unavailable", "req-123")
}

func TestGatewayEndpointWrappersUseSharedUnavailableEnvelope(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, nil, nil, "")
	tests := []struct {
		name   string
		path   string
		body   string
		call   func(http.ResponseWriter, *http.Request)
		reqID  string
		status int
		code   string
	}{
		{
			name:  "responses",
			path:  gatewayEndpointResponses,
			body:  `{"model":"gpt-5","input":"hi"}`,
			call:  handler.Responses,
			reqID: "req-responses",
		},
		{
			name:  "messages",
			path:  gatewayEndpointMessages,
			body:  `{"model":"claude","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`,
			call:  handler.Messages,
			reqID: "req-messages",
		},
		{
			name:  "images",
			path:  gatewayEndpointImagesGenerations,
			body:  `{"model":"gpt-image-2","prompt":"draw"}`,
			call:  handler.ImagesGenerations,
			reqID: "req-images",
		},
		{
			name:  "embeddings",
			path:  gatewayEndpointEmbeddings,
			body:  `{"model":"text-embedding-3-large","input":"hi"}`,
			call:  handler.Embeddings,
			reqID: "req-embeddings",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := gatewayRequestWithID(http.MethodPost, tt.path, tt.body, tt.reqID)
			rec := httptest.NewRecorder()

			tt.call(rec, req)

			assertGatewayErrorEnvelope(t, rec, http.StatusServiceUnavailable, "gateway_unavailable", tt.reqID)
		})
	}
}

func TestWriteChatFailureUsesSharedEnvelope(t *testing.T) {
	t.Parallel()

	handler := NewHandler(nil, nil, nil, nil, "")
	req := gatewayRequestWithID(http.MethodPost, "/v1/chat/completions", "", "req-456")
	rec := httptest.NewRecorder()

	handler.writeChatFailure(rec, req, gatewayEndpointChatCompletions, "req-456", uuid.Nil, time.Now(), chatFailure{
		status:  http.StatusBadRequest,
		code:    "invalid_json",
		message: "request body must be valid JSON",
		stage:   "decode",
	})

	assertGatewayErrorEnvelope(t, rec, http.StatusBadRequest, "invalid_json", "req-456")
}

func TestResolveModelMappingNormalizesDownstreamModel(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	apiKey := store.APIKey{
		ModelMappings: store.JSON(`{
			" GPT_5 Codex ": "gpt-5-codex-upstream",
			"claude-sonnet": "claude-4-sonnet"
		}`),
	}

	got, ok := handler.resolveModelMapping(apiKey, "gpt-5-codex")
	if !ok {
		t.Fatal("expected model mapping to match normalized downstream model")
	}
	if got.Target != "gpt-5-codex-upstream" {
		t.Fatalf("mapped model = %q, want gpt-5-codex-upstream", got.Target)
	}
}

func TestResolveModelMappingIgnoresInvalidOrMissingMappings(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	for _, apiKey := range []store.APIKey{
		{},
		{ModelMappings: store.JSON(`{invalid-json`)},
		{ModelMappings: store.JSON(`{"claude-sonnet":"claude-4-sonnet"}`)},
	} {
		if got, ok := handler.resolveModelMapping(apiKey, "gpt-5"); ok || got.Target != "" {
			t.Fatalf("expected no mapping, got mapped=%#v ok=%v", got, ok)
		}
	}
}
