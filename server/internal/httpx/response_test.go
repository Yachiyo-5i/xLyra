package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestErrorPayloadIncludesRequestIDWhenPresent(t *testing.T) {
	t.Parallel()

	payload := ErrorPayload("invalid_json", "request body must be valid JSON", "req-123")
	if payload.Error.Code != "invalid_json" {
		t.Fatalf("expected code invalid_json, got %q", payload.Error.Code)
	}
	if payload.Error.RequestID != "req-123" {
		t.Fatalf("expected request id req-123, got %q", payload.Error.RequestID)
	}
}

func TestErrorPayloadOmitsRequestIDWhenEmpty(t *testing.T) {
	t.Parallel()

	payload := ErrorPayload("unauthorized", "valid api key is required", "")
	if payload.Error.RequestID != "" {
		t.Fatalf("expected empty request id, got %q", payload.Error.RequestID)
	}
}

func TestErrorWritesJSONEnvelopeWithRequestID(t *testing.T) {
	t.Parallel()

	handler := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Error(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
	}))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}
	var body ErrorEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "invalid_json" || body.Error.Message != "request body must be valid JSON" || body.Error.RequestID != "req-123" {
		t.Fatalf("unexpected error body: %#v", body)
	}
}
