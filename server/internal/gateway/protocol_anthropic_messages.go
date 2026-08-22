package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/httpx"
	routeengine "xlyra/server/internal/router"
)

const (
	gatewayEndpointMessages   = "/v1/messages"
	defaultAnthropicMaxTokens = 8192
)

type anthropicMessagesEndpointAdapter struct{}

func (anthropicMessagesEndpointAdapter) DownstreamPath() string {
	return gatewayEndpointMessages
}

func (anthropicMessagesEndpointAdapter) RouteEndpointType() string {
	return upstreamEndpointTypeAnthropicMessages
}

func (anthropicMessagesEndpointAdapter) DecodeRequest(r *http.Request) (gatewayRequest, *chatFailure) {
	var payload map[string]any
	if err := httpx.DecodeJSONBody(r, &payload); err != nil {
		return gatewayRequest{}, decodeRequestFailure(err)
	}

	model := strings.TrimSpace(anyString(payload["model"]))
	if model == "" {
		return gatewayRequest{}, &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_model",
			message: "model is required",
			stage:   "validate",
		}
	}

	canonical, err := canonicalRequestFromAnthropicMessagesPayload(payload, model)
	if err != nil {
		return gatewayRequest{}, &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_messages_request",
			message: err.Error(),
			stage:   "validate",
		}
	}

	return gatewayRequest{
		DownstreamPath:    gatewayEndpointMessages,
		DownstreamHeaders: r.Header.Clone(),
		RequestedModel:    model,
		Stream:            canonical.Stream,
		Payload:           payload,
		Canonical:         &canonical,
	}, nil
}

type anthropicMessagesProtocolAdapter struct {
	downstreamProtocol canonicalProtocol
	includeUsage       bool
	state              *anthropicMessagesProtocolState
}

type anthropicMessagesProtocolState struct {
	responseTools map[string]responsesToolIdentity
}

type responsesToolIdentity struct {
	Name      string
	Namespace string
}

func newAnthropicMessagesProtocolAdapter(downstream canonicalProtocol) anthropicMessagesProtocolAdapter {
	return anthropicMessagesProtocolAdapter{
		downstreamProtocol: downstream,
		state:              &anthropicMessagesProtocolState{},
	}
}

func (a anthropicMessagesProtocolAdapter) ProtocolName() string {
	switch a.downstreamProtocol {
	case canonicalProtocolOpenAIChat:
		return "anthropic_messages_to_chat_completions"
	case canonicalProtocolOpenAIResponses:
		return "anthropic_messages_to_responses"
	}
	return "anthropic_messages"
}

func (a anthropicMessagesProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if request.DownstreamPath == gatewayEndpointMessages {
		payload := clonePayload(request.Payload)
		payload["model"] = candidate.Model.UpstreamName
		return applyRequestPolicyForCandidate(payload, canonicalProtocolAnthropicMessages, candidate), nil
	}
	if request.Canonical != nil {
		canonical, responseTools, err := prepareResponsesNamespaceToolsForAnthropic(*request.Canonical)
		if err != nil {
			return nil, err
		}
		if a.state != nil {
			a.state.responseTools = responseTools
		}
		payload, err := encodeCanonicalRequestToAnthropicMessages(canonical, candidate)
		if err != nil {
			return nil, err
		}
		return applyRequestPolicyForCandidate(payload, canonicalProtocolAnthropicMessages, candidate), nil
	}
	return convertRequestBetweenProtocols(canonicalProtocolAnthropicMessages, canonicalProtocolAnthropicMessages, request.Payload, stringFromPayloadModel(request.Payload), candidate)
}

func (anthropicMessagesProtocolAdapter) UpstreamPath(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + gatewayEndpointMessages
}

func (a anthropicMessagesProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if statusCode < 200 || statusCode >= 300 {
		return gatewayBufferedResponse{StatusCode: statusCode, ContentType: contentType, Body: body}, nil
	}
	if a.downstreamProtocol != "" && a.downstreamProtocol != canonicalProtocolAnthropicMessages {
		convertedBody, usage, err := convertResponseBetweenProtocols(canonicalProtocolAnthropicMessages, a.downstreamProtocol, body, responseConversionOptions{ResponseTools: a.responseTools()})
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		return gatewayBufferedResponse{StatusCode: statusCode, ContentType: "application/json", Body: convertedBody, Usage: usage}, nil
	}
	return gatewayBufferedResponse{StatusCode: statusCode, ContentType: stringValue(&contentType, "application/json"), Body: body, Usage: parseAnthropicMessageUsage(body)}, nil
}

func (a anthropicMessagesProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	if a.downstreamProtocol != "" && a.downstreamProtocol != canonicalProtocolAnthropicMessages {
		return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolAnthropicMessages, a.downstreamProtocol, canonicalStreamOptions{IncludeUsage: a.includeUsage, Candidate: candidate, ResponseTools: a.responseTools()})
	}
	return proxyUpstreamStreamWithInspector(ctx, w, resp, startedAt, inspectAnthropicMessagesStreamLine)
}

func (a anthropicMessagesProtocolAdapter) responseTools() map[string]responsesToolIdentity {
	if a.state == nil {
		return nil
	}
	return a.state.responseTools
}

