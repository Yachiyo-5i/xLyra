package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"xlyra/server/internal/adapter"
	routeengine "xlyra/server/internal/router"
)

type grokResponsesProtocolAdapter struct {
	inner            openAIResponsesProtocolAdapter
	bridged          map[string]struct{}
	reasoningEfforts map[string]struct{}
	stopSequences    []string
}

func newGrokResponsesProtocolAdapter(request gatewayRequest, supportsReasoningEffort bool) *grokResponsesProtocolAdapter {
	result := &grokResponsesProtocolAdapter{
		inner: openAIResponsesProtocolAdapter{
			includeUsage:       chatStreamUsageEnabled(request.Payload),
			downstreamProtocol: downstreamCanonicalProtocol(request.DownstreamPath),
		},
	}
	if supportsReasoningEffort {
		result.reasoningEfforts = grokReasoningEffortSet([]string{"low", "medium", "high"})
	}
	return result
}

func newGrokResponsesProtocolAdapterWithReasoningEfforts(request gatewayRequest, efforts []string) *grokResponsesProtocolAdapter {
	result := newGrokResponsesProtocolAdapter(request, len(efforts) > 0)
	result.reasoningEfforts = grokReasoningEffortSet(efforts)
	return result
}

func (a *grokResponsesProtocolAdapter) ProtocolName() string {
	return a.inner.ProtocolName()
}

func (a *grokResponsesProtocolAdapter) UpstreamPath(baseURL string) string {
	return a.inner.UpstreamPath(adapter.GrokChatBaseURL)
}

func (a *grokResponsesProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	if params := a.incompatibleRequestParams(request); len(params) > 0 {
		return nil, fmt.Errorf("Grok Responses cannot preserve request parameters: %s", strings.Join(params, ", "))
	}
	payload, err := a.inner.BuildUpstreamPayload(request, candidate)
	if err != nil {
		return nil, err
	}
	a.stopSequences = grokRequestStopSequences(request)
	normalizeGrokCanonicalPayload(payload, request, a.reasoningEfforts)
	a.bridged = normalizeGrokResponsesPayload(payload)
	delete(payload, "metadata")
	delete(payload, "reasoning_effort")
	if len(a.reasoningEfforts) == 0 {
		delete(payload, "reasoning")
	}
	filterGrokResponsesRequestFields(payload)
	return payload, nil
}

func (a *grokResponsesProtocolAdapter) incompatibleRequestParams(request gatewayRequest) []string {
	params := grokIncompatibleRequestParams(request)
	if len(a.reasoningEfforts) == 0 && grokRequestUsesReasoning(request) {
		params = appendUniqueStrings(params, "reasoning")
		sort.Strings(params)
	}
	return params
}

func normalizeGrokCanonicalPayload(payload map[string]any, request gatewayRequest, reasoningEfforts map[string]struct{}) {
	normalizeGrokIdentity(payload)
	canonical := request.Canonical
	if canonical == nil {
		return
	}
	normalizeGrokReasoningEffort(payload, canonical.Params, canonical.SourceProtocol, reasoningEfforts)
	textFormat := canonical.TextFormat
	if canonical.SourceProtocol == canonicalProtocolAnthropicMessages {
		normalizeGrokAnthropicToolChoice(payload, canonical.Raw)
		normalizeGrokAnthropicReasoning(payload, canonical.Params, reasoningEfforts)
		if textFormat == nil {
			if outputConfig, ok := canonical.Params["output_config"].(map[string]any); ok {
				textFormat = outputConfig["format"]
			}
		}
	}
	normalizeGrokServiceTier(payload, canonical.SourceProtocol)
	if textFormat != nil {
		if _, exists := payload["text"]; !exists {
			if text := grokResponsesTextFormat(textFormat); text != nil {
				payload["text"] = text
			}
		}
	}
}

