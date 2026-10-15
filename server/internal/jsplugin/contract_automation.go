package jsplugin

import (
	"fmt"
	"math"
	"net/url"
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
	ActionHTTPRequest      = "http.request"
)

// AutomationPermissions lists every action type, which doubles as the
// permission names.
var AutomationPermissions = []string{ActionAPIKeyResetUsage, ActionNotify, ActionHTTPRequest}

// AutomationResetScopes are the usage windows apikey.reset_usage may clear.
var AutomationResetScopes = []string{"total", "daily", "weekly"}

// Limits on what one handle call may return, and on a binding's inputs.
const (
	MaxAutomationActions = 32
	MaxAutomationMessage = 500
	// MaxAutomationHTTPBody and MaxAutomationHTTPHeaders bound one http.request.
	MaxAutomationHTTPBody    = 64 << 10
	MaxAutomationHTTPHeaders = 16
	MaxAutomationKey         = 128
	MaxAutomationInputs      = 16
	// MaxAutomationEntities is how many objects one binding may hold in all.
	MaxAutomationEntities = 200
)

// AutomationEntity is a built-in object a binding refers to, as much of it as a
// plugin may see. Fields depend on the type.
type AutomationEntity struct {
	Type   string         `ts:"type,doc=oauth_connection, api_key or site."`
	ID     string         `ts:"id,doc=Use this as an action's target."`
	Name   string         `ts:"name,doc=Human-readable name: the account email, the key name."`
	Fields map[string]any `ts:"fields,type=JsonObject,doc=oauth_connection: provider, status. api_key: totalUsed, totalLimit, dailyUsed, weeklyUsed. site: siteType, enabled." max:"32"`
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
	Type           string            `ts:"type,type=AutomationActionType" enum:"apikey.reset_usage,notify,http.request"`
	Target         string            `ts:"target,optional,doc=apikey.reset_usage: the id of an API key among the picked inputs."`
	Scope          string            `ts:"scope,optional,type=AutomationResetScope,doc=apikey.reset_usage: which usage window to clear." enum:"total,daily,weekly"`
	IdempotencyKey string            `ts:"idempotencyKey,optional,doc=apikey.reset_usage and http.request: repeating the same key for the same target (or event object) does nothing. Use the upstream reset time, or the moment of the alert."`
	Level          string            `ts:"level,optional,type=AutomationNoticeLevel,doc=notify: defaults to info." enum:"info,warn"`
	Message        string            `ts:"message,optional,doc=notify: shown in the plugin's action log, at most 500 bytes."`
	Method         string            `ts:"method,optional,doc=http.request: GET, POST (default), PUT, PATCH or DELETE."`
	URL            string            `ts:"url,optional,doc=http.request: where to send it. Its origin (scheme, host and port) must be that of a url input the admin filled in."`
	Headers        map[string]string `ts:"headers,optional,doc=http.request: at most 16 headers; Host, Content-Length, Transfer-Encoding, Connection, Proxy-Authorization and X-Forwarded-* are dropped."`
	Body           string            `ts:"body,optional,doc=http.request: the request body as text (JSON.stringify an object and set Content-Type), at most 64 KiB; not sent with GET."`
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

// httpMethods are what http.request may use.
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// httpOrigin returns scheme://host[:port] of an http(s) URL, or an error saying
// what is wrong with it. URLs with credentials are refused.
func httpOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("must be an http(s) address")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("must not contain a user name or password")
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), nil
}

// allowedOrigins are the destinations an http.request may reach: the origins of
// the url inputs the admin filled in.
func allowedOrigins(specs []AutomationInputSpec, values map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, spec := range specs {
		if spec.Type != "url" {
			continue
		}
		if raw, ok := values[spec.Name].(string); ok {
			if origin, err := httpOrigin(raw); err == nil {
				out[origin] = true
			}
		}
	}
	return out
}

