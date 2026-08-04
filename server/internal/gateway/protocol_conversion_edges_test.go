package gateway

import (
	"reflect"
	"strings"
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestCanonicalChatContentToolCallsAndToolOutput(t *testing.T) {
	t.Parallel()

	request, err := canonicalRequestFromOpenAIChatPayload(map[string]any{
		"model": "alias",
		"messages": []any{
			map[string]any{
				"role": "system",
				"content": []any{
					map[string]any{"type": "text", "text": " System one "},
					map[string]any{"type": "input_text", "text": "ignored by system extraction"},
				},
			},
			map[string]any{"role": "developer", "content": " Developer two "},
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "hello"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": " data:image/png;base64,abc "}},
					map[string]any{"type": "input_image", "image_url": " https://example.test/image.png "},
					map[string]any{"type": "unknown", "text": "skip"},
					"not-a-map",
				},
			},
			map[string]any{
				"role":    "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": "done"}},
				"tool_calls": []any{
					map[string]any{
						"call_id": " call_fallback ",
						"type":    "",
						"function": map[string]any{
							"name":      " lookup ",
							"arguments": `{"city":"Tokyo"}`,
						},
						"thoughtSignature": " sig_camel ",
					},
					map[string]any{"id": "missing_name", "function": map[string]any{"arguments": "{}"}},
				},
				"reasoning_content":  " hidden chain ",
				"thinking_signature": " sig_think ",
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": " call_fallback ",
				"name":         " lookup ",
				"content":      map[string]any{"ok": true},
			},
		},
	}, " alias ")
	if err != nil {
		t.Fatalf("canonicalRequestFromOpenAIChatPayload returned error: %v", err)
	}

	if request.Instructions != "System one\n\nDeveloper two" {
		t.Fatalf("instructions = %q, want joined system/developer text", request.Instructions)
	}
	if request.RequestedModel != "alias" {
		t.Fatalf("requested model = %q, want alias", request.RequestedModel)
	}
	if len(request.Messages) != 3 {
		t.Fatalf("messages len = %d, want user, assistant, tool", len(request.Messages))
	}

	user := request.Messages[0]
	if len(user.Content) != 3 {
		t.Fatalf("user content len = %d, want text plus two images: %#v", len(user.Content), user.Content)
	}
	if user.Content[0].Type != "input_text" || user.Content[0].Text != "hello" {
		t.Fatalf("user text part = %#v, want input_text hello", user.Content[0])
	}
	if user.Content[1].Type != "input_image" || user.Content[1].ImageURL != "data:image/png;base64,abc" {
		t.Fatalf("nested image part = %#v, want normalized data URL", user.Content[1])
	}
	if user.Content[2].Type != "input_image" || user.Content[2].ImageURL != "https://example.test/image.png" {
		t.Fatalf("string image part = %#v, want normalized URL", user.Content[2])
	}

	assistant := request.Messages[1]
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("assistant tool calls len = %d, want one valid call: %#v", len(assistant.ToolCalls), assistant.ToolCalls)
	}
	call := assistant.ToolCalls[0]
	if call.ID != "call_fallback" || call.Type != "function" || call.Name != "lookup" || call.Arguments != `{"city":"Tokyo"}` {
		t.Fatalf("assistant tool call = %#v, want trimmed fallback id/function fields", call)
	}
	if call.Metadata["thoughtSignature"] != " sig_camel " {
		t.Fatalf("tool call metadata = %#v, want thoughtSignature", call.Metadata)
	}
	if len(assistant.Thinking) != 1 || assistant.Thinking[0].Thinking != "hidden chain" || assistant.Thinking[0].Signature != "sig_think" {
		t.Fatalf("assistant thinking = %#v, want reasoning content and signature", assistant.Thinking)
	}

	toolOutput := request.Messages[2]
	if toolOutput.ToolCallID != "call_fallback" || toolOutput.Name != "lookup" || toolOutput.Output != `{"ok":true}` {
		t.Fatalf("tool output = %#v, want normalized JSON output", toolOutput)
	}
}

