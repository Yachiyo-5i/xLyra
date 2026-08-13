package gateway

import (
	"strings"
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestProtocolSpecRegistryLoadsCurrentFamilies(t *testing.T) {
	t.Parallel()

	config, err := loadProtocolSpecRegistry()
	if err != nil {
		t.Fatalf("loadProtocolSpecRegistry returned error: %v", err)
	}
	for _, protocol := range []string{
		"openai_chat",
		"openai_responses",
		"codex_responses",
		"anthropic_messages",
		"antigravity_gemini",
		"openai_images",
		"openai_embeddings",
	} {
		if _, ok := config.Protocols[protocol]; !ok {
			t.Fatalf("missing protocol spec %q", protocol)
		}
	}
	for _, provider := range []string{
		"openai",
		"opencode_go",
		"codex",
		"anthropic",
		"google_gemini",
		"zhipu",
		"glm_code",
		"minimax",
		"moonshot",
		"dashscope",
		"deepseek",
		"xiaomi_mimo",
	} {
		if _, ok := config.Providers[provider]; !ok {
			t.Fatalf("missing provider spec %q", provider)
		}
	}
	for _, modelPattern := range []string{
		"gpt-5*",
		"claude-sonnet-4*",
		"gemini-2.5*",
		"glm-4.5*",
		"minimax-m2*",
		"kimi-k2.5*",
		"qwen3*",
		"deepseek-reasoner",
		"mimo*",
	} {
		if _, ok := config.Models[modelPattern]; !ok {
			t.Fatalf("missing model spec %q", modelPattern)
		}
	}
}

func TestAnthropicMessagesProvidersDeclareMaxTokenDefaults(t *testing.T) {
	t.Parallel()

	config, err := loadProtocolSpecRegistry()
	if err != nil {
		t.Fatalf("loadProtocolSpecRegistry returned error: %v", err)
	}
	for provider, spec := range config.Providers {
		usesAnthropicMessages := normalizeSpecKey(spec.Protocol) == string(canonicalProtocolAnthropicMessages)
		for key, alt := range spec.AlternateProtocols {
			if normalizeSpecKey(key) == string(canonicalProtocolAnthropicMessages) || normalizeSpecKey(alt.Protocol) == string(canonicalProtocolAnthropicMessages) {
				usesAnthropicMessages = true
			}
		}
		if !usesAnthropicMessages {
			continue
		}
		value, ok := intFromAny(spec.RequestParams.Defaults["max_tokens"])
		if !ok || value <= 0 {
			t.Fatalf("provider %q uses Anthropic Messages but lacks request_params.defaults.max_tokens", provider)
		}
	}
}

func TestOpenCodeGoProtocolSpecsResolveIndependentPathsAndDefaults(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "opencode_go"},
		Model: routeengine.CandidateModel{UpstreamName: "minimax-m3"},
	}
	tests := []struct {
		protocol canonicalProtocol
		path     string
	}{
		{protocol: canonicalProtocolOpenAIChat, path: "https://opencode.ai/zen/go/v1/chat/completions"},
		{protocol: canonicalProtocolOpenAIResponses, path: "https://opencode.ai/zen/go/v1/responses"},
		{protocol: canonicalProtocolAnthropicMessages, path: "https://opencode.ai/zen/go/v1/messages"},
	}
	for _, tt := range tests {
		spec := effectiveProtocolSpec(tt.protocol, candidate)
		if got := upstreamPathFromSpec("", spec, "/fallback"); got != tt.path {
			t.Fatalf("protocol %q path = %q, want %q", tt.protocol, got, tt.path)
		}
	}

	chatPayload := applyRequestPolicyForCandidate(map[string]any{"model": "kimi-k3"}, canonicalProtocolOpenAIChat, candidate)
	if _, ok := chatPayload["max_tokens"]; ok {
		t.Fatalf("OpenCode Go chat payload received Anthropic-only default: %#v", chatPayload)
	}
	responsesPayload := applyRequestPolicyForCandidate(map[string]any{"model": "gpt-5.6-luna"}, canonicalProtocolOpenAIResponses, candidate)
	if _, ok := responsesPayload["max_tokens"]; ok {
		t.Fatalf("OpenCode Go Responses payload received Anthropic-only default: %#v", responsesPayload)
	}
	anthropicPayload := applyRequestPolicyForCandidate(map[string]any{"model": "minimax-m3"}, canonicalProtocolAnthropicMessages, candidate)
	if anthropicPayload["max_tokens"] != float64(8192) && anthropicPayload["max_tokens"] != 8192 {
		t.Fatalf("OpenCode Go Anthropic max_tokens = %#v, want 8192", anthropicPayload["max_tokens"])
	}
}

