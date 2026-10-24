package gateway

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	routeengine "xlyra/server/internal/router"
)

func TestResponsesReasoningTextCollectsKnownContentParts(t *testing.T) {
	t.Parallel()

	if got := responsesReasoningText(" private chain "); got != "private chain" {
		t.Fatalf("string reasoning text = %q, want private chain", got)
	}

	got := responsesReasoningText([]any{
		map[string]any{"type": "reasoning_text", "text": " first "},
		map[string]any{"type": "summary_text", "text": "second"},
		map[string]any{"type": "text", "text": " third "},
		map[string]any{"type": "output_text", "text": "ignored"},
		map[string]any{"type": "reasoning_text", "text": " \t\n "},
		"ignored",
	})
	if got != "first\nsecond\nthird" {
		t.Fatalf("reasoning content text = %q, want joined known parts", got)
	}

	if got := responsesReasoningText(map[string]any{"text": "ignored"}); got != "" {
		t.Fatalf("unsupported reasoning text = %q, want empty", got)
	}
}

func TestCanonicalThinkingFromResponsesItemUsesReasoningOrChatFallback(t *testing.T) {
	t.Parallel()

	blocks := canonicalThinkingFromResponsesItem(map[string]any{
		"type": "reasoning",
		"content": []any{
			map[string]any{"type": "reasoning_text", "text": "private"},
			map[string]any{"type": "summary_text", "text": "summary"},
		},
		"reasoning_content": "fallback should not win",
	})
	if len(blocks) != 1 || blocks[0].Thinking != "private\nsummary" || blocks[0].Type != "thinking" {
		t.Fatalf("responses reasoning blocks = %#v", blocks)
	}

	blocks = canonicalThinkingFromResponsesItem(map[string]any{
		"type": "reasoning",
		"summary": []any{
			map[string]any{"type": "summary_text", "text": " summary only "},
		},
	})
	if len(blocks) != 1 || blocks[0].Thinking != "summary only" {
		t.Fatalf("summary-only reasoning blocks = %#v", blocks)
	}

	blocks = canonicalThinkingFromResponsesItem(map[string]any{
		"type":               "message",
		"reasoning_content":  " chat private ",
		"thinking_signature": "sig-chat",
	})
	if len(blocks) != 1 || blocks[0].Thinking != "chat private" || blocks[0].Signature != "sig-chat" {
		t.Fatalf("chat fallback thinking blocks = %#v", blocks)
	}

	if got := canonicalThinkingFromResponsesItem(map[string]any{"type": "reasoning", "content": []any{}}); got != nil {
		t.Fatalf("empty reasoning blocks = %#v, want nil", got)
	}
}

func TestAnthropicThinkingBlocksRoundTripCanonicalMetadata(t *testing.T) {
	t.Parallel()

	raw := map[string]any{
		"type":      "thinking",
		"thinking":  " private anthropic chain ",
		"signature": "sig-anthropic",
		"extra":     "preserved",
	}
	block, ok := canonicalThinkingFromAnthropicBlock(raw)
	if !ok {
		t.Fatal("expected anthropic thinking block to parse")
	}
	if block.Type != "thinking" || block.Thinking != " private anthropic chain " || block.Signature != "sig-anthropic" {
		t.Fatalf("canonical thinking block = %#v", block)
	}
	if block.Raw["extra"] != "preserved" {
		t.Fatalf("raw block was not preserved: %#v", block.Raw)
	}

	anthropic := anthropicThinkingBlocksFromCanonical([]canonicalThinkingBlock{block, canonicalThinkingBlock{Thinking: " \t\n "}})
	if len(anthropic) != 1 {
		t.Fatalf("anthropic blocks length = %d, want 1", len(anthropic))
	}
	item, ok := anthropic[0].(map[string]any)
	if !ok {
		t.Fatalf("anthropic block type = %T, want map", anthropic[0])
	}
	if item["type"] != "thinking" || item["thinking"] != " private anthropic chain " || item["signature"] != "sig-anthropic" || item["extra"] != "preserved" {
		t.Fatalf("anthropic block = %#v", item)
	}
}

