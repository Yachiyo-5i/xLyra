-- +goose Up
CREATE TABLE js_plugins (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL CHECK (source IN ('builtin', 'uploaded')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE js_plugin_versions (
  plugin_id TEXT NOT NULL REFERENCES js_plugins(id) ON DELETE CASCADE,
  version TEXT NOT NULL,
  manifest JSONB NOT NULL,
  package BYTEA NOT NULL,
  package_sha256 TEXT NOT NULL UNIQUE,
  signer TEXT,
  status TEXT NOT NULL CHECK (status IN ('uploaded', 'verified', 'enabled', 'disabled', 'broken')),
  selftest JSONB NOT NULL DEFAULT '{}'::jsonb,
  uploaded_by UUID REFERENCES admins(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (plugin_id, version)
);

CREATE INDEX js_plugin_versions_status_idx ON js_plugin_versions (status);
CREATE INDEX js_plugin_versions_plugin_created_idx ON js_plugin_versions (plugin_id, created_at DESC);

CREATE TABLE js_plugin_bindings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  plugin_id TEXT NOT NULL REFERENCES js_plugins(id) ON DELETE CASCADE,
  version TEXT NOT NULL,
  target_kind TEXT NOT NULL CHECK (target_kind IN ('site_quota_probe', 'protocol_endpoint')),
  target_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (target_kind, target_id),
  FOREIGN KEY (plugin_id, version) REFERENCES js_plugin_versions(plugin_id, version) ON DELETE CASCADE
);

CREATE INDEX js_plugin_bindings_plugin_idx ON js_plugin_bindings (plugin_id, version);

-- +goose Down
DROP TABLE IF EXISTS js_plugin_bindings;
DROP TABLE IF EXISTS js_plugin_versions;
DROP TABLE IF EXISTS js_plugins;
