package jsplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

// MaxAutomationTargets is how many objects one binding may act on.
const MaxAutomationTargets = 200

// AutomationInputError is a problem with what the admin submitted.
type AutomationInputError struct{ Message string }

func (e *AutomationInputError) Error() string { return e.Message }

func inputErrorf(format string, args ...any) error {
	return &AutomationInputError{Message: fmt.Sprintf(format, args...)}
}

// AutomationBindingInput is what an admin picks when binding an automation plugin.
type AutomationBindingInput struct {
	SubjectID string
	TargetIDs []string
	Config    map[string]any
}

// AutomationTargetView is a bound object with its display name.
type AutomationTargetView struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AutomationBindingView is a binding as the admin page shows it.
type AutomationBindingView struct {
	ID           string                 `json:"id"`
	Version      string                 `json:"version"`
	SubjectType  string                 `json:"subject_type"`
	SubjectID    string                 `json:"subject_id"`
	SubjectLabel string                 `json:"subject_label"`
	Provider     string                 `json:"provider"`
	Config       map[string]any         `json:"config"`
	Targets      []AutomationTargetView `json:"targets"`
	State        map[string]any         `json:"state"`
	CreatedAt    string                 `json:"created_at"`
}

func (m *Manager) enabledAutomation(pluginID string) (*Plugin, error) {
	plugin, ok := m.catalog.Registry().ByPluginID(pluginID)
	if !ok || plugin.Manifest.Kind != KindAutomation {
		return nil, inputErrorf("plugin %q is not an enabled automation plugin", pluginID)
	}
	return plugin, nil
}