func (anthropicMessagesProtocolAdapter) ApplyUpstreamHeaders(req *http.Request, upstreamKey string, _ string, _ bool) {
	req.Header.Del("Authorization")
	req.Header.Set("x-api-key", upstreamKey)
	if strings.TrimSpace(req.Header.Get("anthropic-version")) == "" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
}

type providerAnthropicMessagesProtocolAdapter struct {
	provider           string
	baseURL            string
	basePath           string
	path               string
	downstreamProtocol canonicalProtocol
	includeUsage       bool
	customTools        map[string]struct{}
	responseTools      map[string]responsesToolIdentity
}

func newProviderAnthropicMessagesProtocolAdapter(provider string, alt alternateProtocolDefinition, downstream canonicalProtocol) *providerAnthropicMessagesProtocolAdapter {
	return &providerAnthropicMessagesProtocolAdapter{
		provider:           provider,
		baseURL:            strings.TrimSpace(alt.BaseURL),
		basePath:           strings.TrimSpace(alt.BasePath),
		path:               strings.TrimSpace(alt.Path),
		downstreamProtocol: downstream,
	}
}

func (a providerAnthropicMessagesProtocolAdapter) ProtocolName() string {
	switch a.downstreamProtocol {
	case canonicalProtocolOpenAIChat:
		return a.provider + "_anthropic_messages_to_chat_completions"
	case canonicalProtocolOpenAIResponses:
		return a.provider + "_anthropic_messages_to_responses"
	}
	return a.provider + "_anthropic_messages"
}

func (a *providerAnthropicMessagesProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	a.customTools = customToolNamesFromRequest(request)
	inner := newAnthropicMessagesProtocolAdapter(a.downstreamProtocol)
	payload, err := inner.BuildUpstreamPayload(request, candidate)
	if err != nil {
		return nil, err
	}
	a.responseTools = inner.responseTools()
	hydrateProviderAnthropicThinking(payload, candidate, request.DownstreamPath != gatewayEndpointMessages)
	return applyRequestPolicyForCandidate(payload, canonicalProtocolAnthropicMessages, candidate), nil
}

func (a providerAnthropicMessagesProtocolAdapter) UpstreamPath(siteBaseURL string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(a.baseURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(siteBaseURL), "/")
	}
	basePath := strings.Trim(strings.TrimSpace(a.basePath), "/")
	if basePath != "" {
		baseURL += "/" + basePath
	}
	path := strings.TrimSpace(a.path)
	if path == "" {
		path = gatewayEndpointMessages
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}

func (a providerAnthropicMessagesProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	if statusCode >= 200 && statusCode < 300 {
		rememberProviderAnthropicThinkingFromMessageBody(body)
	}
	if statusCode < 200 || statusCode >= 300 || a.downstreamProtocol == "" || a.downstreamProtocol == canonicalProtocolAnthropicMessages {
		return (anthropicMessagesProtocolAdapter{downstreamProtocol: a.downstreamProtocol}).TransformBufferedResponse(statusCode, headers, body)
	}
	convertedBody, usage, err := convertResponseBetweenProtocols(canonicalProtocolAnthropicMessages, a.downstreamProtocol, body, responseConversionOptions{CustomTools: a.customTools, ResponseTools: a.responseTools})
	if err != nil {
		return gatewayBufferedResponse{}, err
	}
	return gatewayBufferedResponse{StatusCode: statusCode, ContentType: "application/json", Body: convertedBody, Usage: usage}, nil
}

func (a providerAnthropicMessagesProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	if a.downstreamProtocol != "" && a.downstreamProtocol != canonicalProtocolAnthropicMessages {
		inspector := newProviderAnthropicStreamInspector()
		return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolAnthropicMessages, a.downstreamProtocol, canonicalStreamOptions{
			IncludeUsage:        a.includeUsage,
			UpstreamLineInspect: inspector.inspect,
			Candidate:           candidate,
			CustomTools:         a.customTools,
			ResponseTools:       a.responseTools,
		})
	}
	return proxyProviderAnthropicMessagesStream(ctx, w, resp, startedAt)
}

func (a providerAnthropicMessagesProtocolAdapter) ApplyUpstreamHeaders(req *http.Request, upstreamKey string, _ string, _ bool) {
	// Authorization is preserved: some Anthropic-compatible relays authenticate via the downstream bearer.
	req.Header.Set("x-api-key", upstreamKey)
	if strings.TrimSpace(req.Header.Get("anthropic-version")) == "" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
}

func canonicalRequestFromAnthropicMessagesPayload(payload map[string]any, requestedModel string) (canonicalRequest, error) {
	request := canonicalRequest{
		DownstreamPath: gatewayEndpointMessages,
		RequestedModel: strings.TrimSpace(requestedModel),
		SourceProtocol: canonicalProtocolAnthropicMessages,
		Stream:         boolFromMap(payload, "stream"),
		Instructions:   anthropicSystemText(payload["system"]),
		RawSystem:      payload["system"],
		Params:         canonicalParamsFromAnthropicPayload(payload),
		Raw:            clonePayload(payload),
	}
	request.Tools = canonicalToolsFromAnthropicMessages(payload["tools"])
	request.ToolChoice = canonicalToolChoiceFromAnthropic(payload["tool_choice"])

	messages, ok := payload["messages"].([]any)
	if !ok {
		return request, fmt.Errorf("messages must be an array")
	}
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role := strings.TrimSpace(anyString(message["role"]))
		if role == "" {
			continue
		}
		request.Messages = append(request.Messages, canonicalMessagesFromAnthropicContent(role, message["content"])...)
	}
	return request, nil
}

