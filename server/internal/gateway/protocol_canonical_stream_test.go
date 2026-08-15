package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	routeengine "xlyra/server/internal/router"
)

func TestStreamProtocolSpecsRegisterCurrentProtocols(t *testing.T) {
	t.Parallel()

	specs := streamProtocolSpecs()
	for _, protocol := range []canonicalProtocol{
		canonicalProtocolOpenAIChat,
		canonicalProtocolOpenAIResponses,
		canonicalProtocolCodexResponses,
		canonicalProtocolAntigravity,
		canonicalProtocolAnthropicMessages,
	} {
		spec, ok := specs[protocol]
		if !ok {
			t.Fatalf("missing stream spec for %s", protocol)
		}
		if spec.NewDecoder == nil {
			t.Fatalf("%s must register stream decoder", protocol)
		}
	}
	for _, protocol := range []canonicalProtocol{
		canonicalProtocolOpenAIChat,
		canonicalProtocolOpenAIResponses,
		canonicalProtocolCodexResponses,
		canonicalProtocolAnthropicMessages,
	} {
		if specs[protocol].NewEncoder == nil {
			t.Fatalf("%s must register stream encoder", protocol)
		}
	}
}

func TestProxyCanonicalStreamChatToResponses(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_123\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_123\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"+
			"data: [DONE]\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	body := rec.Body.String()
	assertGatewayBodyContainsAll(t, body, "response.output_text.delta", `"delta":"hi"`, "response.completed")
}

func TestProxyCanonicalStreamChatToolCallsToResponsesKeepsIndexState(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_tool\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_tool\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_abc\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"\"}}]}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_tool\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"q\\\"\"}}]}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_tool\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\":\\\"x\\\"}\"}}]}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_tool\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n"+
			"data: [DONE]\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	if capture.usage.TotalTokens != 5 {
		t.Fatalf("expected late usage to be captured, got %+v", capture.usage)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(),
		"response.output_item.added",
		`"type":"function_call"`,
		`"call_id":"call_abc"`,
		`"name":"lookup"`,
		"response.function_call_arguments.delta",
		`"arguments":"{\"q\":\"x\"}"`,
		"response.output_item.done",
	)
}

func TestProxyCanonicalStreamChatToolNameArrivesAfterArguments(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		strings.Join([]string{
			`data: {"id":"chatcmpl_late_name","created":1710000000,"model":"gpt-5.4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_late_name","type":"function","function":{"arguments":"{\"input\":\"pa"}}]}}]}`,
			`data: {"id":"chatcmpl_late_name","created":1710000000,"model":"gpt-5.4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"apply_patch","arguments":"tch\"}"}}]}}]}`,
			"data: [DONE]",
		}, "\n\n")+"\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses,
		canonicalStreamOptions{CustomTools: map[string]struct{}{"apply_patch": {}}})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	body := rec.Body.String()
	assertGatewayBodyContainsAll(t, body, `"id":"ctc_call_late_name"`, `"type":"custom_tool_call"`, `"input":"patch"`)
	if strings.Contains(body, `"id":"fc_call_late_name"`) || strings.Contains(body, "response.function_call_arguments.delta") {
		t.Fatalf("custom tool stream leaked a function-call ID or event: %s", body)
	}
}

func TestProxyCanonicalStreamChatToolCallsToResponsesKeepsParallelArgumentsSeparate(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_parallel\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_a\",\"type\":\"function\",\"function\":{\"name\":\"a\",\"arguments\":\"\"}},{\"index\":1,\"id\":\"call_b\",\"type\":\"function\",\"function\":{\"name\":\"b\",\"arguments\":\"\"}}]}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_parallel\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":1,\"function\":{\"arguments\":\"{\\\"b\\\"\"}},{\"index\":0,\"function\":{\"arguments\":\"{\\\"a\\\"\"}}]}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_parallel\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\":1}\"}},{\"index\":1,\"function\":{\"arguments\":\":2}\"}}]}}]}\n\n"+
			"data: [DONE]\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(),
		`"call_id":"call_a"`,
		`"arguments":"{\"a\":1}"`,
		`"call_id":"call_b"`,
		`"arguments":"{\"b\":2}"`,
	)
}

func TestProxyCanonicalStreamInvalidToolCallArgumentsFails(t *testing.T) {
	t.Parallel()

	_, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_bad_args\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_bad\",\"type\":\"function\",\"function\":{\"name\":\"bad\",\"arguments\":\"{\\\"broken\\\"\"}}]}}]}\n\n"+
			"data: [DONE]\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err == nil {
		t.Fatal("expected invalid tool call arguments to fail")
	}
	if !started || capture.endReason != "tool_call_arguments_invalid_json" {
		t.Fatalf("expected invalid-json end reason, started=%v capture=%+v err=%v", started, capture, err)
	}
}

func TestProxyCanonicalStreamChatLengthFinishReasonBecomesResponsesIncomplete(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_length\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_length\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"length\"}]}\n\n"+
			"data: [DONE]\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || capture.streamCompleted || capture.endReason != "response_incomplete" {
		t.Fatalf("expected incomplete Responses stream, started=%v capture=%+v", started, capture)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "response.incomplete") || strings.Contains(body, "response.completed") {
		t.Fatalf("expected response.incomplete without completed, got %q", body)
	}
}

