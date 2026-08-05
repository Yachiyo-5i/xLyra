package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
)

type canonicalProtocol string

const (
	canonicalProtocolOpenAIChat        canonicalProtocol = "openai_chat"
	canonicalProtocolOpenAIResponses   canonicalProtocol = "openai_responses"
	canonicalProtocolOpenAIImages      canonicalProtocol = "openai_images"
	canonicalProtocolOpenAIEmbeddings  canonicalProtocol = "openai_embeddings"
	canonicalProtocolOpenAIAudioSpeech canonicalProtocol = "openai_audio_speech"
	canonicalProtocolCodexResponses    canonicalProtocol = "codex_responses"
	canonicalProtocolAntigravity       canonicalProtocol = "antigravity_gemini"
	canonicalProtocolGoogleGemini      canonicalProtocol = "google_gemini"
	canonicalProtocolAnthropicMessages canonicalProtocol = "anthropic_messages"
	canonicalProtocolGrok              canonicalProtocol = "grok"
)

type canonicalRequest struct {
	DownstreamPath string
	RequestedModel string
	UpstreamModel  string
	SourceProtocol canonicalProtocol
	Stream         bool
	Instructions   string
	RawSystem      any
	Messages       []canonicalMessage
	Tools          []canonicalTool
	ToolChoice     any
	TextFormat     any
	Params         map[string]any
	Image          *canonicalImageRequest
	Raw            map[string]any
}

type canonicalMessage struct {
	Type       string
	Role       string
	Content    []canonicalContentPart
	Thinking   []canonicalThinkingBlock
	RawContent any
	ToolCalls  []canonicalToolCall
	ToolCallID string
	Name       string
	Arguments  string
	Output     any
	ID         string
	Metadata   map[string]any
}

type canonicalContentPart struct {
	Type         string
	Text         string
	ImageURL     string
	FileName     string
	FileData     string
	MimeType     string
	Data         string
	CacheControl any
	Raw          map[string]any
}

type canonicalThinkingBlock struct {
	Type      string
	Thinking  string
	Signature string
	Raw       map[string]any
}

type canonicalTool struct {
	Type        string
	Name        string
	Description string
	Parameters  any
	Raw         map[string]any
}

type canonicalToolCall struct {
	ID        string
	Type      string
	Name      string
	Arguments string
	Metadata  map[string]any
}

type canonicalImageRequest struct {
	Prompt string
	N      int
	Action string
	Images []canonicalImageReference
	Mask   *canonicalImageReference
	Params map[string]any
}

type canonicalImageReference struct {
	ImageURL string
	FileID   string
}

type canonicalResponse struct {
	ID           string
	Model        string
	CreatedAt    int64
	FinishReason string
	Output       []canonicalOutputItem
	Usage        gatewayUsage
	Raw          []byte
}

type canonicalOutputItem struct {
	Type          string
	ID            string
	Role          string
	Status        string
	Text          string
	Content       []canonicalContentPart
	Thinking      []canonicalThinkingBlock
	CallID        string
	Name          string
	Arguments     string
	Metadata      map[string]any
	Result        string
	RevisedPrompt string
	Background    string
	OutputFormat  string
	Quality       string
	Size          string
}

type protocolSpec struct {
	Name           canonicalProtocol
	EndpointType   string
	Path           string
	DecodeRequest  func(map[string]any, string) (canonicalRequest, error)
	EncodeRequest  func(canonicalRequest, routeengine.Candidate) (map[string]any, error)
	DecodeResponse func([]byte) (canonicalResponse, error)
	EncodeResponse func(canonicalResponse, responseConversionOptions) ([]byte, gatewayUsage, error)
}

type paramMapping struct {
	CanonicalKey string
	UpstreamKey  string
}

type responseConversionOptions struct {
	ImageResponseFormat string
	CustomTools         map[string]struct{}
	ResponseTools       map[string]responsesToolIdentity
}

// protocolSpecsRegistry is built once; the specs are read-only lookups, so
// sharing the map avoids rebuilding it on every request.
var protocolSpecsRegistry = buildProtocolSpecs()

func protocolSpecs() map[canonicalProtocol]protocolSpec {
	return protocolSpecsRegistry
}

func buildProtocolSpecs() map[canonicalProtocol]protocolSpec {
	return map[canonicalProtocol]protocolSpec{
		canonicalProtocolOpenAIChat: {
			Name:          canonicalProtocolOpenAIChat,
			EndpointType:  upstreamEndpointTypeOpenAI,
			Path:          gatewayEndpointChatCompletions,
			DecodeRequest: canonicalRequestFromOpenAIChatPayload,
			EncodeRequest: func(request canonicalRequest, candidate routeengine.Candidate) (map[string]any, error) {
				return encodeCanonicalRequestToOpenAIChat(request, candidate), nil
			},
			DecodeResponse: canonicalResponseFromOpenAIChatBody,
			EncodeResponse: func(response canonicalResponse, _ responseConversionOptions) ([]byte, gatewayUsage, error) {
				return encodeCanonicalResponseAsChatCompletion(response)
			},
		},
		canonicalProtocolOpenAIResponses: {
			Name:           canonicalProtocolOpenAIResponses,
			EndpointType:   upstreamEndpointTypeOpenAIResponse,
			Path:           gatewayEndpointResponses,
			DecodeRequest:  canonicalRequestFromOpenAIResponsesPayload,
			EncodeRequest:  encodeCanonicalRequestToOpenAIResponses,
			DecodeResponse: decodeCanonicalResponseFromResponsesBody,
			EncodeResponse: func(response canonicalResponse, options responseConversionOptions) ([]byte, gatewayUsage, error) {
				return encodeCanonicalResponseAsResponses(response, options)
			},
		},
		canonicalProtocolOpenAIImages: {
			Name:         canonicalProtocolOpenAIImages,
			EndpointType: upstreamEndpointTypeOpenAIImage,
			Path:         gatewayEndpointImagesGenerations,
			DecodeRequest: func(payload map[string]any, requestedModel string) (canonicalRequest, error) {
				return canonicalRequestFromOpenAIImagesPayload(payload, requestedModel), nil
			},
			EncodeRequest: func(request canonicalRequest, candidate routeengine.Candidate) (map[string]any, error) {
				return encodeCanonicalRequestToOpenAIImages(request, candidate), nil
			},
			EncodeResponse: func(response canonicalResponse, options responseConversionOptions) ([]byte, gatewayUsage, error) {
				return encodeCanonicalResponseAsImagesWithFormat(response, options.ImageResponseFormat)
			},
		},
		canonicalProtocolAnthropicMessages: {
			Name:           canonicalProtocolAnthropicMessages,
			EndpointType:   "anthropic-messages",
			Path:           gatewayEndpointMessages,
			DecodeRequest:  canonicalRequestFromAnthropicMessagesPayload,
			EncodeRequest:  encodeCanonicalRequestToAnthropicMessages,
			DecodeResponse: canonicalResponseFromAnthropicMessagesBody,
			EncodeResponse: func(response canonicalResponse, _ responseConversionOptions) ([]byte, gatewayUsage, error) {
				return encodeCanonicalResponseAsAnthropicMessage(response)
			},
		},
		canonicalProtocolCodexResponses: {
			Name:          canonicalProtocolCodexResponses,
			EndpointType:  upstreamEndpointTypeOpenAIResponse,
			Path:          codexGatewayResponsesPath,
			DecodeRequest: canonicalRequestFromOpenAIResponsesPayload,
			EncodeRequest: func(request canonicalRequest, candidate routeengine.Candidate) (map[string]any, error) {
				if request.Image != nil {
					return encodeCanonicalImageRequestToCodexResponses(request, candidate), nil
				}
				return encodeCanonicalRequestToOpenAIResponses(request, candidate)
			},
			DecodeResponse: decodeCanonicalResponseFromResponsesBody,
			EncodeResponse: func(response canonicalResponse, options responseConversionOptions) ([]byte, gatewayUsage, error) {
				return encodeCanonicalResponseAsResponses(response, options)
			},
		},
		canonicalProtocolAntigravity: {
			Name: canonicalProtocolAntigravity,
			Path: "/v1internal:generateContent",
			EncodeRequest: func(request canonicalRequest, candidate routeengine.Candidate) (map[string]any, error) {
				return encodeCanonicalRequestToAntigravityGemini(request, candidate.Model.UpstreamName), nil
			},
			DecodeResponse: decodeCanonicalResponseFromAntigravityBody,
		},
		canonicalProtocolGoogleGemini: {
			Name: canonicalProtocolGoogleGemini,
			Path: "/v1beta/models/{model}:generateContent",
			EncodeRequest: func(request canonicalRequest, candidate routeengine.Candidate) (map[string]any, error) {
				payload := encodeCanonicalRequestToAntigravityGemini(request, candidate.Model.UpstreamName)
				delete(payload, "model")
				return payload, nil
			},
			DecodeResponse: decodeCanonicalResponseFromAntigravityBody,
		},
	}
}

