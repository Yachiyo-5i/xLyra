package gateway

import (
	"fmt"
	"net/http"
	"strings"
)

func normalizeClientRequestOptions(payload map[string]any) *chatFailure {
	if serviceTier, ok := payload["service_tier"].(string); ok {
		payload["service_tier"] = strings.ToLower(strings.TrimSpace(serviceTier))
	}
	if err := normalizeReasoningEffortPayload(payload); err != nil {
		return &chatFailure{
			status:  http.StatusBadRequest,
			code:    "invalid_reasoning_effort",
			message: err.Error(),
			stage:   "validate",
		}
	}
	return nil
}

func normalizeReasoningEffortPayload(payload map[string]any) error {
	scalar, scalarSet, err := normalizeReasoningEffortField(payload, "reasoning_effort")
	if err != nil {
		return err
	}

	nested := ""
	nestedSet := false
	if reasoning, ok := payload["reasoning"].(map[string]any); ok {
		nested, nestedSet, err = normalizeReasoningEffortField(reasoning, "effort")
		if err != nil {
			return fmt.Errorf("reasoning.%w", err)
		}
	}
	if scalarSet && nestedSet && scalar != nested {
		return fmt.Errorf("reasoning_effort and reasoning.effort must match")
	}
	return nil
}

func normalizeReasoningEffortField(payload map[string]any, key string) (string, bool, error) {
	raw, exists := payload[key]
	if !exists {
		return "", false, nil
	}
	effort, ok := raw.(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be one of light, medium, high, xhigh, max, ultra", key)
	}
	effort = strings.ToLower(strings.TrimSpace(effort))
	if effort == "" {
		delete(payload, key)
		return "", false, nil
	}
	if !isSupportedReasoningEffort(effort) {
		return "", false, fmt.Errorf("%s must be one of light, medium, high, xhigh, max, ultra", key)
	}
	payload[key] = effort
	return effort, true, nil
}

func isSupportedReasoningEffort(effort string) bool {
	switch effort {
	case "light", "medium", "high", "xhigh", "max", "ultra":
		return true
	default:
		return false
	}
}
