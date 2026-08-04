package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

func TestAntigravityProtocolBuildsV1InternalPayload(t *testing.T) {
	protocol := antigravityProtocolAdapter{}
	request := gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Payload: map[string]any{
			"messages": []any{
				map[string]any{"role": "system", "content": "Be concise."},
				map[string]any{"role": "user", "content": "Hello"},
			},
			"temperature": 0.2,
			"max_tokens":  128,
		},
	}

	payload, err := protocol.BuildUpstreamPayload(request, routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		Model: routeengine.CandidateModel{UpstreamName: "gemini-3-pro"},
	})
	if err == nil {
		t.Fatal("expected missing project error without db metadata")
	}

	protocol = antigravityProtocolAdapter{}
	canonical, err := canonicalRequestFromOpenAIChatPayload(request.Payload, "alias")
	if err != nil {
		t.Fatal(err)
	}
	inner := encodeCanonicalRequestToAntigravityGemini(canonical, "gemini-3-pro")
	if inner["model"] != "gemini-3-pro" {
		t.Fatalf("model = %#v, want gemini-3-pro", inner["model"])
	}
	contents := inner["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents len = %d, want 1", len(contents))
	}
	gen := inner["generationConfig"].(map[string]any)
	if gen["maxOutputTokens"] != 128 {
		t.Fatalf("maxOutputTokens = %#v, want 128", gen["maxOutputTokens"])
	}

	_ = payload
}

func TestAntigravityProtocolTransformsGeminiResponseToChat(t *testing.T) {
	protocol := antigravityProtocolAdapter{}
	body := []byte(`{
		"response": {
			"model": "gemini-3-pro",
			"candidates": [{
				"content": {"parts": [{"text": "Hello from Antigravity"}]},
				"finishReason": "STOP"
			}],
			"usageMetadata": {
				"promptTokenCount": 5,
				"candidatesTokenCount": 7,
				"totalTokenCount": 12
			}
		}
	}`)

	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatal(err)
	}
	choices := out["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["content"] != "Hello from Antigravity" {
		t.Fatalf("content = %#v", message["content"])
	}
	if transformed.Usage.TotalTokens != 12 {
		t.Fatalf("usage total = %d, want 12", transformed.Usage.TotalTokens)
	}
}

func TestAntigravityProtocolTransformsGeminiResponseToResponses(t *testing.T) {
	protocol := antigravityProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}
	body := []byte(`{"response":{"candidates":[{"content":{"parts":[{"text":"Hi"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}}`)

	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{}, body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatal(err)
	}
	if out["object"] != "response" {
		t.Fatalf("object = %#v, want response", out["object"])
	}
	if transformed.Usage.TotalTokens != 5 {
		t.Fatalf("usage total = %d, want 5", transformed.Usage.TotalTokens)
	}
}

func TestAntigravityProtocolTransformsGeminiResponseToMessages(t *testing.T) {
	protocol := antigravityProtocolAdapter{downstreamProtocol: canonicalProtocolAnthropicMessages}
	body := []byte(`{"response":{"model":"gemini-3-pro","candidates":[{"content":{"parts":[{"text":"Hi from Gemini"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}}`)

	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{}, body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatal(err)
	}
	if out["type"] != "message" || out["role"] != "assistant" {
		t.Fatalf("unexpected Anthropic message: %#v", out)
	}
	content := out["content"].([]any)
	if got := content[0].(map[string]any)["text"]; got != "Hi from Gemini" {
		t.Fatalf("content text = %#v, want Hi from Gemini", got)
	}
	if transformed.Usage.TotalTokens != 5 {
		t.Fatalf("usage total = %d, want 5", transformed.Usage.TotalTokens)
	}
}