func convertRequestBetweenProtocols(from canonicalProtocol, to canonicalProtocol, payload map[string]any, requestedModel string, candidate routeengine.Candidate) (map[string]any, error) {
	specs := protocolSpecs()
	source, ok := specs[from]
	if !ok || source.DecodeRequest == nil {
		return nil, fmt.Errorf("request decoder is not registered for protocol %s", from)
	}
	target, ok := specs[to]
	if !ok || target.EncodeRequest == nil {
		return nil, fmt.Errorf("request encoder is not registered for protocol %s", to)
	}
	canonical, err := source.DecodeRequest(payload, requestedModel)
	if err != nil {
		return nil, err
	}
	encoded, err := target.EncodeRequest(canonical, candidate)
	if err != nil {
		return nil, err
	}
	return applyRequestPolicyForCandidate(encoded, to, candidate), nil
}

func convertResponseBetweenProtocols(from canonicalProtocol, to canonicalProtocol, body []byte, options responseConversionOptions) ([]byte, gatewayUsage, error) {
	specs := protocolSpecs()
	source, ok := specs[from]
	if !ok || source.DecodeResponse == nil {
		return nil, gatewayUsage{}, fmt.Errorf("response decoder is not registered for protocol %s", from)
	}
	target, ok := specs[to]
	if !ok || target.EncodeResponse == nil {
		return nil, gatewayUsage{}, fmt.Errorf("response encoder is not registered for protocol %s", to)
	}
	canonical, err := source.DecodeResponse(body)
	if err != nil {
		return nil, gatewayUsage{}, err
	}
	return target.EncodeResponse(canonical, options)
}

func applyParamMappings(out map[string]any, request canonicalRequest, mappings []paramMapping) {
	if out == nil {
		return
	}
	for _, mapping := range mappings {
		if mapping.CanonicalKey == "" || mapping.UpstreamKey == "" {
			continue
		}
		value, ok := request.Params[mapping.CanonicalKey]
		if !ok {
			continue
		}
		out[mapping.UpstreamKey] = value
	}
}

func canonicalRequestFromOpenAIChatPayload(payload map[string]any, requestedModel string) (canonicalRequest, error) {
	request := canonicalRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		RequestedModel: strings.TrimSpace(requestedModel),
		SourceProtocol: canonicalProtocolOpenAIChat,
		Params:         canonicalParamsFromPayload(payload),
		Raw:            clonePayload(payload),
	}
	request.Stream, _ = payload["stream"].(bool)
	request.Tools = canonicalToolsFromOpenAIChat(payload["tools"])
	request.ToolChoice = payload["tool_choice"]
	if responseFormat, ok := payload["response_format"].(map[string]any); ok {
		request.TextFormat = responseFormat
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return request, fmt.Errorf("messages must be an array")
	}
	instructions := make([]string, 0)
	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.TrimSpace(anyString(msg["role"]))
		if role == "" {
			continue
		}
		content := msg["content"]
		switch role {
		case "system", "developer":
			if text := extractMessageText(content); text != "" {
				instructions = append(instructions, text)
			}
			continue
		case "tool", "function":
			request.Messages = append(request.Messages, canonicalMessage{
				Type:       "function_call_output",
				Role:       role,
				ToolCallID: strings.TrimSpace(anyString(msg["tool_call_id"])),
				Name:       strings.TrimSpace(anyString(msg["name"])),
				Output:     normalizeToolOutput(content),
				RawContent: content,
			})
			continue
		}
		message := canonicalMessage{
			Type:       "message",
			Role:       role,
			RawContent: content,
			Content:    canonicalContentPartsFromChatContent(content, role),
		}
		if role == "assistant" {
			message.ToolCalls = canonicalToolCallsFromChatMessage(msg)
			message.Thinking = canonicalThinkingFromChatMessage(msg)
		}
		request.Messages = append(request.Messages, message)
	}
	request.Instructions = strings.Join(instructions, "\n\n")
	return request, nil
}

func canonicalRequestFromOpenAIResponsesPayload(payload map[string]any, requestedModel string) (canonicalRequest, error) {
	request := canonicalRequest{
		DownstreamPath: gatewayEndpointResponses,
		RequestedModel: strings.TrimSpace(requestedModel),
		SourceProtocol: canonicalProtocolOpenAIResponses,
		Stream:         boolFromMap(payload, "stream"),
		Params:         canonicalParamsFromPayload(payload),
		Raw:            clonePayload(payload),
	}
	request.Tools = canonicalToolsFromOpenAIResponses(payload["tools"])
	request.ToolChoice = payload["tool_choice"]
	if text, ok := payload["text"].(map[string]any); ok {
		request.TextFormat = text["format"]
	}
	input, instructions, tools := canonicalResponsesInputEnvelope(payload["input"])
	if topLevel := strings.TrimSpace(anyString(payload["instructions"])); topLevel != "" {
		instructions = append([]string{topLevel}, instructions...)
	}
	request.Instructions = strings.Join(instructions, "\n\n")
	request.Tools = append(request.Tools, tools...)
	request.Messages = canonicalMessagesFromResponsesInput(input)
	return request, nil
}