func TestEffectiveProtocolSpecMergesProtocolProviderAndModel(t *testing.T) {
	t.Parallel()

	spec := effectiveProtocolSpec(canonicalProtocolCodexResponses, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4-codex"},
	})
	if spec.Provider != "codex" {
		t.Fatalf("provider = %q, want codex", spec.Provider)
	}
	if !containsString(spec.RequestParams.Unsupported, "stream_options") {
		t.Fatalf("expected Codex unsupported stream_options, got %#v", spec.RequestParams.Unsupported)
	}
	if !containsString(spec.RequestParams.Unsupported, "prompt_cache_options") {
		t.Fatalf("expected Codex unsupported prompt_cache_options, got %#v", spec.RequestParams.Unsupported)
	}
	if got := spec.RequestParams.Forced["stream"]; got != true {
		t.Fatalf("forced stream = %#v, want true", got)
	}
	if got := spec.RequestParams.Forced["store"]; got != false {
		t.Fatalf("forced store = %#v, want false", got)
	}
	if got := spec.RequestParams.Defaults["max_tokens"]; got != nil {
		t.Fatalf("unexpected Codex max_tokens default = %#v", got)
	}
	if spec.Reasoning["encrypted_reasoning"] != "passthrough" {
		t.Fatalf("encrypted reasoning policy = %#v", spec.Reasoning)
	}
	if !modelCapability(canonicalProtocolOpenAIResponses, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-4.1"},
	}, "text_format") {
		t.Fatal("expected gpt-4.1 text_format capability from model spec")
	}
	mimoSpec := effectiveProtocolSpec(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "xiaomi_mimo"},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"},
	})
	if got := mimoSpec.RequestParams.Defaults["max_tokens"]; got != float64(8192) {
		t.Fatalf("MiMo max_tokens default = %#v, want 8192", got)
	}
	xlyraRelaySpec := effectiveProtocolSpec(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "xlyra"},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5"},
	})
	if xlyraRelaySpec.Provider != "xiaomi_mimo" {
		t.Fatalf("xLyra relay provider = %q, want xiaomi_mimo", xlyraRelaySpec.Provider)
	}
	if got := xlyraRelaySpec.RequestParams.Defaults["max_tokens"]; got != float64(8192) {
		t.Fatalf("xLyra relay MiMo max_tokens default = %#v, want 8192", got)
	}
}

func TestApplyRequestPolicyForCandidateRemovesUnsupportedAndForcesCodexFields(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"model":               "gpt-5.4-codex",
		"stream_options":      map[string]any{"include_usage": true},
		"temperature":         0.2,
		"metadata":            map[string]any{"trace": "x"},
		"parallel_tool_calls": false,
	}
	applyRequestPolicyForCandidate(payload, canonicalProtocolCodexResponses, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4-codex"},
	})
	for _, key := range []string{"stream_options", "temperature", "metadata"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("%s should be removed by policy, got %#v", key, payload[key])
		}
	}
	if payload["stream"] != true || payload["store"] != false || payload["parallel_tool_calls"] != true {
		t.Fatalf("forced Codex fields were not applied: %#v", payload)
	}
}

