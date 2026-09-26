package gateway

import (
	"encoding/json"
	"fmt"
	"strings"

	routeengine "xlyra/server/internal/router"
)

func canonicalRequestFromGoogleGeminiPayload(payload map[string]any, requestedModel string, stream bool) (canonicalRequest, error) {
	request := canonicalRequest{
		DownstreamPath: gatewayEndpointGeminiGenerate,
		RequestedModel: strings.TrimSpace(requestedModel),
		SourceProtocol: canonicalProtocolGoogleGemini,
		Stream:         stream,
		Params:         canonicalParamsFromPayload(payload),
		Raw:            clonePayload(payload),
	}

	if payload == nil {
		return request, fmt.Errorf("request body must be an object")
	}
	contents, ok := payload["contents"].([]any)
	if !ok || len(contents) == 0 {
		return request, fmt.Errorf("contents must be a non-empty array")
	}
	if system, ok := payload["systemInstruction"].(map[string]any); ok {
		request.RawSystem = clonePayload(system)
		request.Instructions = geminiPartsText(system["parts"])
	}
	if config, ok := payload["generationConfig"].(map[string]any); ok {
		request.Params["gemini_generation_config"] = clonePayload(config)
		geminiGenerationConfigToCanonical(request.Params, config)
		if responseFormat := geminiResponseFormat(config); responseFormat != nil {
			request.TextFormat = responseFormat
		}
	}
	request.Tools = canonicalToolsFromGoogleGemini(payload["tools"])
	request.ToolChoice = canonicalToolChoiceFromGoogleGemini(payload["toolConfig"])

	for _, raw := range contents {
		message, err := canonicalMessageFromGoogleGemini(raw)
		if err != nil {
			return request, err
		}
		request.Messages = append(request.Messages, message...)
	}
	if geminiRequestsImage(request.Params["response_modalities"]) {
		request.Image = canonicalImageRequestFromGoogleGemini(request)
	}
	return request, nil
}

