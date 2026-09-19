package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"xlyra/server/internal/claudeversion"
)

func TestClaudeCodeRequestHeadersFollowVersionRefresh(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*http.Request)
	}{
		{name: "gateway", apply: func(req *http.Request) { applyClaudeCodeOAuthGatewayHeaders(req, "token") }},
		{name: "client_impersonation", apply: func(req *http.Request) { applyClaudeCodeClientImpersonationHeaders(req, "session") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var next string
			var fetchErr error
			t.Cleanup(claudeversion.WithFetcher(func(context.Context) (string, error) { return next, fetchErr }))

			assertVersion := func(version string) {
				t.Helper()
				req := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/responses")
				tc.apply(req)
				want := "claude-cli/" + version + " (external, cli)"
				if got := req.Header.Get("User-Agent"); got != want {
					t.Fatalf("User-Agent = %q, want %q", got, want)
				}
			}

			assertVersion(claudeversion.DefaultVersion)
			for _, version := range []string{"2.998.0", "2.999.0"} {
				next = version
				if err := claudeversion.Refresh(context.Background()); err != nil {
					t.Fatalf("Refresh returned error: %v", err)
				}
				assertVersion(version)
			}

			fetchErr = errors.New("registry unavailable")
			if err := claudeversion.Refresh(context.Background()); err == nil {
				t.Fatal("Refresh succeeded, want fetch error")
			}
			assertVersion(next)
		})
	}
}