func TestProxyCanonicalStreamResponsesFunctionCallArgumentsDoneToChat(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_args_done\",\"created_at\":1710000000,\"model\":\"gpt-5.4\"}}\n\n"+
			"data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"\"}}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.done\",\"item_id\":\"fc_1\",\"arguments\":\"{\\\"x\\\":1}\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_args_done\",\"created_at\":1710000000,\"model\":\"gpt-5.4\"}}\n\n",
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), `"tool_calls"`, `"name":"lookup"`, `"arguments":"{\"x\":1}"`, `"finish_reason":"tool_calls"`)
}

func TestProxyCanonicalStreamResponsesRefusalAndAnnotationToChat(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_refusal\",\"created_at\":1710000000,\"model\":\"gpt-5.4\"}}\n\n"+
			"data: {\"type\":\"response.output_text.annotation.added\",\"annotation\":{\"type\":\"url_citation\",\"url\":\"https://example.com\"}}\n\n"+
			"data: {\"type\":\"response.refusal.delta\",\"delta\":\"I cannot help with that.\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_refusal\",\"created_at\":1710000000,\"model\":\"gpt-5.4\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n",
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{IncludeUsage: true})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	if len(capture.annotations) != 1 {
		t.Fatalf("expected one captured annotation, got %#v", capture.annotations)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"content":"I cannot help with that."`) {
		t.Fatalf("expected refusal to be downgraded to chat content, got %q", body)
	}
	if strings.Contains(body, "url_citation") {
		t.Fatalf("annotation should not be emitted as chat content, got %q", body)
	}
}

func TestProxyCanonicalStreamCodexEncryptedReasoningDoesNotLeakToChat(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_codex\",\"created_at\":1710000000,\"model\":\"gpt-5.3-codex\"}}\n\n"+
			"data: {\"type\":\"response.reasoning.encrypted_content.delta\",\"delta\":\"secret-reasoning\"}\n\n"+
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"visible\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_codex\",\"created_at\":1710000000,\"model\":\"gpt-5.3-codex\"}}\n\n",
		canonicalProtocolCodexResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	if len(capture.annotations) != 1 {
		t.Fatalf("expected encrypted reasoning annotation, got %#v", capture.annotations)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret-reasoning") || !strings.Contains(body, `"content":"visible"`) {
		t.Fatalf("expected visible text without encrypted reasoning leak, got %q", body)
	}
}

func TestProxyCanonicalStreamCodexEncryptedReasoningPassthroughToResponses(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_codex\",\"created_at\":1710000000,\"model\":\"gpt-5.3-codex\"}}\n\n"+
			"data: {\"type\":\"response.reasoning.encrypted_content.delta\",\"delta\":\"encrypted-payload\"}\n\n"+
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"visible\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_codex\",\"created_at\":1710000000,\"model\":\"gpt-5.3-codex\"}}\n\n",
		canonicalProtocolCodexResponses, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed Codex stream, started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), "response.reasoning.encrypted_content.delta", "encrypted-payload")
}

func TestProxyCanonicalStreamResponsesAdvancedToolResultToChatMetadata(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_tool_result\",\"created_at\":1710000000,\"model\":\"gpt-5.4\"}}\n\n"+
			"data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"out_1\",\"type\":\"function_call_output\",\"call_id\":\"call_1\",\"output\":\"tool-secret\",\"api_key\":\"sk-test\"}}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_tool_result\",\"created_at\":1710000000,\"model\":\"gpt-5.4\"}}\n\n",
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	if len(capture.annotations) != 1 {
		t.Fatalf("expected advanced tool-result annotation, got %#v", capture.annotations)
	}
	annotation, ok := capture.annotations[0].(map[string]any)
	if !ok || annotation["kind"] != "tool_result" || annotation["phase"] != "started" || annotation["call_id"] != "call_1" {
		t.Fatalf("unexpected advanced annotation: %#v", capture.annotations[0])
	}
	payload, ok := annotation["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected sanitized payload, got %#v", annotation)
	}
	item, ok := payload["item"].(map[string]any)
	if !ok || item["api_key"] != "[redacted]" {
		t.Fatalf("expected sensitive tool payload to be redacted, got %#v", payload)
	}
	if body := rec.Body.String(); strings.Contains(body, "tool-secret") || strings.Contains(body, "sk-test") {
		t.Fatalf("advanced tool result must not leak into chat stream, got %q", body)
	}
}

func TestProxyCanonicalStreamResponsesAdvancedMCPApprovalToChatMetadata(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.mcp_call.in_progress\",\"item_id\":\"mcp_1\",\"server_label\":\"github\",\"arguments\":{\"token\":\"secret-token\"}}\n\n"+
			"data: {\"type\":\"response.approval_request.created\",\"item_id\":\"approval_1\",\"reason\":\"needs access\",\"authorization\":\"Bearer secret\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_mcp\",\"created_at\":1710000000,\"model\":\"gpt-5.4\"}}\n\n",
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	if len(capture.annotations) != 2 {
		t.Fatalf("expected MCP and approval annotations, got %#v", capture.annotations)
	}
	first, ok := capture.annotations[0].(map[string]any)
	if !ok || first["kind"] != "mcp" || first["phase"] != "delta" {
		t.Fatalf("expected MCP annotation, got %#v", capture.annotations[0])
	}
	second, ok := capture.annotations[1].(map[string]any)
	if !ok || second["kind"] != "approval" || second["phase"] != "started" {
		t.Fatalf("expected approval annotation, got %#v", capture.annotations[1])
	}
	if body := rec.Body.String(); strings.Contains(body, "secret-token") || strings.Contains(body, "Bearer secret") {
		t.Fatalf("advanced MCP/approval payload must not leak into chat stream, got %q", body)
	}
}

func TestProxyCanonicalStreamCodexAdvancedImagePartialPassthroughToResponses(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_image\",\"created_at\":1710000000,\"model\":\"gpt-5.3-codex\"}}\n\n"+
			"data: {\"type\":\"response.image_generation_call.partial_image\",\"item_id\":\"img_1\",\"partial_image_b64\":\"abc123\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_image\",\"created_at\":1710000000,\"model\":\"gpt-5.3-codex\"}}\n\n",
		canonicalProtocolCodexResponses, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), "response.image_generation_call.partial_image", "abc123")
}

func TestProxyCanonicalStreamResponsesAdvancedShellEventSanitizesMetadata(t *testing.T) {
	t.Parallel()

	longStdout := strings.Repeat("x", 5000)
	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.shell_command.stdout_delta\",\"item_id\":\"cmd_1\",\"stdout\":\""+longStdout+"\",\"secret\":\"do-not-store\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_shell\",\"created_at\":1710000000,\"model\":\"gpt-5.4\"}}\n\n",
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	if len(capture.annotations) != 1 {
		t.Fatalf("expected shell annotation, got %#v", capture.annotations)
	}
	annotation, ok := capture.annotations[0].(map[string]any)
	if !ok || annotation["kind"] != "shell" || annotation["phase"] != "delta" {
		t.Fatalf("expected shell annotation, got %#v", capture.annotations[0])
	}
	payload, ok := annotation["payload"].(map[string]any)
	if !ok || payload["secret"] != "[redacted]" {
		t.Fatalf("expected sensitive shell payload to be redacted, got %#v", annotation)
	}
	stdout, ok := payload["stdout"].(string)
	if !ok || !strings.Contains(stdout, "[truncated]") || len(stdout) >= len(longStdout) {
		t.Fatalf("expected shell stdout to be truncated, got len=%d payload=%#v", len(stdout), payload)
	}
	if body := rec.Body.String(); strings.Contains(body, "do-not-store") || strings.Contains(body, longStdout[:128]) {
		t.Fatalf("advanced shell payload must not leak into chat stream, got %q", body)
	}
}

func TestProxyCanonicalStreamResponsesIncompleteToChat(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_incomplete\",\"created_at\":1710000000,\"model\":\"gpt-5.4\"}}\n\n"+
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"+
			"data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_incomplete\",\"created_at\":1710000000,\"model\":\"gpt-5.4\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n",
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || capture.streamCompleted || capture.endReason != "response_incomplete" {
		t.Fatalf("expected incomplete stream, started=%v capture=%+v", started, capture)
	}
	if !strings.Contains(rec.Body.String(), `"finish_reason":"length"`) {
		t.Fatalf("expected chat length finish reason, got %q", rec.Body.String())
	}
}

func TestProxyResponsesStreamPassthroughCapturesIncompleteErrorAndAnnotations(t *testing.T) {
	t.Parallel()

	_, incompleteCapture, started, err := proxyResponsesStreamPassthroughTest(t,
		"data: {\"type\":\"response.output_text.annotation.added\",\"annotation\":{\"type\":\"url_citation\",\"url\":\"https://example.com\"}}\n\n"+
			"data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_passthrough\",\"created_at\":1710000000,\"model\":\"gpt-5.4\",\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n")
	if err != nil {
		t.Fatalf("proxyResponsesStreamPassthrough returned error: %v", err)
	}
	if !started || incompleteCapture.streamCompleted || incompleteCapture.endReason != "response_incomplete" {
		t.Fatalf("expected passthrough incomplete, started=%v capture=%+v", started, incompleteCapture)
	}
	if len(incompleteCapture.annotations) != 1 || incompleteCapture.usage.TotalTokens != 5 {
		t.Fatalf("expected annotation and usage capture, got %+v", incompleteCapture)
	}

	errorRec, errorCapture, started, err := proxyResponsesStreamPassthroughTest(t,
		"data: {\"type\":\"response.error\",\"error\":{\"code\":\"bad_request\",\"message\":\"bad stream\"}}\n\n")
	if err != nil {
		t.Fatalf("proxyResponsesStreamPassthrough returned error: %v", err)
	}
	if started || errorCapture.streamCompleted || errorCapture.endReason != "upstream_stream_error" {
		t.Fatalf("expected passthrough upstream_stream_error, started=%v capture=%+v", started, errorCapture)
	}
	if errorRec.Body.Len() != 0 {
		t.Fatalf("pre-output error leaked downstream: %q", errorRec.Body.String())
	}
}

func TestResponsesStreamPreOutputDispositionForLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
		want responsesPreOutputDisposition
	}{
		{name: "event field", line: "event: response.created\n", want: responsesPreOutputDefer},
		{name: "created", line: "data: {\"type\":\"response.created\"}\n", want: responsesPreOutputDefer},
		{name: "in progress", line: "data: {\"type\":\"response.in_progress\"}\n", want: responsesPreOutputDefer},
		{name: "queued", line: "data: {\"type\":\"response.queued\"}\n", want: responsesPreOutputDefer},
		{name: "keepalive", line: "data: {\"type\":\"keepalive\"}\n", want: responsesPreOutputDefer},
		{name: "ping", line: "data: {\"type\":\"ping\"}\n", want: responsesPreOutputDefer},
		{name: "empty message item", line: "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\",\"content\":[]}}\n", want: responsesPreOutputDefer},
		{name: "empty content part", line: "data: {\"type\":\"response.content_part.added\",\"part\":{\"type\":\"output_text\",\"text\":\"\"}}\n", want: responsesPreOutputDefer},
		{name: "function item without arguments", line: "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"name\":\"lookup\",\"arguments\":\"\"}}\n", want: responsesPreOutputDefer},
		{name: "empty image item", line: "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"in_progress\"}}\n", want: responsesPreOutputDefer},
		{name: "image item with result", line: "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"status\":\"completed\",\"result\":\"aW1hZ2U=\"}}\n", want: responsesPreOutputCommit},
		{name: "empty text delta", line: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"\"}\n", want: responsesPreOutputDefer},
		{name: "message item with text", line: "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}}\n", want: responsesPreOutputCommit},
		{name: "function item with arguments", line: "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"arguments\":\"{}\"}}\n", want: responsesPreOutputCommit},
		{name: "unknown advanced event", line: "data: {\"type\":\"response.approval_request.created\",\"approval_id\":\"approval_1\"}\n", want: responsesPreOutputCommit},
		{name: "failed", line: "data: {\"type\":\"response.failed\",\"error\":{\"code\":\"server_is_overloaded\"}}\n", want: responsesPreOutputFail},
		{name: "error", line: "data: {\"type\":\"response.error\",\"error\":{\"code\":\"upstream_error\"}}\n", want: responsesPreOutputFail},
		{name: "generic error", line: "data: {\"type\":\"error\",\"error\":{\"code\":\"upstream_error\"}}\n", want: responsesPreOutputFail},
		{name: "business output", line: "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n", want: responsesPreOutputCommit},
		{name: "completed", line: "data: {\"type\":\"response.completed\"}\n", want: responsesPreOutputCommit},
		{name: "done", line: "data: [DONE]\n", want: responsesPreOutputCommit},
		{name: "malformed data", line: "data: {invalid\n", want: responsesPreOutputCommit},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, _ := responsesStreamPreOutputDispositionForLine([]byte(test.line))
			if got != test.want {
				t.Fatalf("disposition = %d, want %d", got, test.want)
			}
		})
	}
}

func TestProxyResponsesStreamPassthroughDefersEmptyOutputContainersForFailover(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyResponsesStreamPassthroughTest(t,
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_empty_item"}}`)+
			gatewaySSEEvent("response.output_item.added", `{"type":"response.output_item.added","item":{"id":"msg_empty","type":"message","status":"in_progress","content":[]}}`)+
			gatewaySSEEvent("response.content_part.added", `{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`)+
			gatewaySSEEvent("response.failed", `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_is_overloaded","message":"try again later"}}}`),
	)
	if err != nil {
		t.Fatalf("proxyResponsesStreamPassthrough returned error: %v", err)
	}
	if started || capture.endReason != "upstream_stream_error" || capture.semanticFailure == nil {
		t.Fatalf("expected empty containers to preserve failover, started=%v capture=%+v", started, capture)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("empty output containers leaked downstream: %q", rec.Body.String())
	}
}

