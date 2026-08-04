package gateway

import (
	"testing"

	"xlyra/server/internal/store"
)

func TestMatchModelRulePriorityExactOverWildcardOverCatchAll(t *testing.T) {
	t.Parallel()

	rules := []store.APIKeyModelRule{
		{Pattern: "*", Target: "fallback-model", Mode: "hard"},
		{Pattern: "gpt-4*", Target: "wildcard-model", Mode: "hard"},
		{Pattern: "gpt-4o", Target: "exact-model", Mode: "hard"},
	}

	rule, ok := matchModelRule(rules, "gpt-4o")
	if !ok || rule.Target != "exact-model" {
		t.Fatalf("exact match = (%#v, %v), want exact-model", rule, ok)
	}
	rule, ok = matchModelRule(rules, "gpt-4o-mini-2024-07-18")
	if !ok || rule.Target != "wildcard-model" {
		t.Fatalf("wildcard match = (%#v, %v), want wildcard-model", rule, ok)
	}
	rule, ok = matchModelRule(rules, "claude-sonnet")
	if !ok || rule.Target != "fallback-model" {
		t.Fatalf("catch-all match = (%#v, %v), want fallback-model", rule, ok)
	}
}

func TestMatchModelRuleLongestPrefixWins(t *testing.T) {
	t.Parallel()

	rules := []store.APIKeyModelRule{
		{Pattern: "gpt*", Target: "short-prefix", Mode: "hard"},
		{Pattern: "gpt-4o*", Target: "long-prefix", Mode: "hard"},
		{Pattern: "gpt-4*", Target: "mid-prefix", Mode: "hard"},
	}

	rule, ok := matchModelRule(rules, "gpt-4o-audio")
	if !ok || rule.Target != "long-prefix" {
		t.Fatalf("longest prefix match = (%#v, %v), want long-prefix", rule, ok)
	}
	rule, ok = matchModelRule(rules, "gpt-4.1")
	if !ok || rule.Target != "mid-prefix" {
		t.Fatalf("mid prefix match = (%#v, %v), want mid-prefix", rule, ok)
	}
	rule, ok = matchModelRule(rules, "gpt-5.5")
	if !ok || rule.Target != "short-prefix" {
		t.Fatalf("short prefix match = (%#v, %v), want short-prefix", rule, ok)
	}
}

func TestMatchModelRuleNormalizesPatternsAndRequests(t *testing.T) {
	t.Parallel()

	rules := []store.APIKeyModelRule{
		{Pattern: " GPT_5 Codex ", Target: "exact-normalized", Mode: "hard"},
		{Pattern: "openai/gpt-4*", Target: "prefix-normalized", Mode: "hard"},
	}

	rule, ok := matchModelRule(rules, "models/gpt-5-codex")
	if !ok || rule.Target != "exact-normalized" {
		t.Fatalf("normalized exact match = (%#v, %v), want exact-normalized", rule, ok)
	}
	rule, ok = matchModelRule(rules, "GPT-4O")
	if !ok || rule.Target != "prefix-normalized" {
		t.Fatalf("normalized prefix match = (%#v, %v), want prefix-normalized", rule, ok)
	}
}

func TestMatchModelRuleWildcardKeepsTrailingSeparatorBoundary(t *testing.T) {
	t.Parallel()

	rules := []store.APIKeyModelRule{
		{Pattern: "gpt-4.*", Target: "dot-family", Mode: "hard"},
	}

	rule, ok := matchModelRule(rules, "gpt-4.5")
	if !ok || rule.Target != "dot-family" {
		t.Fatalf("dot-boundary match = (%#v, %v), want dot-family", rule, ok)
	}
	if rule, ok := matchModelRule(rules, "gpt-4o"); ok {
		t.Fatalf("gpt-4o matched dot-boundary rule = %#v, want no match", rule)
	}
	if rule, ok := matchModelRule(rules, "gpt-45"); ok {
		t.Fatalf("gpt-45 matched dot-boundary rule = %#v, want no match", rule)
	}

	dashRules := []store.APIKeyModelRule{{Pattern: "gpt-4-*", Target: "dash-family", Mode: "hard"}}
	if rule, ok := matchModelRule(dashRules, "gpt-4o"); ok {
		t.Fatalf("gpt-4o matched dash-boundary rule = %#v, want no match", rule)
	}
	rule, ok = matchModelRule(dashRules, "gpt-4-turbo")
	if !ok || rule.Target != "dash-family" {
		t.Fatalf("dash-boundary match = (%#v, %v), want dash-family", rule, ok)
	}
}

