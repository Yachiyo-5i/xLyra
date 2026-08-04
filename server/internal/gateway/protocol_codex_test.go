package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestOpenAIResolverUsesCodexProtocolForCodexSite(t *testing.T) {
	t.Parallel()

	protocol, err := (openAIProtocolResolver{}).Resolve(t.Context(), gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Payload: map[string]any{
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.3-codex"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if protocol.ProtocolName() != "codex_responses" {
		t.Fatalf("protocol = %q, want codex_responses", protocol.ProtocolName())
	}
}

func TestCodexProtocolBuildsCodexResponsesPayload(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{})
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		Payload: map[string]any{
			"messages":              []any{map[string]any{"role": "user", "content": "hi"}},
			"store":                 true,
			"temperature":           0.2,
			"top_p":                 0.8,
			"max_completion_tokens": 128,
			"stream_options":        map[string]any{"include_usage": true},
			"user":                  "client-user",
			"metadata":              map[string]any{"client": "claude-code"},
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.3-codex"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["model"] != "gpt-5.3-codex" {
		t.Fatalf("model = %#v, want gpt-5.3-codex", payload["model"])
	}
	if payload["store"] != false {
		t.Fatalf("store = %#v, want false", payload["store"])
	}
	if payload["stream"] != true {
		t.Fatalf("stream = %#v, want true", payload["stream"])
	}
	if payload["parallel_tool_calls"] != true {
		t.Fatalf("parallel_tool_calls = %#v, want true", payload["parallel_tool_calls"])
	}
	include, ok := payload["include"].([]string)
	if !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v, want reasoning encrypted content", payload["include"])
	}
	if _, ok := payload["instructions"]; !ok {
		t.Fatalf("instructions should be present")
	}
	for _, key := range []string{"temperature", "top_p", "max_output_tokens", "max_completion_tokens", "stream_options", "user", "metadata"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("%s should be stripped for Codex payload, got %#v", key, payload[key])
		}
	}
}

func TestCodexProtocolBuildsResponsesPayloadFromStringInput(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{})
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model": "alias",
			"input": "hello codex",
			"tool_choice": map[string]any{
				"type": "web_search_preview_2025_03_11",
			},
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.3-codex"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}

	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v, want one message item", payload["input"])
	}
	message, _ := input[0].(map[string]any)
	content, _ := message["content"].([]any)
	if got := message["role"]; got != "user" {
		t.Fatalf("role = %#v, want user", got)
	}
	if len(content) != 1 {
		t.Fatalf("content = %#v, want one content part", message["content"])
	}
	part, _ := content[0].(map[string]any)
	if got := part["type"]; got != "input_text" {
		t.Fatalf("content type = %#v, want input_text", got)
	}
	if got := part["text"]; got != "hello codex" {
		t.Fatalf("content text = %#v, want hello codex", got)
	}
	toolChoice, _ := payload["tool_choice"].(map[string]any)
	if got := toolChoice["type"]; got != "web_search" {
		t.Fatalf("tool_choice.type = %#v, want web_search", got)
	}
}

func TestCodexProtocolNormalizesResponsesHistoryAndStripsCacheRetention(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{})
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model":                  "gpt-5.6-terra",
			"prompt_cache_retention": "24h",
			"input": []any{
				map[string]any{
					"type": "message",
					"role": "user",
					"content": []any{
						map[string]any{"type": "output_text", "text": "hello"},
					},
				},
				map[string]any{
					"type": "message",
					"role": "assistant",
					"content": []any{
						map[string]any{"type": "input_text", "text": "hi"},
						map[string]any{"type": "refusal", "refusal": "no"},
					},
				},
			},
		},
	}, routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.6-terra"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if _, ok := payload["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention should be stripped: %#v", payload)
	}
	input, ok := payload["input"].([]any)
	if !ok || len(input) != 2 {
		t.Fatalf("input = %#v, want two messages", payload["input"])
	}
	userContent := input[0].(map[string]any)["content"].([]any)
	if got := userContent[0].(map[string]any)["type"]; got != "input_text" {
		t.Fatalf("user content type = %#v, want input_text", got)
	}
	assistantContent := input[1].(map[string]any)["content"].([]any)
	if got := assistantContent[0].(map[string]any)["type"]; got != "output_text" {
		t.Fatalf("assistant content type = %#v, want output_text", got)
	}
	if got := assistantContent[1].(map[string]any)["type"]; got != "refusal" {
		t.Fatalf("assistant refusal type = %#v, want refusal", got)
	}
}

