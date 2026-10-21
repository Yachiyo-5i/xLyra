package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	routeengine "xlyra/server/internal/router"
)

type openAIChatProtocolAdapter struct {
	downstreamProtocol canonicalProtocol
	baseURL            string
	basePath           string
	path               string
	mimoTTS            bool
	mimoTTSFormat      string
	// aggregateChatSSE folds an upstream chat SSE body into one JSON response
	// when a plugin site says the upstream only speaks chat SSE, and only when
	// the downstream request did not ask for a stream.
	aggregateChatSSE bool
}

func (a openAIChatProtocolAdapter) ProtocolName() string {
	switch a.downstreamProtocol {
	case canonicalProtocolOpenAIResponses:
		return "openai_chat_completions_to_responses"
	case canonicalProtocolAnthropicMessages:
		return "openai_chat_completions_to_messages"
	case canonicalProtocolGoogleGemini:
		return "openai_chat_completions_to_gemini"
	}
	return "openai_chat_completions"
}

func (openAIChatProtocolAdapter) CredentialEndpointTypes() []string {
	return textCredentialEndpointTypes()
}

func (a openAIChatProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if a.mimoTTS && request.Stream {
		audio, _ := request.Payload["audio"].(map[string]any)
		if !strings.EqualFold(strings.TrimSpace(stringFromMapAny(audio, "format")), "pcm16") {
			return nil, fmt.Errorf("mimo v2.5 TTS streaming requests require audio.format=pcm16")
		}
	}
	if request.Canonical != nil && request.DownstreamPath != gatewayEndpointChatCompletions {
		if err := validateGoogleGeminiConversion(*request.Canonical, canonicalProtocolOpenAIChat); err != nil {
			return nil, err
		}
		return a.applyUpstreamPolicy(encodeCanonicalRequestToOpenAIChat(*request.Canonical, candidate), candidate), nil
	}
	if a.downstreamProtocol == canonicalProtocolOpenAIResponses || request.DownstreamPath == gatewayEndpointResponses {
		if request.Canonical != nil {
			return a.applyUpstreamPolicy(encodeCanonicalRequestToOpenAIChat(*request.Canonical, candidate), candidate), nil
		}
		payload, err := convertRequestBetweenProtocols(canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, request.Payload, stringFromPayloadModel(request.Payload), candidate)
		if err != nil {
			return nil, err
		}
		return a.applyUpstreamPolicy(payload, candidate), nil
	}
	payload := clonePayload(request.Payload)
	payload["model"] = candidate.Model.UpstreamName
	return a.applyUpstreamPolicy(payload, candidate), nil
}

func newOpenAIChatProtocolAdapter(request gatewayRequest, candidate routeengine.Candidate) openAIChatProtocolAdapter {
	spec := effectiveProtocolSpec(canonicalProtocolOpenAIChat, candidate)
	audio, _ := request.Payload["audio"].(map[string]any)
	return openAIChatProtocolAdapter{
		downstreamProtocol: downstreamCanonicalProtocol(request.DownstreamPath),
		baseURL:            spec.OfficialBaseURL,
		basePath:           spec.BasePath,
		path:               spec.Path,
		mimoTTS:            isMiMoV25TTSModel(candidate.Model.UpstreamName),
		mimoTTSFormat:      strings.ToLower(stringFromMapAny(audio, "format")),
		aggregateChatSSE:   pluginSiteUpstreamStreamOnly(candidate.Site.SiteType) && !request.Stream,
	}
}

func (a openAIChatProtocolAdapter) applyUpstreamPolicy(payload map[string]any, candidate routeengine.Candidate) map[string]any {
	if a.mimoTTS {
		payload["stream"] = true
		audio, _ := payload["audio"].(map[string]any)
		audio = clonePayload(audio)
		audio["format"] = "pcm16"
		payload["audio"] = audio
	}
	payload = applyChatUpstreamPolicy(payload, candidate)
	if a.mimoTTS {
		delete(payload, "stream_options")
		delete(payload, "max_tokens")
		delete(payload, "max_completion_tokens")
	}
	return payload
}