// validateAutomationResult checks what the field tags cannot: that each action
// has what its type needs, that its type was declared as a permission, that its
// target is one of the objects the admin picked, and that a request goes only
// where the admin pointed the plugin.
func validateAutomationResult(result *AutomationResult, declared []string, picked []AutomationEntity, specs []AutomationInputSpec, values map[string]any) error {
	keys := map[string]bool{}
	for _, entity := range picked {
		if entity.Type == EntityAPIKey {
			keys[entity.ID] = true
		}
	}
	origins := allowedOrigins(specs, values)
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
		case ActionHTTPRequest:
			if err := validateHTTPAction(action, origins); err != nil {
				return shapeError("%s.%s", path, err.Error())
			}
		}
	}
	return nil
}

func validateHTTPAction(action *AutomationAction, origins map[string]bool) error {
	origin, err := httpOrigin(action.URL)
	if err != nil {
		return fmt.Errorf("url: %w", err)
	}
	if !origins[origin] {
		return fmt.Errorf("url: %s is not the address of a url input the admin filled in", origin)
	}
	if action.Method == "" {
		action.Method = "POST"
	}
	action.Method = strings.ToUpper(action.Method)
	if !contains(httpMethods, action.Method) {
		return fmt.Errorf("method: %q is not one of %s", action.Method, strings.Join(httpMethods, ", "))
	}
	if len(action.Headers) > MaxAutomationHTTPHeaders {
		return fmt.Errorf("headers: at most %d", MaxAutomationHTTPHeaders)
	}
	if len(action.Body) > MaxAutomationHTTPBody {
		return fmt.Errorf("body: at most %d bytes", MaxAutomationHTTPBody)
	}
	if action.IdempotencyKey == "" || len(action.IdempotencyKey) > MaxAutomationKey {
		return fmt.Errorf("idempotencyKey: required for %s, at most %d bytes", action.Type, MaxAutomationKey)
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
// built-in object type (oauth_connection, api_key, site), which the admin picks
// from a list, or a parameter type (string, url, number, integer, boolean).
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

	// Control picks how the console draws the input; see allowedControls. Left
	// out, it follows from the type: an object is a select (a multi-select when
	// Multiple), a boolean a checkbox, a parameter with enum a select, any
	// other parameter an input.
	Control string `json:"control,omitempty"`

	// Objects only.
	Multiple bool `json:"multiple,omitempty"`
	// EventSubject marks the input an event is about. Exactly one object input
	// must carry it; events reach the bindings that picked the object. With
	// Multiple, a binding is about every object picked, and each event (and
	// each schedule.tick) is about one of them.
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
	paramTypes       = []string{"string", "url", "number", "integer", "boolean"}
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
	hasAPIKey, hasURL := false, false
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
		switch input.Type {
		case EntityAPIKey:
			hasAPIKey = true
		case "url":
			hasURL = true
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
	if contains(section.Permissions, ActionHTTPRequest) && !hasURL {
		return fmt.Errorf("automation.permissions: %s needs an input of type \"url\" for the admin to say where requests may go", ActionHTTPRequest)
	}
	return nil
}

// Controls an input may ask for, by what it holds. The first is the default.
const (
	ControlSelect      = "select"
	ControlMultiSelect = "multi_select"
	ControlRadio       = "radio"
	ControlInput       = "input"
	ControlPassword    = "password"
	ControlTextarea    = "textarea"
	ControlCheckbox    = "checkbox"
	ControlSwitch      = "switch"
)

// allowedControls lists the controls that fit an input, default first.
func allowedControls(input *AutomationInputSpec) []string {
	switch {
	case input.IsEntity() && input.Multiple:
		return []string{ControlMultiSelect}
	case input.IsEntity():
		return []string{ControlSelect}
	case input.Type == "boolean":
		return []string{ControlCheckbox, ControlSwitch}
	case len(input.Enum) > 0:
		return []string{ControlSelect, ControlRadio}
	case input.Type == "url":
		return []string{ControlInput, ControlPassword}
	case input.Type == "string":
		return []string{ControlInput, ControlPassword, ControlTextarea}
	}
	return []string{ControlInput}
}

func validateInputSpec(input *AutomationInputSpec) error {
	if input.Control != "" {
		if allowed := allowedControls(input); !contains(allowed, input.Control) {
			return fmt.Errorf("control %q does not fit this input (use one of: %s)", input.Control, strings.Join(allowed, ", "))
		}
	}
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
	case "url":
		raw, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected an http(s) address")
		}
		if _, err := httpOrigin(raw); err != nil {
			return err
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
