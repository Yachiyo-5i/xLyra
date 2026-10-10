package jsplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

// AutomationInputError is a problem with what the admin submitted.
type AutomationInputError struct{ Message string }

func (e *AutomationInputError) Error() string { return e.Message }

func inputErrorf(format string, args ...any) error {
	return &AutomationInputError{Message: fmt.Sprintf(format, args...)}
}

// AutomationBindingInput is what an admin submits to bind an automation plugin:
// for each input the plugin declares, the picked object id (a list of ids when
// the input is multiple) or the parameter value.
type AutomationBindingInput struct {
	Inputs map[string]any
}

// AutomationEntityView is a picked object with its display name.
type AutomationEntityView struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AutomationInputView is one input of a binding as the admin page shows it.
type AutomationInputView struct {
	Name     string                 `json:"name"`
	Entities []AutomationEntityView `json:"entities,omitempty"`
	Value    any                    `json:"value,omitempty"`
}

// AutomationBindingView is a binding as the admin page shows it.
type AutomationBindingView struct {
	ID        string                `json:"id"`
	Version   string                `json:"version"`
	Inputs    []AutomationInputView `json:"inputs"`
	State     map[string]any        `json:"state"`
	CreatedAt string                `json:"created_at"`
}

// AutomationOption is one object an admin may pick for an input. Disabled
// options are shown with the reason they cannot be picked.
type AutomationOption struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	// ReasonCode is stable, for the page to word in its own language; Reason is
	// the same in English.
	ReasonCode string `json:"reason_code,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func (m *Manager) enabledAutomation(pluginID string) (*Plugin, error) {
	plugin, ok := m.catalog.Registry().ByPluginID(pluginID)
	if !ok || plugin.Manifest.Kind != KindAutomation {
		return nil, inputErrorf("plugin %q is not an enabled automation plugin", pluginID)
	}
	return plugin, nil
}

func inputTitle(spec AutomationInputSpec) string {
	if title := spec.Title.Default(); title != "" {
		return title
	}
	return spec.Name
}

// pickedIDs reads an object input: one id, or a list of ids.
func pickedIDs(spec AutomationInputSpec, value any) ([]string, error) {
	var ids []string
	switch typed := value.(type) {
	case nil:
	case string:
		if typed != "" {
			ids = []string{typed}
		}
	case []any:
		for _, item := range typed {
			id, ok := item.(string)
			if !ok {
				return nil, inputErrorf("%s: expected ids", inputTitle(spec))
			}
			ids = append(ids, id)
		}
	case []string:
		ids = typed
	default:
		return nil, inputErrorf("%s: expected an id", inputTitle(spec))
	}
	if !spec.Multiple && len(ids) > 1 {
		return nil, inputErrorf("%s: pick one", inputTitle(spec))
	}
	return ids, nil
}

// validateAutomationInput checks the admin's choices against the inputs the
// plugin declares and returns what to store: the picked objects, and the
// parameters as JSON.
func (m *Manager) validateAutomationInput(ctx context.Context, plugin *Plugin, in AutomationBindingInput) (config store.JSON, rows []store.JSPluginBindingInput, err error) {
	specs := plugin.Manifest.Automation.Inputs
	declared := map[string]bool{}
	for _, spec := range specs {
		declared[spec.Name] = true
	}
	for name := range in.Inputs {
		if !declared[name] {
			return nil, nil, inputErrorf("%q is not an input of this plugin", name)
		}
	}

	params := map[string]any{}
	seen := map[[2]string]string{}
	for _, spec := range specs {
		value := in.Inputs[spec.Name]
		if !spec.IsEntity() {
			if value == nil {
				if spec.Default != nil {
					params[spec.Name] = spec.Default
				} else if spec.IsRequired() {
					return nil, nil, inputErrorf("%s is required", inputTitle(spec))
				}
				continue
			}
			if cerr := checkParam(spec, value); cerr != nil {
				return nil, nil, inputErrorf("%s: %s", inputTitle(spec), cerr.Error())
			}
			params[spec.Name] = value
			continue
		}

		ids, perr := pickedIDs(spec, value)
		if perr != nil {
			return nil, nil, perr
		}
		if len(ids) == 0 {
			if spec.IsRequired() {
				return nil, nil, inputErrorf("pick at least one for %s", inputTitle(spec))
			}
			continue
		}
		kind := lookupEntity(spec.Type)
		unique := make([]string, 0, len(ids))
		for _, id := range ids {
			if _, perr := uuid.Parse(id); perr != nil {
				return nil, nil, inputErrorf("%s: %q is not a uuid", inputTitle(spec), id)
			}
			unique = append(unique, id)
		}
		found, rerr := kind.Resolve(ctx, m.db, unique)
		if rerr != nil {
			return nil, nil, rerr
		}
		byID := map[string]AutomationEntity{}
		for _, entity := range found {
			byID[entity.ID] = entity
		}
		for _, id := range unique {
			entity, ok := byID[id]
			if !ok {
				return nil, nil, inputErrorf("%s: some objects were not found", inputTitle(spec))
			}
			if _, reason := kind.Check(entity, spec); reason != "" {
				return nil, nil, inputErrorf("%s: %q: %s", inputTitle(spec), entity.Name, reason)
			}
			key := [2]string{spec.Type, id}
			if previous, dup := seen[key]; dup {
				return nil, nil, inputErrorf("%q is picked for both %s and %s", entity.Name, previous, inputTitle(spec))
			}
			seen[key] = inputTitle(spec)
			rows = append(rows, store.JSPluginBindingInput{
				InputName: spec.Name, EntityType: spec.Type, EntityID: id, EventSubject: spec.EventSubject,
			})
		}
	}
	if len(rows) > MaxAutomationEntities {
		return nil, nil, inputErrorf("at most %d objects can be picked in all", MaxAutomationEntities)
	}
	raw, merr := json.Marshal(params)
	if merr != nil {
		return nil, nil, merr
	}
	return raw, rows, nil
}

// CreateAutomation binds an enabled automation plugin.
func (m *Manager) CreateAutomation(ctx context.Context, pluginID string, in AutomationBindingInput) (store.JSPluginBinding, error) {
	plugin, err := m.enabledAutomation(pluginID)
	if err != nil {
		return store.JSPluginBinding{}, err
	}
	config, rows, err := m.validateAutomationInput(ctx, plugin, in)
	if err != nil {
		return store.JSPluginBinding{}, err
	}
	events, _ := json.Marshal(plugin.Manifest.Automation.Subscribes)
	return m.repo.CreateAutomationBinding(ctx, store.JSPluginBinding{
		PluginID: pluginID,
		Version:  plugin.Manifest.Version,
		Kind:     KindAutomation,
		Config:   config,
		Events:   events,
	}, rows)
}

// UpdateAutomation replaces everything an admin picked for a binding.
func (m *Manager) UpdateAutomation(ctx context.Context, pluginID string, id uuid.UUID, in AutomationBindingInput) error {
	plugin, err := m.enabledAutomation(pluginID)
	if err != nil {
		return err
	}
	binding, err := m.repo.GetBinding(ctx, id)
	if err != nil || binding.PluginID != pluginID || binding.ScopeType != store.JSPluginScopeAutomation {
		return gorm.ErrRecordNotFound
	}
	config, rows, err := m.validateAutomationInput(ctx, plugin, in)
	if err != nil {
		return err
	}
	return m.repo.UpdateAutomationBinding(ctx, id, config, rows)
}

// DeleteAutomation removes a binding of this plugin.
func (m *Manager) DeleteAutomation(ctx context.Context, pluginID string, id uuid.UUID) error {
	binding, err := m.repo.GetBinding(ctx, id)
	if err != nil || binding.PluginID != pluginID {
		return gorm.ErrRecordNotFound
	}
	return m.repo.DeleteAutomationBinding(ctx, id)
}

// ListAutomations returns a plugin's bindings with what was picked and the saved state.
func (m *Manager) ListAutomations(ctx context.Context, pluginID string) ([]AutomationBindingView, error) {
	bindings, err := m.repo.ListAutomationBindings(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	out := make([]AutomationBindingView, 0, len(bindings))
	for _, binding := range bindings {
		rows, err := m.repo.ListBindingInputs(ctx, binding.ID)
		if err != nil {
			return nil, err
		}
		idsByType := map[string][]string{}
		for _, row := range rows {
			idsByType[row.EntityType] = append(idsByType[row.EntityType], row.EntityID)
		}
		names := map[[2]string]string{}
		for kind, ids := range idsByType {
			known := lookupEntity(kind)
			if known == nil {
				continue
			}
			entities, err := known.Resolve(ctx, m.db, ids)
			if err != nil {
				return nil, err
			}
			for _, entity := range entities {
				names[[2]string{entity.Type, entity.ID}] = entity.Name
			}
		}
		byName := map[string]*AutomationInputView{}
		var order []string
		view := func(name string) *AutomationInputView {
			if byName[name] == nil {
				byName[name] = &AutomationInputView{Name: name}
				order = append(order, name)
			}
			return byName[name]
		}
		for _, row := range rows {
			v := view(row.InputName)
			v.Entities = append(v.Entities, AutomationEntityView{Type: row.EntityType, ID: row.EntityID, Name: names[[2]string{row.EntityType, row.EntityID}]})
		}
		for name, value := range jsonObject(binding.Config) {
			view(name).Value = value
		}
		sort.Strings(order)
		inputs := make([]AutomationInputView, 0, len(order))
		for _, name := range order {
			inputs = append(inputs, *byName[name])
		}
		out = append(out, AutomationBindingView{
			ID:        binding.ID.String(),
			Version:   binding.Version,
			Inputs:    inputs,
			State:     jsonObject(binding.State),
			CreatedAt: binding.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
	return out, nil
}

// AutomationOptions lists the objects an admin may pick for one input of an
// enabled plugin, marking those the input's conditions rule out.
func (m *Manager) AutomationOptions(ctx context.Context, pluginID, inputName string) ([]AutomationOption, error) {
	plugin, err := m.enabledAutomation(pluginID)
	if err != nil {
		return nil, err
	}
	for _, spec := range plugin.Manifest.Automation.Inputs {
		if spec.Name != inputName {
			continue
		}
		kind := lookupEntity(spec.Type)
		if kind == nil {
			return nil, inputErrorf("%q is a parameter, not a list of objects", inputName)
		}
		entities, err := kind.List(ctx, m.db)
		if err != nil {
			return nil, err
		}
		options := make([]AutomationOption, 0, len(entities))
		for _, entity := range entities {
			code, reason := kind.Check(entity, spec)
			options = append(options, AutomationOption{
				ID: entity.ID, Name: entity.Name, Description: kind.Describe(entity),
				Disabled: reason != "", ReasonCode: code, Reason: reason,
			})
		}
		return options, nil
	}
	return nil, inputErrorf("%q is not an input of this plugin", inputName)
}

// ActionLog returns the latest things a plugin asked xLyra to do.
func (m *Manager) ActionLog(ctx context.Context, pluginID string, limit int) ([]store.JSPluginActionLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return m.repo.ListActionLog(ctx, pluginID, limit)
}
