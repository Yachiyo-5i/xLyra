package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	routeengine "xlyra/server/internal/router"
)

const gatewayEndpointResponses = "/v1/responses"

type openAIResponsesProtocolAdapter struct {
	includeUsage       bool
	downstreamProtocol canonicalProtocol
}

type responsesUsage struct {
	PromptTokens         int                         `json:"prompt_tokens"`
	CompletionTokens     int                         `json:"completion_tokens"`
	TotalTokens          int                         `json:"total_tokens"`
	InputTokens          int                         `json:"input_tokens"`
	OutputTokens         int                         `json:"output_tokens"`
	InputTokensDetails   *responsesInputTokenDetail  `json:"input_tokens_details"`
	OutputTokensDetails  *responsesOutputTokenDetail `json:"output_tokens_details"`
	CompletionTokenStats responsesOutputTokenDetail  `json:"completion_tokens_details"`
}

type responsesInputTokenDetail struct {
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
	ImageTokens      int `json:"image_tokens"`
	AudioTokens      int `json:"audio_tokens"`
}

type responsesOutputTokenDetail struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type responsesOutputContent struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations,omitempty"`
}

type responsesOutputItem struct {
	Type              string                   `json:"type"`
	ID                string                   `json:"id"`
	Role              string                   `json:"role"`
	Status            string                   `json:"status"`
	Content           []responsesOutputContent `json:"content"`
	CallID            string                   `json:"call_id"`
	Name              string                   `json:"name"`
	Arguments         string                   `json:"arguments"`
	Result            string                   `json:"result"`
	RevisedPrompt     string                   `json:"revised_prompt"`
	Background        string                   `json:"background"`
	OutputFormat      string                   `json:"output_format"`
	Quality           string                   `json:"quality"`
	Size              string                   `json:"size"`
	ReasoningContent  string                   `json:"reasoning_content,omitempty"`
	Thinking          string                   `json:"thinking,omitempty"`
	ThinkingSignature string                   `json:"thinking_signature,omitempty"`
}

type responsesResponse struct {
	ID        string                `json:"id"`
	CreatedAt int64                 `json:"created_at"`
	Model     string                `json:"model"`
	Output    []responsesOutputItem `json:"output"`
	Usage     *responsesUsage       `json:"usage"`
	Error     any                   `json:"error,omitempty"`
}

type responsesStreamEvent struct {
	Type       string               `json:"type"`
	Delta      string               `json:"delta,omitempty"`
	Text       string               `json:"text,omitempty"`
	Arguments  flexibleJSONString   `json:"arguments,omitempty"`
	Item       *responsesOutputItem `json:"item,omitempty"`
	ItemID     string               `json:"item_id,omitempty"`
	Response   *responsesResponse   `json:"response,omitempty"`
	Error      *responsesAPIError   `json:"error,omitempty"`
	Annotation any                  `json:"annotation,omitempty"`
}

type flexibleJSONString string

func (s *flexibleJSONString) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = flexibleJSONString(text)
		return nil
	}
	*s = flexibleJSONString(string(data))
	return nil
}

func newOpenAIResponsesProtocolAdapter(request gatewayRequest) gatewayProtocolAdapter {
	return openAIResponsesProtocolAdapter{
		includeUsage:       streamIncludeUsage(request.Payload),
		downstreamProtocol: downstreamCanonicalProtocol(request.DownstreamPath),
	}
}

func (a openAIResponsesProtocolAdapter) ProtocolName() string {
	switch a.downstreamProtocol {
	case canonicalProtocolOpenAIResponses:
		return "openai_responses"
	case canonicalProtocolAnthropicMessages:
		return "openai_responses_to_messages"
	}
	return "openai_responses"
}

func (openAIResponsesProtocolAdapter) UpstreamPath(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + gatewayEndpointResponses
}

