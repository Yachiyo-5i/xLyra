package oauth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"xlyra/server/internal/claudeversion"
)

func TestClaudeCodeOAuthRequestsFollowVersionRefresh(t *testing.T) {
	var next string
	t.Cleanup(claudeversion.WithFetcher(func(context.Context) (string, error) { return next, nil }))
	s := &Service{}
	for _, version := range []string{"2.998.0", "2.999.0"} {
		next = version
		if err := claudeversion.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		calls := 0
		client := &http.Client{Transport: oauthRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			want := "claude-cli/" + version + " (external, cli)"
			if got := req.Header.Get("User-Agent"); got != want {
				t.Fatalf("%s User-Agent = %q, want %q", req.URL, got, want)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"access_token":"token"}`)), Header: make(http.Header)}, nil
		})}
		if _, err := s.claudeCodeTokenRequest(context.Background(), map[string]any{}, client, "test token"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.fetchClaudeCodeProfile(context.Background(), "token", client); err != nil {
			t.Fatal(err)
		}
		if calls != 2 {
			t.Fatalf("requests = %d, want 2", calls)
		}
	}
}
