package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	routeengine "xlyra/server/internal/router"
)

func (h Handler) markOAuthConnectionUnavailableOnAuthFailure(ctx context.Context, candidate routeengine.Candidate, result gatewayAttemptResult) {
	if h.oauth == nil || h.db == nil {
		return
	}
	if !oauthModelRequestAuthFailure(candidate.Site.SiteType, result) {
		return
	}
	if err := h.oauth.MarkConnectionUnavailableBySiteID(ctx, candidate.Site.ID, oauthModelRequestAuthFailureMessage(candidate, result)); err != nil && h.logger != nil {
		h.logger.WarnContext(ctx, "failed to mark oauth connection unavailable after upstream auth failure", "site_id", candidate.Site.ID, "site_type", candidate.Site.SiteType, "error", err)
	}
}

func oauthModelRequestAuthFailure(siteType string, result gatewayAttemptResult) bool {
	if !isCodexSite(siteType) && !isAntigravitySite(siteType) && !isClaudeCodeSite(siteType) {
		return false
	}
	return result.errorType == "upstream_credential_invalid" ||
		result.upstreamStatusCode == http.StatusUnauthorized || result.statusCode == http.StatusUnauthorized
}

func oauthModelRequestAuthFailureMessage(candidate routeengine.Candidate, result gatewayAttemptResult) string {
	statusCode := result.upstreamStatusCode
	if statusCode == 0 {
		statusCode = result.statusCode
	}
	parts := []string{fmt.Sprintf("%s model request returned HTTP %d", strings.TrimSpace(candidate.Site.SiteType), statusCode)}
	if path := strings.TrimSpace(result.upstreamPath); path != "" {
		parts = append(parts, "path "+path)
	}
	if model := strings.TrimSpace(candidate.Model.UpstreamName); model != "" {
		parts = append(parts, "model "+model)
	}
	if message := strings.TrimSpace(result.errorMessage); message != "" {
		parts = append(parts, message)
	}
	return strings.Join(parts, ": ")
}
