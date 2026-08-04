package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func isOpenCodeGoSite(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), "opencode_go")
}

func classifyOpenCodeGoUsageLimit(candidate routeengine.Candidate, result gatewayAttemptResult, body []byte, now time.Time) (gatewayAttemptResult, bool) {
	if !isOpenCodeGoSite(candidate.Site.SiteType) || result.upstreamStatusCode != http.StatusTooManyRequests {
		return result, false
	}
	var payload struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		Metadata struct {
			Workspace string `json:"workspace"`
			LimitName string `json:"limitName"`
		} `json:"metadata"`
	}
	if json.Unmarshal(body, &payload) != nil || !strings.EqualFold(strings.TrimSpace(payload.Error.Type), "GoUsageLimitError") {
		return result, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	duration := openCodeGoUsageLimitCooldownDuration(result)
	resetAt := now.Add(duration)
	result.errorType = "opencode_go_usage_limit"
	result.errorMessage = nonEmptyString(strings.TrimSpace(payload.Error.Message), result.errorMessage)
	result.cooldownReason = store.CooldownReasonOpenCodeGoUsageLimitReached
	result.cooldownScope = "credential"
	result.cooldownDuration = duration
	result.cooldownMetadata = map[string]any{
		"limit_name":  strings.TrimSpace(payload.Metadata.LimitName),
		"workspace":   strings.TrimSpace(payload.Metadata.Workspace),
		"reset_at":    resetAt.UTC().Format(time.RFC3339),
		"observed_at": now.UTC().Format(time.RFC3339),
	}
	return result, true
}

func (h Handler) persistOpenCodeGoUsageLimit(ctx context.Context, candidate routeengine.Candidate, result gatewayAttemptResult) {
	if h.db == nil || !isOpenCodeGoSite(candidate.Site.SiteType) || result.errorType != "opencode_go_usage_limit" {
		return
	}
	usage := map[string]any{
		"source":                      "opencode_go",
		"official_realtime_available": false,
		"available":                   false,
		"retry_after_seconds":         result.retryAfterSeconds,
	}
	for key, value := range result.cooldownMetadata {
		usage[key] = value
	}
	encoded, err := json.Marshal(usage)
	if err != nil {
		return
	}
	if err := store.NewSiteAPIKeyStateRepository(h.db.DB()).UpdateUsage(ctx, result.credentialID, encoded); err != nil && h.logger != nil {
		h.logger.WarnContext(ctx, "failed to persist OpenCode Go usage limit", "scope", "gateway", "site_id", candidate.Site.ID, "credential_id", result.credentialID, "error", err)
	}
}