func validateGoogleGeminiConversion(request canonicalRequest, target canonicalProtocol) error {
	if target == canonicalProtocolGoogleGemini && request.SourceProtocol != canonicalProtocolGoogleGemini && request.SourceProtocol != canonicalProtocolAntigravity {
		return validateCanonicalRequestForGoogleGemini(request)
	}
	if request.SourceProtocol != canonicalProtocolGoogleGemini || target == canonicalProtocolGoogleGemini {
		return nil
	}
	for key := range request.Raw {
		switch key {
		case "contents", "systemInstruction", "generationConfig", "tools", "toolConfig":
		default:
			return fmt.Errorf("Gemini field %q cannot be represented by %s without loss", key, target)
		}
	}
	if raw, ok := request.Raw["generationConfig"].(map[string]any); ok {
		for key := range raw {
			switch key {
			case "temperature", "topP", "topK", "maxOutputTokens", "stopSequences", "responseMimeType", "responseSchema", "candidateCount", "responseModalities":
			case "thinkingConfig", "imageConfig":
				return fmt.Errorf("Gemini generationConfig.%s cannot be represented by %s without loss", key, target)
			default:
				return fmt.Errorf("Gemini generationConfig.%s cannot be represented by %s without loss", key, target)
			}
			if err := validateGeminiGenerationField(target, key, request); err != nil {
				return err
			}
		}
	}
	if contents, ok := request.Raw["contents"].([]any); ok {
		for _, rawContent := range contents {
			content, _ := rawContent.(map[string]any)
			if err := validateGeminiKeys(content, target, "role", "parts"); err != nil {
				return err
			}
			parts, _ := content["parts"].([]any)
			for _, rawPart := range parts {
				part, _ := rawPart.(map[string]any)
				if err := validateGeminiKeys(part, target, "text", "thought", "thoughtSignature", "thought_signature", "inlineData", "inline_data", "fileData", "file_data", "functionCall", "function_call", "functionResponse", "function_response"); err != nil {
					return err
				}
				for _, nested := range []struct {
					name    string
					allowed []string
				}{
					{"inlineData", []string{"mimeType", "data"}},
					{"inline_data", []string{"mime_type", "data"}},
					{"fileData", []string{"mimeType", "fileUri", "file_uri"}},
					{"file_data", []string{"mime_type", "file_uri", "fileUri"}},
					{"functionCall", []string{"id", "name", "args"}},
					{"function_call", []string{"id", "name", "args"}},
					{"functionResponse", []string{"id", "name", "response"}},
					{"function_response", []string{"id", "name", "response"}},
				} {
					if value, ok := part[nested.name].(map[string]any); ok {
						if err := validateGeminiKeys(value, target, nested.allowed...); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	if system, ok := request.Raw["systemInstruction"].(map[string]any); ok {
		if err := validateGeminiKeys(system, target, "role", "parts"); err != nil {
			return err
		}
		parts, _ := system["parts"].([]any)
		for _, rawPart := range parts {
			part, _ := rawPart.(map[string]any)
			if err := validateGeminiKeys(part, target, "text"); err != nil {
				return err
			}
		}
	}
	if tools, ok := request.Raw["tools"].([]any); ok {
		for _, rawTool := range tools {
			tool, _ := rawTool.(map[string]any)
			if err := validateGeminiKeys(tool, target, "functionDeclarations", "function_declarations"); err != nil {
				return err
			}
			declarations, _ := tool["functionDeclarations"].([]any)
			if len(declarations) == 0 {
				declarations, _ = tool["function_declarations"].([]any)
			}
			for _, rawDeclaration := range declarations {
				declaration, _ := rawDeclaration.(map[string]any)
				if err := validateGeminiKeys(declaration, target, "name", "description", "parameters"); err != nil {
					return err
				}
			}
		}
	}
	if toolConfig, ok := request.Raw["toolConfig"].(map[string]any); ok {
		if err := validateGeminiKeys(toolConfig, target, "functionCallingConfig", "function_calling_config"); err != nil {
			return err
		}
		if config, ok := toolConfig["functionCallingConfig"].(map[string]any); ok {
			if err := validateGeminiKeys(config, target, "mode", "allowedFunctionNames"); err != nil {
				return err
			}
		}
		if config, ok := toolConfig["function_calling_config"].(map[string]any); ok {
			if err := validateGeminiKeys(config, target, "mode", "allowed_function_names"); err != nil {
				return err
			}
		}
	}
	for _, message := range request.Messages {
		for _, part := range message.Content {
			if part.Type == "gemini_raw" {
				return fmt.Errorf("an unsupported Gemini content part cannot be represented by %s without loss", target)
			}
		}
	}
	if tools, ok := request.Raw["tools"].([]any); ok {
		for _, rawTool := range tools {
			tool, _ := rawTool.(map[string]any)
			if _, ok := tool["functionDeclarations"]; !ok {
				if _, ok := tool["function_declarations"]; !ok {
					return fmt.Errorf("a non-function Gemini tool cannot be represented by %s without loss", target)
				}
			}
		}
	}
	if err := validateGoogleSourceContent(request); err != nil {
		return err
	}
	return nil
}

func validateGeminiGenerationField(target canonicalProtocol, key string, request canonicalRequest) error {
	switch key {
	case "topK":
		if target != canonicalProtocolAnthropicMessages {
			return fmt.Errorf("Gemini generationConfig.%s cannot be represented by %s without loss", key, target)
		}
	case "candidateCount":
		if target != canonicalProtocolOpenAIChat && target != canonicalProtocolOpenAIImages {
			return fmt.Errorf("Gemini generationConfig.%s cannot be represented by %s without loss", key, target)
		}
	case "responseModalities":
		if target == canonicalProtocolOpenAIImages {
			return nil
		}
		if target == canonicalProtocolOpenAIChat && !geminiResponseModalitiesContainImage(request.Params["response_modalities"]) {
			return nil
		}
		return fmt.Errorf("Gemini generationConfig.%s cannot be represented by %s without loss", key, target)
	case "responseMimeType", "responseSchema":
		if target != canonicalProtocolOpenAIChat && target != canonicalProtocolOpenAIResponses {
			return fmt.Errorf("Gemini generationConfig.%s cannot be represented by %s without loss", key, target)
		}
	}
	if target == canonicalProtocolOpenAIImages && request.Image == nil {
		return fmt.Errorf("Gemini generationConfig.%s requires an image request for %s", key, target)
	}
	return nil
}

func validateGoogleSourceContent(request canonicalRequest) error {
	validateParts := func(raw any, allowed map[string]struct{}) error {
		items, ok := raw.([]any)
		if !ok {
			return nil
		}
		for _, item := range items {
			part, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("%s content item cannot be represented by Gemini without loss", request.SourceProtocol)
			}
			typeName := strings.TrimSpace(anyString(part["type"]))
			if _, ok := allowed[typeName]; !ok {
				return fmt.Errorf("%s content type %q cannot be represented by Gemini without loss", request.SourceProtocol, typeName)
			}
		}
		return nil
	}
	validateTextOnly := func(raw any, protocol canonicalProtocol) error {
		items, ok := raw.([]any)
		if !ok {
			if raw == nil {
				return nil
			}
			if _, ok := raw.(string); ok {
				return nil
			}
			return fmt.Errorf("%s instruction content cannot be represented by Gemini without loss", protocol)
		}
		for _, item := range items {
			part, ok := item.(map[string]any)
			if !ok {
				return fmt.Errorf("%s instruction content cannot be represented by Gemini without loss", protocol)
			}
			switch strings.TrimSpace(anyString(part["type"])) {
			case "text", "input_text", "output_text", "":
			default:
				return fmt.Errorf("%s instruction content type %q cannot be represented by Gemini without loss", protocol, anyString(part["type"]))
			}
		}
		return nil
	}
	switch request.SourceProtocol {
	case canonicalProtocolOpenAIChat:
		messages, _ := request.Raw["messages"].([]any)
		allowed := map[string]struct{}{"text": {}, "input_text": {}, "output_text": {}, "image_url": {}, "input_image": {}, "file": {}}
		for _, rawMessage := range messages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				return fmt.Errorf("OpenAI Chat message cannot be represented by Gemini without loss")
			}
			messageKeys := []string{"role", "content"}
			switch strings.TrimSpace(anyString(message["role"])) {
			case "assistant":
				messageKeys = append(messageKeys, "tool_calls", "reasoning_content", "thinking", "thinking_signature")
			case "tool", "function":
				messageKeys = append(messageKeys, "tool_call_id", "name")
			}
			if err := validateSourceKeys(message, canonicalProtocolOpenAIChat, "OpenAI Chat message", messageKeys...); err != nil {
				return err
			}
			if err := validateParts(message["content"], allowed); err != nil {
				return err
			}
			if role := strings.TrimSpace(anyString(message["role"])); role == "system" || role == "developer" {
				if err := validateTextOnly(message["content"], canonicalProtocolOpenAIChat); err != nil {
					return err
				}
			}
			if rawCalls, ok := message["tool_calls"].([]any); ok {
				for _, rawCall := range rawCalls {
					call, ok := rawCall.(map[string]any)
					if !ok {
						return fmt.Errorf("OpenAI Chat tool call cannot be represented by Gemini without loss")
					}
					if err := validateSourceKeys(call, canonicalProtocolOpenAIChat, "OpenAI Chat tool call", "id", "type", "function", "call_id", "thought_signature", "thoughtSignature"); err != nil {
						return err
					}
					function, _ := call["function"].(map[string]any)
					if err := validateSourceKeys(function, canonicalProtocolOpenAIChat, "OpenAI Chat function call", "name", "arguments"); err != nil {
						return err
					}
				}
			}
		}
	case canonicalProtocolOpenAIResponses:
		rawInput, exists := request.Raw["input"]
		if !exists {
			break
		}
		input, ok := rawInput.([]any)
		if !ok {
			if _, ok := rawInput.(string); ok {
				break
			}
			return fmt.Errorf("OpenAI Responses input cannot be represented by Gemini without loss")
		}
		allowed := map[string]struct{}{"input_text": {}, "output_text": {}, "input_image": {}, "input_file": {}}
		for _, rawItem := range input {
			item, ok := rawItem.(map[string]any)
			if !ok {
				return fmt.Errorf("OpenAI Responses input item cannot be represented by Gemini without loss")
			}
			switch itemType := strings.TrimSpace(anyString(item["type"])); itemType {
			case "message", "":
				if err := validateSourceKeys(item, canonicalProtocolOpenAIResponses, "OpenAI Responses message", "type", "role", "content"); err != nil {
					return err
				}
				if err := validateParts(item["content"], allowed); err != nil {
					return err
				}
				if role := strings.TrimSpace(anyString(item["role"])); role == "system" || role == "developer" {
					if err := validateTextOnly(item["content"], canonicalProtocolOpenAIResponses); err != nil {
						return err
					}
				}
			case "reasoning":
				if err := validateSourceKeys(item, canonicalProtocolOpenAIResponses, "OpenAI Responses reasoning", "type", "content", "thinking_signature", "signature"); err != nil {
					return err
				}
				if len(canonicalThinkingFromResponsesItem(item)) == 0 {
					return fmt.Errorf("OpenAI Responses reasoning item cannot be represented by Gemini without loss")
				}
			case "function_call":
				if err := validateSourceKeys(item, canonicalProtocolOpenAIResponses, "OpenAI Responses function call", "type", "id", "call_id", "name", "arguments", "thought_signature", "thoughtSignature"); err != nil {
					return err
				}
			case "function_call_output":
				if err := validateSourceKeys(item, canonicalProtocolOpenAIResponses, "OpenAI Responses function output", "type", "call_id", "output"); err != nil {
					return err
				}
			case "additional_tools":
				if err := validateSourceKeys(item, canonicalProtocolOpenAIResponses, "OpenAI Responses additional tools", "type", "tools"); err != nil {
					return err
				}
			case "custom_tool_call", "custom_tool_call_output":
				return fmt.Errorf("OpenAI Responses %s cannot be represented by Gemini without loss", itemType)
			default:
				return fmt.Errorf("OpenAI Responses input type %q cannot be represented by Gemini without loss", itemType)
			}
		}
	case canonicalProtocolAnthropicMessages:
		messages, _ := request.Raw["messages"].([]any)
		allowed := map[string]struct{}{"text": {}, "thinking": {}, "image": {}, "document": {}, "tool_use": {}, "tool_result": {}}
		for _, rawMessage := range messages {
			message, ok := rawMessage.(map[string]any)
			if !ok {
				return fmt.Errorf("Anthropic message cannot be represented by Gemini without loss")
			}
			if err := validateSourceKeys(message, canonicalProtocolAnthropicMessages, "Anthropic message", "role", "content"); err != nil {
				return err
			}
			if err := validateParts(message["content"], allowed); err != nil {
				return err
			}
			if role := strings.TrimSpace(anyString(message["role"])); role == "system" || role == "developer" {
				if err := validateTextOnly(message["content"], canonicalProtocolAnthropicMessages); err != nil {
					return err
				}
			}
		}
		if rawSystem, exists := request.Raw["system"]; exists {
			switch system := rawSystem.(type) {
			case string:
			case []any:
				for _, rawBlock := range system {
					block, ok := rawBlock.(map[string]any)
					if !ok {
						return fmt.Errorf("Anthropic system block cannot be represented by Gemini without loss")
					}
					if err := validateSourceKeys(block, canonicalProtocolAnthropicMessages, "Anthropic system block", "type", "text"); err != nil {
						return err
					}
					if strings.TrimSpace(anyString(block["type"])) != "text" {
						return fmt.Errorf("Anthropic system block type %q cannot be represented by Gemini without loss", anyString(block["type"]))
					}
				}
			default:
				return fmt.Errorf("Anthropic system cannot be represented by Gemini without loss")
			}
		}
	}
	return nil
}

func validateSourceKeys(value map[string]any, target canonicalProtocol, label string, allowed ...string) error {
	if value == nil {
		return fmt.Errorf("%s cannot be represented by %s without loss", label, target)
	}
	allowedKeys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedKeys[key] = struct{}{}
	}
	for key := range value {
		if _, ok := allowedKeys[key]; !ok {
			return fmt.Errorf("%s field %q cannot be represented by %s without loss", label, key, target)
		}
	}
	return nil
}

func geminiMappedRequestParams() map[string]struct{} {
	return map[string]struct{}{
		"stream": {}, "stream_options": {}, "temperature": {}, "top_p": {}, "top_k": {},
		"max_tokens": {}, "max_completion_tokens": {}, "max_output_tokens": {}, "stop": {},
		"stop_sequences": {}, "seed": {}, "presence_penalty": {}, "frequency_penalty": {},
		"n": {}, "candidate_count": {}, "response_modalities": {}, "modalities": {},
		"thinking": {}, "thinking_config": {}, "reasoning": {}, "reasoning_effort": {},
		"output_config": {}, "response_format": {}, "response_mime_type": {},
		"response_schema": {}, "image_config": {}, "gemini_generation_config": {}, "text": {},
	}
}

func geminiIgnorableRequestParams() map[string]struct{} {
	return map[string]struct{}{
		"user": {}, "metadata": {}, "store": {}, "service_tier": {}, "safety_identifier": {},
		"prompt_cache_key": {}, "prompt_cache_retention": {}, "prompt_cache_options": {},
		"parallel_tool_calls": {}, "include": {}, "background": {}, "max_tool_calls": {},
	}
}

func validateCanonicalRequestForGoogleGemini(request canonicalRequest) error {
	if request.Image != nil {
		if request.Image.Mask != nil {
			return fmt.Errorf("%s image mask cannot be represented by Gemini without loss", request.SourceProtocol)
		}
		for _, image := range request.Image.Images {
			if strings.TrimSpace(image.FileID) != "" {
				return fmt.Errorf("%s image file_id cannot be represented by Gemini without loss", request.SourceProtocol)
			}
		}
		if format := strings.ToLower(strings.TrimSpace(anyString(request.Params["response_format"]))); format != "" && format != "b64_json" {
			return fmt.Errorf("%s image response_format %q cannot be represented by Gemini without loss", request.SourceProtocol, format)
		}
	}
	mapped := geminiMappedRequestParams()
	ignorable := geminiIgnorableRequestParams()
	for key := range request.Params {
		if _, ok := ignorable[key]; ok {
			continue
		}
		if _, ok := mapped[key]; ok {
			continue
		}
		return fmt.Errorf("%s field %q cannot be represented by Gemini without loss", request.SourceProtocol, key)
	}
	for _, tool := range request.Tools {
		if tool.Type != "function" {
			return fmt.Errorf("%s tool type %q cannot be represented by Gemini without loss", request.SourceProtocol, tool.Type)
		}
		switch request.SourceProtocol {
		case canonicalProtocolOpenAIChat:
			if err := validateSourceKeys(tool.Raw, canonicalProtocolGoogleGemini, "OpenAI Chat tool", "type", "function"); err != nil {
				return err
			}
			function, _ := tool.Raw["function"].(map[string]any)
			if err := validateSourceKeys(function, canonicalProtocolGoogleGemini, "OpenAI Chat function tool", "name", "description", "parameters"); err != nil {
				return err
			}
		case canonicalProtocolOpenAIResponses:
			if err := validateSourceKeys(tool.Raw, canonicalProtocolGoogleGemini, "OpenAI Responses tool", "type", "name", "description", "parameters"); err != nil {
				return err
			}
		case canonicalProtocolAnthropicMessages:
			if err := validateSourceKeys(tool.Raw, canonicalProtocolGoogleGemini, "Anthropic tool", "name", "description", "input_schema"); err != nil {
				return err
			}
		}
	}
	for _, message := range request.Messages {
		for _, part := range message.Content {
			switch part.Type {
			case "input_text", "output_text", "input_image", "input_file":
			default:
				return fmt.Errorf("%s content part %q cannot be represented by Gemini without loss", request.SourceProtocol, part.Type)
			}
			if part.CacheControl != nil {
				return fmt.Errorf("%s content cache control cannot be represented by Gemini without loss", request.SourceProtocol)
			}
		}
	}
	return validateGoogleSourceContent(request)
}

func validateGeminiKeys(value map[string]any, target canonicalProtocol, allowed ...string) error {
	if value == nil {
		return fmt.Errorf("invalid Gemini object cannot be represented by %s without loss", target)
	}
	allowedKeys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedKeys[key] = struct{}{}
	}
	for key := range value {
		if _, ok := allowedKeys[key]; !ok {
			return fmt.Errorf("Gemini field %q cannot be represented by %s without loss", key, target)
		}
	}
	return nil
}

