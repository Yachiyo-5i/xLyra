package jsplugin

import (
	"context"
	"strings"
	"testing"
)

func automationSection() AutomationSection {
	return AutomationSection{
		Subscribes:  []string{"oauth.quota_synced"},
		Permissions: []string{ActionAPIKeyResetUsage, ActionNotify},
		Binding: AutomationBinding{
			Subject: AutomationSubjectSpec{Type: "oauth_connection", Providers: []string{"codex"}},
			Target:  AutomationTargetSpec{Type: "api_key", Requires: "finite_total_quota"},
			Config: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"threshold": map[string]any{"type": "integer", "minimum": float64(1), "maximum": float64(100), "default": float64(10)},
					"mode":      map[string]any{"type": "string", "enum": []any{"a", "b"}},
				},
				"required": []any{"mode"},
			},
		},
	}
}

func automationPlugin(t *testing.T, id string, section AutomationSection, body string) *Plugin {
	t.Helper()
	source := meta(id, KindAutomation) + body
	manifest := Manifest{
		ID: id, Name: id, Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: KindAutomation,
		Automation: section,
		SHA256:     map[string]string{"plugin.js": hashSource(source)},
	}
	plugin, err := NewPlugin(manifest, source, nil, 1)
	if err != nil {
		t.Fatalf("NewPlugin: %v", err)
	}
	return plugin
}

func TestAutomationManifestRules(t *testing.T) {
	bad := map[string]func(*AutomationSection){
		"no events":          func(s *AutomationSection) { s.Subscribes = nil },
		"unknown event":      func(s *AutomationSection) { s.Subscribes = []string{"nope"} },
		"duplicate event":    func(s *AutomationSection) { s.Subscribes = []string{"oauth.quota_synced", "oauth.quota_synced"} },
		"unknown subject":    func(s *AutomationSection) { s.Binding.Subject.Type = "site" },
		"unknown permission": func(s *AutomationSection) { s.Permissions = []string{"db.drop"} },
		"reset without target": func(s *AutomationSection) {
			s.Binding.Target = AutomationTargetSpec{}
		},
		"unknown requirement": func(s *AutomationSection) { s.Binding.Target.Requires = "rich" },
		"nested config": func(s *AutomationSection) {
			s.Binding.Config = map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "object"}}}
		},
		"tick without schedule": func(s *AutomationSection) {
			s.Subscribes = []string{EventScheduleTick}
		},
		"tick too often": func(s *AutomationSection) {
			s.Subscribes = []string{EventScheduleTick}
			s.Schedule.EveryMinutes = 1
		},
		"tick too rarely": func(s *AutomationSection) {
			s.Subscribes = []string{EventScheduleTick}
			s.Schedule.EveryMinutes = MaxScheduleMinutes + 1
		},
		"schedule without tick": func(s *AutomationSection) { s.Schedule.EveryMinutes = 60 },
		"bad default": func(s *AutomationSection) {
			s.Binding.Config = map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "integer", "default": "high"}}}
		},
	}
	for name, mutate := range bad {
		section := automationSection()
		mutate(&section)
		if err := validateAutomationSection(section); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := validateAutomationSection(automationSection()); err != nil {
		t.Fatalf("valid section rejected: %v", err)
	}
	ticking := automationSection()
	ticking.Subscribes = []string{"oauth.quota_synced", EventScheduleTick}
	ticking.Schedule.EveryMinutes = 60
	if err := validateAutomationSection(ticking); err != nil {
		t.Fatalf("a schedule with ticks rejected: %v", err)
	}
}

func TestApplyConfigValidatesAndFillsDefaults(t *testing.T) {
	schema := automationSection().Binding.Config
	got, err := ApplyConfig(schema, map[string]any{"mode": "a"})
	if err != nil || got["threshold"] != float64(10) || got["mode"] != "a" {
		t.Fatalf("got %v, %v", got, err)
	}
	for name, config := range map[string]map[string]any{
		"missing required": {},
		"out of range":     {"mode": "a", "threshold": float64(500)},
		"not an integer":   {"mode": "a", "threshold": 1.5},
		"outside enum":     {"mode": "c"},
		"unknown key":      {"mode": "a", "other": true},
	} {
		if _, err := ApplyConfig(schema, config); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestCallAutomationChecksActions(t *testing.T) {
	targets := []AutomationTarget{{Type: "api_key", ID: "key-1", Name: "alice"}}
	event := AutomationEvent{Type: "oauth.quota_synced", Subject: AutomationSubject{Type: "oauth_connection", ID: "c"}, Targets: targets}
	call := func(section AutomationSection, body string) (AutomationResult, error) {
		plugin := automationPlugin(t, "acme-auto", section, body)
		return plugin.CallAutomation(context.Background(), AutomationContext{Event: event.Type, Now: 1, BindingID: "b"}, event)
	}

	good, err := call(automationSection(), `export function handle(ctx, event) {
  return {
    actions: event.targets.map((t) => ({ type: "apikey.reset_usage", target: t.id, scope: "total", idempotencyKey: "9" })),
    state: { seen: ctx.now },
  };
}`)
	if err != nil || len(good.Actions) != 1 || good.Actions[0].Target != "key-1" || good.State["seen"] == nil {
		t.Fatalf("good = %+v err = %v", good, err)
	}

	for name, body := range map[string]string{
		"unknown target":   `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "key-9", scope: "total", idempotencyKey: "1" }] }; }`,
		"missing scope":    `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "key-1", idempotencyKey: "1" }] }; }`,
		"missing key":      `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "key-1", scope: "total" }] }; }`,
		"unknown action":   `export function handle() { return { actions: [{ type: "db.drop" }] }; }`,
		"empty notice":     `export function handle() { return { actions: [{ type: "notify" }] }; }`,
		"unknown field":    `export function handle() { return { actions: [], cooldown: 5 }; }`,
		"too many actions": `export function handle() { return { actions: Array.from({ length: 33 }, () => ({ type: "notify", message: "x" })) }; }`,
		"missing actions":  `export function handle() { return {}; }`,
		"bad scope":        `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "key-1", scope: "monthly", idempotencyKey: "1" }] }; }`,
	} {
		if _, err := call(automationSection(), body); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// A permission the manifest did not declare cannot be used.
	narrow := automationSection()
	narrow.Permissions = []string{ActionNotify}
	if _, err := call(narrow, `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "key-1", scope: "total", idempotencyKey: "1" }] }; }`); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("undeclared permission: err = %v", err)
	}
	notice, err := call(narrow, `export function handle() { return { actions: [{ type: "notify", message: "hi" }] }; }`)
	if err != nil || notice.Actions[0].Level != "info" {
		t.Fatalf("notice = %+v err = %v", notice, err)
	}
}
