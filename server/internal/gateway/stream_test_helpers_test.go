package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func gatewayStreamTestResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func gatewayStreamTestResponseWithoutBody() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func gatewaySSEEvent(event string, data string) string {
	if event == "" {
		return "data: " + data + "\n\n"
	}
	return "event: " + event + "\n" + "data: " + data + "\n\n"
}

func proxyCanonicalStreamTest(t *testing.T, body string, from canonicalProtocol, to canonicalProtocol, options canonicalStreamOptions) (*httptest.ResponseRecorder, streamCaptureState, bool, error) {
	t.Helper()

	rec := httptest.NewRecorder()
	capture, started, err := proxyCanonicalStream(context.Background(), rec, gatewayStreamTestResponse(body), time.Now(), from, to, options)
	return rec, capture, started, err
}

func proxyResponsesStreamPassthroughTest(t *testing.T, body string) (*httptest.ResponseRecorder, streamCaptureState, bool, error) {
	t.Helper()

	rec := httptest.NewRecorder()
	capture, started, err := proxyResponsesStreamPassthrough(context.Background(), rec, gatewayStreamTestResponse(body), time.Now())
	return rec, capture, started, err
}

func assertGatewayBodyContainsAll(t *testing.T, body string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in stream body, got %q", want, body)
		}
	}
}
