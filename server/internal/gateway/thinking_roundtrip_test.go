package gateway

import "testing"

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

	if got := responsesReasoningItem([]canonicalThinkingBlock{{Thinking: " "}}); got != nil {
		t.Fatalf("blank responses reasoning item = %#v, want nil", got)
	}
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
