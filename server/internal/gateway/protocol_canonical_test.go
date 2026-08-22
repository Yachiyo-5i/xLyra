package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestProtocolSpecsRegisterCurrentProtocols(t *testing.T) {
	t.Parallel()

	specs := protocolSpecs()
	tests := map[canonicalProtocol]string{
		canonicalProtocolOpenAIChat:        upstreamEndpointTypeOpenAI,
		canonicalProtocolOpenAIResponses:   upstreamEndpointTypeOpenAIResponse,
		canonicalProtocolOpenAIImages:      upstreamEndpointTypeOpenAIImage,
		canonicalProtocolCodexResponses:    upstreamEndpointTypeOpenAIResponse,
		canonicalProtocolAnthropicMessages: "anthropic-messages",
	}
	for protocol, endpointType := range tests {
		spec, ok := specs[protocol]
		if !ok {
			t.Fatalf("missing spec for %s", protocol)
		}
		if spec.EndpointType != endpointType {
			t.Fatalf("%s endpoint type = %q, want %q", protocol, spec.EndpointType, endpointType)
		}
		if spec.DecodeRequest == nil || spec.EncodeRequest == nil {
			t.Fatalf("%s must register request decode and encode functions", protocol)
		}
	}
	for _, protocol := range []canonicalProtocol{
		canonicalProtocolOpenAIChat,
		canonicalProtocolOpenAIResponses,
		canonicalProtocolCodexResponses,
		canonicalProtocolAntigravity,
		canonicalProtocolAnthropicMessages,
	} {
		if specs[protocol].DecodeResponse == nil {
			t.Fatalf("%s must register a response decoder", protocol)
		}
	}
	for _, protocol := range []canonicalProtocol{
		canonicalProtocolOpenAIChat,
		canonicalProtocolOpenAIResponses,
		canonicalProtocolOpenAIImages,
		canonicalProtocolCodexResponses,
		canonicalProtocolAnthropicMessages,
	} {
		if specs[protocol].EncodeResponse == nil {
			t.Fatalf("%s must register a response encoder", protocol)
		}
	}
}

func TestAnthropicUsageConvertsToChatCompletionTokenDetails(t *testing.T) {
	t.Parallel()

	body, usage, err := convertResponseBetweenProtocols(canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIChat, []byte(`{
		"id":"msg_cache",
		"model":"deepseek-v4-pro",
		"role":"assistant",
		"content":[{"type":"text","text":"ok"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":40,"output_tokens":8,"cache_read_input_tokens":60}
	}`), responseConversionOptions{})
	if err != nil {
		t.Fatalf("convertResponseBetweenProtocols returned error: %v", err)
	}
	if usage.PromptTokens != 100 || usage.CachedPromptTokens != 60 || usage.CompletionTokens != 8 {
		t.Fatalf("usage = %+v, want prompt=100 cached=60 completion=8", usage)
	}

	var response struct {
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode converted response: %v", err)
	}
	if response.Usage["prompt_tokens"] != float64(100) {
		t.Fatalf("prompt_tokens = %#v, want 100", response.Usage["prompt_tokens"])
	}
	details, ok := response.Usage["prompt_tokens_details"].(map[string]any)
	if !ok || details["cached_tokens"] != float64(60) {
		t.Fatalf("prompt_tokens_details = %#v, want cached_tokens=60", response.Usage["prompt_tokens_details"])
	}
	for _, key := range []string{"cached_tokens", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens"} {
		if _, ok := response.Usage[key]; ok {
			t.Fatalf("converted Chat usage contains non-standard field %q: %#v", key, response.Usage)
		}
	}
}

