package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/klauspost/compress/zstd"
)

const encodedResponsesFixture = `{"model":"gpt-5.4","input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}]}`

func TestDecompressRequestBodyAcceptsSupportedEncodings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
		body     []byte
	}{
		{name: "uncompressed", body: []byte(encodedResponsesFixture)},
		{name: "identity", encoding: " identity ", body: []byte(encodedResponsesFixture)},
		{name: "zstd", encoding: " ZsTd ", body: compressZstdFixture(t, []byte(encodedResponsesFixture))},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var decoded map[string]any
			var decodeErr error
			handler := DecompressRequestBody(1024, nil)(LimitRequestBody(1024)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				decodeErr = DecodeJSONBody(r, &decoded)
				if strings.EqualFold(strings.TrimSpace(tt.encoding), "zstd") {
					if got := r.Header.Get("Content-Encoding"); got != "" {
						t.Errorf("Content-Encoding = %q, want empty", got)
					}
					if r.ContentLength != -1 {
						t.Errorf("ContentLength = %d, want -1", r.ContentLength)
					}
				}
			})))
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(tt.body))
			if tt.encoding != "" {
				req.Header.Set("content-encoding", tt.encoding)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if decodeErr != nil {
				t.Fatalf("DecodeJSONBody returned error: %v", decodeErr)
			}
			if decoded["model"] != "gpt-5.4" {
				t.Fatalf("model = %#v, want gpt-5.4", decoded["model"])
			}
		})
	}
}

func TestDecompressRequestBodyRejectsUnsupportedEncoding(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		encodings []string
	}{
		{name: "unsupported", encodings: []string{"gzip"}},
		{name: "multiple", encodings: []string{"zstd", "identity"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			called := false
			handler := middleware.RequestID(DecompressRequestBody(1024, nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			})))
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(encodedResponsesFixture))
			for _, encoding := range tc.encodings {
				req.Header.Add("Content-Encoding", encoding)
			}
			req.Header.Set(middleware.RequestIDHeader, "req-encoding")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if called {
				t.Fatal("handler should not run for unsupported encoding")
			}
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnsupportedMediaType)
			}
			var payload ErrorEnvelope
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if payload.Error.Code != "unsupported_content_encoding" || payload.Error.RequestID != "req-encoding" {
				t.Fatalf("error payload = %#v", payload.Error)
			}
		})
	}
}