func geminiRequestsImage(raw any) bool {
	items, _ := raw.([]any)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(anyString(item)), "IMAGE") {
			return true
		}
	}
	return false
}

func geminiResponseModalitiesContainImage(raw any) bool {
	items, _ := raw.([]any)
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(anyString(item)), "IMAGE") {
			return true
		}
	}
	return false
}

func canonicalImageRequestFromGoogleGemini(request canonicalRequest) *canonicalImageRequest {
	promptParts := make([]string, 0)
	for _, message := range request.Messages {
		if message.Role != "user" {
			continue
		}
		if text := strings.TrimSpace(antigravityCanonicalContentText(message.Content, message.RawContent)); text != "" {
			promptParts = append(promptParts, text)
		}
	}
	count := intFromAnyGateway(request.Params["candidate_count"])
	if count <= 0 {
		count = 1
	}
	params := map[string]any{"response_format": "b64_json"}
	if imageConfig, ok := request.Params["image_config"].(map[string]any); ok {
		params["image_config"] = clonePayload(imageConfig)
	}
	return &canonicalImageRequest{
		Prompt: strings.Join(promptParts, "\n"),
		N:      count,
		Action: "generate",
		Params: params,
	}
}

func geminiGenerationConfigToCanonical(params map[string]any, config map[string]any) {
	for source, target := range map[string]string{
		"temperature":        "temperature",
		"topP":               "top_p",
		"topK":               "top_k",
		"maxOutputTokens":    "max_output_tokens",
		"stopSequences":      "stop",
		"candidateCount":     "candidate_count",
		"responseModalities": "response_modalities",
		"thinkingConfig":     "thinking_config",
		"imageConfig":        "image_config",
	} {
		if value, ok := config[source]; ok {
			params[target] = value
		}
	}
	if mime := strings.TrimSpace(anyString(config["responseMimeType"])); mime != "" {
		params["response_mime_type"] = mime
	}
	if schema := config["responseSchema"]; schema != nil {
		params["response_schema"] = schema
	}
}