func TestCanonicalThinkingFromAnthropicBlockRejectsNonThinkingOrEmptyBlocks(t *testing.T) {
	t.Parallel()

	for _, block := range []map[string]any{
		{"type": "text", "thinking": "private"},
		{"type": "thinking", "thinking": " \t\n "},
		{"type": "thinking"},
	} {
		if got, ok := canonicalThinkingFromAnthropicBlock(block); ok {
			t.Fatalf("expected block %#v to be rejected, got %#v", block, got)
		}
	}
}

func TestReasoningContentEncodersSkipBlankBlocks(t *testing.T) {
	t.Parallel()

	blocks := []canonicalThinkingBlock{
		{Thinking: " first "},
		{Thinking: " \t\n "},
		{Thinking: "second"},
	}
	if got := chatReasoningContent(blocks); got != " first \nsecond" {
		t.Fatalf("chat reasoning content = %q, want nonblank blocks joined", got)
	}

	item := responsesReasoningItem(blocks)
	if item == nil {
		t.Fatal("expected responses reasoning item")
	}
	content, ok := item["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("responses reasoning content = %#v", item["content"])
	}
	part, _ := content[0].(map[string]any)
	if part["type"] != "reasoning_text" || part["text"] != " first \nsecond" {
		t.Fatalf("responses reasoning part = %#v", part)
	}
	summary, ok := item["summary"].([]any)
	if !ok || len(summary) != 1 {
		t.Fatalf("responses reasoning summary = %#v, want list with one summary_text", item["summary"])
	}
	summaryPart, _ := summary[0].(map[string]any)
	if summaryPart["type"] != "summary_text" || summaryPart["text"] != " first \nsecond" {
		t.Fatalf("responses reasoning summary part = %#v", summaryPart)
	}

	if got := responsesReasoningItem([]canonicalThinkingBlock{{Thinking: " "}}); got != nil {
		t.Fatalf("blank responses reasoning item = %#v, want nil", got)
	}
}

func TestResponsesReasoningItemPreservesThinkingSignature(t *testing.T) {
	t.Parallel()
	item := responsesReasoningItem([]canonicalThinkingBlock{{Thinking: "private", Signature: "sig_deepseek"}})
	if item["thinking_signature"] != "sig_deepseek" {
		t.Fatalf("thinking_signature = %#v, want sig_deepseek", item["thinking_signature"])
	}
	blocks := canonicalThinkingFromResponsesItem(item)
	if len(blocks) != 1 || blocks[0].Signature != "sig_deepseek" {
		t.Fatalf("decoded thinking blocks = %#v, want preserved signature", blocks)
	}
}

