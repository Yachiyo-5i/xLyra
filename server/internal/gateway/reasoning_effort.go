package gateway

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// ReasoningEffortInfo is the public, JSON-serializable view of a model's
// reasoning_effort spec block, exposed to clients so effort pickers render
// the levels a model actually supports.
type ReasoningEffortInfo struct {
	Levels            []string `json:"levels"`
	Mode              string   `json:"mode,omitempty"`
	ThinkingMandatory bool     `json:"thinking_mandatory,omitempty"`
	Default           string   `json:"default,omitempty"`
}

// ReasoningEffortSpecForModel resolves the reasoning effort levels declared
// for a model key: the longest matching model glob wins, falling back to the
// model's provider-level block. Returns nil when nothing is declared — new
// models in a known family inherit automatically via glob patterns, unknown
// models simply omit the field and clients fall back to their own defaults.
func ReasoningEffortSpecForModel(modelKey string) *ReasoningEffortInfo {
	config, err := loadProtocolSpecRegistry()
	if err != nil {
		return nil
	}
	_, def := matchModelSpec(config.Models, modelKey)
	spec := def.ReasoningEffort
	if spec == nil {
		if providerDef, ok := providerSpecByKey(def.Provider); ok {
			spec = providerDef.ReasoningEffort
		}
	}
	if spec == nil || len(spec.Levels) == 0 {
		return nil
	}
	levels := make([]string, 0, len(spec.Levels))
	for _, level := range spec.Levels {
		normalized := strings.ToLower(strings.TrimSpace(level))
		if isCanonicalReasoningEffort(normalized) {
			levels = append(levels, normalized)
		}
	}
	if len(levels) == 0 {
		return nil
	}
	sort.Slice(levels, func(i, j int) bool {
		return canonicalReasoningEffortRanks[levels[i]] < canonicalReasoningEffortRanks[levels[j]]
	})
	defaultLevel := strings.ToLower(strings.TrimSpace(spec.Default))
	if !slices.Contains(levels, defaultLevel) {
		defaultLevel = ""
	}
	return &ReasoningEffortInfo{
		Levels:            levels,
		Mode:              strings.ToLower(strings.TrimSpace(spec.Mode)),
		ThinkingMandatory: spec.ThinkingMandatory,
		Default:           defaultLevel,
	}
}

// canonicalReasoningEffortRanks orders every reasoning effort level the
// gateway understands. The ladder is the superset of all upstream vendors;
// each model declares the subset it supports via reasoning_effort spec blocks.
var canonicalReasoningEffortRanks = map[string]int{
	"none":    0,
	"minimal": 1,
	"low":     2,
	"medium":  3,
	"high":    4,
	"xhigh":   5,
	"max":     6,
	"ultra":   7,
}

func isCanonicalReasoningEffort(effort string) bool {
	_, ok := canonicalReasoningEffortRanks[effort]
	return ok
}

func validateReasoningEffortSpec(spec *reasoningEffortSpec) error {
	if spec == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(spec.Mode)) {
	case "", "snap", "passthrough":
	default:
		return fmt.Errorf("reasoning_effort mode %q is not supported; must be snap or passthrough", spec.Mode)
	}
	if len(spec.Levels) == 0 {
		return fmt.Errorf("reasoning_effort requires at least one level")
	}
	seen := map[string]bool{}
	for _, level := range spec.Levels {
		normalized := strings.ToLower(strings.TrimSpace(level))
		if !isCanonicalReasoningEffort(normalized) {
			return fmt.Errorf("reasoning_effort level %q is not a canonical effort", level)
		}
		if seen[normalized] {
			return fmt.Errorf("reasoning_effort level %q is duplicated", level)
		}
		seen[normalized] = true
	}
	if spec.ThinkingMandatory && seen["none"] {
		return fmt.Errorf("reasoning_effort marks thinking as mandatory but allows level \"none\"")
	}
	if defaultLevel := strings.ToLower(strings.TrimSpace(spec.Default)); defaultLevel != "" {
		if !isCanonicalReasoningEffort(defaultLevel) {
			return fmt.Errorf("reasoning_effort default %q is not a canonical effort", spec.Default)
		}
		if !seen[defaultLevel] {
			return fmt.Errorf("reasoning_effort default %q is not in levels", spec.Default)
		}
	}
	return nil
}

// snapReasoningEffort projects a requested effort onto the supported level
// set: exact matches pass through, otherwise the nearest level by canonical
// rank wins, ties resolve upward, and out-of-range requests clamp to the
// closest boundary.
func snapReasoningEffort(requested string, levels []string) string {
	requested = strings.ToLower(strings.TrimSpace(requested))
	requestRank, ok := canonicalReasoningEffortRanks[requested]
	if !ok || len(levels) == 0 {
		return ""
	}
	best := ""
	bestRank := 0
	bestDistance := 0
	for _, level := range levels {
		rank, ok := canonicalReasoningEffortRanks[strings.ToLower(strings.TrimSpace(level))]
		if !ok {
			continue
		}
		distance := rank - requestRank
		if distance < 0 {
			distance = -distance
		}
		if best == "" || distance < bestDistance || (distance == bestDistance && rank > bestRank) {
			best, bestRank, bestDistance = level, rank, distance
		}
	}
	return best
}

// lowestReasoningEffort returns the lowest non-"none" level, used when a
// thinking-mandatory model receives a request that tries to disable thinking.
func lowestReasoningEffort(levels []string) string {
	best := ""
	bestRank := 0
	for _, level := range levels {
		normalized := strings.ToLower(strings.TrimSpace(level))
		rank, ok := canonicalReasoningEffortRanks[normalized]
		if !ok || normalized == "none" {
			continue
		}
		if best == "" || rank < bestRank {
			best, bestRank = normalized, rank
		}
	}
	return best
}