func encodeCanonicalRequestToAnthropicMessages(request canonicalRequest, candidate routeengine.Candidate) (map[string]any, error) {
	maxTokens, err := anthropicMaxTokens(request, candidate)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"model":      candidate.Model.UpstreamName,
		"max_tokens": maxTokens,
		"messages":   encodeCanonicalMessagesAsAnthropicMessages(request.Messages),
	}
	if system := anthropicSystemValue(request); system != nil {
		out["system"] = system
	}
	applyParamMappings(out, request, protocolParamMappings(canonicalProtocolAnthropicMessages, candidate))
	tools := encodeCanonicalToolsAsAnthropic(request.Tools)
	if len(tools) > 0 {
		out["tools"] = tools
		if request.ToolChoice != nil {
			if converted := encodeCanonicalToolChoiceAsAnthropic(request.ToolChoice); converted != nil {
				out["tool_choice"] = converted
			}
		}
	}
	return out, nil
}

func canonicalResponseFromAnthropicMessagesBody(body []byte) (canonicalResponse, error) {
	var root struct {
		ID         string                    `json:"id"`
		Model      string                    `json:"model"`
		Role       string                    `json:"role"`
		Content    []anthropicContentBlock   `json:"content"`
		StopReason string                    `json:"stop_reason"`
		Usage      anthropicMessageUsageBody `json:"usage"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return canonicalResponse{}, err
	}
	if root.ID == "" {
		root.ID = synthesizedResponseID()
	}
	response := canonicalResponse{
		ID:           root.ID,
		Model:        root.Model,
		CreatedAt:    time.Now().Unix(),
		FinishReason: anthropicStopReasonToFinishReason(root.StopReason),
		Usage:        gatewayUsageFromAnthropicUsage(root.Usage),
		Raw:          body,
	}
	for _, block := range root.Content {
		switch block.Type {
		case "thinking":
			if thinking, ok := canonicalThinkingFromAnthropicBlock(map[string]any{
				"type":      block.Type,
				"thinking":  block.Thinking,
				"signature": block.Signature,
			}); ok {
				response.Output = append(response.Output, canonicalOutputItem{
					ID:       "thinking_" + root.ID,
					Type:     "reasoning",
					Status:   "completed",
					Role:     nonEmptyString(root.Role, "assistant"),
					Thinking: []canonicalThinkingBlock{thinking},
				})
			}
		case "text":
			response.Output = append(response.Output, canonicalOutputItem{
				ID:      "msg_" + root.ID,
				Type:    "message",
				Status:  "completed",
				Role:    nonEmptyString(root.Role, "assistant"),
				Text:    block.Text,
				Content: []canonicalContentPart{{Type: "output_text", Text: block.Text}},
			})
		case "tool_use":
			response.Output = append(response.Output, canonicalOutputItem{
				ID:        block.ID,
				Type:      "function_call",
				Status:    "completed",
				CallID:    block.ID,
				Name:      block.Name,
				Arguments: marshalJSONToString(block.Input),
			})
		}
	}
	return response, nil
}

func encodeCanonicalResponseAsAnthropicMessage(response canonicalResponse) ([]byte, gatewayUsage, error) {
	content := make([]any, 0, len(response.Output))
	hasToolUse := false
	for _, item := range response.Output {
		switch item.Type {
		case "reasoning":
			content = append(content, anthropicThinkingBlocksFromCanonical(item.Thinking)...)
		case "function_call":
			hasToolUse = true
			block := map[string]any{
				"type":  "tool_use",
				"id":    nonEmptyString(item.CallID, item.ID),
				"name":  item.Name,
				"input": unmarshalJSONObjectOrEmpty(item.Arguments),
			}
			addToolCallMetadataToAnthropicBlock(block, item.Metadata)
			content = append(content, block)
		case "message":
			content = append(content, anthropicThinkingBlocksFromCanonical(item.Thinking)...)
			text := item.Text
			for _, part := range item.Content {
				if text == "" {
					text += part.Text
				}
			}
			if text != "" {
				content = append(content, map[string]any{"type": "text", "text": text})
			}
		}
	}
	body := map[string]any{
		"id":            responseIDOrFallback(response.ID),
		"type":          "message",
		"role":          "assistant",
		"model":         response.Model,
		"content":       content,
		"stop_reason":   finishReasonToAnthropicStopReason(response.FinishReason, hasToolUse),
		"stop_sequence": nil,
		"usage":         anthropicUsagePayload(response.Usage),
	}
	encoded, err := json.Marshal(body)
	return encoded, response.Usage, err
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

type anthropicCacheCreationUsageBody struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
}

type anthropicMessageUsageBody struct {
	InputTokens              int                              `json:"input_tokens"`
	OutputTokens             int                              `json:"output_tokens"`
	CacheCreationInputTokens int                              `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int                              `json:"cache_read_input_tokens"`
	CacheCreation            *anthropicCacheCreationUsageBody `json:"cache_creation,omitempty"`
}

func canonicalParamsFromAnthropicPayload(payload map[string]any) map[string]any {
	params := map[string]any{}
	for key, value := range payload {
		switch key {
		case "model", "messages", "system", "tools", "tool_choice":
			continue
		case "max_tokens":
			params["max_output_tokens"] = value
		default:
			params[key] = value
		}
	}
	return params
}

func anthropicSystemValue(request canonicalRequest) any {
	if blocks, ok := request.RawSystem.([]any); ok && len(blocks) > 0 {
		return blocks
	}
	if request.Instructions != "" {
		return request.Instructions
	}
	return nil
}

func anthropicSystemText(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			block, _ := item.(map[string]any)
			if strings.TrimSpace(anyString(block["type"])) == "text" {
				if text := strings.TrimSpace(anyString(block["text"])); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func canonicalMessagesFromAnthropicContent(role string, raw any) []canonicalMessage {
	blocks, ok := raw.([]any)
	if !ok {
		text := strings.TrimSpace(anyString(raw))
		if text == "" {
			return []canonicalMessage{{Type: "message", Role: role, RawContent: raw}}
		}
		return []canonicalMessage{{Type: "message", Role: role, RawContent: raw, Content: []canonicalContentPart{{Type: contentPartTypeForRole(role), Text: text}}}}
	}

	result := make([]canonicalMessage, 0, len(blocks))
	message := canonicalMessage{Type: "message", Role: role, RawContent: raw}
	for _, item := range blocks {
		block, _ := item.(map[string]any)
		cacheControl := block["cache_control"]
		switch strings.TrimSpace(anyString(block["type"])) {
		case "text":
			message.Content = append(message.Content, canonicalContentPart{Type: contentPartTypeForRole(role), Text: anyString(block["text"]), CacheControl: cacheControl, Raw: block})
		case "thinking":
			if thinking, ok := canonicalThinkingFromAnthropicBlock(block); ok {
				message.Thinking = append(message.Thinking, thinking)
			}
		case "image":
			message.Content = append(message.Content, canonicalContentPart{Type: "input_image", ImageURL: anthropicImageURL(block["source"]), CacheControl: cacheControl, Raw: block})
		case "document":
			source, _ := block["source"].(map[string]any)
			fileData := strings.TrimSpace(anyString(source["data"]))
			mimeType := strings.TrimSpace(anyString(source["media_type"]))
			if fileData != "" && strings.TrimSpace(anyString(source["type"])) == "base64" {
				fileData = "data:" + nonEmptyString(mimeType, "application/octet-stream") + ";base64," + fileData
			}
			if fileData == "" {
				fileData = strings.TrimSpace(anyString(source["url"]))
			}
			if fileData != "" {
				message.Content = append(message.Content, canonicalContentPart{
					Type:         "input_file",
					FileName:     strings.TrimSpace(anyString(block["title"])),
					FileData:     fileData,
					MimeType:     mimeType,
					CacheControl: cacheControl,
					Raw:          block,
				})
			}
		case "tool_use":
			input := marshalAnyToString(block["input"])
			message.ToolCalls = append(message.ToolCalls, canonicalToolCall{
				ID:        strings.TrimSpace(anyString(block["id"])),
				Type:      "function",
				Name:      strings.TrimSpace(anyString(block["name"])),
				Arguments: input,
				Metadata:  canonicalToolCallMetadata(block),
			})
		case "tool_result":
			if len(message.Content) > 0 || len(message.ToolCalls) > 0 {
				result = append(result, message)
				message = canonicalMessage{Type: "message", Role: role, RawContent: raw}
			}
			result = append(result, canonicalMessage{
				Type:       "function_call_output",
				Role:       role,
				ToolCallID: strings.TrimSpace(anyString(block["tool_use_id"])),
				Output:     normalizeToolOutput(block["content"]),
				RawContent: block["content"],
			})
		}
	}
	if len(message.Content) > 0 || len(message.ToolCalls) > 0 {
		result = append(result, message)
	}
	return result
}

func canonicalToolsFromAnthropicMessages(raw any) []canonicalTool {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	tools := make([]canonicalTool, 0, len(items))
	for _, item := range items {
		tool, _ := item.(map[string]any)
		name := strings.TrimSpace(anyString(tool["name"]))
		if name == "" {
			continue
		}
		tools = append(tools, canonicalTool{
			Type:        "function",
			Name:        name,
			Description: anyString(tool["description"]),
			Parameters:  tool["input_schema"],
			Raw:         tool,
		})
	}
	return tools
}

func canonicalToolChoiceFromAnthropic(raw any) any {
	choice, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	switch strings.TrimSpace(anyString(choice["type"])) {
	case "tool":
		return map[string]any{"type": "function", "name": choice["name"]}
	default:
		return raw
	}
}

func encodeCanonicalMessagesAsAnthropicMessages(messages []canonicalMessage) []any {
	out := make([]any, 0, len(messages))
	for i := 0; i < len(messages); {
		message := messages[i]
		switch message.Type {
		case "function_call":
			next, toolUseBlocks, toolResultBlocks := encodeConsecutiveCanonicalToolRoundTrip(messages, i)
			if len(toolUseBlocks) > 0 {
				out = append(out,
					map[string]any{"role": "assistant", "content": toolUseBlocks},
					map[string]any{"role": "user", "content": toolResultBlocks},
				)
			}
			i = next
		case "function_call_output":
			i++
		default:
			role := strings.TrimSpace(message.Role)
			if role == "" || role == "developer" || role == "system" {
				role = "user"
			}
			blocks := encodeCanonicalContentAsAnthropic(message.Content)
			if len(message.ToolCalls) > 0 {
				if len(message.Content) == 0 {
					blocks = nil
				}
				next, toolUseBlocks, toolResultBlocks := encodeCanonicalMessageToolCallRoundTrip(messages, i)
				if len(toolUseBlocks) > 0 {
					blocks = append(anthropicThinkingBlocksFromCanonical(message.Thinking), blocks...)
					blocks = append(blocks, toolUseBlocks...)
					out = append(out,
						map[string]any{"role": role, "content": blocks},
						map[string]any{"role": "user", "content": toolResultBlocks},
					)
					i = next
					continue
				}
				if !anthropicContentHasMeaningfulText(blocks) {
					i = next
					continue
				}
			}
			if role == "assistant" {
				blocks = append(anthropicThinkingBlocksFromCanonical(message.Thinking), blocks...)
			}
			out = append(out, map[string]any{"role": role, "content": blocks})
			i++
		}
	}
	return out
}

func encodeConsecutiveCanonicalToolRoundTrip(messages []canonicalMessage, start int) (int, []any, []any) {
	calls := make([]canonicalMessage, 0, 1)
	i := start
	for i < len(messages) && messages[i].Type == "function_call" {
		calls = append(calls, messages[i])
		i++
	}

	outputs := make([]canonicalMessage, 0, 1)
	for i < len(messages) && messages[i].Type == "function_call_output" {
		outputs = append(outputs, messages[i])
		i++
	}
	if len(calls) == 0 || len(outputs) == 0 {
		return i, nil, nil
	}

	outputByID := make(map[string]canonicalMessage, len(outputs))
	for _, output := range outputs {
		callID := strings.TrimSpace(output.ToolCallID)
		if callID == "" {
			continue
		}
		if _, exists := outputByID[callID]; !exists {
			outputByID[callID] = output
		}
	}

	toolUseBlocks := make([]any, 0, len(calls))
	toolResultBlocks := make([]any, 0, len(calls))
	for _, call := range calls {
		callID := strings.TrimSpace(nonEmptyString(call.ToolCallID, call.ID))
		if callID == "" {
			continue
		}
		output, ok := outputByID[callID]
		if !ok {
			continue
		}
		block := map[string]any{
			"type":  "tool_use",
			"id":    callID,
			"name":  call.Name,
			"input": unmarshalJSONObjectOrEmpty(call.Arguments),
		}
		addToolCallMetadataToAnthropicBlock(block, call.Metadata)
		toolUseBlocks = append(toolUseBlocks, block)
		toolResultBlocks = append(toolResultBlocks, map[string]any{
			"type":        "tool_result",
			"tool_use_id": callID,
			"content":     anthropicToolResultContent(output),
		})
	}
	if len(toolUseBlocks) == 0 {
		return i, nil, nil
	}
	return i, toolUseBlocks, toolResultBlocks
}

func encodeCanonicalMessageToolCallRoundTrip(messages []canonicalMessage, start int) (int, []any, []any) {
	if start < 0 || start >= len(messages) {
		return start, nil, nil
	}
	message := messages[start]
	i := start + 1
	outputs := make([]canonicalMessage, 0, 1)
	for i < len(messages) && messages[i].Type == "function_call_output" {
		outputs = append(outputs, messages[i])
		i++
	}
	if len(message.ToolCalls) == 0 || len(outputs) == 0 {
		return i, nil, nil
	}

	outputByID := make(map[string]canonicalMessage, len(outputs))
	for _, output := range outputs {
		callID := strings.TrimSpace(output.ToolCallID)
		if callID == "" {
			continue
		}
		if _, exists := outputByID[callID]; !exists {
			outputByID[callID] = output
		}
	}

	toolUseBlocks := make([]any, 0, len(message.ToolCalls))
	toolResultBlocks := make([]any, 0, len(message.ToolCalls))
	for _, toolCall := range message.ToolCalls {
		callID := strings.TrimSpace(toolCall.ID)
		if callID == "" {
			continue
		}
		output, ok := outputByID[callID]
		if !ok {
			continue
		}
		block := map[string]any{
			"type":  "tool_use",
			"id":    callID,
			"name":  toolCall.Name,
			"input": unmarshalJSONObjectOrEmpty(toolCall.Arguments),
		}
		addToolCallMetadataToAnthropicBlock(block, toolCall.Metadata)
		toolUseBlocks = append(toolUseBlocks, block)
		toolResultBlocks = append(toolResultBlocks, map[string]any{
			"type":        "tool_result",
			"tool_use_id": callID,
			"content":     anthropicToolResultContent(output),
		})
	}
	if len(toolUseBlocks) == 0 {
		return i, nil, nil
	}
	return i, toolUseBlocks, toolResultBlocks
}

func anthropicToolResultContent(output canonicalMessage) any {
	if blocks, ok := output.RawContent.([]any); ok && anthropicToolResultBlocksValid(blocks) {
		return blocks
	}
	return normalizeToolOutput(output.Output)
}

func anthropicToolResultBlocksValid(blocks []any) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			return false
		}
		switch strings.TrimSpace(anyString(block["type"])) {
		case "text", "image":
		default:
			return false
		}
	}
	return true
}