func TestCanonicalFileAttachmentConversions(t *testing.T) {
	t.Parallel()

	dataURL := "data:application/pdf;base64,cGRm"
	parts := canonicalContentPartsFromChatContent([]any{map[string]any{
		"type": "file",
		"file": map[string]any{"filename": "report.pdf", "file_data": dataURL},
	}}, "user")
	if len(parts) != 1 || parts[0].Type != "input_file" || parts[0].FileName != "report.pdf" || parts[0].FileData != dataURL {
		t.Fatalf("chat file parts = %#v", parts)
	}

	responses := encodeCanonicalContentAsResponses(parts, "user").([]map[string]any)
	if responses[0]["type"] != "input_file" || responses[0]["filename"] != "report.pdf" || responses[0]["file_data"] != dataURL {
		t.Fatalf("responses file content = %#v", responses)
	}
	chat := encodeCanonicalContentAsChat(parts).([]any)
	chatFile := chat[0].(map[string]any)["file"].(map[string]any)
	if chatFile["filename"] != "report.pdf" || chatFile["file_data"] != dataURL {
		t.Fatalf("chat file content = %#v", chat)
	}
	anthropic := encodeCanonicalContentAsAnthropic(parts)
	document := anthropic[0].(map[string]any)
	source := document["source"].(map[string]any)
	if document["type"] != "document" || document["title"] != "report.pdf" || source["media_type"] != "application/pdf" || source["data"] != "cGRm" {
		t.Fatalf("anthropic document content = %#v", anthropic)
	}
}

func TestCanonicalAnthropicDocumentConvertsToDataURL(t *testing.T) {
	t.Parallel()

	messages := canonicalMessagesFromAnthropicContent("user", []any{map[string]any{
		"type":  "document",
		"title": "notes.txt",
		"source": map[string]any{
			"type":       "base64",
			"media_type": "text/plain",
			"data":       "aGVsbG8=",
		},
	}})
	if len(messages) != 1 || len(messages[0].Content) != 1 {
		t.Fatalf("anthropic document messages = %#v", messages)
	}
	part := messages[0].Content[0]
	if part.Type != "input_file" || part.FileName != "notes.txt" || part.FileData != "data:text/plain;base64,aGVsbG8=" {
		t.Fatalf("anthropic document part = %#v", part)
	}
}

func TestCanonicalChatRequestEncodesToResponsesAndBackToChat(t *testing.T) {
	t.Parallel()

	chat := map[string]any{
		"model":                 "alias",
		"stream":                true,
		"messages":              []any{map[string]any{"role": "system", "content": "Be concise."}, map[string]any{"role": "user", "content": "Hello"}},
		"max_completion_tokens": 64,
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
	}
	canonical, err := canonicalRequestFromOpenAIChatPayload(chat, "alias")
	if err != nil {
		t.Fatalf("canonicalRequestFromOpenAIChatPayload returned error: %v", err)
	}
	responsesPayload, err := encodeCanonicalRequestToOpenAIResponses(canonical, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4"},
	})
	if err != nil {
		t.Fatalf("encodeCanonicalRequestToOpenAIResponses returned error: %v", err)
	}
	if responsesPayload["instructions"] != "Be concise." {
		t.Fatalf("instructions = %#v, want Be concise.", responsesPayload["instructions"])
	}
	if responsesPayload["max_output_tokens"] != 64 {
		t.Fatalf("max_output_tokens = %#v, want 64", responsesPayload["max_output_tokens"])
	}

	backCanonical, err := canonicalRequestFromOpenAIResponsesPayload(responsesPayload, "gpt-5.4")
	if err != nil {
		t.Fatalf("canonicalRequestFromOpenAIResponsesPayload returned error: %v", err)
	}
	chatPayload := encodeCanonicalRequestToOpenAIChat(backCanonical, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "upstream-chat"},
	})
	if chatPayload["model"] != "upstream-chat" {
		t.Fatalf("model = %#v, want upstream-chat", chatPayload["model"])
	}
	messages, ok := chatPayload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v, want system + user", chatPayload["messages"])
	}
	tools, ok := chatPayload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one function tool", chatPayload["tools"])
	}
}

func TestCanonicalChatAssistantReasoningRoundTripsToAnthropicMessages(t *testing.T) {
	t.Parallel()

	canonical, err := canonicalRequestFromOpenAIChatPayload(map[string]any{
		"model": "alias",
		"messages": []any{
			map[string]any{
				"role":               "assistant",
				"content":            "Answer",
				"reasoning_content":  "private chain",
				"thinking_signature": "sig_123",
			},
		},
	}, "alias")
	if err != nil {
		t.Fatalf("canonicalRequestFromOpenAIChatPayload returned error: %v", err)
	}
	payload, err := encodeCanonicalRequestToAnthropicMessages(canonical, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-pro"},
	})
	if err != nil {
		t.Fatalf("encodeCanonicalRequestToAnthropicMessages returned error: %v", err)
	}
	messages := payload["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["type"] != "thinking" || content[0].(map[string]any)["thinking"] != "private chain" {
		t.Fatalf("thinking block was not preserved: %#v", content)
	}
}

