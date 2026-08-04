package store

import (
	"testing"
)

func TestParseAPIKeyModelRulesLegacyMapConvertsSortedHardRules(t *testing.T) {
	t.Parallel()

	rules := ParseAPIKeyModelRules(JSON(`{"zeta":"model-z","alpha":"model-a","blank":"  "}`))
	if len(rules) != 2 {
		t.Fatalf("legacy rules = %#v, want 2 entries with blank dropped", rules)
	}
	if rules[0].Pattern != "alpha" || rules[0].Target != "model-a" || rules[0].Mode != APIKeyModelRuleModeHard {
		t.Fatalf("first legacy rule = %#v, want sorted alpha/hard", rules[0])
	}
	if rules[1].Pattern != "zeta" || rules[1].Target != "model-z" {
		t.Fatalf("second legacy rule = %#v", rules[1])
	}
}

func TestParseAPIKeyModelRulesArrayFormatNormalizesMode(t *testing.T) {
	t.Parallel()

	rules := ParseAPIKeyModelRules(JSON(`[
		{"pattern":" gpt-4* ","target":" gpt-5.5 ","mode":"SOFT"},
		{"pattern":"*","target":"gpt-5.5","mode":"bogus"},
		{"pattern":"","target":"gpt-5.5"}
	]`))
	if len(rules) != 2 {
		t.Fatalf("rules = %#v, want 2 entries with blank pattern dropped", rules)
	}
	if rules[0].Pattern != "gpt-4*" || rules[0].Target != "gpt-5.5" || rules[0].Mode != APIKeyModelRuleModeSoft {
		t.Fatalf("first rule = %#v, want trimmed soft rule", rules[0])
	}
	if rules[1].Mode != APIKeyModelRuleModeHard {
		t.Fatalf("second rule mode = %q, want invalid mode normalized to hard", rules[1].Mode)
	}
}

func TestParseAPIKeyModelRulesInvalidInputsReturnNil(t *testing.T) {
	t.Parallel()

	for _, raw := range []JSON{nil, JSON(``), JSON(`  `), JSON(`{}`), JSON(`[]`), JSON(`{bad`), JSON(`[{"pattern":1}]`), JSON(`"text"`)} {
		if rules := ParseAPIKeyModelRules(raw); rules != nil {
			t.Fatalf("rules for %q = %#v, want nil", raw, rules)
		}
	}
}

func TestAPIKeyModelRuleKindPredicates(t *testing.T) {
	t.Parallel()

	if rule := (APIKeyModelRule{Pattern: "*"}); !rule.IsCatchAll() || rule.IsWildcard() || rule.IsExact() {
		t.Fatalf("catch-all predicates wrong: %#v", rule)
	}
	if rule := (APIKeyModelRule{Pattern: "gpt-4*"}); rule.IsCatchAll() || !rule.IsWildcard() || rule.IsExact() {
		t.Fatalf("wildcard predicates wrong: %#v", rule)
	}
	if rule := (APIKeyModelRule{Pattern: "gpt-4o"}); rule.IsCatchAll() || rule.IsWildcard() || !rule.IsExact() {
		t.Fatalf("exact predicates wrong: %#v", rule)
	}
}

func TestMigrateLegacyModelMappingsConvertsMapAndSkipsArrays(t *testing.T) {
	t.Parallel()

	migrated, ok := migrateLegacyModelMappings(JSON(`{"gpt-4.1":"upstream"}`))
	if !ok {
		t.Fatal("legacy map should migrate")
	}
	rules := ParseAPIKeyModelRules(migrated)
	if len(rules) != 1 || rules[0].Pattern != "gpt-4.1" || rules[0].Target != "upstream" || rules[0].Mode != APIKeyModelRuleModeHard {
		t.Fatalf("migrated rules = %#v", rules)
	}

	empty, ok := migrateLegacyModelMappings(JSON(`{}`))
	if !ok || string(empty) != "[]" {
		t.Fatalf("empty legacy map migration = (%q, %v), want empty array", empty, ok)
	}

	if _, ok := migrateLegacyModelMappings(JSON(`[{"pattern":"a","target":"b"}]`)); ok {
		t.Fatal("array format must not be migrated again")
	}
	if _, ok := migrateLegacyModelMappings(JSON(`{bad`)); ok {
		t.Fatal("invalid json must not be migrated")
	}
	if _, ok := migrateLegacyModelMappings(nil); ok {
		t.Fatal("empty value must not be migrated")
	}
}

func TestUpdateAPIKeyModelMappingsParamSemantics(t *testing.T) {
	t.Parallel()

	current := JSON(`[{"pattern":"keep","target":"model-a","mode":"hard"}]`)
	preserved := jsonDefault(jsonFromAny(nil, string(current)), "[]")
	if string(preserved) != string(current) {
		t.Fatalf("nil mappings param = %s, want current value preserved", preserved)
	}
	cleared := jsonDefault(jsonFromAny([]APIKeyModelRule{}, string(current)), "[]")
	if string(cleared) != "[]" {
		t.Fatalf("empty mappings param = %s, want cleared to []", cleared)
	}
}
