package gateway

import (
	"strings"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/store"
)

func matchModelRule(rules []store.APIKeyModelRule, requestedModel string) (store.APIKeyModelRule, bool) {
	normalized := catalog.NormalizeModelKey(requestedModel)
	if normalized == "" {
		return store.APIKeyModelRule{}, false
	}

	var wildcardMatch store.APIKeyModelRule
	wildcardLen := -1
	var catchAll *store.APIKeyModelRule

	for index := range rules {
		rule := rules[index]
		switch {
		case rule.IsCatchAll():
			if catchAll == nil {
				catchAll = &rules[index]
			}
		case rule.IsWildcard():
			rawPrefix := strings.TrimSuffix(strings.TrimSpace(rule.Pattern), "*")
			if strings.Contains(rawPrefix, "*") {
				continue
			}
			prefix := catalog.NormalizeModelKeyPrefix(rawPrefix)
			if prefix == "" || !strings.HasPrefix(normalized, prefix) {
				continue
			}
			if len(prefix) > wildcardLen {
				wildcardMatch = rule
				wildcardLen = len(prefix)
			}
		default:
			if catalog.NormalizeModelKey(rule.Pattern) == normalized {
				return rule, true
			}
		}
	}

	if wildcardLen >= 0 {
		return wildcardMatch, true
	}
	if catchAll != nil {
		return *catchAll, true
	}
	return store.APIKeyModelRule{}, false
}