func TestCanonicalResponsesReasoningAndThinkingFieldsRoundTripToCanonical(t *testing.T) {
	t.Parallel()

	response, err := canonicalResponseFromOpenAIChatBody([]byte(`{
		"id":"chatcmpl_reasoning",
		"created":123,
		"model":"mimo-v2.5-pro",
		"choices":[{"index":0,"message":{"role":"assistant","content":"Hello","reasoning_content":"private chain","thinking_signature":"sig_123"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}
	}`))
	if err != nil {
		t.Fatalf("canonicalResponseFromOpenAIChatBody returned error: %v", err)
	}
	if len(response.Output) == 0 || len(response.Output[0].Thinking) == 0 {
		t.Fatalf("expected thinking output, got %#v", response.Output)
	}
	if response.Output[0].Thinking[0].Thinking != "private chain" {
		t.Fatalf("thinking text = %#v, want private chain", response.Output[0].Thinking[0].Thinking)
	}
}

func TestConvertRequestBetweenProtocolsUsesRegisteredSpecs(t *testing.T) {
	t.Parallel()

	payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, map[string]any{
		"model":    "alias",
		"messages": []any{map[string]any{"role": "user", "content": "Hello"}},
		"stream":   true,
	}, "alias", routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4"}})
	if err != nil {
		t.Fatalf("convertRequestBetweenProtocols returned error: %v", err)
	}
	if payload["model"] != "gpt-5.4" {
		t.Fatalf("model = %#v, want gpt-5.4", payload["model"])
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v, want one message", payload["input"])
	}
}

