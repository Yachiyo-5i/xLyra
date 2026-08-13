package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
)

func TestApplyDownstreamPassthroughHeadersPassesClientHeadersForResponses(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/responses")
	downstreamHeaders := http.Header{}
	downstreamHeaders.Set("Authorization", "Bearer downstream-secret")
	downstreamHeaders.Set("Cookie", "session=secret")
	downstreamHeaders.Set("X-Api-Key", "downstream-key")
	downstreamHeaders.Set("Originator", "codex-tui")
	downstreamHeaders.Set("User-Agent", "codex-tui/0.133.0 (Mac OS 26.0.1; arm64)")
	downstreamHeaders.Set("Session-Id", "session-123")
	downstreamHeaders.Set("Thread-Id", "thread-123")
	downstreamHeaders.Set("X-Client-Request-Id", "client-request-123")
	downstreamHeaders.Set("X-Codex-Beta-Features", "terminal_resize_reflow")
	downstreamHeaders.Set("X-Codex-Turn-Metadata", `{"turn_id":"turn-123"}`)
	downstreamHeaders.Set("X-Codex-Window-Id", "window-123")

	applyDownstreamPassthroughHeaders(upstreamReq, gatewayRequest{
		DownstreamPath:    gatewayEndpointResponses,
		DownstreamHeaders: downstreamHeaders,
	}, testRouteCandidateForSite("openai", "gpt-5.4"))

	for _, key := range []string{"User-Agent", "Originator", "Session-Id", "Thread-Id", "X-Client-Request-Id", "X-Codex-Beta-Features", "X-Codex-Turn-Metadata", "X-Codex-Window-Id"} {
		if got := upstreamReq.Header.Get(key); got == "" {
			t.Fatalf("%s was not passed through", key)
		}
	}
	for _, key := range []string{"Authorization", "Cookie", "X-Api-Key"} {
		assertNoGatewayHeader(t, upstreamReq, key)
	}
}

func TestApplyDownstreamPassthroughHeadersDropsAllDownstreamHeadersForCodex(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/responses")
	upstreamReq.Header.Set("User-Agent", codexGatewayUserAgent)
	upstreamReq.Header.Set("Originator", codexOriginator)

	downstreamHeaders := http.Header{}
	downstreamHeaders.Set("Originator", "openclaw")
	downstreamHeaders.Set("User-Agent", "hermes/1.2.3")
	downstreamHeaders.Set("Session-Id", "session-123")
	downstreamHeaders.Set("X-Oai-Attestation", "attestation-blob")
	downstreamHeaders.Set("X-Codex-Installation-Id", "install-123")
	downstreamHeaders.Set("Openai-Beta", "responses=v1")
	downstreamHeaders.Set("X-Codex-Turn-State", "turn-state")
	downstreamHeaders.Set("X-Downstream-Trace", "keep-me")

	applyDownstreamPassthroughHeaders(upstreamReq, gatewayRequest{
		DownstreamPath:    gatewayEndpointResponses,
		DownstreamHeaders: downstreamHeaders,
	}, testRouteCandidateForSite("codex", "gpt-5.4"))

	assertGatewayHeader(t, upstreamReq, "User-Agent", codexGatewayUserAgent)
	assertGatewayHeader(t, upstreamReq, "Originator", codexOriginator)
	for _, key := range []string{"Session-Id", "X-Oai-Attestation", "X-Codex-Installation-Id", "Openai-Beta", "X-Codex-Turn-State", "X-Downstream-Trace"} {
		assertNoGatewayHeader(t, upstreamReq, key)
	}
}

func TestApplyDownstreamPassthroughHeadersPassesResponsesLiteForCodex(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses")
	upstreamReq.Header.Set("User-Agent", codexGatewayUserAgent)
	upstreamReq.Header.Set("Originator", codexOriginator)

	downstreamHeaders := http.Header{}
	downstreamHeaders.Set(codexResponsesLiteHeader, "true")
	downstreamHeaders.Set("Session-Id", "session-123")
	downstreamHeaders.Set("X-Downstream-Trace", "drop-me")

	applyDownstreamPassthroughHeaders(upstreamReq, gatewayRequest{
		DownstreamPath:    gatewayEndpointResponses,
		DownstreamHeaders: downstreamHeaders,
	}, testRouteCandidateForSite("codex", "gpt-5.6"))

	assertGatewayHeader(t, upstreamReq, codexResponsesLiteHeader, "true")
	for _, key := range []string{"Session-Id", "X-Downstream-Trace"} {
		assertNoGatewayHeader(t, upstreamReq, key)
	}
	assertGatewayHeader(t, upstreamReq, "User-Agent", codexGatewayUserAgent)
	assertGatewayHeader(t, upstreamReq, "Originator", codexOriginator)
}