func TestThinkingSignatureRoundTripViaEncryptedContent(t *testing.T) {
	t.Parallel()

	item := responsesReasoningItem([]canonicalThinkingBlock{{Thinking: "plan", Signature: "sig-1"}})
	if item["thinking_signature"] != "sig-1" {
		t.Fatalf("thinking_signature = %#v", item["thinking_signature"])
	}
	if item["encrypted_content"] != anthropicEncryptedContentPrefix+"sig-1" {
		t.Fatalf("encrypted_content = %#v", item["encrypted_content"])
	}
	delete(item, "thinking_signature")
	blocks := canonicalThinkingFromResponsesItem(item)
	if len(blocks) != 1 || blocks[0].Thinking != "plan" || blocks[0].Signature != "sig-1" {
		t.Fatalf("signature restored from encrypted_content = %#v", blocks)
	}

	fromSignature := canonicalThinkingFromResponsesItem(map[string]any{
		"type":              "reasoning",
		"summary":           []any{map[string]any{"type": "summary_text", "text": "sum"}},
		"signature":         "from-signature",
		"encrypted_content": anthropicEncryptedContentPrefix + "from-enc",
	})
	if len(fromSignature) != 1 || fromSignature[0].Signature != "from-signature" {
		t.Fatalf("signature field should beat encrypted_content, got %#v", fromSignature)
	}
	fromField := canonicalThinkingFromResponsesItem(map[string]any{
		"type":               "reasoning",
		"summary":            []any{map[string]any{"type": "summary_text", "text": "sum"}},
		"thinking_signature": "from-field",
		"signature":          "from-signature",
		"encrypted_content":  anthropicEncryptedContentPrefix + "from-enc",
	})
	if len(fromField) != 1 || fromField[0].Signature != "from-field" {
		t.Fatalf("thinking_signature should win, got %#v", fromField)
	}

	native := canonicalThinkingFromResponsesItem(map[string]any{
		"type":              "reasoning",
		"summary":           []any{map[string]any{"type": "summary_text", "text": "gpt summary"}},
		"encrypted_content": "native-blob",
	})
	if len(native) != 1 || native[0].Thinking != "gpt summary" || native[0].Signature != "" {
		t.Fatalf("non-claude encrypted_content = %#v", native)
	}

	signatureOnly := map[string]any{
		"type":              "reasoning",
		"encrypted_content": anthropicEncryptedContentPrefix + "sig-only",
	}
	if got := canonicalThinkingFromResponsesItem(signatureOnly); got != nil {
		t.Fatalf("signature without text = %#v, want no thinking block", got)
	}
	if got := responsesItemThinkingSignature(signatureOnly); got != "sig-only" {
		t.Fatalf("signature-only encrypted_content = %q", got)
	}

	body, err := json.Marshal(map[string]any{
		"id": "resp_1",
		"output": []any{map[string]any{
			"type":              "reasoning",
			"id":                "rs_1",
			"summary":           []any{map[string]any{"type": "summary_text", "text": "plan"}},
			"encrypted_content": anthropicEncryptedContentPrefix + "sig-struct",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := decodeCanonicalResponseFromResponsesBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Output) != 1 || len(response.Output[0].Thinking) != 1 {
		t.Fatalf("decoded output = %#v", response.Output)
	}
	if response.Output[0].Thinking[0].Thinking != "plan" || response.Output[0].Thinking[0].Signature != "sig-struct" {
		t.Fatalf("decoded thinking = %#v", response.Output[0].Thinking)
	}
}

func TestThinkingSignatureAccumulatesIntoResponsesStreamEncryptedContent(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	capture := &streamCaptureState{}
	encoder := newOpenAIResponsesStreamEncoder(canonicalStreamOptions{}, rec, capture)
	for _, event := range []canonicalStreamEvent{
		{Type: canonicalStreamEventCreated, ID: "resp_test", Model: "claude-sonnet-4-5"},
		{Type: canonicalStreamEventReasoningDelta, ReasoningKind: "thinking", Delta: "plan"},
		{Type: canonicalStreamEventReasoningDelta, ReasoningKind: "thinking_signature", Delta: "sig"},
		{Type: canonicalStreamEventReasoningDelta, ReasoningKind: "thinking_signature", Delta: "-more"},
		{Type: canonicalStreamEventTextDelta, Delta: "hello"},
		{Type: canonicalStreamEventUsage, Usage: completionUsage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}},
		{Type: canonicalStreamEventCompleted},
	} {
		if err := encoder.EncodeEvent(event); err != nil {
			t.Fatalf("encode %s: %v", event.Type, err)
		}
	}

	var added, done map[string]any
	for _, frame := range strings.Split(rec.Body.String(), "\n\n") {
		data := strings.TrimSpace(frame)
		if !strings.Contains(data, "response.output_item.") {
			continue
		}
		payload := map[string]any{}
		line := data
		if idx := strings.Index(line, "data:"); idx >= 0 {
			line = strings.TrimSpace(line[idx+len("data:"):])
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("decode frame: %v\n%s", err, data)
		}
		item, _ := payload["item"].(map[string]any)
		if item["type"] != "reasoning" {
			continue
		}
		switch payload["type"] {
		case "response.output_item.added":
			added = item
		case "response.output_item.done":
			done = item
		}
	}
	if added == nil || done == nil {
		t.Fatalf("missing reasoning output items in stream: %s", rec.Body.String())
	}
	want := anthropicEncryptedContentPrefix + "sig-more"
	if added["encrypted_content"] != want || done["encrypted_content"] != want {
		t.Fatalf("stream encrypted_content added=%#v done=%#v", added["encrypted_content"], done["encrypted_content"])
	}
	var completed map[string]any
	for _, frame := range strings.Split(rec.Body.String(), "\n\n") {
		if !strings.Contains(frame, "response.completed") {
			continue
		}
		line := frame
		if idx := strings.Index(line, "data:"); idx >= 0 {
			line = strings.TrimSpace(line[idx+len("data:"):])
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("decode completed frame: %v", err)
		}
		completed, _ = payload["response"].(map[string]any)
	}
	output, _ := completed["output"].([]any)
	if len(output) == 0 || output[0].(map[string]any)["encrypted_content"] != want {
		t.Fatalf("completed output = %#v", completed["output"])
	}
	delete(done, "thinking_signature")
	blocks := canonicalThinkingFromResponsesItem(done)
	if len(blocks) != 1 || blocks[0].Signature != "sig-more" || blocks[0].Thinking != "plan" {
		t.Fatalf("stream item round trip = %#v", blocks)
	}
}

func TestUnsignedThinkingDroppedForNativeAnthropicKeptForDomestic(t *testing.T) {
	t.Parallel()

	messages := []canonicalMessage{
		{Type: "message", Role: "user", Content: []canonicalContentPart{{Type: "input_text", Text: "hi"}}},
		{Type: "message", Role: "assistant", Thinking: []canonicalThinkingBlock{{Thinking: "unsigned plan"}}, Content: []canonicalContentPart{{Type: "output_text", Text: "answer"}}},
		{Type: "message", Role: "assistant", Thinking: []canonicalThinkingBlock{{Thinking: "only thinking"}}},
		{Type: "message", Role: "assistant", Thinking: []canonicalThinkingBlock{{Thinking: "signed plan", Signature: "sig-ok"}}, Content: []canonicalContentPart{{Type: "output_text", Text: "kept"}}},
	}
	native := finishAnthropicThinkingPayload(t, "claude-sonnet-4-5", messages, false)
	nativeMessages := native["messages"].([]any)
	if len(nativeMessages) != 3 {
		t.Fatalf("native messages = %#v, want user + text answer + signed answer", nativeMessages)
	}
	if anthropicMessageHasThinking(nativeMessages[1]) || anthropicMessageText(nativeMessages[1]) != "answer" {
		t.Fatalf("unsigned thinking was kept: %#v", nativeMessages[1])
	}
	if !anthropicMessageHasSignedThinking(nativeMessages[2], "sig-ok") {
		t.Fatalf("signed thinking dropped: %#v", nativeMessages[2])
	}
	for _, raw := range nativeMessages {
		if anthropicMessageContentEmpty(raw) {
			t.Fatalf("native payload has an empty assistant message: %#v", nativeMessages)
		}
	}

	domestic := finishAnthropicThinkingPayload(t, "deepseek-v4-flash", messages, false)
	domesticMessages := domestic["messages"].([]any)
	if len(domesticMessages) != 4 {
		t.Fatalf("domestic messages = %#v, want unsigned thinking kept", domesticMessages)
	}
	if !anthropicMessageHasThinking(domesticMessages[1]) || anthropicMessageText(domesticMessages[1]) != "answer" {
		t.Fatalf("domestic unsigned thinking dropped: %#v", domesticMessages[1])
	}
	if !anthropicMessageHasThinking(domesticMessages[2]) {
		t.Fatalf("domestic thinking-only turn dropped: %#v", domesticMessages[2])
	}
}

func TestAnthropicThinkingDisabledWhenToolUseLacksSignature(t *testing.T) {
	t.Parallel()

	messages := []canonicalMessage{
		{Type: "message", Role: "user", Content: []canonicalContentPart{{Type: "input_text", Text: "hi"}}},
		{Type: "message", Role: "assistant", Thinking: []canonicalThinkingBlock{{Thinking: "unsigned plan"}}, Content: []canonicalContentPart{{Type: "output_text", Text: "calling"}}},
		{Type: "function_call", ToolCallID: "call_1", Name: "lookup", Arguments: `{}`},
		{Type: "function_call_output", ToolCallID: "call_1", Output: "ok"},
	}
	native := finishAnthropicThinkingPayload(t, "claude-sonnet-4-5", messages, true)
	if _, ok := native["thinking"]; ok {
		t.Fatalf("thinking param kept without a signed thinking block: %#v", native["thinking"])
	}
	nativeAssistant := native["messages"].([]any)[1].(map[string]any)
	if anthropicMessageHasThinking(nativeAssistant) || !anthropicMessageHasBlockType(nativeAssistant, "tool_use") {
		t.Fatalf("native tool round = %#v", nativeAssistant["content"])
	}

	domestic := finishAnthropicThinkingPayload(t, "deepseek-v4-flash", messages, true)
	if _, ok := domestic["thinking"]; !ok {
		t.Fatal("domestic thinking param was removed")
	}
	domesticAssistant := domestic["messages"].([]any)[1].(map[string]any)
	if !anthropicMessageHasThinking(domesticAssistant) || !anthropicMessageHasBlockType(domesticAssistant, "tool_use") {
		t.Fatalf("domestic tool round = %#v", domesticAssistant["content"])
	}

	signed := []canonicalMessage{
		messages[0],
		{Type: "message", Role: "assistant", Thinking: []canonicalThinkingBlock{{Thinking: "signed plan", Signature: "sig-ok"}}, Content: []canonicalContentPart{{Type: "output_text", Text: "calling"}}},
		messages[2],
		messages[3],
	}
	kept := finishAnthropicThinkingPayload(t, "claude-sonnet-4-5", signed, true)
	if _, ok := kept["thinking"]; !ok {
		t.Fatal("thinking param removed from a signed tool round")
	}
	if !anthropicMessageHasSignedThinking(kept["messages"].([]any)[1], "sig-ok") {
		t.Fatalf("signed tool round = %#v", kept["messages"].([]any)[1])
	}
}

func finishAnthropicThinkingPayload(t *testing.T, model string, messages []canonicalMessage, thinking bool) map[string]any {
	t.Helper()
	request := canonicalRequest{Messages: messages, Params: map[string]any{}}
	if thinking {
		request.Params["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 2048}
	}
	candidate := routeengine.Candidate{Model: routeengine.CandidateModel{UpstreamName: model}}
	payload, err := encodeCanonicalRequestToAnthropicMessages(request, candidate)
	if err != nil {
		t.Fatalf("encode %s: %v", model, err)
	}
	return finishAnthropicUpstreamPayload(payload, gatewayRequest{}, candidate)
}

func anthropicMessageText(raw any) string {
	message, _ := raw.(map[string]any)
	content, _ := message["content"].([]any)
	for _, blockRaw := range content {
		block, _ := blockRaw.(map[string]any)
		if block["type"] == "text" {
			return anyString(block["text"])
		}
	}
	return ""
}

func anthropicMessageHasThinking(raw any) bool {
	return anthropicMessageHasBlockType(raw, "thinking")
}

func anthropicMessageHasSignedThinking(raw any, signature string) bool {
	message, _ := raw.(map[string]any)
	content, _ := message["content"].([]any)
	for _, blockRaw := range content {
		block, _ := blockRaw.(map[string]any)
		if block["type"] == "thinking" && block["signature"] == signature && strings.TrimSpace(anyString(block["thinking"])) != "" {
			return true
		}
	}
	return false
}

func anthropicMessageHasBlockType(raw any, blockType string) bool {
	message, _ := raw.(map[string]any)
	content, _ := message["content"].([]any)
	for _, blockRaw := range content {
		block, _ := blockRaw.(map[string]any)
		if block["type"] == blockType {
			return true
		}
	}
	return false
}

func anthropicMessageContentEmpty(raw any) bool {
	message, _ := raw.(map[string]any)
	content, _ := message["content"].([]any)
	return anthropicAssistantContentEmpty(content)
}

func TestResponsesOutputContentAsAnyPreservesAnnotations(t *testing.T) {
	t.Parallel()

	items := responsesOutputContentAsAny([]responsesOutputContent{
		{Type: "output_text", Text: "visible", Annotations: []any{map[string]any{"type": "url_citation"}}},
	})
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item type = %T, want map", items[0])
	}
	if item["type"] != "output_text" || item["text"] != "visible" {
		t.Fatalf("output content item = %#v", item)
	}
	if annotations, _ := item["annotations"].([]any); len(annotations) != 1 {
		t.Fatalf("annotations = %#v, want one", item["annotations"])
	}
}
