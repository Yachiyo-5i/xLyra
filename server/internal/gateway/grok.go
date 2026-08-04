package gateway

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"xlyra/server/internal/adapter"
)

var grokSupportedResponsesToolTypes = map[string]struct{}{
	"function":           {},
	"web_search":         {},
	"web_search_preview": {},
	"image_generation":   {},
}

func normalizeGrokTools(tools []any, bridged map[string]struct{}) []any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		toolType := strings.ToLower(strings.TrimSpace(anyString(toolMap["type"])))
		switch toolType {
		case "namespace":
			if nested, ok := toolMap["tools"].([]any); ok {
				out = append(out, normalizeGrokTools(nested, bridged)...)
			}
			continue
		case "custom":
			name := anyString(toolMap["name"])
			if name == "" {
				continue
			}
			bridged[name] = struct{}{}
			out = append(out, grokBridgedFunctionTool(name, anyString(toolMap["description"])))
			continue
		}
		if _, supported := grokSupportedResponsesToolTypes[toolType]; supported {
			out = append(out, tool)
		}
	}
	return out
}

func grokBridgedFunctionTool(name, description string) map[string]any {
	tool := map[string]any{
		"type": "function",
		"name": name,
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "The raw tool input text.",
				},
			},
			"required":             []any{"input"},
			"additionalProperties": false,
		},
	}
	if description != "" {
		tool["description"] = description
	}
	return tool
}

func bridgeGrokCustomToolCall(message map[string]any) {
	message["type"] = "function_call"
	input, _ := message["input"].(string)
	arguments, err := json.Marshal(map[string]any{"input": input})
	if err != nil {
		arguments = []byte(`{"input":""}`)
	}
	message["arguments"] = string(arguments)
	delete(message, "input")
}

func grokBridgedInputFromArguments(arguments string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(arguments), &parsed); err == nil {
		if input, ok := parsed["input"].(string); ok {
			return input
		}
	}
	return arguments
}

func normalizeGrokResponsesPayload(payload map[string]any) map[string]struct{} {
	bridged := map[string]struct{}{}
	if input, ok := payload["input"].([]any); ok {
		filtered := make([]any, 0, len(input))
		var hoistedTools []any
		for _, item := range input {
			message, ok := item.(map[string]any)
			if ok {
				switch strings.ToLower(strings.TrimSpace(anyString(message["type"]))) {
				case "additional_tools":
					if tools, ok := message["tools"].([]any); ok {
						hoistedTools = append(hoistedTools, tools...)
					}
					continue
				case "reasoning":
					continue
				case "custom_tool_call":
					bridgeGrokCustomToolCall(message)
				case "custom_tool_call_output":
					message["type"] = "function_call_output"
				default:
					normalizeGrokResponsesContent(message)
				}
			}
			filtered = append(filtered, item)
		}
		payload["input"] = filtered
		if len(hoistedTools) > 0 {
			existing, _ := payload["tools"].([]any)
			payload["tools"] = append(existing, hoistedTools...)
		}
	}

	if include, ok := payload["include"].([]any); ok {
		kept := make([]any, 0, len(include))
		for _, entry := range include {
			if !strings.Contains(strings.ToLower(anyString(entry)), "reasoning.encrypted_content") {
				kept = append(kept, entry)
			}
		}
		if len(kept) == 0 {
			delete(payload, "include")
		} else {
			payload["include"] = kept
		}
	}

	tools, _ := payload["tools"].([]any)
	kept := normalizeGrokTools(tools, bridged)
	if len(kept) == 0 {
		delete(payload, "tools")
		delete(payload, "tool_choice")
	} else {
		payload["tools"] = kept
	}

	delete(payload, "client_metadata")
	if reasoning, ok := payload["reasoning"].(map[string]any); ok {
		delete(reasoning, "context")
	}
	return bridged
}

func normalizeGrokResponsesContent(message map[string]any) {
	content, ok := message["content"].([]any)
	if !ok {
		return
	}
	assistant := strings.EqualFold(strings.TrimSpace(anyString(message["role"])), "assistant")
	for _, part := range content {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(anyString(partMap["type"])), "text") {
			if assistant {
				partMap["type"] = "output_text"
			} else {
				partMap["type"] = "input_text"
			}
		}
	}
}

func normalizeGrokImagesPayload(payload map[string]any) {
	if size, ok := payload["size"].(string); ok {
		if width, height, parsed := parseGrokSize(size); parsed {
			if _, exists := payload["resolution"]; !exists {
				payload["resolution"] = grokResolutionFromDims(width, height)
			}
			if _, exists := payload["aspect_ratio"]; !exists {
				payload["aspect_ratio"] = grokAspectRatioFromDims(width, height)
			}
		} else if strings.EqualFold(strings.TrimSpace(size), "auto") {
			if _, exists := payload["aspect_ratio"]; !exists {
				payload["aspect_ratio"] = "auto"
			}
		}
	}
	delete(payload, "size")

	switch strings.ToLower(anyString(payload["quality"])) {
	case "":
	case "low", "medium", "high":
	case "standard":
		payload["quality"] = "medium"
	case "hd":
		payload["quality"] = "high"
	default:
		delete(payload, "quality")
	}

	if enabled, explicit := grokBoolValue(payload["enable_nsfw"]); explicit {
		payload["enable_nsfw"] = enabled
	} else {
		payload["enable_nsfw"] = true
	}
}

func grokBoolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1":
			return true, true
		case "false", "0":
			return false, true
		}
	}
	return false, false
}

func parseGrokSize(size string) (int, int, bool) {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(size)), "x", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, widthErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, heightErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if widthErr != nil || heightErr != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func grokResolutionFromDims(width, height int) string {
	if width >= 1536 || height >= 1536 {
		return "2k"
	}
	return "1k"
}

var grokAspectRatios = []struct {
	label string
	ratio float64
}{
	{"1:1", 1.0},
	{"16:9", 16.0 / 9.0},
	{"9:16", 9.0 / 16.0},
	{"4:3", 4.0 / 3.0},
	{"3:4", 3.0 / 4.0},
	{"3:2", 3.0 / 2.0},
	{"2:3", 2.0 / 3.0},
	{"2:1", 2.0},
	{"1:2", 0.5},
	{"19.5:9", 19.5 / 9.0},
	{"9:19.5", 9.0 / 19.5},
	{"20:9", 20.0 / 9.0},
	{"9:20", 9.0 / 20.0},
}

func grokAspectRatioFromDims(width, height int) string {
	target := float64(width) / float64(height)
	best := grokAspectRatios[0].label
	bestDiff := math.Abs(math.Log(target / grokAspectRatios[0].ratio))
	for _, ar := range grokAspectRatios[1:] {
		diff := math.Abs(math.Log(target / ar.ratio))
		if diff < bestDiff {
			bestDiff = diff
			best = ar.label
		}
	}
	return best
}

func applyGrokGatewayHeaders(req *http.Request, token string, model string) {
	if req == nil || (!adapter.IsGrokCLIProxyURL(req.URL) && !adapter.IsGrokAPIURL(req.URL)) {
		return
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	if !adapter.IsGrokCLIProxyURL(req.URL) {
		return
	}
	req.Header.Set("X-XAI-Token-Auth", adapter.GrokTokenAuthHeader)
	req.Header.Set("x-grok-client-version", adapter.GrokClientVersion)
	req.Header.Set("x-grok-client-surface", adapter.GrokClientSurface)
	req.Header.Set("User-Agent", adapter.GrokUserAgent)
	if model = strings.TrimSpace(model); model != "" {
		req.Header.Set(adapter.GrokModelHeader, model)
	}
}
