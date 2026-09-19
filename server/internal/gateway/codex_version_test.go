package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"xlyra/server/internal/codexversion"
)

func TestCodexRequestHeadersFollowVersionRefresh(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*http.Request)
	}{
		{name: "gateway", apply: func(req *http.Request) { applyCodexGatewayHeaders(req, "account", false) }},
		{name: "gateway_stream", apply: func(req *http.Request) { applyCodexGatewayHeaders(req, "account", true) }},
		{name: "client_impersonation", apply: applyCodexClientImpersonationHeaders},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var next string
			var fetchErr error
			t.Cleanup(codexversion.WithFetcher(func(context.Context) (string, error) { return next, fetchErr }))

			assertVersion := func(version string) {
				t.Helper()
				req := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/responses")
				tc.apply(req)
				want := "codex_cli_rs/" + version + " (Mac OS 26.0; arm64) vscode/1.99.3"
				if got := req.Header.Get("User-Agent"); got != want {
					t.Fatalf("User-Agent = %q, want %q", got, want)
				}
			}

			assertVersion(codexversion.DefaultVersion)
			for _, version := range []string{"0.998.0", "0.999.0"} {
				next = version
				if err := codexversion.Refresh(context.Background()); err != nil {
					t.Fatalf("Refresh returned error: %v", err)
				}
				assertVersion(version)
			}

			fetchErr = errors.New("registry unavailable")
			if err := codexversion.Refresh(context.Background()); err == nil {
				t.Fatal("Refresh succeeded, want fetch error")
			}
			assertVersion(next)
		})
	}
}