func normalizeGrokReasoningEffort(payload map[string]any, params map[string]any, source canonicalProtocol, reasoningEfforts map[string]struct{}) {
	if len(reasoningEfforts) == 0 {
		return
	}
	effort := strings.ToLower(strings.TrimSpace(anyString(params["reasoning_effort"])))
	if effort == "" {
		if reasoning, ok := params["reasoning"].(map[string]any); ok {
			effort = strings.ToLower(strings.TrimSpace(anyString(reasoning["effort"])))
		}
	}
	if effort == "" {
		return
	}
	effort = nearestGrokReasoningEffort(effort, reasoningEfforts)
	if effort == "" {
		return
	}
	if source == canonicalProtocolOpenAIResponses {
		if existing, ok := payload["reasoning"].(map[string]any); ok {
			preserved := clonePayload(existing)
			preserved["effort"] = effort
			payload["reasoning"] = preserved
			return
		}
	}
	payload["reasoning"] = map[string]any{"effort": effort, "summary": "detailed"}
}

func grokReasoningEffortSet(efforts []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, effort := range efforts {
		effort = strings.ToLower(strings.TrimSpace(effort))
		if effort != "" {
			result[effort] = struct{}{}
		}
	}
	return result
}

func nearestGrokReasoningEffort(requested string, supported map[string]struct{}) string {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if _, ok := supported[requested]; ok {
		return requested
	}
	levels := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	requestedIndex := len(levels) - 1
	for index, level := range levels {
		if level == requested {
			requestedIndex = index
			break
		}
	}
	for index := requestedIndex; index >= 0; index-- {
		if _, ok := supported[levels[index]]; ok {
			return levels[index]
		}
	}
	for index := requestedIndex + 1; index < len(levels); index++ {
		if _, ok := supported[levels[index]]; ok {
			return levels[index]
		}
	}
	return ""
}

func normalizeGrokIdentity(payload map[string]any) {
	if _, exists := payload["safety_identifier"]; !exists {
		if user := strings.TrimSpace(anyString(payload["user"])); user != "" {
			payload["safety_identifier"] = user
		} else if metadata, ok := payload["metadata"].(map[string]any); ok {
			if userID := strings.TrimSpace(anyString(metadata["user_id"])); userID != "" {
				payload["safety_identifier"] = userID
			}
		}
	}
	delete(payload, "user")
}

func normalizeGrokServiceTier(payload map[string]any, source canonicalProtocol) {
	tier := strings.ToLower(strings.TrimSpace(anyString(payload["service_tier"])))
	if source == canonicalProtocolAnthropicMessages {
		switch tier {
		case "auto":
			payload["service_tier"] = "priority"
		case "standard_only":
			payload["service_tier"] = "default"
		}
		return
	}
	switch tier {
	case "auto", "standard":
		payload["service_tier"] = "default"
	case "fast":
		payload["service_tier"] = "priority"
	}
}

func normalizeGrokAnthropicToolChoice(payload map[string]any, raw map[string]any) {
	choice, _ := raw["tool_choice"].(map[string]any)
	if len(choice) == 0 {
		return
	}
	choiceType := strings.ToLower(strings.TrimSpace(anyString(choice["type"])))
	switch choiceType {
	case "auto":
		payload["tool_choice"] = "auto"
	case "any":
		payload["tool_choice"] = "required"
	case "none":
		payload["tool_choice"] = "none"
	case "tool":
		name := strings.TrimSpace(anyString(choice["name"]))
		if name != "" {
			payload["tool_choice"] = map[string]any{"type": "function", "name": name}
		}
	default:
		return
	}
	if disabled, ok := choice["disable_parallel_tool_use"].(bool); ok && choiceType != "none" {
		payload["parallel_tool_calls"] = !disabled
	}
}

func normalizeGrokAnthropicReasoning(payload map[string]any, params map[string]any, reasoningEfforts map[string]struct{}) {
	if len(reasoningEfforts) == 0 {
		return
	}
	if outputConfig, ok := params["output_config"].(map[string]any); ok {
		if effort := strings.ToLower(strings.TrimSpace(anyString(outputConfig["effort"]))); effort != "" {
			effort = nearestGrokReasoningEffort(effort, reasoningEfforts)
			if effort != "" {
				payload["reasoning"] = map[string]any{"effort": effort, "summary": "detailed"}
			}
		}
	}
	thinking, ok := params["thinking"].(map[string]any)
	if !ok {
		return
	}
	switch strings.ToLower(strings.TrimSpace(anyString(thinking["type"]))) {
	case "disabled":
		delete(payload, "reasoning")
	case "enabled", "adaptive":
		if _, exists := payload["reasoning"]; !exists {
			if effort := nearestGrokReasoningEffort("high", reasoningEfforts); effort != "" {
				payload["reasoning"] = map[string]any{"effort": effort, "summary": "detailed"}
			}
		}
	}
}