func TestConvertResponseBetweenProtocolsUsesRegisteredSpecs(t *testing.T) {
	t.Parallel()

	body, usage, err := convertResponseBetweenProtocols(canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, []byte(`{
		"id": "chatcmpl_test",
		"created": 123,
		"model": "gpt-5.4",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "Hello"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5}
	}`), responseConversionOptions{})
	if err != nil {
		t.Fatalf("convertResponseBetweenProtocols returned error: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response["object"] != "response" {
		t.Fatalf("object = %#v, want response", response["object"])
	}
	if usage.TotalTokens != 5 {
		t.Fatalf("usage total = %d, want 5", usage.TotalTokens)
	}
}

func TestEncodeCanonicalResponseAsResponsesUsesUniqueFallbackIDs(t *testing.T) {
	t.Parallel()

	body, _, err := encodeCanonicalResponseAsResponses(canonicalResponse{
		ID: "resp_fallback",
		Output: []canonicalOutputItem{
			{Type: "message", ID: "item_message_one", Text: "one"},
			{Type: "message", ID: "item_message_two", Text: "two"},
			{Type: "image_generation_call", ID: "item_image_one", Result: "one"},
			{Type: "image_generation_call", ID: "item_image_two", Result: "two"},
		},
	}, responseConversionOptions{})
	if err != nil {
		t.Fatalf("encodeCanonicalResponseAsResponses returned error: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode Responses body: %v", err)
	}
	output := response["output"].([]any)
	ids := make(map[string]struct{}, len(output))
	for _, raw := range output {
		item := raw.(map[string]any)
		id := anyString(item["id"])
		if _, exists := ids[id]; exists {
			t.Fatalf("duplicate output item ID %q in %#v", id, output)
		}
		ids[id] = struct{}{}
	}
	for index, want := range []string{"msg_resp_fallback_0", "msg_resp_fallback_1", "ig_resp_fallback_2", "ig_resp_fallback_3"} {
		if got := anyString(output[index].(map[string]any)["id"]); got != want {
			t.Fatalf("output[%d] ID = %q, want %q", index, got, want)
		}
	}
}

func TestEncodeCanonicalResponseAsResponsesUsesFallbackIDsForCallsWithoutCallIDs(t *testing.T) {
	t.Parallel()

	body, _, err := encodeCanonicalResponseAsResponses(canonicalResponse{
		ID: "resp_call_fallback",
		Output: []canonicalOutputItem{
			{Type: "function_call", ID: "fc_", Name: "lookup", Arguments: "{}"},
			{Type: "function_call", ID: "fc_", Name: "apply_patch", Arguments: "{}"},
		},
	}, responseConversionOptions{CustomTools: map[string]struct{}{"apply_patch": {}}})
	if err != nil {
		t.Fatalf("encodeCanonicalResponseAsResponses returned error: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode Responses body: %v", err)
	}
	output := response["output"].([]any)
	for index, want := range []string{"fc_resp_call_fallback_0", "ctc_resp_call_fallback_1"} {
		if got := anyString(output[index].(map[string]any)["id"]); got != want {
			t.Fatalf("output[%d] ID = %q, want %q", index, got, want)
		}
	}
}

func TestConvertAntigravityImageResponseThroughCanonical(t *testing.T) {
	t.Parallel()

	body, usage, err := convertResponseBetweenProtocols(canonicalProtocolAntigravity, canonicalProtocolOpenAIImages, []byte(`{
		"candidates": [{
			"content": {"parts": [{"inlineData": {"mimeType": "image/png", "data": "ZmFrZQ=="}}]}
		}],
		"usageMetadata": {"promptTokenCount": 4, "candidatesTokenCount": 6, "totalTokenCount": 10}
	}`), responseConversionOptions{ImageResponseFormat: "url"})
	if err != nil {
		t.Fatalf("convertResponseBetweenProtocols returned error: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	data := response["data"].([]any)
	first := data[0].(map[string]any)
	if first["url"] != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("url = %#v", first["url"])
	}
	if usage.ImageCount != 1 || usage.TotalTokens != 10 {
		t.Fatalf("usage = %#v, want one image and 10 total tokens", usage)
	}
}

func TestEncodeCanonicalResponseAsImagesSupportsB64AndURLFormats(t *testing.T) {
	t.Parallel()

	response := canonicalResponse{
		CreatedAt: 1710000000,
		Output: []canonicalOutputItem{
			{
				Type:          "image_generation_call",
				Result:        " ZmFrZQ== ",
				RevisedPrompt: "clean prompt",
				OutputFormat:  "webp",
				Quality:       "high",
				Size:          "1024x1024",
				Background:    "transparent",
			},
		},
		Usage: gatewayUsage{
			PromptTokens:      12,
			CompletionTokens:  20,
			TotalTokens:       32,
			InputTextTokens:   7,
			InputImageTokens:  5,
			OutputImageTokens: 20,
		},
	}

	body, usage, err := encodeCanonicalResponseAsImagesWithFormat(response, "")
	if err != nil {
		t.Fatalf("encodeCanonicalResponseAsImagesWithFormat returned error: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode b64 image response: %v", err)
	}
	data := envelope["data"].([]any)
	image := data[0].(map[string]any)
	if image["b64_json"] != "ZmFrZQ==" || image["revised_prompt"] != "clean prompt" {
		t.Fatalf("unexpected b64 image payload: %#v", image)
	}
	if envelope["output_format"] != "webp" || envelope["quality"] != "high" || envelope["size"] != "1024x1024" || envelope["background"] != "transparent" {
		t.Fatalf("unexpected image envelope metadata: %#v", envelope)
	}
	if usage.ImageCount != 1 || usage.TotalTokens != 32 || usage.InputTextTokens != 7 || usage.OutputImageTokens != 20 {
		t.Fatalf("unexpected image usage: %#v", usage)
	}

	body, usage, err = encodeCanonicalResponseAsImagesWithFormat(response, "url")
	if err != nil {
		t.Fatalf("encodeCanonicalResponseAsImagesWithFormat returned error: %v", err)
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode URL image response: %v", err)
	}
	data = envelope["data"].([]any)
	image = data[0].(map[string]any)
	if image["url"] != "data:image/webp;base64,ZmFrZQ==" {
		t.Fatalf("unexpected URL image payload: %#v", image)
	}
	if usage.ImageCount != 1 || usage.TotalTokens != 32 {
		t.Fatalf("unexpected URL image usage: %#v", usage)
	}
}

func TestEncodeCanonicalResponseAsImagesRejectsMissingImageOutput(t *testing.T) {
	t.Parallel()

	if _, _, err := encodeCanonicalResponseAsImagesWithFormat(canonicalResponse{Output: []canonicalOutputItem{{Type: "message", Text: "not an image"}}}, ""); err == nil {
		t.Fatal("expected missing image_generation_call output to fail")
	}
}

func TestCanonicalChatEncodingHelpers(t *testing.T) {
	t.Parallel()

	jsonSchema := map[string]any{"type": "json_schema", "name": "answer", "schema": map[string]any{"type": "object"}}
	responseFormat := encodeCanonicalTextFormatAsChatResponseFormat(jsonSchema).(map[string]any)
	if responseFormat["type"] != "json_schema" || responseFormat["json_schema"] == nil {
		t.Fatalf("json_schema response format = %#v", responseFormat)
	}
	jsonObject := encodeCanonicalTextFormatAsChatResponseFormat(map[string]any{"type": "json_object"}).(map[string]any)
	if jsonObject["type"] != "json_object" {
		t.Fatalf("json_object response format = %#v", jsonObject)
	}
	if got := encodeCanonicalTextFormatAsChatResponseFormat(map[string]any{"type": "text"}); got != nil {
		t.Fatalf("unsupported response format = %#v, want nil", got)
	}

	toolCalls := encodeCanonicalToolCallsAsChat([]canonicalToolCall{{
		ID:        "call_123",
		Name:      "lookup",
		Arguments: `{"city":"Tokyo"}`,
		Metadata:  map[string]any{"thoughtSignature": "sig_123"},
	}})
	if len(toolCalls) != 1 {
		t.Fatalf("toolCalls = %#v, want one", toolCalls)
	}
	call := toolCalls[0].(map[string]any)
	function := call["function"].(map[string]any)
	if call["id"] != "call_123" || call["type"] != "function" || call["thought_signature"] != "sig_123" {
		t.Fatalf("unexpected chat tool call metadata: %#v", call)
	}
	if function["name"] != "lookup" || function["arguments"] != `{"city":"Tokyo"}` {
		t.Fatalf("unexpected chat tool function: %#v", function)
	}

	if got := normalizeImageURLString(map[string]any{"image_url": map[string]any{"url": " https://example.test/a.png "}}); got != "https://example.test/a.png" {
		t.Fatalf("nested image url = %q", got)
	}
	if got := normalizeImageURLString(map[string]any{"image_url": " https://example.test/b.png "}); got != "https://example.test/b.png" {
		t.Fatalf("string image url = %q", got)
	}
}

func TestCanonicalCodexImageRequestEncoding(t *testing.T) {
	t.Parallel()

	canonical := canonicalRequestFromOpenAIImagesPayload(map[string]any{
		"model":           "gpt-image-2",
		"prompt":          "draw a cat",
		"n":               2,
		"size":            "1024x1024",
		"quality":         "high",
		"metadata":        map[string]any{"request": "abc"},
		"response_format": "b64_json",
	}, "gpt-image-2")
	payload := applyCodexRequestPolicy(encodeCanonicalImageRequestToCodexResponses(canonical, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2"},
	}), routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2"}})

	if payload["model"] != codexImageToolHostModel {
		t.Fatalf("model = %#v, want %q", payload["model"], codexImageToolHostModel)
	}
	if payload["stream"] != true {
		t.Fatalf("stream = %#v, want true", payload["stream"])
	}
	if _, ok := payload["n"]; ok {
		t.Fatalf("n should be dropped from Codex responses payload")
	}
	tools := payload["tools"].([]any)
	tool := tools[0].(map[string]any)
	if tool["model"] != "gpt-image-2" || tool["size"] != "1024x1024" || tool["quality"] != "high" {
		t.Fatalf("unexpected image tool: %#v", tool)
	}
}

func TestCanonicalAntigravityEncodingMapsContentAndGenerationConfig(t *testing.T) {
	t.Parallel()

	canonical, err := canonicalRequestFromOpenAIResponsesPayload(map[string]any{
		"model":             "alias",
		"instructions":      "Be concise.",
		"input":             "Hello",
		"max_output_tokens": 128,
		"temperature":       0.2,
	}, "alias")
	if err != nil {
		t.Fatalf("canonicalRequestFromOpenAIResponsesPayload returned error: %v", err)
	}

	payload := encodeCanonicalRequestToAntigravityGemini(canonical, "gemini-3-pro")
	if payload["model"] != "gemini-3-pro" {
		t.Fatalf("model = %#v, want gemini-3-pro", payload["model"])
	}
	if _, ok := payload["systemInstruction"].(map[string]any); !ok {
		t.Fatalf("expected systemInstruction, got %#v", payload["systemInstruction"])
	}
	config := payload["generationConfig"].(map[string]any)
	if config["maxOutputTokens"] != 128 || config["temperature"] != 0.2 {
		t.Fatalf("unexpected generationConfig: %#v", config)
	}
	contents := payload["contents"].([]any)
	if !strings.Contains(strings.TrimSpace(anyString(contents[0].(map[string]any)["role"])), "user") {
		t.Fatalf("unexpected contents: %#v", contents)
	}
}

func TestCanonicalImageRequest_N_Field(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload map[string]any
		wantN   int
	}{
		{
			name:    "n present as int",
			payload: map[string]any{"model": "dall-e-3", "prompt": "cat", "n": 2},
			wantN:   2,
		},
		{
			name:    "n present as float64",
			payload: map[string]any{"model": "dall-e-3", "prompt": "cat", "n": float64(3)},
			wantN:   3,
		},
		{
			name:    "n absent",
			payload: map[string]any{"model": "dall-e-3", "prompt": "cat"},
			wantN:   0,
		},
		{
			name:    "n zero",
			payload: map[string]any{"model": "dall-e-3", "prompt": "cat", "n": 0},
			wantN:   0,
		},
		{
			name:    "n negative",
			payload: map[string]any{"model": "dall-e-3", "prompt": "cat", "n": -1},
			wantN:   0,
		},
		{
			name:    "n as string ignored",
			payload: map[string]any{"model": "dall-e-3", "prompt": "cat", "n": "two"},
			wantN:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := canonicalRequestFromOpenAIImagesPayload(tt.payload, "dall-e-3")
			if request.Image == nil {
				t.Fatal("expected Image to be non-nil")
			}
			if request.Image.N != tt.wantN {
				t.Errorf("Image.N = %d, want %d", request.Image.N, tt.wantN)
			}
		})
	}
}

