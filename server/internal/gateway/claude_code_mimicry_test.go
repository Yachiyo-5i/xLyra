package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

func TestClaudeCodeFingerprintSuffixMatchesCLIAlgorithm(t *testing.T) {
	t.Parallel()

	// The suffix hashes the characters at indices 4, 7 and 20 of the first user text.
	text := "hello world, this is a test"
	sum := sha256.Sum256([]byte("59cf53e54c78" + string([]byte{text[4], text[7], text[20]}) + "2.1.258"))
	want := hex.EncodeToString(sum[:])[:3]
	if got := claudeCodeFingerprintSuffix(text, "2.1.258"); got != want {
		t.Fatalf("suffix = %q, want %q", got, want)
	}

	short := sha256.Sum256([]byte("59cf53e54c78" + "000" + "2.1.258"))
	if got := claudeCodeFingerprintSuffix("", "2.1.258"); got != hex.EncodeToString(short[:])[:3] {
		t.Fatalf("short text suffix = %q", got)
	}
}

func TestClaudeCodeBillingAttributionTextFormat(t *testing.T) {
	t.Parallel()

	got := claudeCodeBillingAttributionText("hello", "2.1.258")
	if !strings.HasPrefix(got, "x-anthropic-billing-header: cc_version=2.1.258.") || !strings.HasSuffix(got, "; cc_entrypoint=cli;") {
		t.Fatalf("billing text = %q", got)
	}
}

func TestApplyClaudeCodeMimicryPayloadRebuildsRequest(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"model":    "claude-sonnet-4-5",
		"system":   []map[string]any{{"type": "text", "text": "Be terse.", "cache_control": map[string]any{"type": "ephemeral"}}},
		"messages": []map[string]any{{"role": "user", "content": "hello world, this is a test"}},
		"thinking": map[string]any{"type": "enabled", "budget_tokens": 1024},
		"tool_choice": map[string]any{
			"type": "auto",
		},
		"diagnostics": map[string]any{"x": 1},
	}

	applyClaudeCodeMimicryPayload(payload, claudeCodeMimicryOptions{
		SessionID:   "12345678-1234-1234-1234-123456789abc",
		ClientID:    strings.Repeat("c", 64),
		AccountUUID: "acct-uuid",
		Version:     "2.1.258",
	})

	if _, ok := payload["diagnostics"]; ok {
		t.Fatal("diagnostics should be removed")
	}
	system := payload["system"].([]any)
	if len(system) != 3 {
		t.Fatalf("system blocks = %d, want 3", len(system))
	}
	billing := system[0].(map[string]any)
	if _, hasCache := billing["cache_control"]; hasCache {
		t.Fatal("billing block must not carry cache_control")
	}
	wantBilling := claudeCodeBillingAttributionText("hello world, this is a test", "2.1.258")
	if billing["text"] != wantBilling {
		t.Fatalf("billing text = %q, want %q", billing["text"], wantBilling)
	}
	if system[1].(map[string]any)["text"] != claudeCodeGatewaySystemPrompt {
		t.Fatalf("identity block = %#v", system[1])
	}
	if _, ok := system[2].(map[string]any)["cache_control"]; !ok {
		t.Fatal("expansion block should carry the cache breakpoint")
	}

	messages := payload["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %d, want instruction, ack and original", len(messages))
	}
	instruction := messages[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if instruction["text"] != "[System Instructions]\nBe terse." {
		t.Fatalf("instruction text = %q", instruction["text"])
	}
	if _, ok := instruction["cache_control"]; !ok {
		t.Fatal("original system cache_control should move to the instruction block")
	}

	if payload["temperature"] != 1 {
		t.Fatalf("temperature = %#v, want 1", payload["temperature"])
	}
	if payload["max_tokens"] != claudeCodeDefaultMaxTokens {
		t.Fatalf("max_tokens = %#v", payload["max_tokens"])
	}
	if _, ok := payload["context_management"]; !ok {
		t.Fatal("context_management should be added for thinking requests")
	}
	if tools, ok := payload["tools"].([]any); !ok || len(tools) != 0 {
		t.Fatalf("tools = %#v, want empty array", payload["tools"])
	}
	if _, ok := payload["tool_choice"]; ok {
		t.Fatal("tool_choice must be dropped when there are no tools")
	}

	userID := payload["metadata"].(map[string]any)["user_id"].(string)
	if want := claudeCodeAccountMetadataUserID(strings.Repeat("c", 64), "acct-uuid", "12345678-1234-1234-1234-123456789abc"); userID != want {
		t.Fatalf("user_id = %q, want %q", userID, want)
	}
}