func grokResponsesTextFormat(raw any) map[string]any {
	format, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	typeName := strings.TrimSpace(anyString(format["type"]))
	if typeName == "" {
		return nil
	}
	result := map[string]any{"type": typeName}
	for key, value := range format {
		if key == "type" {
			continue
		}
		if key == "json_schema" {
			if nested, ok := value.(map[string]any); ok {
				for nestedKey, nestedValue := range nested {
					result[nestedKey] = nestedValue
				}
			}
			continue
		}
		result[key] = value
	}
	if typeName == "json_schema" {
		if strings.TrimSpace(anyString(result["name"])) == "" {
			result["name"] = "response"
		}
		if _, ok := result["schema"]; !ok {
			return nil
		}
	}
	return map[string]any{"format": result}
}

var grokResponsesRequestFields = map[string]struct{}{
	"background":             {},
	"conversation":           {},
	"include":                {},
	"input":                  {},
	"instructions":           {},
	"max_output_tokens":      {},
	"max_tool_calls":         {},
	"model":                  {},
	"parallel_tool_calls":    {},
	"previous_response_id":   {},
	"prompt":                 {},
	"prompt_cache_retention": {},
	"reasoning":              {},
	"safety_identifier":      {},
	"service_tier":           {},
	"store":                  {},
	"stream":                 {},
	"stream_options":         {},
	"temperature":            {},
	"text":                   {},
	"tool_choice":            {},
	"tools":                  {},
	"top_p":                  {},
	"truncation":             {},
}

var grokCompatibleParamsByProtocol = map[canonicalProtocol]map[string]struct{}{
	canonicalProtocolOpenAIResponses: {
		"background": {}, "conversation": {}, "include": {}, "max_output_tokens": {}, "max_tool_calls": {},
		"metadata": {}, "parallel_tool_calls": {}, "previous_response_id": {}, "prompt": {},
		"prompt_cache_retention": {}, "reasoning": {}, "reasoning_effort": {}, "response_format": {}, "safety_identifier": {},
		"service_tier": {}, "store": {}, "stream": {}, "stream_options": {}, "temperature": {}, "text": {},
		"top_p": {}, "truncation": {}, "user": {},
	},
	canonicalProtocolOpenAIChat: {
		"max_completion_tokens": {}, "max_tokens": {}, "parallel_tool_calls": {}, "reasoning_effort": {},
		"response_format": {}, "service_tier": {}, "stop": {}, "store": {}, "stream": {},
		"stream_options": {}, "temperature": {}, "top_p": {}, "user": {},
	},
	canonicalProtocolAnthropicMessages: {
		"max_output_tokens": {}, "metadata": {}, "output_config": {}, "service_tier": {},
		"stop_sequences": {}, "stream": {}, "temperature": {}, "thinking": {}, "top_p": {},
	},
}

func filterGrokResponsesRequestFields(payload map[string]any) {
	for key := range payload {
		if _, ok := grokResponsesRequestFields[key]; !ok {
			delete(payload, key)
		}
	}
}

func grokRequestUsesReasoning(request gatewayRequest) bool {
	if request.Canonical == nil {
		return false
	}
	params := request.Canonical.Params
	if strings.TrimSpace(anyString(params["reasoning_effort"])) != "" {
		return true
	}
	if reasoning, ok := params["reasoning"].(map[string]any); ok && len(reasoning) > 0 {
		return true
	}
	if thinking, ok := params["thinking"].(map[string]any); ok {
		typeName := strings.ToLower(strings.TrimSpace(anyString(thinking["type"])))
		if typeName == "enabled" || typeName == "adaptive" {
			return true
		}
	}
	if outputConfig, ok := params["output_config"].(map[string]any); ok {
		return strings.TrimSpace(anyString(outputConfig["effort"])) != ""
	}
	return false
}