func geminiThinkingConfigFromCrossProtocolParams(params map[string]any) map[string]any {
	if effort := geminiEffortFromCrossProtocolParams(params); effort != "" {
		return geminiThinkingConfigFromEffort(effort)
	}
	return nil
}

func geminiEffortFromCrossProtocolParams(params map[string]any) string {
	if effort := strings.ToLower(strings.TrimSpace(anyString(params["reasoning_effort"]))); effort != "" {
		return effort
	}
	if reasoning, ok := params["reasoning"].(map[string]any); ok {
		if effort := strings.ToLower(strings.TrimSpace(anyString(reasoning["effort"]))); effort != "" {
			return effort
		}
	}
	if outputConfig, ok := params["output_config"].(map[string]any); ok {
		if effort := strings.ToLower(strings.TrimSpace(anyString(outputConfig["effort"]))); effort != "" {
			return effort
		}
	}
	return ""
}

// geminiThinkingConfigFromEffort maps cross-protocol reasoning_effort onto Gemini /
// Antigravity generationConfig.thinkingConfig.
//
// Upstream field names follow the Google ThinkingConfig protobuf used by both the
// Gemini API and Antigravity Cloud Code PA: includeThoughts, thinkingLevel, and
// thinkingBudget (NOT budgetTokens — Antigravity rejects that unknown name).
func geminiThinkingConfigFromEffort(effort string) map[string]any {
	switch effort {
	case "auto":
		return nil
	case "none":
		return map[string]any{"includeThoughts": false, "thinkingBudget": 0}
	case "minimal":
		return map[string]any{"includeThoughts": true, "thinkingLevel": "MINIMAL", "thinkingBudget": 512}
	case "low":
		return map[string]any{"includeThoughts": true, "thinkingLevel": "LOW", "thinkingBudget": 1024}
	case "medium":
		return map[string]any{"includeThoughts": true, "thinkingLevel": "MEDIUM", "thinkingBudget": 8192}
	case "high", "xhigh", "max", "ultra":
		return map[string]any{"includeThoughts": true, "thinkingLevel": "HIGH", "thinkingBudget": 24576}
	default:
		return nil
	}
}