// applyReasoningEffortPolicy reconciles the effort levels present in an
// upstream-bound payload with the model's reasoning_effort spec. It snaps
// values in place wherever they live (reasoning_effort, reasoning.effort,
// output_config.effort) and, for thinking-mandatory models, rewrites
// thinking.type="disabled" to "enabled" so strict upstreams do not reject
// the request.
//
// The gateway-level value "auto" is always resolved here and never forwarded:
// it maps to the model's configured default level when the model requires an
// explicit effort (thinking mandatory), and is dropped otherwise so the
// upstream's own default applies.
func applyReasoningEffortPolicy(payload map[string]any, spec resolvedProtocolSpec) map[string]any {
	if payload == nil {
		return payload
	}
	policy := spec.ReasoningEffort
	resolveAutoReasoningEffort(payload, policy)
	if policy == nil || len(policy.Levels) == 0 {
		return payload
	}
	if strings.EqualFold(strings.TrimSpace(policy.Mode), "passthrough") {
		return payload
	}

	// A thinking-mandatory model treats an explicit "none" as "no preference
	// beyond keeping thinking cheap": enableMandatoryThinking below maps it
	// to the model's default level, so the snap stage leaves it untouched.
	skipNoneSnap := func(effort string) bool {
		return policy.ThinkingMandatory && strings.EqualFold(strings.TrimSpace(effort), "none")
	}
	if raw, ok := payload["reasoning_effort"]; ok {
		if effort, ok := raw.(string); ok && !skipNoneSnap(effort) {
			if resolved := snapReasoningEffort(effort, policy.Levels); resolved != "" && resolved != effort {
				payload["reasoning_effort"] = resolved
			}
		}
	}
	for _, key := range []string{"reasoning", "output_config"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		raw, ok := nested["effort"]
		if !ok {
			continue
		}
		effort, ok := raw.(string)
		if !ok || skipNoneSnap(effort) {
			continue
		}
		if resolved := snapReasoningEffort(effort, policy.Levels); resolved != "" && resolved != effort {
			nested["effort"] = resolved
		}
	}

	if policy.ThinkingMandatory {
		enableMandatoryThinking(payload, policy)
	}
	return payload
}

// resolveAutoReasoningEffort rewrites the gateway-level "auto" effort value
// in place, wherever it appears. Thinking-mandatory models get their
// configured default (falling back to the lowest level); every other model
// has the field removed so the upstream applies its own default. A nil policy
// (undeclared model) always drops "auto" — upstreams do not know the value.
func resolveAutoReasoningEffort(payload map[string]any, policy *reasoningEffortSpec) {
	resolve := func(effort string) (string, bool) {
		if !strings.EqualFold(strings.TrimSpace(effort), "auto") {
			return effort, true
		}
		if policy != nil && policy.ThinkingMandatory {
			if resolved := defaultReasoningEffort(policy); resolved != "" {
				return resolved, true
			}
		}
		return "", false
	}
	if raw, ok := payload["reasoning_effort"].(string); ok {
		if resolved, keep := resolve(raw); keep {
			payload["reasoning_effort"] = resolved
		} else {
			delete(payload, "reasoning_effort")
		}
	}
	for _, key := range []string{"reasoning", "output_config"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		raw, ok := nested["effort"].(string)
		if !ok {
			continue
		}
		if resolved, keep := resolve(raw); keep {
			nested["effort"] = resolved
		} else {
			delete(nested, "effort")
		}
	}
}

// defaultReasoningEffort returns the level an omitted/"auto" effort resolves
// to for thinking-mandatory models: the configured default when valid, else
// the lowest supported level.
func defaultReasoningEffort(policy *reasoningEffortSpec) string {
	if def := strings.ToLower(strings.TrimSpace(policy.Default)); def != "" {
		if slices.Contains(policy.Levels, def) {
			return def
		}
	}
	return lowestReasoningEffort(policy.Levels)
}

// enableMandatoryThinking flips thinking off-switch requests back on for
// models that reject disabled thinking (GLM-5.3, Kimi K3, GPT-6). The
// resulting effort is the model's configured default — an explicit "off"
// means "no preference beyond keeping thinking cheap", which the model's own
// default satisfies (lowest level only when no default is declared).
func enableMandatoryThinking(payload map[string]any, policy *reasoningEffortSpec) bool {
	disabled := false
	switch thinking := payload["thinking"].(type) {
	case map[string]any:
		if strings.EqualFold(strings.TrimSpace(anyString(thinking["type"])), "disabled") {
			thinking["type"] = "enabled"
			disabled = true
		}
	case bool:
		if !thinking {
			payload["thinking"] = map[string]any{"type": "enabled"}
			disabled = true
		}
	}
	if effort, ok := payload["reasoning_effort"].(string); ok && strings.EqualFold(strings.TrimSpace(effort), "none") {
		payload["reasoning_effort"] = defaultReasoningEffort(policy)
		disabled = true
	}
	if !disabled {
		return false
	}
	if !payloadHasAnyEffort(payload) {
		if resolved := defaultReasoningEffort(policy); resolved != "" {
			payload["reasoning_effort"] = resolved
		}
	}
	return true
}

func payloadHasAnyEffort(payload map[string]any) bool {
	if effort, ok := payload["reasoning_effort"].(string); ok && strings.TrimSpace(effort) != "" {
		return true
	}
	for _, key := range []string{"reasoning", "output_config"} {
		if nested, ok := payload[key].(map[string]any); ok {
			if effort, ok := nested["effort"].(string); ok && strings.TrimSpace(effort) != "" {
				return true
			}
		}
	}
	return false
}