func TestCodexProtocolResponsesLiteForcesParallelToolCallsFalse(t *testing.T) {
	t.Parallel()

	downstreamHeaders := http.Header{}
	downstreamHeaders.Set(codexResponsesLiteHeader, "true")

	protocol := newCodexProtocolAdapter(gatewayRequest{})
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath:    gatewayEndpointResponses,
		DownstreamHeaders: downstreamHeaders,
		Payload: map[string]any{
			"model":               "gpt-5.6-codex-mini",
			"input":               "hi",
			"parallel_tool_calls": false,
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.6-codex-mini"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls = %#v, want false when Responses-Lite header is present", payload["parallel_tool_calls"])
	}
}

func TestCodexProtocolWithoutResponsesLiteKeepsParallelToolCallsForced(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{})
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model":               "gpt-5.6-codex-mini",
			"input":               "hi",
			"parallel_tool_calls": false,
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.6-codex-mini"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if payload["parallel_tool_calls"] != true {
		t.Fatalf("parallel_tool_calls = %#v, want true without Responses-Lite header", payload["parallel_tool_calls"])
	}
}

func TestCodexProtocolBuildsImagesPayloadAsImageGenerationTool(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations})
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointImagesGenerations,
		Payload: map[string]any{
			"model":              "gpt-image-2",
			"prompt":             "draw a cat",
			"n":                  2,
			"size":               "1024x1024",
			"quality":            "high",
			"background":         "transparent",
			"output_format":      "png",
			"output_compression": 90,
			"moderation":         "auto",
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if got := payload["model"]; got != codexImageToolHostModel {
		t.Fatalf("model = %#v, want %q", got, codexImageToolHostModel)
	}
	if got := payload["tool_choice"]; got == nil {
		t.Fatalf("tool_choice should be present")
	}
	if got := payload["stream"]; got != true {
		t.Fatalf("stream = %#v, want true", got)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one image_generation tool", payload["tools"])
	}
	tool, _ := tools[0].(map[string]any)
	if got := tool["type"]; got != "image_generation" {
		t.Fatalf("tool.type = %#v, want image_generation", got)
	}
	if got := tool["model"]; got != "gpt-image-2" {
		t.Fatalf("tool.model = %#v, want gpt-image-2", got)
	}
	if got := tool["size"]; got != "1024x1024" {
		t.Fatalf("tool.size = %#v, want 1024x1024", got)
	}
	if got := tool["background"]; got != "transparent" {
		t.Fatalf("tool.background = %#v, want transparent", got)
	}
	if _, ok := payload["n"]; ok {
		t.Fatalf("payload.n should not be present, got %#v", payload["n"])
	}
	if got, ok := tool["n"]; !ok || got != 2 {
		t.Fatalf("tool.n = %#v, want 2", got)
	}
}

func TestCodexProtocolRewritesImageToolResponsesModel(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointResponses})
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model": "gpt-image-2",
			"input": "draw a cat",
			"tools": []any{
				map[string]any{
					"type": "image_generation",
				},
			},
			"tool_choice": map[string]any{"type": "image_generation"},
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2"},
	})
	if err != nil {
		t.Fatalf("BuildUpstreamPayload returned error: %v", err)
	}
	if got := payload["model"]; got != codexImageToolHostModel {
		t.Fatalf("model = %#v, want %q", got, codexImageToolHostModel)
	}
	if got := payload["stream"]; got != true {
		t.Fatalf("stream = %#v, want true", got)
	}
	if _, ok := payload["n"]; ok {
		t.Fatalf("payload.n should not be present, got %#v", payload["n"])
	}
}

func TestCodexProtocolAcceptsResponsesImageEditTool(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointResponses})
	payload, err := protocol.BuildUpstreamPayload(gatewayRequest{
		DownstreamPath: gatewayEndpointResponses,
		Payload: map[string]any{
			"model": "gpt-image-2",
			"input": "edit this image",
			"tools": []any{
				map[string]any{
					"type":   "image_generation",
					"action": "edit",
				},
			},
			"tool_choice": map[string]any{"type": "image_generation"},
		},
	}, routeengine.Candidate{
		Model: routeengine.CandidateModel{UpstreamName: "gpt-image-2"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload == nil {
		t.Fatal("expected non-nil payload")
	}
}

func TestIsCodexImageGenerationRequestMatchesResponsesImageTool(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "codex"}}
	request := gatewayRequest{DownstreamPath: gatewayEndpointResponses}
	payload := map[string]any{
		"tools": []any{
			map[string]any{"type": "image_generation"},
		},
	}

	if !isCodexImageGenerationRequest(candidate, request, payload) {
		t.Fatal("expected Codex responses image_generation tool request to match")
	}
}

