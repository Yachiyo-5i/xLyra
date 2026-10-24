package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

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
