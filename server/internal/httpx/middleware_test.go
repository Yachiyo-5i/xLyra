package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestWithRequestIDHeaderCopiesContextRequestID(t *testing.T) {
	t.Parallel()

	handler := middleware.RequestID(WithRequestIDHeader(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.RequestIDHeader, "req-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("X-Request-ID"); got != "req-123" {
		t.Fatalf("expected request id response header, got %q", got)
	}
}

func TestWithRequestIDHeaderDoesNotSetEmptyRequestID(t *testing.T) {
	t.Parallel()

	handler := WithRequestIDHeader(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if got := rec.Header().Get("X-Request-ID"); got != "" {
		t.Fatalf("expected no request id response header, got %q", got)
	}
}

func TestLimitRequestBodyRejectsDeclaredOversizeWith413(t *testing.T) {
	t.Parallel()

	called := false
	handler := LimitRequestBody(16)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 100)))
	req.ContentLength = 100
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("handler should not run when Content-Length exceeds the limit")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}

func TestLimitRequestBodyTruncatedStreamSurfacesTooLargeError(t *testing.T) {
	t.Parallel()

	var decodeErr error
	handler := LimitRequestBody(16)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var dst any
		decodeErr = DecodeJSONBody(r, &dst)
	}))

	// Chunked body (ContentLength -1) larger than the limit must be truncated by
	// MaxBytesReader and surface as ErrRequestBodyTooLarge during decode.
	body := `{"data":"` + strings.Repeat("a", 100) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.ContentLength = -1
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !errors.Is(decodeErr, ErrRequestBodyTooLarge) {
		t.Fatalf("expected ErrRequestBodyTooLarge, got %v", decodeErr)
	}
}

func TestLimitRequestBodyAllowsBodyWithinLimit(t *testing.T) {
	t.Parallel()

	var decodeErr error
	var decoded map[string]any
	handler := LimitRequestBody(1024)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		decodeErr = DecodeJSONBody(r, &decoded)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"gpt"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if decodeErr != nil {
		t.Fatalf("expected clean decode within limit, got %v", decodeErr)
	}
	if decoded["model"] != "gpt" {
		t.Fatalf("expected decoded body, got %+v", decoded)
	}
}
