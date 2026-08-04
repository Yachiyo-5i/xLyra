package gateway

import "strings"

func canonicalThinkingFromChatMessage(msg map[string]any) []canonicalThinkingBlock {
	thinking := firstNonEmptyGatewayString(
		anyString(msg["reasoning_content"]),
		anyString(msg["thinking"]),
	)
	if thinking == "" {
		return nil
	}
	return []canonicalThinkingBlock{{
		Type:      "thinking",
		Thinking:  thinking,
		Signature: anyString(msg["thinking_signature"]),
	}}
}

func canonicalThinkingFromResponsesItem(item map[string]any) []canonicalThinkingBlock {
	itemType := strings.TrimSpace(anyString(item["type"]))
	if itemType == "reasoning" {
		if thinking := responsesReasoningText(item["content"]); thinking != "" {
			return []canonicalThinkingBlock{{Type: "thinking", Thinking: thinking}}
		}
	}
	return canonicalThinkingFromChatMessage(item)
}

func responsesReasoningText(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case []any:
		parts := make([]string, 0, len(value))
		for _, rawPart := range value {
			part, _ := rawPart.(map[string]any)
			switch strings.TrimSpace(anyString(part["type"])) {
			case "reasoning_text", "summary_text", "text":
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

func anthropicThinkingBlocksFromCanonical(blocks []canonicalThinkingBlock) []any {
	out := make([]any, 0, len(blocks))
	for _, block := range blocks {
		thinking := strings.TrimSpace(block.Thinking)
		if thinking == "" {
			continue
		}
		item := clonePayload(block.Raw)
		if item == nil {
			item = map[string]any{}
		}
		item["type"] = nonEmptyString(block.Type, "thinking")
		item["thinking"] = block.Thinking
		if signature := strings.TrimSpace(block.Signature); signature != "" {
			item["signature"] = signature
		}
		out = append(out, item)
	}
	return out
}

func chatReasoningContent(blocks []canonicalThinkingBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if thinking := strings.TrimSpace(block.Thinking); thinking != "" {
			parts = append(parts, block.Thinking)
		}
	}
	return strings.Join(parts, "\n")
}

func responsesReasoningItem(blocks []canonicalThinkingBlock) map[string]any {
	text := chatReasoningContent(blocks)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return map[string]any{
		"type": "reasoning",
		"content": []any{
			map[string]any{"type": "reasoning_text", "text": text},
		},
	}
}

func canonicalThinkingFromAnthropicBlock(block map[string]any) (canonicalThinkingBlock, bool) {
	if strings.TrimSpace(anyString(block["type"])) != "thinking" {
		return canonicalThinkingBlock{}, false
	}
	thinking := rawStringFromAny(block["thinking"])
	if strings.TrimSpace(thinking) == "" {
		return canonicalThinkingBlock{}, false
	}
	return canonicalThinkingBlock{
		Type:      "thinking",
		Thinking:  thinking,
		Signature: rawStringFromAny(block["signature"]),
		Raw:       block,
	}, true
}

func canonicalThinkingBlocksFromOutput(items []canonicalOutputItem) []canonicalThinkingBlock {
	out := make([]canonicalThinkingBlock, 0)
	for _, item := range items {
		out = append(out, item.Thinking...)
	}
	return out
}

func responsesOutputContentAsAny(content []responsesOutputContent) []any {
	out := make([]any, 0, len(content))
	for _, part := range content {
		item := map[string]any{
			"type": part.Type,
			"text": part.Text,
		}
		if len(part.Annotations) > 0 {
			item["annotations"] = part.Annotations
		}
		out = append(out, item)
	}
	return out
}