// normalizeGeminiThinkingConfig rewrites legacy / cross-protocol aliases onto the
// upstream ThinkingConfig field names before the payload is sent.
func normalizeGeminiThinkingConfig(raw any) map[string]any {
	config, ok := raw.(map[string]any)
	if !ok || len(config) == 0 {
		return nil
	}
	out := make(map[string]any, len(config)+1)
	for key, value := range config {
		out[key] = value
	}
	if _, hasBudget := out["thinkingBudget"]; !hasBudget {
		if budget, ok := out["budgetTokens"]; ok {
			out["thinkingBudget"] = budget
			delete(out, "budgetTokens")
		} else if budget, ok := out["budget_tokens"]; ok {
			out["thinkingBudget"] = budget
			delete(out, "budget_tokens")
		}
	} else {
		delete(out, "budgetTokens")
		delete(out, "budget_tokens")
	}
	return out
}

func geminiResponseFormat(config map[string]any) map[string]any {
	mime := strings.TrimSpace(anyString(config["responseMimeType"]))
	if mime == "" && config["responseSchema"] == nil {
		return nil
	}
	format := map[string]any{}
	if strings.EqualFold(mime, "application/json") {
		format["type"] = "json_object"
	} else if mime != "" {
		format["type"] = mime
	}
	if schema := config["responseSchema"]; schema != nil {
		format["type"] = "json_schema"
		format["json_schema"] = map[string]any{"schema": schema}
	}
	return format
}

