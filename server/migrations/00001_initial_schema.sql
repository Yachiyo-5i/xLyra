-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE FUNCTION set_updated_at()
RETURNS trigger AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE admins (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  username TEXT NOT NULL,
  nickname TEXT NOT NULL DEFAULT '',
  avatar TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'owner',
  status TEXT NOT NULL DEFAULT 'active',
  totp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  totp_secret_encrypted TEXT,
  totp_pending_secret_encrypted TEXT,
  last_login_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX admins_username_unique_idx ON admins (LOWER(username));

CREATE TABLE admin_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  admin_id UUID NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
  session_token_hash TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ,
  last_seen_at TIMESTAMPTZ,
  ip_address INET,
  user_agent TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX admin_sessions_admin_id_idx ON admin_sessions (admin_id);
CREATE INDEX admin_sessions_expires_at_idx ON admin_sessions (expires_at);

CREATE TABLE admin_access_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  admin_id UUID NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  masked_token TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  last_used_at TIMESTAMPTZ,
  last_used_ip INET,
  last_used_user_agent TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX admin_access_tokens_singleton_idx ON admin_access_tokens ((TRUE));
CREATE INDEX admin_access_tokens_admin_id_idx ON admin_access_tokens (admin_id);
CREATE INDEX admin_access_tokens_enabled_idx ON admin_access_tokens (enabled);

CREATE TABLE admin_audit_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  actor_type TEXT NOT NULL DEFAULT 'system',
  admin_id UUID REFERENCES admins(id) ON DELETE SET NULL,
  admin_session_id UUID REFERENCES admin_sessions(id) ON DELETE SET NULL,
  access_token_id UUID REFERENCES admin_access_tokens(id) ON DELETE SET NULL,
  action TEXT NOT NULL,
  resource_type TEXT NOT NULL DEFAULT '',
  resource_id TEXT NOT NULL DEFAULT '',
  ip_address INET,
  user_agent TEXT NOT NULL DEFAULT '',
  request_id TEXT NOT NULL DEFAULT '',
  success BOOLEAN NOT NULL DEFAULT TRUE,
  error_code TEXT NOT NULL DEFAULT '',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX admin_audit_logs_created_at_idx ON admin_audit_logs (created_at DESC);
CREATE INDEX admin_audit_logs_action_idx ON admin_audit_logs (action);
CREATE INDEX admin_audit_logs_actor_type_idx ON admin_audit_logs (actor_type);

CREATE TABLE api_keys (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  key_prefix TEXT NOT NULL UNIQUE,
  key_hash TEXT NOT NULL UNIQUE,
  encrypted_secret TEXT,
  masked_key TEXT NOT NULL DEFAULT '',
  key_kind TEXT NOT NULL DEFAULT 'generated' CHECK (key_kind IN ('generated', 'custom')),
  scope TEXT NOT NULL DEFAULT 'gateway',
  status TEXT NOT NULL DEFAULT 'active',
  model_policy TEXT NOT NULL DEFAULT 'allow_all' CHECK (model_policy IN ('allow_all', 'allow_list')),
  site_policy TEXT NOT NULL DEFAULT 'allow_all' CHECK (site_policy IN ('allow_all', 'allow_list')),
  model_mappings JSONB NOT NULL DEFAULT '{}'::jsonb,
  quota_limit NUMERIC(18,8),
  quota_used NUMERIC(18,8) NOT NULL DEFAULT 0,
  quota_unlimited BOOLEAN NOT NULL DEFAULT TRUE,
  quota_daily_limit NUMERIC(18,8),
  quota_daily_used NUMERIC(18,8) NOT NULL DEFAULT 0,
  quota_daily_unlimited BOOLEAN NOT NULL DEFAULT TRUE,
  quota_daily_window_start TIMESTAMPTZ,
  quota_weekly_limit NUMERIC(18,8),
  quota_weekly_used NUMERIC(18,8) NOT NULL DEFAULT 0,
  quota_weekly_unlimited BOOLEAN NOT NULL DEFAULT TRUE,
  quota_weekly_window_start TIMESTAMPTZ,
  created_by_admin_id UUID REFERENCES admins(id) ON DELETE SET NULL,
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX api_keys_status_idx ON api_keys (status);
CREATE INDEX api_keys_scope_status_idx ON api_keys (scope, status);

CREATE TABLE sites (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  site_type TEXT NOT NULL,
  base_url TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  routing_priority NUMERIC(2,1) NOT NULL DEFAULT 1.0 CHECK (routing_priority >= 1.0 AND routing_priority <= 5.0),
  meta JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX sites_enabled_idx ON sites (enabled);

CREATE TABLE site_groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX site_groups_enabled_sort_idx ON site_groups (enabled, sort_order, name);

CREATE TABLE site_group_sites (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id UUID NOT NULL REFERENCES site_groups(id) ON DELETE CASCADE,
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(group_id, site_id)
);

CREATE INDEX site_group_sites_group_id_idx ON site_group_sites (group_id);
CREATE INDEX site_group_sites_site_id_idx ON site_group_sites (site_id);

CREATE TABLE site_states (
  site_id UUID PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  validation_ok BOOLEAN,
  validation_message TEXT,
  sync_status TEXT NOT NULL DEFAULT 'pending',
  sync_message TEXT,
  last_sync_started_at TIMESTAMPTZ,
  last_synced_at TIMESTAMPTZ,
  api_key_count INTEGER NOT NULL DEFAULT 0,
  model_count INTEGER NOT NULL DEFAULT 0,
  raw_status JSONB NOT NULL DEFAULT '{}'::jsonb,
  user_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
  pricing JSONB NOT NULL DEFAULT '{}'::jsonb,
  checkin JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX site_states_sync_status_idx ON site_states (sync_status);
CREATE INDEX site_states_last_synced_at_idx ON site_states (last_synced_at DESC);

CREATE TABLE oauth_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider TEXT NOT NULL,
  state TEXT NOT NULL UNIQUE,
  pkce_verifier TEXT NOT NULL,
  redirect_uri TEXT NOT NULL,
  success_redirect_url TEXT NOT NULL DEFAULT '',
  failure_redirect_url TEXT NOT NULL DEFAULT '',
  site_id UUID REFERENCES sites(id) ON DELETE CASCADE,
  site_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'pending',
  expires_at TIMESTAMPTZ NOT NULL,
  completed_at TIMESTAMPTZ,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX oauth_sessions_provider_status_idx ON oauth_sessions (provider, status);
CREATE INDEX oauth_sessions_expires_at_idx ON oauth_sessions (expires_at);

CREATE TABLE oauth_connections (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider TEXT NOT NULL,
  site_id UUID REFERENCES sites(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'connected',
  account_id TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  encrypted_access_token TEXT NOT NULL,
  masked_access_token TEXT NOT NULL,
  encrypted_refresh_token TEXT NOT NULL,
  masked_refresh_token TEXT NOT NULL,
  encrypted_id_token TEXT NOT NULL,
  masked_id_token TEXT NOT NULL,
  token_type TEXT NOT NULL DEFAULT 'Bearer',
  scopes TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ,
  last_refresh_at TIMESTAMPTZ,
  last_sync_at TIMESTAMPTZ,
  raw_profile JSONB NOT NULL DEFAULT '{}'::jsonb,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT oauth_connections_provider_email_unique UNIQUE(provider, email),
  UNIQUE(site_id)
);

CREATE INDEX oauth_connections_provider_status_idx ON oauth_connections (provider, status);
CREATE INDEX oauth_connections_expires_at_idx ON oauth_connections (expires_at);

CREATE TABLE site_credentials (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  credential_type TEXT NOT NULL,
  encrypted_secret TEXT NOT NULL,
  masked_secret TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  routing_priority NUMERIC(2,1) NOT NULL DEFAULT 1.0,
  upstream_cost_multiplier NUMERIC(8,4) NOT NULL DEFAULT 1.0000,
  meta JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(site_id, credential_type)
);

CREATE TABLE site_api_key_states (
  site_credential_id UUID PRIMARY KEY REFERENCES site_credentials(id) ON DELETE CASCADE,
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  upstream_id INTEGER,
  name TEXT NOT NULL DEFAULT '',
  upstream_status JSONB NOT NULL DEFAULT 'null'::jsonb,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  group_name TEXT,
  remain_quota BIGINT,
  used_quota BIGINT,
  unlimited_quota BOOLEAN NOT NULL DEFAULT FALSE,
  expired_time BIGINT,
  model_limits_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  model_limits JSONB NOT NULL DEFAULT '[]'::jsonb,
  usage JSONB NOT NULL DEFAULT '{}'::jsonb,
  raw JSONB NOT NULL DEFAULT '{}'::jsonb,
  sync_status TEXT NOT NULL DEFAULT 'pending',
  sync_message TEXT,
  last_synced_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX site_api_key_states_site_id_idx ON site_api_key_states (site_id);
CREATE INDEX site_api_key_states_upstream_id_idx ON site_api_key_states (site_id, upstream_id);

CREATE TABLE canonical_models (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  model_key TEXT NOT NULL,
  display_name TEXT NOT NULL,
  provider TEXT NOT NULL DEFAULT 'unknown',
  category TEXT NOT NULL DEFAULT 'chat',
  capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'active',
  input_price NUMERIC(18,8),
  output_price NUMERIC(18,8),
  cache_read_ratio NUMERIC(18,8),
  cache_write_ratio NUMERIC(18,8),
  cache_write_1h_ratio NUMERIC(18,8),
  supported_endpoint_types JSONB NOT NULL DEFAULT '[]'::jsonb,
  modalities JSONB NOT NULL DEFAULT '[]'::jsonb,
  context_window INTEGER,
  max_output_tokens INTEGER,
  pricing_source TEXT NOT NULL DEFAULT 'none',
  last_pricing_synced_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX canonical_models_model_key_lower_idx ON canonical_models (LOWER(model_key));

CREATE TABLE api_key_site_permissions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  api_key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(api_key_id, site_id)
);

CREATE INDEX api_key_site_permissions_api_key_id_idx ON api_key_site_permissions (api_key_id);

CREATE TABLE api_key_site_group_permissions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  api_key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  group_id UUID NOT NULL REFERENCES site_groups(id) ON DELETE CASCADE,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(api_key_id, group_id)
);

CREATE INDEX api_key_site_group_permissions_api_key_id_idx ON api_key_site_group_permissions (api_key_id);
CREATE INDEX api_key_site_group_permissions_group_id_idx ON api_key_site_group_permissions (group_id);

CREATE TABLE site_models (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  canonical_model_id UUID REFERENCES canonical_models(id) ON DELETE SET NULL,
  upstream_model_name TEXT NOT NULL,
  display_name TEXT NOT NULL,
  capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
  status TEXT NOT NULL DEFAULT 'active',
  canonical_match_source TEXT NOT NULL DEFAULT 'unmatched',
  canonical_match_confidence INTEGER NOT NULL DEFAULT 0,
  canonical_matched_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(site_id, upstream_model_name)
);

CREATE INDEX site_models_canonical_model_id_idx ON site_models (canonical_model_id);

CREATE TABLE api_key_site_model_permissions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  api_key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  site_model_id UUID NOT NULL REFERENCES site_models(id) ON DELETE CASCADE,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(api_key_id, site_model_id)
);

CREATE INDEX api_key_site_model_permissions_api_key_id_idx ON api_key_site_model_permissions (api_key_id);
CREATE INDEX api_key_site_model_permissions_site_model_id_idx ON api_key_site_model_permissions (site_model_id);

CREATE TABLE canonical_model_aliases (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  canonical_model_id UUID NOT NULL REFERENCES canonical_models(id) ON DELETE CASCADE,
  alias TEXT NOT NULL,
  normalized_alias TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'manual',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(normalized_alias)
);

CREATE INDEX canonical_model_aliases_model_id_idx ON canonical_model_aliases (canonical_model_id);

CREATE TABLE site_api_key_models (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  site_credential_id UUID NOT NULL REFERENCES site_credentials(id) ON DELETE CASCADE,
  site_model_id UUID REFERENCES site_models(id) ON DELETE CASCADE,
  upstream_model_name TEXT NOT NULL,
  display_name TEXT NOT NULL,
  available BOOLEAN NOT NULL DEFAULT TRUE,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  raw JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_seen_at TIMESTAMPTZ,
  last_synced_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(site_credential_id, upstream_model_name)
);

CREATE INDEX site_api_key_models_site_id_idx ON site_api_key_models (site_id);
CREATE INDEX site_api_key_models_site_model_id_idx ON site_api_key_models (site_model_id);
CREATE INDEX site_api_key_models_credential_id_idx ON site_api_key_models (site_credential_id);

CREATE TABLE site_pricing_groups (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  group_name TEXT NOT NULL,
  display_name TEXT,
  ratio NUMERIC(18,8) NOT NULL DEFAULT 1,
  is_auto BOOLEAN NOT NULL DEFAULT FALSE,
  available BOOLEAN NOT NULL DEFAULT TRUE,
  raw JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_synced_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(site_id, group_name)
);

CREATE INDEX site_pricing_groups_site_id_idx ON site_pricing_groups (site_id);
CREATE INDEX site_pricing_groups_available_idx ON site_pricing_groups (site_id, available);

CREATE TABLE site_model_pricings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  site_model_id UUID REFERENCES site_models(id) ON DELETE SET NULL,
  model_name TEXT NOT NULL,
  group_name TEXT NOT NULL,
  quota_type INTEGER NOT NULL DEFAULT 0,
  billing_type TEXT NOT NULL DEFAULT 'ratio',
  currency TEXT NOT NULL DEFAULT 'USD',
  group_ratio NUMERIC(18,8) NOT NULL DEFAULT 1,
  model_ratio NUMERIC(18,8),
  completion_ratio NUMERIC(18,8),
  cache_ratio NUMERIC(18,8),
  create_cache_ratio NUMERIC(18,8),
  create_cache_1h_ratio NUMERIC(18,8),
  image_ratio NUMERIC(18,8),
  audio_ratio NUMERIC(18,8),
  audio_completion_ratio NUMERIC(18,8),
  model_price NUMERIC(18,8),
  per_request_value NUMERIC(18,8),
  input_value NUMERIC(18,8),
  output_value NUMERIC(18,8),
  vendor_id INTEGER,
  vendor_name TEXT,
  vendor_icon TEXT,
  description TEXT,
  owner_by TEXT,
  pricing_source TEXT NOT NULL DEFAULT 'unknown',
  manual_override BOOLEAN NOT NULL DEFAULT FALSE,
  manual_updated_at TIMESTAMPTZ,
  manual_note TEXT,
  available BOOLEAN NOT NULL DEFAULT TRUE,
  raw JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_synced_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(site_id, model_name, group_name)
);

CREATE INDEX site_model_pricings_site_id_idx ON site_model_pricings (site_id);
CREATE INDEX site_model_pricings_site_model_id_idx ON site_model_pricings (site_model_id);
CREATE INDEX site_model_pricings_model_name_idx ON site_model_pricings (site_id, model_name);
CREATE INDEX site_model_pricings_available_idx ON site_model_pricings (site_id, available);
CREATE INDEX site_model_pricings_pricing_source_idx ON site_model_pricings (pricing_source);
CREATE INDEX site_model_pricings_manual_override_idx ON site_model_pricings (manual_override);

CREATE TABLE route_cooldowns (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  site_model_id UUID REFERENCES site_models(id) ON DELETE CASCADE,
  site_credential_id UUID REFERENCES site_credentials(id) ON DELETE CASCADE,
  scope TEXT NOT NULL DEFAULT 'site',
  source TEXT NOT NULL DEFAULT 'manual',
  reason TEXT NOT NULL DEFAULT 'cooldown',
  active_until TIMESTAMPTZ NOT NULL,
  cleared_at TIMESTAMPTZ,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX route_cooldowns_site_id_idx ON route_cooldowns (site_id, active_until DESC);
CREATE INDEX route_cooldowns_site_model_id_idx ON route_cooldowns (site_model_id, active_until DESC);
CREATE INDEX route_cooldowns_site_credential_id_idx ON route_cooldowns (site_credential_id, active_until DESC);
CREATE INDEX route_cooldowns_active_idx ON route_cooldowns (active_until DESC) WHERE cleared_at IS NULL;

CREATE TABLE request_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_id TEXT NOT NULL UNIQUE,
  api_key_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
  site_id UUID REFERENCES sites(id) ON DELETE SET NULL,
  canonical_model_id UUID REFERENCES canonical_models(id) ON DELETE SET NULL,
  site_model_id UUID REFERENCES site_models(id) ON DELETE SET NULL,
  endpoint TEXT NOT NULL,
  status_code INTEGER NOT NULL,
  success BOOLEAN NOT NULL DEFAULT FALSE,
  error_type TEXT,
  latency_ms INTEGER,
  upstream_latency_ms INTEGER,
  request_tokens INTEGER,
  response_tokens INTEGER,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX request_logs_created_at_idx ON request_logs (created_at DESC);
CREATE INDEX request_logs_created_success_idx ON request_logs (created_at DESC, success);
CREATE INDEX request_logs_canonical_model_created_idx ON request_logs (canonical_model_id, created_at DESC);
CREATE INDEX request_logs_error_type_created_idx ON request_logs (error_type, created_at DESC);
CREATE INDEX request_logs_site_id_idx ON request_logs (site_id);

CREATE TABLE usage_records (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  request_log_id UUID NOT NULL UNIQUE REFERENCES request_logs(id) ON DELETE CASCADE,
  api_key_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
  site_id UUID REFERENCES sites(id) ON DELETE SET NULL,
  canonical_model_id UUID REFERENCES canonical_models(id) ON DELETE SET NULL,
  prompt_tokens INTEGER NOT NULL DEFAULT 0,
  completion_tokens INTEGER NOT NULL DEFAULT 0,
  total_tokens INTEGER NOT NULL DEFAULT 0,
  cached_tokens BIGINT,
  estimated_cost NUMERIC(18,8),
  currency TEXT NOT NULL DEFAULT 'USD',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX usage_records_created_at_idx ON usage_records (created_at DESC);
CREATE INDEX usage_records_canonical_model_created_idx ON usage_records (canonical_model_id, created_at DESC);
CREATE INDEX usage_records_site_created_idx ON usage_records (site_id, created_at DESC);

CREATE TABLE request_usage_daily_summaries (
  summary_key TEXT PRIMARY KEY,
  bucket_start TIMESTAMPTZ NOT NULL,
  timezone TEXT NOT NULL,
  site_key TEXT NOT NULL,
  site_id UUID,
  site_name TEXT NOT NULL DEFAULT '',
  site_slug TEXT NOT NULL DEFAULT '',
  site_type TEXT NOT NULL DEFAULT '',
  canonical_model_key TEXT NOT NULL,
  canonical_model_id UUID,
  site_model_key TEXT NOT NULL,
  site_model_id UUID,
  upstream_model_name TEXT NOT NULL DEFAULT '',
  api_key_key TEXT NOT NULL,
  api_key_id UUID,
  api_key_name TEXT NOT NULL DEFAULT '',
  endpoint TEXT NOT NULL,
  status_code INTEGER NOT NULL,
  success BOOLEAN NOT NULL DEFAULT FALSE,
  error_type TEXT NOT NULL,
  currency TEXT NOT NULL DEFAULT 'USD',
  request_count BIGINT NOT NULL DEFAULT 0,
  success_count BIGINT NOT NULL DEFAULT 0,
  failure_count BIGINT NOT NULL DEFAULT 0,
  prompt_tokens BIGINT NOT NULL DEFAULT 0,
  completion_tokens BIGINT NOT NULL DEFAULT 0,
  total_tokens BIGINT NOT NULL DEFAULT 0,
  cached_tokens BIGINT NOT NULL DEFAULT 0,
  estimated_cost NUMERIC(18,8) NOT NULL DEFAULT 0,
  latency_count BIGINT NOT NULL DEFAULT 0,
  latency_total_ms BIGINT NOT NULL DEFAULT 0,
  latency_min_ms BIGINT,
  latency_max_ms BIGINT,
  upstream_latency_count BIGINT NOT NULL DEFAULT 0,
  upstream_latency_total_ms BIGINT NOT NULL DEFAULT 0,
  upstream_latency_min_ms BIGINT,
  upstream_latency_max_ms BIGINT,
  first_request_at TIMESTAMPTZ,
  last_request_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX request_usage_daily_summaries_bucket_idx ON request_usage_daily_summaries (bucket_start DESC);
CREATE INDEX request_usage_daily_summaries_site_bucket_idx ON request_usage_daily_summaries (site_key, bucket_start DESC);
CREATE INDEX request_usage_daily_summaries_model_bucket_idx ON request_usage_daily_summaries (canonical_model_key, bucket_start DESC);
CREATE INDEX request_usage_daily_summaries_currency_bucket_idx ON request_usage_daily_summaries (currency, bucket_start DESC);

CREATE TABLE request_usage_summary_days (
  bucket_start TIMESTAMPTZ NOT NULL,
  timezone TEXT NOT NULL,
  status TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT '',
  request_count BIGINT NOT NULL DEFAULT 0,
  last_summarized_at TIMESTAMPTZ,
  last_cleaned_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (bucket_start, timezone)
);

CREATE INDEX request_usage_summary_days_status_idx ON request_usage_summary_days (status, bucket_start DESC);

CREATE TABLE gateway_rate_limits (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  scope TEXT NOT NULL CHECK (scope IN ('global', 'api_key')),
  api_key_id UUID REFERENCES api_keys(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'disabled' CHECK (status IN ('enabled', 'disabled')),
  rpm_limit BIGINT CHECK (rpm_limit IS NULL OR rpm_limit > 0),
  tpm_limit BIGINT CHECK (tpm_limit IS NULL OR tpm_limit > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (
    (scope = 'global' AND api_key_id IS NULL) OR
    (scope = 'api_key' AND api_key_id IS NOT NULL)
  )
);

CREATE UNIQUE INDEX gateway_rate_limits_global_unique
ON gateway_rate_limits (scope)
WHERE scope = 'global';

CREATE UNIQUE INDEX gateway_rate_limits_api_key_unique
ON gateway_rate_limits (api_key_id)
WHERE scope = 'api_key';

CREATE INDEX gateway_rate_limits_scope_status_idx
ON gateway_rate_limits (scope, status);

CREATE TABLE gateway_rate_limit_windows (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  scope_key TEXT NOT NULL,
  window_start TIMESTAMPTZ NOT NULL,
  rpm_used BIGINT NOT NULL DEFAULT 0,
  tpm_reserved BIGINT NOT NULL DEFAULT 0,
  tpm_actual BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (scope_key, window_start)
);

CREATE INDEX gateway_rate_limit_windows_window_start_idx
ON gateway_rate_limit_windows (window_start DESC);

CREATE TABLE health_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_id UUID NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  site_model_id UUID REFERENCES site_models(id) ON DELETE CASCADE,
  scope TEXT NOT NULL DEFAULT 'site',
  source TEXT NOT NULL DEFAULT 'manual',
  endpoint TEXT NOT NULL,
  method TEXT NOT NULL DEFAULT 'GET',
  success BOOLEAN NOT NULL DEFAULT FALSE,
  status_code INTEGER,
  latency_ms INTEGER,
  error_type TEXT,
  error_message TEXT,
  checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX health_snapshots_site_id_idx ON health_snapshots (site_id, checked_at DESC);
CREATE INDEX health_snapshots_scope_idx ON health_snapshots (scope, checked_at DESC);
CREATE INDEX health_snapshots_scope_source_checked_idx ON health_snapshots (scope, source, checked_at DESC);
CREATE INDEX health_snapshots_site_scope_source_checked_idx ON health_snapshots (site_id, scope, source, checked_at DESC);
CREATE INDEX health_snapshots_site_model_id_idx ON health_snapshots (site_model_id, checked_at DESC);

CREATE TABLE site_health_states (
  site_id UUID PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'unknown',
  last_snapshot_id UUID REFERENCES health_snapshots(id) ON DELETE SET NULL,
  last_success_at TIMESTAMPTZ,
  last_failure_at TIMESTAMPTZ,
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  recent_success_rate NUMERIC(5,4),
  recent_avg_latency_ms INTEGER,
  checked_at TIMESTAMPTZ,
  message TEXT,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX site_health_states_status_idx ON site_health_states (status);
CREATE INDEX site_health_states_checked_at_idx ON site_health_states (checked_at DESC);

CREATE TRIGGER admins_set_updated_at
BEFORE UPDATE ON admins
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER admin_sessions_set_updated_at
BEFORE UPDATE ON admin_sessions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER admin_access_tokens_set_updated_at
BEFORE UPDATE ON admin_access_tokens
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER api_keys_set_updated_at
BEFORE UPDATE ON api_keys
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER api_key_site_permissions_set_updated_at
BEFORE UPDATE ON api_key_site_permissions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER api_key_site_group_permissions_set_updated_at
BEFORE UPDATE ON api_key_site_group_permissions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER api_key_site_model_permissions_set_updated_at
BEFORE UPDATE ON api_key_site_model_permissions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER sites_set_updated_at
BEFORE UPDATE ON sites
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_groups_set_updated_at
BEFORE UPDATE ON site_groups
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_group_sites_set_updated_at
BEFORE UPDATE ON site_group_sites
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_states_set_updated_at
BEFORE UPDATE ON site_states
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER oauth_sessions_set_updated_at
BEFORE UPDATE ON oauth_sessions
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER oauth_connections_set_updated_at
BEFORE UPDATE ON oauth_connections
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_credentials_set_updated_at
BEFORE UPDATE ON site_credentials
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_api_key_states_set_updated_at
BEFORE UPDATE ON site_api_key_states
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER canonical_models_set_updated_at
BEFORE UPDATE ON canonical_models
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER canonical_model_aliases_set_updated_at
BEFORE UPDATE ON canonical_model_aliases
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_models_set_updated_at
BEFORE UPDATE ON site_models
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_api_key_models_set_updated_at
BEFORE UPDATE ON site_api_key_models
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_pricing_groups_set_updated_at
BEFORE UPDATE ON site_pricing_groups
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_model_pricings_set_updated_at
BEFORE UPDATE ON site_model_pricings
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER route_cooldowns_set_updated_at
BEFORE UPDATE ON route_cooldowns
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER gateway_rate_limits_set_updated_at
BEFORE UPDATE ON gateway_rate_limits
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER gateway_rate_limit_windows_set_updated_at
BEFORE UPDATE ON gateway_rate_limit_windows
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER site_health_states_set_updated_at
BEFORE UPDATE ON site_health_states
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd
