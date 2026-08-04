package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

func TestResponsesProtocolHelperEdgeCases(t *testing.T) {
	t.Parallel()

	if streamIncludeUsage(nil) {
		t.Fatal("nil payload should not include usage")
	}
	if streamIncludeUsage(map[string]any{"stream_options": map[string]any{"include_usage": "true"}}) {
		t.Fatal("non-bool include_usage should not include usage")
	}
	if !streamIncludeUsage(map[string]any{"stream_options": map[string]any{"include_usage": true}}) {
		t.Fatal("bool include_usage should include usage")
	}

	var event responsesStreamEvent
	if err := json.Unmarshal([]byte(`{"type":"response.function_call_arguments.delta","arguments":"{\"city\":\"Tokyo\"}"}`), &event); err != nil {
		t.Fatalf("unmarshal string arguments: %v", err)
	}
	if string(event.Arguments) != `{"city":"Tokyo"}` {
		t.Fatalf("string arguments = %q, want decoded JSON string", event.Arguments)
	}
	if err := json.Unmarshal([]byte(`{"type":"response.function_call_arguments.delta","arguments":{"city":"Tokyo"}}`), &event); err != nil {
		t.Fatalf("unmarshal object arguments: %v", err)
	}
	if string(event.Arguments) != `{"city":"Tokyo"}` {
		t.Fatalf("object arguments = %q, want raw JSON object text", event.Arguments)
	}
}

func TestCompletionUsageFromResponsesUsageFallbacksAndDetails(t *testing.T) {
	t.Parallel()

	got := completionUsageFromResponsesUsage(&responsesUsage{
		InputTokens:  11,
		OutputTokens: 7,
		InputTokensDetails: &responsesInputTokenDetail{
			CachedTokens:     4,
			CacheWriteTokens: 5,
			ImageTokens:      3,
		},
		CompletionTokenStats: responsesOutputTokenDetail{ReasoningTokens: 2},
	})
	if got.PromptTokens != 11 || got.CompletionTokens != 7 || got.TotalTokens != 18 {
		t.Fatalf("usage totals = %#v, want input/output fallback plus total", got)
	}
	if got.CachedPromptTokens != 4 || got.InputImageTokens != 3 || got.ReasoningTokens != 2 {
		t.Fatalf("usage details = %#v, want cached/image/reasoning details", got)
	}
	if got.CacheWriteTokens != 5 {
		t.Fatalf("cache write tokens = %d, want input_tokens_details.cache_write_tokens", got.CacheWriteTokens)
	}

	invalid := parseResponsesUsage([]byte(`{"usage":`))
	if invalid != (completionUsage{}) {
		t.Fatalf("invalid JSON usage = %#v, want empty usage", invalid)
	}

	parsed := parseResponsesUsage([]byte(`{"usage":{"prompt_tokens":5,"completion_tokens":6,"total_tokens":11}}`))
	if parsed.PromptTokens != 5 || parsed.CompletionTokens != 6 || parsed.TotalTokens != 11 {
		t.Fatalf("parsed usage = %#v, want prompt/completion/total tokens", parsed)
	}
}

func TestShouldUseOpenAIResponsesForResponsesOnlyModel(t *testing.T) {
	t.Parallel()

	useResponses := shouldUseOpenAIResponses(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5-codex"},
	}, []string{"openai-response"})
	if !useResponses {
		t.Fatal("expected responses-only model to use /v1/responses")
	}
}

func TestShouldUseOpenAIResponsesForCodexModelWhenDualStack(t *testing.T) {
	t.Parallel()

	useResponses := shouldUseOpenAIResponses(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5-codex"},
	}, []string{"openai", "openai-response"})
	if !useResponses {
		t.Fatal("expected codex model to prefer /v1/responses")
	}
}

func TestOpenAIResolverPureProtocolBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		request   gatewayRequest
		candidate routeengine.Candidate
		wantName  string
	}{
		{
			name:     "embeddings endpoint",
			request:  gatewayRequest{DownstreamPath: gatewayEndpointEmbeddings, Payload: map[string]any{"model": "text-embedding-3-large", "input": "hi"}},
			wantName: "openai_embeddings",
		},
		{
			name:     "google image endpoint",
			request:  gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations, Payload: map[string]any{"model": "gemini-image", "prompt": "draw"}},
			wantName: "google_generate_content",
			candidate: routeengine.Candidate{
				Site: routeengine.CandidateSite{SiteType: "google_gemini"},
			},
		},
		{
			name:     "antigravity image endpoint",
			request:  gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations, Payload: map[string]any{"model": "gemini-image", "prompt": "draw"}},
			wantName: "antigravity_image_generate_content",
			candidate: routeengine.Candidate{
				Site: routeengine.CandidateSite{SiteType: "antigravity"},
			},
		},
		{
			name:     "codex chat endpoint",
			request:  gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Payload: map[string]any{"model": "gpt-5-codex", "messages": []any{}}},
			wantName: "codex_responses",
			candidate: routeengine.Candidate{
				Site: routeengine.CandidateSite{SiteType: "codex"},
			},
		},
		{
			name:     "anthropic messages endpoint",
			request:  gatewayRequest{DownstreamPath: gatewayEndpointMessages, Payload: map[string]any{"model": "claude", "messages": []any{}}},
			wantName: "anthropic_messages",
			candidate: routeengine.Candidate{
				Site: routeengine.CandidateSite{SiteType: "anthropic"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), tt.request, tt.candidate)
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			if protocol.ProtocolName() != tt.wantName {
				t.Fatalf("protocol = %q, want %q", protocol.ProtocolName(), tt.wantName)
			}
		})
	}
}

func TestOpenAIResolverHelpersWithoutDatabase(t *testing.T) {
	t.Parallel()

	types, err := (openAIProtocolResolver{}).supportedEndpointTypes(t.Context(), uuid.New())
	if err != nil {
		t.Fatalf("supportedEndpointTypes returned error: %v", err)
	}
	if len(types) != 0 {
		t.Fatalf("supportedEndpointTypes without db = %#v, want empty", types)
	}

	if got := providerNameForCandidate(routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: " custom_openai "}}); got != "custom_openai" {
		t.Fatalf("providerNameForCandidate fallback = %q, want custom_openai", got)
	}
	if got := providerNameForCandidate(routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "zhipu"}}); got != "zhipu" {
		t.Fatalf("providerNameForCandidate zhipu = %q, want zhipu", got)
	}
	if !containsEndpointType([]string{" OpenAI-Response "}, "openai-response") {
		t.Fatal("expected endpoint type match to ignore case and surrounding whitespace")
	}
	if containsEndpointType([]string{"openai"}, "openai-response") {
		t.Fatal("expected different endpoint type not to match")
	}
	if shouldUseOpenAIResponses(gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.1"}}, []string{"openai", "openai-response"}) {
		t.Fatal("dual-stack non-Codex chat requests should stay on chat completions")
	}
	if !shouldUseOpenAIResponses(gatewayRequest{DownstreamPath: gatewayEndpointMessages}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.1"}}, []string{"openai", "openai-response"}) {
		t.Fatal("messages downstream should prefer responses when available")
	}
}

