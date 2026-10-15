-- +goose Up
-- Which plugin serves a site is stored in the site's gateway config, which is
-- what routing and the site pages read. The rows mirrored here were never read,
-- and could drift from the config, so they go. Bindings that remain are the ones
-- this table owns: downstream paths and automations.
DELETE FROM js_plugin_bindings WHERE scope_type = 'site';
ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_scope_type_check;
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_scope_type_check
  CHECK (scope_type IN ('endpoint', 'oauth_connection'));

-- +goose Down
ALTER TABLE js_plugin_bindings DROP CONSTRAINT IF EXISTS js_plugin_bindings_scope_type_check;
ALTER TABLE js_plugin_bindings ADD CONSTRAINT js_plugin_bindings_scope_type_check
  CHECK (scope_type IN ('site', 'endpoint', 'oauth_connection'));