func canonicalMessageFromGoogleGemini(raw any) ([]canonicalMessage, error) {
	content, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("each contents item must be an object")
	}
	role := strings.TrimSpace(anyString(content["role"]))
	if role == "model" {
		role = "assistant"
	}
	if role == "" {
		role = "user"
	}
	parts, ok := content["parts"].([]any)
	if !ok {
		return nil, fmt.Errorf("contents.parts must be an array")
	}
	message := canonicalMessage{Type: "message", Role: role, RawContent: cloneAnyMap(content)}
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("contents.parts items must be objects")
		}
		if text := anyString(part["text"]); text != "" {
			if boolFromMap(part, "thought") {
				message.Thinking = append(message.Thinking, canonicalThinkingBlock{
					Type:      "thinking",
					Thinking:  text,
					Signature: firstNonEmptyGatewayString(anyString(part["thoughtSignature"]), anyString(part["thought_signature"])),
					Raw:       clonePayload(part),
				})
			} else {
				message.Content = append(message.Content, canonicalContentPart{Type: contentPartTypeForRole(role), Text: text, Raw: clonePayload(part)})
			}
			continue
		}
		if inline := geminiDataMap(part, "inlineData", "inline_data"); inline != nil {
			message.Content = append(message.Content, canonicalContentPart{
				Type:     "input_image",
				MimeType: anyString(inline["mimeType"]),
				Data:     anyString(inline["data"]),
				ImageURL: geminiDataURL(inline),
				Raw:      clonePayload(part),
			})
			continue
		}
		if file := geminiDataMap(part, "fileData", "file_data"); file != nil {
			message.Content = append(message.Content, canonicalContentPart{
				Type:     "input_file",
				MimeType: anyString(file["mimeType"]),
				FileData: firstNonEmptyGatewayString(anyString(file["fileUri"]), anyString(file["file_uri"])),
				Raw:      clonePayload(part),
			})
			continue
		}
		if call := geminiDataMap(part, "functionCall", "function_call"); call != nil {
			args, _ := json.Marshal(call["args"])
			message.ToolCalls = append(message.ToolCalls, canonicalToolCall{
				ID:        firstNonEmptyGatewayString(anyString(call["id"]), "call_gemini"),
				Type:      "function",
				Name:      anyString(call["name"]),
				Arguments: string(args),
				Metadata:  geminiPartMetadata(part),
			})
			continue
		}
		if response := geminiDataMap(part, "functionResponse", "function_response"); response != nil {
			message.Type = "function_call_output"
			message.Role = "tool"
			message.ToolCallID = anyString(response["id"])
			message.Name = anyString(response["name"])
			message.Output = response["response"]
			message.RawContent = clonePayload(part)
			continue
		}
		message.Content = append(message.Content, canonicalContentPart{Type: "gemini_raw", Raw: clonePayload(part)})
	}
	return []canonicalMessage{message}, nil
}

