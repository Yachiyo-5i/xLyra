package jsplugin

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// The automation kind reacts to events xLyra emits. The plugin only decides;
// it returns declarative actions and xLyra validates and performs them, so the
// plugin never touches keys, quotas or the database itself.
//
// What a binding needs is declared by the plugin as a list of inputs: built-in
// objects the admin picks (an OAuth account, API keys) and plain parameters.
// xLyra draws the form from that list and hands the picked values to the plugin.

// AutomationEventSubjects maps each event a plugin may subscribe to onto the
// entity type it is about, or anySubject.
var AutomationEventSubjects = map[string]string{
	"oauth.quota_synced": EntityOAuthConnection,
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

// Limits on what one handle call may return, and on a binding's inputs.
const (
	MaxAutomationActions = 32
	MaxAutomationMessage = 500
	MaxAutomationKey     = 128
	MaxAutomationInputs  = 16
	// MaxAutomationEntities is how many objects one binding may hold in all.
	MaxAutomationEntities = 200
)

// AutomationEntity is a built-in object a binding refers to, as much of it as a
// plugin may see. Fields depend on the type.
type AutomationEntity struct {
	Type   string         `ts:"type,doc=oauth_connection or api_key."`
	ID     string         `ts:"id,doc=Use this as an action's target."`
	Name   string         `ts:"name,doc=Human-readable name: the account email, the key name."`
	Fields map[string]any `ts:"fields,type=JsonObject,doc=oauth_connection: provider, status. api_key: totalUsed, totalLimit, dailyUsed, weeklyUsed." max:"32"`
}

// AutomationContext is passed to a handle hook.
type AutomationContext struct {
	Event     string         `ts:"event,doc=The event type being handled."`
	Now       int64          `ts:"now,doc=Unix time in milliseconds."`
	BindingID string         `ts:"bindingId"`
	Inputs    map[string]any `ts:"inputs,type=Record<string, AutomationInputValue>,doc=What the admin picked, by the input names declared in automation.inputs. An object input is an AutomationEntity (an array of them when multiple), a parameter is its value." max:"64"`
	State     map[string]any `ts:"state,type=JsonObject,doc=What this binding returned as state last time; empty at first." max:"64"`
}

// AutomationEvent is what happened, with the snapshots needed to tell what changed.
type AutomationEvent struct {
	Type     string           `ts:"type"`
	Subject  AutomationEntity `ts:"subject,doc=The object the event is about: the one picked for the input marked eventSubject."`
	Previous map[string]any   `ts:"previous,optional,type=JsonObject,doc=The state before the change; absent the first time." max:"64"`
	Current  map[string]any   `ts:"current,type=JsonObject,doc=The state after the change." max:"64"`
}

// AutomationAction is one thing a plugin asks xLyra to do.
type AutomationAction struct {
	Type           string `ts:"type,type=AutomationActionType" enum:"apikey.reset_usage,notify"`
	Target         string `ts:"target,optional,doc=apikey.reset_usage: the id of an API key among the picked inputs."`
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

// entitiesIn collects the objects among a binding's inputs: every map that
// carries a string type and id, alone or in an array.
func entitiesIn(inputs map[string]any) []AutomationEntity {
	var out []AutomationEntity
	add := func(value any) {
		object, ok := value.(map[string]any)
		if !ok {
			return
		}
		kind, _ := object["type"].(string)
		id, _ := object["id"].(string)
		if kind != "" && id != "" {
			out = append(out, AutomationEntity{Type: kind, ID: id})
		}
	}
	for _, value := range inputs {
		if list, ok := value.([]any); ok {
			for _, item := range list {
				add(item)
			}
			continue
		}
		add(value)
	}
	return out
}

// validateAutomationResult checks what the field tags cannot: that each action
// has what its type needs, that its type was declared as a permission, and that
// its target is one of the objects the admin picked.
func validateAutomationResult(result *AutomationResult, declared []string, picked []AutomationEntity) error {
	keys := map[string]bool{}
	for _, entity := range picked {
		if entity.Type == EntityAPIKey {
			keys[entity.ID] = true
		}
	}
	for i := range result.Actions {
		action := &result.Actions[i]
		path := fmt.Sprintf("result.actions[%d]", i)
		if !contains(declared, action.Type) {
			return shapeError("%s.type: %q is not listed in automation.permissions", path, action.Type)
		}
		switch action.Type {
		case ActionAPIKeyResetUsage:
			if action.Target == "" || !keys[action.Target] {
				return shapeError("%s.target: %q is not an API key among the inputs", path, action.Target)
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
	Permissions []string `json:"permissions,omitempty"`
	// Inputs is what an admin fills in to bind the plugin, in form order.
	Inputs []AutomationInputSpec `json:"inputs"`
	// Schedule is required when the plugin subscribes to schedule.tick, and only then.
	Schedule AutomationSchedule `json:"schedule,omitempty"`
	// Form is the copy around the binding form. Anything left out uses xLyra's
	// default wording.
	Form AutomationForm `json:"form,omitempty"`
}

// AutomationForm is the plugin's own wording for the binding form.
type AutomationForm struct {
	Title       LocalizedText `json:"title,omitempty"`
	Description LocalizedText `json:"description,omitempty"`
	// AddLabel is the text of the button that adds a binding.
	AddLabel LocalizedText `json:"addLabel,omitempty"`
	// EmptyText is shown while there are no bindings yet.
	EmptyText LocalizedText `json:"emptyText,omitempty"`
}

// AutomationSchedule says how often schedule.tick is emitted per binding.
type AutomationSchedule struct {
	EveryMinutes int `json:"everyMinutes,omitempty"`
}

// AutomationInputSpec declares one input of a binding. Type is either a
// built-in object type (oauth_connection, api_key), which the admin picks from
// a list, or a parameter type (string, number, integer, boolean).
type AutomationInputSpec struct {
	Name        string        `json:"name"`
	Type        string        `json:"type"`
	Title       LocalizedText `json:"title,omitempty"`
	Description LocalizedText `json:"description,omitempty"`
	// Placeholder is shown in an empty picker or field.
	Placeholder LocalizedText `json:"placeholder,omitempty"`
	// EmptyText is shown by an object picker when there is nothing to pick.
	EmptyText LocalizedText `json:"emptyText,omitempty"`
	// Required defaults to true for objects and false for parameters.
	Required *bool `json:"required,omitempty"`

	// Objects only.
	Multiple bool `json:"multiple,omitempty"`
	// EventSubject marks the input an event is about. Exactly one object input
	// must carry it; events reach the bindings that picked the object.
	EventSubject bool `json:"eventSubject,omitempty"`
	// Providers restricts OAuth accounts to these providers.
	Providers []string `json:"providers,omitempty"`
	// Requires is a condition every picked object must meet, such as finite_total_quota.
	Requires string `json:"requires,omitempty"`

	// Parameters only.
	Default any      `json:"default,omitempty"`
	Enum    []any    `json:"enum,omitempty"`
	Minimum *float64 `json:"minimum,omitempty"`
	Maximum *float64 `json:"maximum,omitempty"`
}

// IsEntity reports whether the input is a built-in object picker.
func (s AutomationInputSpec) IsEntity() bool { return lookupEntity(s.Type) != nil }

// IsRequired applies the default for Required.
func (s AutomationInputSpec) IsRequired() bool {
	if s.Required != nil {
		return *s.Required
	}
	return s.IsEntity()
}

var (
	inputNamePattern = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,31}$`)
	paramTypes       = []string{"string", "number", "integer", "boolean"}
)

func validateAutomationSection(section AutomationSection) error {
	if len(section.Subscribes) == 0 {
		return fmt.Errorf("automation.subscribes must list at least one event")
	}
	for label, text := range map[string]LocalizedText{
		"form.title": section.Form.Title, "form.description": section.Form.Description,
		"form.addLabel": section.Form.AddLabel, "form.emptyText": section.Form.EmptyText,
	} {
		if err := text.validate(); err != nil {
			return fmt.Errorf("automation.%s: %w", label, err)
		}
	}
	if len(section.Inputs) == 0 {
		return fmt.Errorf("automation.inputs must declare at least the object the events are about")
	}
	if len(section.Inputs) > MaxAutomationInputs {
		return fmt.Errorf("automation.inputs has more than %d entries", MaxAutomationInputs)
	}

	names := map[string]bool{}
	var subject *AutomationInputSpec
	hasAPIKey := false
	for i := range section.Inputs {
		input := &section.Inputs[i]
		if !inputNamePattern.MatchString(input.Name) {
			return fmt.Errorf("automation.inputs[%d].name %q must start with a lowercase letter and contain only letters and digits", i, input.Name)
		}
		if names[input.Name] {
			return fmt.Errorf("automation.inputs: duplicate name %q", input.Name)
		}
		names[input.Name] = true
		for label, text := range map[string]LocalizedText{
			"title": input.Title, "description": input.Description, "placeholder": input.Placeholder, "emptyText": input.EmptyText,
		} {
			if err := text.validate(); err != nil {
				return fmt.Errorf("automation.inputs.%s.%s: %w", input.Name, label, err)
			}
		}
		if err := validateInputSpec(input); err != nil {
			return fmt.Errorf("automation.inputs.%s: %w", input.Name, err)
		}
		if input.Type == EntityAPIKey {
			hasAPIKey = true
		}
		if input.EventSubject {
			if subject != nil {
				return fmt.Errorf("automation.inputs: only one input can be the eventSubject")
			}
			subject = input
		}
	}
	if subject == nil {
		return fmt.Errorf("automation.inputs: mark the object the events are about with eventSubject")
	}

	seen := map[string]bool{}
	for _, event := range section.Subscribes {
		want, ok := AutomationEventSubjects[event]
		if !ok {
			return fmt.Errorf("automation.subscribes: unknown event %q (supported: %s)", event, strings.Join(automationEventNames(), ", "))
		}
		if want != anySubject && want != subject.Type {
			return fmt.Errorf("automation.subscribes: event %q is about %s, but the eventSubject input %q is %s", event, want, subject.Name, subject.Type)
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
	if contains(section.Permissions, ActionAPIKeyResetUsage) && !hasAPIKey {
		return fmt.Errorf("automation.permissions: %s needs an input of type %q to pick the keys", ActionAPIKeyResetUsage, EntityAPIKey)
	}
	return nil
}

func validateInputSpec(input *AutomationInputSpec) error {
	if kind := lookupEntity(input.Type); kind != nil {
		if input.Default != nil || len(input.Enum) > 0 || input.Minimum != nil || input.Maximum != nil {
			return fmt.Errorf("default, enum, minimum and maximum are for parameters, not %s", input.Type)
		}
		if len(input.Providers) > 0 && !kind.SupportsProviders {
			return fmt.Errorf("providers does not apply to %s", input.Type)
		}
		if input.Requires != "" && !contains(kind.Requirements, input.Requires) {
			return fmt.Errorf("requires %q is not supported for %s (supported: %s)", input.Requires, input.Type, strings.Join(kind.Requirements, ", "))
		}
		if input.EventSubject && input.Multiple {
			return fmt.Errorf("the eventSubject cannot be multiple: each binding is about one object")
		}
		return nil
	}
	if !contains(paramTypes, input.Type) {
		return fmt.Errorf("type %q is not supported (objects: %s; parameters: %s)", input.Type, strings.Join(entityNames(), ", "), strings.Join(paramTypes, ", "))
	}
	if input.Multiple || input.EventSubject || len(input.Providers) > 0 || input.Requires != "" {
		return fmt.Errorf("multiple, eventSubject, providers and requires are for objects, not %s", input.Type)
	}
	if input.Default != nil {
		if err := checkParam(*input, input.Default); err != nil {
			return fmt.Errorf("default: %w", err)
		}
	}
	return nil
}

func automationEventNames() []string {
	names := make([]string, 0, len(AutomationEventSubjects))
	for name := range AutomationEventSubjects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// checkParam checks an admin's value for a parameter input.
func checkParam(input AutomationInputSpec, value any) error {
	switch input.Type {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "number", "integer":
		number, ok := asFloat(value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("expected number")
		}
		if input.Type == "integer" && number != math.Trunc(number) {
			return fmt.Errorf("expected integer")
		}
		if input.Minimum != nil && number < *input.Minimum {
			return fmt.Errorf("must be at least %v", *input.Minimum)
		}
		if input.Maximum != nil && number > *input.Maximum {
			return fmt.Errorf("must be at most %v", *input.Maximum)
		}
	}
	if len(input.Enum) > 0 {
		for _, option := range input.Enum {
			if option == value {
				return nil
			}
		}
		return fmt.Errorf("%v is not one of the allowed values", value)
	}
	return nil
}