func TestAntigravityProtocolConvertsTextualAnthropicToolUseToMessagesToolUse(t *testing.T) {
	protocol := antigravityProtocolAdapter{downstreamProtocol: canonicalProtocolAnthropicMessages}
	body := []byte(`{"response":{"model":"gemini-3-pro","candidates":[{"content":{"parts":[{"text":"[{\"id\":\"call_123\",\"type\":\"tool_use\",\"name\":\"TaskCreate\",\"input\":{\"subject\":\"分析许言回归状态\"}}]"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}}`)

	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{}, body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatal(err)
	}
	if out["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason = %#v, want tool_use; body=%s", out["stop_reason"], string(transformed.Body))
	}
	content := out["content"].([]any)
	first := content[0].(map[string]any)
	if first["type"] != "tool_use" || first["name"] != "TaskCreate" || first["id"] != "call_123" {
		t.Fatalf("unexpected tool_use block: %#v", first)
	}
}

func TestAntigravityAnthropicToolResultPreservesToolUseID(t *testing.T) {
	canonical, err := canonicalRequestFromAnthropicMessagesPayload(map[string]any{
		"model":      "claude-opus-4-6",
		"max_tokens": 128,
		"messages": []any{
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "id": "toolu_123", "name": "read_file", "input": map[string]any{"path": "README.md"}, "thought_signature": "sig_123"},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "toolu_123", "content": "file contents"},
			}},
		},
	}, "claude-opus-4-6")
	if err != nil {
		t.Fatal(err)
	}

	payload := encodeCanonicalRequestToAntigravityGemini(canonical, "claude-opus-4-6-thinking")
	contents := payload["contents"].([]any)
	if len(contents) != 2 {
		t.Fatalf("contents len = %d, want 2: %#v", len(contents), contents)
	}
	toolUse := contents[0].(map[string]any)
	toolUseParts := toolUse["parts"].([]any)
	functionCall := toolUseParts[0].(map[string]any)["functionCall"].(map[string]any)
	if toolUse["role"] != "model" || functionCall["id"] != "toolu_123" || functionCall["name"] != "read_file" {
		t.Fatalf("expected preceding model functionCall with id, got %#v", contents[0])
	}
	if toolUseParts[0].(map[string]any)["thoughtSignature"] != "sig_123" {
		t.Fatalf("functionCall part lost thoughtSignature: %#v", contents[0])
	}
	toolResult := contents[1].(map[string]any)
	parts := toolResult["parts"].([]any)
	functionResponse := parts[0].(map[string]any)["functionResponse"].(map[string]any)
	if functionResponse["id"] != "toolu_123" {
		t.Fatalf("functionResponse.id = %#v, want toolu_123; payload=%#v", functionResponse["id"], payload)
	}
	if functionResponse["name"] != "read_file" {
		t.Fatalf("functionResponse.name = %#v, want read_file", functionResponse["name"])
	}
}