func TestApplyClaudeCodeMimicryPayloadKeepsExplicitFields(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"model":       "claude-sonnet-4-5",
		"max_tokens":  64,
		"temperature": 0.2,
		"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
		"system":      claudeCodeGatewaySystemPrompt + "\n\nProject rules.",
	}
	applyClaudeCodeMimicryPayload(payload, claudeCodeMimicryOptions{SessionID: "s", ClientID: "c", Version: "2.1.258"})

	if payload["temperature"] != 0.2 || payload["max_tokens"] != 64 {
		t.Fatalf("explicit sampling fields were overwritten: %#v", payload)
	}
	messages := payload["messages"].([]any)
	instruction := messages[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if instruction != "[System Instructions]\nProject rules." {
		t.Fatalf("identity prefix should be stripped from instructions, got %q", instruction)
	}
}

func TestApplyClaudeCodeMimicryPayloadSkipsTemperatureWhenTopPSet(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"model": "claude-sonnet-4-5", "top_p": 0.9, "messages": []any{}}
	applyClaudeCodeMimicryPayload(payload, claudeCodeMimicryOptions{SessionID: "s", ClientID: "c", Version: "2.1.258"})
	if _, ok := payload["temperature"]; ok {
		t.Fatalf("temperature must not be added next to top_p: %#v", payload)
	}
}

func TestApplyClaudeCodeMimicryPayloadLeavesRealClientBody(t *testing.T) {
	t.Parallel()

	system := []any{map[string]any{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.1.abc; cc_entrypoint=cli;"}}
	payload := map[string]any{
		"model":    "claude-sonnet-4-5",
		"system":   system,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		"metadata": map[string]any{"user_id": claudeCodeAccountMetadataUserID("old-device", "old-account", "12345678-1234-1234-1234-123456789abc")},
	}
	applyClaudeCodeMimicryPayload(payload, claudeCodeMimicryOptions{
		SessionID:   "12345678-1234-1234-1234-123456789abc",
		ClientID:    "new-device",
		AccountUUID: "new-account",
		Version:     "2.1.258",
		RealClient:  true,
	})

	if got := payload["system"].([]any); len(got) != 1 {
		t.Fatalf("real client system must be untouched: %#v", got)
	}
	if _, ok := payload["temperature"]; ok {
		t.Fatal("real client body must not receive default sampling fields")
	}
	userID := payload["metadata"].(map[string]any)["user_id"].(string)
	if !strings.Contains(userID, "new-device") || !strings.Contains(userID, "new-account") || strings.Contains(userID, "old-device") {
		t.Fatalf("user_id should carry the account identity: %q", userID)
	}
}

func TestIsRealClaudeCodeRequest(t *testing.T) {
	t.Parallel()

	withUserID := map[string]any{"metadata": map[string]any{"user_id": "x"}}
	cli := http.Header{}
	cli.Set("User-Agent", "claude-cli/2.1.100 (external, cli)")
	if !isRealClaudeCodeRequest(cli, withUserID) {
		t.Fatal("claude-cli UA with user_id should be a real client")
	}
	if isRealClaudeCodeRequest(cli, map[string]any{}) {
		t.Fatal("claude-cli UA without user_id is not a real client")
	}
	if isRealClaudeCodeRequest(http.Header{}, withUserID) {
		t.Fatal("unknown UA without billing block is not a real client")
	}
	relayed := map[string]any{
		"metadata": map[string]any{"user_id": "x"},
		"system":   []any{map[string]any{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.1.abc; cc_entrypoint=cli;"}},
	}
	if !isRealClaudeCodeRequest(http.Header{}, relayed) {
		t.Fatal("billing block with user_id marks relayed real traffic")
	}
}

func TestEnforceClaudeCodeCacheControlLimitDropsToolsThenMessages(t *testing.T) {
	t.Parallel()

	cc := func() map[string]any { return map[string]any{"type": "ephemeral"} }
	payload := map[string]any{
		"tools":  []any{map[string]any{"name": "a", "cache_control": cc()}, map[string]any{"name": "b", "cache_control": cc()}},
		"system": []any{map[string]any{"type": "text", "text": "s", "cache_control": cc()}},
		"messages": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "1", "cache_control": cc()}}},
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "2", "cache_control": cc()}}},
		},
	}
	enforceClaudeCodeCacheControlLimit(payload)

	count := 0
	for _, tool := range payload["tools"].([]any) {
		if _, ok := tool.(map[string]any)["cache_control"]; ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("tool breakpoints = %d, want the last tool removed first leaving 1", count)
	}
	first := payload["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if _, ok := first["cache_control"]; !ok {
		t.Fatal("message breakpoints should be kept when removing tool breakpoints is enough")
	}
}