func TestOpenAIResolverUsesChatAdapterForDownstreamResponsesWhenUpstreamResponsesUnsupported(t *testing.T) {
	t.Parallel()

	protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        map[string]any{"model": "gpt-5.4", "input": "hi"},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "upstream-gpt"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if protocol.ProtocolName() != "openai_chat_completions_to_responses" {
		t.Fatalf("protocol = %q, want openai_chat_completions_to_responses", protocol.ProtocolName())
	}
}

func TestOpenAIResolverUsesOfficialDeepSeekAnthropicForMessages(t *testing.T) {
	t.Parallel()

	protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{
		DownstreamPath: gatewayEndpointMessages,
		Payload:        map[string]any{"model": "deepseek-v4-pro", "messages": []any{}},
	}, routeengine.Candidate{
		Site: routeengine.CandidateSite{
			SiteType: "deepseek",
			BaseURL:  "https://api.deepseek.com",
		},
		Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-pro"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if protocol.ProtocolName() != "deepseek_anthropic_messages" {
		t.Fatalf("protocol = %q, want deepseek_anthropic_messages", protocol.ProtocolName())
	}
	if got := protocol.UpstreamPath("https://api.deepseek.com"); got != "https://api.deepseek.com/anthropic/v1/messages" {
		t.Fatalf("UpstreamPath = %q", got)
	}
}

func TestAnthropicEndpointTypeUsesProviderSpecPathForResponsesConversion(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        map[string]any{"model": "gpt-5.4", "input": "hi", "stream": true},
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Stream:         true,
			Messages: []canonicalMessage{{
				Type:    "message",
				Role:    "user",
				Content: []canonicalContentPart{{Type: "input_text", Text: "hi"}},
			}},
			Params: map[string]any{"max_output_tokens": 64},
		},
	}
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			SiteType: "xiaomi_mimo",
			BaseURL:  "https://token-plan-cn.xiaomimimo.com",
		},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"},
	}

	protocol := anthropicMessagesProtocolForCandidate(request, candidate)
	if protocol.ProtocolName() != "xiaomi_mimo_anthropic_messages_to_responses" {
		t.Fatalf("protocol = %q, want xiaomi_mimo_anthropic_messages_to_responses", protocol.ProtocolName())
	}
	if got := protocol.UpstreamPath(candidate.Site.BaseURL); got != "https://token-plan-cn.xiaomimimo.com/anthropic/v1/messages" {
		t.Fatalf("UpstreamPath = %q", got)
	}
	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "mimo-v2.5-pro" || payload["max_tokens"] != 64 {
		t.Fatalf("unexpected Anthropic payload: %#v", payload)
	}
	if _, ok := payload["messages"].([]any); !ok {
		t.Fatalf("payload was not converted to Anthropic messages: %#v", payload)
	}
}

func TestGLMProviderSpecsUseConfiguredOpenAIChatPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		siteType string
		baseURL  string
		want     string
	}{
		{
			name:     "zhipu general",
			siteType: "zhipu",
			baseURL:  "https://open.bigmodel.cn/api/paas/v4",
			want:     "https://open.bigmodel.cn/api/paas/v4/chat/completions",
		},
		{
			name:     "glm coding",
			siteType: "glm_code",
			baseURL:  "https://open.bigmodel.cn/api/coding/paas/v4",
			want:     "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{
				DownstreamPath: gatewayEndpointChatCompletions,
				Payload:        map[string]any{"model": "glm-5.1", "messages": []any{}},
			}, routeengine.Candidate{
				Site: routeengine.CandidateSite{
					SiteType: tt.siteType,
					BaseURL:  tt.baseURL,
				},
				Model: routeengine.CandidateModel{UpstreamName: "glm-5.1"},
			})
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			if got := protocol.UpstreamPath(tt.baseURL); got != tt.want {
				t.Fatalf("UpstreamPath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestZhipuProviderSpecUsesConfiguredOpenAIEmbeddingAndImagePaths(t *testing.T) {
	t.Parallel()

	baseURL := "https://open.bigmodel.cn/api/paas/v4"
	tests := []struct {
		name       string
		path       string
		payload    map[string]any
		model      string
		wantName   string
		wantUpPath string
	}{
		{
			name:       "embeddings",
			path:       gatewayEndpointEmbeddings,
			payload:    map[string]any{"model": "embedding-3", "input": "hello"},
			model:      "embedding-3",
			wantName:   "openai_embeddings",
			wantUpPath: "https://open.bigmodel.cn/api/paas/v4/embeddings",
		},
		{
			name:       "images",
			path:       gatewayEndpointImagesGenerations,
			payload:    map[string]any{"model": "glm-image", "prompt": "draw a cat"},
			model:      "glm-image",
			wantName:   "openai_images_generations",
			wantUpPath: "https://open.bigmodel.cn/api/paas/v4/images/generations",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{
				DownstreamPath: tt.path,
				Payload:        tt.payload,
			}, routeengine.Candidate{
				Site: routeengine.CandidateSite{
					SiteType: "zhipu",
					BaseURL:  baseURL,
				},
				Model: routeengine.CandidateModel{UpstreamName: tt.model},
			})
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			if protocol.ProtocolName() != tt.wantName {
				t.Fatalf("protocol = %q, want %q", protocol.ProtocolName(), tt.wantName)
			}
			if got := protocol.UpstreamPath(baseURL); got != tt.wantUpPath {
				t.Fatalf("UpstreamPath = %q, want %q", got, tt.wantUpPath)
			}
		})
	}
}

func TestGLMProviderSpecsUseConfiguredAnthropicPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		siteType string
		baseURL  string
		wantName string
	}{
		{
			name:     "zhipu general",
			siteType: "zhipu",
			baseURL:  "https://open.bigmodel.cn/api/paas/v4",
			wantName: "zhipu_anthropic_messages",
		},
		{
			name:     "glm coding",
			siteType: "glm_code",
			baseURL:  "https://open.bigmodel.cn/api/coding/paas/v4",
			wantName: "glm_code_anthropic_messages",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := gatewayRequest{
				DownstreamPath: gatewayEndpointMessages,
				Payload:        map[string]any{"model": "glm-5.1", "messages": []any{}},
			}
			candidate := routeengine.Candidate{
				Site: routeengine.CandidateSite{
					SiteType: tt.siteType,
					BaseURL:  tt.baseURL,
				},
				Model: routeengine.CandidateModel{UpstreamName: "glm-5.1"},
			}

			protocol := anthropicMessagesProtocolForCandidate(request, candidate)
			if protocol.ProtocolName() != tt.wantName {
				t.Fatalf("protocol = %q, want %q", protocol.ProtocolName(), tt.wantName)
			}
			if got := protocol.UpstreamPath(candidate.Site.BaseURL); got != "https://open.bigmodel.cn/api/anthropic/v1/messages" {
				t.Fatalf("UpstreamPath = %q", got)
			}
		})
	}
}