func canonicalResponsesInputEnvelope(raw any) (any, []string, []canonicalTool) {
	items, ok := raw.([]any)
	if !ok {
		return raw, nil, nil
	}
	filtered := make([]any, 0, len(items))
	instructions := make([]string, 0)
	tools := make([]canonicalTool, 0)
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		itemType := strings.TrimSpace(anyString(entry["type"]))
		if itemType == "additional_tools" {
			tools = append(tools, canonicalToolsFromOpenAIResponses(entry["tools"])...)
			continue
		}
		if itemType == "message" || itemType == "" {
			role := strings.TrimSpace(anyString(entry["role"]))
			if role == "developer" || role == "system" {
				if text := canonicalResponsesContentText(entry["content"]); text != "" {
					instructions = append(instructions, text)
				}
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered, instructions, tools
}

func canonicalResponsesContentText(content any) string {
	parts := canonicalContentPartsFromResponsesContent(content)
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part.Text); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func canonicalRequestFromOpenAIImagesPayload(payload map[string]any, requestedModel string) canonicalRequest {
	return canonicalRequestFromOpenAIImagesPayloadForPath(payload, requestedModel, gatewayEndpointImagesGenerations)
}

func canonicalRequestFromOpenAIImagesPayloadForPath(payload map[string]any, requestedModel string, downstreamPath string) canonicalRequest {
	if strings.TrimSpace(downstreamPath) == "" {
		downstreamPath = gatewayEndpointImagesGenerations
	}
	action := "generate"
	if strings.TrimSpace(downstreamPath) == gatewayEndpointImagesEdits {
		action = "edit"
	}
	imageParams := canonicalParamsFromPayload(payload)
	n := canonicalImageCountFromPayload(payload)
	request := canonicalRequest{
		DownstreamPath: strings.TrimSpace(downstreamPath),
		RequestedModel: strings.TrimSpace(requestedModel),
		SourceProtocol: canonicalProtocolOpenAIImages,
		Params:         canonicalParamsFromPayload(payload),
		Raw:            clonePayload(payload),
		Image: &canonicalImageRequest{
			Prompt: strings.TrimSpace(anyString(payload["prompt"])),
			N:      n,
			Action: action,
			Images: canonicalImageReferencesFromPayload(payload),
			Mask:   canonicalImageMaskFromPayload(payload),
			Params: imageParams,
		},
	}
	return request
}

func canonicalImageCountFromPayload(payload map[string]any) int {
	if payload == nil {
		return 0
	}
	raw, ok := payload["n"]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case float64:
		if v <= 0 {
			return 0
		}
		return int(v)
	case float32:
		if v <= 0 {
			return 0
		}
		return int(v)
	case int:
		if v <= 0 {
			return 0
		}
		return v
	case int64:
		if v <= 0 {
			return 0
		}
		return int(v)
	case json.Number:
		i, err := v.Int64()
		if err != nil || i <= 0 {
			return 0
		}
		return int(i)
	default:
		return 0
	}
}

func canonicalParamsFromPayload(payload map[string]any) map[string]any {
	params := map[string]any{}
	for key, value := range payload {
		switch key {
		case "model", "messages", "input", "instructions", "tools", "tool_choice", "response_format", "text", "prompt", "images", "image", "mask":
			continue
		default:
			params[key] = value
		}
	}
	if responseFormat, ok := payload["response_format"]; ok {
		params["response_format"] = responseFormat
	}
	if text, ok := payload["text"]; ok {
		params["text"] = text
	}
	return params
}

func canonicalImageReferencesFromPayload(payload map[string]any) []canonicalImageReference {
	if payload == nil {
		return nil
	}
	if refs := canonicalImageReferencesFromValue(payload["images"]); len(refs) > 0 {
		return refs
	}
	return canonicalImageReferencesFromValue(payload["image"])
}

func canonicalImageReferencesFromValue(value any) []canonicalImageReference {
	switch typed := value.(type) {
	case []any:
		refs := make([]canonicalImageReference, 0, len(typed))
		for _, item := range typed {
			if ref := canonicalImageReferenceFromValue(item); ref.hasValue() {
				refs = append(refs, ref)
			}
		}
		return refs
	case []map[string]any:
		refs := make([]canonicalImageReference, 0, len(typed))
		for _, item := range typed {
			if ref := canonicalImageReferenceFromValue(item); ref.hasValue() {
				refs = append(refs, ref)
			}
		}
		return refs
	default:
		if ref := canonicalImageReferenceFromValue(value); ref.hasValue() {
			return []canonicalImageReference{ref}
		}
		return nil
	}
}

func canonicalImageMaskFromPayload(payload map[string]any) *canonicalImageReference {
	if payload == nil {
		return nil
	}
	ref := canonicalImageReferenceFromValue(payload["mask"])
	if !ref.hasValue() {
		return nil
	}
	return &ref
}

func canonicalImageReferenceFromValue(value any) canonicalImageReference {
	switch item := value.(type) {
	case string:
		return canonicalImageReference{ImageURL: canonicalImageURLFromString(item)}
	case map[string]any:
		ref := canonicalImageReference{
			ImageURL: strings.TrimSpace(anyString(normalizeImageURL(item["image_url"]))),
			FileID:   strings.TrimSpace(anyString(item["file_id"])),
		}
		if ref.hasValue() {
			return ref
		}
		for _, key := range []string{"url", "b64_json", "b64"} {
			if text := canonicalImageURLFromString(anyString(item[key])); text != "" {
				return canonicalImageReference{ImageURL: text}
			}
		}
		return canonicalImageReference{}
	default:
		return canonicalImageReference{}
	}
}

func canonicalImageURLFromString(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if strings.HasPrefix(lower, "data:") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return text
	}
	return "data:image/png;base64," + text
}

func (r canonicalImageReference) hasValue() bool {
	return strings.TrimSpace(r.ImageURL) != "" || strings.TrimSpace(r.FileID) != ""
}

func canonicalContentPartsFromChatContent(content any, role string) []canonicalContentPart {
	switch value := content.(type) {
	case string:
		return []canonicalContentPart{{Type: contentPartTypeForRole(role), Text: value}}
	case []any:
		parts := make([]canonicalContentPart, 0, len(value))
		for _, raw := range value {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch strings.TrimSpace(anyString(part["type"])) {
			case "text", "input_text", "output_text":
				parts = append(parts, canonicalContentPart{
					Type: contentPartTypeForRole(role),
					Text: anyString(part["text"]),
					Raw:  part,
				})
			case "image_url", "input_image":
				parts = append(parts, canonicalContentPart{
					Type:     "input_image",
					ImageURL: normalizeImageURLString(part),
					Raw:      part,
				})
			case "file":
				file, _ := part["file"].(map[string]any)
				fileData := strings.TrimSpace(anyString(file["file_data"]))
				if fileData == "" {
					fileData = strings.TrimSpace(anyString(file["data"]))
				}
				if fileData != "" {
					parts = append(parts, canonicalContentPart{
						Type:     "input_file",
						FileName: strings.TrimSpace(anyString(file["filename"])),
						FileData: fileData,
						MimeType: strings.TrimSpace(anyString(file["mime_type"])),
						Raw:      part,
					})
				}
			}
		}
		return parts
	default:
		return []canonicalContentPart{{Type: contentPartTypeForRole(role), Text: fmt.Sprintf("%v", normalizeToolOutput(content))}}
	}
}

