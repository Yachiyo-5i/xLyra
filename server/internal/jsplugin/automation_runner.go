package jsplugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

const (
	automationPollInterval = 2 * time.Second
	automationBatch        = 50
	automationMaxAttempts  = 4
	automationKeepFinished = 14 * 24 * time.Hour
	automationStateLimit   = 8 << 10
)

// automationRetryDelays is the wait before the 2nd, 3rd and 4th attempt.
var automationRetryDelays = []time.Duration{5 * time.Second, 30 * time.Second, 5 * time.Minute}

// AutomationHost carries out the actions a plugin asks for. xLyra implements it,
// so the plugin package never touches keys or quotas itself.
type AutomationHost interface {
	// ResetAPIKeyQuota clears the used amount of the given windows (total,
	// daily, weekly) of one API key.
	ResetAPIKeyQuota(ctx context.Context, id uuid.UUID, scopes []string) error
}

// EmittedEvent is something that happened in xLyra that automation plugins may
// be called for.
type EmittedEvent struct {
	Type     string
	Subject  AutomationSubject
	Previous map[string]any
	Current  map[string]any
}

// EmitEvent queues the event for every automation binding on its subject that
// subscribes to it. Pass the transaction that stores the change, so the event
// and the change are saved together and a restart cannot lose one.
func (m *Manager) EmitEvent(ctx context.Context, tx *gorm.DB, event EmittedEvent) error {
	if m == nil {
		return nil
	}
	if event.Current == nil {
		event.Current = map[string]any{}
	}
	payload, err := json.Marshal(map[string]any{
		"type":     event.Type,
		"subject":  encodeValue(event.Subject),
		"previous": event.Previous,
		"current":  event.Current,
	})
	if err != nil {
		return err
	}
	queued, err := store.NewJSPluginRepository(tx).EnqueueEvent(ctx, event.Type, event.Subject.ID, payload)
	if err != nil {
		return err
	}
	if queued > 0 {
		select {
		case m.wakeup <- struct{}{}:
		default:
		}
	}
	return nil
}

// StartAutomation runs the loop that hands queued events to their plugins and
// carries out the actions they return, until ctx ends. xLyra runs as one
// process, so one loop is the only consumer.
func (m *Manager) StartAutomation(ctx context.Context, host AutomationHost) {
	go func() {
		ticker := time.NewTicker(automationPollInterval)
		defer ticker.Stop()
		lastPurge := time.Time{}
		for {
			for m.processDue(ctx, host) >= automationBatch {
			}
			if time.Since(lastPurge) > time.Hour {
				lastPurge = time.Now()
				if err := m.repo.PurgeFinished(ctx, time.Now().Add(-automationKeepFinished)); err != nil && ctx.Err() == nil {
					slog.Warn("js plugin event purge failed", "error", err)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-m.wakeup:
			}
		}
	}()
}

// processDue handles the events that are due and returns how many it took.
func (m *Manager) processDue(ctx context.Context, host AutomationHost) int {
	if ctx.Err() != nil {
		return 0
	}
	events, err := m.repo.DueEvents(ctx, time.Now(), automationBatch)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("js plugin event poll failed", "error", err)
		}
		return 0
	}
	for _, event := range events {
		m.handleEvent(ctx, host, event)
	}
	return len(events)
}

func (m *Manager) handleEvent(ctx context.Context, host AutomationHost, event store.JSPluginEvent) {
	status, detail, retry := m.runEvent(ctx, host, event)
	var err error
	switch {
	case retry && event.Attempts+1 < automationMaxAttempts:
		delay := automationRetryDelays[min(event.Attempts, len(automationRetryDelays)-1)]
		err = m.repo.RetryEvent(ctx, event.ID, detail, time.Now().Add(delay))
	case retry:
		err = m.repo.FinishEvent(ctx, event.ID, store.JSPluginEventFailed, detail)
	default:
		err = m.repo.FinishEvent(ctx, event.ID, status, detail)
	}
	if err != nil && ctx.Err() == nil {
		slog.Warn("js plugin event update failed", "event_id", event.ID, "error", err)
	}
}