func TestApplyDownstreamPassthroughHeadersRejectsInactiveResponsesLiteForCodex(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		value string
	}{
		{name: "false value", path: gatewayEndpointResponses, value: "false"},
		{name: "unknown value", path: gatewayEndpointResponses, value: "enabled"},
		{name: "messages endpoint", path: gatewayEndpointMessages, value: "true"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses")
			downstreamHeaders := http.Header{}
			downstreamHeaders.Set(codexResponsesLiteHeader, test.value)

			applyDownstreamPassthroughHeaders(upstreamReq, gatewayRequest{
				DownstreamPath:    test.path,
				DownstreamHeaders: downstreamHeaders,
			}, testRouteCandidateForSite("codex", "gpt-5.6"))

			assertNoGatewayHeader(t, upstreamReq, codexResponsesLiteHeader)
		})
	}
}

func TestApplyDownstreamPassthroughHeadersPassesRemoteCompactionV2HeadersForCodex(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses")
	upstreamReq.Header.Set("User-Agent", codexGatewayUserAgent)
	upstreamReq.Header.Set("Originator", codexOriginator)

	downstreamHeaders := remoteCompactionV2Headers()
	downstreamHeaders.Set("Authorization", "Bearer downstream-secret")
	downstreamHeaders.Set("User-Agent", "codex_cli_rs/0.144.1")
	downstreamHeaders.Set("Originator", "codex_cli_rs")
	downstreamHeaders.Set("X-Downstream-Trace", "drop-me")

	applyDownstreamPassthroughHeaders(upstreamReq, remoteCompactionV2GatewayRequest(downstreamHeaders, "gpt-5.4"), testRouteCandidateForSite("codex", "gpt-5.4"))

	for _, key := range []string{
		"Session-Id",
		"Thread-Id",
		"X-Client-Request-Id",
		"X-Codex-Beta-Features",
		"X-Codex-Installation-Id",
		"X-Codex-Parent-Thread-Id",
		"X-Codex-Turn-Metadata",
		"X-Codex-Turn-State",
		"X-Codex-Window-Id",
		"X-Oai-Attestation",
		"X-Openai-Subagent",
	} {
		if got := upstreamReq.Header.Get(key); got == "" {
			t.Fatalf("%s was not passed through", key)
		}
	}
	assertGatewayHeader(t, upstreamReq, "User-Agent", codexGatewayUserAgent)
	assertGatewayHeader(t, upstreamReq, "Originator", codexOriginator)
	for _, key := range []string{"Authorization", "X-Downstream-Trace"} {
		assertNoGatewayHeader(t, upstreamReq, key)
	}
}

func TestApplyDownstreamPassthroughHeadersPassesRemoteCompactionV2HeadersForGPTAggregator(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://aggregator.example/v1/responses")
	downstreamHeaders := remoteCompactionV2Headers()
	downstreamHeaders.Set("User-Agent", "codex_cli_rs/0.144.1")
	downstreamHeaders.Set("Originator", "codex_cli_rs")

	applyDownstreamPassthroughHeaders(upstreamReq, remoteCompactionV2GatewayRequest(downstreamHeaders, "alias-gpt"), testRouteCandidateForSite("newapi", "gpt-5.4"))

	for _, key := range []string{
		"User-Agent",
		"Originator",
		"Session-Id",
		"Thread-Id",
		"X-Client-Request-Id",
		"X-Codex-Beta-Features",
		"X-Codex-Installation-Id",
		"X-Codex-Turn-Metadata",
		"X-Codex-Turn-State",
		"X-Codex-Window-Id",
	} {
		if got := upstreamReq.Header.Get(key); got == "" {
			t.Fatalf("%s was not passed through", key)
		}
	}
}