func canonicalMessagesFromResponsesInput(raw any) []canonicalMessage {
	switch input := raw.(type) {
	case string:
		if strings.TrimSpace(input) == "" {
			return nil
		}
		return []canonicalMessage{{
			Type:       "message",
			Role:       "user",
			RawContent: input,
			Content:    []canonicalContentPart{{Type: "input_text", Text: input}},
		}}
	case []any:
		messages := make([]canonicalMessage, 0, len(input))
		for _, item := range input {
			entry, _ := item.(map[string]any)
			itemType := strings.TrimSpace(anyString(entry["type"]))
			if itemType == "" && strings.TrimSpace(anyString(entry["role"])) != "" {
				itemType = "message"
			}
			switch itemType {
			case "message", "":
				role := strings.TrimSpace(anyString(entry["role"]))
				if role == "" {
					role = "user"
				}
				messages = append(messages, canonicalMessage{
					Type:       "message",
					Role:       role,
					RawContent: entry["content"],
					Content:    canonicalContentPartsFromResponsesContent(entry["content"]),
					Thinking:   canonicalThinkingFromResponsesItem(entry),
				})
			case "function_call":
				callID := strings.TrimSpace(anyString(entry["call_id"]))
				if callID == "" {
					callID = strings.TrimSpace(anyString(entry["id"]))
				}
				messages = append(messages, canonicalMessage{
					Type:       "function_call",
					ID:         strings.TrimSpace(anyString(entry["id"])),
					ToolCallID: callID,
					Name:       strings.TrimSpace(anyString(entry["name"])),
					Arguments:  anyString(entry["arguments"]),
					Metadata:   toolCallMetadataFromGatewayMap(entry),
				})
			case "custom_tool_call":
				callID := strings.TrimSpace(anyString(entry["call_id"]))
				if callID == "" {
					callID = strings.TrimSpace(anyString(entry["id"]))
				}
				messages = append(messages, canonicalMessage{
					Type:       "function_call",
					ID:         strings.TrimSpace(anyString(entry["id"])),
					ToolCallID: callID,
					Name:       strings.TrimSpace(anyString(entry["name"])),
					Arguments:  bridgedCustomToolArguments(entry["input"]),
					Metadata:   toolCallMetadataFromGatewayMap(entry),
				})
			case "function_call_output":
				messages = append(messages, canonicalMessage{
					Type:       "function_call_output",
					ToolCallID: strings.TrimSpace(anyString(entry["call_id"])),
					Output:     anyString(entry["output"]),
					RawContent: entry["output"],
				})
			case "custom_tool_call_output":
				messages = append(messages, canonicalMessage{
					Type:       "function_call_output",
					ToolCallID: strings.TrimSpace(anyString(entry["call_id"])),
					Output:     entry["output"],
					RawContent: entry["output"],
				})
			}
		}
		return messages
	default:
		return nil
	}
}

func canonicalContentPartsFromResponsesContent(content any) []canonicalContentPart {
	switch value := content.(type) {
	case string:
		return []canonicalContentPart{{Type: "input_text", Text: value}}
	case []any:
		parts := make([]canonicalContentPart, 0, len(value))
		for _, raw := range value {
			part, _ := raw.(map[string]any)
			switch strings.TrimSpace(anyString(part["type"])) {
			case "input_text", "output_text", "":
				parts = append(parts, canonicalContentPart{
					Type: nonEmptyString(anyString(part["type"]), "input_text"),
					Text: anyString(part["text"]),
					Raw:  part,
				})
			case "input_image":
				parts = append(parts, canonicalContentPart{
					Type:     "input_image",
					ImageURL: strings.TrimSpace(anyString(part["image_url"])),
					Raw:      part,
				})
			case "input_file":
				fileData := strings.TrimSpace(anyString(part["file_data"]))
				if fileData == "" {
					fileData = strings.TrimSpace(anyString(part["file_url"]))
				}
				if fileData != "" {
					parts = append(parts, canonicalContentPart{
						Type:     "input_file",
						FileName: strings.TrimSpace(anyString(part["filename"])),
						FileData: fileData,
						MimeType: strings.TrimSpace(anyString(part["mime_type"])),
						Raw:      part,
					})
				}
			}
		}
		return parts
	default:
		return nil
	}
}

func canonicalToolsFromOpenAIChat(raw any) []canonicalTool {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	tools := make([]canonicalTool, 0, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		toolType := strings.TrimSpace(anyString(tool["type"]))
		if toolType == "function" {
			fn, _ := tool["function"].(map[string]any)
			tools = append(tools, canonicalTool{
				Type:        "function",
				Name:        anyString(fn["name"]),
				Description: anyString(fn["description"]),
				Parameters:  fn["parameters"],
				Raw:         tool,
			})
			continue
		}
		tools = append(tools, canonicalTool{Type: toolType, Raw: tool})
	}
	return tools
}

func canonicalToolsFromOpenAIResponses(raw any) []canonicalTool {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	tools := make([]canonicalTool, 0, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		toolType := strings.TrimSpace(anyString(tool["type"]))
		tools = append(tools, canonicalTool{
			Type:        toolType,
			Name:        anyString(tool["name"]),
			Description: anyString(tool["description"]),
			Parameters:  tool["parameters"],
			Raw:         tool,
		})
	}
	return tools
}

func canonicalToolCallsFromChatMessage(msg map[string]any) []canonicalToolCall {
	toolCalls, _ := msg["tool_calls"].([]any)
	result := make([]canonicalToolCall, 0, len(toolCalls))
	for _, raw := range toolCalls {
		toolCall, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		function, _ := toolCall["function"].(map[string]any)
		name := strings.TrimSpace(anyString(function["name"]))
		if name == "" {
			continue
		}
		callID := strings.TrimSpace(anyString(toolCall["id"]))
		if callID == "" {
			callID = strings.TrimSpace(anyString(toolCall["call_id"]))
		}
		result = append(result, canonicalToolCall{
			ID:        callID,
			Type:      nonEmptyString(anyString(toolCall["type"]), "function"),
			Name:      name,
			Arguments: anyString(function["arguments"]),
			Metadata:  toolCallMetadataFromGatewayMap(toolCall),
		})
	}
	return result
}

func encodeCanonicalRequestToOpenAIResponses(request canonicalRequest, candidate routeengine.Candidate) (map[string]any, error) {
	out := map[string]any{"model": candidate.Model.UpstreamName}
	request.UpstreamModel = candidate.Model.UpstreamName
	applyParamMappings(out, request, protocolParamMappings(canonicalProtocolOpenAIResponses, candidate))
	if len(request.Tools) > 0 {
		out["tools"] = encodeCanonicalToolsAsResponses(request.Tools)
	}
	if request.ToolChoice != nil {
		if converted := encodeCanonicalToolChoiceAsResponses(request.ToolChoice); converted != nil {
			out["tool_choice"] = converted
		}
	}
	if request.TextFormat != nil {
		if format, ok := request.TextFormat.(map[string]any); ok {
			if converted := encodeCanonicalTextFormatAsResponsesText(format, candidate); converted != nil {
				out["text"] = converted
			}
		}
	}
	if maxOutputTokens, ok := intFromAny(request.Params["max_output_tokens"]); ok && maxOutputTokens > 0 {
		out["max_output_tokens"] = maxOutputTokens
	} else if maxCompletionTokens, ok := intFromAny(request.Params["max_completion_tokens"]); ok && maxCompletionTokens > 0 {
		out["max_output_tokens"] = maxCompletionTokens
	} else if maxTokens, ok := intFromAny(request.Params["max_tokens"]); ok && maxTokens > 0 {
		out["max_output_tokens"] = maxTokens
	}
	if reasoningEffort, ok := request.Params["reasoning_effort"].(string); ok && strings.TrimSpace(reasoningEffort) != "" && modelCapability(canonicalProtocolOpenAIResponses, candidate, "reasoning_summary") {
		out["reasoning"] = map[string]any{
			"effort":  strings.TrimSpace(reasoningEffort),
			"summary": "detailed",
		}
	}
	if request.Instructions != "" {
		out["instructions"] = request.Instructions
	}
	out["input"] = encodeCanonicalMessagesAsResponsesInput(request.Messages)
	return out, nil
}