// runEvent calls the plugin for one event and carries out its actions. retry
// reports a failure worth trying again; actions already applied are skipped on
// the next attempt because of their idempotency keys.
func (m *Manager) runEvent(ctx context.Context, host AutomationHost, event store.JSPluginEvent) (status, detail string, retry bool) {
	binding, err := m.repo.GetBinding(ctx, event.BindingID)
	if err != nil {
		return store.JSPluginEventSkipped, "binding no longer exists", false
	}
	plugin, ok := m.catalog.Registry().ByPluginID(binding.PluginID)
	if !ok || plugin.Manifest.Kind != KindAutomation {
		return store.JSPluginEventSkipped, "plugin is not enabled", false
	}
	version, err := m.repo.GetVersion(ctx, binding.PluginID, plugin.Manifest.Version)
	if err != nil {
		return "", err.Error(), true
	}
	granted := grantedSet(version.GrantedPermissions)

	input, err := m.buildEvent(ctx, binding, event)
	if err != nil {
		return "", err.Error(), true
	}
	config, err := ApplyConfig(plugin.Manifest.Automation.Binding.Config, jsonObject(binding.Config))
	if err != nil {
		m.logAction(ctx, binding, plugin, event, AutomationAction{Type: "handle"}, "", store.JSPluginActionFailed, "binding config is no longer valid: "+err.Error())
		return store.JSPluginEventFailed, err.Error(), false
	}
	result, err := plugin.CallAutomation(ctx, AutomationContext{
		Event:     event.EventType,
		Now:       time.Now().UnixMilli(),
		BindingID: binding.ID.String(),
		Config:    config,
		State:     jsonObject(binding.State),
	}, input)
	if err != nil {
		m.logAction(ctx, binding, plugin, event, AutomationAction{Type: "handle"}, "", store.JSPluginActionFailed, err.Error())
		return "", err.Error(), true
	}

	failed := ""
	for _, action := range result.Actions {
		if !granted[action.Type] {
			m.logAction(ctx, binding, plugin, event, action, action.Target, store.JSPluginActionFailed, "permission "+action.Type+" was not granted")
			continue
		}
		outcome, note, err := m.performAction(ctx, host, binding, action)
		if err != nil {
			m.logAction(ctx, binding, plugin, event, action, action.Target, store.JSPluginActionFailed, err.Error())
			failed = err.Error()
			continue
		}
		m.logAction(ctx, binding, plugin, event, action, action.Target, outcome, note)
	}
	if failed != "" {
		return "", failed, true
	}
	if result.State != nil {
		raw, err := json.Marshal(result.State)
		if err == nil && len(raw) <= automationStateLimit {
			if err := m.repo.SaveBindingState(ctx, binding.ID, raw); err != nil {
				return "", err.Error(), true
			}
		} else {
			m.logAction(ctx, binding, plugin, event, AutomationAction{Type: "handle"}, "", store.JSPluginActionFailed, "state is larger than 8 KiB and was not saved")
		}
	}
	return store.JSPluginEventDone, "", false
}