func TestProxyResponsesStreamPassthroughRejectsOversizedPreOutputWithoutStartingResponse(t *testing.T) {
	t.Parallel()

	instructions := strings.Repeat("x", 1<<20)
	var body strings.Builder
	for body.Len() <= responsesPreOutputMaxBytes {
		body.WriteString(gatewaySSEEvent("response.created", `{"type":"response.created","response":{"instructions":"`+instructions+`"}}`))
	}
	rec, capture, started, err := proxyResponsesStreamPassthroughTest(t, body.String())
	if !errors.Is(err, errResponsesPreOutputTooLarge) {
		t.Fatalf("error = %v, want pre-output size failure", err)
	}
	if started || capture.endReason != "upstream_stream_preoutput_too_large" {
		t.Fatalf("oversized pre-output started=%v capture=%+v", started, capture)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("oversized pre-output leaked downstream: %d bytes", rec.Body.Len())
	}
}

func TestProxyResponsesStreamPassthroughDefersPreOutputOverloadForFailover(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyResponsesStreamPassthroughTest(t,
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_overloaded"}}`)+
			gatewaySSEEvent("response.in_progress", `{"type":"response.in_progress","response":{"id":"resp_overloaded"}}`)+
			gatewaySSEEvent("response.failed", `{"type":"response.failed","response":{"id":"resp_overloaded","status":"failed","error":{"code":"server_is_overloaded","message":"try again later"}}}`),
	)
	if err != nil {
		t.Fatalf("proxyResponsesStreamPassthrough returned error: %v", err)
	}
	if started || capture.endReason != "upstream_stream_error" {
		t.Fatalf("expected unstarted upstream stream error, started=%v capture=%+v", started, capture)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("pre-output overload leaked downstream: %q", rec.Body.String())
	}
}