func encodeCanonicalRequestToOpenAIChat(request canonicalRequest, candidate routeengine.Candidate) map[string]any {
	out := map[string]any{
		"model":    candidate.Model.UpstreamName,
		"messages": []any{},
	}
	applyParamMappings(out, request, protocolParamMappings(canonicalProtocolOpenAIChat, candidate))
	if maxOutputTokens, ok := intFromAny(request.Params["max_output_tokens"]); ok && maxOutputTokens > 0 {
		out["max_tokens"] = maxOutputTokens
	}
	if reasoning, ok := request.Params["reasoning"].(map[string]any); ok {
		if effort := strings.TrimSpace(anyString(reasoning["effort"])); effort != "" {
			out["reasoning_effort"] = effort
		}
	}
	if request.TextFormat != nil {
		out["response_format"] = encodeCanonicalTextFormatAsChatResponseFormat(request.TextFormat)
	}
	if len(request.Tools) > 0 {
		if converted := encodeCanonicalToolsAsChat(request.Tools); converted != nil {
			out["tools"] = converted
		}
	}
	if request.ToolChoice != nil {
		if converted := encodeCanonicalToolChoiceAsChat(request.ToolChoice); converted != nil {
			out["tool_choice"] = converted
		}
	}
	messages := make([]any, 0, len(request.Messages)+1)
	if request.Instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": request.Instructions})
	}
	messages = append(messages, encodeCanonicalMessagesAsChatMessages(request.Messages)...)
	out["messages"] = messages
	return out
}

func encodeCanonicalRequestToOpenAIImages(request canonicalRequest, candidate routeengine.Candidate) map[string]any {
	out := clonePayload(request.Raw)
	out["model"] = candidate.Model.UpstreamName
	if request.Image != nil {
		if request.Image.Prompt != "" {
			out["prompt"] = request.Image.Prompt
		}
		if len(request.Image.Images) > 0 {
			out["images"] = encodeCanonicalImageReferences(request.Image.Images)
		}
		if request.Image.Mask != nil && strings.TrimSpace(request.Image.Mask.ImageURL) != "" {
			out["mask"] = map[string]any{"image_url": request.Image.Mask.ImageURL}
		}
	}
	return out
}

func encodeCanonicalImageReferences(refs []canonicalImageReference) []any {
	result := make([]any, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.ImageURL) != "" {
			result = append(result, map[string]any{"image_url": ref.ImageURL})
		}
	}
	return result
}

func encodeCanonicalImageRequestToCodexResponses(request canonicalRequest, candidate routeengine.Candidate) map[string]any {
	prompt := ""
	if request.Image != nil {
		prompt = request.Image.Prompt
	}

	contentParts := []any{
		map[string]any{"type": "input_text", "text": prompt},
	}
	if request.Image != nil {
		for _, img := range request.Image.Images {
			if strings.TrimSpace(img.ImageURL) != "" {
				contentParts = append(contentParts, map[string]any{
					"type":      "input_image",
					"image_url": img.ImageURL,
				})
			}
		}
	}
	input := []any{
		map[string]any{
			"type":    "message",
			"role":    "user",
			"content": contentParts,
		},
	}

	responsesPayload := map[string]any{
		"model":        codexResponsesHostModel(map[string]any{"tools": []any{map[string]any{"type": "image_generation"}}}, candidate),
		"instructions": "",
		"input":        input,
		"stream":       true,
		"reasoning": map[string]any{
			"effort":  "medium",
			"summary": "auto",
		},
		"tools": []any{
			codexImageGenerationToolFromCanonical(request, candidate),
		},
		"tool_choice": map[string]any{"type": "image_generation"},
	}
	for _, key := range []string{"previous_response_id", "metadata"} {
		if value, ok := request.Params[key]; ok {
			responsesPayload[key] = value
		}
	}
	return responsesPayload
}

func codexImageGenerationToolFromCanonical(request canonicalRequest, candidate routeengine.Candidate) map[string]any {
	tool := map[string]any{
		"type":  "image_generation",
		"model": candidate.Model.UpstreamName,
	}
	params := request.Params
	if request.Image != nil && request.Image.Params != nil {
		params = request.Image.Params
	}
	for _, key := range []string{
		"background",
		"moderation",
		"output_format",
		"quality",
		"size",
		"partial_images",
		"output_compression",
		"input_fidelity",
	} {
		if value, ok := params[key]; ok {
			tool[key] = value
		}
	}
	if request.Image != nil {
		if request.Image.Action == "edit" {
			tool["action"] = "edit"
		}
		if request.Image.Mask != nil && strings.TrimSpace(request.Image.Mask.ImageURL) != "" {
			tool["input_image_mask"] = map[string]any{"image_url": request.Image.Mask.ImageURL}
		}
		if request.Image.N > 1 && !codexImageModelDallE3(candidate.Model.UpstreamName) {
			tool["n"] = request.Image.N
		}
	}
	return tool
}

func codexImageModelDallE3(model string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(model)), "dall-e-3")
}

func encodeCanonicalMessagesAsResponsesInput(messages []canonicalMessage) []any {
	input := make([]any, 0, len(messages))
	for _, message := range messages {
		switch message.Type {
		case "function_call_output":
			callID := strings.TrimSpace(message.ToolCallID)
			output := normalizeToolOutput(message.Output)
			if callID == "" {
				input = append(input, map[string]any{"role": "user", "content": output})
			} else {
				input = append(input, map[string]any{"type": "function_call_output", "call_id": callID, "output": output})
			}
		case "function_call":
			item := map[string]any{
				"type":      "function_call",
				"call_id":   nonEmptyString(message.ToolCallID, message.ID),
				"name":      message.Name,
				"arguments": message.Arguments,
			}
			addToolCallMetadataToGenericMap(item, message.Metadata)
			input = append(input, item)
		default:
			if message.Role == "assistant" {
				if reasoning := responsesReasoningItem(message.Thinking); reasoning != nil {
					input = append(input, reasoning)
				}
			}
			if len(message.Content) > 0 || len(message.ToolCalls) == 0 {
				item := map[string]any{"role": message.Role}
				if len(message.Content) == 1 && message.Content[0].Type != "input_image" && message.RawContent != nil {
					if text, ok := message.RawContent.(string); ok {
						item["content"] = text
					} else {
						item["content"] = encodeCanonicalContentAsResponses(message.Content, message.Role)
					}
				} else if len(message.Content) > 0 {
					item["content"] = encodeCanonicalContentAsResponses(message.Content, message.Role)
				} else {
					item["content"] = normalizeToolOutput(message.RawContent)
				}
				input = append(input, item)
			}
			if message.Role == "assistant" {
				for _, toolCall := range message.ToolCalls {
					item := map[string]any{
						"type":      "function_call",
						"call_id":   toolCall.ID,
						"name":      toolCall.Name,
						"arguments": toolCall.Arguments,
					}
					addToolCallMetadataToGenericMap(item, toolCall.Metadata)
					input = append(input, item)
				}
			}
		}
	}
	return input
}

