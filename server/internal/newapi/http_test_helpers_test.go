package newapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type newapiTestRoutes map[string]func(http.ResponseWriter, *http.Request)

func newapiTestServer(t *testing.T, routes newapiTestRoutes) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := routes[r.URL.Path]
		if !ok {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		route(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func newTestClient(transport roundTripFunc) Client {
	return NewClientWithHTTPClient(&http.Client{Transport: transport})
}

func serviceJSONResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func assertUserAuth(t *testing.T, r *http.Request) {
	t.Helper()

	if got := r.Header.Get("Authorization"); got != "access-token" {
		t.Fatalf("expected raw access token auth, got %q", got)
	}
	if got := r.Header.Get("New-Api-User"); got != "42" {
		t.Fatalf("expected New-Api-User 42, got %q", got)
	}
}

func assertRequestMethod(t *testing.T, r *http.Request, method string) {
	t.Helper()

	if r.Method != method {
		t.Fatalf("method = %s, want %s", r.Method, method)
	}
}

func assertGatewayAuth(t *testing.T, r *http.Request) {
	t.Helper()

	if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Fatalf("expected bearer API key auth, got %q", got)
	}
}

func assertAnyGatewayAuth(t *testing.T, r *http.Request) {
	t.Helper()

	if got := r.Header.Get("Authorization"); got != "Bearer sk-key-chat" && got != "Bearer sk-key-embedding" {
		t.Fatalf("expected bearer API key auth, got %q", got)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func writeNewAPIMessage(t *testing.T, w http.ResponseWriter, status int, message string) {
	t.Helper()

	w.WriteHeader(status)
	writeJSON(t, w, map[string]any{"message": message})
}

func writeNewAPIInvalidURL(t *testing.T, w http.ResponseWriter) {
	t.Helper()

	w.WriteHeader(http.StatusNotFound)
	writeJSON(t, w, map[string]any{
		"error": map[string]any{"message": "Invalid URL"},
	})
}
