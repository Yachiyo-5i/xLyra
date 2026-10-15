package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Statuses of a queued automation event.
const (
	JSPluginEventPending = "pending"
	JSPluginEventDone    = "done"
	JSPluginEventFailed  = "failed"
	JSPluginEventSkipped = "skipped"
)

// Outcomes recorded in the action log.
const (
	JSPluginActionApplied = "applied"
	JSPluginActionSkipped = "skipped"
	JSPluginActionFailed  = "failed"
)

// JSPluginBindingInput is an object an automation binding picked for one of the
// inputs its plugin declares. Applied remembers, per usage scope, the idempotency
// key of the last action carried out on it, so a repeated event does nothing.
type JSPluginBindingInput struct {
	BindingID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	InputName    string
	EntityType   string `gorm:"primaryKey"`
	EntityID     string `gorm:"primaryKey"`
	EventSubject bool
	Applied      JSON `gorm:"type:jsonb"`
}

func (JSPluginBindingInput) TableName() string { return "js_plugin_binding_inputs" }

// JSPluginEvent is one event queued for one binding.
type JSPluginEvent struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	BindingID     uuid.UUID `gorm:"type:uuid"`
	EventType     string
	Payload       JSON `gorm:"type:jsonb"`
	Status        string
	Attempts      int
	LastError     string
	NextAttemptAt time.Time
	CreatedAt     time.Time
	FinishedAt    *time.Time
}

// JSPluginActionLog is one thing a plugin asked for and how it went.
type JSPluginActionLog struct {
	ID        int64         `gorm:"primaryKey;autoIncrement" json:"id"`
	BindingID uuid.NullUUID `gorm:"type:uuid" json:"binding_id"`
	PluginID  string        `json:"plugin_id"`
	Version   string        `json:"version"`
	EventType string        `json:"event_type"`
	Action    JSON          `gorm:"type:jsonb" json:"action"`
	TargetID  string        `json:"target_id"`
	Status    string        `json:"status"`
	Detail    string        `json:"detail"`
	CreatedAt time.Time     `json:"created_at"`
}

func (JSPluginActionLog) TableName() string { return "js_plugin_action_log" }

// CreateAutomationBinding stores an automation binding with its picked objects
// in one transaction. The binding's slot is its own id, so a plugin can be bound
// as many times as an admin likes.
func (r JSPluginRepository) CreateAutomationBinding(ctx context.Context, binding JSPluginBinding, inputs []JSPluginBindingInput) (JSPluginBinding, error) {
	if binding.ID == uuid.Nil {
		binding.ID = uuid.New()
	}
	binding.Slot = binding.ID.String()
	binding.ScopeType = JSPluginScopeAutomation
	binding.ScopeID = binding.ID.String()
	if len(binding.Config) == 0 {
		binding.Config = JSON("{}")
	}
	if len(binding.Events) == 0 {
		binding.Events = JSON("[]")
	}
	binding.State = JSON("{}")
	if jsonArrayHas(binding.Events, "schedule.tick") {
		now := time.Now()
		binding.NextTickAt = &now
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&binding).Error; err != nil {
			return err
		}
		return replaceInputs(tx, binding.ID, inputs)
	})
	return binding, err
}

// UpdateAutomationBinding replaces a binding's parameters and picked objects.
// Objects that stay keep their idempotency record.
func (r JSPluginRepository) UpdateAutomationBinding(ctx context.Context, id uuid.UUID, config JSON, inputs []JSPluginBindingInput) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&JSPluginBinding{}).Where("id = ? AND scope_type = ?", id, JSPluginScopeAutomation).
			Update("config", config)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return replaceInputs(tx, id, inputs)
	})
}

