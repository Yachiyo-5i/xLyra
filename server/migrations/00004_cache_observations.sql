-- +goose Up
CREATE TABLE cache_observations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_log_id UUID NOT NULL,
  api_key_id UUID NOT NULL,
  canonical_model_id UUID NOT NULL,
  success BOOLEAN NOT NULL,
  prefix_hash TEXT NOT NULL DEFAULT '',
  root_prefix_hash TEXT NOT NULL DEFAULT '',
  prefix_lineage JSONB NOT NULL DEFAULT '[]'::jsonb,
  lineage_truncated BOOLEAN NOT NULL DEFAULT FALSE,
  session_hash TEXT NOT NULL DEFAULT '',
  cache_domain_hash TEXT NOT NULL DEFAULT '',
  cache_fingerprint TEXT NOT NULL DEFAULT '',
  lineage_depth INTEGER NOT NULL DEFAULT 0,
  downstream_protocol TEXT NOT NULL DEFAULT '',
  upstream_protocol TEXT NOT NULL DEFAULT '',
  cache_policy_hash TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(request_log_id)
);

CREATE INDEX cache_observations_lookup_idx
  ON cache_observations (api_key_id, canonical_model_id, created_at);
CREATE INDEX cache_observations_expires_at_idx
  ON cache_observations (expires_at);
CREATE INDEX cache_observations_created_at_idx
  ON cache_observations (created_at);

-- +goose Down
DROP TABLE IF EXISTS cache_observations;