func TestAnthropicEndpointTypeUsesMiMoCustomRegionBaseURL(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointMessages,
		Payload:        map[string]any{"model": "mimo-v2.5-pro", "messages": []any{}},
	}
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			SiteType: "xiaomi_mimo",
			BaseURL:  "https://token-plan-sgp.xiaomimimo.com",
		},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"},
	}

	protocol := anthropicMessagesProtocolForCandidate(request, candidate)
	if protocol.ProtocolName() != "xiaomi_mimo_anthropic_messages" {
		t.Fatalf("protocol = %q, want xiaomi_mimo_anthropic_messages", protocol.ProtocolName())
	}
	if got := protocol.UpstreamPath(candidate.Site.BaseURL); got != "https://token-plan-sgp.xiaomimimo.com/anthropic/v1/messages" {
		t.Fatalf("UpstreamPath = %q", got)
	}
}

func TestProviderAnthropicPayloadUsesConfiguredMaxTokensDefault(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        map[string]any{"model": "gpt-5.4", "input": "hi", "stream": true},
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Stream:         true,
			Messages: []canonicalMessage{{
				Type:    "message",
				Role:    "user",
				Content: []canonicalContentPart{{Type: "input_text", Text: "hi"}},
			}},
			Params: map[string]any{},
		},
	}
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			SiteType: "xiaomi_mimo",
			BaseURL:  "https://token-plan-cn.xiaomimimo.com",
		},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"},
	}

	protocol := anthropicMessagesProtocolForCandidate(request, candidate)
	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if got := payload["max_tokens"]; got != 8192 {
		t.Fatalf("max_tokens = %#v, want configured default 8192", got)
	}
}

func TestProviderAnthropicResponsesBridgesCustomToolsForChineseProviders(t *testing.T) {
	t.Parallel()

	providers := []string{"zhipu", "glm_code", "minimax", "deepseek", "xiaomi_mimo", "kimi_code"}
	for _, provider := range providers {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			canonical, err := canonicalRequestFromOpenAIResponsesPayload(map[string]any{
				"model":             "provider-model",
				"input":             "update the file",
				"max_output_tokens": 64,
				"tools": []any{
					map[string]any{"type": "custom", "name": "apply_patch", "description": "Apply a patch"},
					map[string]any{"type": "function", "name": "lookup", "description": "Look up data", "parameters": map[string]any{"type": "object"}},
				},
				"tool_choice": map[string]any{"type": "custom", "name": "apply_patch"},
			}, "provider-model")
			if err != nil {
				t.Fatalf("canonicalRequestFromOpenAIResponsesPayload returned error: %v", err)
			}
			request := gatewayRequest{DownstreamPath: gatewayEndpointResponses, Canonical: &canonical}
			candidate := routeengine.Candidate{
				Site:  routeengine.CandidateSite{SiteType: provider},
				Model: routeengine.CandidateModel{UpstreamName: "provider-model"},
			}
			protocol := newProviderAnthropicMessagesProtocolAdapter(provider, alternateProtocolDefinition{}, canonicalProtocolOpenAIResponses)
			payload, err := protocol.BuildUpstreamPayload(request, candidate)
			if err != nil {
				t.Fatalf("BuildUpstreamPayload returned error: %v", err)
			}
			tools, ok := payload["tools"].([]any)
			if !ok || len(tools) != 2 {
				t.Fatalf("tools = %#v, want custom and function tools", payload["tools"])
			}
			customTool := tools[0].(map[string]any)
			schema := customTool["input_schema"].(map[string]any)
			properties := schema["properties"].(map[string]any)
			inputSchema := properties["input"].(map[string]any)
			if customTool["name"] != "apply_patch" || inputSchema["type"] != "string" {
				t.Fatalf("custom tool was not wrapped for Messages: %#v", customTool)
			}
			choice, _ := payload["tool_choice"].(map[string]any)
			if choice["type"] != "tool" || choice["name"] != "apply_patch" {
				t.Fatalf("custom tool_choice was not converted: %#v", payload["tool_choice"])
			}

			body := []byte(`{"id":"msg_custom","model":"provider-model","role":"assistant","content":[{"type":"tool_use","id":"call_custom","name":"apply_patch","input":{"input":"*** Begin Patch"}},{"type":"tool_use","id":"call_function","name":"lookup","input":{"q":"x"}}],"stop_reason":"tool_use","usage":{"input_tokens":4,"output_tokens":2}}`)
			transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
			if err != nil {
				t.Fatalf("TransformBufferedResponse returned error: %v", err)
			}
			var response map[string]any
			if err := json.Unmarshal(transformed.Body, &response); err != nil {
				t.Fatalf("decode transformed response: %v", err)
			}
			output := response["output"].([]any)
			customCall := output[0].(map[string]any)
			functionCall := output[1].(map[string]any)
			if customCall["type"] != "custom_tool_call" || customCall["input"] != "*** Begin Patch" {
				t.Fatalf("custom response call was not restored: %#v", customCall)
			}
			if !strings.HasPrefix(anyString(customCall["id"]), "ctc_") {
				t.Fatalf("custom response call ID = %#v, want ctc_ prefix", customCall["id"])
			}
			if _, exists := customCall["arguments"]; exists {
				t.Fatalf("custom response call retained arguments: %#v", customCall)
			}
			if functionCall["type"] != "function_call" || functionCall["arguments"] != `{"q":"x"}` {
				t.Fatalf("ordinary function call changed: %#v", functionCall)
			}
		})
	}
}