func TestCanonicalImageRequest_PreservesPrompt(t *testing.T) {
	t.Parallel()

	request := canonicalRequestFromOpenAIImagesPayload(map[string]any{
		"model":  "dall-e-3",
		"prompt": "  a beautiful sunset  ",
		"n":      2,
		"size":   "1024x1024",
	}, "dall-e-3")

	if request.Image == nil {
		t.Fatal("expected Image to be non-nil")
	}
	if request.Image.Prompt != "a beautiful sunset" {
		t.Errorf("Image.Prompt = %q, want %q", request.Image.Prompt, "a beautiful sunset")
	}
	if request.Image.N != 2 {
		t.Errorf("Image.N = %d, want 2", request.Image.N)
	}
	if request.Image.Params["size"] != "1024x1024" {
		t.Errorf("Image.Params[size] = %v, want 1024x1024", request.Image.Params["size"])
	}
}
func TestCanonicalImageCountFromPayloadNumericBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload map[string]any
		want    int
	}{
		{name: "nil payload defaults to zero", payload: nil, want: 0},
		{name: "missing n defaults to zero", payload: map[string]any{}, want: 0},
		{name: "float32 value is truncated", payload: map[string]any{"n": float32(2.9)}, want: 2},
		{name: "non-positive float32 defaults to zero", payload: map[string]any{"n": float32(-1)}, want: 0},
		{name: "int64 value is accepted", payload: map[string]any{"n": int64(4)}, want: 4},
		{name: "non-positive int64 defaults to zero", payload: map[string]any{"n": int64(0)}, want: 0},
		{name: "json number integer is accepted", payload: map[string]any{"n": json.Number("5")}, want: 5},
		{name: "json number decimal defaults to zero", payload: map[string]any{"n": json.Number("1.5")}, want: 0},
		{name: "string value defaults to zero", payload: map[string]any{"n": "6"}, want: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := canonicalImageCountFromPayload(tt.payload); got != tt.want {
				t.Fatalf("canonicalImageCountFromPayload() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCanonicalImageReferencesFromPayloadHandlesImagesImageAndEmptyValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload map[string]any
		want    []canonicalImageReference
	}{
		{
			name: "images array keeps image_url and file_id entries and skips empty values",
			payload: map[string]any{
				"images": []any{
					map[string]any{"type": "input_image", "image_url": map[string]any{"url": " https://example.test/a.png "}},
					map[string]any{"file_id": " file_123 "},
					map[string]any{"image_url": "   ", "file_id": "   "},
					"aWZha2U=",
				},
				"image": map[string]any{"image_url": "https://example.test/fallback.png"},
			},
			want: []canonicalImageReference{
				{ImageURL: "https://example.test/a.png"},
				{FileID: "file_123"},
				{ImageURL: "data:image/png;base64,aWZha2U="},
			},
		},
		{
			name: "image key is used when images has no valid references",
			payload: map[string]any{
				"images": []any{
					map[string]any{"image_url": "   "},
					nil,
				},
				"image": map[string]any{"image_url": " https://example.test/single.png "},
			},
			want: []canonicalImageReference{{ImageURL: "https://example.test/single.png"}},
		},
		{
			name: "single image file id is accepted",
			payload: map[string]any{
				"image": map[string]any{"file_id": " file_single "},
			},
			want: []canonicalImageReference{{FileID: "file_single"}},
		},
		{
			name:    "nil payload returns nil",
			payload: nil,
			want:    nil,
		},
		{
			name:    "empty payload returns nil",
			payload: map[string]any{},
			want:    nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := canonicalImageReferencesFromPayload(tt.payload)
			assertCanonicalImageReferencesEqual(t, got, tt.want)
		})
	}
}

func TestCanonicalImageReferencesFromPayloadHandlesTypedMapArrays(t *testing.T) {
	t.Parallel()

	got := canonicalImageReferencesFromPayload(map[string]any{
		"images": []map[string]any{
			{"image_url": map[string]any{"url": " https://example.test/typed.png "}},
			{"file_id": " file_typed "},
			{},
		},
	})
	want := []canonicalImageReference{
		{ImageURL: "https://example.test/typed.png"},
		{FileID: "file_typed"},
	}
	assertCanonicalImageReferencesEqual(t, got, want)
}

func TestCanonicalImageMaskFromPayloadHandlesReferencesAndEmptyValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload map[string]any
		want    *canonicalImageReference
	}{
		{
			name:    "mask image url is trimmed",
			payload: map[string]any{"mask": map[string]any{"image_url": map[string]any{"url": " https://example.test/mask.png "}}},
			want:    &canonicalImageReference{ImageURL: "https://example.test/mask.png"},
		},
		{
			name:    "mask file id is accepted",
			payload: map[string]any{"mask": map[string]any{"file_id": " mask_file "}},
			want:    &canonicalImageReference{FileID: "mask_file"},
		},
		{
			name:    "empty mask returns nil",
			payload: map[string]any{"mask": map[string]any{"image_url": " ", "file_id": " "}},
			want:    nil,
		},
		{
			name:    "string mask is wrapped as data url",
			payload: map[string]any{"mask": "bWFzaw=="},
			want:    &canonicalImageReference{ImageURL: "data:image/png;base64,bWFzaw=="},
		},
		{
			name:    "nil payload returns nil",
			payload: nil,
			want:    nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := canonicalImageMaskFromPayload(tt.payload)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("canonicalImageMaskFromPayload() = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("canonicalImageMaskFromPayload() = nil, want %#v", *tt.want)
			}
			assertCanonicalImageReferencesEqual(t, []canonicalImageReference{*got}, []canonicalImageReference{*tt.want})
		})
	}
}