func TestProxyCanonicalStreamAntigravityFunctionCallPreservesThoughtSignatureToMessages(t *testing.T) {
	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"ag_call\",\"name\":\"Bash\",\"args\":{\"command\":\"pwd\"}},\"thoughtSignature\":\"sig_agent\"}]}}]}}\n\n",
		canonicalProtocolAntigravity, canonicalProtocolAnthropicMessages, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	body := rec.Body.String()
	for _, want := range []string{`"type":"tool_use"`, `"id":"ag_call"`, `"name":"Bash"`, `"thought_signature":"sig_agent"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in stream, got %q", want, body)
		}
	}
}

func TestAntigravityUnpairedToolResultDegradesToText(t *testing.T) {
	payload := encodeCanonicalRequestToAntigravityGemini(canonicalRequest{
		Messages: []canonicalMessage{
			{Type: "function_call_output", Role: "user", ToolCallID: "toolu_missing", Output: "late result"},
		},
	}, "claude-opus-4-6-thinking")

	contents := payload["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents len = %d, want 1: %#v", len(contents), contents)
	}
	parts := contents[0].(map[string]any)["parts"].([]any)
	if _, hasFunctionResponse := parts[0].(map[string]any)["functionResponse"]; hasFunctionResponse {
		t.Fatalf("unpaired tool result must not be emitted as functionResponse: %#v", contents)
	}
	text := parts[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "toolu_missing") || !strings.Contains(text, "late result") {
		t.Fatalf("degraded tool result text lost details: %#v", contents)
	}
}

func TestAntigravityMissingThoughtSignatureToolCallDegradesToText(t *testing.T) {
	payload := encodeCanonicalRequestToAntigravityGemini(canonicalRequest{
		Messages: []canonicalMessage{
			{Type: "message", Role: "assistant", ToolCalls: []canonicalToolCall{
				{ID: "call_signed", Name: "Bash", Arguments: `{"command":"pwd"}`, Metadata: map[string]any{"thought_signature": "sig_ok"}},
				{ID: "call_missing", Name: "Edit", Arguments: `{"file":"README.md"}`},
			}},
			{Type: "function_call_output", ToolCallID: "call_signed", Output: "ok"},
			{Type: "function_call_output", ToolCallID: "call_missing", Output: "edited"},
		},
	}, "gemini-pro-agent")

	contents := payload["contents"].([]any)
	modelParts := contents[0].(map[string]any)["parts"].([]any)
	if len(modelParts) != 2 {
		t.Fatalf("model parts len = %d, want degraded text plus signed functionCall: %#v", len(modelParts), modelParts)
	}
	if text := anyString(modelParts[0].(map[string]any)["text"]); !strings.Contains(text, "call_missing") || !strings.Contains(text, "Edit") {
		t.Fatalf("missing-signature tool call was not degraded with details: %#v", modelParts[0])
	}
	functionCallPart := modelParts[1].(map[string]any)
	functionCall := functionCallPart["functionCall"].(map[string]any)
	if functionCall["id"] != "call_signed" || functionCallPart["thoughtSignature"] != "sig_ok" {
		t.Fatalf("signed tool call was not preserved structurally: %#v", functionCallPart)
	}
	userParts := contents[1].(map[string]any)["parts"].([]any)
	if len(userParts) != 2 {
		t.Fatalf("user parts len = %d, want signed functionResponse plus degraded text: %#v", len(userParts), userParts)
	}
	if functionResponse := userParts[0].(map[string]any)["functionResponse"].(map[string]any); functionResponse["id"] != "call_signed" {
		t.Fatalf("signed functionResponse missing: %#v", userParts[0])
	}
	if _, hasFunctionResponse := userParts[1].(map[string]any)["functionResponse"]; hasFunctionResponse {
		t.Fatalf("missing-signature tool result must not be emitted as functionResponse: %#v", userParts[1])
	}
}

func TestProxyCanonicalStreamAntigravityTextualToolUseToMessages(t *testing.T) {
	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"[{\\\"id\\\":\\\"call_123\\\",\\\"type\\\":\\\"tool_use\\\",\\\"name\\\":\\\"TaskCreate\\\",\\\"input\\\":{\\\"subject\\\":\\\"x\\\"}}]\"}]}}]}}\n\n",
		canonicalProtocolAntigravity, canonicalProtocolAnthropicMessages, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	body := rec.Body.String()
	for _, want := range []string{`"type":"tool_use"`, `"id":"call_123"`, `"name":"TaskCreate"`, `"partial_json":"{\"subject\":\"x\"}"`, `"stop_reason":"tool_use"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in stream, got %q", want, body)
		}
	}
}

func TestAntigravitySanitizesClaudeCodeToolSchemas(t *testing.T) {
	canonical, err := canonicalRequestFromAnthropicMessagesPayload(map[string]any{
		"model":      "gemini-alias",
		"max_tokens": 128,
		"messages": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "hi"}}},
		},
		"tools": []any{
			map[string]any{
				"name":        "read_file",
				"description": "Read a file",
				"input_schema": map[string]any{
					"$schema": "https://json-schema.org/draft/2020-12/schema",
					"type":    "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":          "string",
							"propertyNames": map[string]any{"pattern": ".*"},
						},
						"limit": map[string]any{
							"type":             "integer",
							"exclusiveMinimum": 0,
							"minimum":          1,
						},
					},
					"required": []any{"path"},
				},
			},
		},
	}, "gemini-alias")
	if err != nil {
		t.Fatal(err)
	}

	payload := encodeCanonicalRequestToAntigravityGemini(canonical, "gemini-3-pro")
	tools := payload["tools"].([]any)
	declarations := tools[0].(map[string]any)["functionDeclarations"].([]any)
	params := declarations[0].(map[string]any)["parameters"].(map[string]any)
	encoded, _ := json.Marshal(params)
	for _, banned := range []string{"$schema", "propertyNames", "exclusiveMinimum"} {
		if strings.Contains(string(encoded), banned) {
			t.Fatalf("schema still contains %q: %s", banned, string(encoded))
		}
	}
	properties := params["properties"].(map[string]any)
	limit := properties["limit"].(map[string]any)
	if limit["minimum"] != float64(1) && limit["minimum"] != 1 {
		t.Fatalf("minimum should be preserved, got %#v", limit["minimum"])
	}
}