func TestProviderAnthropicResponsesBridgesResponsesLiteTools(t *testing.T) {
	t.Parallel()

	canonical, err := canonicalRequestFromOpenAIResponsesPayload(map[string]any{
		"model":             "gpt-5.6-sol",
		"max_output_tokens": 64,
		"tool_choice":       "auto",
		"input": []any{
			map[string]any{
				"type": "additional_tools",
				"role": "developer",
				"tools": []any{
					map[string]any{"type": "custom", "name": "exec", "description": "Run tool calls"},
					map[string]any{"type": "function", "name": "wait", "description": "Wait for a cell", "parameters": map[string]any{"type": "object"}},
					map[string]any{"type": "namespace", "name": "collaboration"},
				},
			},
			map[string]any{
				"type": "message",
				"role": "developer",
				"content": []any{
					map[string]any{"type": "input_text", "text": "You are Codex."},
				},
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "Inspect the repository."},
				},
			},
		},
	}, "gpt-5.6-sol")
	if err != nil {
		t.Fatalf("canonicalRequestFromOpenAIResponsesPayload returned error: %v", err)
	}
	if canonical.Instructions != "You are Codex." {
		t.Fatalf("instructions = %q, want Responses Lite developer instructions", canonical.Instructions)
	}
	if len(canonical.Tools) != 3 {
		t.Fatalf("canonical tools = %#v, want all additional tools", canonical.Tools)
	}
	if len(canonical.Messages) != 1 || canonical.Messages[0].Role != "user" {
		t.Fatalf("canonical messages = %#v, want only the user message", canonical.Messages)
	}

	request := gatewayRequest{DownstreamPath: gatewayEndpointResponses, Canonical: &canonical}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "minimax"},
		Model: routeengine.CandidateModel{UpstreamName: "MiniMax-M3"},
	}
	protocol := newProviderAnthropicMessagesProtocolAdapter("minimax", alternateProtocolDefinition{}, canonicalProtocolOpenAIResponses)
	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["system"] != "You are Codex." {
		t.Fatalf("system = %#v, want Responses Lite developer instructions", payload["system"])
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %#v, want exec and wait", payload["tools"])
	}
	if tools[0].(map[string]any)["name"] != "exec" || tools[1].(map[string]any)["name"] != "wait" {
		t.Fatalf("unexpected Responses Lite tools: %#v", tools)
	}
	choice, _ := payload["tool_choice"].(map[string]any)
	if choice["type"] != "auto" {
		t.Fatalf("tool_choice = %#v, want auto", payload["tool_choice"])
	}

	body := []byte(`{"id":"msg_exec","model":"MiniMax-M3","role":"assistant","content":[{"type":"tool_use","id":"call_exec","name":"exec","input":{"input":"await tools.exec_command({cmd: \"git log -1\"})"}}],"stop_reason":"tool_use","usage":{"input_tokens":4,"output_tokens":2}}`)
	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(transformed.Body, &response); err != nil {
		t.Fatalf("decode transformed response: %v", err)
	}
	call := response["output"].([]any)[0].(map[string]any)
	if call["type"] != "custom_tool_call" || call["name"] != "exec" {
		t.Fatalf("Responses Lite exec call = %#v, want custom_tool_call", call)
	}
}

func TestAnthropicMessagesOmitsToolChoiceWithoutConvertibleTools(t *testing.T) {
	t.Parallel()

	payload, err := encodeCanonicalRequestToAnthropicMessages(canonicalRequest{
		Messages: []canonicalMessage{{
			Type:    "message",
			Role:    "user",
			Content: []canonicalContentPart{{Type: "input_text", Text: "Hello"}},
		}},
		Tools:      []canonicalTool{{Type: "namespace", Name: "collaboration"}},
		ToolChoice: "auto",
		Params:     map[string]any{"max_output_tokens": 64},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "MiniMax-M3"}})
	if err != nil {
		t.Fatalf("encodeCanonicalRequestToAnthropicMessages returned error: %v", err)
	}
	if _, ok := payload["tools"]; ok {
		t.Fatalf("tools should be omitted when none can be converted: %#v", payload["tools"])
	}
	if _, ok := payload["tool_choice"]; ok {
		t.Fatalf("tool_choice should be omitted without tools: %#v", payload["tool_choice"])
	}
}

func TestResponsesCustomToolHistoryConvertsToAnthropicRoundTrip(t *testing.T) {
	t.Parallel()

	messages := canonicalMessagesFromResponsesInput([]any{
		map[string]any{"type": "custom_tool_call", "id": "ctc_1", "call_id": "call_1", "name": "apply_patch", "input": "*** Begin Patch"},
		map[string]any{"type": "custom_tool_call_output", "call_id": "call_1", "output": "Done!"},
	})
	payload, err := encodeCanonicalRequestToAnthropicMessages(canonicalRequest{
		SourceProtocol: canonicalProtocolOpenAIResponses,
		Messages:       messages,
		Params:         map[string]any{"max_output_tokens": 64},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "provider-model"}})
	if err != nil {
		t.Fatalf("encodeCanonicalRequestToAnthropicMessages returned error: %v", err)
	}
	encodedMessages := payload["messages"].([]any)
	if len(encodedMessages) != 2 {
		t.Fatalf("messages = %#v, want assistant tool_use and user tool_result", encodedMessages)
	}
	toolUse := encodedMessages[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	toolInput := toolUse["input"].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["id"] != "call_1" || toolInput["input"] != "*** Begin Patch" {
		t.Fatalf("unexpected tool_use history: %#v", toolUse)
	}
	toolResult := encodedMessages[1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "call_1" || toolResult["content"] != "Done!" {
		t.Fatalf("unexpected tool_result history: %#v", toolResult)
	}
}

func TestProviderAnthropicResponsesStreamsCustomToolCalls(t *testing.T) {
	t.Parallel()

	protocol := newProviderAnthropicMessagesProtocolAdapter("kimi_code", alternateProtocolDefinition{}, canonicalProtocolOpenAIResponses)
	request := gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Stream:         true,
			Messages:       []canonicalMessage{{Type: "message", Role: "user", Content: []canonicalContentPart{{Type: "input_text", Text: "patch"}}}},
			Tools:          []canonicalTool{{Type: "custom", Name: "apply_patch"}},
			Params:         map[string]any{"max_output_tokens": 64},
		},
	}
	candidate := routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "kimi_code"}, Model: routeengine.CandidateModel{UpstreamName: "k3"}}
	if _, err := protocol.BuildUpstreamPayload(request, candidate); err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_stream_custom","model":"k3"}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_stream_custom","name":"apply_patch","input":{}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"input\":\"pa"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"tch\"}"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":3,"output_tokens":2}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}
	recorder := httptest.NewRecorder()
	capture, started, err := protocol.ProxyStream(context.Background(), recorder, resp, time.Now(), candidate)
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v; body=%s", err, recorder.Body.String())
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("stream did not complete: started=%v capture=%#v", started, capture)
	}
	body := recorder.Body.String()
	assertGatewayBodyContainsAll(t, body,
		`"type":"custom_tool_call"`,
		`"id":"ctc_call_stream_custom"`,
		"response.custom_tool_call_input.delta",
		"response.custom_tool_call_input.done",
		`"input":"patch"`,
		"response.completed",
	)
	if strings.Contains(body, "response.function_call_arguments.delta") {
		t.Fatalf("custom arguments leaked as function deltas: %s", body)
	}
}

func TestXLyraRelayAnthropicPayloadUsesProviderMaxTokensDefault(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        map[string]any{"model": "gpt-5.5", "input": "hi", "stream": true},
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Stream:         true,
			Messages: []canonicalMessage{{
				Type:    "message",
				Role:    "user",
				Content: []canonicalContentPart{{Type: "input_text", Text: "hi"}},
			}},
			Params: map[string]any{},
		},
	}
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			SiteType: "xlyra",
			BaseURL:  "https://xl.cci.im",
		},
		Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5"},
	}

	protocol := anthropicMessagesProtocolForCandidate(request, candidate)
	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if got := payload["max_tokens"]; got != 8192 {
		t.Fatalf("xLyra relay max_tokens = %#v, want provider default 8192", got)
	}
}

func TestProviderAnthropicPayloadRequiresMaxTokensWhenUnconfigured(t *testing.T) {
	t.Parallel()

	_, err := encodeCanonicalRequestToAnthropicMessages(canonicalRequest{
		SourceProtocol: canonicalProtocolOpenAIResponses,
		Messages: []canonicalMessage{{
			Type:    "message",
			Role:    "user",
			Content: []canonicalContentPart{{Type: "input_text", Text: "hi"}},
		}},
		Params: map[string]any{},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "unregistered_anthropic"},
		Model: routeengine.CandidateModel{UpstreamName: "unregistered-model"},
	})
	if err == nil {
		t.Fatal("expected missing max_tokens configuration error")
	}
	if !strings.Contains(err.Error(), "max_tokens is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClaudeCodeSiteAnthropicPayloadUsesProviderMaxTokensDefault(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        map[string]any{"model": "claude-fable-5", "input": "hi", "stream": true},
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Stream:         true,
			Messages: []canonicalMessage{{
				Type:    "message",
				Role:    "user",
				Content: []canonicalContentPart{{Type: "input_text", Text: "hi"}},
			}},
			Params: map[string]any{},
		},
	}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "claude_code", BaseURL: "https://api.anthropic.com"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-fable-5"},
	}

	protocol := newAnthropicMessagesProtocolAdapter(canonicalProtocolOpenAIResponses)
	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if got := payload["max_tokens"]; got != 8192 {
		t.Fatalf("claude_code site max_tokens = %#v, want provider default 8192", got)
	}
}

func TestGenericRelayClaudeModelUsesAnthropicMaxTokensDefault(t *testing.T) {
	t.Parallel()

	request := gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        map[string]any{"model": "claude-fable-5", "input": "hi", "stream": true},
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Stream:         true,
			Messages: []canonicalMessage{{
				Type:    "message",
				Role:    "user",
				Content: []canonicalContentPart{{Type: "input_text", Text: "hi"}},
			}},
			Params: map[string]any{},
		},
	}
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "newapi", BaseURL: "https://relay.example.com"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-fable-5"},
	}

	protocol := anthropicMessagesProtocolForCandidate(request, candidate)
	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if got := payload["max_tokens"]; got != 8192 {
		t.Fatalf("generic relay claude model max_tokens = %#v, want provider default 8192", got)
	}
}

func TestProviderAnthropicResponsesStreamCachesThinkingForToolRoundTrip(t *testing.T) {
	t.Parallel()

	callID := "toolu_provider_roundtrip_cache"
	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_provider_cache","model":"mimo-v2.5-pro"}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private reasoning"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_provider"}}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"` + callID + `","name":"lookup","input":{}}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":7,"output_tokens":3}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}
	recorder := httptest.NewRecorder()
	protocol := newProviderAnthropicMessagesProtocolAdapter("xiaomi_mimo", alternateProtocolDefinition{}, canonicalProtocolOpenAIResponses)

	capture, started, err := protocol.ProxyStream(context.Background(), recorder, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("stream not completed: started=%v capture=%#v body=%s", started, capture, recorder.Body.String())
	}

	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Stream:         true,
			Messages: []canonicalMessage{
				{Type: "function_call", ID: callID, ToolCallID: callID, Name: "lookup", Arguments: `{}`},
				{Type: "function_call_output", ToolCallID: callID, Output: "ok"},
			},
		},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"}})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	messages := payload["messages"].([]any)
	assistantMessage := messages[0].(map[string]any)
	content := assistantMessage["content"].([]any)
	firstBlock := content[0].(map[string]any)
	if firstBlock["type"] != "thinking" || firstBlock["thinking"] != "private reasoning" || firstBlock["signature"] != "sig_provider" {
		t.Fatalf("expected cached thinking block before tool_use, got %#v", content)
	}
}

