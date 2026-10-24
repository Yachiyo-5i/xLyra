package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAggregateChatCompletionSSE(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"c1","model":"chat-model","created":1,"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"think "}}]}`,
		``,
		`data: {"id":"c1","choices":[{"index":0,"delta":{"reasoning_content":"hard","content":"Hel"}}]}`,
		``,
		`data: {"id":"c1","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
		``,
		`data: {"id":"c1","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	body, err := aggregateChatCompletionSSE([]byte(stream))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	var decoded struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Role             string `json:"role"`
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Object != "chat.completion" || decoded.Model != "chat-model" || len(decoded.Choices) != 1 {
		t.Fatalf("unexpected envelope: %s", body)
	}
	choice := decoded.Choices[0]
	if choice.Message.Content != "Hello" || choice.Message.ReasoningContent != "think hard" || choice.Message.Role != "assistant" || choice.FinishReason != "stop" {
		t.Fatalf("unexpected choice: %+v", choice)
	}
	if decoded.Usage.PromptTokens != 3 || decoded.Usage.CompletionTokens != 2 {
		t.Fatalf("unexpected usage: %+v", decoded.Usage)
	}
}

func TestAggregateChatCompletionSSEToolCalls(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"c2","model":"chat-model","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`,
		`data: {"id":"c2","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}, "\n")
	body, err := aggregateChatCompletionSSE([]byte(stream))
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !strings.Contains(string(body), `"arguments":"{\"q\":\"x\"}"`) || !strings.Contains(string(body), `"finish_reason":"tool_calls"`) || !strings.Contains(string(body), `"content":null`) {
		t.Fatalf("unexpected tool call aggregation: %s", body)
	}
}

func TestAggregateChatCompletionSSEErrors(t *testing.T) {
	if _, err := aggregateChatCompletionSSE([]byte("data: [DONE]\n")); err == nil {
		t.Fatal("expected error for empty stream")
	}
	if _, err := aggregateChatCompletionSSE([]byte(`data: {"error":{"message":"boom"}}` + "\n")); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected upstream error, got %v", err)
	}
}
