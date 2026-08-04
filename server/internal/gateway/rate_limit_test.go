package gateway

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/ratelimit"
)

func TestWriteRateLimitFailureReturnsRetryAfterAndGatewayEnvelope(t *testing.T) {
	t.Parallel()

	handler := Handler{logger: slog.Default()}
	req := httptest.NewRequest(http.MethodPost, gatewayEndpointChatCompletions, nil)
	rec := httptest.NewRecorder()
	requestID := "req-rate-limited"

	handler.writeRateLimitFailure(
		rec,
		req,
		gatewayEndpointChatCompletions,
		requestID,
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		time.Now(),
		gatewayRequest{RequestedModel: "gpt-5", Stream: true},
		ratelimit.LimitError{
			Scope:             "api_key",
			ScopeKey:          "key-1",
			LimitType:         "tpm",
			RetryAfterSeconds: 42,
		},
	)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("Retry-After"); got != "42" {
		t.Fatalf("Retry-After = %q, want 42", got)
	}

	var body struct {
		Error struct {
			Code              string `json:"code"`
			Message           string `json:"message"`
			RequestID         string `json:"request_id"`
			Scope             string `json:"scope"`
			LimitType         string `json:"limit_type"`
			RetryAfterSeconds int64  `json:"retry_after_seconds"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "rate_limit_exceeded" || body.Error.Message != "rate limit exceeded" {
		t.Fatalf("unexpected error envelope: %#v", body.Error)
	}
	if body.Error.RequestID != requestID || body.Error.Scope != "api_key" || body.Error.LimitType != "tpm" || body.Error.RetryAfterSeconds != 42 {
		t.Fatalf("unexpected rate limit metadata: %#v", body.Error)
	}
}

func TestWriteRateLimitFailureDefaultsRetryAfterToOneSecond(t *testing.T) {
	t.Parallel()

	handler := Handler{logger: slog.Default()}
	req := httptest.NewRequest(http.MethodGet, gatewayEndpointModels, nil)
	rec := httptest.NewRecorder()

	handler.writeRateLimitFailure(
		rec,
		req,
		gatewayEndpointModels,
		"req-models-limited",
		uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		time.Now(),
		gatewayRequest{RequestedModel: "models"},
		ratelimit.LimitError{Scope: "global", LimitType: "rpm"},
	)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
}

func TestEndpointUsesTPMForTokenBilledGenerationEndpoints(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{gatewayEndpointChatCompletions, gatewayEndpointResponses, gatewayEndpointMessages, gatewayEndpointAudioSpeech} {
		if !endpointUsesTPM(endpoint) {
			t.Fatalf("expected %s to reserve TPM", endpoint)
		}
	}
	for _, endpoint := range []string{gatewayEndpointModels, gatewayEndpointImagesGenerations, gatewayEndpointImagesEdits, gatewayEndpointEmbeddings} {
		if endpointUsesTPM(endpoint) {
			t.Fatalf("expected %s not to reserve TPM", endpoint)
		}
	}
}