func TestCanonicalResponsesInputAndEncodingBoundaries(t *testing.T) {
	t.Parallel()

	messages := canonicalMessagesFromResponsesInput([]any{
		map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "output_text", "text": "visible"},
				map[string]any{"type": "input_image", "image_url": " https://example.test/input.png "},
			},
			"thinking":           " private reasoning ",
			"thinking_signature": " sig_private ",
		},
		map[string]any{
			"type":              "function_call",
			"id":                " fc_1 ",
			"name":              " lookup ",
			"arguments":         `{"q":"x"}`,
			"thought_signature": " sig_call ",
		},
		map[string]any{
			"type":    "function_call_output",
			"call_id": " fc_1 ",
			"output":  " result ",
		},
		map[string]any{
			"role":    "",
			"content": "default role",
		},
	})
	if len(messages) != 4 {
		t.Fatalf("messages len = %d, want 4: %#v", len(messages), messages)
	}
	if messages[0].Role != "assistant" || len(messages[0].Content) != 2 || messages[0].Content[0].Type != "output_text" {
		t.Fatalf("assistant message = %#v, want output_text plus image", messages[0])
	}
	if len(messages[0].Thinking) != 1 || messages[0].Thinking[0].Thinking != "private reasoning" || messages[0].Thinking[0].Signature != "sig_private" {
		t.Fatalf("assistant thinking = %#v, want chat-style thinking", messages[0].Thinking)
	}
	reasoning := canonicalThinkingFromResponsesItem(map[string]any{
		"type":    "reasoning",
		"content": []any{map[string]any{"type": "reasoning_text", "text": "step one"}, map[string]any{"type": "summary_text", "text": "step two"}},
	})
	if len(reasoning) != 1 || reasoning[0].Thinking != "step one\nstep two" {
		t.Fatalf("reasoning item thinking = %#v, want joined reasoning text", reasoning)
	}
	if messages[1].ToolCallID != "fc_1" || messages[1].ID != "fc_1" || messages[1].Name != "lookup" || messages[1].Metadata["thought_signature"] != " sig_call " {
		t.Fatalf("function call = %#v, want id fallback and metadata", messages[1])
	}
	if messages[2].ToolCallID != "fc_1" || messages[2].Output != "result" {
		t.Fatalf("function output = %#v, want trimmed call id and output", messages[2])
	}
	if messages[3].Role != "user" || messages[3].Content[0].Text != "default role" {
		t.Fatalf("default role message = %#v, want user text message", messages[3])
	}

	encoded := encodeCanonicalMessagesAsResponsesInput([]canonicalMessage{
		{Type: "function_call_output", Output: map[string]any{"ok": true}},
		{Type: "function_call_output", ToolCallID: " call_1 ", Output: nil},
		{Type: "function_call", ID: "fc_2", Name: "search", Arguments: `{}`, Metadata: map[string]any{"thoughtSignature": "sig_encoded"}},
		{Type: "message", Role: "assistant", Thinking: []canonicalThinkingBlock{{Thinking: "think"}}, ToolCalls: []canonicalToolCall{{
			ID:        "call_2",
			Name:      "lookup",
			Arguments: `{"x":1}`,
		}}},
	})
	if len(encoded) != 5 {
		t.Fatalf("encoded input len = %d, want 5: %#v", len(encoded), encoded)
	}
	if first := encoded[0].(map[string]any); first["role"] != "user" || first["content"] != `{"ok":true}` {
		t.Fatalf("unpaired function output = %#v, want user JSON content", first)
	}
	if second := encoded[1].(map[string]any); second["type"] != "function_call_output" || second["call_id"] != "call_1" || second["output"] != "" {
		t.Fatalf("paired nil function output = %#v, want function_call_output with empty output", second)
	}
	if third := encoded[2].(map[string]any); third["call_id"] != "fc_2" || third["thought_signature"] != "sig_encoded" {
		t.Fatalf("standalone function call = %#v, want metadata and id fallback", third)
	}
	if reasoning := encoded[3].(map[string]any); reasoning["type"] != "reasoning" {
		t.Fatalf("assistant thinking item = %#v, want reasoning item", reasoning)
	}
	if toolCall := encoded[4].(map[string]any); toolCall["type"] != "function_call" || toolCall["call_id"] != "call_2" {
		t.Fatalf("assistant tool call item = %#v, want function_call", toolCall)
	}
}

