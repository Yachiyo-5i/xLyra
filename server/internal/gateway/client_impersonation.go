package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
)

const (
	claudeCodeGatewayUserAgent      = adapter.ClaudeCodeUserAgent
	claudeCodeGatewayApp            = "cli"
	claudeCodeGatewayAnthropicBeta  = adapter.ClaudeCodeOAuthBeta
	claudeCodeGatewaySystemPrompt   = "You are Claude Code, Anthropic's official CLI for Claude."
	claudeCodeBillingHeaderPrefix   = "x-anthropic-billing-header"
	claudeCodeCLIEntrypointMarker   = "cc_entrypoint=cli"
	claudeCodeGatewayAPIVersion     = adapter.ClaudeCodeAPIVersion
	claudeCodeGatewaySessionHeader  = "X-Claude-Code-Session-Id"
	claudeCodeGatewayAppHeader      = "X-App"
	claudeCodeGatewayBetaHeader     = "anthropic-beta"
	claudeCodeGatewayVersionHeader  = "anthropic-version"
	claudeCodeGatewayProtection     = "x-anthropic-additional-protection"
	codexGatewaySessionHeader       = "Session-Id"
	codexGatewayThreadHeader        = "Thread-Id"
	codexGatewayClientRequestHeader = "X-Client-Request-Id"
	codexGatewayBetaFeaturesHeader  = "X-Codex-Beta-Features"
	codexGatewayTurnMetadataHeader  = "X-Codex-Turn-Metadata"
	codexGatewayWindowHeader        = "X-Codex-Window-Id"
	codexGatewayBetaFeatures        = "terminal_resize_reflow"
)

var claudeCodeLegacyMetadataUserIDPattern = regexp.MustCompile(`^user_([a-fA-F0-9]{64})_account_([a-fA-F0-9-]*)_session_([a-fA-F0-9-]{36})$`)

type gatewayClientImpersonationKind string

const (
	gatewayClientImpersonationNone       gatewayClientImpersonationKind = ""
	gatewayClientImpersonationCodex      gatewayClientImpersonationKind = "codex"
	gatewayClientImpersonationClaudeCode gatewayClientImpersonationKind = "claude_code"
)

func applyGatewayClientImpersonationHeaders(req *http.Request, cfg *sitepkg.GatewayConfig, protocol gatewayProtocolAdapter, request gatewayRequest, candidate routeengine.Candidate, claudeCodeSessionID string) {
	if req == nil {
		return
	}
	switch gatewayClientImpersonationKindForRequest(protocol, request, candidate) {
	case gatewayClientImpersonationCodex:
		if cfg == nil || !gatewayConfigBool(cfg.ImpersonateCodexClient) {
			return
		}
		applyCodexClientImpersonationHeaders(req)
	case gatewayClientImpersonationClaudeCode:
		if !claudeCodeClientImpersonationEnabled(cfg, candidate) {
			return
		}
		applyClaudeCodeClientImpersonationHeaders(req, claudeCodeSessionID)
	}
}

func applyGatewayClientImpersonationPayload(payload map[string]any, cfg *sitepkg.GatewayConfig, protocol gatewayProtocolAdapter, request gatewayRequest, candidate routeengine.Candidate) string {
	if payload == nil || !claudeCodeClientImpersonationEnabled(cfg, candidate) {
		return ""
	}
	if gatewayClientImpersonationKindForRequest(protocol, request, candidate) != gatewayClientImpersonationClaudeCode {
		return ""
	}
	if !gatewayProtocolIsAnthropicMessages(protocol) {
		return ""
	}

	sessionID := strings.TrimSpace(request.DownstreamHeaders.Get(claudeCodeGatewaySessionHeader))
	if sessionID == "" {
		sessionID = claudeCodePayloadMetadataSessionID(payload)
	}
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	applyClaudeCodeClientImpersonationPayload(payload, sessionID)
	return sessionID
}