func grokIncompatibleRequestParams(request gatewayRequest) []string {
	if request.Canonical == nil {
		return nil
	}
	canonical := request.Canonical
	switch canonical.SourceProtocol {
	case canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalProtocolAnthropicMessages:
	default:
		return nil
	}
	incompatible := make([]string, 0)
	seen := map[string]struct{}{}
	add := func(value string) {
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		incompatible = append(incompatible, value)
	}
	compatible := grokCompatibleParamsByProtocol[canonical.SourceProtocol]
	for key, value := range canonical.Params {
		if value == nil {
			continue
		}
		if _, ok := compatible[key]; !ok {
			add(key)
		}
	}
	for _, message := range canonical.Messages {
		if len(message.Thinking) > 0 {
			add("thinking_history")
		}
		for _, part := range message.Content {
			if part.CacheControl != nil {
				add("cache_control")
			}
		}
	}
	if canonical.SourceProtocol == canonicalProtocolOpenAIResponses {
		if input, ok := request.Payload["input"].([]any); ok {
			for _, raw := range input {
				item, _ := raw.(map[string]any)
				if strings.EqualFold(strings.TrimSpace(anyString(item["type"])), "reasoning") {
					add("reasoning_history")
				}
			}
		}
		if metadata, ok := request.Payload["metadata"].(map[string]any); ok {
			for key := range metadata {
				if key != "user_id" {
					add("metadata." + key)
				}
			}
		}
		if include, ok := request.Payload["include"].([]any); ok {
			for _, item := range include {
				if strings.Contains(strings.ToLower(anyString(item)), "reasoning.encrypted_content") {
					add("include.reasoning.encrypted_content")
				}
			}
		}
		if reasoning, ok := request.Payload["reasoning"].(map[string]any); ok {
			for key := range reasoning {
				if key != "effort" && key != "summary" {
					add("reasoning." + key)
				}
			}
		}
	}
	tier := strings.ToLower(strings.TrimSpace(anyString(canonical.Params["service_tier"])))
	if tier != "" {
		allowed := map[string]struct{}{"default": {}, "priority": {}}
		if canonical.SourceProtocol == canonicalProtocolAnthropicMessages {
			allowed["auto"] = struct{}{}
			allowed["standard_only"] = struct{}{}
		} else {
			allowed["auto"] = struct{}{}
			allowed["standard"] = struct{}{}
			allowed["fast"] = struct{}{}
		}
		if _, ok := allowed[tier]; !ok {
			add("service_tier")
		}
	}
	if format := grokRequestTextFormat(canonical); format != nil {
		typeName := strings.ToLower(strings.TrimSpace(anyString(format["type"])))
		if typeName != "" && typeName != "text" && typeName != "json_object" && typeName != "json_schema" {
			add("text.format.type")
		}
	}
	sort.Strings(incompatible)
	return incompatible
}

func grokRequestTextFormat(request *canonicalRequest) map[string]any {
	if request == nil {
		return nil
	}
	if format, ok := request.TextFormat.(map[string]any); ok {
		return format
	}
	if outputConfig, ok := request.Params["output_config"].(map[string]any); ok {
		format, _ := outputConfig["format"].(map[string]any)
		return format
	}
	return nil
}

func grokRequestStopSequences(request gatewayRequest) []string {
	if request.Canonical == nil {
		return nil
	}
	values := make([]any, 0)
	for _, key := range []string{"stop_sequences", "stop"} {
		switch value := request.Canonical.Params[key].(type) {
		case string:
			values = append(values, value)
		case []any:
			values = append(values, value...)
		case []string:
			for _, item := range value {
				values = append(values, item)
			}
		}
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		stop := anyString(value)
		if stop == "" {
			continue
		}
		if _, ok := seen[stop]; ok {
			continue
		}
		seen[stop] = struct{}{}
		result = append(result, stop)
	}
	return result
}

func (a *grokResponsesProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	response, err := a.inner.TransformBufferedResponse(statusCode, headers, body)
	if err != nil {
		return response, err
	}
	if len(a.bridged) > 0 && response.StatusCode >= 200 && response.StatusCode < 300 && a.inner.downstreamProtocol == canonicalProtocolOpenAIResponses {
		response.Body = rewriteGrokBufferedResponsesBody(response.Body, a.bridged)
	}
	if len(a.stopSequences) > 0 && response.StatusCode >= 200 && response.StatusCode < 300 {
		response.Body = applyGrokBufferedStopSequences(response.Body, a.inner.downstreamProtocol, a.stopSequences)
	}
	return response, nil
}