func encodeCanonicalMessagesAsChatMessages(messages []canonicalMessage) []any {
	out := make([]any, 0, len(messages))
	for _, message := range messages {
		switch message.Type {
		case "function_call":
			callID := nonEmptyString(message.ToolCallID, message.ID)
			item := map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id":   callID,
					"type": "function",
					"function": map[string]any{
						"name":      message.Name,
						"arguments": message.Arguments,
					},
				}},
			}
			if toolCalls, ok := item["tool_calls"].([]any); ok && len(toolCalls) > 0 {
				if toolCall, ok := toolCalls[0].(map[string]any); ok {
					addToolCallMetadataToGenericMap(toolCall, message.Metadata)
				}
			}
			out = append(out, item)
		case "function_call_output":
			out = append(out, map[string]any{
				"role":         "tool",
				"tool_call_id": message.ToolCallID,
				"content":      anyString(message.Output),
			})
		default:
			role := strings.TrimSpace(message.Role)
			if role == "developer" {
				role = "system"
			}
			if role == "" {
				role = "user"
			}
			item := map[string]any{
				"role":    role,
				"content": encodeCanonicalContentAsChat(message.Content),
			}
			if role == "assistant" {
				if reasoning := chatReasoningContent(message.Thinking); reasoning != "" {
					item["reasoning_content"] = reasoning
				}
				for _, block := range message.Thinking {
					if signature := strings.TrimSpace(block.Signature); signature != "" {
						item["thinking_signature"] = signature
						break
					}
				}
			}
			if len(message.ToolCalls) > 0 {
				item["tool_calls"] = encodeCanonicalToolCallsAsChat(message.ToolCalls)
			}
			out = append(out, item)
		}
	}
	return out
}

func encodeCanonicalContentAsResponses(parts []canonicalContentPart, role string) any {
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "input_image":
			out = append(out, map[string]any{"type": "input_image", "image_url": part.ImageURL})
		case "input_file":
			item := map[string]any{"type": "input_file", "file_data": part.FileData}
			if part.FileName != "" {
				item["filename"] = part.FileName
			}
			out = append(out, item)
		default:
			out = append(out, map[string]any{"type": contentPartTypeForRole(role), "text": part.Text})
		}
	}
	return out
}

func encodeCanonicalContentAsChat(parts []canonicalContentPart) any {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 && parts[0].Type != "input_image" && parts[0].Type != "input_file" {
		return parts[0].Text
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "input_image":
			out = append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": part.ImageURL}})
		case "input_file":
			file := map[string]any{"file_data": part.FileData}
			if part.FileName != "" {
				file["filename"] = part.FileName
			}
			out = append(out, map[string]any{"type": "file", "file": file})
		default:
			out = append(out, map[string]any{"type": "text", "text": part.Text})
		}
	}
	return out
}

func encodeCanonicalToolsAsResponses(tools []canonicalTool) any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			if tool.Raw != nil {
				out = append(out, tool.Raw)
			}
			continue
		}
		out = append(out, map[string]any{
			"type":        "function",
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		})
	}
	return out
}

func encodeCanonicalToolsAsChat(tools []canonicalTool) any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters,
			},
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func encodeCanonicalToolChoiceAsResponses(raw any) any {
	switch value := raw.(type) {
	case string:
		return value
	case map[string]any:
		if strings.TrimSpace(anyString(value["type"])) == "function" {
			if fn, ok := value["function"].(map[string]any); ok {
				if name := strings.TrimSpace(anyString(fn["name"])); name != "" {
					return map[string]any{"type": "function", "name": name}
				}
			}
		}
		return value
	default:
		return nil
	}
}

func encodeCanonicalToolChoiceAsChat(raw any) any {
	switch choice := raw.(type) {
	case string:
		if choice == "none" || choice == "auto" || choice == "required" {
			return choice
		}
	case map[string]any:
		if strings.TrimSpace(anyString(choice["type"])) == "function" {
			if name := canonicalToolChoiceFunctionName(choice); name != "" {
				return map[string]any{
					"type": "function",
					"function": map[string]any{
						"name": name,
					},
				}
			}
		}
	}
	return nil
}

// canonicalToolChoiceFunctionName reads the forced function name from either the
// flat ({"name"}) or OpenAI-nested ({"function":{"name"}}) tool_choice shape.
func canonicalToolChoiceFunctionName(value map[string]any) string {
	if name := strings.TrimSpace(anyString(value["name"])); name != "" {
		return name
	}
	if fn, ok := value["function"].(map[string]any); ok {
		return strings.TrimSpace(anyString(fn["name"]))
	}
	return ""
}

func encodeCanonicalTextFormatAsResponsesText(raw map[string]any, candidate routeengine.Candidate) any {
	if !modelCapability(canonicalProtocolOpenAIResponses, candidate, "text_format") {
		return nil
	}
	formatType := strings.TrimSpace(anyString(raw["type"]))
	if formatType == "" {
		return nil
	}
	format := map[string]any{"type": formatType}
	if formatType == "json_schema" {
		if schema, ok := raw["json_schema"].(map[string]any); ok {
			for key, value := range schema {
				if key == "type" {
					continue
				}
				format[key] = value
			}
			if nested, ok := format["json_schema"].(map[string]any); ok {
				for key, value := range nested {
					if _, exists := format[key]; !exists {
						format[key] = value
					}
				}
				delete(format, "json_schema")
			}
		}
	}
	return map[string]any{"format": format}
}

func encodeCanonicalTextFormatAsChatResponseFormat(raw any) any {
	format, _ := raw.(map[string]any)
	if len(format) == 0 {
		return nil
	}
	switch strings.TrimSpace(anyString(format["type"])) {
	case "json_schema":
		return map[string]any{"type": "json_schema", "json_schema": format}
	case "json_object":
		return map[string]any{"type": "json_object"}
	default:
		return nil
	}
}

func encodeCanonicalToolCallsAsChat(toolCalls []canonicalToolCall) []any {
	out := make([]any, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		item := map[string]any{
			"id":   toolCall.ID,
			"type": nonEmptyString(toolCall.Type, "function"),
			"function": map[string]any{
				"name":      toolCall.Name,
				"arguments": toolCall.Arguments,
			},
		}
		addToolCallMetadataToGenericMap(item, toolCall.Metadata)
		out = append(out, item)
	}
	return out
}

func addToolCallMetadataToGenericMap(item map[string]any, metadata map[string]any) {
	signature := firstNonEmptyGatewayString(
		rawStringFromAny(metadata["thought_signature"]),
		rawStringFromAny(metadata["thoughtSignature"]),
	)
	if signature != "" {
		item["thought_signature"] = signature
	}
}

func canonicalToolCallMetadata(raw map[string]any) map[string]any {
	return toolCallMetadataFromGatewayMap(raw)
}

