package gateway

import (
	"net/http"
	"testing"
)

func TestApplyClaudeCodeOAuthGatewayHeadersUsesBearerAndRemovesAPIKey(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("x-api-key", "old-key")
	applyClaudeCodeOAuthGatewayHeaders(req, " token-main ")

	if got := req.Header.Get("x-api-key"); got != "" {
		t.Fatalf("x-api-key = %q, want empty", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token-main" {
		t.Fatalf("Authorization = %q, want Bearer token-main", got)
	}
	if got := req.Header.Get("User-Agent"); got != claudeCodeGatewayUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, claudeCodeGatewayUserAgent)
	}
	if got := req.Header.Get("anthropic-beta"); got != claudeCodeGatewayAnthropicBeta {
		t.Fatalf("anthropic-beta = %q, want %q", got, claudeCodeGatewayAnthropicBeta)
	}
	if got := req.Header.Get("X-App"); got != claudeCodeGatewayApp {
		t.Fatalf("X-App = %q, want %q", got, claudeCodeGatewayApp)
	}
	if got := req.Header.Get(claudeCodeGatewayProtection); got != "true" {
		t.Fatalf("x-anthropic-additional-protection = %q, want true", got)
	}
}