func addToolCallMetadataToAnthropicBlock(block map[string]any, metadata map[string]any) {
	signature := firstNonEmptyGatewayString(
		rawStringFromAny(metadata["thought_signature"]),
		rawStringFromAny(metadata["thoughtSignature"]),
	)
	if signature != "" {
		block["thought_signature"] = signature
	}
}

func anthropicContentHasMeaningfulText(blocks []any) bool {
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		if strings.TrimSpace(anyString(block["type"])) != "text" {
			continue
		}
		if strings.TrimSpace(anyString(block["text"])) != "" {
			return true
		}
	}
	return false
}

func encodeCanonicalContentAsAnthropic(parts []canonicalContentPart) []any {
	if len(parts) == 0 {
		return []any{map[string]any{"type": "text", "text": ""}}
	}
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "input_image":
			block := map[string]any{"type": "image", "source": anthropicImageSource(part.ImageURL)}
			if part.CacheControl != nil {
				block["cache_control"] = part.CacheControl
			}
			out = append(out, block)
		case "input_file":
			block := map[string]any{
				"type":   "document",
				"source": anthropicDocumentSource(part.FileData, part.MimeType),
			}
			if part.FileName != "" {
				block["title"] = part.FileName
			}
			if part.CacheControl != nil {
				block["cache_control"] = part.CacheControl
			}
			out = append(out, block)
		default:
			block := map[string]any{"type": "text", "text": part.Text}
			if part.CacheControl != nil {
				block["cache_control"] = part.CacheControl
			}
			out = append(out, block)
		}
	}
	return out
}