func TestProviderAnthropicChatStreamPassesThinkingForRoundTrip(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`data: {"type":"message_start","message":{"id":"msg_provider_chat_thinking","model":"deepseek-v4-pro"}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private reasoning"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_provider"}}`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_chat_thinking","name":"lookup","input":{}}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":7,"output_tokens":3}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}
	recorder := httptest.NewRecorder()
	protocol := newProviderAnthropicMessagesProtocolAdapter("deepseek", alternateProtocolDefinition{}, canonicalProtocolOpenAIChat)

	capture, started, err := protocol.ProxyStream(context.Background(), recorder, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("stream not completed: started=%v capture=%#v body=%s", started, capture, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"reasoning_content":"private reasoning"`) {
		t.Fatalf("chat stream did not pass reasoning_content: %s", body)
	}
	if !strings.Contains(body, `"thinking_signature":"sig_provider"`) {
		t.Fatalf("chat stream did not pass thinking_signature: %s", body)
	}
}

func TestOpenAIResponsesPreservesReasoningContentThroughCanonical(t *testing.T) {
	t.Parallel()

	body, usage, err := convertResponseBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolAnthropicMessages, []byte(`{
		"id": "resp_1",
		"created_at": 123,
		"model": "deepseek-v4-pro",
		"status": "completed",
		"output": [{
			"id": "msg_1",
			"type": "message",
			"status": "completed",
			"role": "assistant",
			"reasoning_content": "private chain",
			"content": [{"type":"output_text","text":"hello","annotations":[]} ]
		}],
		"usage": {"input_tokens": 1, "output_tokens": 2, "total_tokens": 3}
	}`), responseConversionOptions{})
	if err != nil {
		t.Fatalf("convertResponseBetweenProtocols returned error: %v", err)
	}
	if usage.TotalTokens != 3 {
		t.Fatalf("usage = %#v, want total 3", usage)
	}
	if !strings.Contains(string(body), `"type":"thinking"`) || !strings.Contains(string(body), "private chain") {
		t.Fatalf("response did not preserve thinking: %s", body)
	}
}

func TestMiMoAnthropicResponsesDegradesToolResultWhenThinkingCacheMissing(t *testing.T) {
	t.Parallel()

	callID := "call_missing_mimo_thinking_cache"
	protocol := newProviderAnthropicMessagesProtocolAdapter("xiaomi_mimo", alternateProtocolDefinition{}, canonicalProtocolOpenAIResponses)
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Messages: []canonicalMessage{
				{Type: "function_call", ID: "fc_missing_cache", ToolCallID: callID, Name: "lookup", Arguments: `{"q":"lost"}`},
				{Type: "function_call_output", ToolCallID: callID, Output: "result after restart"},
			},
		},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "mimo-v2.5-pro"}})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages len = %d, want degraded user message only: %#v", len(messages), messages)
	}
	userMessage := messages[0].(map[string]any)
	if userMessage["role"] != "user" {
		t.Fatalf("degraded message role = %#v, want user: %#v", userMessage["role"], messages)
	}
	content := userMessage["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "text" || !strings.Contains(block["text"].(string), "result after restart") {
		t.Fatalf("expected tool result to be preserved as text, got %#v", messages)
	}
	for _, rawMessage := range messages {
		content, _ := rawMessage.(map[string]any)["content"].([]any)
		for _, rawBlock := range content {
			if rawBlock.(map[string]any)["type"] == "tool_use" || rawBlock.(map[string]any)["type"] == "tool_result" {
				t.Fatalf("degraded MiMo payload must not contain tool_use/tool_result blocks: %#v", messages)
			}
		}
	}
}

func TestThinkingRoundtripModelDegradesToolResultWhenThinkingCacheMissing(t *testing.T) {
	t.Parallel()

	callID := "call_missing_deepseek_thinking_cache"
	protocol := newProviderAnthropicMessagesProtocolAdapter("deepseek", alternateProtocolDefinition{}, canonicalProtocolOpenAIChat)
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIChat,
			Messages: []canonicalMessage{
				{Type: "message", Role: "user", Content: []canonicalContentPart{{Type: "input_text", Text: "start"}}},
				{Type: "message", Role: "assistant", ToolCalls: []canonicalToolCall{{
					ID:        callID,
					Type:      "function",
					Name:      "lookup",
					Arguments: `{"q":"lost"}`,
				}}},
				{Type: "function_call_output", ToolCallID: callID, Output: "result after restart"},
			},
		},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "deepseek", BaseURL: "https://api.deepseek.com"},
		Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-pro"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	messages := payload["messages"].([]any)
	for _, rawMessage := range messages {
		content, _ := rawMessage.(map[string]any)["content"].([]any)
		for _, rawBlock := range content {
			block := rawBlock.(map[string]any)
			if block["type"] == "tool_use" || block["type"] == "tool_result" {
				t.Fatalf("unhydrated thinking round must be degraded before DeepSeek Anthropic request: %#v", messages)
			}
		}
	}
	lastMessage := messages[len(messages)-1].(map[string]any)
	if lastMessage["role"] != "user" {
		t.Fatalf("last degraded message role = %#v, want user: %#v", lastMessage["role"], messages)
	}
	lastContent := lastMessage["content"].([]any)
	if !strings.Contains(lastContent[0].(map[string]any)["text"].(string), "result after restart") {
		t.Fatalf("degraded tool result was not preserved as text: %#v", messages)
	}
}

func TestProviderAnthropicResponsesOmitsUnpairedToolCalls(t *testing.T) {
	t.Parallel()

	protocol := newProviderAnthropicMessagesProtocolAdapter("deepseek", alternateProtocolDefinition{}, canonicalProtocolOpenAIResponses)
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Messages: []canonicalMessage{
				{Type: "message", Role: "user", Content: []canonicalContentPart{{Type: "input_text", Text: "start"}}},
				{Type: "function_call", ID: "fc_orphan", ToolCallID: "call_orphan", Name: "lookup", Arguments: `{"q":"missing"}`},
				{Type: "message", Role: "user", Content: []canonicalContentPart{{Type: "input_text", Text: "continue"}}},
				{Type: "function_call", ID: "fc_ok", ToolCallID: "call_ok", Name: "lookup", Arguments: `{"q":"ok"}`},
				{Type: "function_call_output", ToolCallID: "call_ok", Output: "ok"},
			},
		},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "deepseek-chat"}})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 4 {
		t.Fatalf("messages len = %d, want 4: %#v", len(messages), messages)
	}
	for _, rawMessage := range messages {
		message := rawMessage.(map[string]any)
		content, _ := message["content"].([]any)
		for _, rawBlock := range content {
			block, _ := rawBlock.(map[string]any)
			if block["type"] == "tool_use" && block["id"] == "call_orphan" {
				t.Fatalf("unpaired tool_use should not be emitted: %#v", messages)
			}
		}
	}
	assistantContent := messages[2].(map[string]any)["content"].([]any)
	toolUse := assistantContent[0].(map[string]any)
	if toolUse["type"] != "tool_use" || toolUse["id"] != "call_ok" {
		t.Fatalf("expected paired tool_use at messages[2], got %#v", messages[2])
	}
	userContent := messages[3].(map[string]any)["content"].([]any)
	toolResult := userContent[0].(map[string]any)
	if toolResult["type"] != "tool_result" || toolResult["tool_use_id"] != "call_ok" {
		t.Fatalf("expected paired tool_result immediately after, got %#v", messages[3])
	}
}

func TestProviderAnthropicResponsesGroupsMultipleToolCallsWithResults(t *testing.T) {
	t.Parallel()

	protocol := newProviderAnthropicMessagesProtocolAdapter("deepseek", alternateProtocolDefinition{}, canonicalProtocolOpenAIResponses)
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIResponses,
			Messages: []canonicalMessage{
				{Type: "function_call", ID: "fc_a", ToolCallID: "call_a", Name: "lookup", Arguments: `{"q":"a"}`},
				{Type: "function_call", ID: "fc_b", ToolCallID: "call_b", Name: "lookup", Arguments: `{"q":"b"}`},
				{Type: "function_call_output", ToolCallID: "call_a", Output: "a"},
				{Type: "function_call_output", ToolCallID: "call_b", Output: "b"},
			},
		},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "deepseek-chat"}})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %#v", len(messages), messages)
	}
	assistantContent := messages[0].(map[string]any)["content"].([]any)
	userContent := messages[1].(map[string]any)["content"].([]any)
	if len(assistantContent) != 2 || len(userContent) != 2 {
		t.Fatalf("tool roundtrip was not grouped: %#v", messages)
	}
	if assistantContent[0].(map[string]any)["id"] != "call_a" || assistantContent[1].(map[string]any)["id"] != "call_b" {
		t.Fatalf("unexpected tool_use order: %#v", assistantContent)
	}
	if userContent[0].(map[string]any)["tool_use_id"] != "call_a" || userContent[1].(map[string]any)["tool_use_id"] != "call_b" {
		t.Fatalf("unexpected tool_result order: %#v", userContent)
	}
}

func TestProviderAnthropicMessagesPairsChatToolCallsWithResults(t *testing.T) {
	t.Parallel()

	protocol := newProviderAnthropicMessagesProtocolAdapter("deepseek", alternateProtocolDefinition{}, canonicalProtocolOpenAIResponses)
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIChat,
			Messages: []canonicalMessage{
				{
					Type: "message",
					Role: "assistant",
					ToolCalls: []canonicalToolCall{
						{ID: "call_chat", Name: "lookup", Arguments: `{"q":"chat"}`},
					},
				},
				{Type: "function_call_output", ToolCallID: "call_chat", Output: "chat ok"},
			},
		},
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "deepseek-chat"}})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2: %#v", len(messages), messages)
	}
	assistantContent := messages[0].(map[string]any)["content"].([]any)
	userContent := messages[1].(map[string]any)["content"].([]any)
	if assistantContent[0].(map[string]any)["type"] != "tool_use" || assistantContent[0].(map[string]any)["id"] != "call_chat" {
		t.Fatalf("expected chat tool_use, got %#v", assistantContent)
	}
	if userContent[0].(map[string]any)["type"] != "tool_result" || userContent[0].(map[string]any)["tool_use_id"] != "call_chat" {
		t.Fatalf("expected chat tool_result, got %#v", userContent)
	}
}

func TestOpenAIResolverDoesNotUseOfficialDeepSeekAnthropicForNewAPI(t *testing.T) {
	t.Parallel()

	protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{
		DownstreamPath: gatewayEndpointMessages,
		Payload:        map[string]any{"model": "deepseek-v4-pro", "messages": []any{}},
	}, routeengine.Candidate{
		Site: routeengine.CandidateSite{
			SiteType: "newapi",
			BaseURL:  "https://api.deepseek.com",
		},
		Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-pro"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if protocol.ProtocolName() == "deepseek_anthropic_messages" {
		t.Fatal("NewAPI/third-party DeepSeek proxy must not use official Anthropic endpoint override")
	}
}

func TestOpenAIResolverUsesAnthropicMessagesForAnthropicSite(t *testing.T) {
	t.Parallel()

	protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Payload: map[string]any{
			"model": "claude-sonnet-4-20250514",
			"messages": []any{
				map[string]any{"role": "system", "content": "Be concise."},
				map[string]any{"role": "user", "content": "Hi"},
			},
			"tools": []any{map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup",
					"description": "Lookup a value",
					"parameters":  map[string]any{"type": "object"},
				},
			}},
		},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "anthropic", BaseURL: "https://api.anthropic.com"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-20250514"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if protocol.ProtocolName() != "anthropic_messages_to_chat_completions" {
		t.Fatalf("protocol = %q", protocol.ProtocolName())
	}
	if got := protocol.UpstreamPath("https://api.anthropic.com"); got != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("UpstreamPath = %q", got)
	}

	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIChat,
			Stream:         true,
			Instructions:   "Be concise.",
			Messages: []canonicalMessage{{
				Type:    "message",
				Role:    "user",
				Content: []canonicalContentPart{{Type: "input_text", Text: "Hi"}},
			}},
			Tools: []canonicalTool{{
				Type:        "function",
				Name:        "lookup",
				Description: "Lookup a value",
				Parameters:  map[string]any{"type": "object"},
			}},
			Params: map[string]any{"max_output_tokens": 64},
		},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "anthropic", BaseURL: "https://api.anthropic.com"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-20250514"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "claude-sonnet-4-20250514" || payload["system"] != "Be concise." || payload["max_tokens"] != 64 {
		t.Fatalf("unexpected Anthropic payload: %#v", payload)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", payload["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "lookup" || tool["input_schema"] == nil {
		t.Fatalf("unexpected tool: %#v", tool)
	}
}

func TestAnthropicMessagesAdapterAppliesOfficialHeaders(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Authorization", "Bearer sk-ant-test")
	anthropicMessagesProtocolAdapter{}.ApplyUpstreamHeaders(req, "sk-ant-test", "", true)

	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty", got)
	}
	if got := req.Header.Get("x-api-key"); got != "sk-ant-test" {
		t.Fatalf("x-api-key = %q", got)
	}
	if got := req.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", got)
	}
}

func TestAnthropicMessagesEndpointCapturesDownstreamHeaders(t *testing.T) {
	t.Parallel()

	body := strings.NewReader(`{"model":"claude-opus-4-8[1m]","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`)
	req, err := http.NewRequest(http.MethodPost, "https://gateway.test/v1/messages", body)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Anthropic-Beta", "context-1m-2025-08-07")

	request, failure := anthropicMessagesEndpointAdapter{}.DecodeRequest(req)
	if failure != nil {
		t.Fatalf("DecodeRequest returned failure: %#v", failure)
	}
	if got := request.DownstreamHeaders.Get("Anthropic-Beta"); got != "context-1m-2025-08-07" {
		t.Fatalf("Anthropic-Beta = %q, want context-1m-2025-08-07", got)
	}
}

func TestAnthropicMessagesAdapterPreservesNativeMessagesPayload(t *testing.T) {
	t.Parallel()

	payload, err := (anthropicMessagesProtocolAdapter{}).BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointMessages,
		Payload: map[string]any{
			"model":      "claude-sonnet-4-20250514",
			"max_tokens": 128,
			"thinking":   map[string]any{"type": "enabled", "budget_tokens": 1024},
			"messages": []any{
				map[string]any{
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "thinking", "thinking": "private chain", "signature": "sig_123"},
						map[string]any{"type": "tool_use", "id": "toolu_1", "name": "lookup", "input": map[string]any{"q": "x"}},
					},
				},
			},
		},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "anthropic"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-20250514"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	messages := payload["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["type"] != "thinking" || content[0].(map[string]any)["signature"] != "sig_123" {
		t.Fatalf("native thinking block was not preserved: %#v", payload)
	}
	if payload["thinking"] == nil {
		t.Fatalf("native thinking request config was not preserved: %#v", payload)
	}
}