func TestApplyRequestPolicyForCandidateAppliesDefaultsOnlyWhenMissing(t *testing.T) {
	t.Parallel()

	missing := map[string]any{"model": "mimo-v2.5-pro"}
	applyRequestPolicyForCandidate(missing, canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "xiaomi_mimo"},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"},
	})
	if got := missing["max_tokens"]; got != float64(8192) {
		t.Fatalf("default max_tokens = %#v, want 8192", got)
	}

	explicit := map[string]any{"model": "mimo-v2.5-pro", "max_tokens": 64}
	applyRequestPolicyForCandidate(explicit, canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "xiaomi_mimo"},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"},
	})
	if got := explicit["max_tokens"]; got != 64 {
		t.Fatalf("explicit max_tokens = %#v, want 64", got)
	}

	xlyraRelay := map[string]any{"model": "mimo-v2.5"}
	applyRequestPolicyForCandidate(xlyraRelay, canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "xlyra"},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5"},
	})
	if got := xlyraRelay["max_tokens"]; got != float64(8192) {
		t.Fatalf("xLyra relay default max_tokens = %#v, want 8192", got)
	}
}

func TestApplyRequestPolicyForCodexConditionalRules(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"model":        "gpt-5.4-codex",
		"n":            2,
		"service_tier": "default",
		"tools": []any{map[string]any{
			"type":  "image_generation",
			"model": "gpt-image-2",
		}},
	}
	applyRequestPolicyForCandidate(payload, canonicalProtocolCodexResponses, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4-codex"},
	})
	if _, ok := payload["n"]; ok {
		t.Fatalf("n should be removed for Codex image_generation requests, got %#v", payload)
	}
	if _, ok := payload["service_tier"]; ok {
		t.Fatalf("unsupported service_tier should be removed, got %#v", payload)
	}

	priority := map[string]any{
		"model":        "gpt-5.4-codex",
		"service_tier": "priority",
	}
	applyRequestPolicyForCandidate(priority, canonicalProtocolCodexResponses, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4-codex"},
	})
	if priority["service_tier"] != "priority" {
		t.Fatalf("priority service_tier should be preserved, got %#v", priority)
	}

	fast := map[string]any{
		"model":        "gpt-5.4-codex",
		"service_tier": "fast",
	}
	applyRequestPolicyForCandidate(fast, canonicalProtocolCodexResponses, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4-codex"},
	})
	if fast["service_tier"] != "fast" {
		t.Fatalf("fast service_tier should be preserved, got %#v", fast)
	}
}

func TestProtocolParamMappingsUseStaticSpec(t *testing.T) {
	t.Parallel()

	mappings := protocolParamMappings(canonicalProtocolOpenAIResponses, routeengine.Candidate{})
	if len(mappings) == 0 {
		t.Fatal("expected OpenAI Responses param mappings")
	}
	if !hasParamMapping(mappings, "parallel_tool_calls", "parallel_tool_calls") {
		t.Fatalf("missing parallel_tool_calls mapping: %#v", mappings)
	}
}

func TestAlternateProtocolForOfficialDeepSeekAnthropicMessages(t *testing.T) {
	t.Parallel()

	alt, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "deepseek", BaseURL: "https://api.deepseek.com"},
		Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-pro"},
	})
	if !ok {
		t.Fatal("expected official DeepSeek v4 Anthropic alternate protocol")
	}
	if alt.BaseURL != "" || alt.BasePath != "/anthropic" || alt.Path != "/v1/messages" {
		t.Fatalf("unexpected alternate protocol: %#v", alt)
	}

	if _, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "newapi", BaseURL: "https://api.deepseek.com"},
		Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-pro"},
	}); ok {
		t.Fatal("third-party proxy site must not use official DeepSeek alternate protocol")
	}
}

