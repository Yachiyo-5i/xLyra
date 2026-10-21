package gateway

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/protocolspec"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

const (
	openCodeGoSessionHeader   = "x-opencode-session-id"
	openCodeGoAffinityHeader  = "x-session-affinity"
	openCodeGoUserAgent       = "opencode/latest/2.0.29/cli"
	openCodeGoIdentifierSize  = 26
	openCodeGoTimestampSize   = 12
	openCodeGoRandomSize      = openCodeGoIdentifierSize - openCodeGoTimestampSize
	openCodeGoIdentifierChars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

func isOpenCodeGoSite(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), "opencode_go")
}

func isOpenCodeZenSite(candidate routeengine.Candidate) bool {
	return isOpenCodeGoSite(candidate.Site.SiteType) || protocolspec.IsOpenCodeZenBaseURL(candidate.Site.BaseURL)
}

func applyOpenCodeGoDefaultTools(payload map[string]any, candidate routeengine.Candidate, protocol gatewayProtocolAdapter) {
	if payload == nil || protocol == nil || !isOpenCodeZenSite(candidate) {
		return
	}
	if _, exists := payload["tools"]; exists {
		return
	}

	parameters := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "文件路径",
			},
		},
		"required": []any{"path"},
	}
	var tool any
	path := endpointPath(protocol.UpstreamPath(candidate.Site.BaseURL))
	switch {
	case strings.HasSuffix(path, gatewayEndpointChatCompletions):
		tool = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "read",
				"description": "读取文件",
				"parameters":  parameters,
			},
		}
	case strings.HasSuffix(path, gatewayEndpointResponses):
		tool = map[string]any{
			"type":        "function",
			"name":        "read",
			"description": "读取文件",
			"parameters":  parameters,
		}
	case strings.HasSuffix(path, gatewayEndpointMessages):
		tool = map[string]any{
			"name":         "read",
			"description":  "读取文件",
			"input_schema": parameters,
		}
	default:
		return
	}
	payload["tools"] = []any{tool}
}

func buildGatewayUpstreamPayload(protocol gatewayProtocolAdapter, request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		return nil, err
	}
	// Conversion can omit an explicitly empty tools array. Only add a fallback
	// when the downstream request omitted tools entirely.
	if _, exists := request.Payload["tools"]; !exists {
		applyOpenCodeGoDefaultTools(payload, candidate, protocol)
	}
	return payload, nil
}

// applyOpenCodeGoGatewayHeaders replaces all application headers with the
// small set expected by the OpenCode Go endpoint.
func applyOpenCodeGoGatewayHeaders(req *http.Request, request gatewayRequest, apiKey string) {
	if req == nil {
		return
	}
	req.Header = make(http.Header)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", openCodeGoUserAgent)

	sessionID := openCodeGoValidHeaderID(request.DownstreamHeaders.Get(openCodeGoSessionHeader))
	if sessionID == "" {
		sessionID = openCodeGoNewSessionID()
	}
	affinityID := openCodeGoValidHeaderID(request.DownstreamHeaders.Get(openCodeGoAffinityHeader))
	if affinityID == "" {
		affinityID = sessionID
	}
	req.Header.Set(openCodeGoSessionHeader, sessionID)
	req.Header.Set(openCodeGoAffinityHeader, affinityID)
}

func openCodeGoValidHeaderID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) != len("ses_")+openCodeGoIdentifierSize || !strings.HasPrefix(value, "ses_") {
		return ""
	}
	for index, char := range value[len("ses_"):] {
		if index < openCodeGoTimestampSize {
			if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
				return ""
			}
			continue
		}
		if !strings.ContainsRune(openCodeGoIdentifierChars, char) {
			return ""
		}
	}
	return value
}

func openCodeGoNewSessionID() string {
	timestamp := fmt.Sprintf("%012x", uint64(time.Now().UnixMilli()))
	random := make([]byte, openCodeGoRandomSize)
	if _, err := rand.Read(random); err != nil {
		for index := range random {
			random[index] = byte(time.Now().UnixNano() >> (index % 8 * 8))
		}
	}
	for index, value := range random {
		random[index] = openCodeGoIdentifierChars[int(value)%len(openCodeGoIdentifierChars)]
	}
	return "ses_" + timestamp + string(random)
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