func TestProxyResponsesStreamPassthroughDefersOverloadAfterEmptyOutputItem(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyResponsesStreamPassthroughTest(t,
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_overloaded"}}`)+
			gatewaySSEEvent("response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_overloaded","type":"message","role":"assistant","status":"in_progress","content":[]}}`)+
			gatewaySSEEvent("response.failed", `{"type":"response.failed","response":{"id":"resp_overloaded","status":"failed","error":{"code":"server_is_overloaded","message":"try again later"}}}`),
	)
	if err != nil {
		t.Fatalf("proxyResponsesStreamPassthrough returned error: %v", err)
	}
	if started || capture.endReason != "upstream_stream_error" || capture.semanticFailure == nil {
		t.Fatalf("expected unstarted semantic failure, started=%v capture=%+v", started, capture)
	}
	if capture.preOutputEventsBuffered != 2 {
		t.Fatalf("buffered pre-output events = %d, want 2", capture.preOutputEventsBuffered)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("empty output item leaked downstream: %q", rec.Body.String())
	}
}

func TestProxyResponsesStreamPassthroughDoesNotDeferOverloadAfterOutput(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyResponsesStreamPassthroughTest(t,
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_started"}}`)+
			gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_started","delta":"hello"}`)+
			gatewaySSEEvent("response.failed", `{"type":"response.failed","response":{"id":"resp_started","status":"failed","error":{"code":"server_is_overloaded","message":"try again later"}}}`),
	)
	if err != nil {
		t.Fatalf("proxyResponsesStreamPassthrough returned error: %v", err)
	}
	if !started || capture.endReason != "upstream_stream_error" {
		t.Fatalf("expected started upstream stream error, started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), "response.created", "response.output_text.delta", "server_is_overloaded")
}

func TestProxyResponsesStreamPassthroughCommitsLifecycleEventsBeforeDone(t *testing.T) {
	t.Parallel()

	body := gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_empty"}}`) +
		gatewaySSEEvent("response.in_progress", `{"type":"response.in_progress","response":{"id":"resp_empty"}}`) +
		"data: [DONE]\n\n"
	rec, capture, started, err := proxyResponsesStreamPassthroughTest(t, body)
	if err != nil {
		t.Fatalf("proxyResponsesStreamPassthrough returned error: %v", err)
	}
	if !started || !capture.streamCompleted || capture.endReason != "done" {
		t.Fatalf("expected completed lifecycle-only stream, started=%v capture=%+v", started, capture)
	}
	if rec.Body.String() != body {
		t.Fatalf("passthrough body = %q, want %q", rec.Body.String(), body)
	}
}

func TestProxyResponsesStreamPassthroughKeepsFailoverAvailableAfterDownstreamHeartbeat(t *testing.T) {
	recorder := newSynchronizedStreamRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	session := newDownstreamSSESessionWithIntervals(ctx, recorder, gatewayEndpointResponses, cancel, 10*time.Millisecond, time.Second)
	defer session.Close()
	defer cancel()

	waitForStreamBody(t, recorder, ": xlyra-keepalive\n\n")
	instructions := strings.Repeat("x", 70<<10)
	overloadedBody := gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_overloaded","instructions":"`+instructions+`"}}`) +
		gatewaySSEEvent("response.failed", `{"type":"response.failed","response":{"id":"resp_overloaded","status":"failed","error":{"code":"server_is_overloaded"}}}`)
	capture, started, err := proxyResponsesStreamPassthrough(ctx, session, gatewayStreamTestResponse(overloadedBody), time.Now())
	if err != nil {
		t.Fatalf("overloaded passthrough returned error: %v", err)
	}
	if started || capture.endReason != "upstream_stream_error" {
		t.Fatalf("expected unstarted overload after heartbeat, started=%v capture=%+v", started, capture)
	}

	fallbackBody := gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_fallback"}}`) +
		gatewaySSEEvent("response.output_text.delta", `{"type":"response.output_text.delta","item_id":"msg_fallback","delta":"fallback-ok"}`) +
		gatewaySSEEvent("response.completed", `{"type":"response.completed","response":{"id":"resp_fallback","output":[]}}`)
	capture, started, err = proxyResponsesStreamPassthrough(ctx, session, gatewayStreamTestResponse(fallbackBody), time.Now())
	if err != nil {
		t.Fatalf("fallback passthrough returned error: %v", err)
	}
	if !started || !capture.streamCompleted || capture.endReason != "done" {
		t.Fatalf("expected completed fallback after heartbeat, started=%v capture=%+v", started, capture)
	}
	session.FinishSSE()
	_, body, _ := recorder.snapshot()
	assertGatewayBodyContainsAll(t, body, "xlyra-keepalive", "resp_fallback", "fallback-ok", "response.completed")
	if strings.Contains(body, "resp_overloaded") || strings.Contains(body, "server_is_overloaded") {
		t.Fatalf("pre-output overload leaked after heartbeat: %q", body)
	}
}

func TestProxyCanonicalStreamResponsesErrorEventFails(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_error\",\"created_at\":1710000000,\"model\":\"gpt-5.4\"}}\n\n"+
			"data: {\"type\":\"response.error\",\"error\":{\"code\":\"bad_request\",\"message\":\"bad stream\"}}\n\n",
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if started || capture.endReason != "upstream_stream_error" || capture.semanticFailure == nil {
		t.Fatalf("expected unstarted semantic failure, started=%v capture=%+v", started, capture)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("pre-output semantic failure leaked converted response: %q", rec.Body.String())
	}
}

func TestProxyCanonicalStreamResponsesPreOutputOverloadDefersAcrossConversion(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_overloaded","model":"gpt-5.6-terra"}}`)+
			gatewaySSEEvent("response.in_progress", `{"type":"response.in_progress","response":{"id":"resp_overloaded","status":"in_progress"}}`)+
			gatewaySSEEvent("response.failed", `{"type":"response.failed","response":{"id":"resp_overloaded","status":"failed","error":{"code":"server_is_overloaded","message":"try again later"}}}`),
		canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if started || capture.endReason != "upstream_stream_error" {
		t.Fatalf("expected unstarted upstream overload, started=%v capture=%+v", started, capture)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("pre-output overload leaked converted response: %q", rec.Body.String())
	}
}

func TestProxyCanonicalStreamResponsesRejectsOversizedPreOutputWithoutStartingResponse(t *testing.T) {
	t.Parallel()

	identifier := strings.Repeat("x", 1<<20)
	var body strings.Builder
	for body.Len() <= responsesPreOutputMaxBytes {
		body.WriteString(gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"`+identifier+`","model":"gpt-5.6-terra"}}`))
	}
	rec, capture, started, err := proxyCanonicalStreamTest(t, body.String(), canonicalProtocolOpenAIResponses, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if !errors.Is(err, errResponsesPreOutputTooLarge) {
		t.Fatalf("error = %v, want pre-output size failure", err)
	}
	if started || capture.endReason != "upstream_stream_preoutput_too_large" {
		t.Fatalf("oversized pre-output started=%v capture=%+v", started, capture)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("oversized pre-output leaked downstream: %d bytes", rec.Body.Len())
	}
}

func TestProxyCanonicalStreamResponsesDefersEmptyFunctionCallBeforeFailure(t *testing.T) {
	t.Parallel()

	for _, target := range []canonicalProtocol{
		canonicalProtocolOpenAIChat,
		canonicalProtocolAnthropicMessages,
	} {
		target := target
		t.Run(string(target), func(t *testing.T) {
			t.Parallel()

			rec, capture, started, err := proxyCanonicalStreamTest(t,
				gatewaySSEEvent("response.created", `{"type":"response.created","response":{"id":"resp_empty_function","model":"gpt-5.6-terra"}}`)+
					gatewaySSEEvent("response.output_item.added", `{"type":"response.output_item.added","item":{"id":"fc_empty","type":"function_call","call_id":"call_empty","name":"lookup","arguments":""}}`)+
					gatewaySSEEvent("response.failed", `{"type":"response.failed","response":{"id":"resp_empty_function","status":"failed","error":{"code":"server_is_overloaded","message":"try again later"}}}`),
				canonicalProtocolOpenAIResponses, target, canonicalStreamOptions{})
			if err != nil {
				t.Fatalf("proxyCanonicalStream returned error: %v", err)
			}
			if started || capture.endReason != "upstream_stream_error" || capture.semanticFailure == nil {
				t.Fatalf("expected unstarted semantic failure, target=%s started=%v capture=%+v", target, started, capture)
			}
			if rec.Body.Len() != 0 {
				t.Fatalf("empty function call leaked converted response for %s: %q", target, rec.Body.String())
			}
		})
	}
}

func TestProxyCanonicalStreamAntigravityToolCallToResponses(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"id\":\"ag_call\",\"name\":\"lookup\",\"args\":{\"city\":\"Tokyo\"}}}]}}]}}\n\n",
		canonicalProtocolAntigravity, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed Antigravity tool stream, started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), `"type":"function_call"`, `"call_id":"ag_call"`, `"name":"lookup"`, `"arguments":"{\"city\":\"Tokyo\"}"`)
}

func TestProxyCanonicalStreamAntigravityChunkedTextualToolUseToMessages(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"[{\\\"id\\\":\\\"call_\"}]}}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":1,\"totalTokenCount\":11}}}\n\n"+
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"abc123\"}]}}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":2,\"totalTokenCount\":12}}}\n\n"+
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"\\\",\\\"input\\\":{\\\"status\\\":\\\"completed\\\",\\\"taskId\\\":\\\"2\\\"},\\\"name\\\":\\\"TaskUpdate\\\",\\\"type\\\":\\\"tool_use\"}]}}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":8,\"totalTokenCount\":18}}}\n\n"+
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"\\\"}]\"}]}}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":9,\"totalTokenCount\":19}}}\n\n"+
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":9,\"totalTokenCount\":19}}}\n\n",
		canonicalProtocolAntigravity, canonicalProtocolAnthropicMessages, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected completed Antigravity stream, started=%v capture=%+v", started, capture)
	}
	body := rec.Body.String()
	assertGatewayBodyContainsAll(t, body, `"type":"tool_use"`, `"id":"call_abc123"`, `"name":"TaskUpdate"`, `"partial_json":"{\"status\":\"completed\",\"taskId\":\"2\"}"`, `"stop_reason":"tool_use"`)
	if strings.Contains(body, `[{\"id\"`) || strings.Contains(body, `call_abc123\",\"input`) {
		t.Fatalf("raw textual tool JSON should not be emitted as text, got %q", body)
	}
}

func TestProxyCanonicalStreamAntigravityErrorAndMaxTokens(t *testing.T) {
	t.Parallel()

	_, errorCapture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"error\":{\"code\":\"permission_denied\",\"message\":\"blocked\"}}\n\n",
		canonicalProtocolAntigravity, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err == nil {
		t.Fatal("expected Antigravity error event to fail")
	}
	if !started || errorCapture.endReason != "upstream_stream_error" {
		t.Fatalf("expected upstream_stream_error, started=%v capture=%+v err=%v", started, errorCapture, err)
	}

	lengthRec, lengthCapture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"response\":{\"candidates\":[{\"finishReason\":\"MAX_TOKENS\",\"content\":{\"parts\":[{\"text\":\"partial\"}]}}]}}\n\n",
		canonicalProtocolAntigravity, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || lengthCapture.streamCompleted || lengthCapture.endReason != "response_incomplete" {
		t.Fatalf("expected Antigravity incomplete length finish, started=%v capture=%+v", started, lengthCapture)
	}
	if !strings.Contains(lengthRec.Body.String(), `"finish_reason":"length"`) {
		t.Fatalf("expected chat length finish reason, got %q", lengthRec.Body.String())
	}
}

func TestProxyCanonicalStreamOpenAIChatEOFIsIncomplete(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_123\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || capture.streamCompleted || capture.endReason != "upstream_stream_incomplete" {
		t.Fatalf("expected incomplete OpenAI stream, started=%v capture=%+v", started, capture)
	}
	if strings.Contains(rec.Body.String(), "response.completed") {
		t.Fatalf("unexpected synthesized completion for OpenAI EOF: %q", rec.Body.String())
	}
}

func TestProxyCanonicalStreamAntigravityToChatCompletesOnEOF(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":2,\"totalTokenCount\":3}}}\n\n",
		canonicalProtocolAntigravity, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted || !capture.sawDone {
		t.Fatalf("expected EOF completion, started=%v capture=%+v", started, capture)
	}
	assertGatewayBodyContainsAll(t, rec.Body.String(), `"content":"hi"`, "data: [DONE]")
}

func TestProxyCanonicalStreamAntigravityUsesStableResponseID(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hi\"}]}}]}}\n\n"+
			"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\" there\"}]}}]}}\n\n",
		canonicalProtocolAntigravity, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted {
		t.Fatalf("expected EOF completion, started=%v capture=%+v", started, capture)
	}
	ids := []string{}
	for _, part := range strings.Split(rec.Body.String(), "\n\n") {
		line := strings.TrimSpace(part)
		if !strings.HasPrefix(line, "data: {") || !strings.Contains(line, `"content"`) {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var chunk struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			t.Fatalf("decode chat chunk: %v", err)
		}
		ids = append(ids, chunk.ID)
	}
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("expected stable response id across text chunks, got %#v; body=%q", ids, rec.Body.String())
	}
}

func TestReasoningPolicyForProtocol_UsesCandidate(t *testing.T) {
	t.Parallel()

	// With empty candidate, falls back to protocol-level policy.
	emptyCandidate := routeengine.Candidate{}
	policy := reasoningPolicyForProtocol(canonicalProtocolOpenAIChat, "encrypted_reasoning", emptyCandidate)
	if policy != "strip" {
		t.Fatalf("OpenAI Chat encrypted_reasoning policy = %q, want strip", policy)
	}

	// With Codex provider candidate, should get provider-level reasoning policy.
	codexCandidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "codex"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-5.4-codex"},
	}
	codexPolicy := reasoningPolicyForProtocol(canonicalProtocolCodexResponses, "encrypted_reasoning", codexCandidate)
	if codexPolicy != "passthrough" {
		t.Fatalf("Codex encrypted_reasoning policy = %q, want passthrough", codexPolicy)
	}

	// Anthropic protocol defaults
	anthropicPolicy := reasoningPolicyForProtocol(canonicalProtocolAnthropicMessages, "thinking", emptyCandidate)
	if anthropicPolicy != "passthrough" {
		t.Fatalf("Anthropic Messages thinking policy = %q, want passthrough", anthropicPolicy)
	}

	// Anthropic with actual anthropic site
	anthropicCandidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "anthropic"},
		Model: routeengine.CandidateModel{UpstreamName: "claude-sonnet-4-20250514"},
	}
	anthropicSitePolicy := reasoningPolicyForProtocol(canonicalProtocolAnthropicMessages, "thinking", anthropicCandidate)
	if anthropicSitePolicy != "passthrough" {
		t.Fatalf("Anthropic site thinking policy = %q, want passthrough", anthropicSitePolicy)
	}

	// OpenAI Responses protocol
	openaiCandidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{SiteType: "openai"},
		Model: routeengine.CandidateModel{UpstreamName: "gpt-4.1"},
	}
	responsesPolicy := reasoningPolicyForProtocol(canonicalProtocolOpenAIResponses, "encrypted_reasoning", openaiCandidate)
	if responsesPolicy != "passthrough" {
		t.Fatalf("OpenAI Responses encrypted_reasoning policy = %q, want passthrough", responsesPolicy)
	}
}

