-- +goose Up
ALTER TABLE js_plugin_versions DROP CONSTRAINT IF EXISTS js_plugin_versions_package_sha256_key;

CREATE TABLE js_plugin_trusted_keys (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  public_key TEXT NOT NULL,
  fingerprint TEXT NOT NULL UNIQUE,
  created_by UUID REFERENCES admins(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (public_key)
);

CREATE INDEX js_plugin_trusted_keys_created_idx ON js_plugin_trusted_keys (created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS js_plugin_trusted_keys;

ALTER TABLE js_plugin_versions ADD CONSTRAINT js_plugin_versions_package_sha256_key UNIQUE (package_sha256);