func TestIsCodexRemoteCompactionV2RequestRequiresFeatureTriggerAndGPTModel(t *testing.T) {
	headers := remoteCompactionV2Headers()
	request := remoteCompactionV2GatewayRequest(headers, "gpt-5.4")
	if !isCodexRemoteCompactionV2Request(request, testRouteCandidateForSite("newapi", "gpt-5.4")) {
		t.Fatal("expected remote compaction v2 request")
	}

	missingFeature := remoteCompactionV2GatewayRequest(http.Header{}, "gpt-5.4")
	if isCodexRemoteCompactionV2Request(missingFeature, testRouteCandidateForSite("newapi", "gpt-5.4")) {
		t.Fatal("request without remote_compaction_v2 feature should not match")
	}

	missingTrigger := remoteCompactionV2GatewayRequest(headers, "gpt-5.4")
	missingTrigger.Payload["input"] = []any{map[string]any{"type": "message", "role": "user"}}
	if isCodexRemoteCompactionV2Request(missingTrigger, testRouteCandidateForSite("newapi", "gpt-5.4")) {
		t.Fatal("request without compaction_trigger should not match")
	}

	claudeRequest := remoteCompactionV2GatewayRequest(headers, "claude-sonnet-4-5")
	if isCodexRemoteCompactionV2Request(claudeRequest, testRouteCandidateForSite("anthropic", "claude-sonnet-4-5")) {
		t.Fatal("non-GPT model should not match")
	}
}

func TestApplyDownstreamPassthroughHeadersPassesAnthropicHeadersForMessages(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/messages")
	downstreamHeaders := http.Header{}
	downstreamHeaders.Set("Anthropic-Beta", "context-1m-2025-08-07")
	downstreamHeaders.Set("User-Agent", "claude-code/1.0")
	downstreamHeaders.Set("Originator", "claude-code")
	downstreamHeaders.Set("Authorization", "Bearer downstream-secret")

	applyDownstreamPassthroughHeaders(upstreamReq, gatewayRequest{
		DownstreamPath:    gatewayEndpointMessages,
		DownstreamHeaders: downstreamHeaders,
	}, testRouteCandidateForSite("anthropic", "claude-sonnet-4-5"))

	assertGatewayHeader(t, upstreamReq, "Anthropic-Beta", "context-1m-2025-08-07")
	assertGatewayHeader(t, upstreamReq, "User-Agent", "claude-code/1.0")
	assertNoGatewayHeader(t, upstreamReq, "Authorization")
}

func TestApplyGatewayClientImpersonationHeadersOverridesDownstreamHeaders(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/responses")
	downstreamHeaders := http.Header{}
	downstreamHeaders.Set("User-Agent", "downstream-client/1.0")
	downstreamHeaders.Set("Originator", "downstream-client")
	downstreamHeaders.Set("X-Claude-Code-Session-Id", "real-claude-session")

	applyDownstreamPassthroughHeaders(upstreamReq, gatewayRequest{
		DownstreamPath:    gatewayEndpointResponses,
		DownstreamHeaders: downstreamHeaders,
	}, testRouteCandidateForSite("anthropic", "claude-sonnet-4-5"))
	applyGatewayClientImpersonationHeaders(upstreamReq, &sitepkg.GatewayConfig{
		ImpersonateCodexClient:      testBool(false),
		ImpersonateClaudeCodeClient: testBool(true),
	}, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), testRouteCandidate("claude-sonnet-4-5"), "")

	assertGatewayHeader(t, upstreamReq, "User-Agent", claudeCodeGatewayUserAgent)
	assertGatewayHeader(t, upstreamReq, "Originator", "downstream-client")
	assertGatewayHeader(t, upstreamReq, "X-Claude-Code-Session-Id", "real-claude-session")
}

func TestApplyGatewayClientImpersonationHeadersGeneratesClaudeSessionID(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/messages")

	applyGatewayClientImpersonationHeaders(upstreamReq, &sitepkg.GatewayConfig{
		ImpersonateClaudeCodeClient: testBool(true),
	}, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), testRouteCandidate("claude-sonnet-4-5"), "")

	if _, err := uuid.Parse(upstreamReq.Header.Get("X-Claude-Code-Session-Id")); err != nil {
		t.Fatalf("X-Claude-Code-Session-Id is not a UUID: %v", err)
	}
	assertGatewayHeader(t, upstreamReq, claudeCodeGatewayAppHeader, claudeCodeGatewayApp)
	assertGatewayHeader(t, upstreamReq, claudeCodeGatewayBetaHeader, claudeCodeGatewayAnthropicBeta)
	assertGatewayHeader(t, upstreamReq, claudeCodeGatewayVersionHeader, claudeCodeGatewayAPIVersion)
	assertGatewayHeader(t, upstreamReq, claudeCodeGatewayProtection, "true")
}

