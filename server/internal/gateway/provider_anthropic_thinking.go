package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	routeengine "xlyra/server/internal/router"
)

const (
	providerThinkingCacheTTL        = 2 * time.Hour
	providerThinkingCacheMaxEntries = 2048
)

var providerThinkingCache = newProviderAnthropicThinkingCache()

type providerAnthropicThinkingCache struct {
	mu      sync.Mutex
	entries map[string]providerThinkingCacheEntry
}

type providerThinkingCacheEntry struct {
	blocks    []map[string]any
	expiresAt time.Time
}

func newProviderAnthropicThinkingCache() *providerAnthropicThinkingCache {
	return &providerAnthropicThinkingCache{entries: map[string]providerThinkingCacheEntry{}}
}

func (c *providerAnthropicThinkingCache) remember(toolUseIDs []string, blocks []map[string]any) {
	if len(toolUseIDs) == 0 || len(blocks) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.pruneLocked(now)
	entry := providerThinkingCacheEntry{
		blocks:    cloneThinkingBlocks(blocks),
		expiresAt: now.Add(providerThinkingCacheTTL),
	}
	for _, id := range toolUseIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		c.entries[id] = entry
	}
	if len(c.entries) > providerThinkingCacheMaxEntries {
		c.pruneOverflowLocked()
	}
}

func (c *providerAnthropicThinkingCache) lookup(toolUseIDs []string) ([]map[string]any, bool) {
	if len(toolUseIDs) == 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.pruneLocked(now)
	for _, id := range toolUseIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		entry, ok := c.entries[id]
		if !ok || now.After(entry.expiresAt) {
			delete(c.entries, id)
			continue
		}
		return cloneThinkingBlocks(entry.blocks), true
	}
	return nil, false
}

func (c *providerAnthropicThinkingCache) pruneLocked(now time.Time) {
	for id, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, id)
		}
	}
}

func (c *providerAnthropicThinkingCache) pruneOverflowLocked() {
	overflow := len(c.entries) - providerThinkingCacheMaxEntries
	for id := range c.entries {
		delete(c.entries, id)
		overflow--
		if overflow <= 0 {
			return
		}
	}
}

func hydrateProviderAnthropicThinking(payload map[string]any, candidate routeengine.Candidate, degradeMissing bool) {
	if payload == nil || !providerThinkingModeEnabled(payload) {
		return
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return
	}
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok || strings.TrimSpace(anyString(message["role"])) != "assistant" {
			continue
		}
		content, ok := message["content"].([]any)
		if !ok {
			continue
		}
		if anthropicContentHasUsableThinking(content) {
			continue
		}
		toolUseIDs := anthropicToolUseIDs(content)
		blocks, ok := providerThinkingCache.lookup(toolUseIDs)
		if !ok {
			continue
		}
		message["content"] = prependAnthropicThinkingBlocks(content, blocks)
	}
	if degradeMissing && providerShouldDegradeMissingThinking(candidate) {
		payload["messages"] = rewriteUnhydratedProviderToolRounds(messages)
	}
}

func providerShouldDegradeMissingThinking(candidate routeengine.Candidate) bool {
	return modelCapability(canonicalProtocolAnthropicMessages, candidate, "thinking_roundtrip") ||
		modelCapability(canonicalProtocolAnthropicMessages, candidate, "thinking_roundtrip_degrade_missing")
}

func rewriteUnhydratedProviderToolRounds(messages []any) []any {
	out := make([]any, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		message, ok := messages[i].(map[string]any)
		if !ok {
			out = append(out, messages[i])
			continue
		}
		role := strings.TrimSpace(anyString(message["role"]))
		content, _ := message["content"].([]any)
		if role == "assistant" {
			toolUseIDs := anthropicToolUseIDs(content)
			if len(toolUseIDs) > 0 && !anthropicContentHasUsableThinking(content) {
				if textContent := removeAnthropicToolUseBlocks(content); anthropicContentHasMeaningfulText(textContent) {
					rewritten := clonePayload(message)
					rewritten["content"] = textContent
					out = append(out, rewritten)
				}
				if i+1 < len(messages) {
					next, nextOK := messages[i+1].(map[string]any)
					nextContent, _ := next["content"].([]any)
					if nextOK && strings.TrimSpace(anyString(next["role"])) == "user" {
						if rewrittenContent, changed := rewriteAnthropicToolResultsAsText(nextContent, toolUseIDs); changed {
							rewritten := clonePayload(next)
							rewritten["content"] = rewrittenContent
							out = append(out, rewritten)
							i++
						}
					}
				}
				continue
			}
		}
		out = append(out, messages[i])
	}
	return out
}