func TestOpenAIResolverUsesImagesAdapterForImageEndpoint(t *testing.T) {
	t.Parallel()

	protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{
		DownstreamPath: gatewayEndpointImagesGenerations,
		Payload:        map[string]any{"model": "gpt-image-2", "prompt": "draw a cat"},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if protocol.ProtocolName() != "openai_images_generations" {
		t.Fatalf("protocol = %q, want openai_images_generations", protocol.ProtocolName())
	}
}

func TestOpenAIResolverUsesCodexAdapterForCodexImageEndpoint(t *testing.T) {
	t.Parallel()

	protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{
		DownstreamPath: gatewayEndpointImagesGenerations,
		Payload:        map[string]any{"model": "gpt-image-2", "prompt": "draw a cat"},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if protocol.ProtocolName() != "codex_responses" {
		t.Fatalf("protocol = %q, want codex_responses", protocol.ProtocolName())
	}
}

func TestResponsesToChatCompletionsPayloadMapsCoreFields(t *testing.T) {
	t.Parallel()

	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, map[string]any{
		"model":             "alias",
		"instructions":      "Be concise.",
		"input":             "Hello",
		"stream":            true,
		"max_output_tokens": 128.0,
		"reasoning":         map[string]any{"effort": "medium"},
		"tools": []any{
			map[string]any{
				"type":        "function",
				"name":        "lookup_weather",
				"description": "Get weather",
				"parameters":  map[string]any{"type": "object"},
			},
			map[string]any{"type": "web_search"},
		},
	}, "alias", routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "upstream-gpt"}})
	if err != nil {
		t.Fatalf("convertRequestBetweenProtocols returned error: %v", err)
	}

	if payload["model"] != "upstream-gpt" {
		t.Fatalf("model = %#v, want upstream-gpt", payload["model"])
	}
	if payload["max_tokens"] != 128 {
		t.Fatalf("max_tokens = %#v, want 128", payload["max_tokens"])
	}
	if payload["reasoning_effort"] != "medium" {
		t.Fatalf("reasoning_effort = %#v, want medium", payload["reasoning_effort"])
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v, want system + user", payload["messages"])
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want only function tool", payload["tools"])
	}
}