func TestOpenAIRequestEncodingHelperBoundaries(t *testing.T) {
	t.Parallel()

	request := canonicalRequest{
		Instructions: "respond briefly",
		Params: map[string]any{
			"max_output_tokens": 90,
			"reasoning":         map[string]any{"effort": " medium "},
		},
		TextFormat: map[string]any{"type": "json_schema", "name": "answer", "schema": map[string]any{"type": "object"}},
		Tools: []canonicalTool{
			{Type: "function", Name: "search", Description: "Search", Parameters: map[string]any{"type": "object"}},
			{Type: "web_search_preview", Raw: map[string]any{"type": "web_search_preview"}},
		},
		ToolChoice: map[string]any{"type": "function", "name": "search"},
		Messages: []canonicalMessage{
			{Type: "message", Role: "developer", Content: []canonicalContentPart{{Type: "input_text", Text: "dev"}}},
			{Type: "message", Role: "", RawContent: map[string]any{"fallback": true}},
		},
	}

	chat := encodeCanonicalRequestToOpenAIChat(request, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "chat-upstream"},
	})
	if chat["model"] != "chat-upstream" || chat["max_tokens"] != 90 || chat["reasoning_effort"] != "medium" {
		t.Fatalf("chat scalar fields = %#v, want mapped model/max/reasoning", chat)
	}
	if choice := chat["tool_choice"].(map[string]any); choice["type"] != "function" || choice["function"].(map[string]any)["name"] != "search" {
		t.Fatalf("chat tool_choice = %#v, want nested function choice", choice)
	}
	if tools := chat["tools"].([]any); len(tools) != 1 {
		t.Fatalf("chat tools = %#v, want only function tools", tools)
	}
	messages := chat["messages"].([]any)
	if messages[0].(map[string]any)["role"] != "system" || messages[1].(map[string]any)["role"] != "system" || messages[2].(map[string]any)["role"] != "user" {
		t.Fatalf("chat messages = %#v, want developer as system and empty role as user", messages)
	}
	if messages[2].(map[string]any)["content"] != "" {
		t.Fatalf("empty canonical content = %#v, want empty string content", messages[2])
	}

	responsesTools := encodeCanonicalToolsAsResponses(request.Tools).([]any)
	if len(responsesTools) != 2 {
		t.Fatalf("responses tools = %#v, want function plus raw non-function", responsesTools)
	}
	if choice := encodeCanonicalToolChoiceAsResponses(map[string]any{"type": "function", "function": map[string]any{"name": " search "}}).(map[string]any); choice["name"] != "search" {
		t.Fatalf("responses tool choice = %#v, want flattened trimmed function name", choice)
	}
	if got := encodeCanonicalToolChoiceAsChat("invalid"); got != nil {
		t.Fatalf("invalid chat tool choice = %#v, want nil", got)
	}
}