func TestAlternateProtocolForOfficialMiMoAnthropicMessages(t *testing.T) {
	t.Parallel()

	alt, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "xiaomi_mimo", BaseURL: "https://token-plan-cn.xiaomimimo.com"},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"},
	})
	if !ok {
		t.Fatal("expected official MiMo Anthropic alternate protocol")
	}
	if alt.BaseURL != "" || alt.BasePath != "/anthropic" || alt.Path != "/v1/messages" {
		t.Fatalf("unexpected alternate protocol: %#v", alt)
	}

	if _, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "newapi", BaseURL: "https://token-plan-cn.xiaomimimo.com"},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"},
	}); ok {
		t.Fatal("third-party proxy site must not use official MiMo alternate protocol")
	}

	if _, ok := alternateProtocolForCandidate(canonicalProtocolAnthropicMessages, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "newapi", BaseURL: "https://api.minimaxi.com"},
		Model: routeengine.CandidateModel{UpstreamName: "minimax-m2"},
	}); ok {
		t.Fatal("third-party proxy site must not use official MiniMax alternate protocol")
	}
}

func TestApplyRequestPolicyForModelOverrides(t *testing.T) {
	t.Parallel()

	deepseek := map[string]any{
		"model":       "deepseek-reasoner",
		"messages":    []any{},
		"temperature": 0.1,
		"top_p":       0.9,
		"max_tokens":  128,
	}
	applyRequestPolicyForCandidate(deepseek, canonicalProtocolOpenAIChat, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "deepseek"},
		Model: routeengine.CandidateModel{UpstreamName: "deepseek-reasoner"},
	})
	if _, ok := deepseek["temperature"]; ok {
		t.Fatalf("deepseek-reasoner temperature should be removed, got %#v", deepseek)
	}
	if _, ok := deepseek["top_p"]; ok {
		t.Fatalf("deepseek-reasoner top_p should be removed, got %#v", deepseek)
	}
	if deepseek["max_tokens"] != 128 {
		t.Fatalf("max_tokens should be preserved, got %#v", deepseek)
	}

	kimi := map[string]any{
		"model":       "kimi-k2.5-preview",
		"messages":    []any{},
		"temperature": 0.2,
		"top_p":       0.7,
	}
	applyRequestPolicyForCandidate(kimi, canonicalProtocolOpenAIChat, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "moonshot"},
		Model: routeengine.CandidateModel{UpstreamName: "kimi-k2.5-preview"},
	})
	if kimi["temperature"] != 0.6 || kimi["top_p"] != float64(1) {
		t.Fatalf("Kimi fixed params were not applied, got %#v", kimi)
	}
}

func TestThinkingRoundtripCapabilitiesAreDeclaredForReasoningModels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		model    string
		provider string
	}{
		{name: "deepseek", model: "deepseek-v4-pro", provider: "deepseek"},
		{name: "mimo", model: "mimo-v2.5-pro", provider: "xiaomi_mimo"},
		{name: "minimax", model: "minimax-m2", provider: "minimax"},
		{name: "kimi-thinking", model: "kimi-k2-thinking", provider: "moonshot"},
		{name: "kimi-k26", model: "kimi-k2.6", provider: "moonshot"},
		{name: "kimi-code", model: "kimi-k2.5-code", provider: "kimi_code"},
		{name: "kimi-k3-official", model: "k3", provider: "kimi_code"},
		{name: "kimi-k3-compatible", model: "kimi-k3-preview", provider: "kimi_code"},
		{name: "kimi-for-coding", model: "kimi-for-coding-highspeed", provider: "kimi_code"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := effectiveProtocolSpec(canonicalProtocolAnthropicMessages, routeengine.Candidate{
				Site:  routeengine.CandidateSite{SiteType: tc.provider},
				Model: routeengine.CandidateModel{UpstreamName: tc.model},
			})
			if !spec.Capabilities["thinking_roundtrip"] {
				t.Fatalf("thinking_roundtrip missing for %s: %#v", tc.model, spec.Capabilities)
			}
		})
	}
}