func TestChatCompletionsToResponsesPayloadMapsCoreFields(t *testing.T) {
	t.Parallel()

	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, map[string]any{
		"model":                 "alias",
		"stream":                true,
		"messages":              []any{map[string]any{"role": "system", "content": "You are helpful."}, map[string]any{"role": "user", "content": "Hello"}},
		"max_completion_tokens": 128.0,
		"temperature":           0.2,
		"top_p":                 0.8,
		"parallel_tool_calls":   true,
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup_weather",
					"description": "Get weather",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
		"tool_choice":      map[string]any{"type": "function", "function": map[string]any{"name": "lookup_weather"}},
		"response_format":  map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "foo", "schema": map[string]any{"type": "object"}}},
		"reasoning_effort": "medium",
	}, "alias", routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4"}})
	if err != nil {
		t.Fatalf("convertRequestBetweenProtocols returned error: %v", err)
	}

	if got := payload["model"]; got != "gpt-5.4" {
		t.Fatalf("expected upstream model gpt-5.4, got %#v", got)
	}
	if got := payload["max_output_tokens"]; got != 128 {
		t.Fatalf("expected max_output_tokens 128, got %#v", got)
	}
	if got := payload["instructions"]; got != "You are helpful." {
		t.Fatalf("expected instructions to contain system prompt, got %#v", got)
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("expected one input item, got %#v", payload["input"])
	}
	if got := payload["parallel_tool_calls"]; got != true {
		t.Fatalf("expected parallel_tool_calls true, got %#v", got)
	}
	if _, ok := payload["reasoning"].(map[string]any); !ok {
		t.Fatalf("expected reasoning block, got %#v", payload["reasoning"])
	}
	if _, ok := payload["text"].(map[string]any); !ok {
		t.Fatalf("expected text format block, got %#v", payload["text"])
	}
}

func TestOpenAIChatAdapterTransformsBufferedChatResponseToResponses(t *testing.T) {
	t.Parallel()

	adapter := openAIChatProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}
	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, []byte(`{
		"id":"chatcmpl_123",
		"created":1710000000,
		"model":"upstream-gpt",
		"choices":[{"index":0,"message":{"role":"assistant","content":"Hello from chat"}}],
		"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}
	}`))
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.ContentType != "application/json" {
		t.Fatalf("ContentType = %q, want application/json", transformed.ContentType)
	}
	assertGatewayBodyContainsAll(t, string(transformed.Body), `"object":"response"`, `"output_text"`)
	if transformed.Usage.PromptTokens != 11 || transformed.Usage.CompletionTokens != 7 {
		t.Fatalf("unexpected usage: %+v", transformed.Usage)
	}
}

func TestOpenAIResponsesTransformBufferedResponseToChat(t *testing.T) {
	t.Parallel()

	adapter := newOpenAIResponsesProtocolAdapter(gatewayRequest{}).(openAIResponsesProtocolAdapter)
	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, []byte(`{
		"id":"resp_123",
		"created_at":1710000000,
		"model":"gpt-5-codex",
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello from responses"}]}],
		"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}
	}`))
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.ContentType != "application/json" {
		t.Fatalf("expected application/json content type, got %q", transformed.ContentType)
	}
	assertGatewayBodyContainsAll(t, string(transformed.Body), "\"chat.completion\"")
	if transformed.Usage.PromptTokens != 11 || transformed.Usage.CompletionTokens != 7 {
		t.Fatalf("unexpected usage: %+v", transformed.Usage)
	}
}

func TestOpenAIResponsesAdapterPassesThroughForDownstreamResponses(t *testing.T) {
	t.Parallel()

	adapter := newOpenAIResponsesProtocolAdapter(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        map[string]any{"model": "alias", "input": "hi"},
	}).(openAIResponsesProtocolAdapter)
	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, []byte(`{
		"id":"resp_123",
		"created_at":1710000000,
		"model":"upstream-gpt",
		"output":[],
		"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}
	}`))
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	assertGatewayBodyContainsAll(t, string(transformed.Body), `"id":"resp_123"`)
	if transformed.Usage.PromptTokens != 11 || transformed.Usage.CompletionTokens != 7 {
		t.Fatalf("unexpected usage: %+v", transformed.Usage)
	}
}

func TestOpenAIResponsesAdapterParsesBufferedSSEForDownstreamResponses(t *testing.T) {
	t.Parallel()

	adapter := newOpenAIResponsesProtocolAdapter(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload:        map[string]any{"model": "alias", "input": "hi"},
	}).(openAIResponsesProtocolAdapter)
	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}}, []byte(
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_123\",\"created_at\":1710000000,\"model\":\"upstream-gpt\"}}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"created_at\":1710000000,\"model\":\"upstream-gpt\",\"output\":[{\"id\":\"ig_123\",\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"ZmFrZQ==\"}],\"usage\":{\"input_tokens\":11,\"output_tokens\":7,\"total_tokens\":18}}}\n\n",
	))
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.ContentType != "application/json" {
		t.Fatalf("ContentType = %q, want application/json", transformed.ContentType)
	}
	assertGatewayBodyContainsAll(t, string(transformed.Body), `"type":"image_generation_call"`)
	if transformed.Usage.PromptTokens != 11 || transformed.Usage.CompletionTokens != 7 {
		t.Fatalf("unexpected usage: %+v", transformed.Usage)
	}
}

func TestOpenAIResponsesProxyStreamTransformsToChatChunks(t *testing.T) {
	t.Parallel()

	adapter := newOpenAIResponsesProtocolAdapter(gatewayRequest{
		Payload: map[string]any{
			"stream_options": map[string]any{"include_usage": true},
		},
	}).(openAIResponsesProtocolAdapter)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_123\",\"created_at\":1710000000,\"model\":\"gpt-5-codex\"}}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hel\"}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"lo\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"created_at\":1710000000,\"model\":\"gpt-5-codex\",\"usage\":{\"input_tokens\":11,\"output_tokens\":7,\"total_tokens\":18}}}\n\n",
		)),
	}
	rec := httptest.NewRecorder()

	capture, started, err := adapter.ProxyStream(context.Background(), rec, resp, time.Now(), routeengine.Candidate{})
	if err != nil {
		t.Fatalf("ProxyStream returned error: %v", err)
	}
	if !started {
		t.Fatal("expected transformed stream to start downstream response")
	}
	if !capture.streamCompleted || !capture.sawDone {
		t.Fatalf("expected completed stream, got %+v", capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(),
		"\"chat.completion.chunk\"",
		"\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"total_tokens\":18}",
		"data: [DONE]",
	)
}