// performAction carries out one granted action.
func (m *Manager) performAction(ctx context.Context, host AutomationHost, binding store.JSPluginBinding, action AutomationAction) (outcome, note string, err error) {
	switch action.Type {
	case ActionNotify:
		attrs := []any{"plugin_id", binding.PluginID, "binding_id", binding.ID.String(), "message", action.Message}
		if action.Level == "warn" {
			slog.Warn("js plugin notice", attrs...)
		} else {
			slog.Info("js plugin notice", attrs...)
		}
		return store.JSPluginActionApplied, "", nil
	case ActionAPIKeyResetUsage:
		keyID, err := uuid.Parse(action.Target)
		if err != nil {
			return "", "", fmt.Errorf("target %q is not an API key id", action.Target)
		}
		if host == nil {
			return "", "", errors.New("no host to carry out the action")
		}
		ran, err := m.repo.ApplyOnce(ctx, binding.ID, "api_key", action.Target, action.Scope, action.IdempotencyKey, func(ctx context.Context) error {
			return host.ResetAPIKeyQuota(ctx, keyID, []string{action.Scope})
		})
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			return store.JSPluginActionSkipped, "target is no longer bound", nil
		case err != nil:
			return "", "", err
		case !ran:
			return store.JSPluginActionSkipped, "already applied for this key", nil
		}
		return store.JSPluginActionApplied, "", nil
	}
	return "", "", fmt.Errorf("unknown action %q", action.Type)
}

// buildEvent reads the queued payload and attaches the bound targets with their
// current usage.
func (m *Manager) buildEvent(ctx context.Context, binding store.JSPluginBinding, queued store.JSPluginEvent) (AutomationEvent, error) {
	var payload struct {
		Type     string            `json:"type"`
		Subject  AutomationSubject `json:"subject"`
		Previous map[string]any    `json:"previous"`
		Current  map[string]any    `json:"current"`
	}
	if err := json.Unmarshal(queued.Payload, &payload); err != nil {
		return AutomationEvent{}, err
	}
	event := AutomationEvent{Type: queued.EventType, Subject: payload.Subject, Previous: payload.Previous, Current: payload.Current}
	targets, err := m.repo.ListBindingTargets(ctx, binding.ID)
	if err != nil {
		return AutomationEvent{}, err
	}
	var ids []uuid.UUID
	for _, target := range targets {
		if target.TargetType != "api_key" {
			continue
		}
		if id, err := uuid.Parse(target.TargetID); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		keys, err := store.NewAPIKeyRepository(m.db.DB()).ListByIDs(ctx, ids)
		if err != nil {
			return AutomationEvent{}, err
		}
		for _, key := range keys {
			event.Targets = append(event.Targets, AutomationTarget{Type: "api_key", ID: key.ID.String(), Name: key.Name, Usage: apiKeyUsage(key)})
		}
	}
	return event, nil
}

func apiKeyUsage(key store.APIKey) map[string]any {
	usage := map[string]any{
		"totalUsed":  key.QuotaTotalUsed,
		"dailyUsed":  key.QuotaDailyUsed,
		"weeklyUsed": key.QuotaWeeklyUsed,
	}
	if finiteTotalQuota(key) {
		usage["totalLimit"] = key.QuotaLimit.Float64
	}
	return usage
}

func finiteTotalQuota(key store.APIKey) bool {
	return !key.QuotaUnlimited && key.QuotaLimit.Valid
}

func (m *Manager) logAction(ctx context.Context, binding store.JSPluginBinding, plugin *Plugin, event store.JSPluginEvent, action AutomationAction, target, status, detail string) {
	raw, _ := json.Marshal(encodeValue(action))
	entry := store.JSPluginActionLog{
		BindingID: uuid.NullUUID{UUID: binding.ID, Valid: true},
		PluginID:  plugin.Manifest.ID,
		Version:   plugin.Manifest.Version,
		EventType: event.EventType,
		Action:    raw,
		TargetID:  target,
		Status:    status,
		Detail:    truncate(strings.TrimSpace(detail), 500),
	}
	if err := m.repo.AppendActionLog(ctx, entry); err != nil && ctx.Err() == nil {
		slog.Warn("js plugin action log failed", "plugin_id", plugin.Manifest.ID, "error", err)
	}
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

func jsonObject(raw store.JSON) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func grantedSet(raw store.JSON) map[string]bool {
	var list []string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &list)
	}
	set := make(map[string]bool, len(list))
	for _, name := range list {
		set[name] = true
	}
	return set
}