// validateAutomationInput checks the admin's choices against the plugin's
// manifest and returns what to store.
func (m *Manager) validateAutomationInput(ctx context.Context, plugin *Plugin, in AutomationBindingInput) (subject store.OAuthConnection, config store.JSON, targets []store.JSPluginBindingTarget, err error) {
	spec := plugin.Manifest.Automation.Binding
	subjectID, perr := uuid.Parse(in.SubjectID)
	if perr != nil {
		return subject, nil, nil, inputErrorf("subject_id must be a uuid")
	}
	subject, gerr := store.NewOAuthConnectionRepository(m.db.DB()).GetByID(ctx, subjectID)
	if gerr != nil {
		if errors.Is(gerr, gorm.ErrRecordNotFound) {
			return subject, nil, nil, inputErrorf("the OAuth account was not found")
		}
		return subject, nil, nil, gerr
	}
	if len(spec.Subject.Providers) > 0 && !contains(spec.Subject.Providers, subject.Provider) {
		return subject, nil, nil, inputErrorf("this plugin works with %v accounts, not %s", spec.Subject.Providers, subject.Provider)
	}

	if spec.Target.Type == "" {
		if len(in.TargetIDs) > 0 {
			return subject, nil, nil, inputErrorf("this plugin does not act on any objects, so no targets can be bound")
		}
	} else {
		if len(in.TargetIDs) == 0 {
			return subject, nil, nil, inputErrorf("pick at least one %s", spec.Target.Type)
		}
		if len(in.TargetIDs) > MaxAutomationTargets {
			return subject, nil, nil, inputErrorf("at most %d targets can be bound", MaxAutomationTargets)
		}
		ids := make([]uuid.UUID, 0, len(in.TargetIDs))
		seen := map[uuid.UUID]bool{}
		for _, raw := range in.TargetIDs {
			id, perr := uuid.Parse(raw)
			if perr != nil {
				return subject, nil, nil, inputErrorf("target %q is not a uuid", raw)
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		keys, lerr := store.NewAPIKeyRepository(m.db.DB()).ListByIDs(ctx, ids)
		if lerr != nil {
			return subject, nil, nil, lerr
		}
		if len(keys) != len(ids) {
			return subject, nil, nil, inputErrorf("some API keys were not found")
		}
		for _, key := range keys {
			if spec.Target.Requires == "finite_total_quota" && !finiteTotalQuota(key) {
				return subject, nil, nil, inputErrorf("API key %q has no finite total quota, so there is nothing to reset", key.Name)
			}
			targets = append(targets, store.JSPluginBindingTarget{TargetType: "api_key", TargetID: key.ID.String()})
		}
	}

	applied, cerr := ApplyConfig(spec.Config, in.Config)
	if cerr != nil {
		return subject, nil, nil, inputErrorf("%s", cerr.Error())
	}
	raw, merr := json.Marshal(applied)
	if merr != nil {
		return subject, nil, nil, merr
	}
	return subject, raw, targets, nil
}

// CreateAutomation binds an enabled automation plugin to an OAuth account.
func (m *Manager) CreateAutomation(ctx context.Context, pluginID string, in AutomationBindingInput) (store.JSPluginBinding, error) {
	plugin, err := m.enabledAutomation(pluginID)
	if err != nil {
		return store.JSPluginBinding{}, err
	}
	subject, config, targets, err := m.validateAutomationInput(ctx, plugin, in)
	if err != nil {
		return store.JSPluginBinding{}, err
	}
	events, _ := json.Marshal(plugin.Manifest.Automation.Subscribes)
	return m.repo.CreateAutomationBinding(ctx, store.JSPluginBinding{
		PluginID: pluginID,
		Version:  plugin.Manifest.Version,
		Kind:     KindAutomation,
		ScopeID:  subject.ID.String(),
		Config:   config,
		Events:   events,
	}, targets)
}

// UpdateAutomation changes a binding's parameters and targets. The account it
// is bound to stays; to move it, delete the binding and create another.
func (m *Manager) UpdateAutomation(ctx context.Context, pluginID string, id uuid.UUID, in AutomationBindingInput) error {
	plugin, err := m.enabledAutomation(pluginID)
	if err != nil {
		return err
	}
	binding, err := m.repo.GetBinding(ctx, id)
	if err != nil || binding.PluginID != pluginID || binding.ScopeType != store.JSPluginScopeOAuthConnection {
		return gorm.ErrRecordNotFound
	}
	in.SubjectID = binding.ScopeID
	_, config, targets, err := m.validateAutomationInput(ctx, plugin, in)
	if err != nil {
		return err
	}
	return m.repo.UpdateAutomationBinding(ctx, id, config, targets)
}

// DeleteAutomation removes a binding of this plugin.
func (m *Manager) DeleteAutomation(ctx context.Context, pluginID string, id uuid.UUID) error {
	binding, err := m.repo.GetBinding(ctx, id)
	if err != nil || binding.PluginID != pluginID {
		return gorm.ErrRecordNotFound
	}
	return m.repo.DeleteAutomationBinding(ctx, id)
}

// ListAutomations returns a plugin's bindings with their targets and saved state.
func (m *Manager) ListAutomations(ctx context.Context, pluginID string) ([]AutomationBindingView, error) {
	bindings, err := m.repo.ListAutomationBindings(ctx, pluginID)
	if err != nil {
		return nil, err
	}
	conns := store.NewOAuthConnectionRepository(m.db.DB())
	keys := store.NewAPIKeyRepository(m.db.DB())
	out := make([]AutomationBindingView, 0, len(bindings))
	for _, binding := range bindings {
		view := AutomationBindingView{
			ID:          binding.ID.String(),
			Version:     binding.Version,
			SubjectType: binding.ScopeType,
			SubjectID:   binding.ScopeID,
			Config:      jsonObject(binding.Config),
			State:       jsonObject(binding.State),
			Targets:     []AutomationTargetView{},
			CreatedAt:   binding.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if id, perr := uuid.Parse(binding.ScopeID); perr == nil {
			if conn, gerr := conns.GetByID(ctx, id); gerr == nil {
				view.SubjectLabel, view.Provider = conn.Email, conn.Provider
			}
		}
		targets, err := m.repo.ListBindingTargets(ctx, binding.ID)
		if err != nil {
			return nil, err
		}
		var ids []uuid.UUID
		for _, target := range targets {
			if id, perr := uuid.Parse(target.TargetID); perr == nil {
				ids = append(ids, id)
			}
		}
		names := map[string]string{}
		if len(ids) > 0 {
			rows, err := keys.ListByIDs(ctx, ids)
			if err != nil {
				return nil, err
			}
			for _, row := range rows {
				names[row.ID.String()] = row.Name
			}
		}
		for _, target := range targets {
			view.Targets = append(view.Targets, AutomationTargetView{Type: target.TargetType, ID: target.TargetID, Name: names[target.TargetID]})
		}
		out = append(out, view)
	}
	return out, nil
}

// ActionLog returns the latest things a plugin asked xLyra to do.
func (m *Manager) ActionLog(ctx context.Context, pluginID string, limit int) ([]store.JSPluginActionLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return m.repo.ListActionLog(ctx, pluginID, limit)
}
