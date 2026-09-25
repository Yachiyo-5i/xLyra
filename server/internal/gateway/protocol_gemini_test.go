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

	routeengine "xlyra/server/internal/router"
)

func TestCanonicalRequestFromGoogleGeminiPayload(t *testing.T) {
	payload := map[string]any{
		"systemInstruction": map[string]any{"parts": []any{map[string]any{"text": "be concise"}}},
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}},
			map[string]any{"role": "model", "parts": []any{
				map[string]any{"text": "private", "thought": true, "thoughtSignature": "sig"},
				map[string]any{"functionCall": map[string]any{"id": "call_1", "name": "lookup", "args": map[string]any{"q": "x"}}, "thoughtSignature": "sig"},
			}},
		},
		"tools": []any{map[string]any{"functionDeclarations": []any{map[string]any{
			"name": "lookup", "description": "find data", "parameters": map[string]any{"type": "object"},
		}}}},
		"toolConfig": map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}},
		"generationConfig": map[string]any{
			"temperature": 0.2, "topP": 0.8, "maxOutputTokens": 100,
			"responseMimeType": "application/json", "thinkingConfig": map[string]any{"includeThoughts": true},
		},
	}
	request, err := canonicalRequestFromGoogleGeminiPayload(payload, "gemini-2.5-pro", true)
	if err != nil {
		t.Fatal(err)
	}
	if request.SourceProtocol != canonicalProtocolGoogleGemini || !request.Stream || request.Instructions != "be concise" {
		t.Fatalf("request metadata = %#v", request)
	}
	if request.Params["top_p"] != 0.8 || request.Params["max_output_tokens"] != 100 {
		t.Fatalf("generation config was not mapped: %#v", request.Params)
	}
	if len(request.Messages) != 2 || len(request.Messages[1].Thinking) != 1 || len(request.Messages[1].ToolCalls) != 1 {
		t.Fatalf("messages = %#v", request.Messages)
	}
	if request.Messages[1].ToolCalls[0].Metadata["thoughtSignature"] != "sig" {
		t.Fatalf("tool metadata = %#v", request.Messages[1].ToolCalls[0].Metadata)
	}
	if len(request.Tools) != 1 || request.Tools[0].Name != "lookup" {
		t.Fatalf("tools = %#v", request.Tools)
	}
}

func TestGoogleGeminiResponseRoundTripPreservesUsage(t *testing.T) {
	response := canonicalResponse{
		ID:           "resp_1",
		Model:        "gemini-2.5-pro",
		FinishReason: "stop",
		Output: []canonicalOutputItem{{
			Type:     "message",
			Role:     "assistant",
			Text:     "answer",
			Thinking: []canonicalThinkingBlock{{Thinking: "private", Signature: "sig"}},
		}, {
			Type:      "function_call",
			ID:        "call_1",
			Name:      "lookup",
			Arguments: `{"q":"x"}`,
			Metadata:  map[string]any{"thoughtSignature": "sig"},
		}},
		Usage: gatewayUsage{PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7, ReasoningTokens: 1},
	}
	body, usage, err := encodeCanonicalResponseAsGoogleGemini(response, responseConversionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens != 7 {
		t.Fatalf("usage = %#v", usage)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	metadata := payload["usageMetadata"].(map[string]any)
	if metadata["totalTokenCount"] != float64(7) || metadata["thoughtsTokenCount"] != float64(1) {
		t.Fatalf("usageMetadata = %#v", metadata)
	}
	encoded := string(body)
	for _, want := range []string{"\"thought\":true", "\"thoughtSignature\":\"sig\"", "\"functionCall\""} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("response lost %s: %s", want, encoded)
		}
	}
	decoded, err := decodeCanonicalResponseFromAntigravityBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Usage.TotalTokens != 7 || len(decoded.Output) == 0 {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestGoogleGeminiRequestEncoderPreservesNativeGenerationConfig(t *testing.T) {
	request, err := canonicalRequestFromGoogleGeminiPayload(map[string]any{
		"contents":         []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "draw"}}}},
		"generationConfig": map[string]any{"responseModalities": []any{"TEXT", "IMAGE"}, "imageConfig": map[string]any{"aspectRatio": "16:9"}},
	}, "gemini-2.5-flash", false)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := encodeCanonicalRequestToGoogleGemini(request, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-flash"}})
	if err != nil {
		t.Fatal(err)
	}
	config := payload["generationConfig"].(map[string]any)
	if config["responseModalities"] == nil || config["imageConfig"] == nil {
		t.Fatalf("generationConfig = %#v", config)
	}
}

