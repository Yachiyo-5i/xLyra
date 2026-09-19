package adapter

import (
	"context"
	"net/http"
	"testing"

	"xlyra/server/internal/claudeversion"
)

func TestClaudeCodeOAuthHeadersFollowVersionRefresh(t *testing.T) {
	var next string
	t.Cleanup(claudeversion.WithFetcher(func(context.Context) (string, error) { return next, nil }))
	for _, version := range []string{"2.998.0", "2.999.0"} {
		next = version
		if err := claudeversion.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodGet, "https://example.test/api/oauth/usage", nil)
		if err != nil {
			t.Fatal(err)
		}
		applyClaudeCodeOAuthHeaders(req, "token")
		want := "claude-cli/" + version + " (external, cli)"
		if got := req.Header.Get("User-Agent"); got != want {
			t.Fatalf("User-Agent = %q, want %q", got, want)
		}
	}
}