func TestMatchModelRuleIgnoresInnerStarWildcards(t *testing.T) {
	t.Parallel()

	rules := []store.APIKeyModelRule{{Pattern: "gpt-*-turbo*", Target: "broken", Mode: "hard"}}
	if rule, ok := matchModelRule(rules, "gpt-4-turbo"); ok {
		t.Fatalf("inner-star rule matched = %#v, want ignored", rule)
	}
}

func TestMatchModelRuleReturnsFalseWithoutMatch(t *testing.T) {
	t.Parallel()

	rules := []store.APIKeyModelRule{
		{Pattern: "gpt-4o", Target: "exact-model", Mode: "hard"},
		{Pattern: "claude*", Target: "wildcard-model", Mode: "hard"},
	}

	if rule, ok := matchModelRule(rules, "gemini-pro"); ok {
		t.Fatalf("unexpected match = %#v", rule)
	}
	if rule, ok := matchModelRule(nil, "gpt-4o"); ok {
		t.Fatalf("nil rules match = %#v", rule)
	}
	if rule, ok := matchModelRule(rules, ""); ok {
		t.Fatalf("empty model match = %#v", rule)
	}
}

func TestResolveModelMappingSkipsIdentityMapping(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	apiKey := store.APIKey{ModelMappings: store.JSON(`[{"pattern":"*","target":"gpt-5.5","mode":"hard"}]`)}

	if rule, ok := handler.resolveModelMapping(apiKey, "gpt-5.5"); ok {
		t.Fatalf("identity mapping = (%#v, %v), want no mapping", rule, ok)
	}
	rule, ok := handler.resolveModelMapping(apiKey, "anything-else")
	if !ok || rule.Target != "gpt-5.5" {
		t.Fatalf("catch-all mapping = (%#v, %v), want gpt-5.5", rule, ok)
	}
}

func TestResolveModelMappingLegacyMapFormat(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	apiKey := store.APIKey{ModelMappings: store.JSON(`{" GPT_5 Codex ":"gpt-5-codex-upstream","claude-sonnet":"claude-4-sonnet"}`)}

	rule, ok := handler.resolveModelMapping(apiKey, "gpt-5-codex")
	if !ok || rule.Target != "gpt-5-codex-upstream" || rule.Mode != store.APIKeyModelRuleModeHard {
		t.Fatalf("legacy mapping = (%#v, %v), want hard gpt-5-codex-upstream", rule, ok)
	}
	if _, ok := handler.resolveModelMapping(apiKey, "gpt-4o"); ok {
		t.Fatal("legacy mapping must not match unrelated model")
	}
}

func TestModelRuleAliasPayloadsExposeExactAliasesOnly(t *testing.T) {
	t.Parallel()

	items := map[string]map[string]any{
		"gpt-5.5": {
			"id":       "gpt-5.5",
			"object":   "model",
			"metadata": map[string]any{"display_name": "GPT-5.5", "category": "chat"},
		},
	}
	rules := []store.APIKeyModelRule{
		{Pattern: "my-alias", Target: "gpt-5.5", Mode: "hard"},
		{Pattern: "gpt-4*", Target: "gpt-5.5", Mode: "hard"},
		{Pattern: "*", Target: "gpt-5.5", Mode: "hard"},
		{Pattern: "GPT-5.5", Target: "gpt-5.5", Mode: "hard"},
		{Pattern: "orphan-alias", Target: "missing-model", Mode: "hard"},
	}

	aliases := modelRuleAliasPayloads(rules, items)
	if len(aliases) != 1 {
		t.Fatalf("aliases = %#v, want only the exact alias for an available target", aliases)
	}
	if aliases[0]["id"] != "my-alias" {
		t.Fatalf("alias id = %v, want my-alias", aliases[0]["id"])
	}
	metadata := aliases[0]["metadata"].(map[string]any)
	if metadata["mapped_model"] != "gpt-5.5" || metadata["display_name"] != "my-alias" {
		t.Fatalf("alias metadata = %#v", metadata)
	}
	if targetMetadata := items["gpt-5.5"]["metadata"].(map[string]any); targetMetadata["display_name"] != "GPT-5.5" {
		t.Fatalf("target metadata mutated: %#v", targetMetadata)
	}
}