func (a openAIResponsesProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if a.downstreamProtocol == canonicalProtocolOpenAIResponses || request.DownstreamPath == gatewayEndpointResponses {
		payload := clonePayload(request.Payload)
		payload["model"] = candidate.Model.UpstreamName
		return applyRequestPolicyForCandidate(payload, canonicalProtocolOpenAIResponses, candidate), nil
	}
	if request.Canonical != nil {
		payload, err := encodeCanonicalRequestToOpenAIResponses(*request.Canonical, candidate)
		if err != nil {
			return nil, err
		}
		return applyRequestPolicyForCandidate(payload, canonicalProtocolOpenAIResponses, candidate), nil
	}
	return convertRequestBetweenProtocols(canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, request.Payload, stringFromPayloadModel(request.Payload), candidate)
}

func (a openAIResponsesProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if statusCode < 200 || statusCode >= 300 {
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: contentType,
			Body:        body,
		}, nil
	}
	normalizedBody, _, err := parseBufferedResponsesBody(body)
	if err != nil {
		return gatewayBufferedResponse{}, err
	}
	if len(normalizedBody) > 0 {
		body = normalizedBody
		contentType = "application/json"
	}
	body = normalizeResponsesBufferedUsage(body)
	if a.downstreamProtocol == canonicalProtocolOpenAIResponses {
		return gatewayBufferedResponse{
			StatusCode:  statusCode,
			ContentType: stringValue(&contentType, "application/json"),
			Body:        body,
			Usage:       parseResponsesUsage(body),
		}, nil
	}

	target := a.downstreamProtocol
	if target == "" {
		target = canonicalProtocolOpenAIChat
	}
	convertedBody, usage, err := convertResponseBetweenProtocols(canonicalProtocolOpenAIResponses, target, body, responseConversionOptions{})
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

func (a openAIResponsesProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	if a.downstreamProtocol == canonicalProtocolOpenAIResponses {
		return proxyResponsesStreamPassthrough(ctx, w, resp, startedAt)
	}
	target := a.downstreamProtocol
	if target == "" {
		target = canonicalProtocolOpenAIChat
	}
	return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolOpenAIResponses, target, canonicalStreamOptions{IncludeUsage: a.includeUsage, Candidate: candidate})
}

func streamIncludeUsage(payload map[string]any) bool {
	streamOptions, ok := payload["stream_options"].(map[string]any)
	if !ok {
		return false
	}
	includeUsage, _ := streamOptions["include_usage"].(bool)
	return includeUsage
}

func completionUsageFromResponsesUsage(usage *responsesUsage) completionUsage {
	if usage == nil {
		return completionUsage{}
	}
	result := completionUsage{
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		ReasoningTokens:  usage.CompletionTokenStats.ReasoningTokens,
	}
	if result.PromptTokens == 0 {
		result.PromptTokens = usage.InputTokens
	}
	if result.CompletionTokens == 0 {
		result.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokensDetails != nil {
		result.CachedPromptTokens = usage.InputTokensDetails.CachedTokens
		result.CacheWriteTokens = usage.InputTokensDetails.CacheWriteTokens
		result.InputImageTokens = usage.InputTokensDetails.ImageTokens
	}
	if usage.OutputTokensDetails != nil {
		result.ReasoningTokens = usage.OutputTokensDetails.ReasoningTokens
	}
	if result.TotalTokens == 0 {
		result.TotalTokens = result.PromptTokens + result.CompletionTokens
	}
	return result.normalized()
}

func responseIDOrFallback(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "chatcmpl-xlyra"
	}
	return id
}

func extractMessageText(content any) string {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, partValue := range value {
			part, ok := partValue.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(anyString(part["type"])) == "text" {
				if text := strings.TrimSpace(anyString(part["text"])); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func normalizeToolOutput(content any) any {
	switch value := content.(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprintf("%v", value)
		}
		return string(encoded)
	}
}

func normalizeImageURL(value any) any {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if url := anyString(typed["url"]); url != "" {
			return url
		}
	}
	return value
}

func anyString(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func intFromAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}