func TestClaudeCodeIdentityStoreKeepsFingerprintStablePerAccount(t *testing.T) {
	t.Parallel()

	store := newClaudeCodeIdentityStore()
	now := time.Now()
	store.now = func() time.Time { return now }

	first := store.Fingerprint("account-a", nil)
	if len(first.ClientID) != 64 {
		t.Fatalf("ClientID = %q, want 64 hex chars", first.ClientID)
	}
	if again := store.Fingerprint("account-a", nil); again != first {
		t.Fatalf("fingerprint changed between calls: %#v vs %#v", first, again)
	}
	if other := store.Fingerprint("account-b", nil); other.ClientID == first.ClientID {
		t.Fatal("different accounts must get different client ids")
	}

	now = now.Add(claudeCodeIdentityTTL - time.Hour)
	if kept := store.Fingerprint("account-a", nil); kept.ClientID != first.ClientID {
		t.Fatal("use should refresh the sliding TTL")
	}
	now = now.Add(claudeCodeIdentityTTL + time.Minute)
	if renewed := store.Fingerprint("account-a", nil); renewed.ClientID == first.ClientID {
		t.Fatal("expired identity should be regenerated")
	}
}

func TestClaudeCodeIdentityStoreAdoptsRealClientRuntimeOnlyOnCreate(t *testing.T) {
	t.Parallel()

	store := newClaudeCodeIdentityStore()
	thirdParty := http.Header{}
	thirdParty.Set("User-Agent", "python-httpx/0.27")
	thirdParty.Set("X-Stainless-Lang", "python")
	if got := store.Fingerprint("third-party", thirdParty); got.StainlessLang != "js" {
		t.Fatalf("non Claude Code runtime headers must be ignored, got %q", got.StainlessLang)
	}

	cli := http.Header{}
	cli.Set("User-Agent", "claude-cli/2.1.100 (external, cli)")
	cli.Set("X-Stainless-Os", "MacOS")
	cli.Set("X-Stainless-Arch", "x64")
	got := store.Fingerprint("real", cli)
	if got.StainlessOS != "MacOS" || got.StainlessArch != "x64" {
		t.Fatalf("runtime headers from real client not adopted: %#v", got)
	}
	if again := store.Fingerprint("real", http.Header{}); again.StainlessOS != "MacOS" {
		t.Fatal("adopted runtime should persist")
	}
}

func TestClaudeCodeIdentityStoreStickySession(t *testing.T) {
	t.Parallel()

	store := newClaudeCodeIdentityStore()
	now := time.Now()
	store.now = func() time.Time { return now }

	first := store.StickySession("account")
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("session is not a UUID: %v", err)
	}
	now = now.Add(claudeCodeStickySessionTTL - time.Second)
	if got := store.StickySession("account"); got != first {
		t.Fatal("session should be reused while active")
	}
	now = now.Add(claudeCodeStickySessionTTL - time.Second)
	if got := store.StickySession("account"); got != first {
		t.Fatal("each use should extend the session")
	}
	now = now.Add(claudeCodeStickySessionTTL + time.Second)
	if got := store.StickySession("account"); got == first {
		t.Fatal("idle session should rotate")
	}
}

