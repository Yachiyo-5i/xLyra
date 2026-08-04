package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/adapter"
	routeengine "xlyra/server/internal/router"
)

type grokResponsesProtocolAdapter struct {
	inner                   openAIResponsesProtocolAdapter
	bridged                 map[string]struct{}
	supportsReasoningEffort bool
}

func newGrokResponsesProtocolAdapter(request gatewayRequest, supportsReasoningEffort bool) *grokResponsesProtocolAdapter {
	return &grokResponsesProtocolAdapter{
		inner: openAIResponsesProtocolAdapter{
			includeUsage:       streamIncludeUsage(request.Payload),
			downstreamProtocol: downstreamCanonicalProtocol(request.DownstreamPath),
		},
		supportsReasoningEffort: supportsReasoningEffort,
	}
}

func (a *grokResponsesProtocolAdapter) ProtocolName() string {
	return a.inner.ProtocolName()
}

func (a *grokResponsesProtocolAdapter) UpstreamPath(baseURL string) string {
	return a.inner.UpstreamPath(adapter.GrokChatBaseURL)
}

func (a *grokResponsesProtocolAdapter) BuildUpstreamPayload(request gatewayRequest, candidate routeengine.Candidate) (map[string]any, error) {
	payload, err := a.inner.BuildUpstreamPayload(request, candidate)
	if err != nil {
		return nil, err
	}
	if request.DownstreamPath == gatewayEndpointResponses {
		a.bridged = normalizeGrokResponsesPayload(payload)
	}
	if !a.supportsReasoningEffort {
		delete(payload, "reasoning")
		delete(payload, "reasoning_effort")
	}
	return payload, nil
}

func (a *grokResponsesProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	response, err := a.inner.TransformBufferedResponse(statusCode, headers, body)
	if err != nil {
		return response, err
	}
	if len(a.bridged) > 0 && response.StatusCode >= 200 && response.StatusCode < 300 && a.inner.downstreamProtocol == canonicalProtocolOpenAIResponses {
		response.Body = rewriteGrokBufferedResponsesBody(response.Body, a.bridged)
	}
	return response, nil
}

func (a *grokResponsesProtocolAdapter) ProxyStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time, candidate routeengine.Candidate) (streamCaptureState, bool, error) {
	if len(a.bridged) > 0 && a.inner.downstreamProtocol == canonicalProtocolOpenAIResponses {
		return proxyGrokResponsesStreamBridged(ctx, w, resp, startedAt, a.bridged)
	}
	return a.inner.ProxyStream(ctx, w, resp, startedAt, candidate)
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