func TestApplyGatewayClientImpersonationHeadersDefaultsForClaudeCodeSite(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/messages")

	applyGatewayClientImpersonationHeaders(upstreamReq, nil, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), testRouteCandidateForSite("claude_code", "claude-sonnet-4-5"), "")

	if _, err := uuid.Parse(upstreamReq.Header.Get("X-Claude-Code-Session-Id")); err != nil {
		t.Fatalf("X-Claude-Code-Session-Id is not a UUID: %v", err)
	}
	assertGatewayHeader(t, upstreamReq, claudeCodeGatewayAppHeader, claudeCodeGatewayApp)
	assertGatewayHeader(t, upstreamReq, claudeCodeGatewayBetaHeader, claudeCodeGatewayAnthropicBeta)
	assertGatewayHeader(t, upstreamReq, claudeCodeGatewayVersionHeader, claudeCodeGatewayAPIVersion)
	assertGatewayHeader(t, upstreamReq, claudeCodeGatewayProtection, "true")
}

func TestApplyGatewayClientImpersonationPayloadAddsClaudeCodeMessagesMarkers(t *testing.T) {
	payload := map[string]any{
		"model":      "claude-sonnet-4-5",
		"max_tokens": 1024,
		"messages":   []any{},
		"system":     "Existing system prompt.",
	}
	request := testGatewayRequest("claude-sonnet-4-5")
	request.DownstreamHeaders = http.Header{}
	request.DownstreamHeaders.Set(claudeCodeGatewaySessionHeader, "12345678-1234-1234-1234-123456789abc")

	sessionID := applyGatewayClientImpersonationPayload(payload, &sitepkg.GatewayConfig{
		ImpersonateClaudeCodeClient: testBool(true),
	}, anthropicMessagesProtocolAdapter{}, request, testRouteCandidate("claude-sonnet-4-5"))

	if sessionID != "12345678-1234-1234-1234-123456789abc" {
		t.Fatalf("sessionID = %q", sessionID)
	}
	system, ok := payload["system"].([]any)
	if !ok || len(system) != 2 {
		t.Fatalf("system = %#v, want two content blocks", payload["system"])
	}
	first, _ := system[0].(map[string]any)
	if got := first["text"]; got != claudeCodeGatewaySystemPrompt {
		t.Fatalf("first system text = %#v, want Claude Code prompt", got)
	}
	metadata, _ := payload["metadata"].(map[string]any)
	userID, _ := metadata["user_id"].(string)
	if !claudeCodeMetadataUserIDValid(userID) {
		t.Fatalf("metadata.user_id is not valid Claude Code metadata: %q", userID)
	}
	if !strings.Contains(userID, sessionID) {
		t.Fatalf("metadata.user_id = %q, want session %q", userID, sessionID)
	}
}

func TestApplyGatewayClientImpersonationPayloadDefaultsForClaudeCodeSite(t *testing.T) {
	payload := map[string]any{
		"model":       "claude-sonnet-4-5",
		"max_tokens":  1024,
		"messages":    []any{},
		"diagnostics": map[string]any{"session": "cli"},
	}

	sessionID := applyGatewayClientImpersonationPayload(payload, nil, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), testRouteCandidateForSite("claude_code", "claude-sonnet-4-5"))

	if _, err := uuid.Parse(sessionID); err != nil {
		t.Fatalf("sessionID is not a UUID: %v", err)
	}
	if !claudeCodeSystemPromptPresent(payload["system"]) {
		t.Fatalf("system = %#v, want Claude Code prompt", payload["system"])
	}
	metadata, _ := payload["metadata"].(map[string]any)
	userID, _ := metadata["user_id"].(string)
	if !claudeCodeMetadataUserIDValid(userID) {
		t.Fatalf("metadata.user_id is not valid Claude Code metadata: %q", userID)
	}
	if _, ok := payload["diagnostics"]; ok {
		t.Fatalf("diagnostics should be removed before Claude Code upstream request: %#v", payload["diagnostics"])
	}
}