func (a openAIChatProtocolAdapter) UpstreamPath(baseURL string) string {
	return upstreamPathFromSpec(baseURL, resolvedProtocolSpec{
		OfficialBaseURL: a.baseURL,
		BasePath:        a.basePath,
		Path:            a.path,
	}, gatewayEndpointChatCompletions)
}

func (a openAIChatProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if a.mimoTTS && statusCode >= 200 && statusCode < 300 && (isEventStreamContentType(contentType) || looksLikeSSEBody(body)) {
		convertedBody, usage, err := bufferMiMoTTSChatStream(body, a.mimoTTSFormat)
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: "application/json",
			Body:        convertedBody,
			Usage:       usage,
		}, nil
	}
	if a.aggregateChatSSE && statusCode >= 200 && statusCode < 300 && (isEventStreamContentType(contentType) || looksLikeSSEBody(body)) {
		aggregated, err := aggregateChatCompletionSSE(body)
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		body = aggregated
		contentType = "application/json"
	}
	if a.downstreamProtocol != "" && a.downstreamProtocol != canonicalProtocolOpenAIChat && statusCode >= 200 && statusCode < 300 {
		convertedBody, usage, err := convertResponseBetweenProtocols(canonicalProtocolOpenAIChat, a.downstreamProtocol, body, responseConversionOptions{RequireUsage: true})
		if err != nil {
			return gatewayBufferedResponse{}, err
		}
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: "application/json",
			Body:        convertedBody,
			Usage:       completionUsageFromGatewayUsage(usage),
		}, nil
	}
	return gatewayBufferedResponse{
		StatusCode:  statusCode,
		ContentType: contentType,
		Body:        body,
		Usage:       parseCompletionUsage(body),
	}, nil
}

func (a openAIChatProtocolAdapter) usesRawBufferedResponse() bool {
	return a.mimoTTS || a.aggregateChatSSE
}

func (a openAIChatProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	switch a.downstreamProtocol {
	case canonicalProtocolOpenAIResponses:
		return proxyChatCompletionsStreamAsResponses(ctx, w, resp, startedAt)
	case canonicalProtocolAnthropicMessages:
		return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolOpenAIChat, canonicalProtocolAnthropicMessages, canonicalStreamOptions{IncludeUsage: true, RequireUsage: true, Candidate: candidate})
	case canonicalProtocolGoogleGemini:
		return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolOpenAIChat, canonicalProtocolGoogleGemini, canonicalStreamOptions{IncludeUsage: true, RequireUsage: true, Candidate: candidate})
	}
	return proxyUpstreamStream(ctx, w, resp, startedAt)
}

type openAIChatUpstreamResolver struct{}

func (openAIChatUpstreamResolver) Resolve(_ context.Context, request gatewayRequest, candidate routeengine.Candidate) (gatewayProtocolAdapter, error) {
	return newOpenAIChatProtocolAdapter(request, candidate), nil
}

// applyChatUpstreamPolicy applies the request policy and injects stream_options
// with include_usage=true for streaming requests so that upstream providers return
// token usage data in SSE chunks.
func applyChatUpstreamPolicy(payload map[string]any, candidate routeengine.Candidate) map[string]any {
	payload = applyRequestPolicyForCandidate(payload, canonicalProtocolOpenAIChat, candidate)
	if boolFromMap(payload, "stream") {
		if _, exists := payload["stream_options"]; !exists {
			payload["stream_options"] = map[string]any{
				"include_usage": true,
			}
		}
	}
	return payload
}

