-- +goose Up
-- Plugin bindings become "mounts": a plugin version attached to a scope (a site,
-- a downstream endpoint) in a slot, with optional admin-supplied config. The old
-- target_kind string mixed the plugin kind and the scope, and its CHECK had to be
-- widened for every new kind.
ALTER TABLE js_plugin_bindings ADD COLUMN kind TEXT;
ALTER TABLE js_plugin_bindings ADD COLUMN scope_type TEXT;
ALTER TABLE js_plugin_bindings ADD COLUMN scope_id TEXT;
ALTER TABLE js_plugin_bindings ADD COLUMN slot TEXT;
ALTER TABLE js_plugin_bindings ADD COLUMN config JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE js_plugin_bindings SET
  kind = CASE
    WHEN target_kind = 'site_quota_probe' THEN 'quota_probe'
    WHEN target_kind = 'protocol_endpoint' THEN 'protocol'
    ELSE substring(target_kind FROM char_length('site_plugin:') + 1)
  END,
  scope_type = CASE WHEN target_kind = 'protocol_endpoint' THEN 'endpoint' ELSE 'site' END,
  scope_id = target_id;
UPDATE js_plugin_bindings SET slot = kind;

ALTER TABLE js_plugin_bindings ALTER COLUMN kind SET NOT NULL;
ALTER TABLE js_plugin_bindings ALTER COLUMN scope_type SET NOT NULL;
ALTER TABLE js_plugin_bindings ALTER COLUMN scope_id SET NOT NULL;
ALTER TABLE js_plugin_bindings ALTER COLUMN slot SET NOT NULL;

ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_target_kind_check;
ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_target_kind_target_id_key;
ALTER TABLE js_plugin_bindings DROP COLUMN target_kind;
ALTER TABLE js_plugin_bindings DROP COLUMN target_id;

ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_scope_slot_key UNIQUE (scope_type, scope_id, slot);
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_scope_type_check CHECK (scope_type IN ('site', 'endpoint'));

-- +goose Down
ALTER TABLE js_plugin_bindings ADD COLUMN target_kind TEXT;
ALTER TABLE js_plugin_bindings ADD COLUMN target_id TEXT;
UPDATE js_plugin_bindings SET
  target_kind = CASE
    WHEN kind = 'quota_probe' THEN 'site_quota_probe'
    WHEN kind = 'protocol' THEN 'protocol_endpoint'
    ELSE 'site_plugin:' || kind
  END,
  target_id = scope_id;
ALTER TABLE js_plugin_bindings ALTER COLUMN target_kind SET NOT NULL;
ALTER TABLE js_plugin_bindings ALTER COLUMN target_id SET NOT NULL;
ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_scope_type_check;
ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_scope_slot_key;
ALTER TABLE js_plugin_bindings DROP COLUMN kind;
ALTER TABLE js_plugin_bindings DROP COLUMN scope_type;
ALTER TABLE js_plugin_bindings DROP COLUMN scope_id;
ALTER TABLE js_plugin_bindings DROP COLUMN slot;
ALTER TABLE js_plugin_bindings DROP COLUMN config;
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_target_kind_target_id_key UNIQUE (target_kind, target_id);
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_target_kind_check
  CHECK (target_kind IN ('site_quota_probe', 'protocol_endpoint') OR target_kind LIKE 'site_plugin:%');
