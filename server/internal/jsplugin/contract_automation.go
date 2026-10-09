package jsplugin

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The automation kind reacts to events xLyra emits. The plugin only decides;
// it returns declarative actions and xLyra validates and performs them, so the
// plugin never touches keys, quotas or the database itself.

// AutomationEventSubjects maps each event a plugin may subscribe to onto the
// subject type its bindings attach to.
var AutomationEventSubjects = map[string]string{
	"oauth.quota_synced": "oauth_connection",
	EventScheduleTick:    anySubject,
}

// EventScheduleTick is emitted for each binding on the interval the plugin
// declares in automation.schedule, whether or not anything changed. It suits
// work that depends on time rather than on a change.
const EventScheduleTick = "schedule.tick"

// anySubject marks an event that is not about one kind of object.
const anySubject = "*"

// Bounds of automation.schedule.everyMinutes: a tick is a poll, not a timer.
const (
	MinScheduleMinutes = 5
	MaxScheduleMinutes = 7 * 24 * 60
)

// Automation action types. A plugin must declare each one it returns in
// automation.permissions, and an admin must grant it when enabling the version.
const (
	ActionAPIKeyResetUsage = "apikey.reset_usage"
	ActionNotify           = "notify"
)

// AutomationPermissions lists every action type, which doubles as the
// permission names.
var AutomationPermissions = []string{ActionAPIKeyResetUsage, ActionNotify}

// AutomationResetScopes are the usage windows apikey.reset_usage may clear.
var AutomationResetScopes = []string{"total", "daily", "weekly"}

// AutomationTargetTypes are the objects a binding can act on.
var AutomationTargetTypes = []string{"api_key"}

// Limits on what one handle call may return.
const (
	MaxAutomationActions = 32
	MaxAutomationMessage = 500
	MaxAutomationKey     = 128
)

// AutomationContext is passed to a handle hook.
type AutomationContext struct {
	Event     string         `ts:"event,doc=The event type being handled."`
	Now       int64          `ts:"now,doc=Unix time in milliseconds."`
	BindingID string         `ts:"bindingId"`
	Config    map[string]any `ts:"config,type=JsonObject,doc=The admin's parameters for this binding, validated against automation.binding.config." max:"64"`
	State     map[string]any `ts:"state,type=JsonObject,doc=What this binding returned as state last time; empty at first." max:"64"`
}

// AutomationSubject is the object the event happened to.
type AutomationSubject struct {
	Type     string `ts:"type"`
	ID       string `ts:"id"`
	Provider string `ts:"provider,optional"`
	Label    string `ts:"label,optional,doc=Human-readable name, such as the account email."`
}

// AutomationTarget is an object the binding may act on, with its current usage.
type AutomationTarget struct {
	Type  string         `ts:"type"`
	ID    string         `ts:"id,doc=Use this as an action's target."`
	Name  string         `ts:"name"`
	Usage map[string]any `ts:"usage,type=JsonObject,doc=For api_key: totalUsed, totalLimit, dailyUsed, weeklyUsed." max:"32"`
}

// AutomationEvent is what happened, with the snapshots needed to tell what changed.
type AutomationEvent struct {
	Type     string             `ts:"type"`
	Subject  AutomationSubject  `ts:"subject"`
	Previous map[string]any     `ts:"previous,optional,type=JsonObject,doc=The state before the change; absent the first time." max:"64"`
	Current  map[string]any     `ts:"current,type=JsonObject,doc=The state after the change." max:"64"`
	Targets  []AutomationTarget `ts:"targets,doc=The objects an admin bound to this subject." max:"200"`
}

// AutomationAction is one thing a plugin asks xLyra to do.
type AutomationAction struct {
	Type           string `ts:"type,type=AutomationActionType" enum:"apikey.reset_usage,notify"`
	Target         string `ts:"target,optional,doc=apikey.reset_usage: the id of one of event.targets."`
	Scope          string `ts:"scope,optional,type=AutomationResetScope,doc=apikey.reset_usage: which usage window to clear." enum:"total,daily,weekly"`
	IdempotencyKey string `ts:"idempotencyKey,optional,doc=apikey.reset_usage: repeating the same key for the same target and scope does nothing. Use the upstream reset time."`
	Level          string `ts:"level,optional,type=AutomationNoticeLevel,doc=notify: defaults to info." enum:"info,warn"`
	Message        string `ts:"message,optional,doc=notify: shown in the plugin's action log, at most 500 bytes."`
}

// AutomationResult is the return value of a handle hook.
type AutomationResult struct {
	Actions []AutomationAction `ts:"actions" max:"32"`
	State   map[string]any     `ts:"state,optional,type=JsonObject,doc=Replaces this binding's saved state. Omit to keep it. At most 8 KiB." max:"64"`
}