func TestProtocolEventMappingUsesStaticSpec(t *testing.T) {
	t.Parallel()

	responsesName := protocolEventName(canonicalProtocolOpenAIResponses, canonicalStreamEventTextDelta, "fallback", routeengine.Candidate{})
	if responsesName != "response.output_text.delta" {
		t.Fatalf("Responses text event = %q", responsesName)
	}
	messagesMapping, ok := protocolEventMapping(canonicalProtocolAnthropicMessages, canonicalStreamEventToolCallDelta, routeengine.Candidate{})
	if !ok {
		t.Fatal("missing Anthropic tool call delta mapping")
	}
	if messagesMapping.Name != "content_block_delta" || messagesMapping.Condition["delta.type"] != "input_json_delta" {
		t.Fatalf("unexpected Anthropic mapping: %#v", messagesMapping)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func hasParamMapping(items []paramMapping, canonical string, upstream string) bool {
	for _, item := range items {
		if item.CanonicalKey == canonical && item.UpstreamKey == upstream {
			return true
		}
	}
	return false
}

func TestValidateRequestParamPolicyConflicts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		policy        requestParamPolicy
		wantConflicts int
	}{
		{
			name: "no conflicts",
			policy: requestParamPolicy{
				Defaults: map[string]any{"temperature": 0.7},
				Fixed:    map[string]any{"top_p": 1.0},
				Forced:   map[string]any{"stream": true},
			},
			wantConflicts: 0,
		},
		{
			name: "defaults and fixed conflict",
			policy: requestParamPolicy{
				Defaults: map[string]any{"temperature": 0.7},
				Fixed:    map[string]any{"temperature": 1.0},
			},
			wantConflicts: 1,
		},
		{
			name: "defaults and forced conflict",
			policy: requestParamPolicy{
				Defaults: map[string]any{"stream": false},
				Forced:   map[string]any{"stream": true},
			},
			wantConflicts: 1,
		},
		{
			name: "fixed and forced conflict",
			policy: requestParamPolicy{
				Fixed:  map[string]any{"temperature": 1.0},
				Forced: map[string]any{"temperature": 0.5},
			},
			wantConflicts: 1,
		},
		{
			name: "all three conflict on same key",
			policy: requestParamPolicy{
				Defaults: map[string]any{"temperature": 0.7},
				Fixed:    map[string]any{"temperature": 1.0},
				Forced:   map[string]any{"temperature": 0.5},
			},
			wantConflicts: 3,
		},
		{
			name: "multiple keys with mixed conflicts",
			policy: requestParamPolicy{
				Defaults: map[string]any{"temperature": 0.7, "top_p": 0.9},
				Fixed:    map[string]any{"temperature": 1.0},
				Forced:   map[string]any{"top_p": 1.0},
			},
			wantConflicts: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conflicts := validateRequestParamPolicyConflicts(tt.policy)
			if len(conflicts) != tt.wantConflicts {
				t.Errorf("got %d conflicts %v, want %d", len(conflicts), conflicts, tt.wantConflicts)
			}
		})
	}
}

func TestValidateProtocolSpecRegistry_CurrentSpecsHaveNoConflicts(t *testing.T) {
	t.Parallel()

	config, err := loadProtocolSpecRegistry()
	if err != nil {
		t.Fatalf("loadProtocolSpecRegistry returned error: %v", err)
	}
	if err := validateProtocolSpecRegistry(config); err != nil {
		t.Fatalf("current protocol_specs.json has conflicts: %v", err)
	}
}