func TestApplyGatewayClientImpersonationPayloadSkipsNonMessagesProtocol(t *testing.T) {
	payload := map[string]any{
		"model":       "claude-sonnet-4-5",
		"diagnostics": map[string]any{"session": "cli"},
	}

	sessionID := applyGatewayClientImpersonationPayload(payload, &sitepkg.GatewayConfig{
		ImpersonateClaudeCodeClient: testBool(true),
	}, openAIResponsesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), testRouteCandidate("claude-sonnet-4-5"))

	if sessionID != "" {
		t.Fatalf("sessionID = %q, want empty", sessionID)
	}
	if _, ok := payload["metadata"]; ok {
		t.Fatalf("metadata was added for non-messages protocol: %#v", payload["metadata"])
	}
	if _, ok := payload["diagnostics"]; !ok {
		t.Fatalf("diagnostics should be preserved outside the Claude Code impersonation path: %#v", payload)
	}
}

func TestApplyGatewayClientImpersonationPayloadKeepsDiagnosticsWhenImpersonationDisabled(t *testing.T) {
	payload := map[string]any{
		"model":       "claude-sonnet-4-5",
		"max_tokens":  1024,
		"messages":    []any{},
		"diagnostics": map[string]any{"session": "cli"},
	}

	sessionID := applyGatewayClientImpersonationPayload(payload, nil, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), testRouteCandidate("claude-sonnet-4-5"))

	if sessionID != "" {
		t.Fatalf("sessionID = %q, want empty", sessionID)
	}
	if _, ok := payload["metadata"]; ok {
		t.Fatalf("metadata was added without impersonation enabled: %#v", payload["metadata"])
	}
	if _, ok := payload["diagnostics"]; !ok {
		t.Fatalf("diagnostics should be preserved outside the Claude Code impersonation path: %#v", payload)
	}
}

func TestApplyGatewayClientImpersonationHeadersUsesClaudeCodeForClaudeModelWhenBothEnabled(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/messages")
	upstreamReq.Header.Set("Originator", codexOriginator)

	applyGatewayClientImpersonationHeaders(upstreamReq, &sitepkg.GatewayConfig{
		ImpersonateCodexClient:      testBool(true),
		ImpersonateClaudeCodeClient: testBool(true),
	}, openAIResponsesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), testRouteCandidate("claude-sonnet-4-5"), "")

	assertGatewayHeader(t, upstreamReq, "User-Agent", claudeCodeGatewayUserAgent)
	assertNoGatewayHeader(t, upstreamReq, "Originator")
}

func TestApplyGatewayClientImpersonationHeadersUsesCodexForGPTModelWhenBothEnabled(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/responses")

	applyGatewayClientImpersonationHeaders(upstreamReq, &sitepkg.GatewayConfig{
		ImpersonateCodexClient:      testBool(true),
		ImpersonateClaudeCodeClient: testBool(true),
	}, openAIResponsesProtocolAdapter{}, testGatewayRequest("gpt-5.4"), testRouteCandidate("gpt-5.4"), "")

	assertGatewayHeader(t, upstreamReq, "User-Agent", codexGatewayUserAgent)
	assertGatewayHeader(t, upstreamReq, "Originator", codexOriginator)
	assertNoGatewayHeader(t, upstreamReq, "X-Claude-Code-Session-Id")
}

func TestApplyGatewayClientImpersonationHeadersAddsCodexFingerprintHeaders(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/responses")

	applyGatewayClientImpersonationHeaders(upstreamReq, &sitepkg.GatewayConfig{
		ImpersonateCodexClient: testBool(true),
	}, openAIResponsesProtocolAdapter{}, testGatewayRequest("gpt-5.4"), testRouteCandidate("gpt-5.4"), "")

	for _, key := range []string{
		"Session-Id",
		"Thread-Id",
		"X-Client-Request-Id",
		"X-Codex-Beta-Features",
		"X-Codex-Turn-Metadata",
		"X-Codex-Window-Id",
	} {
		if got := upstreamReq.Header.Get(key); got == "" {
			t.Fatalf("%s was not set", key)
		}
	}
	if _, err := uuid.Parse(upstreamReq.Header.Get("X-Codex-Window-Id")); err != nil {
		t.Fatalf("X-Codex-Window-Id is not a UUID: %v", err)
	}
	var turnMetadata map[string]string
	if err := json.Unmarshal([]byte(upstreamReq.Header.Get("X-Codex-Turn-Metadata")), &turnMetadata); err != nil {
		t.Fatalf("X-Codex-Turn-Metadata is not JSON: %v", err)
	}
	if _, err := uuid.Parse(turnMetadata["turn_id"]); err != nil {
		t.Fatalf("turn_id is not a UUID: %v", err)
	}
}

