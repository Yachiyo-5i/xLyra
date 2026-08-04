package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

const bridgeImageFunctionName = "generate_image"

type bridgeImageToolSpec struct {
	Size         string
	Quality      string
	OutputFormat string
	Forced       bool
}

type bridgeFunctionCall struct {
	CallID    string
	Arguments string
}

func (c bridgeFunctionCall) Prompt() string {
	var args struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(c.Arguments), &args); err != nil {
		return ""
	}
	return strings.TrimSpace(args.Prompt)
}

func bridgeImageFunctionTool() map[string]any {
	return map[string]any{
		"type":        "function",
		"name":        bridgeImageFunctionName,
		"description": "Generate an image from a detailed text prompt. Call this tool whenever the user asks for an image, picture, drawing, illustration, or photo to be created. The generated image is displayed to the user automatically; you never receive or output the image data yourself.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "A detailed description of the image to generate.",
				},
			},
			"required": []any{"prompt"},
		},
	}
}

func rewriteImageToolForBridge(request gatewayRequest) (gatewayRequest, bridgeImageToolSpec, bool) {
	if request.DownstreamPath != gatewayEndpointResponses {
		return request, bridgeImageToolSpec{}, false
	}
	if !codexPayloadHasImageGenerationTool(request.Payload) {
		return request, bridgeImageToolSpec{}, false
	}

	payload := clonePayload(request.Payload)
	spec := bridgeImageToolSpec{}

	tools, _ := payload["tools"].([]any)
	rewritten := make([]any, 0, len(tools))
	replaced := false
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		if !strings.EqualFold(strings.TrimSpace(anyString(tool["type"])), "image_generation") {
			rewritten = append(rewritten, item)
			continue
		}
		if !replaced {
			spec.Size = strings.TrimSpace(anyString(tool["size"]))
			spec.Quality = strings.TrimSpace(anyString(tool["quality"]))
			spec.OutputFormat = strings.TrimSpace(anyString(tool["output_format"]))
			rewritten = append(rewritten, bridgeImageFunctionTool())
			replaced = true
		}
	}
	if !replaced {
		return request, bridgeImageToolSpec{}, false
	}
	payload["tools"] = rewritten

	if codexToolChoiceIsImageGeneration(payload["tool_choice"]) || requestPayloadContainsImagegenDirective(payload["input"]) {
		spec.Forced = true
		payload["tool_choice"] = map[string]any{"type": "function", "name": bridgeImageFunctionName}
	}

	if input, ok := payload["input"].([]any); ok {
		payload["input"] = rewriteBridgeImageHistoryItems(input)
	}

	request.Payload = payload
	if canonical, err := canonicalRequestFromOpenAIResponsesPayload(payload, request.RequestedModel); err == nil {
		request.Canonical = &canonical
	}
	return request, spec, true
}

func rewriteBridgeImageHistoryItems(input []any) []any {
	result := make([]any, 0, len(input)+2)
	for _, raw := range input {
		item, _ := raw.(map[string]any)
		if !strings.EqualFold(strings.TrimSpace(anyString(item["type"])), "image_generation_call") {
			result = append(result, raw)
			continue
		}
		callID := strings.TrimSpace(anyString(item["id"]))
		if callID == "" {
			callID = fmt.Sprintf("igc_%d", len(result))
		}
		arguments := "{}"
		if revised := strings.TrimSpace(anyString(item["revised_prompt"])); revised != "" {
			if encoded, err := json.Marshal(map[string]any{"prompt": revised}); err == nil {
				arguments = string(encoded)
			}
		}
		result = append(result,
			map[string]any{
				"type":      "function_call",
				"call_id":   callID,
				"name":      bridgeImageFunctionName,
				"arguments": arguments,
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  "The image was generated successfully and shown to the user.",
			},
		)
	}
	return result
}

func bridgeFunctionCallOutput(call bridgeFunctionCall, outcome bridgeImageOutcome) map[string]any {
	output := "The image was generated successfully and is already displayed to the user. Do not describe the image data; just continue the conversation."
	if !outcome.OK {
		message := strings.TrimSpace(outcome.ErrorMessage)
		if message == "" {
			message = "image generation failed"
		}
		output = "Image generation failed: " + message + ". Apologize briefly and continue without the image."
	}
	return map[string]any{
		"type":    "function_call_output",
		"call_id": call.CallID,
		"output":  output,
	}
}

func responsesInputItemsForBridge(payload map[string]any) []any {
	switch input := payload["input"].(type) {
	case []any:
		return append([]any{}, input...)
	case string:
		if strings.TrimSpace(input) == "" {
			return []any{}
		}
		return []any{map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": input},
			},
		}}
	default:
		return []any{}
	}
}

func bridgeRoundOutputForReplay(output []any) []any {
	result := make([]any, 0, len(output))
	for _, raw := range output {
		item, _ := raw.(map[string]any)
		switch strings.TrimSpace(anyString(item["type"])) {
		case "message", "function_call":
			result = append(result, raw)
		}
	}
	return result
}

func bridgeReplayHasUserFunctionCall(items []any) bool {
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if !strings.EqualFold(strings.TrimSpace(anyString(item["type"])), "function_call") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(anyString(item["name"])), bridgeImageFunctionName) {
			return true
		}
	}
	return false
}