func gatewayClientImpersonationKindForRequest(protocol gatewayProtocolAdapter, request gatewayRequest, candidate routeengine.Candidate) gatewayClientImpersonationKind {
	for _, model := range []string{
		candidate.Model.UpstreamName,
		candidate.Model.DisplayName,
		request.RequestedModel,
		stringFromPayloadModel(request.Payload),
	} {
		if kind := gatewayClientImpersonationKindForModel(model); kind != gatewayClientImpersonationNone {
			return kind
		}
	}

	switch protocol.(type) {
	case anthropicMessagesProtocolAdapter, *providerAnthropicMessagesProtocolAdapter:
		return gatewayClientImpersonationClaudeCode
	case codexProtocolAdapter, openAIChatProtocolAdapter, openAIResponsesProtocolAdapter:
		return gatewayClientImpersonationCodex
	}

	return gatewayClientImpersonationNone
}

func claudeCodeClientImpersonationEnabled(cfg *sitepkg.GatewayConfig, candidate routeengine.Candidate) bool {
	return isClaudeCodeSite(candidate.Site.SiteType) || (cfg != nil && gatewayConfigBool(cfg.ImpersonateClaudeCodeClient))
}

func gatewayClientImpersonationKindForModel(model string) gatewayClientImpersonationKind {
	if isClaudeModelName(model) {
		return gatewayClientImpersonationClaudeCode
	}
	if isGPTModelName(model) {
		return gatewayClientImpersonationCodex
	}
	return gatewayClientImpersonationNone
}

func gatewayProtocolIsAnthropicMessages(protocol gatewayProtocolAdapter) bool {
	switch protocol.(type) {
	case anthropicMessagesProtocolAdapter, *providerAnthropicMessagesProtocolAdapter:
		return true
	default:
		return false
	}
}

func isClaudeModelName(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(model, "claude-")
}

func isGPTModelName(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(model, "gpt-"):
		return true
	case strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"), strings.HasPrefix(model, "o4"):
		return true
	default:
		return strings.Contains(model, "codex")
	}
}

func applyCodexClientImpersonationHeaders(req *http.Request) {
	req.Header.Set("User-Agent", codexGatewayUserAgent)
	req.Header.Set("Originator", codexOriginator)
	ensureCodexFingerprintHeaders(req)
}

func ensureCodexFingerprintHeaders(req *http.Request) {
	if req == nil {
		return
	}
	sessionID := codexHeaderValueOrNew(req, codexGatewaySessionHeader)
	threadID := codexHeaderValueOrNew(req, codexGatewayThreadHeader)
	clientRequestID := codexHeaderValueOrNew(req, codexGatewayClientRequestHeader)
	windowID := codexHeaderValueOrNew(req, codexGatewayWindowHeader)
	if strings.TrimSpace(req.Header.Get(codexGatewayBetaFeaturesHeader)) == "" {
		req.Header.Set(codexGatewayBetaFeaturesHeader, codexGatewayBetaFeatures)
	}
	if strings.TrimSpace(req.Header.Get(codexGatewayTurnMetadataHeader)) == "" {
		metadata := map[string]string{
			"turn_id":           clientRequestID,
			"session_id":        sessionID,
			"thread_id":         threadID,
			"x-codex-window-id": windowID,
		}
		encoded, _ := json.Marshal(metadata)
		req.Header.Set(codexGatewayTurnMetadataHeader, string(encoded))
	}
}

func codexHeaderValueOrNew(req *http.Request, header string) string {
	value := strings.TrimSpace(req.Header.Get(header))
	if value == "" {
		value = uuid.NewString()
		req.Header.Set(header, value)
	}
	return value
}

func applyClaudeCodeClientImpersonationHeaders(req *http.Request, sessionID string) {
	req.Header.Set("User-Agent", claudeCodeGatewayUserAgent)
	req.Header.Set(claudeCodeGatewayAppHeader, claudeCodeGatewayApp)
	req.Header.Set(claudeCodeGatewayBetaHeader, claudeCodeGatewayAnthropicBeta)
	req.Header.Set(claudeCodeGatewayVersionHeader, claudeCodeGatewayAPIVersion)
	req.Header.Set(claudeCodeGatewayProtection, "true")
	if strings.EqualFold(strings.TrimSpace(req.Header.Get("Originator")), codexOriginator) {
		req.Header.Del("Originator")
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = strings.TrimSpace(req.Header.Get(claudeCodeGatewaySessionHeader))
	}
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	req.Header.Set(claudeCodeGatewaySessionHeader, sessionID)
}