func TestApplyClaudeCodeRuntimeHeaders(t *testing.T) {
	t.Parallel()

	req := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/messages")
	req.Header.Set("Accept", "text/event-stream")
	fingerprint := defaultClaudeCodeFingerprint()
	fingerprint.StainlessOS = "MacOS"

	applyClaudeCodeRuntimeHeaders(req, fingerprint, true)

	if got := req.Header["X-Stainless-OS"]; len(got) != 1 || got[0] != "MacOS" {
		t.Fatalf("X-Stainless-OS = %#v", got)
	}
	for header, want := range map[string]string{
		"X-Stainless-Lang":                          "js",
		"X-Stainless-Package-Version":               "0.94.0",
		"X-Stainless-Arch":                          "arm64",
		"X-Stainless-Runtime":                       "node",
		"X-Stainless-Runtime-Version":               "v24.3.0",
		"X-Stainless-Retry-Count":                   "0",
		"X-Stainless-Timeout":                       "600",
		"X-Stainless-Helper-Method":                 "stream",
		"Anthropic-Dangerous-Direct-Browser-Access": "true",
		"Accept": "application/json",
	} {
		if got := req.Header.Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if _, err := uuid.Parse(req.Header.Get(claudeCodeGatewayClientRequestHeader)); err != nil {
		t.Fatalf("X-Client-Request-Id is not a UUID: %v", err)
	}

	applyClaudeCodeRuntimeHeaders(req, fingerprint, false)
	if got := req.Header.Get("X-Stainless-Helper-Method"); got != "" {
		t.Fatalf("helper method should be cleared for non-stream requests, got %q", got)
	}
}

func TestApplyGatewayClientImpersonationPayloadUsesStableAccountIdentity(t *testing.T) {
	candidate := testRouteCandidateForSite("claude_code", "claude-sonnet-4-5")
	candidate.Site.ID = uuid.New()
	newPayload := func() map[string]any {
		return map[string]any{
			"model":    "claude-sonnet-4-5",
			"messages": []any{map[string]any{"role": "user", "content": "hello"}},
		}
	}

	first := newPayload()
	firstSession := applyGatewayClientImpersonationPayloadForAccount(first, nil, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), candidate, "account-uuid-1")
	second := newPayload()
	secondSession := applyGatewayClientImpersonationPayloadForAccount(second, nil, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), candidate, "account-uuid-1")

	if firstSession == "" || firstSession != secondSession {
		t.Fatalf("sessions = %q / %q, want a sticky shared session", firstSession, secondSession)
	}
	firstUserID := first["metadata"].(map[string]any)["user_id"]
	if firstUserID != second["metadata"].(map[string]any)["user_id"] {
		t.Fatal("same account must keep the same metadata.user_id")
	}
	if !strings.Contains(firstUserID.(string), "account-uuid-1") {
		t.Fatalf("user_id = %q, want account uuid", firstUserID)
	}

	otherSite := routeengine.Candidate{}
	otherSite.Site = candidate.Site
	otherSite.Site.SiteType = "newapi"
	third := newPayload()
	applyGatewayClientImpersonationPayloadForAccount(third, nil, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), otherSite, "ignored")
	if _, ok := third["metadata"]; ok {
		t.Fatalf("non claude_code site without the toggle must be untouched: %#v", third)
	}
}

func TestClaudeCodeFirstUserTextHandlesBlocksAndSkipsAssistant(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"messages": []any{
		map[string]any{"role": "assistant", "content": "ignored"},
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "image"},
			map[string]any{"type": "text", "text": "first text block"},
			map[string]any{"type": "text", "text": "second"},
		}},
	}}
	if got := claudeCodeFirstUserText(payload); got != "first text block" {
		t.Fatalf("first user text = %q", got)
	}
	if got := claudeCodeFirstUserText(map[string]any{}); got != "" {
		t.Fatalf("missing messages = %q, want empty", got)
	}
	// Only the first user message counts, even when it has no text.
	noText := map[string]any{"messages": []any{
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "image"}}},
		map[string]any{"role": "user", "content": "later"},
	}}
	if got := claudeCodeFirstUserText(noText); got != "" {
		t.Fatalf("first user without text = %q, want empty", got)
	}
}

func TestClaudeCodeSystemHasBillingBlockSupportsAllShapes(t *testing.T) {
	t.Parallel()

	billing := "x-anthropic-billing-header: cc_version=2.1.1.abc; cc_entrypoint=cli;"
	for name, system := range map[string]any{
		"string":      billing,
		"any blocks":  []any{map[string]any{"type": "text", "text": "other"}, map[string]any{"type": "text", "text": billing}},
		"typed slice": []map[string]any{{"type": "text", "text": billing}},
	} {
		if !claudeCodeSystemHasBillingBlock(system) {
			t.Fatalf("%s: billing block not detected", name)
		}
	}
	for name, system := range map[string]any{
		"nil":    nil,
		"plain":  "You are Claude Code",
		"blocks": []any{map[string]any{"type": "text", "text": "hello"}, "not a block"},
	} {
		if claudeCodeSystemHasBillingBlock(system) {
			t.Fatalf("%s: unexpected billing block", name)
		}
	}
}

