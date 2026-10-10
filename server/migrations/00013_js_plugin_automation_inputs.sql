-- +goose Up
-- An automation binding now holds named inputs declared by the plugin (the objects
-- an admin picked, with plain parameters in config) instead of one fixed OAuth
-- account and a list of API keys. The bindings made the old way are removed: they
-- have no input names to carry over.
DELETE FROM js_plugin_bindings WHERE scope_type = 'oauth_connection';
DROP TABLE js_plugin_binding_targets;

ALTER TABLE js_plugin_bindings DROP CONSTRAINT js_plugin_bindings_scope_type_check;
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_scope_type_check
  CHECK (scope_type IN ('endpoint', 'automation'));

CREATE TABLE js_plugin_binding_inputs (
  binding_id UUID NOT NULL REFERENCES js_plugin_bindings(id) ON DELETE CASCADE,
  input_name TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  -- the object events are about, so an event finds the bindings that picked it
  event_subject BOOLEAN NOT NULL DEFAULT FALSE,
  -- scope -> idempotency key of the last action applied to this object
  applied JSONB NOT NULL DEFAULT '{}'::jsonb,
  PRIMARY KEY (binding_id, entity_type, entity_id)
);
CREATE INDEX js_plugin_binding_inputs_subject_idx
  ON js_plugin_binding_inputs (entity_type, entity_id) WHERE event_subject;

-- +goose Down
DROP TABLE js_plugin_binding_inputs;
DELETE FROM js_plugin_bindings WHERE scope_type = 'automation';
ALTER TABLE js_plugin_bindings DROP CONSTRAINT js_plugin_bindings_scope_type_check;
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_scope_type_check
  CHECK (scope_type IN ('endpoint', 'oauth_connection'));
CREATE TABLE js_plugin_binding_targets (
  binding_id UUID NOT NULL REFERENCES js_plugin_bindings(id) ON DELETE CASCADE,
  target_type TEXT NOT NULL,
  target_id TEXT NOT NULL,
  applied JSONB NOT NULL DEFAULT '{}'::jsonb,
  PRIMARY KEY (binding_id, target_type, target_id)
);