func gatewayConfigBool(value *bool) bool {
	return value != nil && *value
}

func applyClaudeCodeClientImpersonationPayload(payload map[string]any, sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = uuid.NewString()
	}
	// The Anthropic OAuth upstream rejects unknown top-level fields with 400,
	// so client-side extras like diagnostics must not reach it.
	delete(payload, "diagnostics")
	ensureClaudeCodeSystemPrompt(payload)
	ensureClaudeCodeMetadataUserID(payload, sessionID)
}

func ensureClaudeCodeSystemPrompt(payload map[string]any) {
	if claudeCodeSystemPromptPresent(payload["system"]) {
		return
	}
	promptBlock := map[string]any{
		"type": "text",
		"text": claudeCodeGatewaySystemPrompt,
	}
	switch system := payload["system"].(type) {
	case []any:
		payload["system"] = append([]any{promptBlock}, system...)
	case string:
		if strings.TrimSpace(system) == "" {
			payload["system"] = []any{promptBlock}
		} else {
			payload["system"] = []any{promptBlock, map[string]any{"type": "text", "text": system}}
		}
	default:
		payload["system"] = []any{promptBlock}
	}
}

func claudeCodeSystemPromptPresent(system any) bool {
	switch typed := system.(type) {
	case string:
		return claudeCodeSystemTextPresent(typed)
	case []any:
		for _, item := range typed {
			if block, ok := item.(map[string]any); ok && claudeCodeSystemTextPresent(anyString(block["text"])) {
				return true
			}
		}
	}
	return false
}

func claudeCodeSystemTextPresent(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	return strings.Contains(text, "Claude Code") ||
		(strings.HasPrefix(text, claudeCodeBillingHeaderPrefix) && strings.Contains(text, claudeCodeCLIEntrypointMarker))
}

func ensureClaudeCodeMetadataUserID(payload map[string]any, sessionID string) {
	metadata, _ := payload["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
		payload["metadata"] = metadata
	}
	if userID, _ := metadata["user_id"].(string); claudeCodeMetadataUserIDValid(userID) {
		return
	}
	metadata["user_id"] = claudeCodeMetadataUserID(sessionID)
}

func claudeCodePayloadMetadataSessionID(payload map[string]any) string {
	metadata, _ := payload["metadata"].(map[string]any)
	if metadata == nil {
		return ""
	}
	userID, _ := metadata["user_id"].(string)
	return claudeCodeMetadataSessionID(userID)
}

func claudeCodeMetadataUserID(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	userID := struct {
		DeviceID    string `json:"device_id"`
		AccountUUID string `json:"account_uuid"`
		SessionID   string `json:"session_id"`
	}{
		DeviceID:  hex.EncodeToString(sum[:]),
		SessionID: strings.TrimSpace(sessionID),
	}
	encoded, _ := json.Marshal(userID)
	return string(encoded)
}

func claudeCodeMetadataUserIDValid(value string) bool {
	return claudeCodeMetadataSessionID(value) != ""
}

func claudeCodeMetadataSessionID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "{") {
		var userID struct {
			DeviceID    string `json:"device_id"`
			AccountUUID string `json:"account_uuid"`
			SessionID   string `json:"session_id"`
		}
		if err := json.Unmarshal([]byte(value), &userID); err != nil {
			return ""
		}
		if strings.TrimSpace(userID.DeviceID) == "" || strings.TrimSpace(userID.SessionID) == "" {
			return ""
		}
		return strings.TrimSpace(userID.SessionID)
	}
	matches := claudeCodeLegacyMetadataUserIDPattern.FindStringSubmatch(value)
	if matches == nil {
		return ""
	}
	return matches[3]
}
