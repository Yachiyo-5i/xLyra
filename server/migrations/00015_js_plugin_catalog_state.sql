-- +goose Up
-- Ephemeral catalog generation. Not part of backups: each instance polls this
-- counter and reloads enabled plugins; the row is recreated as they run.
CREATE TABLE js_plugin_catalog_state (
  id SMALLINT PRIMARY KEY CHECK (id = 1),
  generation BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO js_plugin_catalog_state (id, generation) VALUES (1, 0);

-- +goose Down
DROP TABLE js_plugin_catalog_state;
