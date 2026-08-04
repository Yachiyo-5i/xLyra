package gateway

import (
	"strings"

	"github.com/google/uuid"
)

func responsesItemIDPrefix(itemType string) (string, bool) {
	switch strings.TrimSpace(itemType) {
	case "additional_tools":
		return "at", true
	case "message":
		return "msg", true
	case "agent_message":
		return "amsg", true
	case "reasoning":
		return "rs", true
	case "local_shell_call":
		return "lsh", true
	case "function_call":
		return "fc", true
	case "tool_search_call":
		return "tsc", true
	case "function_call_output":
		return "fco", true
	case "custom_tool_call":
		return "ctc", true
	case "custom_tool_call_output":
		return "ctco", true
	case "tool_search_output":
		return "tso", true
	case "web_search_call":
		return "ws", true
	case "image_generation_call":
		return "ig", true
	case "compaction", "context_compaction", "compaction_summary":
		return "cmp", true
	default:
		return "", false
	}
}

func responsesItemIDMatchesType(itemType string, itemID string) bool {
	prefix, ok := responsesItemIDPrefix(itemType)
	normalized := strings.TrimSpace(itemID)
	return ok && len(normalized) > len(prefix)+1 && strings.HasPrefix(normalized, prefix+"_")
}

func responsesItemIDForType(itemType string, itemID string, fallback string) string {
	prefix, ok := responsesItemIDPrefix(itemType)
	if !ok {
		return strings.TrimSpace(itemID)
	}
	itemID = strings.TrimSpace(itemID)
	if responsesItemIDMatchesType(itemType, itemID) {
		return itemID
	}
	suffix := strings.TrimSpace(fallback)
	if suffix == "" {
		suffix = itemID
		if separator := strings.IndexByte(suffix, '_'); separator > 0 {
			suffix = suffix[separator+1:]
		}
	}
	if suffix == "" {
		suffix = uuid.NewString()
	}
	return prefix + "_" + suffix
}
