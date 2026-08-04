package gateway

import (
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestMultiHopConversion_ChatToMessagesToResponses(t *testing.T) {
	t.Parallel()

	anthropicCandidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "anthropic"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-20250514"},
	}
	openaiCandidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-4.1"},
	}

	chatPayload := map[string]any{
		"model":       "claude-sonnet-4-20250514",
		"temperature": 0.7,
		"top_p":       0.9,
		"stream":      true,
		"messages": []any{
			map[string]any{"role": "system", "content": "You are helpful."},
			map[string]any{"role": "user", "content": "Hello"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "get_weather",
					"description": "Get the weather",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}

	// Hop 1: Chat → Anthropic Messages (use anthropic candidate for max_tokens default)
	messagesPayload, err := convertRequestBetweenProtocols(
		canonicalProtocolOpenAIChat, canonicalProtocolAnthropicMessages,
		chatPayload, "claude-sonnet-4-20250514", anthropicCandidate,
	)
	if err != nil {
		t.Fatalf("Chat → Messages conversion failed: %v", err)
	}

	// Verify Messages payload has required Anthropic fields
	if messagesPayload["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("after hop 1: model = %v, want claude-sonnet-4-20250514", messagesPayload["model"])
	}
	messages, ok := messagesPayload["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("after hop 1: messages missing or empty, got %#v", messagesPayload["messages"])
	}
	if messagesPayload["system"] == nil {
		t.Fatalf("after hop 1: system instruction missing")
	}
	tools, ok := messagesPayload["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("after hop 1: tools missing, got %#v", messagesPayload["tools"])
	}

	// Hop 2: Anthropic Messages → Responses (use openai candidate)
	responsesPayload, err := convertRequestBetweenProtocols(
		canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIResponses,
		messagesPayload, "gpt-4.1", openaiCandidate,
	)
	if err != nil {
		t.Fatalf("Messages → Responses conversion failed: %v", err)
	}

	// Verify final Responses payload
	if responsesPayload["model"] != "gpt-4.1" {
		t.Errorf("after hop 2: model = %v, want gpt-4.1", responsesPayload["model"])
	}
	input, ok := responsesPayload["input"].([]any)
	if !ok || len(input) == 0 {
		t.Fatalf("after hop 2: input missing or empty, got %#v", responsesPayload["input"])
	}
	responsesTools, ok := responsesPayload["tools"].([]any)
	if !ok || len(responsesTools) == 0 {
		t.Fatalf("after hop 2: tools missing after 2 hops, got %#v", responsesPayload["tools"])
	}
	// Verify tool structure survived both hops
	tool0, ok := responsesTools[0].(map[string]any)
	if !ok {
		t.Fatalf("after hop 2: tools[0] is not map, got %T", responsesTools[0])
	}
	if tool0["name"] != "get_weather" {
		t.Errorf("after hop 2: tool name = %v, want get_weather", tool0["name"])
	}
}

func TestMultiHopConversion_ResponsesToChatToMessages(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "anthropic"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-20250514"},
	}

	responsesPayload := map[string]any{
		"model":        "claude-sonnet-4-20250514",
		"stream":       true,
		"instructions": "Be concise.",
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "What is 2+2?"},
				},
			},
		},
		"tools": []any{
			map[string]any{
				"type":        "function",
				"name":        "calculate",
				"description": "Do math",
				"parameters":  map[string]any{"type": "object"},
			},
		},
	}

	// Hop 1: Responses → Chat
	chatPayload, err := convertRequestBetweenProtocols(
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat,
		responsesPayload, "claude-sonnet-4-20250514", candidate,
	)
	if err != nil {
		t.Fatalf("Responses → Chat conversion failed: %v", err)
	}
	chatMessages, ok := chatPayload["messages"].([]any)
	if !ok || len(chatMessages) == 0 {
		t.Fatalf("after hop 1: messages missing, got %#v", chatPayload["messages"])
	}
	chatTools, ok := chatPayload["tools"].([]any)
	if !ok || len(chatTools) == 0 {
		t.Fatalf("after hop 1: tools missing, got %#v", chatPayload["tools"])
	}

	// Hop 2: Chat → Anthropic Messages
	messagesPayload, err := convertRequestBetweenProtocols(
		canonicalProtocolOpenAIChat, canonicalProtocolAnthropicMessages,
		chatPayload, "claude-sonnet-4-20250514", candidate,
	)
	if err != nil {
		t.Fatalf("Chat → Messages conversion failed: %v", err)
	}
	if messagesPayload["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("after hop 2: model = %v", messagesPayload["model"])
	}
	msgMessages, ok := messagesPayload["messages"].([]any)
	if !ok || len(msgMessages) == 0 {
		t.Fatalf("after hop 2: messages missing, got %#v", messagesPayload["messages"])
	}
	msgTools, ok := messagesPayload["tools"].([]any)
	if !ok || len(msgTools) == 0 {
		t.Fatalf("after hop 2: tools missing after 2 hops, got %#v", messagesPayload["tools"])
	}
	tool0, ok := msgTools[0].(map[string]any)
	if !ok {
		t.Fatalf("after hop 2: tools[0] is not map, got %T", msgTools[0])
	}
	if tool0["name"] != "calculate" {
		t.Errorf("after hop 2: tool name = %v, want calculate", tool0["name"])
	}
}

func TestMultiHopConversion_PolicyNotDoubleApplied(t *testing.T) {
	t.Parallel()

	// Use a candidate where Codex forced policies apply
	codexCandidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4-codex"},
	}

	chatPayload := map[string]any{
		"model":       "gpt-5.4-codex",
		"temperature": 0.5,
		"messages": []any{
			map[string]any{"role": "user", "content": "test"},
		},
	}

	// Hop: Chat → Codex Responses (applies Codex policy)
	codexPayload, err := convertRequestBetweenProtocols(
		canonicalProtocolOpenAIChat, canonicalProtocolCodexResponses,
		chatPayload, "gpt-5.4-codex", codexCandidate,
	)
	if err != nil {
		t.Fatalf("Chat → Codex conversion failed: %v", err)
	}

	// Codex policy should remove temperature
	if _, ok := codexPayload["temperature"]; ok {
		t.Errorf("temperature should be removed by Codex policy, got %v", codexPayload["temperature"])
	}
	// stream should be forced true
	if codexPayload["stream"] != true {
		t.Errorf("stream should be forced true by Codex, got %v", codexPayload["stream"])
	}
}

func TestMultiHopConversion_ToolChoicePreserved(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-4.1"},
	}

	chatPayload := map[string]any{
		"model": "gpt-4.1",
		"messages": []any{
			map[string]any{"role": "user", "content": "test"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "Search",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
		"tool_choice": "required",
	}

	// Hop 1: Chat → Responses
	responsesPayload, err := convertRequestBetweenProtocols(
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses,
		chatPayload, "gpt-4.1", candidate,
	)
	if err != nil {
		t.Fatalf("Chat → Responses failed: %v", err)
	}
	if responsesPayload["tool_choice"] == nil {
		t.Fatal("tool_choice lost in Chat → Responses conversion")
	}

	// Hop 2: Responses → Chat (round-trip)
	chatRoundTrip, err := convertRequestBetweenProtocols(
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat,
		responsesPayload, "gpt-4.1", candidate,
	)
	if err != nil {
		t.Fatalf("Responses → Chat round-trip failed: %v", err)
	}
	if chatRoundTrip["tool_choice"] == nil {
		t.Fatal("tool_choice lost in Responses → Chat round-trip")
	}
}