func TestDecompressRequestBodyRejectsInvalidZstd(t *testing.T) {
	t.Parallel()

	validZstd := compressZstdFixture(t, []byte(encodedResponsesFixture))
	tests := []struct {
		name string
		body []byte
	}{
		{name: "corrupt zstd", body: append([]byte(nil), validZstd[:len(validZstd)-1]...)},
		{name: "uncompressed json", body: []byte(encodedResponsesFixture)},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := DecompressRequestBody(1024, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var decoded any
				if err := DecodeJSONBody(r, &decoded); !errors.Is(err, ErrRequestBodyEncoding) {
					t.Errorf("DecodeJSONBody error = %v, want ErrRequestBodyEncoding", err)
				}
				Error(w, r, http.StatusBadRequest, "invalid_content_encoding", "request body content encoding is invalid")
			}))
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(tt.body))
			req.Header.Set("Content-Encoding", "zstd")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestDecompressedRequestBodyCannotBypassSizeLimit(t *testing.T) {
	t.Parallel()

	const maxBytes = int64(256)
	body := []byte(`{"model":"gpt-5.4","input":"` + strings.Repeat("a", 4096) + `"}`)
	compressed := compressZstdFixture(t, body)
	if int64(len(compressed)) >= maxBytes {
		t.Fatalf("compressed fixture size = %d, want below %d", len(compressed), maxBytes)
	}

	handler := DecompressRequestBody(maxBytes, nil)(LimitRequestBody(maxBytes)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded any
		err := DecodeJSONBody(r, &decoded)
		if !errors.Is(err, ErrRequestBodyTooLarge) {
			t.Errorf("DecodeJSONBody error = %v, want ErrRequestBodyTooLarge", err)
		}
		Error(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "request body exceeds the maximum allowed size")
	})))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "zstd")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestCompressedRequestBodyCannotBypassInputSizeLimit(t *testing.T) {
	t.Parallel()

	const maxBytes = int64(32)
	fixture := make([]byte, 512)
	for i := range fixture {
		fixture[i] = byte(i * 31)
	}
	body := compressZstdFixture(t, fixture)
	if int64(len(body)) <= maxBytes {
		t.Fatalf("compressed fixture size = %d, want above %d", len(body), maxBytes)
	}
	var decodeErr error
	handler := DecompressRequestBody(maxBytes, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded any
		decodeErr = DecodeJSONBody(r, &decoded)
		if errors.Is(decodeErr, ErrRequestBodyTooLarge) {
			Error(w, r, http.StatusRequestEntityTooLarge, "request_body_too_large", "request body exceeds the maximum allowed size")
		}
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	req.ContentLength = -1
	req.Header.Set("Content-Encoding", "zstd")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !errors.Is(decodeErr, ErrRequestBodyTooLarge) {
		t.Fatalf("DecodeJSONBody error = %v, want ErrRequestBodyTooLarge", decodeErr)
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestDecompressRequestBodyClosesDecoderAndSource(t *testing.T) {
	t.Parallel()

	source := &trackedReadCloser{Reader: bytes.NewReader(compressZstdFixture(t, []byte(encodedResponsesFixture)))}
	var wrapped io.ReadCloser
	handler := DecompressRequestBody(1024, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		wrapped = r.Body
		var decoded any
		if err := DecodeJSONBody(r, &decoded); err != nil {
			t.Errorf("DecodeJSONBody returned error: %v", err)
		}
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", source)
	req.Header.Set("Content-Encoding", "zstd")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if source.closeCount != 1 {
		t.Fatalf("source close count = %d, want 1", source.closeCount)
	}
	if _, err := wrapped.Read(make([]byte, 1)); !errors.Is(err, ErrRequestBodyEncoding) {
		t.Fatalf("read after close error = %v, want ErrRequestBodyEncoding", err)
	}
	if err := wrapped.Close(); err != nil {
		t.Fatalf("second close returned error: %v", err)
	}
	if source.closeCount != 1 {
		t.Fatalf("source close count after second close = %d, want 1", source.closeCount)
	}
}

func TestDecompressionLogExcludesSensitiveRequestData(t *testing.T) {
	t.Parallel()

	const secret = "secret-bearer-token"
	const prompt = "private-prompt-value"
	body := []byte(`{"model":"gpt-5.4","input":"` + prompt + `"}`)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := DecompressRequestBody(1024, logger)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var decoded any
		if err := DecodeJSONBody(r, &decoded); err != nil {
			t.Errorf("DecodeJSONBody returned error: %v", err)
		}
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressZstdFixture(t, body)))
	req.Header.Set("Content-Encoding", "zstd")
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	logged := logs.String()
	if strings.Contains(logged, secret) || strings.Contains(logged, prompt) {
		t.Fatalf("log contains sensitive request data: %s", logged)
	}
	for _, field := range []string{"content_encoding", "compressed_bytes", "decompressed_bytes", "duration_ms"} {
		if !strings.Contains(logged, field) {
			t.Fatalf("log missing %q: %s", field, logged)
		}
	}
}

func compressZstdFixture(t *testing.T, body []byte) []byte {
	t.Helper()

	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("create zstd encoder: %v", err)
	}
	defer encoder.Close()
	return encoder.EncodeAll(body, nil)
}

type trackedReadCloser struct {
	io.Reader
	closeCount int
}

func (r *trackedReadCloser) Close() error {
	r.closeCount++
	return nil
}