func encodeCanonicalToolsAsAnthropic(tools []canonicalTool) []any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		if tool.Type == "custom" {
			out = append(out, map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"input": map[string]any{"type": "string"},
					},
					"required":             []any{"input"},
					"additionalProperties": false,
				},
			})
			continue
		}
		if tool.Type != "function" {
			continue
		}
		out = append(out, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": anthropicToolInputSchema(tool.Parameters),
		})
	}
	return out
}

func prepareResponsesNamespaceToolsForAnthropic(request canonicalRequest) (canonicalRequest, map[string]responsesToolIdentity, error) {
	if request.SourceProtocol != canonicalProtocolOpenAIResponses {
		return request, nil, nil
	}
	aliases := map[string]string{}
	aliasesByName := map[string][]string{}
	identities := map[string]responsesToolIdentity{}
	usedNames := map[string]struct{}{}
	topLevelNames := map[string]struct{}{}
	for _, tool := range request.Tools {
		if tool.Type != "namespace" && strings.TrimSpace(tool.Name) != "" {
			usedNames[tool.Name] = struct{}{}
			topLevelNames[tool.Name] = struct{}{}
		}
	}
	tools := make([]canonicalTool, 0, len(request.Tools))
	for _, tool := range request.Tools {
		if tool.Type != "namespace" {
			tools = append(tools, tool)
			continue
		}
		appendResponsesNamespaceToolsForAnthropic(&tools, tool, "", "", aliases, identities, usedNames)
	}
	request.Tools = tools
	for alias, identity := range identities {
		aliasesByName[identity.Name] = append(aliasesByName[identity.Name], alias)
	}
	toolChoice, err := aliasResponsesNamespaceToolChoice(request.ToolChoice, aliases, aliasesByName, topLevelNames)
	if err != nil {
		return canonicalRequest{}, nil, err
	}
	request.ToolChoice = toolChoice
	request.Messages = aliasResponsesNamespaceToolHistory(request.Messages, aliases)
	if len(identities) == 0 {
		return request, nil, nil
	}
	return request, identities, nil
}

