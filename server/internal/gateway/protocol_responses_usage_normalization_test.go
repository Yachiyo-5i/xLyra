package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestResponsesStreamPassthroughCompletesUsageDetails(t *testing.T) {
	t.Parallel()

	stream := gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_1","output":[],"usage":{"input_tokens":50,"output_tokens":12,"total_tokens":62,"input_tokens_details":{"image_tokens":40,"cache_write_tokens":5},"output_tokens_details":{}}}}`)

	rec, capture, started, err := proxyResponsesStreamPassthroughTest(t, stream)
	if err != nil {
		t.Fatalf("proxyResponsesStreamPassthrough returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("stream state = started %v capture %#v", started, capture)
	}

	root := responseCompletedPayloadFromStream(t, rec.Body.String())
	response := root["response"].(map[string]any)
	usage := response["usage"].(map[string]any)
	inputDetails := usage["input_tokens_details"].(map[string]any)
	outputDetails := usage["output_tokens_details"].(map[string]any)
	if inputDetails["cached_tokens"] != float64(0) {
		t.Fatalf("cached_tokens = %#v, want 0", inputDetails["cached_tokens"])
	}
	if inputDetails["image_tokens"] != float64(40) || inputDetails["cache_write_tokens"] != float64(5) {
		t.Fatalf("input token details not preserved: %#v", inputDetails)
	}
	if outputDetails["reasoning_tokens"] != float64(0) {
		t.Fatalf("reasoning_tokens = %#v, want 0", outputDetails["reasoning_tokens"])
	}
	if capture.usage.InputImageTokens != 40 || capture.usage.CacheWriteTokens != 5 {
		t.Fatalf("capture usage = %#v", capture.usage)
	}
}

func TestOpenAIResponsesBufferedResponseCompletesUsageDetails(t *testing.T) {
	t.Parallel()

	adapter := openAIResponsesProtocolAdapter{downstreamProtocol: canonicalProtocolOpenAIResponses}
	transformed, err := adapter.TransformBufferedResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, []byte(`{
		"id": "resp_1",
		"object": "response",
		"created_at": 1710000000,
		"model": "gpt-5.6-sol",
		"output": [],
		"usage": {
			"input_tokens": 7,
			"output_tokens": 3,
			"total_tokens": 10,
			"input_tokens_details": {"image_tokens": 2}
		}
	}`))
	if err != nil {
		t.Fatalf("TransformBufferedResponse returned error: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(transformed.Body, &root); err != nil {
		t.Fatalf("normalized body is invalid JSON: %v", err)
	}
	usage := root["usage"].(map[string]any)
	inputDetails := usage["input_tokens_details"].(map[string]any)
	outputDetails := usage["output_tokens_details"].(map[string]any)
	if inputDetails["cached_tokens"] != float64(0) || inputDetails["image_tokens"] != float64(2) {
		t.Fatalf("input token details = %#v", inputDetails)
	}
	if outputDetails["reasoning_tokens"] != float64(0) {
		t.Fatalf("output token details = %#v", outputDetails)
	}
}

func TestResponsesUsagePayloadIncludesCodexTokenDetails(t *testing.T) {
	t.Parallel()

	payload := responsesUsagePayload(completionUsage{
		PromptTokens:       9,
		CompletionTokens:   4,
		TotalTokens:        13,
		CachedPromptTokens: 3,
		CacheWriteTokens:   2,
		InputImageTokens:   1,
		ReasoningTokens:    6,
	})
	inputDetails := payload["input_tokens_details"].(map[string]any)
	outputDetails := payload["output_tokens_details"].(map[string]any)
	if inputDetails["cached_tokens"] != 3 || inputDetails["cache_write_tokens"] != 2 || inputDetails["image_tokens"] != 1 {
		t.Fatalf("input token details = %#v", inputDetails)
	}
	if outputDetails["reasoning_tokens"] != 6 {
		t.Fatalf("output token details = %#v", outputDetails)
	}
}

func TestResponsesUsageNormalizationPreservesCompletionReasoningTokens(t *testing.T) {
	t.Parallel()

	normalized, changed, err := normalizeResponsesJSONUsage([]byte(`{
		"usage": {
			"input_tokens": 10,
			"output_tokens": 4,
			"total_tokens": 14,
			"completion_tokens_details": {"reasoning_tokens": 3}
		}
	}`))
	if err != nil {
		t.Fatalf("normalizeResponsesJSONUsage returned error: %v", err)
	}
	if !changed {
		t.Fatal("expected usage details to be normalized")
	}

	var root map[string]any
	if err := json.Unmarshal(normalized, &root); err != nil {
		t.Fatalf("normalized body is invalid JSON: %v", err)
	}
	usage := root["usage"].(map[string]any)
	outputDetails := usage["output_tokens_details"].(map[string]any)
	if outputDetails["reasoning_tokens"] != float64(3) {
		t.Fatalf("reasoning_tokens = %#v, want 3", outputDetails["reasoning_tokens"])
	}
}

func responseCompletedPayloadFromStream(t *testing.T, body string) map[string]any {
	t.Helper()

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if !strings.Contains(data, `"response.completed"`) {
			continue
		}
		var root map[string]any
		if err := json.Unmarshal([]byte(data), &root); err != nil {
			t.Fatalf("completed event is invalid JSON: %v", err)
		}
		return root
	}
	t.Fatalf("response.completed data not found in %q", body)
	return nil
}
