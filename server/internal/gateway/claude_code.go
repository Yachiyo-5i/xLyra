package gateway

import (
	"net/http"
	"strings"
)

func isClaudeCodeSite(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), "claude_code")
}

func applyClaudeCodeOAuthGatewayHeaders(req *http.Request, accessToken string) {
	req.Header.Del("x-api-key")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", claudeCodeGatewayUserAgent)
	req.Header.Set(claudeCodeGatewayAppHeader, claudeCodeGatewayApp)
	req.Header.Set(claudeCodeGatewayBetaHeader, claudeCodeGatewayAnthropicBeta)
	req.Header.Set(claudeCodeGatewayVersionHeader, claudeCodeGatewayAPIVersion)
	req.Header.Set(claudeCodeGatewayProtection, "true")
}