func TestAntigravityImagePromptConfigAndModelHelpers(t *testing.T) {
	t.Parallel()

	prompt := antigravityCanonicalImagePrompt(canonicalRequest{
		Params: map[string]any{"quality": "standard", "style": "vivid"},
		Image: &canonicalImageRequest{
			Prompt: "  city at night  ",
			Params: map[string]any{"quality": "hd", "style": "natural"},
		},
	})
	for _, want := range []string{"city at night", "high quality", "natural lighting"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt %q does not contain %q", prompt, want)
		}
	}
	if strings.Contains(prompt, "vivid colors") {
		t.Fatalf("image params should override request params, got prompt %q", prompt)
	}
	if got := antigravityCanonicalImagePrompt(canonicalRequest{}); got != "A beautiful image" {
		t.Fatalf("empty image prompt = %q, want default", got)
	}

	tests := []struct {
		name            string
		params          map[string]any
		model           string
		wantModel       string
		wantAspectRatio string
		wantImageSize   string
	}{
		{
			name:            "direct keys override model suffixes",
			params:          map[string]any{"aspectRatio": " 4:5 ", "imageSize": " 2k "},
			model:           "gemini-3-pro-image-16x9-4k",
			wantModel:       "gemini-3-pro-image",
			wantAspectRatio: "4:5",
			wantImageSize:   "2K",
		},
		{
			name:            "pixel size maps to nearby ratio and quality maps size",
			params:          map[string]any{"size": "1536x1024", "quality": "standard"},
			model:           "gemini-3-pro-image",
			wantModel:       "gemini-3-pro-image",
			wantAspectRatio: "3:2",
			wantImageSize:   "1K",
		},
		{
			name:            "model suffixes supply defaults",
			params:          map[string]any{},
			model:           "gemini-3-pro-image-21-9-hd",
			wantModel:       "gemini-3-pro-image",
			wantAspectRatio: "21:9",
			wantImageSize:   "4K",
		},
		{
			name:            "invalid ratio falls back to square with no image size",
			params:          map[string]any{"size": "wide"},
			model:           "",
			wantModel:       "gemini-3-pro-image",
			wantAspectRatio: "1:1",
			wantImageSize:   "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			model, config := antigravityCanonicalImageConfig(canonicalRequest{Params: tt.params}, tt.model)
			if model != tt.wantModel {
				t.Fatalf("model = %q, want %q", model, tt.wantModel)
			}
			if config["aspectRatio"] != tt.wantAspectRatio {
				t.Fatalf("aspectRatio = %#v, want %q", config["aspectRatio"], tt.wantAspectRatio)
			}
			if tt.wantImageSize == "" {
				if _, ok := config["imageSize"]; ok {
					t.Fatalf("imageSize = %#v, want omitted", config["imageSize"])
				}
				return
			}
			if config["imageSize"] != tt.wantImageSize {
				t.Fatalf("imageSize = %#v, want %q", config["imageSize"], tt.wantImageSize)
			}
		})
	}
}

