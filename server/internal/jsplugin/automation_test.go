package jsplugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func f64(v float64) *float64 { return &v }

func automationSection() AutomationSection {
	return AutomationSection{
		Subscribes:  []string{"oauth.quota_synced"},
		Permissions: []string{ActionAPIKeyResetUsage, ActionNotify},
		Inputs: []AutomationInputSpec{
			{Name: "account", Type: EntityOAuthConnection, Title: LocalizedText{"": "Account"}, Providers: []string{"codex"}, EventSubject: true},
			{Name: "keys", Type: EntityAPIKey, Multiple: true, Requires: EntityRequireFiniteTotalQuota},
			{Name: "threshold", Type: "integer", Minimum: f64(1), Maximum: f64(100), Default: float64(10)},
			{Name: "mode", Type: "string", Enum: []any{"a", "b"}},
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
		"unknown permission": func(s *AutomationSection) { s.Permissions = []string{"db.drop"} },
		"no inputs":          func(s *AutomationSection) { s.Inputs = nil },
		"no event subject":   func(s *AutomationSection) { s.Inputs[0].EventSubject = false },
		"two event subjects": func(s *AutomationSection) { s.Inputs[1].EventSubject = true },
		"multiple subject":   func(s *AutomationSection) { s.Inputs[0].Multiple = true },
		"parameter subject": func(s *AutomationSection) {
			s.Inputs[0].EventSubject = false
			s.Inputs[2].EventSubject = true
		},
		"subject of the wrong type": func(s *AutomationSection) {
			s.Inputs[0].EventSubject = false
			s.Inputs[1].EventSubject = true
			s.Inputs[1].Multiple = false
		},
		"unknown input type": func(s *AutomationSection) { s.Inputs[2].Type = "site" },
		"bad input name":     func(s *AutomationSection) { s.Inputs[2].Name = "Bad Name" },
		"duplicate name":     func(s *AutomationSection) { s.Inputs[2].Name = "keys" },
		"too many inputs": func(s *AutomationSection) {
			for i := 0; i < MaxAutomationInputs; i++ {
				s.Inputs = append(s.Inputs, AutomationInputSpec{Name: "p" + string(rune('a'+i)), Type: "boolean"})
			}
		},
		"unknown requirement":       func(s *AutomationSection) { s.Inputs[1].Requires = "rich" },
		"providers on an api key":   func(s *AutomationSection) { s.Inputs[1].Providers = []string{"codex"} },
		"default on an object":      func(s *AutomationSection) { s.Inputs[1].Default = "x" },
		"object options on a param": func(s *AutomationSection) { s.Inputs[2].Requires = EntityRequireFiniteTotalQuota },
		"bad default":               func(s *AutomationSection) { s.Inputs[2].Default = "high" },
		"reset without a key input": func(s *AutomationSection) { s.Inputs = []AutomationInputSpec{s.Inputs[0]} },
		"tick without schedule":     func(s *AutomationSection) { s.Subscribes = []string{EventScheduleTick} },
		"tick too often": func(s *AutomationSection) {
			s.Subscribes = []string{EventScheduleTick}
			s.Schedule.EveryMinutes = 1
		},
		"tick too rarely": func(s *AutomationSection) {
			s.Subscribes = []string{EventScheduleTick}
			s.Schedule.EveryMinutes = MaxScheduleMinutes + 1
		},
		"schedule without tick": func(s *AutomationSection) { s.Schedule.EveryMinutes = 60 },
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
	// A tick is about whatever object the binding is about, whatever its type.
	ticking := automationSection()
	ticking.Subscribes = []string{"oauth.quota_synced", EventScheduleTick}
	ticking.Schedule.EveryMinutes = 60
	if err := validateAutomationSection(ticking); err != nil {
		t.Fatalf("a schedule with ticks rejected: %v", err)
	}
	onlyTicks := automationSection()
	onlyTicks.Subscribes = []string{EventScheduleTick}
	onlyTicks.Schedule.EveryMinutes = 60
	onlyTicks.Inputs = []AutomationInputSpec{{Name: "key", Type: EntityAPIKey, EventSubject: true}}
	onlyTicks.Permissions = []string{ActionNotify}
	if err := validateAutomationSection(onlyTicks); err != nil {
		t.Fatalf("ticks about an api key rejected: %v", err)
	}
}

func TestCheckParam(t *testing.T) {
	spec := AutomationInputSpec{Name: "threshold", Type: "integer", Minimum: f64(1), Maximum: f64(100)}
	if err := checkParam(spec, float64(50)); err != nil {
		t.Fatalf("valid value rejected: %v", err)
	}
	for name, value := range map[string]any{"out of range": float64(500), "not an integer": 1.5, "not a number": "7"} {
		if err := checkParam(spec, value); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	enum := AutomationInputSpec{Name: "mode", Type: "string", Enum: []any{"a", "b"}}
	if checkParam(enum, "a") != nil || checkParam(enum, "c") == nil {
		t.Fatal("enum not enforced")
	}
	if checkParam(AutomationInputSpec{Type: "boolean"}, "yes") == nil {
		t.Fatal("a string was taken as a boolean")
	}
}

func entityMap(kind, id, name string) map[string]any {
	return map[string]any{"type": kind, "id": id, "name": name, "fields": map[string]any{}}
}

func TestCallAutomationChecksActions(t *testing.T) {
	inputs := map[string]any{
		"account": entityMap(EntityOAuthConnection, "conn-1", "owner"),
		"keys":    []any{entityMap(EntityAPIKey, "key-1", "alice")},
	}
	event := AutomationEvent{Type: "oauth.quota_synced", Subject: AutomationEntity{Type: EntityOAuthConnection, ID: "conn-1", Name: "owner"}}
	call := func(section AutomationSection, body string) (AutomationResult, error) {
		plugin := automationPlugin(t, "acme-auto", section, body)
		return plugin.CallAutomation(context.Background(), AutomationContext{Event: event.Type, Now: 1, BindingID: "b", Inputs: inputs}, event)
	}

	good, err := call(automationSection(), `export function handle(ctx, event) {
  return {
    actions: ctx.inputs.keys.map((k) => ({ type: "apikey.reset_usage", target: k.id, scope: "total", idempotencyKey: "9" })),
    state: { seen: ctx.now, account: ctx.inputs.account.name, subject: event.subject.id },
  };
}`)
	if err != nil || len(good.Actions) != 1 || good.Actions[0].Target != "key-1" ||
		good.State["account"] != "owner" || good.State["subject"] != "conn-1" {
		t.Fatalf("good = %+v err = %v", good, err)
	}

	for name, body := range map[string]string{
		"unknown target":           `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "key-9", scope: "total", idempotencyKey: "1" }] }; }`,
		"an account as the target": `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "conn-1", scope: "total", idempotencyKey: "1" }] }; }`,
		"missing scope":            `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "key-1", idempotencyKey: "1" }] }; }`,
		"missing key":              `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "key-1", scope: "total" }] }; }`,
		"unknown action":           `export function handle() { return { actions: [{ type: "db.drop" }] }; }`,
		"empty notice":             `export function handle() { return { actions: [{ type: "notify" }] }; }`,
		"unknown field":            `export function handle() { return { actions: [], cooldown: 5 }; }`,
		"too many actions":         `export function handle() { return { actions: Array.from({ length: 33 }, () => ({ type: "notify", message: "x" })) }; }`,
		"missing actions":          `export function handle() { return {}; }`,
		"bad scope":                `export function handle() { return { actions: [{ type: "apikey.reset_usage", target: "key-1", scope: "monthly", idempotencyKey: "1" }] }; }`,
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

func TestLocalizedTextAcceptsAStringOrAnObjectAndRoundTrips(t *testing.T) {
	var form AutomationForm
	raw := `{"title":"Resets","description":{"zh":"重置","en":"Reset"},"addLabel":{"jp":"追加"}}`
	if err := json.Unmarshal([]byte(raw), &form); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if form.Title.Default() != "Resets" || form.Description["zh"] != "重置" || form.Description.Default() != "Reset" || form.AddLabel.Default() != "追加" {
		t.Fatalf("form = %+v", form)
	}
	out, err := json.Marshal(form)
	if err != nil {
		t.Fatal(err)
	}
	// A plain string stays a plain string, so the manifest stored for the page reads as written.
	var back map[string]json.RawMessage
	if err := json.Unmarshal(out, &back); err != nil || string(back["title"]) != `"Resets"` {
		t.Fatalf("encoded = %s", out)
	}
	if _, present := back["emptyText"]; present {
		t.Fatalf("an unset text was written out: %s", out)
	}
	if err := json.Unmarshal([]byte(`{"title":3}`), &form); err == nil {
		t.Fatal("a number was taken as text")
	}
}

func TestAutomationFormTextIsValidated(t *testing.T) {
	long := strings.Repeat("x", maxTextBytes+1)
	bad := map[string]func(*AutomationSection){
		"too long":       func(s *AutomationSection) { s.Inputs[0].Title = LocalizedText{"": long} },
		"not a language": func(s *AutomationSection) { s.Inputs[0].Description = LocalizedText{"中文": "x"} },
		"too many": func(s *AutomationSection) {
			s.Form.Title = LocalizedText{"aa": "x", "bb": "x", "cc": "x", "dd": "x", "ee": "x", "ff": "x", "gg": "x", "hh": "x", "ii": "x"}
		},
		"long placeholder": func(s *AutomationSection) { s.Inputs[1].Placeholder = LocalizedText{"": long} },
		"long form text":   func(s *AutomationSection) { s.Form.EmptyText = LocalizedText{"": long} },
	}
	for name, mutate := range bad {
		section := automationSection()
		mutate(&section)
		if err := validateAutomationSection(section); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	good := automationSection()
	good.Form = AutomationForm{Title: LocalizedText{"zh": "自动化", "en": "Automations", "zh-CN": "自动化"}, AddLabel: LocalizedText{"": "Add"}}
	good.Inputs[0].Title = LocalizedText{"zh": "账号", "en": "Account"}
	good.Inputs[1].Placeholder = LocalizedText{"": "Pick keys"}
	if err := validateAutomationSection(good); err != nil {
		t.Fatalf("localized copy rejected: %v", err)
	}
}
