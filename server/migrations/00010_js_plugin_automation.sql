-- +goose Up
-- Storage for the automation plugin kind: what an admin granted a version, the
-- objects a binding acts on, the events waiting to be handled (an outbox, so a
-- restart never drops one) and a log of what plugins asked xLyra to do.
ALTER TABLE js_plugin_versions ADD COLUMN granted_permissions JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_scope_type_check;
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_scope_type_check
  CHECK (scope_type IN ('site', 'endpoint', 'oauth_connection'));
ALTER TABLE js_plugin_bindings ADD COLUMN events JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE js_plugin_bindings ADD COLUMN state JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE js_plugin_binding_targets (
  binding_id UUID NOT NULL REFERENCES js_plugin_bindings(id) ON DELETE CASCADE,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  -- scope -> idempotency key of the last action applied to this target.
  applied JSONB NOT NULL DEFAULT '{}'::jsonb,
  PRIMARY KEY (binding_id, target_type, target_id)
);

CREATE TABLE js_plugin_events (
  id BIGSERIAL PRIMARY KEY,
  binding_id UUID NOT NULL REFERENCES js_plugin_bindings(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed', 'skipped')),
  attempts INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ
);
CREATE INDEX js_plugin_events_due_idx ON js_plugin_events (next_attempt_at) WHERE status = 'pending';
CREATE INDEX js_plugin_events_finished_idx ON js_plugin_events (finished_at) WHERE status <> 'pending';

CREATE TABLE js_plugin_action_log (
  id BIGSERIAL PRIMARY KEY,
  binding_id UUID REFERENCES js_plugin_bindings(id) ON DELETE SET NULL,
  plugin_id TEXT NOT NULL,
  version TEXT NOT NULL,
  event_type TEXT NOT NULL,
  action JSONB NOT NULL,
  target_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('applied', 'skipped', 'failed')),
  detail TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX js_plugin_action_log_plugin_idx ON js_plugin_action_log (plugin_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS js_plugin_action_log;
DROP TABLE IF EXISTS js_plugin_events;
DROP TABLE IF EXISTS js_plugin_binding_targets;
DELETE FROM js_plugin_bindings WHERE scope_type = 'oauth_connection';
ALTER TABLE js_plugin_bindings DROP COLUMN IF EXISTS state;
ALTER TABLE js_plugin_bindings DROP COLUMN IF EXISTS events;
ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_scope_type_check;
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_scope_type_check CHECK (scope_type IN ('site', 'endpoint'));
ALTER TABLE js_plugin_versions DROP COLUMN IF EXISTS granted_permissions;