// validateAutomationResult checks what the field tags cannot: that each action
// has what its type needs, and that its type was declared as a permission.
func validateAutomationResult(result *AutomationResult, declared []string, targets []AutomationTarget) error {
	known := map[string]bool{}
	for _, target := range targets {
		known[target.ID] = true
	}
	for i := range result.Actions {
		action := &result.Actions[i]
		path := fmt.Sprintf("result.actions[%d]", i)
		if !contains(declared, action.Type) {
			return shapeError("%s.type: %q is not listed in automation.permissions", path, action.Type)
		}
		switch action.Type {
		case ActionAPIKeyResetUsage:
			if action.Target == "" || !known[action.Target] {
				return shapeError("%s.target: %q is not one of event.targets", path, action.Target)
			}
			if action.Scope == "" {
				return shapeError("%s.scope: required for %s", path, action.Type)
			}
			if action.IdempotencyKey == "" || len(action.IdempotencyKey) > MaxAutomationKey {
				return shapeError("%s.idempotencyKey: required for %s, at most %d bytes", path, action.Type, MaxAutomationKey)
			}
		case ActionNotify:
			if strings.TrimSpace(action.Message) == "" || len(action.Message) > MaxAutomationMessage {
				return shapeError("%s.message: required for notify, at most %d bytes", path, MaxAutomationMessage)
			}
			if action.Level == "" {
				action.Level = "info"
			}
		}
	}
	return nil
}

// ---- manifest: the automation section ----

// AutomationSection is the automation manifest block.
type AutomationSection struct {
	// Subscribes lists the events the plugin handles.
	Subscribes []string `json:"subscribes"`
	// Permissions lists the action types the plugin may return. An admin grants
	// them when enabling a version.
	Permissions []string          `json:"permissions,omitempty"`
	Binding     AutomationBinding `json:"binding"`
	// Schedule is required when the plugin subscribes to schedule.tick, and only then.
	Schedule AutomationSchedule `json:"schedule,omitempty"`
}

// AutomationSchedule says how often schedule.tick is emitted per binding.
type AutomationSchedule struct {
	EveryMinutes int `json:"everyMinutes,omitempty"`
}

// AutomationBinding describes what an admin picks when binding the plugin.
type AutomationBinding struct {
	Subject AutomationSubjectSpec `json:"subject"`
	Target  AutomationTargetSpec  `json:"target,omitempty"`
	// Config is a JSON Schema (a small subset) for the admin's parameters.
	Config map[string]any `json:"config,omitempty"`
}

// AutomationSubjectSpec says what the plugin attaches to.
type AutomationSubjectSpec struct {
	Type string `json:"type"`
	// Providers, when set, restricts the subjects to those providers.
	Providers []string `json:"providers,omitempty"`
}

// AutomationTargetSpec says what the plugin may act on. Leave Type empty for a
// plugin that only returns notices.
type AutomationTargetSpec struct {
	Type string `json:"type,omitempty"`
	// Requires is a condition every target must meet, such as finite_total_quota.
	Requires string `json:"requires,omitempty"`
}

// AutomationTargetRequirements are the values Target.Requires may take.
var AutomationTargetRequirements = []string{"finite_total_quota"}

// AutomationSubjectTypes are the objects a plugin can attach to.
var AutomationSubjectTypes = []string{"oauth_connection"}

func validateAutomationSection(section AutomationSection) error {
	if len(section.Subscribes) == 0 {
		return fmt.Errorf("automation.subscribes must list at least one event")
	}
	subject := section.Binding.Subject.Type
	if !contains(AutomationSubjectTypes, subject) {
		return fmt.Errorf("automation.binding.subject.type %q is not supported (supported: %s)", subject, strings.Join(AutomationSubjectTypes, ", "))
	}
	seen := map[string]bool{}
	for _, event := range section.Subscribes {
		want, ok := AutomationEventSubjects[event]
		if !ok {
			return fmt.Errorf("automation.subscribes: unknown event %q (supported: %s)", event, strings.Join(automationEventNames(), ", "))
		}
		if want != anySubject && want != subject {
			return fmt.Errorf("automation.subscribes: event %q is about %s, not %s", event, want, subject)
		}
		if seen[event] {
			return fmt.Errorf("automation.subscribes: duplicate event %q", event)
		}
		seen[event] = true
	}
	ticks := seen[EventScheduleTick]
	switch {
	case ticks && (section.Schedule.EveryMinutes < MinScheduleMinutes || section.Schedule.EveryMinutes > MaxScheduleMinutes):
		return fmt.Errorf("automation.schedule.everyMinutes must be between %d and %d when subscribing to %s", MinScheduleMinutes, MaxScheduleMinutes, EventScheduleTick)
	case !ticks && section.Schedule.EveryMinutes != 0:
		return fmt.Errorf("automation.schedule only applies to %s, which is not in automation.subscribes", EventScheduleTick)
	}
	for _, permission := range section.Permissions {
		if !contains(AutomationPermissions, permission) {
			return fmt.Errorf("automation.permissions: unknown permission %q (supported: %s)", permission, strings.Join(AutomationPermissions, ", "))
		}
	}
	if t := section.Binding.Target.Type; t != "" && !contains(AutomationTargetTypes, t) {
		return fmt.Errorf("automation.binding.target.type %q is not supported (supported: %s)", t, strings.Join(AutomationTargetTypes, ", "))
	}
	if r := section.Binding.Target.Requires; r != "" && !contains(AutomationTargetRequirements, r) {
		return fmt.Errorf("automation.binding.target.requires %q is not supported (supported: %s)", r, strings.Join(AutomationTargetRequirements, ", "))
	}
	if section.Binding.Target.Type == "" && contains(section.Permissions, ActionAPIKeyResetUsage) {
		return fmt.Errorf("automation.permissions: %s needs automation.binding.target.type \"api_key\"", ActionAPIKeyResetUsage)
	}
	return validateConfigSchema(section.Binding.Config)
}

