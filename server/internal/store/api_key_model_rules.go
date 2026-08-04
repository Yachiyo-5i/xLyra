package store

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

const (
	APIKeyModelRuleModeHard = "hard"
	APIKeyModelRuleModeSoft = "soft"

	APIKeyModelRuleCatchAll = "*"
)

type APIKeyModelRule struct {
	Pattern string `json:"pattern"`
	Target  string `json:"target"`
	Mode    string `json:"mode,omitempty"`
}

func (r APIKeyModelRule) IsCatchAll() bool {
	return strings.TrimSpace(r.Pattern) == APIKeyModelRuleCatchAll
}

func (r APIKeyModelRule) IsWildcard() bool {
	pattern := strings.TrimSpace(r.Pattern)
	return len(pattern) > 1 && strings.HasSuffix(pattern, "*")
}

func (r APIKeyModelRule) IsExact() bool {
	return !r.IsCatchAll() && !r.IsWildcard()
}

func NormalizeAPIKeyModelRuleMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), APIKeyModelRuleModeSoft) {
		return APIKeyModelRuleModeSoft
	}
	return APIKeyModelRuleModeHard
}

func NormalizeAPIKeyModelRules(rules []APIKeyModelRule) []APIKeyModelRule {
	normalized := make([]APIKeyModelRule, 0, len(rules))
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Pattern)
		target := strings.TrimSpace(rule.Target)
		if pattern == "" || target == "" {
			continue
		}
		normalized = append(normalized, APIKeyModelRule{
			Pattern: pattern,
			Target:  target,
			Mode:    NormalizeAPIKeyModelRuleMode(rule.Mode),
		})
	}
	return normalized
}

func ParseAPIKeyModelRules(data JSON) []APIKeyModelRule {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0] == '[' {
		var rules []APIKeyModelRule
		if err := json.Unmarshal(trimmed, &rules); err != nil {
			return nil
		}
		normalized := NormalizeAPIKeyModelRules(rules)
		if len(normalized) == 0 {
			return nil
		}
		return normalized
	}
	var legacy map[string]string
	if err := json.Unmarshal(trimmed, &legacy); err != nil {
		return nil
	}
	return LegacyAPIKeyModelRules(legacy)
}

func LegacyAPIKeyModelRules(mappings map[string]string) []APIKeyModelRule {
	if len(mappings) == 0 {
		return nil
	}
	patterns := make([]string, 0, len(mappings))
	for pattern := range mappings {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	rules := make([]APIKeyModelRule, 0, len(patterns))
	for _, pattern := range patterns {
		rules = append(rules, APIKeyModelRule{
			Pattern: pattern,
			Target:  mappings[pattern],
			Mode:    APIKeyModelRuleModeHard,
		})
	}
	normalized := NormalizeAPIKeyModelRules(rules)
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func (k APIKey) ModelRules() []APIKeyModelRule {
	return ParseAPIKeyModelRules(k.ModelMappings)
}
