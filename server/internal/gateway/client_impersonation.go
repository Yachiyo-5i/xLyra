package gateway

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/claudeversion"
	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
)

const (
	claudeCodeGatewayApp                 = "cli"
	claudeCodeGatewayAnthropicBeta       = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,prompt-caching-scope-2026-01-05,effort-2025-11-24,context-management-2025-06-27,thinking-binding-controls-2026-08-01,mid-conversation-output-config-2026-07-01,extended-cache-ttl-2025-04-11"
	claudeCodeGatewaySystemPrompt        = "You are Claude Code, Anthropic's official CLI for Claude."
	claudeCodeBillingHeaderPrefix        = "x-anthropic-billing-header"
	claudeCodeCLIEntrypointMarker        = "cc_entrypoint=cli"
	claudeCodeGatewayAPIVersion          = adapter.ClaudeCodeAPIVersion
	claudeCodeGatewaySessionHeader       = "X-Claude-Code-Session-Id"
	claudeCodeGatewayAppHeader           = "X-App"
	claudeCodeGatewayBetaHeader          = "anthropic-beta"
	claudeCodeGatewayVersionHeader       = "anthropic-version"
	claudeCodeGatewayProtection          = "x-anthropic-additional-protection"
	claudeCodeGatewayClientRequestHeader = "X-Client-Request-Id"
	codexGatewaySessionHeader            = "Session-Id"
	codexGatewayThreadHeader             = "Thread-Id"
	codexGatewayClientRequestHeader      = "X-Client-Request-Id"
	codexGatewayBetaFeaturesHeader       = "X-Codex-Beta-Features"
	codexGatewayTurnMetadataHeader       = "X-Codex-Turn-Metadata"
	codexGatewayWindowHeader             = "X-Codex-Window-Id"
	codexGatewayBetaFeatures             = "terminal_resize_reflow"
)

var claudeCodeGatewayUserAgent = adapter.ClaudeCodeUserAgent

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
		fingerprint := claudeCodeIdentities.Fingerprint(claudeCodeIdentityKey(candidate), request.DownstreamHeaders)
		stream := request.Stream || strings.EqualFold(strings.TrimSpace(req.Header.Get("Accept")), "text/event-stream")
		applyClaudeCodeClientImpersonationHeadersWithFingerprint(req, claudeCodeSessionID, fingerprint, stream)
	}
}

func applyGatewayClientImpersonationPayload(payload map[string]any, cfg *sitepkg.GatewayConfig, protocol gatewayProtocolAdapter, request gatewayRequest, candidate routeengine.Candidate) string {
	return applyGatewayClientImpersonationPayloadForAccount(payload, cfg, protocol, request, candidate, "")
}

// applyGatewayClientImpersonationPayloadForAccount is the account aware variant
// of applyGatewayClientImpersonationPayload. accountUUID is the upstream
// Anthropic account uuid and is only used for Claude Code sites.
func applyGatewayClientImpersonationPayloadForAccount(payload map[string]any, cfg *sitepkg.GatewayConfig, protocol gatewayProtocolAdapter, request gatewayRequest, candidate routeengine.Candidate, accountUUID string) string {
	if payload == nil || !claudeCodeClientImpersonationEnabled(cfg, candidate) {
		return ""
	}
	if gatewayClientImpersonationKindForRequest(protocol, request, candidate) != gatewayClientImpersonationClaudeCode {
		return ""
	}
	if !gatewayProtocolIsAnthropicMessages(protocol) {
		return ""
	}
	if !isClaudeCodeSite(candidate.Site.SiteType) {
		accountUUID = ""
	}

	key := claudeCodeIdentityKey(candidate)
	fingerprint := claudeCodeIdentities.Fingerprint(key, request.DownstreamHeaders)

	sessionID := strings.TrimSpace(request.DownstreamHeaders.Get(claudeCodeGatewaySessionHeader))
	if sessionID == "" {
		sessionID = claudeCodePayloadMetadataSessionID(payload)
	}
	if sessionID == "" {
		sessionID = claudeCodeIdentities.StickySession(key)
	}

	applyClaudeCodeMimicryPayload(payload, claudeCodeMimicryOptions{
		SessionID:   sessionID,
		ClientID:    fingerprint.ClientID,
		AccountUUID: accountUUID,
		Version:     claudeversion.Version(),
		RealClient:  isRealClaudeCodeRequest(request.DownstreamHeaders, payload),
	})
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
	req.Header.Set("User-Agent", codexGatewayUserAgent())
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
	applyClaudeCodeClientImpersonationHeadersWithFingerprint(req, sessionID, defaultClaudeCodeFingerprint(), false)
}

func applyClaudeCodeClientImpersonationHeadersWithFingerprint(req *http.Request, sessionID string, fingerprint claudeCodeFingerprint, stream bool) {
	req.Header.Set("User-Agent", claudeCodeGatewayUserAgent())
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
	applyClaudeCodeRuntimeHeaders(req, fingerprint, stream)
}

// applyClaudeCodeRuntimeHeaders adds the Anthropic SDK (Stainless) runtime
// description and the other per-request headers a real Claude Code CLI sends.
func applyClaudeCodeRuntimeHeaders(req *http.Request, fingerprint claudeCodeFingerprint, stream bool) {
	fallback := defaultClaudeCodeFingerprint()
	pick := func(value, def string) string {
		if strings.TrimSpace(value) == "" {
			return def
		}
		return value
	}
	// The real SDK sends the OS header as "X-Stainless-OS"; assigning the map key
	// directly avoids Go canonicalising it to "X-Stainless-Os" on the wire.
	delete(req.Header, "X-Stainless-Os")
	req.Header["X-Stainless-OS"] = []string{pick(fingerprint.StainlessOS, fallback.StainlessOS)}
	req.Header.Set("X-Stainless-Lang", pick(fingerprint.StainlessLang, fallback.StainlessLang))
	req.Header.Set("X-Stainless-Package-Version", pick(fingerprint.StainlessPackageVersion, fallback.StainlessPackageVersion))
	req.Header.Set("X-Stainless-Arch", pick(fingerprint.StainlessArch, fallback.StainlessArch))
	req.Header.Set("X-Stainless-Runtime", pick(fingerprint.StainlessRuntime, fallback.StainlessRuntime))
	req.Header.Set("X-Stainless-Runtime-Version", pick(fingerprint.StainlessRuntimeVersion, fallback.StainlessRuntimeVersion))
	req.Header.Set("X-Stainless-Retry-Count", "0")
	req.Header.Set("X-Stainless-Timeout", "600")
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	// The real CLI always asks for application/json, even for streaming calls.
	req.Header.Set("Accept", "application/json")
	if stream {
		req.Header.Set("X-Stainless-Helper-Method", "stream")
	} else {
		req.Header.Del("X-Stainless-Helper-Method")
	}
	if strings.TrimSpace(req.Header.Get(claudeCodeGatewayClientRequestHeader)) == "" {
		req.Header.Set(claudeCodeGatewayClientRequestHeader, uuid.NewString())
	}
}

func gatewayConfigBool(value *bool) bool {
	return value != nil && *value
}

func claudeCodePayloadMetadataSessionID(payload map[string]any) string {
	metadata, _ := payload["metadata"].(map[string]any)
	if metadata == nil {
		return ""
	}
	userID, _ := metadata["user_id"].(string)
	return claudeCodeMetadataSessionID(userID)
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