func toolCallMetadataFromGatewayMap(raw map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"thought_signature", "thoughtSignature"} {
		if value := rawStringFromAny(raw[key]); value != "" {
			out[key] = value
		}
	}
	if namespace := strings.TrimSpace(anyString(raw["namespace"])); namespace != "" {
		out["namespace"] = namespace
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func canonicalResponseFromOpenAIChatBody(body []byte) (canonicalResponse, error) {
	var root struct {
		ID      string          `json:"id"`
		Created int64           `json:"created"`
		Model   string          `json:"model"`
		Choices []chatChoice    `json:"choices"`
		Usage   completionUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return canonicalResponse{}, err
	}
	if root.ID == "" {
		root.ID = synthesizedResponseID()
	}
	if root.Created == 0 {
		root.Created = time.Now().Unix()
	}
	response := canonicalResponse{
		ID:        root.ID,
		Model:     root.Model,
		CreatedAt: root.Created,
		Usage:     root.Usage.normalized(),
		Raw:       body,
	}
	for _, choice := range root.Choices {
		thinking := canonicalThinkingFromChatMessage(map[string]any{
			"reasoning_content":  choice.Message.ReasoningContent,
			"thinking":           choice.Message.Thinking,
			"thinking_signature": choice.Message.ThinkingSignature,
		})
		if len(thinking) > 0 {
			response.Output = append(response.Output, canonicalOutputItem{
				ID:       fmt.Sprintf("thinking_%s_%d", root.ID, choice.Index),
				Type:     "reasoning",
				Status:   "completed",
				Role:     "assistant",
				Thinking: thinking,
			})
		}
		text := strings.TrimSpace(anyString(choice.Message.Content))
		if text != "" {
			response.Output = append(response.Output, canonicalOutputItem{
				ID:      fmt.Sprintf("msg_%s_%d", root.ID, choice.Index),
				Type:    "message",
				Status:  "completed",
				Role:    "assistant",
				Text:    text,
				Content: []canonicalContentPart{{Type: "output_text", Text: text}},
			})
		}
		for _, toolCall := range choice.Message.ToolCalls {
			response.Output = append(response.Output, canonicalOutputItem{
				ID:        fmt.Sprintf("fc_%s", toolCall.ID),
				Type:      "function_call",
				Status:    "completed",
				CallID:    toolCall.ID,
				Name:      toolCall.Function.Name,
				Arguments: toolCall.Function.Arguments,
				Metadata: toolCallMetadataFromGatewayMap(map[string]any{
					"thought_signature": toolCall.ThoughtSignature,
					"thoughtSignature":  toolCall.ThoughtSignatureCamel,
				}),
			})
		}
	}
	return response, nil
}

func decodeCanonicalResponseFromResponsesBody(body []byte) (canonicalResponse, error) {
	var response responsesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return canonicalResponse{}, err
	}
	return canonicalResponseFromResponsesResponse(response), nil
}

func decodeCanonicalResponseFromAntigravityBody(body []byte) (canonicalResponse, error) {
	gemini, err := antigravityUnwrapGeminiBody(body)
	if err != nil {
		return canonicalResponse{}, err
	}
	if images := canonicalImageOutputFromAntigravityGemini(gemini); len(images) > 0 {
		response := canonicalResponse{
			ID:        "resp_" + uuid.NewString(),
			Model:     stringFromMapAny(gemini, "model"),
			CreatedAt: time.Now().Unix(),
			Usage:     antigravityUsage(gemini),
			Output:    images,
		}
		return response, nil
	}
	return canonicalResponseFromAntigravityGemini(gemini), nil
}

func canonicalResponseFromResponsesResponse(response responsesResponse) canonicalResponse {
	out := canonicalResponse{
		ID:        response.ID,
		Model:     response.Model,
		CreatedAt: response.CreatedAt,
		Usage:     completionUsageFromResponsesUsage(response.Usage),
	}
	if response.Usage != nil {
		out.Usage.OutputImageTokens = response.Usage.OutputTokens
		if response.Usage.InputTokensDetails != nil {
			out.Usage.InputImageTokens = response.Usage.InputTokensDetails.ImageTokens
			out.Usage.InputTextTokens = response.Usage.InputTokens - response.Usage.InputTokensDetails.ImageTokens - response.Usage.InputTokensDetails.AudioTokens
			if out.Usage.InputTextTokens < 0 {
				out.Usage.InputTextTokens = response.Usage.InputTokens
			}
		}
	}
	for _, item := range response.Output {
		output := canonicalOutputItem{
			Type:   item.Type,
			ID:     item.ID,
			Role:   item.Role,
			Status: item.Status,
			Thinking: canonicalThinkingFromResponsesItem(map[string]any{
				"type":               item.Type,
				"content":            responsesOutputContentAsAny(item.Content),
				"reasoning_content":  item.ReasoningContent,
				"thinking":           item.Thinking,
				"thinking_signature": item.ThinkingSignature,
			}),
			CallID:        item.CallID,
			Name:          item.Name,
			Arguments:     item.Arguments,
			Result:        item.Result,
			RevisedPrompt: item.RevisedPrompt,
			Background:    item.Background,
			OutputFormat:  item.OutputFormat,
			Quality:       item.Quality,
			Size:          item.Size,
		}
		for _, content := range item.Content {
			output.Content = append(output.Content, canonicalContentPart{Type: content.Type, Text: content.Text})
			if strings.TrimSpace(content.Text) != "" {
				output.Text += content.Text
			}
		}
		out.Output = append(out.Output, output)
	}
	return out
}

func encodeCanonicalResponseAsResponses(response canonicalResponse, options responseConversionOptions) ([]byte, gatewayUsage, error) {
	output := make([]any, 0, len(response.Output))
	for index, item := range response.Output {
		fallbackID := fmt.Sprintf("%s_%d", responseIDOrFallback(response.ID), index)
		switch item.Type {
		case "reasoning":
			if reasoning := responsesReasoningItem(item.Thinking); reasoning != nil {
				reasoning["id"] = responsesItemIDForType("reasoning", item.ID, fallbackID)
				output = append(output, reasoning)
			}
		case "function_call":
			fallback := strings.TrimSpace(item.CallID)
			if fallback == "" {
				fallback = fallbackID
			}
			if _, custom := options.CustomTools[item.Name]; custom {
				output = append(output, map[string]any{
					"id":      responsesItemIDForType("custom_tool_call", item.ID, fallback),
					"type":    "custom_tool_call",
					"status":  nonEmptyString(item.Status, "completed"),
					"call_id": item.CallID,
					"name":    item.Name,
					"input":   bridgedCustomToolInputFromArguments(item.Arguments),
				})
				continue
			}
			name := item.Name
			call := map[string]any{
				"id":        responsesItemIDForType("function_call", item.ID, fallback),
				"type":      "function_call",
				"status":    nonEmptyString(item.Status, "completed"),
				"call_id":   item.CallID,
				"arguments": item.Arguments,
			}
			if identity, ok := options.ResponseTools[item.Name]; ok {
				name = identity.Name
				call["namespace"] = identity.Namespace
			}
			call["name"] = name
			output = append(output, call)
		case "image_generation_call":
			output = append(output, map[string]any{
				"id":             responsesItemIDForType("image_generation_call", item.ID, fallbackID),
				"type":           "image_generation_call",
				"status":         nonEmptyString(item.Status, "completed"),
				"result":         item.Result,
				"revised_prompt": item.RevisedPrompt,
				"background":     item.Background,
				"output_format":  item.OutputFormat,
				"quality":        item.Quality,
				"size":           item.Size,
			})
		default:
			if reasoning := responsesReasoningItem(item.Thinking); reasoning != nil {
				reasoning["id"] = responsesItemIDForType("reasoning", item.ID, fallbackID)
				output = append(output, reasoning)
			}
			text := item.Text
			if text == "" {
				for _, content := range item.Content {
					text += content.Text
				}
			}
			output = append(output, map[string]any{
				"id":     responsesItemIDForType("message", item.ID, fallbackID),
				"type":   "message",
				"status": nonEmptyString(item.Status, "completed"),
				"role":   nonEmptyString(item.Role, "assistant"),
				"content": []any{map[string]any{
					"type":        "output_text",
					"text":        text,
					"annotations": []any{},
				}},
			})
		}
	}
	body := map[string]any{
		"id":                 responseIDOrFallback(response.ID),
		"object":             "response",
		"created_at":         response.CreatedAt,
		"status":             "completed",
		"background":         false,
		"error":              nil,
		"incomplete_details": nil,
		"model":              response.Model,
		"output":             output,
		"usage":              responsesUsagePayload(completionUsageFromGatewayUsage(response.Usage)),
	}
	encoded, err := json.Marshal(body)
	return encoded, response.Usage, err
}