func appendResponsesNamespaceToolsForAnthropic(out *[]canonicalTool, tool canonicalTool, parent string, parentDescription string, aliases map[string]string, identities map[string]responsesToolIdentity, usedNames map[string]struct{}) {
	namespace := strings.TrimSpace(tool.Name)
	if parent != "" && namespace != "" {
		namespace = parent + "." + namespace
	} else if namespace == "" {
		namespace = parent
	}
	description := strings.TrimSpace(strings.Join([]string{parentDescription, tool.Description}, " "))
	nested := canonicalToolsFromOpenAIResponses(tool.Raw["tools"])
	for _, child := range nested {
		if child.Type == "namespace" {
			appendResponsesNamespaceToolsForAnthropic(out, child, namespace, description, aliases, identities, usedNames)
			continue
		}
		if child.Type != "function" || namespace == "" || strings.TrimSpace(child.Name) == "" {
			continue
		}
		alias := anthropicNamespaceToolAlias(namespace, child.Name, usedNames)
		key := responsesNamespaceToolKey(namespace, child.Name)
		aliases[key] = alias
		identities[alias] = responsesToolIdentity{Name: child.Name, Namespace: namespace}
		usedNames[alias] = struct{}{}
		child.Name = alias
		child.Description = strings.TrimSpace(strings.Join([]string{fmt.Sprintf("Namespace %s.", namespace), description, child.Description}, " "))
		*out = append(*out, child)
	}
}

func anthropicNamespaceToolAlias(namespace string, name string, usedNames map[string]struct{}) string {
	key := responsesNamespaceToolKey(namespace, name)
	for attempt := 0; ; attempt++ {
		material := key
		if attempt > 0 {
			material = fmt.Sprintf("%s\x00%d", key, attempt)
		}
		sum := sha256.Sum256([]byte(material))
		alias := fmt.Sprintf("xlyra_ns_%x", sum[:16])
		if _, exists := usedNames[alias]; !exists {
			return alias
		}
	}
}

