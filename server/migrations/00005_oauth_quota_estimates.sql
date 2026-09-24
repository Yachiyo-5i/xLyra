-- +goose Up
CREATE TABLE oauth_quota_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  oauth_connection_id UUID NOT NULL REFERENCES oauth_connections(id) ON DELETE CASCADE,
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  five_hour_used_percent NUMERIC(8,4),
  five_hour_remaining_percent NUMERIC(8,4),
  five_hour_reset_at TIMESTAMPTZ,
  weekly_used_percent NUMERIC(8,4),
  weekly_remaining_percent NUMERIC(8,4),
  weekly_reset_at TIMESTAMPTZ,
  available BOOLEAN NOT NULL DEFAULT FALSE,
  raw_quota JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX oauth_quota_snapshots_connection_observed_idx
  ON oauth_quota_snapshots (oauth_connection_id, observed_at DESC);
CREATE INDEX oauth_quota_snapshots_site_observed_idx
  ON oauth_quota_snapshots (site_id, observed_at DESC);

CREATE TABLE oauth_quota_window_rounds (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  oauth_connection_id UUID NOT NULL REFERENCES oauth_connections(id) ON DELETE CASCADE,
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  window_type TEXT NOT NULL CHECK (window_type IN ('five_hour', 'weekly')),
  status TEXT NOT NULL CHECK (status IN ('current', 'previous')),
  started_at TIMESTAMPTZ NOT NULL,
  ended_at TIMESTAMPTZ,
  start_used_percent NUMERIC(8,4),
  latest_used_percent NUMERIC(8,4),
  start_remaining_percent NUMERIC(8,4),
  latest_remaining_percent NUMERIC(8,4),
  start_reset_at TIMESTAMPTZ,
  latest_reset_at TIMESTAMPTZ,
  start_snapshot_id UUID REFERENCES oauth_quota_snapshots(id) ON DELETE SET NULL,
  latest_snapshot_id UUID REFERENCES oauth_quota_snapshots(id) ON DELETE SET NULL,
  cumulative_used_percent NUMERIC(12,4) NOT NULL DEFAULT 0,
  cumulative_system_cost NUMERIC(18,8) NOT NULL DEFAULT 0,
  request_count BIGINT NOT NULL DEFAULT 0,
  sample_count INTEGER NOT NULL DEFAULT 0,
  estimated_total NUMERIC(18,8),
  confidence TEXT NOT NULL DEFAULT 'baseline',
  external_usage_hint BOOLEAN NOT NULL DEFAULT FALSE,
  currency TEXT NOT NULL DEFAULT 'USD',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (oauth_connection_id, window_type, status)
);

CREATE INDEX oauth_quota_window_rounds_connection_idx
  ON oauth_quota_window_rounds (oauth_connection_id, window_type, status);

-- +goose Down
DROP TABLE IF EXISTS oauth_quota_window_rounds;
DROP TABLE IF EXISTS oauth_quota_snapshots;