// chatSSEChunk is one OpenAI chat completion SSE event.
type chatSSEChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Created int64  `json:"created"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage json.RawMessage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
		Type    string `json:"type"`
	} `json:"error"`
}

type chatSSEToolCall struct {
	id        string
	typ       string
	name      strings.Builder
	arguments strings.Builder
}

type chatSSEChoice struct {
	role         string
	content      strings.Builder
	reasoning    strings.Builder
	toolCalls    map[int]*chatSSEToolCall
	finishReason string
}

// aggregateChatCompletionSSE folds an OpenAI chat completion SSE stream into a
// single chat.completion JSON document. Plugin sites that only speak chat SSE
// use it when the downstream request did not ask for a stream.
func aggregateChatCompletionSSE(body []byte) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 32<<20)

	var (
		id, model string
		created   int64
		usage     json.RawMessage
		choices   = map[int]*chatSSEChoice{}
		sawChunk  bool
	)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk chatSSEChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("decode upstream chat chunk: %w", err)
		}
		if chunk.Error != nil {
			message := strings.TrimSpace(chunk.Error.Message)
			if message == "" {
				message = "upstream returned an error event"
			}
			return nil, fmt.Errorf("upstream stream error: %s", message)
		}
		sawChunk = true
		if chunk.ID != "" {
			id = chunk.ID
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Created > 0 {
			created = chunk.Created
		}
		if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			acc := choices[choice.Index]
			if acc == nil {
				acc = &chatSSEChoice{toolCalls: map[int]*chatSSEToolCall{}}
				choices[choice.Index] = acc
			}
			if choice.Delta.Role != "" {
				acc.role = choice.Delta.Role
			}
			acc.content.WriteString(choice.Delta.Content)
			acc.reasoning.WriteString(choice.Delta.ReasoningContent)
			for _, call := range choice.Delta.ToolCalls {
				entry := acc.toolCalls[call.Index]
				if entry == nil {
					entry = &chatSSEToolCall{}
					acc.toolCalls[call.Index] = entry
				}
				if call.ID != "" {
					entry.id = call.ID
				}
				if call.Type != "" {
					entry.typ = call.Type
				}
				entry.name.WriteString(call.Function.Name)
				entry.arguments.WriteString(call.Function.Arguments)
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				acc.finishReason = *choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read upstream chat stream: %w", err)
	}
	if !sawChunk {
		return nil, fmt.Errorf("upstream stream ended before any chat chunk was received")
	}

	indexes := make([]int, 0, len(choices))
	for index := range choices {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	outChoices := make([]map[string]any, 0, len(indexes))
	for _, index := range indexes {
		acc := choices[index]
		role := acc.role
		if role == "" {
			role = "assistant"
		}
		message := map[string]any{"role": role}
		if acc.content.Len() > 0 || len(acc.toolCalls) == 0 {
			message["content"] = acc.content.String()
		} else {
			message["content"] = nil
		}
		if acc.reasoning.Len() > 0 {
			message["reasoning_content"] = acc.reasoning.String()
		}
		if len(acc.toolCalls) > 0 {
			callIndexes := make([]int, 0, len(acc.toolCalls))
			for callIndex := range acc.toolCalls {
				callIndexes = append(callIndexes, callIndex)
			}
			sort.Ints(callIndexes)
			calls := make([]map[string]any, 0, len(callIndexes))
			for _, callIndex := range callIndexes {
				entry := acc.toolCalls[callIndex]
				callType := entry.typ
				if callType == "" {
					callType = "function"
				}
				calls = append(calls, map[string]any{
					"id":   entry.id,
					"type": callType,
					"function": map[string]any{
						"name":      entry.name.String(),
						"arguments": entry.arguments.String(),
					},
				})
			}
			message["tool_calls"] = calls
		}
		finish := acc.finishReason
		if finish == "" {
			finish = "stop"
		}
		outChoices = append(outChoices, map[string]any{
			"index":         index,
			"message":       message,
			"finish_reason": finish,
		})
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	result := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": outChoices,
	}
	if len(usage) > 0 {
		result["usage"] = usage
	}
	return json.Marshal(result)
}