func TestIsCodexImageGenerationRequestMatchesImagesEndpoint(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "codex"}}
	request := gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations}

	if !isCodexImageGenerationRequest(candidate, request, nil) {
		t.Fatal("expected Codex images endpoint request to match")
	}
}

func TestIsCodexImageGenerationRequestIgnoresNonCodexImageTool(t *testing.T) {
	t.Parallel()

	candidate := routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "openai"}}
	request := gatewayRequest{DownstreamPath: gatewayEndpointResponses}
	payload := map[string]any{
		"tool_choice": map[string]any{"type": "image_generation"},
	}

	if isCodexImageGenerationRequest(candidate, request, payload) {
		t.Fatal("expected non-Codex image_generation request not to match")
	}
}

func TestCodexProtocolTransformsResponsesImageOutputToImagesBody(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{DownstreamPath: gatewayEndpointImagesGenerations})
	transformed, err := protocol.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, []byte(`{
		"id":"resp_123",
		"created_at":1710000000,
		"model":"gpt-5.4",
		"output":[{"id":"ig_123","type":"image_generation_call","status":"completed","result":"ZmFrZS1pbWFnZQ==","revised_prompt":"a cat","output_format":"png","quality":"high","size":"1024x1024","background":"transparent"}],
		"usage":{"input_tokens":50,"output_tokens":1200,"total_tokens":1250,"input_tokens_details":{"image_tokens":40}}
	}`))
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}
	if transformed.ContentType != "application/json" {
		t.Fatalf("ContentType = %q, want application/json", transformed.ContentType)
	}
	body := string(transformed.Body)
	assertGatewayBodyContainsAll(t, body, `"b64_json":"ZmFrZS1pbWFnZQ=="`, `"output_format":"png"`)
	if transformed.Usage.ImageCount != 1 {
		t.Fatalf("ImageCount = %d, want 1", transformed.Usage.ImageCount)
	}
}

func TestCodexProtocolUpstreamPath(t *testing.T) {
	t.Parallel()

	protocol := newCodexProtocolAdapter(gatewayRequest{})
	tests := map[string]string{
		"":                                      "https://chatgpt.com/backend-api/codex/responses",
		"https://chatgpt.com/backend-api":       "https://chatgpt.com/backend-api/codex/responses",
		"https://chatgpt.com/backend-api/":      "https://chatgpt.com/backend-api/codex/responses",
		"https://chatgpt.com":                   "https://chatgpt.com/backend-api/codex/responses",
		"https://chatgpt.com/backend-api/codex": "https://chatgpt.com/backend-api/codex/responses",
	}
	for baseURL, want := range tests {
		if got := protocol.UpstreamPath(baseURL); got != want {
			t.Fatalf("UpstreamPath(%q) = %q, want %q", baseURL, got, want)
		}
	}
}

func TestApplyCodexGatewayHeaders(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCodexGatewayHeaders(req, "acc_123", true)
	if req.Header.Get("Chatgpt-Account-Id") != "acc_123" {
		t.Fatalf("missing account id header")
	}
	if req.Header.Get("Originator") != codexOriginator {
		t.Fatalf("Originator = %q, want %q", req.Header.Get("Originator"), codexOriginator)
	}
	if req.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", req.Header.Get("Accept"))
	}
}

func TestClassifyCodexUsageLimitReached(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	item, ok := classifyCodexUpstreamError(http.StatusTooManyRequests, []byte(`{"error":{"type":"usage_limit_reached","message":"limit","resets_in_seconds":123}}`), now)
	if !ok {
		t.Fatal("expected codex usage limit classification")
	}
	if item.ErrorType != "codex_usage_limit_reached" {
		t.Fatalf("ErrorType = %q, want codex_usage_limit_reached", item.ErrorType)
	}
	if item.CooldownScope != "credential" {
		t.Fatalf("CooldownScope = %q, want credential", item.CooldownScope)
	}
	if item.CooldownDuration != 123*time.Second {
		t.Fatalf("CooldownDuration = %v, want 123s", item.CooldownDuration)
	}
}

func TestClassifyCodexModelCapacityAsModelCooldown(t *testing.T) {
	t.Parallel()

	item, ok := classifyCodexUpstreamError(http.StatusBadRequest, []byte(`{"error":{"message":"Selected model is at capacity. Please try a different model."}}`), time.Now())
	if !ok {
		t.Fatal("expected codex model capacity classification")
	}
	if item.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, want 429", item.StatusCode)
	}
	if item.CooldownScope != "model" {
		t.Fatalf("CooldownScope = %q, want model", item.CooldownScope)
	}
}