func TestImagesWithFormatDefaultsB64AndFallsBackUsageDetails(t *testing.T) {
	t.Parallel()

	body, usage, err := encodeCanonicalResponseAsImagesWithFormat(canonicalResponse{
		CreatedAt: 1710000001,
		Output: []canonicalOutputItem{
			{Type: "message", Text: "skip non-image output"},
			{Type: "image_generation_call", Result: "   "},
			{Type: " image_generation_call ", Result: " YmFzZTY0 "},
		},
		Usage: gatewayUsage{
			PromptTokens:     9,
			CompletionTokens: 11,
			TotalTokens:      20,
			InputImageTokens: 4,
		},
	}, "  ")
	if err != nil {
		t.Fatalf("encodeCanonicalResponseAsImagesWithFormat returned error: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode image response: %v", err)
	}
	data := envelope["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("data length = %d, want 1", len(data))
	}
	image := data[0].(map[string]any)
	if image["b64_json"] != "YmFzZTY0" {
		t.Fatalf("image payload = %#v, want trimmed b64_json", image)
	}
	if _, ok := image["url"]; ok {
		t.Fatalf("default format should not include url: %#v", image)
	}

	if usage.ImageCount != 1 || usage.TotalTokens != 20 {
		t.Fatalf("usage count/tokens = %#v, want one image and 20 total tokens", usage)
	}
	if usage.InputTextTokens != 5 || usage.InputImageTokens != 4 || usage.OutputImageTokens != 11 {
		t.Fatalf("usage detail fallback = %#v, want text=5 inputImage=4 outputImage=11", usage)
	}
}

func TestImagesWithFormatURLNormalizesMimeTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		outputFormat string
		wantURL      string
	}{
		{name: "empty output format defaults to png", outputFormat: "", wantURL: "data:image/png;base64,AAA="},
		{name: "bare output format gains image prefix", outputFormat: "jpeg", wantURL: "data:image/jpeg;base64,AAA="},
		{name: "image mime type is kept", outputFormat: "image/webp", wantURL: "data:image/webp;base64,AAA="},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, usage, err := encodeCanonicalResponseAsImagesWithFormat(canonicalResponse{
				Output: []canonicalOutputItem{{
					Type:         "image_generation_call",
					Result:       " AAA= ",
					OutputFormat: tt.outputFormat,
				}},
			}, " URL ")
			if err != nil {
				t.Fatalf("encodeCanonicalResponseAsImagesWithFormat returned error: %v", err)
			}

			var envelope map[string]any
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatalf("decode URL image response: %v", err)
			}
			data := envelope["data"].([]any)
			image := data[0].(map[string]any)
			if image["url"] != tt.wantURL {
				t.Fatalf("url = %#v, want %q", image["url"], tt.wantURL)
			}
			if _, ok := image["b64_json"]; ok {
				t.Fatalf("url format should not include b64_json: %#v", image)
			}
			if usage.ImageCount != 1 {
				t.Fatalf("usage image count = %d, want 1", usage.ImageCount)
			}
		})
	}
}