func responsesNamespaceToolKey(namespace string, name string) string {
	return strings.TrimSpace(namespace) + "\x00" + strings.TrimSpace(name)
}

func aliasResponsesNamespaceToolChoice(raw any, aliases map[string]string, aliasesByName map[string][]string, topLevelNames map[string]struct{}) (any, error) {
	choice, ok := raw.(map[string]any)
	if !ok {
		return raw, nil
	}
	namespace := strings.TrimSpace(anyString(choice["namespace"]))
	name := canonicalToolChoiceFunctionName(choice)
	if name == "" {
		return raw, nil
	}
	if namespace != "" {
		alias, exists := aliases[responsesNamespaceToolKey(namespace, name)]
		if !exists {
			return nil, fmt.Errorf("namespace tool choice %s.%s is not available", namespace, name)
		}
		return map[string]any{"type": "function", "name": alias}, nil
	}
	if _, topLevel := topLevelNames[name]; topLevel {
		return raw, nil
	}
	matches := aliasesByName[name]
	switch len(matches) {
	case 0:
		return raw, nil
	case 1:
		return map[string]any{"type": "function", "name": matches[0]}, nil
	default:
		return nil, fmt.Errorf("namespace tool choice %q is ambiguous across %d namespaces", name, len(matches))
	}
}

func aliasResponsesNamespaceToolHistory(messages []canonicalMessage, aliases map[string]string) []canonicalMessage {
	if len(aliases) == 0 || len(messages) == 0 {
		return messages
	}
	out := append([]canonicalMessage(nil), messages...)
	for i := range out {
		if out[i].Type == "function_call" {
			if alias, ok := aliases[responsesNamespaceToolKey(anyString(out[i].Metadata["namespace"]), out[i].Name)]; ok {
				out[i].Name = alias
			}
		}
		if len(out[i].ToolCalls) == 0 {
			continue
		}
		out[i].ToolCalls = append([]canonicalToolCall(nil), out[i].ToolCalls...)
		for j := range out[i].ToolCalls {
			call := &out[i].ToolCalls[j]
			if alias, ok := aliases[responsesNamespaceToolKey(anyString(call.Metadata["namespace"]), call.Name)]; ok {
				call.Name = alias
			}
		}
	}
	return out
}