func TestApplyGatewayClientImpersonationHeadersAddsRuntimeFingerprintForClaudeCodeSite(t *testing.T) {
	candidate := testRouteCandidateForSite("claude_code", "claude-sonnet-4-5")
	candidate.Site.ID = uuid.New()
	request := testGatewayRequest("claude-sonnet-4-5")
	request.Stream = true
	downstream := http.Header{}
	downstream.Set("User-Agent", "claude-cli/2.1.100 (external, cli)")
	downstream.Set("X-Stainless-Os", "MacOS")
	request.DownstreamHeaders = downstream

	req := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/messages")
	req.Header.Set("Accept", "text/event-stream")
	applyGatewayClientImpersonationHeaders(req, nil, anthropicMessagesProtocolAdapter{}, request, candidate, "")

	if got := req.Header["X-Stainless-OS"]; len(got) != 1 || got[0] != "MacOS" {
		t.Fatalf("X-Stainless-OS = %#v, want the identity adopted from the real client", got)
	}
	assertGatewayHeader(t, req, "Accept", "application/json")
	assertGatewayHeader(t, req, "X-Stainless-Helper-Method", "stream")
	assertGatewayHeader(t, req, "Anthropic-Dangerous-Direct-Browser-Access", "true")
	assertGatewayHeader(t, req, claudeCodeGatewayBetaHeader, claudeCodeGatewayAnthropicBeta)

	// A second request from a different downstream keeps the account's runtime.
	other := gatewayHeaderRequest(t, http.MethodPost, "https://example.test/v1/messages")
	applyGatewayClientImpersonationHeaders(other, nil, anthropicMessagesProtocolAdapter{}, testGatewayRequest("claude-sonnet-4-5"), candidate, "")
	if got := other.Header["X-Stainless-OS"]; len(got) != 1 || got[0] != "MacOS" {
		t.Fatalf("X-Stainless-OS = %#v, want a stable per-account runtime", got)
	}
	if other.Header.Get("X-Stainless-Helper-Method") != "" {
		t.Fatal("non-stream request must not advertise the stream helper")
	}
}

func TestClaudeCodeBetaHeaderListsFullMimicrySet(t *testing.T) {
	t.Parallel()

	for _, beta := range []string{
		"claude-code-20250219", "oauth-2025-04-20", "interleaved-thinking-2025-05-14",
		"prompt-caching-scope-2026-01-05", "effort-2025-11-24", "context-management-2025-06-27",
		"thinking-binding-controls-2026-08-01", "mid-conversation-output-config-2026-07-01",
		"extended-cache-ttl-2025-04-11",
	} {
		if !strings.Contains(claudeCodeGatewayAnthropicBeta, beta) {
			t.Fatalf("anthropic-beta %q is missing %s", claudeCodeGatewayAnthropicBeta, beta)
		}
	}
}

func TestSyncClaudeCodeBillingVersionRewritesVersionAndSuffix(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.100.abc; cc_entrypoint=cli;"},
			map[string]any{"type": "text", "text": "cc_version=1.0.0.zzz stays in ordinary text"},
		},
		"messages": []map[string]any{{"role": "user", "content": "hello world, this is a test"}},
	}

	syncClaudeCodeBillingVersion(payload, "2.1.258")

	system := payload["system"].([]any)
	want := claudeCodeBillingAttributionText("hello world, this is a test", "2.1.258")
	if got := system[0].(map[string]any)["text"]; got != want {
		t.Fatalf("billing text = %q, want %q", got, want)
	}
	if got := system[1].(map[string]any)["text"]; got != "cc_version=1.0.0.zzz stays in ordinary text" {
		t.Fatalf("non-billing block changed: %q", got)
	}
}

func TestSyncClaudeCodeBillingVersionKeepsSuffixlessBlockSuffixless(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"system":   "x-anthropic-billing-header: cc_version=2.1.100; cc_entrypoint=cli;",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	syncClaudeCodeBillingVersion(payload, "2.1.258")
	if got := payload["system"]; got != "x-anthropic-billing-header: cc_version=2.1.258; cc_entrypoint=cli;" {
		t.Fatalf("system = %q", got)
	}
}

func TestSyncClaudeCodeBillingVersionIgnoresRequestsWithoutBillingBlock(t *testing.T) {
	t.Parallel()

	payload := map[string]any{"system": "plain", "messages": []any{}}
	syncClaudeCodeBillingVersion(payload, "2.1.258")
	if payload["system"] != "plain" {
		t.Fatalf("system = %#v", payload["system"])
	}
	syncClaudeCodeBillingVersion(map[string]any{"system": claudeCodeSystemPromptPrefixes[0]}, "")
}

func TestApplyClaudeCodeMimicryPayloadSyncsRealClientBillingVersion(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"system":   []any{map[string]any{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.100.abc; cc_entrypoint=cli;"}},
		"messages": []any{map[string]any{"role": "user", "content": "hello world, this is a test"}},
		"metadata": map[string]any{"user_id": claudeCodeAccountMetadataUserID("d", "a", "s")},
	}
	applyClaudeCodeMimicryPayload(payload, claudeCodeMimicryOptions{SessionID: "s", ClientID: "d", Version: "2.1.258", RealClient: true})

	got := payload["system"].([]any)[0].(map[string]any)["text"]
	if want := claudeCodeBillingAttributionText("hello world, this is a test", "2.1.258"); got != want {
		t.Fatalf("billing text = %q, want %q", got, want)
	}
}