// Real OpenAI chat/completions streams emit finish_reason and the usage chunk in two
// separate frames (finish_reason chunk -> usage-only chunk -> [DONE]). When the
// downstream protocol is /v1/responses, the response.completed event must carry the
// usage that arrived AFTER finish_reason, not the zero-value snapshot taken at
// finish_reason time. Regression guard for that bug.
func TestProxyCanonicalStreamChatToResponsesEmitsUsageWithCompleted(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_usage\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_usage\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
			"data: {\"id\":\"chatcmpl_usage\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":11,\"total_tokens\":18}}\n\n"+
			"data: [DONE]\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted || capture.endReason != "done" {
		t.Fatalf("expected completed stream, started=%v capture=%+v", started, capture)
	}
	if capture.usage.TotalTokens != 18 || capture.usage.PromptTokens != 7 || capture.usage.CompletionTokens != 11 {
		t.Fatalf("capture.usage missing tokens: %+v", capture.usage)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "response.completed") {
		t.Fatalf("expected response.completed event, body=%q", body)
	}
	completedIdx := strings.LastIndex(body, "event: response.completed")
	if completedIdx < 0 {
		t.Fatalf("response.completed SSE frame not found, body=%q", body)
	}
	completedTail := body[completedIdx:]
	for _, want := range []string{`"input_tokens":7`, `"output_tokens":11`, `"total_tokens":18`} {
		if !strings.Contains(completedTail, want) {
			t.Fatalf("response.completed missing %s, tail=%q", want, completedTail)
		}
	}
}