func bridgedCustomToolArguments(input any) string {
	inputText, ok := input.(string)
	if !ok {
		encodedInput, err := json.Marshal(input)
		if err != nil {
			inputText = ""
		} else {
			inputText = string(encodedInput)
		}
	}
	encoded, err := json.Marshal(map[string]any{"input": inputText})
	if err != nil {
		return `{"input":""}`
	}
	return string(encoded)
}

func bridgedCustomToolInputFromArguments(arguments string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(arguments), &parsed); err == nil {
		if input, ok := parsed["input"].(string); ok {
			return input
		}
		if input, ok := parsed["input"]; ok {
			encoded, err := json.Marshal(input)
			if err == nil {
				return string(encoded)
			}
		}
	}
	return arguments
}

func encodeCanonicalResponseAsChatCompletion(response canonicalResponse) ([]byte, gatewayUsage, error) {
	text := ""
	toolCalls := make([]map[string]any, 0)
	for _, item := range response.Output {
		switch item.Type {
		case "function_call":
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			callID := strings.TrimSpace(item.CallID)
			if callID == "" {
				callID = strings.TrimSpace(item.ID)
			}
			toolCalls = append(toolCalls, map[string]any{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": item.Arguments,
				},
			})
		default:
			if item.Type == "reasoning" {
				continue
			}
			if item.Type != "message" {
				continue
			}
			if item.Role != "" && item.Role != "assistant" {
				continue
			}
			if item.Text != "" {
				text += item.Text
				continue
			}
			for _, content := range item.Content {
				if strings.TrimSpace(content.Text) != "" {
					text += content.Text
				}
			}
		}
	}
	message := map[string]any{"role": "assistant", "content": text}
	if reasoning := chatReasoningContent(canonicalThinkingBlocksFromOutput(response.Output)); reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	finishReason := nonEmptyString(response.FinishReason, "stop")
	if len(toolCalls) > 0 && text == "" {
		message["content"] = ""
		message["tool_calls"] = toolCalls
		finishReason = "tool_calls"
	}
	body := map[string]any{
		"id":      responseIDOrFallback(response.ID),
		"object":  "chat.completion",
		"created": response.CreatedAt,
		"model":   response.Model,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": completionUsageFromGatewayUsage(response.Usage),
	}
	encoded, err := json.Marshal(body)
	return encoded, response.Usage, err
}

func encodeCanonicalResponseAsImagesWithFormat(response canonicalResponse, responseFormat string) ([]byte, gatewayUsage, error) {
	format := strings.ToLower(strings.TrimSpace(responseFormat))
	if format == "" {
		format = "b64_json"
	}
	envelope := openAIImagesResponse{
		Created: response.CreatedAt,
		Data:    make([]map[string]any, 0),
	}
	for _, item := range response.Output {
		if !strings.EqualFold(strings.TrimSpace(item.Type), "image_generation_call") {
			continue
		}
		result := strings.TrimSpace(item.Result)
		if result == "" {
			continue
		}
		image := map[string]any{}
		if format == "url" {
			mimeType := strings.TrimSpace(item.OutputFormat)
			if mimeType == "" {
				mimeType = "image/png"
			}
			if !strings.HasPrefix(mimeType, "image/") {
				mimeType = "image/" + mimeType
			}
			image["url"] = "data:" + mimeType + ";base64," + result
		} else {
			image["b64_json"] = result
		}
		if revisedPrompt := strings.TrimSpace(item.RevisedPrompt); revisedPrompt != "" {
			image["revised_prompt"] = revisedPrompt
		}
		envelope.Data = append(envelope.Data, image)
		if envelope.OutputFormat == "" {
			envelope.OutputFormat = strings.TrimSpace(item.OutputFormat)
		}
		if envelope.Quality == "" {
			envelope.Quality = strings.TrimSpace(item.Quality)
		}
		if envelope.Size == "" {
			envelope.Size = strings.TrimSpace(item.Size)
		}
		if envelope.Background == "" {
			envelope.Background = strings.TrimSpace(item.Background)
		}
	}
	if len(envelope.Data) == 0 {
		return nil, gatewayUsage{}, fmt.Errorf("responses body did not contain image_generation_call output")
	}
	if response.Usage.TotalTokens > 0 || response.Usage.PromptTokens > 0 || response.Usage.CompletionTokens > 0 {
		envelope.Usage = &openAIImagesUsage{
			InputTokens:  response.Usage.PromptTokens,
			OutputTokens: response.Usage.CompletionTokens,
			TotalTokens:  response.Usage.TotalTokens,
		}
		envelope.Usage.InputTokensDetails.ImageTokens = response.Usage.InputImageTokens
		envelope.Usage.InputTokensDetails.TextTokens = response.Usage.InputTextTokens
		if envelope.Usage.InputTokensDetails.TextTokens == 0 {
			envelope.Usage.InputTokensDetails.TextTokens = response.Usage.PromptTokens - response.Usage.InputImageTokens
		}
		envelope.Usage.OutputTokensDetails.ImageTokens = response.Usage.outputImageTokensOrCompletion()
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, gatewayUsage{}, err
	}
	return encoded, parseOpenAIImagesUsage(encoded), nil
}

func canonicalImageOutputFromAntigravityGemini(gemini map[string]any) []canonicalOutputItem {
	output := make([]canonicalOutputItem, 0)
	for _, image := range antigravityGeminiImages(gemini) {
		data := strings.TrimSpace(stringFromMapAny(image, "data"))
		if data == "" {
			continue
		}
		mimeType := strings.TrimSpace(firstNonEmptyGatewayString(stringFromMapAny(image, "mimeType"), stringFromMapAny(image, "mime_type")))
		if mimeType == "" {
			mimeType = "image/png"
		}
		outputFormat := strings.TrimPrefix(mimeType, "image/")
		output = append(output, canonicalOutputItem{
			ID:           "ig_" + uuid.NewString(),
			Type:         "image_generation_call",
			Status:       "completed",
			Result:       data,
			OutputFormat: outputFormat,
		})
	}
	return output
}

func completionUsageFromGatewayUsage(usage gatewayUsage) completionUsage {
	return completionUsage{
		PromptTokens:             usage.PromptTokens,
		CompletionTokens:         usage.CompletionTokens,
		TotalTokens:              usage.TotalTokens,
		CachedPromptTokens:       usage.CachedPromptTokens,
		CacheWriteTokens:         usage.CacheWriteTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		ReasoningTokens:          usage.ReasoningTokens,
	}.normalized()
}

func contentPartTypeForRole(role string) string {
	if strings.EqualFold(strings.TrimSpace(role), "assistant") {
		return "output_text"
	}
	return "input_text"
}

func normalizeImageURLString(part map[string]any) string {
	if nested, ok := part["image_url"].(map[string]any); ok {
		return strings.TrimSpace(anyString(nested["url"]))
	}
	return strings.TrimSpace(anyString(normalizeImageURL(part["image_url"])))
}

func boolFromMap(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func stringFromPayloadModel(payload map[string]any) string {
	return strings.TrimSpace(anyString(payload["model"]))
}