func (a *grokResponsesProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	if len(a.bridged) > 0 && a.inner.downstreamProtocol == canonicalProtocolOpenAIResponses {
		return proxyGrokResponsesStreamBridged(ctx, w, resp, startedAt, a.bridged)
	}
	if len(a.stopSequences) > 0 && a.inner.downstreamProtocol != canonicalProtocolOpenAIResponses {
		target := a.inner.downstreamProtocol
		if target == "" {
			target = canonicalProtocolOpenAIChat
		}
		return proxyCanonicalStream(ctx, w, resp, startedAt, canonicalProtocolOpenAIResponses, target, canonicalStreamOptions{
			IncludeUsage:  a.inner.includeUsage,
			Candidate:     candidate,
			StopSequences: a.stopSequences,
		})
	}
	return a.inner.ProxyStream(ctx, w, resp, startedAt, candidate)
}

func applyGrokBufferedStopSequences(body []byte, protocol canonicalProtocol, stopSequences []string) []byte {
	var payload map[string]any
	if len(stopSequences) == 0 || json.Unmarshal(body, &payload) != nil {
		return body
	}
	changed := false
	switch protocol {
	case canonicalProtocolAnthropicMessages:
		content, _ := payload["content"].([]any)
		for index, raw := range content {
			block, _ := raw.(map[string]any)
			if anyString(block["type"]) != "text" {
				continue
			}
			text, stop, matched := truncateAtStopSequence(anyStringRaw(block["text"]), stopSequences)
			if !matched {
				continue
			}
			block["text"] = text
			payload["content"] = content[:index+1]
			payload["stop_reason"] = "stop_sequence"
			payload["stop_sequence"] = stop
			changed = true
			break
		}
	case canonicalProtocolOpenAIChat, "":
		choices, _ := payload["choices"].([]any)
		for _, raw := range choices {
			choice, _ := raw.(map[string]any)
			message, _ := choice["message"].(map[string]any)
			text, _, matched := truncateAtStopSequence(anyStringRaw(message["content"]), stopSequences)
			if !matched {
				continue
			}
			message["content"] = text
			delete(message, "tool_calls")
			choice["finish_reason"] = "stop"
			changed = true
		}
	}
	if !changed {
		return body
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func truncateAtStopSequence(text string, stopSequences []string) (string, string, bool) {
	runes := []rune(text)
	matchIndex := -1
	matched := ""
	for _, stop := range stopSequences {
		sequence := []rune(stop)
		if len(sequence) == 0 || len(sequence) > len(runes) {
			continue
		}
		for index := 0; index+len(sequence) <= len(runes); index++ {
			equal := true
			for offset := range sequence {
				if runes[index+offset] != sequence[offset] {
					equal = false
					break
				}
			}
			if equal && (matchIndex < 0 || index < matchIndex) {
				matchIndex = index
				matched = stop
			}
		}
	}
	if matchIndex < 0 {
		return text, "", false
	}
	return string(runes[:matchIndex]), matched, true
}

type grokImagesProtocolAdapter struct {
	openAIImagesProtocolAdapter
}

func newGrokImagesProtocolAdapter(request gatewayRequest, candidate routeengine.Candidate) grokImagesProtocolAdapter {
	return grokImagesProtocolAdapter{newOpenAIImagesProtocolAdapter(request, candidate)}
}

func (a grokImagesProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	payload, err := a.openAIImagesProtocolAdapter.BuildUpstreamPayload(request, candidate)
	if err != nil {
		return nil, err
	}
	normalizeGrokImagesPayload(payload)
	return payload, nil
}

func (a grokImagesProtocolAdapter) UpstreamPath(_ string) string {
	path := gatewayEndpointImagesGenerations
	if a.downstreamPath == gatewayEndpointImagesEdits {
		path = gatewayEndpointImagesEdits
	}
	return adapter.GrokAPIBaseURL + path
}

func (a grokImagesProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	response, err := a.openAIImagesProtocolAdapter.TransformBufferedResponse(statusCode, headers, body)
	if err != nil {
		return response, err
	}
	if grokUpstreamCharged(body) {
		response.UpstreamBilled = true
		if response.Usage.ImageCount == 0 {
			response.Usage.ImageCount = 1
		}
	}
	return response, nil
}

func grokUpstreamCharged(body []byte) bool {
	var envelope struct {
		Usage struct {
			CostInUSDTicks json.Number `json:"cost_in_usd_ticks"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}
	ticks, err := envelope.Usage.CostInUSDTicks.Int64()
	if err != nil {
		return false
	}
	return ticks > 0
}

func rewriteGrokBufferedResponsesBody(body []byte, bridged map[string]struct{}) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	output, ok := payload["output"].([]any)
	if !ok {
		return body
	}
	changed := false
	for _, item := range output {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if rewriteGrokBridgedOutputItem(itemMap, bridged) {
			changed = true
		}
	}
	if !changed {
		return body
	}
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

func rewriteGrokBridgedOutputItem(item map[string]any, bridged map[string]struct{}) bool {
	if anyString(item["type"]) != "function_call" {
		return false
	}
	if _, ok := bridged[anyString(item["name"])]; !ok {
		return false
	}
	item["type"] = "custom_tool_call"
	item["id"] = responsesItemIDForType("custom_tool_call", anyString(item["id"]), anyString(item["call_id"]))
	arguments, _ := item["arguments"].(string)
	item["input"] = grokBridgedInputFromArguments(arguments)
	delete(item, "arguments")
	return true
}

type grokStreamBridge struct {
	bridged map[string]struct{}
	calls   map[string]*strings.Builder
	itemIDs map[string]string
}

func newGrokStreamBridge(bridged map[string]struct{}) *grokStreamBridge {
	return &grokStreamBridge{bridged: bridged, calls: map[string]*strings.Builder{}, itemIDs: map[string]string{}}
}

func proxyGrokResponsesStreamBridged(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, bridged map[string]struct{}) (streamCaptureState, bool, error) {
	capture := streamCaptureState{}
	if resp == nil || resp.Body == nil {
		capture.endReason = "upstream_stream_missing_body"
		return capture, false, fmt.Errorf("upstream stream body is not available")
	}
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)
	bridge := newGrokStreamBridge(bridged)
	headersWritten := false
	writeHeaders := func() {
		if headersWritten {
			return
		}
		copyStreamingHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		headersWritten = true
	}
	var block []byte
	flushBlock := func() error {
		if len(block) == 0 {
			return nil
		}
		out := bridge.rewriteBlock(block, &capture)
		block = nil
		if len(out) == 0 {
			return nil
		}
		if _, writeErr := w.Write(out); writeErr != nil {
			return writeErr
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			capture.endReason = "downstream_client_cancelled"
			return capture, headersWritten, err
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if !headersWritten {
				capture.firstByteLatency = time.Since(startedAt).Milliseconds()
				writeHeaders()
			}
			block = append(block, line...)
			if len(bytes.TrimSpace(line)) == 0 {
				if writeErr := flushBlock(); writeErr != nil {
					capture.endReason = "downstream_stream_write_failed"
					return capture, headersWritten, writeErr
				}
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			if writeErr := flushBlock(); writeErr != nil {
				capture.endReason = "downstream_stream_write_failed"
				return capture, headersWritten, writeErr
			}
			if capture.streamCompleted {
				capture.endReason = "done"
			} else if capture.endReason != "" {
			} else if capture.sawDone {
				capture.streamCompleted = true
				capture.endReason = "done"
			} else if headersWritten {
				capture.endReason = "upstream_stream_eof"
			} else {
				capture.endReason = "upstream_stream_empty"
			}
			return capture, headersWritten, nil
		}
		capture.endReason = "upstream_stream_read_failed"
		return capture, headersWritten, err
	}
}

func (b *grokStreamBridge) rewriteBlock(block []byte, capture *streamCaptureState) []byte {
	data := sseBlockData(block)
	if data == "" || data == "[DONE]" {
		if data == "[DONE]" && capture != nil {
			capture.sawDone = true
		}
		return block
	}
	inspectResponsesStreamLine([]byte("data: "+data), capture)
	var event map[string]any
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return block
	}
	switch anyString(event["type"]) {
	case "response.output_item.added":
		item, ok := event["item"].(map[string]any)
		if !ok || anyString(item["type"]) != "function_call" {
			return block
		}
		if _, tracked := b.bridged[anyString(item["name"])]; !tracked {
			return block
		}
		upstreamID := strings.TrimSpace(anyString(item["id"]))
		callID := strings.TrimSpace(anyString(item["call_id"]))
		downstreamID := responsesItemIDForType("custom_tool_call", upstreamID, callID)
		lookupID := upstreamID
		if lookupID == "" {
			lookupID = callID
		}
		if lookupID == "" {
			lookupID = downstreamID
		}
		builder := &strings.Builder{}
		b.calls[lookupID] = builder
		b.itemIDs[lookupID] = downstreamID
		b.calls[downstreamID] = builder
		b.itemIDs[downstreamID] = downstreamID
		if upstreamID != "" {
			b.calls[upstreamID] = builder
			b.itemIDs[upstreamID] = downstreamID
		}
		if callID != "" {
			b.calls[callID] = builder
			b.itemIDs[callID] = downstreamID
		}
		item["id"] = downstreamID
		item["type"] = "custom_tool_call"
		item["input"] = ""
		delete(item, "arguments")
		return encodeSSEEvents(event)
	case "response.function_call_arguments.delta":
		builder, tracked := b.calls[anyString(event["item_id"])]
		if !tracked {
			return block
		}
		builder.WriteString(anyStringRaw(event["delta"]))
		return nil
	case "response.function_call_arguments.done":
		itemID := strings.TrimSpace(anyString(event["item_id"]))
		builder, tracked := b.calls[itemID]
		if !tracked {
			return block
		}
		arguments := anyStringRaw(event["arguments"])
		if arguments == "" {
			arguments = builder.String()
		}
		input := grokBridgedInputFromArguments(arguments)
		downstreamID := b.itemIDs[itemID]
		if downstreamID == "" {
			downstreamID = responsesItemIDForType("custom_tool_call", itemID, "")
		}
		deltaEvent := map[string]any{
			"type":    "response.custom_tool_call_input.delta",
			"item_id": downstreamID,
			"delta":   input,
		}
		doneEvent := map[string]any{
			"type":    "response.custom_tool_call_input.done",
			"item_id": downstreamID,
			"input":   input,
		}
		for _, key := range []string{"output_index", "sequence_number"} {
			if value, ok := event[key]; ok {
				deltaEvent[key] = value
				doneEvent[key] = value
			}
		}
		return encodeSSEEvents(deltaEvent, doneEvent)
	case "response.output_item.done":
		item, ok := event["item"].(map[string]any)
		if !ok {
			return block
		}
		if id := anyString(item["id"]); id != "" {
			delete(b.calls, id)
			if downstreamID := b.itemIDs[id]; downstreamID != "" {
				item["id"] = downstreamID
			}
		}
		if !rewriteGrokBridgedOutputItem(item, b.bridged) {
			return block
		}
		return encodeSSEEvents(event)
	case "response.completed", "response.incomplete", "response.failed":
		response, ok := event["response"].(map[string]any)
		if !ok {
			return block
		}
		output, ok := response["output"].([]any)
		if !ok {
			return block
		}
		changed := false
		for _, item := range output {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if upstreamID := anyString(itemMap["id"]); upstreamID != "" {
				if downstreamID := b.itemIDs[upstreamID]; downstreamID != "" {
					itemMap["id"] = downstreamID
				}
			}
			if rewriteGrokBridgedOutputItem(itemMap, b.bridged) {
				changed = true
			}
		}
		if !changed {
			return block
		}
		return encodeSSEEvents(event)
	}
	return block
}

func sseBlockData(block []byte) string {
	var parts []string
	for _, line := range bytes.Split(block, []byte("\n")) {
		text := strings.TrimRight(string(line), "\r")
		if strings.HasPrefix(text, "data:") {
			parts = append(parts, strings.TrimSpace(strings.TrimPrefix(text, "data:")))
		}
	}
	return strings.Join(parts, "\n")
}

func encodeSSEEvents(events ...map[string]any) []byte {
	var out bytes.Buffer
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			continue
		}
		out.WriteString("event: ")
		out.WriteString(anyString(event["type"]))
		out.WriteString("\n")
		out.WriteString("data: ")
		out.Write(encoded)
		out.WriteString("\n\n")
	}
	return out.Bytes()
}

func anyStringRaw(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