// Some upstreams pack finish_reason and usage into the same chunk before [DONE].
// The downstream response.completed must still carry the usage; verify the decoder
// reorders correctly when both arrive together.
func TestProxyCanonicalStreamChatToResponsesUsageInFinishChunk(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_combined\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_combined\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":6,\"total_tokens\":10}}\n\n"+
			"data: [DONE]\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted || capture.usage.TotalTokens != 10 {
		t.Fatalf("unexpected capture: started=%v capture=%+v", started, capture)
	}
	body := rec.Body.String()
	completedIdx := strings.LastIndex(body, "event: response.completed")
	if completedIdx < 0 {
		t.Fatalf("response.completed SSE frame not found, body=%q", body)
	}
	completedTail := body[completedIdx:]
	for _, want := range []string{`"input_tokens":4`, `"output_tokens":6`, `"total_tokens":10`} {
		if !strings.Contains(completedTail, want) {
			t.Fatalf("response.completed missing %s, tail=%q", want, completedTail)
		}
	}
}

// When the upstream sends finish_reason but no [DONE] before EOF, the decoder's
// Flush() must still drive a terminal event so the downstream sees response.completed.
func TestProxyCanonicalStreamChatToResponsesFlushesPendingTerminalOnEOF(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_eof\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n"+
			"data: {\"id\":\"chatcmpl_eof\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started || !capture.streamCompleted || capture.endReason != "done" {
		t.Fatalf("expected completed stream via Flush, started=%v capture=%+v", started, capture)
	}
	if !strings.Contains(rec.Body.String(), "response.completed") {
		t.Fatalf("expected response.completed via EOF flush, body=%q", rec.Body.String())
	}
}