func TestApplyGatewayClientImpersonationHeadersUsesModelBeforeProtocol(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/messages")

	applyGatewayClientImpersonationHeaders(upstreamReq, &sitepkg.GatewayConfig{
		ImpersonateCodexClient:      testBool(true),
		ImpersonateClaudeCodeClient: testBool(true),
	}, anthropicMessagesProtocolAdapter{}, testGatewayRequest("gpt-5.4"), testRouteCandidate("gpt-5.4"), "")

	assertGatewayHeader(t, upstreamReq, "User-Agent", codexGatewayUserAgent)
	assertNoGatewayHeader(t, upstreamReq, "X-Claude-Code-Session-Id")
}

func TestApplyGatewayClientImpersonationHeadersDoesNotMixCodexIntoClaudeRequest(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/messages")

	applyGatewayClientImpersonationHeaders(upstreamReq, &sitepkg.GatewayConfig{
		ImpersonateCodexClient: testBool(true),
	}, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), testRouteCandidate("claude-sonnet-4-5"), "")

	assertNoGatewayHeader(t, upstreamReq, "User-Agent")
	assertNoGatewayHeader(t, upstreamReq, "Originator")
}

func TestApplyGatewayClientImpersonationHeadersDoesNotMixClaudeCodeIntoGPTRequest(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/responses")

	applyGatewayClientImpersonationHeaders(upstreamReq, &sitepkg.GatewayConfig{
		ImpersonateClaudeCodeClient: testBool(true),
	}, openAIResponsesProtocolAdapter{}, testGatewayRequest("gpt-5.4"), testRouteCandidate("gpt-5.4"), "")

	assertNoGatewayHeader(t, upstreamReq, "User-Agent")
	assertNoGatewayHeader(t, upstreamReq, "X-Claude-Code-Session-Id")
}

func TestRequestHeadersOverrideConfiguredClientImpersonation(t *testing.T) {
	upstreamReq := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/responses")

	applyGatewayClientImpersonationHeaders(upstreamReq, &sitepkg.GatewayConfig{
		ImpersonateCodexClient: testBool(true),
	}, openAIResponsesProtocolAdapter{}, testGatewayRequest("gpt-5.4"), testRouteCandidate("gpt-5.4"), "")
	for key, value := range map[string]string{
		"User-Agent": "manual-client/1.0",
		"Originator": "manual-client",
	} {
		upstreamReq.Header.Set(key, value)
	}

	assertGatewayHeader(t, upstreamReq, "User-Agent", "manual-client/1.0")
	assertGatewayHeader(t, upstreamReq, "Originator", "manual-client")
}

func TestApplyGatewayClientImpersonationPayloadReusesSessionFromMetadata(t *testing.T) {
	sessionID := "12345678-1234-1234-1234-123456789abc"
	payload := map[string]any{
		"model":    "claude-sonnet-4-5",
		"system":   []any{map[string]any{"type": "text", "text": claudeCodeGatewaySystemPrompt}},
		"metadata": map[string]any{"user_id": claudeCodeMetadataUserID(sessionID)},
	}

	got := applyGatewayClientImpersonationPayload(payload, &sitepkg.GatewayConfig{
		ImpersonateClaudeCodeClient: testBool(true),
	}, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), testRouteCandidate("claude-sonnet-4-5"))

	if got != sessionID {
		t.Fatalf("sessionID = %q, want %q", got, sessionID)
	}
	system := payload["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("existing Claude Code system prompt should not be duplicated: %#v", system)
	}
}