func TestImageGenerationOutputsFromResponsesFiltersCompletedImageResults(t *testing.T) {
	t.Parallel()

	outputs := imageGenerationOutputsFromResponses(responsesResponse{
		Output: []responsesOutputItem{
			{Type: "message", Result: "ignored"},
			{Type: " image_generation_call ", Result: "  "},
			{Type: "IMAGE_GENERATION_CALL", Result: "Zmlyc3Q=", RevisedPrompt: "first"},
			{Type: "image_generation_call", Result: "c2Vjb25k"},
		},
	})

	if len(outputs) != 2 {
		t.Fatalf("outputs length = %d, want 2: %#v", len(outputs), outputs)
	}
	if outputs[0].Result != "Zmlyc3Q=" || outputs[0].RevisedPrompt != "first" {
		t.Fatalf("first output = %#v, want first image result", outputs[0])
	}
	if outputs[1].Result != "c2Vjb25k" {
		t.Fatalf("second output = %#v, want second image result", outputs[1])
	}
}

func TestWriteCodexImageStreamEventsWritesPartialAndCompletedImages(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	capture := streamCaptureState{}
	headersWritten := false
	writeHeaders := func() {
		headersWritten = true
		rec.WriteHeader(http.StatusOK)
	}

	err := writeCodexImageStreamEvents(rec, &capture, "image_generation", responsesStreamBufferEvent{
		Type:              "response.image_generation_call.partial_image",
		PartialImageB64:   " cGFydGlhbA== ",
		PartialImageIndex: 3,
	}, writeHeaders)
	if err != nil {
		t.Fatalf("partial image event returned error: %v", err)
	}
	if !headersWritten {
		t.Fatal("partial image event should write headers")
	}
	if capture.usage.ImageCount != 1 {
		t.Fatalf("partial image count = %d, want 1", capture.usage.ImageCount)
	}

	err = writeCodexImageStreamEvents(rec, &capture, "image_generation", responsesStreamBufferEvent{
		Type: "response.completed",
		Response: responsesResponse{
			ID: "resp_123",
			Output: []responsesOutputItem{{
				Type:          "image_generation_call",
				Result:        " ZmluYWw= ",
				RevisedPrompt: " polished prompt ",
			}},
			Usage: &responsesUsage{InputTokens: 5, OutputTokens: 7, TotalTokens: 12},
		},
	}, writeHeaders)
	if err != nil {
		t.Fatalf("completed image event returned error: %v", err)
	}
	if !capture.streamCompleted || !capture.sawDone {
		t.Fatalf("capture completion flags = streamCompleted:%v sawDone:%v, want both true", capture.streamCompleted, capture.sawDone)
	}
	if capture.usage.ImageCount != 1 || capture.usage.PromptTokens != 5 || capture.usage.CompletionTokens != 7 {
		t.Fatalf("capture usage = %#v, want image count and token usage", capture.usage)
	}

	events := strings.Split(strings.TrimSpace(rec.Body.String()), "\n\n")
	if len(events) != 2 {
		t.Fatalf("written events = %d, want 2:\n%s", len(events), rec.Body.String())
	}
	if !strings.HasPrefix(events[0], "event: image_generation.partial_image\n") {
		t.Fatalf("partial event frame = %q", events[0])
	}
	if !strings.HasPrefix(events[1], "event: image_generation.completed\n") {
		t.Fatalf("completed event frame = %q", events[1])
	}

	var partial struct {
		Type              string `json:"type"`
		B64JSON           string `json:"b64_json"`
		PartialImageIndex int    `json:"partial_image_index"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.SplitN(events[0], "\n", 2)[1], "data: ")), &partial); err != nil {
		t.Fatalf("decode partial payload: %v", err)
	}
	if partial.Type != "image_generation.partial_image" || partial.B64JSON != "cGFydGlhbA==" || partial.PartialImageIndex != 3 {
		t.Fatalf("partial payload = %#v", partial)
	}

	var completed struct {
		Type          string `json:"type"`
		B64JSON       string `json:"b64_json"`
		RevisedPrompt string `json:"revised_prompt"`
		Usage         struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.SplitN(events[1], "\n", 2)[1], "data: ")), &completed); err != nil {
		t.Fatalf("decode completed payload: %v", err)
	}
	if completed.Type != "image_generation.completed" || completed.B64JSON != "ZmluYWw=" || completed.RevisedPrompt != "polished prompt" {
		t.Fatalf("completed payload = %#v", completed)
	}
	if completed.Usage.InputTokens != 5 || completed.Usage.OutputTokens != 7 || completed.Usage.TotalTokens != 12 {
		t.Fatalf("completed usage = %#v", completed.Usage)
	}
}