func canonicalToolsFromGoogleGemini(raw any) []canonicalTool {
	items, _ := raw.([]any)
	tools := make([]canonicalTool, 0)
	for _, item := range items {
		tool, _ := item.(map[string]any)
		declarations, _ := tool["functionDeclarations"].([]any)
		if len(declarations) == 0 {
			declarations, _ = tool["function_declarations"].([]any)
		}
		for _, declaration := range declarations {
			decl, _ := declaration.(map[string]any)
			tools = append(tools, canonicalTool{
				Type:        "function",
				Name:        anyString(decl["name"]),
				Description: anyString(decl["description"]),
				Parameters:  decl["parameters"],
				Raw:         clonePayload(decl),
			})
		}
	}
	return tools
}

func canonicalToolChoiceFromGoogleGemini(raw any) any {
	config, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	if calling, ok := config["functionCallingConfig"].(map[string]any); ok {
		switch strings.ToUpper(strings.TrimSpace(anyString(calling["mode"]))) {
		case "AUTO":
			return "auto"
		case "NONE":
			return "none"
		case "ANY":
			if names, ok := calling["allowedFunctionNames"].([]any); ok && len(names) == 1 {
				return map[string]any{"type": "function", "name": anyString(names[0])}
			}
			return "required"
		}
	}
	return clonePayload(config)
}

func geminiPartsText(raw any) string {
	parts, _ := raw.([]any)
	texts := make([]string, 0, len(parts))
	for _, item := range parts {
		part, _ := item.(map[string]any)
		if text := strings.TrimSpace(anyString(part["text"])); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func geminiDataMap(part map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := part[key].(map[string]any); ok {
			return value
		}
	}
	return nil
}

func geminiDataURL(data map[string]any) string {
	mime := anyString(data["mimeType"])
	encoded := anyString(data["data"])
	if mime == "" || encoded == "" {
		return ""
	}
	return "data:" + mime + ";base64," + encoded
}

func geminiPartMetadata(part map[string]any) map[string]any {
	metadata := map[string]any{}
	if signature := firstNonEmptyGatewayString(anyString(part["thoughtSignature"]), anyString(part["thought_signature"])); signature != "" {
		metadata["thoughtSignature"] = signature
		metadata["thought_signature"] = signature
	}
	return metadata
}

func cloneAnyMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	return clonePayload(value)
}

func encodeCanonicalRequestToGoogleGemini(request canonicalRequest, candidate routeengine.Candidate) (map[string]any, error) {
	payload := encodeCanonicalRequestToAntigravityGemini(request, candidate.Model.UpstreamName)
	delete(payload, "model")
	if config, ok := payload["generationConfig"].(map[string]any); ok {
		geminiGenerationConfigFromCanonical(config, request)
	}
	return payload, nil
}

func encodeCanonicalImageRequestToGoogleGemini(request canonicalRequest, _ routeengine.Candidate) map[string]any {
	prompt := ""
	imageCount := 1
	params := request.Params
	if request.Image != nil {
		prompt = request.Image.Prompt
		if request.Image.N > 0 {
			imageCount = request.Image.N
		}
		if request.Image.Params != nil {
			params = request.Image.Params
		}
	}
	parts := []any{map[string]any{"text": prompt}}
	if request.Image != nil {
		for _, image := range request.Image.Images {
			if image.ImageURL == "" {
				continue
			}
			if part := antigravityInlineDataPart(image.ImageURL, ""); part != nil {
				parts = append(parts, part)
			}
		}
	}
	payload := map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": parts}},
		"generationConfig": map[string]any{
			"candidateCount":     imageCount,
			"responseModalities": []any{"IMAGE"},
		},
	}
	if imageConfig, ok := params["image_config"].(map[string]any); ok {
		payload["generationConfig"].(map[string]any)["imageConfig"] = imageConfig
	}
	return payload
}