func TestClaudeCodeMetadataSessionIDSupportsJSONAndLegacyFormats(t *testing.T) {
	t.Parallel()

	sessionID := "12345678-1234-1234-1234-123456789abc"
	jsonUserID := claudeCodeMetadataUserID(sessionID)
	if got := claudeCodeMetadataSessionID(jsonUserID); got != sessionID {
		t.Fatalf("json metadata session = %q, want %q", got, sessionID)
	}
	if got := claudeCodePayloadMetadataSessionID(map[string]any{"metadata": map[string]any{"user_id": jsonUserID}}); got != sessionID {
		t.Fatalf("payload metadata session = %q, want %q", got, sessionID)
	}

	legacy := "user_" + strings.Repeat("a", 64) + "_account_abcdef12_session_" + sessionID
	if got := claudeCodeMetadataSessionID(legacy); got != sessionID {
		t.Fatalf("legacy metadata session = %q, want %q", got, sessionID)
	}
	for _, value := range []string{"", "{bad-json", `{"device_id":"","session_id":"` + sessionID + `"}`, "not-legacy"} {
		if got := claudeCodeMetadataSessionID(value); got != "" {
			t.Fatalf("invalid metadata session %q = %q, want empty", value, got)
		}
	}
	if got := claudeCodePayloadMetadataSessionID(map[string]any{"metadata": "not-a-map"}); got != "" {
		t.Fatalf("non-map payload metadata session = %q, want empty", got)
	}
}

func TestGatewayClientImpersonationKindModelFallbacks(t *testing.T) {
	t.Parallel()

	for _, model := range []string{"o1-preview", " o3-mini ", "o4-mini", "my-codex-model"} {
		if got := gatewayClientImpersonationKindForModel(model); got != gatewayClientImpersonationCodex {
			t.Fatalf("model %q kind = %q, want codex", model, got)
		}
	}
	if got := gatewayClientImpersonationKindForRequest(nil, gatewayRequest{
		RequestedModel: "unknown",
		Payload:        map[string]any{"model": "gpt-5"},
	}, routeengine.Candidate{}); got != gatewayClientImpersonationCodex {
		t.Fatalf("payload model fallback kind = %q, want codex", got)
	}
	if got := gatewayClientImpersonationKindForRequest(nil, gatewayRequest{}, routeengine.Candidate{}); got != gatewayClientImpersonationNone {
		t.Fatalf("empty request kind = %q, want none", got)
	}
}

func testBool(value bool) *bool {
	return &value
}

func testGatewayRequest(model string) gatewayRequest {
	return gatewayRequest{
		RequestedModel: model,
		Payload: map[string]any{
			"model": model,
		},
	}
}

func testRouteCandidate(model string) routeengine.Candidate {
	return routeengine.Candidate{
		Model: routeengine.CandidateModel{
			UpstreamName: model,
			DisplayName:  model,
		},
	}
}

func testRouteCandidateForSite(siteType string, model string) routeengine.Candidate {
	candidate := testRouteCandidate(model)
	candidate.Site.SiteType = siteType
	return candidate
}

func remoteCompactionV2Headers() http.Header {
	headers := http.Header{}
	headers.Set("Session-Id", "session-123")
	headers.Set("Thread-Id", "thread-123")
	headers.Set("X-Oai-Attestation", "attestation-blob")
	headers.Set("X-Client-Request-Id", "client-request-123")
	headers.Set("X-Codex-Beta-Features", "terminal_resize_reflow,remote_compaction_v2")
	headers.Set("X-Codex-Installation-Id", "install-123")
	headers.Set("X-Codex-Parent-Thread-Id", "parent-thread-123")
	headers.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","window_id":"window-123"}`)
	headers.Set("X-Codex-Turn-State", "turn-state-123")
	headers.Set("X-Codex-Window-Id", "window-123")
	headers.Set("X-Openai-Subagent", "compact")
	return headers
}

func remoteCompactionV2GatewayRequest(headers http.Header, model string) gatewayRequest {
	return gatewayRequest{
		DownstreamPath:    gatewayEndpointResponses,
		DownstreamHeaders: headers,
		RequestedModel:    model,
		Stream:            true,
		Payload: map[string]any{
			"model":  model,
			"stream": true,
			"input": []any{
				map[string]any{"type": "message", "role": "user"},
				map[string]any{"type": "compaction_trigger"},
			},
			"client_metadata": map[string]any{
				"x-codex-turn-metadata": `{"request_kind":"compaction","window_id":"window-123"}`,
				"x-codex-window-id":     "window-123",
			},
		},
	}
}