func TestValidateProtocolSpecRegistry_DetectsConflicts(t *testing.T) {
	t.Parallel()

	config := protocolSpecRegistryConfig{
		Protocols: map[string]protocolSpecDefinition{
			"test_protocol": {
				RequestParams: requestParamPolicy{
					Defaults: map[string]any{"temperature": 0.7},
					Forced:   map[string]any{"temperature": 0.5},
				},
			},
		},
		Providers: map[string]providerSpecDefinition{},
		Models:    map[string]modelSpecDefinition{},
	}

	err := validateProtocolSpecRegistry(config)
	if err == nil {
		t.Fatal("expected validation error for conflicting policy")
	}
	if !containsString([]string{err.Error()}, err.Error()) {
		t.Fatalf("unexpected error format: %v", err)
	}
}

func TestValidatePayloadParams(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-4.1"},
	}

	tests := []struct {
		name         string
		payload      map[string]any
		protocolName string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "valid temperature",
			payload:      map[string]any{"temperature": 0.7},
			protocolName: "openai_chat",
			wantErr:      false,
		},
		{
			name:         "temperature at max boundary",
			payload:      map[string]any{"temperature": 2.0},
			protocolName: "openai_chat",
			wantErr:      false,
		},
		{
			name:         "temperature above max",
			payload:      map[string]any{"temperature": 2.5},
			protocolName: "openai_chat",
			wantErr:      true,
			errContains:  "temperature",
		},
		{
			name:         "temperature below min",
			payload:      map[string]any{"temperature": -0.1},
			protocolName: "openai_chat",
			wantErr:      true,
			errContains:  "temperature",
		},
		{
			name:         "valid top_p",
			payload:      map[string]any{"top_p": 0.95},
			protocolName: "openai_chat",
			wantErr:      false,
		},
		{
			name:         "top_p above max",
			payload:      map[string]any{"top_p": 1.5},
			protocolName: "openai_chat",
			wantErr:      true,
			errContains:  "top_p",
		},
		{
			name:         "max_tokens valid",
			payload:      map[string]any{"max_tokens": float64(100)},
			protocolName: "openai_chat",
			wantErr:      false,
		},
		{
			name:         "max_tokens zero",
			payload:      map[string]any{"max_tokens": float64(0)},
			protocolName: "openai_chat",
			wantErr:      true,
			errContains:  "max_tokens",
		},
		{
			name:         "nil payload",
			payload:      nil,
			protocolName: "openai_chat",
			wantErr:      false,
		},
		{
			name:         "non-numeric temperature ignored",
			payload:      map[string]any{"temperature": "high"},
			protocolName: "openai_chat",
			wantErr:      false,
		},
		{
			name:         "absent key not validated",
			payload:      map[string]any{"model": "gpt-4.1"},
			protocolName: "openai_chat",
			wantErr:      false,
		},
		{
			name:         "anthropic temperature above 1",
			payload:      map[string]any{"temperature": 1.5},
			protocolName: "anthropic_messages",
			wantErr:      true,
			errContains:  "temperature",
		},
		{
			name:         "anthropic temperature at 1",
			payload:      map[string]any{"temperature": 1.0},
			protocolName: "anthropic_messages",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validatePayloadParams(tt.payload, tt.protocolName, candidate)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.errContains)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidatePayloadParams_ResponsesProtocol(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-4.1"},
	}

	err := validatePayloadParams(map[string]any{"max_output_tokens": float64(0)}, "openai_responses", candidate)
	if err == nil {
		t.Fatal("expected error for max_output_tokens=0")
	}

	err = validatePayloadParams(map[string]any{"max_output_tokens": float64(4096)}, "openai_responses", candidate)
	if err != nil {
		t.Fatalf("unexpected error for valid max_output_tokens: %v", err)
	}
}

func TestOfficialBaseURLForProviderNormalizesProviderKey(t *testing.T) {
	t.Parallel()

	if got := OfficialBaseURLForProvider(" OpenAI "); got != "https://api.openai.com" {
		t.Fatalf("OpenAI official base URL = %q, want OpenAI API", got)
	}
	if got := OfficialBaseURLForProvider("missing-provider"); got != "" {
		t.Fatalf("missing provider official base URL = %q, want empty", got)
	}
}