func geminiGenerationConfigFromCanonical(config map[string]any, request canonicalRequest) {
	if raw, ok := request.Params["gemini_generation_config"].(map[string]any); ok {
		for key, value := range raw {
			config[key] = value
		}
	}
	if value, ok := request.Params["candidate_count"]; ok {
		config["candidateCount"] = value
	}
	if value, ok := request.Params["response_modalities"]; ok {
		config["responseModalities"] = value
	}
	if value, ok := request.Params["thinking_config"]; ok {
		if normalized := normalizeGeminiThinkingConfig(value); len(normalized) > 0 {
			config["thinkingConfig"] = normalized
		} else {
			config["thinkingConfig"] = value
		}
	}
	if value, ok := request.Params["image_config"]; ok {
		config["imageConfig"] = value
	}
	if value, ok := request.Params["response_mime_type"]; ok {
		config["responseMimeType"] = value
	}
	if value, ok := request.Params["response_schema"]; ok {
		config["responseSchema"] = value
	}
}

func encodeCanonicalResponseAsGoogleGemini(response canonicalResponse, _ responseConversionOptions) ([]byte, gatewayUsage, error) {
	usage := response.Usage.normalized()
	if !gatewayUsageAvailable(usage) {
		return nil, gatewayUsage{}, fmt.Errorf("Gemini response usage is unavailable")
	}
	parts := make([]any, 0)
	for _, item := range response.Output {
		for _, thinking := range item.Thinking {
			part := map[string]any{"text": thinking.Thinking, "thought": true}
			if thinking.Signature != "" {
				part["thoughtSignature"] = thinking.Signature
			}
			parts = append(parts, part)
		}
		if item.Type == "function_call" {
			call := map[string]any{"name": item.Name, "args": unmarshalJSONObjectOrEmpty(item.Arguments)}
			if item.ID != "" {
				call["id"] = item.ID
			}
			part := map[string]any{"functionCall": call}
			addAntigravityThoughtSignature(part, item.Metadata)
			parts = append(parts, part)
			continue
		}
		if item.Type == "image_generation_call" && item.Result != "" {
			mime := "image/png"
			if item.OutputFormat != "" {
				mime = "image/" + strings.TrimSpace(item.OutputFormat)
			}
			parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": item.Result}})
			continue
		}
		if item.Text != "" {
			parts = append(parts, map[string]any{"text": item.Text})
			continue
		}
		for _, content := range item.Content {
			if content.Text != "" {
				parts = append(parts, map[string]any{"text": content.Text})
			}
		}
	}
	if len(parts) == 0 {
		parts = []any{map[string]any{"text": ""}}
	}
	finishReason := geminiFinishReason(response.FinishReason)
	payload := map[string]any{
		"responseId": response.ID,
		"candidates": []any{map[string]any{
			"content":      map[string]any{"role": "model", "parts": parts},
			"finishReason": finishReason,
		}},
		"usageMetadata": geminiUsageMetadata(usage),
	}
	if response.Model != "" {
		payload["modelVersion"] = response.Model
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, gatewayUsage{}, err
	}
	return body, usage, nil
}

func gatewayUsageAvailable(usage gatewayUsage) bool {
	return usage.TotalTokens > 0 || usage.PromptTokens > 0 || usage.CompletionTokens > 0 || usage.CachedPromptTokens > 0 || usage.ReasoningTokens > 0
}

func geminiFinishReason(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length":
		return "MAX_TOKENS"
	case "content_filter":
		return "SAFETY"
	case "tool_calls":
		return "STOP"
	default:
		return "STOP"
	}
}

func geminiUsageMetadata(usage gatewayUsage) map[string]any {
	usage = usage.normalized()
	metadata := map[string]any{
		"promptTokenCount":     usage.PromptTokens,
		"candidatesTokenCount": usage.CompletionTokens,
		"totalTokenCount":      usage.TotalTokens,
	}
	if usage.CachedPromptTokens > 0 {
		metadata["cachedContentTokenCount"] = usage.CachedPromptTokens
	}
	if usage.ReasoningTokens > 0 {
		metadata["thoughtsTokenCount"] = usage.ReasoningTokens
	}
	return metadata
}