func replaceInputs(tx *gorm.DB, bindingID uuid.UUID, inputs []JSPluginBindingInput) error {
	var existing []JSPluginBindingInput
	if err := tx.Where("binding_id = ?", bindingID).Find(&existing).Error; err != nil {
		return err
	}
	keep := map[[2]string]bool{}
	for _, input := range inputs {
		keep[[2]string{input.EntityType, input.EntityID}] = true
		if err := tx.Exec(`
			INSERT INTO js_plugin_binding_inputs (binding_id, input_name, entity_type, entity_id, event_subject)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (binding_id, entity_type, entity_id) DO UPDATE SET
				input_name = EXCLUDED.input_name, event_subject = EXCLUDED.event_subject
		`, bindingID, input.InputName, input.EntityType, input.EntityID, input.EventSubject).Error; err != nil {
			return err
		}
	}
	for _, row := range existing {
		if keep[[2]string{row.EntityType, row.EntityID}] {
			continue
		}
		if err := tx.Where("binding_id = ? AND entity_type = ? AND entity_id = ?", bindingID, row.EntityType, row.EntityID).
			Delete(&JSPluginBindingInput{}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r JSPluginRepository) GetBinding(ctx context.Context, id uuid.UUID) (JSPluginBinding, error) {
	var item JSPluginBinding
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&item).Error
	return item, err
}

// ListAutomationBindings returns the automation bindings of one plugin.
func (r JSPluginRepository) ListAutomationBindings(ctx context.Context, pluginID string) ([]JSPluginBinding, error) {
	var items []JSPluginBinding
	err := r.db.WithContext(ctx).
		Where("plugin_id = ? AND scope_type = ?", pluginID, JSPluginScopeAutomation).
		Order("created_at ASC").Find(&items).Error
	return items, err
}

// ListBindingInputs returns the objects a binding picked.
func (r JSPluginRepository) ListBindingInputs(ctx context.Context, bindingID uuid.UUID) ([]JSPluginBindingInput, error) {
	var items []JSPluginBindingInput
	err := r.db.WithContext(ctx).Where("binding_id = ?", bindingID).Order("input_name ASC, entity_id ASC").Find(&items).Error
	return items, err
}

// DeleteAutomationBinding removes a binding with its inputs and queued events.
func (r JSPluginRepository) DeleteAutomationBinding(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Where("id = ? AND scope_type = ?", id, JSPluginScopeAutomation).Delete(&JSPluginBinding{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// SetAutomationEvents refreshes the events every automation binding of a
// plugin is called for, after a new version changed what it subscribes to.
func (r JSPluginRepository) SetAutomationEvents(ctx context.Context, pluginID string, events []string) error {
	raw, err := json.Marshal(events)
	if err != nil {
		return err
	}
	// A binding that now subscribes to ticks is due at once; one that no longer
	// does has nothing scheduled.
	return r.db.WithContext(ctx).Exec(`
		UPDATE js_plugin_bindings SET events = ?::jsonb,
			next_tick_at = CASE WHEN jsonb_exists(?::jsonb, 'schedule.tick') THEN COALESCE(next_tick_at, NOW()) ELSE NULL END
		WHERE plugin_id = ? AND scope_type = ?`,
		string(raw), string(raw), pluginID, JSPluginScopeAutomation).Error
}

// DueTickBindings returns the bindings whose schedule.tick is due.
func (r JSPluginRepository) DueTickBindings(ctx context.Context, now time.Time, limit int) ([]JSPluginBinding, error) {
	var items []JSPluginBinding
	err := r.db.WithContext(ctx).
		Where("scope_type = ? AND next_tick_at IS NOT NULL AND next_tick_at <= ? AND jsonb_exists(events, ?)",
			JSPluginScopeAutomation, now, "schedule.tick").
		Order("next_tick_at ASC").Limit(limit).Find(&items).Error
	return items, err
}

// QueueTick queues one event for one binding and schedules its next tick, in
// one transaction, so a tick is never queued twice for the same slot of time.
func (r JSPluginRepository) QueueTick(ctx context.Context, bindingID uuid.UUID, eventType string, payload JSON, next time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO js_plugin_events (binding_id, event_type, payload) VALUES (?, ?, ?::jsonb)`,
			bindingID, eventType, string(payload)).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE js_plugin_bindings SET next_tick_at = ? WHERE id = ?`, next, bindingID).Error
	})
}

// PostponeTick moves a binding's next tick without queueing anything.
func (r JSPluginRepository) PostponeTick(ctx context.Context, bindingID uuid.UUID, next time.Time) error {
	return r.db.WithContext(ctx).Exec(`UPDATE js_plugin_bindings SET next_tick_at = ? WHERE id = ?`, next, bindingID).Error
}

func (r JSPluginRepository) SaveBindingState(ctx context.Context, id uuid.UUID, state JSON) error {
	return r.db.WithContext(ctx).Model(&JSPluginBinding{}).Where("id = ?", id).Update("state", state).Error
}

// EnqueueEvent queues one event for every automation binding that picked the
// object as the one events are about and is called for this event. It takes the
// caller's transaction so the event is stored together with the change that
// caused it. It returns the number queued.
func (r JSPluginRepository) EnqueueEvent(ctx context.Context, eventType, entityType, entityID string, payload JSON) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO js_plugin_events (binding_id, event_type, payload)
		SELECT b.id, ?, ?::jsonb FROM js_plugin_bindings b
		JOIN js_plugin_binding_inputs i ON i.binding_id = b.id AND i.event_subject
		JOIN js_plugin_versions v ON v.plugin_id = b.plugin_id AND v.version = b.version
		WHERE i.entity_type = ? AND i.entity_id = ? AND jsonb_exists(b.events, ?) AND v.status = ?
	`, eventType, string(payload), entityType, entityID, eventType, JSPluginStatusEnabled)
	return result.RowsAffected, result.Error
}

// DueEvents returns pending events whose time has come, oldest first.
func (r JSPluginRepository) DueEvents(ctx context.Context, now time.Time, limit int) ([]JSPluginEvent, error) {
	var items []JSPluginEvent
	err := r.db.WithContext(ctx).
		Where("status = ? AND next_attempt_at <= ?", JSPluginEventPending, now).
		Order("id ASC").Limit(limit).Find(&items).Error
	return items, err
}

// FinishEvent closes an event as done, failed or skipped.
func (r JSPluginRepository) FinishEvent(ctx context.Context, id int64, status, detail string) error {
	return r.db.WithContext(ctx).Exec(
		`UPDATE js_plugin_events SET status = ?, last_error = ?, attempts = attempts + 1, finished_at = NOW() WHERE id = ?`,
		status, detail, id).Error
}

// RetryEvent keeps an event pending and schedules its next attempt.
func (r JSPluginRepository) RetryEvent(ctx context.Context, id int64, detail string, next time.Time) error {
	return r.db.WithContext(ctx).Exec(
		`UPDATE js_plugin_events SET attempts = attempts + 1, last_error = ?, next_attempt_at = ? WHERE id = ?`,
		detail, next, id).Error
}

// PurgeFinished deletes finished events and old log rows.
func (r JSPluginRepository) PurgeFinished(ctx context.Context, before time.Time) error {
	if err := r.db.WithContext(ctx).Exec(
		`DELETE FROM js_plugin_events WHERE status <> ? AND finished_at < ?`, JSPluginEventPending, before).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(`DELETE FROM js_plugin_action_log WHERE created_at < ?`, before).Error
}

// ApplyOnce runs fn for an object a binding picked, unless the same key was
// already applied for this scope. The record is written only after fn
// succeeds, in the same transaction that holds the object's row lock, so
// concurrent runs serialize and a failed fn is retried. It reports whether fn
// ran, and gorm.ErrRecordNotFound when the binding no longer holds the object.
func (r JSPluginRepository) ApplyOnce(ctx context.Context, bindingID uuid.UUID, entityType, entityID, scope, key string, fn func(context.Context) error) (bool, error) {
	ran := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row JSPluginBindingInput
		err := tx.Raw(`SELECT * FROM js_plugin_binding_inputs WHERE binding_id = ? AND entity_type = ? AND entity_id = ? FOR UPDATE`,
			bindingID, entityType, entityID).Scan(&row).Error
		if err != nil {
			return err
		}
		if row.BindingID == uuid.Nil {
			return gorm.ErrRecordNotFound
		}
		applied := map[string]string{}
		if len(row.Applied) > 0 {
			_ = json.Unmarshal(row.Applied, &applied)
		}
		if applied[scope] == key {
			return nil
		}
		if err := fn(ctx); err != nil {
			return err
		}
		ran = true
		applied[scope] = key
		raw, err := json.Marshal(applied)
		if err != nil {
			return err
		}
		return tx.Exec(`UPDATE js_plugin_binding_inputs SET applied = ?::jsonb WHERE binding_id = ? AND entity_type = ? AND entity_id = ?`,
			string(raw), bindingID, entityType, entityID).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	return ran, err
}

func (r JSPluginRepository) AppendActionLog(ctx context.Context, entry JSPluginActionLog) error {
	if len(entry.Action) == 0 {
		entry.Action = JSON("{}")
	}
	return r.db.WithContext(ctx).Create(&entry).Error
}

// ListActionLog returns the most recent log rows of one plugin.
func (r JSPluginRepository) ListActionLog(ctx context.Context, pluginID string, limit int) ([]JSPluginActionLog, error) {
	var items []JSPluginActionLog
	err := r.db.WithContext(ctx).Where("plugin_id = ?", pluginID).Order("id DESC").Limit(limit).Find(&items).Error
	return items, err
}

// jsonArrayHas reports whether a JSON array of strings contains want.
func jsonArrayHas(raw JSON, want string) bool {
	var items []string
	if err := json.Unmarshal(raw, &items); err != nil {
		return false
	}
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
