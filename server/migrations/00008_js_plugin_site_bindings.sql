-- +goose Up
-- Per-site plugin kinds (model_list, credential_check, error_classifier,
-- pricing_parse) bind with target_kind "site_plugin:<kind>".
ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_target_kind_check;
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_target_kind_check
  CHECK (target_kind IN ('site_quota_probe', 'protocol_endpoint') OR target_kind LIKE 'site_plugin:%');

-- +goose Down
DELETE FROM js_plugin_bindings WHERE target_kind LIKE 'site_plugin:%';
ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_target_kind_check;
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_target_kind_check
  CHECK (target_kind IN ('site_quota_probe', 'protocol_endpoint'));