func TestGoogleProtocolValidatesCanonicalChatRequestWithoutRecursing(t *testing.T) {
	payload := map[string]any{
		"model": "alias",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	canonical, err := canonicalRequestFromOpenAIChatPayload(payload, "alias")
	if err != nil {
		t.Fatal(err)
	}
	protocol := newGoogleProtocolAdapter(gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Canonical:      &canonical,
		Payload:        payload,
	})
	if _, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Canonical:      &canonical,
		Payload:        payload,
	}, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-pro"}}); err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
}

func TestGeminiImageRequestAndOpenAIImageResponseConversion(t *testing.T) {
	request, err := canonicalRequestFromGoogleGeminiPayload(map[string]any{
		"contents":         []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "a red kite"}}}},
		"generationConfig": map[string]any{"responseModalities": []any{"IMAGE"}, "candidateCount": 2},
	}, "gemini-image", false)
	if err != nil {
		t.Fatal(err)
	}
	if request.Image == nil || request.Image.N != 2 || request.Image.Prompt != "a red kite" {
		t.Fatalf("image request = %#v", request.Image)
	}
	openAIRequest := encodeCanonicalRequestToOpenAIImages(request, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gpt-image-1"}})
	if openAIRequest["prompt"] != "a red kite" || openAIRequest["n"] != 2 || openAIRequest["response_format"] != "b64_json" {
		t.Fatalf("OpenAI image request = %#v", openAIRequest)
	}
	body := []byte(`{"created":123,"data":[{"b64_json":"aGVsbG8="}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`)
	converted, usage, err := convertResponseBetweenProtocols(canonicalProtocolOpenAIImages, canonicalProtocolGoogleGemini, body, responseConversionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if usage.TotalTokens != 5 || !strings.Contains(string(converted), `"inlineData"`) {
		t.Fatalf("converted image response = %s usage=%#v", converted, usage)
	}
}

func TestGoogleGeminiStreamEncoderEmitsUsageMetadata(t *testing.T) {
	recorder := httptest.NewRecorder()
	capture := streamCaptureState{}
	encoder := newGoogleGeminiStreamEncoder(canonicalStreamOptions{}, recorder, &capture)
	for _, event := range []canonicalStreamEvent{
		{Type: canonicalStreamEventCreated, ID: "resp_1", Model: "gemini-2.5-pro"},
		{Type: canonicalStreamEventTextDelta, Delta: "hello"},
		{Type: canonicalStreamEventUsage, Usage: gatewayUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}},
		{Type: canonicalStreamEventCompleted, FinishReason: "stop"},
	} {
		if err := encoder.EncodeEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if !capture.streamCompleted || capture.usage.TotalTokens != 5 {
		t.Fatalf("capture = %#v", capture)
	}
	if !strings.Contains(recorder.Body.String(), `"usageMetadata":{"candidatesTokenCount":3,"promptTokenCount":2,"totalTokenCount":5}`) {
		t.Fatalf("stream body did not contain usage metadata: %s", recorder.Body.String())
	}
}

func TestGeminiConversionRejectsUnrepresentableFields(t *testing.T) {
	request, err := canonicalRequestFromGoogleGeminiPayload(map[string]any{
		"contents":       []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hello"}}}},
		"safetySettings": []any{map[string]any{"category": "HARM_CATEGORY_HATE_SPEECH"}},
	}, "gemini-2.5-pro", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleGeminiConversion(request, canonicalProtocolOpenAIChat); err == nil {
		t.Fatal("expected unsupported Gemini field to be rejected")
	}
}

func TestGeminiImageConversionRejectsTextTargetAndUnsupportedImageFields(t *testing.T) {
	request, err := canonicalRequestFromGoogleGeminiPayload(map[string]any{
		"contents":         []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "draw"}}}},
		"generationConfig": map[string]any{"responseModalities": []any{"IMAGE"}},
	}, "gemini-image", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleGeminiConversion(request, canonicalProtocolOpenAIChat); err == nil {
		t.Fatal("expected IMAGE modality to be rejected for OpenAI Chat")
	}

	imageRequest := canonicalRequestFromOpenAIImagesPayload(map[string]any{
		"model":   "gpt-image-1",
		"prompt":  "edit",
		"quality": "hd",
		"mask":    "data:image/png;base64,bWFzaw==",
	}, "gpt-image-1")
	if err := validateGoogleGeminiConversion(imageRequest, canonicalProtocolGoogleGemini); err == nil {
		t.Fatal("expected unsupported OpenAI image fields to be rejected for Gemini")
	}
}