func removeAnthropicToolUseBlocks(content []any) []any {
	out := make([]any, 0, len(content))
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if ok && strings.TrimSpace(anyString(block["type"])) == "tool_use" {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func rewriteAnthropicToolResultsAsText(content []any, toolUseIDs []string) ([]any, bool) {
	idSet := map[string]struct{}{}
	for _, id := range toolUseIDs {
		if id = strings.TrimSpace(id); id != "" {
			idSet[id] = struct{}{}
		}
	}
	out := make([]any, 0, len(content))
	changed := false
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(anyString(block["type"])) != "tool_result" {
			out = append(out, raw)
			continue
		}
		toolUseID := strings.TrimSpace(anyString(block["tool_use_id"]))
		if _, ok := idSet[toolUseID]; !ok {
			out = append(out, raw)
			continue
		}
		changed = true
		out = append(out, map[string]any{
			"type": "text",
			"text": providerToolResultText(toolUseID, block["content"]),
		})
	}
	return out, changed
}

func providerToolResultText(toolUseID string, content any) string {
	text, _ := normalizeToolOutput(content).(string)
	toolUseID = strings.TrimSpace(toolUseID)
	if toolUseID == "" {
		return strings.TrimSpace(text)
	}
	return fmt.Sprintf("Tool result for %s:\n%s", toolUseID, strings.TrimSpace(text))
}

func providerThinkingModeEnabled(payload map[string]any) bool {
	thinking, ok := payload["thinking"]
	if !ok {
		return true
	}
	switch value := thinking.(type) {
	case map[string]any:
		return strings.TrimSpace(anyString(value["type"])) != "disabled"
	case bool:
		return value
	default:
		return true
	}
}

func anthropicContentHasUsableThinking(content []any) bool {
	for _, raw := range content {
		block, _ := raw.(map[string]any)
		if strings.TrimSpace(anyString(block["type"])) != "thinking" {
			continue
		}
		if strings.TrimSpace(anyString(block["thinking"])) != "" {
			return true
		}
	}
	return false
}

func anthropicToolUseIDs(content []any) []string {
	var ids []string
	for _, raw := range content {
		block, _ := raw.(map[string]any)
		if strings.TrimSpace(anyString(block["type"])) != "tool_use" {
			continue
		}
		if id := strings.TrimSpace(anyString(block["id"])); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func prependAnthropicThinkingBlocks(content []any, blocks []map[string]any) []any {
	out := make([]any, 0, len(blocks)+len(content))
	for _, block := range blocks {
		out = append(out, clonePayload(block))
	}
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if ok && strings.TrimSpace(anyString(block["type"])) == "thinking" {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func rememberProviderAnthropicThinkingFromMessageBody(body []byte) {
	if len(body) == 0 {
		return
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return
	}
	rememberProviderAnthropicThinkingFromContent(root["content"])
}

func rememberProviderAnthropicThinkingFromContent(raw any) {
	content, ok := raw.([]any)
	if !ok {
		return
	}
	var blocks []map[string]any
	var toolUseIDs []string
	for _, rawBlock := range content {
		block, _ := rawBlock.(map[string]any)
		switch strings.TrimSpace(anyString(block["type"])) {
		case "thinking":
			if strings.TrimSpace(rawStringFromAny(block["thinking"])) != "" {
				blocks = append(blocks, clonePayload(block))
			}
		case "tool_use":
			if id := strings.TrimSpace(anyString(block["id"])); id != "" {
				toolUseIDs = append(toolUseIDs, id)
			}
		}
	}
	providerThinkingCache.remember(toolUseIDs, blocks)
}

type providerAnthropicStreamInspector struct {
	thinkingByIndex  map[int]*strings.Builder
	signatureByIndex map[int]*strings.Builder
	blockByIndex     map[int]map[string]any
	toolUseIDs       []string
}

func newProviderAnthropicStreamInspector() *providerAnthropicStreamInspector {
	return &providerAnthropicStreamInspector{
		thinkingByIndex:  map[int]*strings.Builder{},
		signatureByIndex: map[int]*strings.Builder{},
		blockByIndex:     map[int]map[string]any{},
	}
}

func (i *providerAnthropicStreamInspector) inspect(line []byte, capture *streamCaptureState) {
	data, _, ok := sseDataFromLine(line)
	if !ok {
		return
	}
	var event struct {
		Type         string                    `json:"type"`
		Index        int                       `json:"index"`
		ContentBlock map[string]any            `json:"content_block"`
		Delta        map[string]any            `json:"delta"`
		Usage        anthropicMessageUsageBody `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}
	switch event.Type {
	case "content_block_start":
		i.inspectContentBlockStart(event.Index, event.ContentBlock)
	case "content_block_delta":
		i.inspectContentBlockDelta(event.Index, event.Delta)
	case "message_delta":
		if capture != nil && (event.Usage.OutputTokens > 0 || event.Usage.InputTokens > 0) {
			capture.usage = completionUsageFromGatewayUsage(gatewayUsageFromAnthropicUsage(event.Usage))
		}
	case "message_stop":
		if capture != nil {
			capture.sawDone = true
			capture.streamCompleted = true
			capture.endReason = "done"
		}
		i.flush()
	}
}

func (i *providerAnthropicStreamInspector) inspectContentBlockStart(index int, block map[string]any) {
	switch strings.TrimSpace(anyString(block["type"])) {
	case "thinking":
		builder := &strings.Builder{}
		if initial := rawStringFromAny(block["thinking"]); initial != "" {
			builder.WriteString(initial)
		}
		i.thinkingByIndex[index] = builder
		i.blockByIndex[index] = clonePayload(block)
	case "tool_use":
		if id := strings.TrimSpace(anyString(block["id"])); id != "" {
			i.toolUseIDs = append(i.toolUseIDs, id)
		}
	}
}

func (i *providerAnthropicStreamInspector) inspectContentBlockDelta(index int, delta map[string]any) {
	switch strings.TrimSpace(anyString(delta["type"])) {
	case "thinking_delta":
		builder := i.thinkingByIndex[index]
		if builder == nil {
			builder = &strings.Builder{}
			i.thinkingByIndex[index] = builder
		}
		builder.WriteString(rawStringFromAny(delta["thinking"]))
	case "signature_delta":
		builder := i.signatureByIndex[index]
		if builder == nil {
			builder = &strings.Builder{}
			i.signatureByIndex[index] = builder
		}
		builder.WriteString(rawStringFromAny(delta["signature"]))
	}
}

func (i *providerAnthropicStreamInspector) flush() {
	var blocks []map[string]any
	indexes := make([]int, 0, len(i.thinkingByIndex))
	for index := range i.thinkingByIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		builder := i.thinkingByIndex[index]
		thinking := strings.TrimSpace(builder.String())
		if thinking == "" {
			continue
		}
		block := clonePayload(i.blockByIndex[index])
		block["type"] = "thinking"
		block["thinking"] = builder.String()
		if signature := i.signatureByIndex[index]; signature != nil && strings.TrimSpace(signature.String()) != "" {
			block["signature"] = signature.String()
		}
		blocks = append(blocks, block)
	}
	providerThinkingCache.remember(i.toolUseIDs, blocks)
}

func proxyProviderAnthropicMessagesStream(ctx context.Context, w http.ResponseWriter, resp *http.Response, startedAt time.Time) (streamCaptureState, bool, error) {
	capture := streamCaptureState{}
	if resp == nil || resp.Body == nil {
		capture.endReason = "upstream_stream_missing_body"
		return capture, false, fmt.Errorf("upstream stream body is not available")
	}

	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReader(resp.Body)
	headersWritten := false
	inspector := newProviderAnthropicStreamInspector()

	writeHeaders := func() {
		if headersWritten {
			return
		}
		copyStreamingHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		headersWritten = true
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
			if _, writeErr := w.Write(line); writeErr != nil {
				capture.endReason = "downstream_stream_write_failed"
				return capture, headersWritten, writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
			inspector.inspect(line, &capture)
		}

		if err == nil {
			continue
		}
		if err == io.EOF {
			if capture.streamCompleted {
				return capture, headersWritten, nil
			}
			if headersWritten {
				inspector.flush()
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

func cloneThinkingBlocks(blocks []map[string]any) []map[string]any {
	if len(blocks) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, clonePayload(block))
	}
	return out
}

func rawStringFromAny(value any) string {
	text, _ := value.(string)
	return text
}