// Anthropic reports input/cache tokens only in message_start.usage and output
// tokens only in message_delta.usage. When such a stream is converted to a
// different downstream protocol the decoder emits two usage events; a naive
// last-writer-wins in the encoder would let the output-only delta wipe the
// prompt/cache tokens, undercounting billing. The decoder must accumulate so
// the final captured usage carries both sides. (F2 regression.)
func TestProxyCanonicalStreamAnthropicSplitUsageAccumulatesPromptAndCompletion(t *testing.T) {
	t.Parallel()

	body := gatewaySSEEvent("message_start", `{"type":"message_start","message":{"id":"msg_split","model":"claude-x","usage":{"input_tokens":100,"cache_read_input_tokens":20,"cache_creation_input_tokens":5,"output_tokens":1}}}`) +
		gatewaySSEEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`) +
		gatewaySSEEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`) +
		gatewaySSEEvent("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		gatewaySSEEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":50}}`) +
		gatewaySSEEvent("message_stop", `{"type":"message_stop"}`)

	_, capture, started, err := proxyCanonicalStreamTest(t, body,
		canonicalProtocolAnthropicMessages, canonicalProtocolOpenAIChat, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("proxyCanonicalStream returned error: %v", err)
	}
	if !started {
		t.Fatalf("expected stream to start, capture=%+v", capture)
	}
	if capture.usage.PromptTokens != 125 {
		t.Fatalf("prompt tokens undercounted: got %d want 125 (usage=%+v)", capture.usage.PromptTokens, capture.usage)
	}
	if capture.usage.CompletionTokens != 50 {
		t.Fatalf("completion tokens wrong: got %d want 50 (usage=%+v)", capture.usage.CompletionTokens, capture.usage)
	}
	if capture.usage.TotalTokens != 175 {
		t.Fatalf("total tokens wrong: got %d want 175 (usage=%+v)", capture.usage.TotalTokens, capture.usage)
	}
	if capture.usage.CachedPromptTokens != 20 {
		t.Fatalf("cached prompt tokens wrong: got %d want 20 (usage=%+v)", capture.usage.CachedPromptTokens, capture.usage)
	}
	if capture.usage.CacheCreationInputTokens != 5 {
		t.Fatalf("cache creation tokens wrong: got %d want 5 (usage=%+v)", capture.usage.CacheCreationInputTokens, capture.usage)
	}
}

// A malformed/truncated line after content is already streaming must be skipped,
// not abort the whole stream and discard everything already sent. (F13 regression.)
func TestProxyCanonicalStreamSkipsMalformedLineAfterHeaders(t *testing.T) {
	t.Parallel()

	rec, capture, started, err := proxyCanonicalStreamTest(t,
		"data: {\"id\":\"chatcmpl_skip\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n"+
			"data: {broken json not closed\n\n"+
			"data: {\"id\":\"chatcmpl_skip\",\"created\":1710000000,\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
			"data: [DONE]\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err != nil {
		t.Fatalf("malformed line should not fail the stream, err=%v", err)
	}
	if !started || !capture.streamCompleted || capture.endReason != "done" {
		t.Fatalf("stream should complete gracefully, started=%v capture=%+v", started, capture)
	}
	if capture.malformedLines != 1 {
		t.Fatalf("expected 1 skipped malformed line, got %d", capture.malformedLines)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "response.completed") {
		t.Fatalf("expected stream to finish with response.completed, body=%q", body)
	}
	if !strings.Contains(body, "Hi") {
		t.Fatalf("content sent before the malformed line should be preserved, body=%q", body)
	}
}

// Before any bytes are committed, a decode failure still aborts so the gateway
// can fall over cleanly rather than emit a broken/empty response. (F13.)
func TestProxyCanonicalStreamFailsMalformedLineBeforeHeaders(t *testing.T) {
	t.Parallel()

	_, capture, _, err := proxyCanonicalStreamTest(t,
		"data: {broken before anything is sent\n\n",
		canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalStreamOptions{})
	if err == nil {
		t.Fatal("expected a pre-header decode failure to surface as an error")
	}
	if capture.endReason != "upstream_stream_parse_failed" {
		t.Fatalf("expected upstream_stream_parse_failed, got %+v", capture)
	}
}
