-- +goose Up
-- When an automation binding that subscribes to schedule.tick is next due.
ALTER TABLE js_plugin_bindings ADD COLUMN next_tick_at TIMESTAMPTZ;
CREATE INDEX js_plugin_bindings_tick_idx ON js_plugin_bindings (next_tick_at) WHERE next_tick_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS js_plugin_bindings_tick_idx;
ALTER TABLE js_plugin_bindings DROP COLUMN IF EXISTS next_tick_at;