func TestAntigravityRequestPayloadSystemToolsAndResponses(t *testing.T) {
	t.Parallel()

	payload := encodeCanonicalRequestToAntigravityGemini(canonicalRequest{
		Instructions: "top instruction",
		Messages: []canonicalMessage{
			{Type: "message", Role: "system", Content: []canonicalContentPart{{Text: "system message"}}},
			{Type: "message", Role: "developer", RawContent: []any{map[string]any{"text": "developer raw"}}},
			{Type: "message", Role: "user", RawContent: []any{"a", "b"}},
			{Type: "function_call", ID: "signed_call", Name: "lookup", Arguments: `{"q":"x"}`, Metadata: map[string]any{"thought_signature": "sig"}},
			{Type: "function_call_output", ToolCallID: "signed_call", Output: map[string]any{"ok": true}},
		},
		Params: map[string]any{
			"response_format": map[string]any{"type": "json_object"},
			"top_p":           0.8,
		},
		Tools: []canonicalTool{
			{
				Type:        "function",
				Name:        "lookup",
				Description: "Lookup data",
				Parameters: map[string]any{
					"type":                 []any{"null", "object"},
					"additionalProperties": false,
					"properties": map[string]any{
						"q": map[string]any{"type": []any{"null", "string"}, "pattern": "ignored"},
					},
				},
			},
			{Type: "web_search_preview", Raw: map[string]any{"type": "web_search_preview"}},
		},
	}, "gemini-3-pro")

	system := payload["systemInstruction"].(map[string]any)
	systemParts := system["parts"].([]any)
	if len(systemParts) != 3 {
		t.Fatalf("system parts len = %d, want instructions plus system/developer messages: %#v", len(systemParts), systemParts)
	}
	if systemParts[0].(map[string]any)["text"] != "top instruction" || systemParts[2].(map[string]any)["text"] != `[{"text":"developer raw"}]` {
		t.Fatalf("system parts = %#v, want instruction and raw developer content", systemParts)
	}
	generationConfig := payload["generationConfig"].(map[string]any)
	if generationConfig["responseMimeType"] != "application/json" || generationConfig["topP"] != 0.8 {
		t.Fatalf("generationConfig = %#v, want response MIME type and topP", generationConfig)
	}

	tools := payload["tools"].([]any)
	declarations := tools[0].(map[string]any)["functionDeclarations"].([]any)
	if len(declarations) != 1 {
		t.Fatalf("function declarations = %#v, want only function tool", declarations)
	}
	parameters := declarations[0].(map[string]any)["parameters"].(map[string]any)
	if parameters["type"] != "object" {
		t.Fatalf("sanitized type = %#v, want object", parameters["type"])
	}
	if _, ok := parameters["additionalProperties"]; ok {
		t.Fatalf("sanitized schema retained unsupported additionalProperties: %#v", parameters)
	}
	property := parameters["properties"].(map[string]any)["q"].(map[string]any)
	if property["type"] != "string" {
		t.Fatalf("sanitized property type = %#v, want string", property["type"])
	}
	if _, ok := property["pattern"]; ok {
		t.Fatalf("sanitized property retained unsupported pattern: %#v", property)
	}

	contents := payload["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents len = %d, want user text, model call, user response: %#v", len(contents), contents)
	}
	userParts := contents[0].(map[string]any)["parts"].([]any)
	if userParts[0].(map[string]any)["text"] != `["a","b"]` {
		t.Fatalf("user raw fallback part = %#v, want JSON array text", userParts[0])
	}
	functionCallPart := contents[1].(map[string]any)["parts"].([]any)[0].(map[string]any)
	if functionCallPart["thoughtSignature"] != "sig" {
		t.Fatalf("function call part = %#v, want thoughtSignature", functionCallPart)
	}
	functionResponse := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if functionResponse["id"] != "signed_call" || functionResponse["name"] != "lookup" {
		t.Fatalf("function response = %#v, want paired id/name", functionResponse)
	}
	if !reflect.DeepEqual(functionResponse["response"], map[string]any{"content": `{"ok":true}`}) {
		t.Fatalf("function response payload = %#v, want normalized JSON content", functionResponse["response"])
	}
}

func TestOpenAIResolverDefaultPureAdapters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		request   gatewayRequest
		candidate routeengine.Candidate
		wantName  string
	}{
		{
			name:     "plain chat falls back to chat completions without database capabilities",
			request:  gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Payload: map[string]any{"model": "alias", "messages": []any{}}},
			wantName: "openai_chat_completions",
		},
		{
			name:     "plain images endpoint uses OpenAI images adapter",
			request:  gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations, Payload: map[string]any{"model": "alias", "prompt": "draw"}},
			wantName: "openai_images_generations",
		},
		{
			name:     "streaming antigravity chat uses stream protocol name",
			request:  gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, Stream: true, Payload: map[string]any{"model": "alias", "messages": []any{}}},
			wantName: "antigravity_stream_generate_content",
			candidate: routeengine.Candidate{
				Site: routeengine.CandidateSite{SiteType: " antigravity "},
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
				t.Fatalf("protocol name = %q, want %q", protocol.ProtocolName(), tt.wantName)
			}
		})
	}
}