func TestCrossProtocolToGeminiStripsIgnorableFieldsAndKeepsToolCalls(t *testing.T) {
	request, err := canonicalRequestFromOpenAIChatPayload(map[string]any{
		"model": "gpt-alias",
		"messages": []any{
			map[string]any{"role": "user", "content": "lookup"},
			map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{
				"id": "call_1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": `{"q":"x"}`},
			}}},
		},
		"user":                  "client-user",
		"metadata":              map[string]any{"trace": "x"},
		"store":                 true,
		"service_tier":          "default",
		"parallel_tool_calls":   true,
		"safety_identifier":     "safe",
		"prompt_cache_key":      "cache-1",
		"prompt_cache_retention": "24h",
	}, "gpt-alias")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleGeminiConversion(request, canonicalProtocolGoogleGemini); err != nil {
		t.Fatalf("ignorable OpenAI Chat fields should be stripped, got %v", err)
	}
	encoded, err := encodeCanonicalRequestToGoogleGemini(request, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-pro"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"user", "metadata", "store", "service_tier", "parallel_tool_calls"} {
		if _, ok := encoded[key]; ok {
			t.Fatalf("%s should not appear in Gemini payload: %#v", key, encoded[key])
		}
	}
	contents := encoded["contents"].([]any)
	modelParts := contents[1].(map[string]any)["parts"].([]any)
	if _, ok := modelParts[0].(map[string]any)["functionCall"]; !ok {
		t.Fatalf("unsigned cross-protocol tool call was dropped: %#v", modelParts)
	}
}