func TestNormalizeImageURLStringHandlesMissingAndNonStringURLs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		part map[string]any
		want string
	}{
		{name: "missing image url returns empty string", part: map[string]any{}, want: ""},
		{name: "nested map without string url returns empty string", part: map[string]any{"image_url": map[string]any{"url": 123}}, want: ""},
		{name: "direct map with url string is normalized", part: map[string]any{"image_url": map[string]any{"url": " https://example.test/nested.png "}}, want: "https://example.test/nested.png"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeImageURLString(tt.part); got != tt.want {
				t.Fatalf("normalizeImageURLString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPayloadModelAndBoolFromMapHelpersTrimAndTypeCheck(t *testing.T) {
	t.Parallel()

	if got := stringFromPayloadModel(map[string]any{"model": "  gpt-image-2  "}); got != "gpt-image-2" {
		t.Fatalf("stringFromPayloadModel() = %q, want gpt-image-2", got)
	}
	if got := stringFromPayloadModel(map[string]any{"model": 123}); got != "" {
		t.Fatalf("non-string model = %q, want empty string", got)
	}
	if got := boolFromMap(map[string]any{"stream": true}, "stream"); !got {
		t.Fatal("boolFromMap() = false, want true for bool value")
	}
	if got := boolFromMap(map[string]any{"stream": "true"}, "stream"); got {
		t.Fatal("boolFromMap() = true, want false for non-bool value")
	}
	if got := boolFromMap(map[string]any{}, "stream"); got {
		t.Fatal("boolFromMap() = true, want false for missing key")
	}
}

func assertCanonicalImageReferencesEqual(t *testing.T, got, want []canonicalImageReference) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("references length = %d, want %d; got %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("references[%d] = %#v, want %#v; all got %#v", i, got[i], want[i], got)
		}
	}
}
