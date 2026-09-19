package claudeversion

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type registryRoundTripFunc func(*http.Request) (*http.Response, error)

func (f registryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRefreshFromNPM(t *testing.T) {
	status := http.StatusOK
	body := `{"dist-tags":{"latest":"2.999.0","stable":"2.998.0","next":"3.0.0-beta.1"}}`
	var transportErr error
	previous := http.DefaultTransport
	http.DefaultTransport = registryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://registry.npmjs.org/@anthropic-ai%2Fclaude-code" {
			t.Fatalf("unexpected registry URL: %s", req.URL)
		}
		if req.Method != http.MethodGet || req.Header.Get("Accept") != "application/json" {
			t.Fatalf("unexpected registry request: %s %v", req.Method, req.Header)
		}
		if transportErr != nil {
			return nil, transportErr
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	t.Cleanup(WithFetcher(fetchFromNPM))
	if got := Version(); got != DefaultVersion {
		t.Fatalf("initial version = %q, want %q", got, DefaultVersion)
	}
	if err := Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := Version(); got != "2.999.0" {
		t.Fatalf("version = %q, want latest tag 2.999.0", got)
	}
	for _, tc := range []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{name: "network", status: http.StatusOK, err: errors.New("registry unavailable")},
		{name: "http", status: http.StatusServiceUnavailable, body: body},
		{name: "json", status: http.StatusOK, body: "{"},
		{name: "missing_latest", status: http.StatusOK, body: `{"dist-tags":{"stable":"2.998.0"}}`},
		{name: "invalid", status: http.StatusOK, body: `{"dist-tags":{"latest":"invalid"}}`},
		{name: "prerelease", status: http.StatusOK, body: `{"dist-tags":{"latest":"3.0.0-beta.1"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body, transportErr = tc.status, tc.body, tc.err
			if err := Refresh(context.Background()); err == nil {
				t.Fatal("expected refresh error")
			}
			if got := Version(); got != "2.999.0" {
				t.Fatalf("version after failed refresh = %q, want 2.999.0", got)
			}
		})
	}
}