func automationEventNames() []string {
	names := make([]string, 0, len(AutomationEventSubjects))
	for name := range AutomationEventSubjects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ---- binding config: a small JSON Schema subset ----
//
// The schema is an object of flat properties. Each property has a type of
// string, number, integer or boolean, and may carry title, description,
// default, enum, minimum and maximum. This is enough for an admin form and for
// xLyra to validate input without a general schema engine.

const maxConfigProperties = 16

var configPropertyTypes = []string{"string", "number", "integer", "boolean"}

func validateConfigSchema(schema map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	if t, _ := schema["type"].(string); t != "object" {
		return fmt.Errorf("automation.binding.config.type must be \"object\"")
	}
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) > maxConfigProperties {
		return fmt.Errorf("automation.binding.config has more than %d properties", maxConfigProperties)
	}
	for name, raw := range properties {
		property, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("automation.binding.config.properties.%s: expected object", name)
		}
		kind, _ := property["type"].(string)
		if !contains(configPropertyTypes, kind) {
			return fmt.Errorf("automation.binding.config.properties.%s.type %q is not supported (supported: %s)", name, kind, strings.Join(configPropertyTypes, ", "))
		}
		if def, has := property["default"]; has {
			if err := checkConfigValue(name, property, def); err != nil {
				return fmt.Errorf("automation.binding.config: default of %w", err)
			}
		}
	}
	if required, ok := schema["required"].([]any); ok {
		for _, item := range required {
			name, _ := item.(string)
			if _, has := properties[name]; !has {
				return fmt.Errorf("automation.binding.config.required: %q is not a property", name)
			}
		}
	}
	return nil
}

// ApplyConfig validates an admin's parameters against the schema and fills in
// defaults. Unknown keys are rejected.
func ApplyConfig(schema map[string]any, config map[string]any) (map[string]any, error) {
	properties, _ := schema["properties"].(map[string]any)
	out := map[string]any{}
	for key := range config {
		if _, ok := properties[key]; !ok {
			return nil, fmt.Errorf("config.%s is not a parameter of this plugin", key)
		}
	}
	for name, raw := range properties {
		property, _ := raw.(map[string]any)
		value, has := config[name]
		if !has {
			if def, hasDefault := property["default"]; hasDefault {
				out[name] = def
			}
			continue
		}
		if err := checkConfigValue(name, property, value); err != nil {
			return nil, err
		}
		out[name] = value
	}
	if required, ok := schema["required"].([]any); ok {
		for _, item := range required {
			name, _ := item.(string)
			if _, has := out[name]; !has {
				return nil, fmt.Errorf("config.%s is required", name)
			}
		}
	}
	return out, nil
}

func checkConfigValue(name string, property map[string]any, value any) error {
	kind, _ := property["type"].(string)
	switch kind {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: expected string", name)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: expected boolean", name)
		}
	case "number", "integer":
		number, ok := asFloat(value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("%s: expected number", name)
		}
		if kind == "integer" && number != math.Trunc(number) {
			return fmt.Errorf("%s: expected integer", name)
		}
		if min, ok := asFloat(property["minimum"]); ok && number < min {
			return fmt.Errorf("%s: must be at least %v", name, min)
		}
		if max, ok := asFloat(property["maximum"]); ok && number > max {
			return fmt.Errorf("%s: must be at most %v", name, max)
		}
	}
	if options, ok := property["enum"].([]any); ok && len(options) > 0 {
		for _, option := range options {
			if option == value {
				return nil
			}
		}
		return fmt.Errorf("%s: %v is not one of the allowed values", name, value)
	}
	return nil
}