func TestCrossProtocolToGeminiMapsReasoningEffortToThinkingConfig(t *testing.T) {
	chat, err := canonicalRequestFromOpenAIChatPayload(map[string]any{
		"model":            "gpt-alias",
		"messages":         []any{map[string]any{"role": "user", "content": "think"}},
		"reasoning_effort": "high",
		"user":             "client-user",
	}, "gpt-alias")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := encodeCanonicalRequestToGoogleGemini(chat, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-pro"}})
	if err != nil {
		t.Fatal(err)
	}
	config := encoded["generationConfig"].(map[string]any)
	thinking := config["thinkingConfig"].(map[string]any)
	if thinking["thinkingLevel"] != "HIGH" || thinking["includeThoughts"] != true || thinking["budgetTokens"] != 24576 {
		t.Fatalf("thinkingConfig = %#v, want high effort mapping", thinking)
	}

	responses, err := canonicalRequestFromOpenAIResponsesPayload(map[string]any{
		"model":     "gpt-alias",
		"input":     "think",
		"reasoning": map[string]any{"effort": "low"},
		"metadata":  map[string]any{"trace": "x"},
	}, "gpt-alias")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = encodeCanonicalRequestToGoogleGemini(responses, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-pro"}})
	if err != nil {
		t.Fatal(err)
	}
	thinking = encoded["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
	if thinking["thinkingLevel"] != "LOW" || thinking["budgetTokens"] != 1024 {
		t.Fatalf("responses thinkingConfig = %#v, want low effort mapping", thinking)
	}

	anthropic, err := canonicalRequestFromAnthropicMessagesPayload(map[string]any{
		"model":         "claude",
		"max_tokens":    64,
		"messages":      []any{map[string]any{"role": "user", "content": "think"}},
		"output_config": map[string]any{"effort": "medium"},
		"metadata":      map[string]any{"user_id": "u1"},
	}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = encodeCanonicalRequestToGoogleGemini(anthropic, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-pro"}})
	if err != nil {
		t.Fatal(err)
	}
	thinking = encoded["generationConfig"].(map[string]any)["thinkingConfig"].(map[string]any)
	if thinking["thinkingLevel"] != "MEDIUM" || thinking["budgetTokens"] != 8192 {
		t.Fatalf("anthropic thinkingConfig = %#v, want medium effort mapping", thinking)
	}
}

func TestCrossProtocolToGeminiRejectsUnrepresentableParams(t *testing.T) {
	request, err := canonicalRequestFromOpenAIChatPayload(map[string]any{
		"model":      "gpt-alias",
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"logit_bias": map[string]any{"42": 1},
	}, "gpt-alias")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleGeminiConversion(request, canonicalProtocolGoogleGemini); err == nil {
		t.Fatal("expected logit_bias to be rejected for Gemini conversion")
	}

	responses, err := canonicalRequestFromOpenAIResponsesPayload(map[string]any{
		"model":                "gpt-alias",
		"input":                "hi",
		"previous_response_id": "resp_1",
	}, "gpt-alias")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleGeminiConversion(responses, canonicalProtocolGoogleGemini); err == nil {
		t.Fatal("expected previous_response_id to be rejected for Gemini conversion")
	}
}

func TestCrossProtocolToGeminiRejectsDroppedSourceFields(t *testing.T) {
	chat, err := canonicalRequestFromOpenAIChatPayload(map[string]any{
		"model": "gpt-alias",
		"messages": []any{map[string]any{
			"role": "assistant", "content": "hello", "audio": map[string]any{"id": "audio_1"},
		}},
	}, "gpt-alias")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleGeminiConversion(chat, canonicalProtocolGoogleGemini); err == nil {
		t.Fatal("expected unsupported OpenAI Chat message field to be rejected")
	}

	responses, err := canonicalRequestFromOpenAIResponsesPayload(map[string]any{
		"model": "gpt-alias",
		"input": []any{map[string]any{"type": "custom_tool_call", "name": "exec", "input": "pwd"}},
	}, "gpt-alias")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleGeminiConversion(responses, canonicalProtocolGoogleGemini); err == nil {
		t.Fatal("expected custom Responses tool to be rejected")
	}

	anthropic, err := canonicalRequestFromAnthropicMessagesPayload(map[string]any{
		"model":      "claude",
		"max_tokens": 32,
		"system":     []any{map[string]any{"type": "text", "text": "hello", "cache_control": map[string]any{"type": "ephemeral"}}},
		"messages":   []any{map[string]any{"role": "user", "content": "hello"}},
	}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGoogleGeminiConversion(anthropic, canonicalProtocolGoogleGemini); err == nil {
		t.Fatal("expected unsupported Anthropic system field to be rejected")
	}
}

func TestCrossProtocolToGeminiPreservesThinkingParts(t *testing.T) {
	request := canonicalRequest{
		SourceProtocol: canonicalProtocolAnthropicMessages,
		Messages:       []canonicalMessage{{Role: "assistant", Thinking: []canonicalThinkingBlock{{Thinking: "private", Signature: "sig"}}}},
	}
	encoded, err := encodeCanonicalRequestToGoogleGemini(request, routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: "gemini-2.5-pro"}})
	if err != nil {
		t.Fatal(err)
	}
	parts := encoded["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	thinking := parts[0].(map[string]any)
	if thinking["thought"] != true || thinking["thoughtSignature"] != "sig" {
		t.Fatalf("thinking part = %#v", thinking)
	}
}

func TestImageStreamsBufferAndPreserveUsage(t *testing.T) {
	t.Run("OpenAIImagesToGemini", func(t *testing.T) {
		upstream := `data: {"type":"image_generation.completed","data":[{"b64_json":"aGVsbG8="}],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}` + "\n\n"
		response := &http.Response{Body: io.NopCloser(strings.NewReader(upstream))}
		recorder := httptest.NewRecorder()
		capture, started, err := proxyOpenAIImagesStreamAsGemini(context.Background(), recorder, response, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !started || !capture.streamCompleted || capture.usage.TotalTokens != 5 {
			t.Fatalf("capture = %#v started=%v", capture, started)
		}
		if !strings.Contains(recorder.Body.String(), `"inlineData"`) || !strings.Contains(recorder.Body.String(), `"totalTokenCount":5`) {
			t.Fatalf("Gemini stream = %s", recorder.Body.String())
		}
	})

	t.Run("GeminiToOpenAIImages", func(t *testing.T) {
		upstream := `data: {"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}` + "\n\n"
		response := &http.Response{Body: io.NopCloser(strings.NewReader(upstream))}
		recorder := httptest.NewRecorder()
		capture, started, err := proxyGoogleGeminiStreamAsOpenAIImages(context.Background(), recorder, response, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !started || !capture.streamCompleted || capture.usage.TotalTokens != 5 {
			t.Fatalf("capture = %#v started=%v", capture, started)
		}
		if !strings.Contains(recorder.Body.String(), `"image_generation.completed"`) || !strings.Contains(recorder.Body.String(), `"total_tokens":5`) {
			t.Fatalf("OpenAI Images stream = %s", recorder.Body.String())
		}
	})
}