func TestAntigravityImagePayloadBuildsV1InternalImageRequest(t *testing.T) {
	canonical := canonicalRequestFromOpenAIImagesPayload(map[string]any{
		"prompt":          "A tiny robot",
		"size":            "1280x720",
		"quality":         "hd",
		"image_size":      "2K",
		"style":           "natural",
		"response_format": "b64_json",
	}, "gpt-image-2")
	payload := antigravityCanonicalImagePayload(canonical, "gemini-3-pro-image-4k", "project-123")

	if payload["requestType"] != "image_gen" {
		t.Fatalf("requestType = %#v, want image_gen", payload["requestType"])
	}
	if payload["model"] != "gemini-3-pro-image" {
		t.Fatalf("model = %#v, want gemini-3-pro-image", payload["model"])
	}
	request := payload["request"].(map[string]any)
	config := request["generationConfig"].(map[string]any)
	imageConfig := config["imageConfig"].(map[string]any)
	if imageConfig["aspectRatio"] != "16:9" {
		t.Fatalf("aspectRatio = %#v, want 16:9", imageConfig["aspectRatio"])
	}
	if imageConfig["imageSize"] != "2K" {
		t.Fatalf("imageSize = %#v, want 2K", imageConfig["imageSize"])
	}
	if len(request["safetySettings"].([]any)) == 0 {
		t.Fatal("expected safetySettings")
	}
}

func TestAntigravityProtocolTransformsImageResponse(t *testing.T) {
	protocol := antigravityProtocolAdapter{downstreamImages: true, imageResponseFormat: "url"}
	body := []byte(`{
		"response": {
			"candidates": [{
				"content": {"parts": [{"inlineData": {"mimeType": "image/png", "data": "ZmFrZQ=="}}]}
			}],
			"usageMetadata": {
				"promptTokenCount": 11,
				"candidatesTokenCount": 22,
				"totalTokenCount": 33
			}
		}
	}`)

	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(transformed.Body, &out); err != nil {
		t.Fatal(err)
	}
	items := out["data"].([]any)
	first := items[0].(map[string]any)
	if first["url"] != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("url = %#v", first["url"])
	}
	if transformed.Usage.ImageCount != 1 {
		t.Fatalf("image count = %d, want 1", transformed.Usage.ImageCount)
	}
	if transformed.Usage.TotalTokens != 33 {
		t.Fatalf("usage total = %d, want 33", transformed.Usage.TotalTokens)
	}
}

func TestEncodeCanonicalRequestToAntigravityGeminiIncludesFile(t *testing.T) {
	payload := encodeCanonicalRequestToAntigravityGemini(canonicalRequest{
		Messages: []canonicalMessage{{
			Role: "user",
			Content: []canonicalContentPart{
				{Type: "input_text", Text: "Summarize this document"},
				{Type: "input_file", FileName: "report.pdf", FileData: "data:application/pdf;base64,ZmFrZQ=="},
			},
		}},
	}, "gemini-3-pro")
	contents := payload["contents"].([]any)
	parts := contents[0].(map[string]any)["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts length = %d, want 2", len(parts))
	}
	inlineData := parts[1].(map[string]any)["inlineData"].(map[string]any)
	if inlineData["mimeType"] != "application/pdf" {
		t.Fatalf("mimeType = %#v, want application/pdf", inlineData["mimeType"])
	}
	if inlineData["data"] != "ZmFrZQ==" {
		t.Fatalf("data = %#v, want ZmFrZQ==", inlineData["data"])
	}
}