// anthropicToolInputSchema guarantees a JSON Schema object; Anthropic rejects a null input_schema.
func anthropicToolInputSchema(parameters any) any {
	if schema, ok := parameters.(map[string]any); ok && len(schema) > 0 {
		return schema
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func encodeCanonicalToolChoiceAsAnthropic(raw any) any {
	switch value := raw.(type) {
	case string:
		switch value {
		case "auto", "none":
			return map[string]any{"type": value}
		case "required":
			return map[string]any{"type": "any"}
		}
	case map[string]any:
		if toolType := strings.TrimSpace(anyString(value["type"])); toolType == "function" || toolType == "custom" {
			if name := canonicalToolChoiceFunctionName(value); name != "" {
				return map[string]any{"type": "tool", "name": name}
			}
		}
	}
	return nil
}

func customToolNamesFromRequest(request gatewayRequest) map[string]struct{} {
	if request.Canonical == nil {
		return nil
	}
	names := map[string]struct{}{}
	for _, tool := range request.Canonical.Tools {
		if tool.Type != "custom" {
			continue
		}
		if name := strings.TrimSpace(tool.Name); name != "" {
			names[name] = struct{}{}
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func anthropicMaxTokens(request canonicalRequest, candidate routeengine.Candidate) (int, error) {
	for _, key := range []string{"max_output_tokens", "max_tokens", "max_completion_tokens"} {
		if value, ok := intFromAny(request.Params[key]); ok && value > 0 {
			return value, nil
		}
	}
	spec := effectiveProtocolSpec(canonicalProtocolAnthropicMessages, candidate)
	if value, ok := intFromAny(spec.RequestParams.Defaults["max_tokens"]); ok && value > 0 {
		return value, nil
	}
	return defaultAnthropicMaxTokens, nil
}

func anthropicImageURL(raw any) string {
	source, _ := raw.(map[string]any)
	if strings.TrimSpace(anyString(source["type"])) == "base64" {
		mediaType := strings.TrimSpace(anyString(source["media_type"]))
		if mediaType == "" {
			mediaType = "image/png"
		}
		return "data:" + mediaType + ";base64," + strings.TrimSpace(anyString(source["data"]))
	}
	return strings.TrimSpace(anyString(source["url"]))
}

func anthropicImageSource(url string) map[string]any {
	if strings.HasPrefix(url, "data:") {
		parts := strings.SplitN(strings.TrimPrefix(url, "data:"), ";base64,", 2)
		if len(parts) == 2 {
			return map[string]any{"type": "base64", "media_type": parts[0], "data": parts[1]}
		}
	}
	return map[string]any{"type": "url", "url": url}
}

func anthropicDocumentSource(data string, mimeType string) map[string]any {
	if strings.HasPrefix(data, "data:") {
		parts := strings.SplitN(strings.TrimPrefix(data, "data:"), ";base64,", 2)
		if len(parts) == 2 {
			if strings.TrimSpace(parts[0]) == "" {
				parts[0] = mimeType
			}
			return map[string]any{"type": "base64", "media_type": parts[0], "data": parts[1]}
		}
	}
	if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") {
		return map[string]any{"type": "url", "url": data}
	}
	return map[string]any{"type": "base64", "media_type": nonEmptyString(mimeType, "application/octet-stream"), "data": data}
}

func parseAnthropicMessageUsage(body []byte) gatewayUsage {
	var root struct {
		Usage anthropicMessageUsageBody `json:"usage"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return gatewayUsage{}
	}
	return gatewayUsageFromAnthropicUsage(root.Usage)
}

func gatewayUsageFromAnthropicUsage(usage anthropicMessageUsageBody) gatewayUsage {
	cacheCreation5mTokens := usage.CacheCreationInputTokens
	cacheCreation1hTokens := 0
	if usage.CacheCreation != nil {
		cacheCreation5mTokens = usage.CacheCreation.Ephemeral5mInputTokens
		cacheCreation1hTokens = usage.CacheCreation.Ephemeral1hInputTokens
	}
	cacheCreationTokens := cacheCreation5mTokens + cacheCreation1hTokens
	if cacheCreationTokens == 0 {
		cacheCreationTokens = usage.CacheCreationInputTokens
	}
	return gatewayUsage{
		PromptTokens:               usage.InputTokens + usage.CacheReadInputTokens + cacheCreationTokens,
		CompletionTokens:           usage.OutputTokens,
		TotalTokens:                usage.InputTokens + usage.CacheReadInputTokens + cacheCreationTokens + usage.OutputTokens,
		CachedPromptTokens:         usage.CacheReadInputTokens,
		CacheCreationInputTokens:   cacheCreationTokens,
		CacheCreation5mInputTokens: cacheCreation5mTokens,
		CacheCreation1hInputTokens: cacheCreation1hTokens,
	}
}

func anthropicUsagePayload(usage gatewayUsage) map[string]any {
	usage = usage.normalized()
	payload := map[string]any{
		"input_tokens":                usage.PromptTokens,
		"output_tokens":               usage.CompletionTokens,
		"cache_creation_input_tokens": usage.CacheCreationInputTokens,
		"cache_read_input_tokens":     usage.CachedPromptTokens,
	}
	if usage.hasCacheCreationBreakdown() {
		payload["cache_creation"] = map[string]any{
			"ephemeral_5m_input_tokens": usage.CacheCreation5mInputTokens,
			"ephemeral_1h_input_tokens": usage.CacheCreation1hInputTokens,
		}
	}
	return payload
}

func inspectAnthropicMessagesStreamLine(line []byte, capture *streamCaptureState) {
	if capture == nil {
		return
	}
	data, _, ok := sseDataFromLine(line)
	if !ok {
		return
	}
	var event struct {
		Type    string                    `json:"type"`
		Message map[string]any            `json:"message"`
		Usage   anthropicMessageUsageBody `json:"usage"`
		Error   map[string]any            `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}
	switch event.Type {
	case "message_start":
		if messageUsage, ok := event.Message["usage"]; ok {
			usageBody := anthropicMessageUsageBody{}
			if encoded, err := json.Marshal(messageUsage); err == nil && json.Unmarshal(encoded, &usageBody) == nil {
				capture.usage = completionUsageFromGatewayUsage(gatewayUsageFromAnthropicUsage(usageBody))
			}
		}
	case "message_delta":
		if event.Usage.OutputTokens > 0 || event.Usage.InputTokens > 0 || event.Usage.CacheReadInputTokens > 0 || event.Usage.CacheCreationInputTokens > 0 {
			current := capture.usage
			deltaUsage := gatewayUsageFromAnthropicUsage(event.Usage)
			if deltaUsage.PromptTokens > 0 || deltaUsage.CachedPromptTokens > 0 || deltaUsage.CacheCreationInputTokens > 0 {
				current.PromptTokens = deltaUsage.PromptTokens
				current.CachedPromptTokens = deltaUsage.CachedPromptTokens
				current.CacheCreationInputTokens = deltaUsage.CacheCreationInputTokens
			}
			if deltaUsage.CompletionTokens > 0 {
				current.CompletionTokens = deltaUsage.CompletionTokens
			}
			current.TotalTokens = 0
			capture.usage = current.normalized()
		}
	case "message_stop":
		capture.sawDone = true
		capture.streamCompleted = true
		capture.endReason = "done"
	case "error":
		capture.endReason = "upstream_stream_error"
	}
}

func anthropicStopReasonToFinishReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "stop_sequence", "end_turn":
		return "stop"
	default:
		return strings.TrimSpace(reason)
	}
}

func finishReasonToAnthropicStopReason(reason string, hasToolUse bool) string {
	if hasToolUse {
		return "tool_use"
	}
	switch strings.TrimSpace(reason) {
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	case "":
		return "end_turn"
	default:
		return strings.TrimSpace(reason)
	}
}

func marshalAnyToString(value any) string {
	if value == nil {
		return "{}"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func marshalJSONToString(value json.RawMessage) string {
	if len(value) == 0 {
		return "{}"
	}
	if json.Valid(value) {
		return string(value)
	}
	return "{}"
}

func unmarshalJSONObjectOrEmpty(text string) any {
	text = strings.TrimSpace(text)
	if text == "" {
		return map[string]any{}
	}
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return map[string]any{}
	}
	return value
}
