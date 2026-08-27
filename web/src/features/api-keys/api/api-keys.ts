import { apiFetch } from '@/lib/http'

type DownstreamAPIKeyStatus = 'active' | 'disabled' | string
type DownstreamAPIKeyModelPolicy = 'allow_all' | 'allow_list' | string
type RateLimitStatus = 'enabled' | 'disabled'

type RateLimitConfig = {
  status: RateLimitStatus
  rpm_limit?: number | null
  tpm_limit?: number | null
}

export type ImageToolBridgeConfig = {
  enabled: boolean
  model: string
  site_id?: string | null
  max_calls?: number | null
}

export type ModelRule = {
  pattern: string
  target: string
  mode?: 'hard' | 'soft'
}

export type DownstreamAPIKeyModel = {
  id?: string
  api_key_id?: string
  canonical_model_id?: string | null
  canonical_model_key?: string | null
  model_key?: string
  site_model_id?: string
  site_id?: string
  site_name?: string
  site_slug?: string
  site_type?: string
  upstream_model_name?: string
  display_name?: string
  enabled: boolean
  created_at?: string
  updated_at?: string
}

export type DownstreamAPIKeySite = {
  id?: string
  api_key_id?: string
  site_id: string
  enabled: boolean
  created_at?: string
  updated_at?: string
}

export type DownstreamAPIKeySiteGroup = {
  id?: string
  api_key_id?: string
  group_id: string
  enabled: boolean
  created_at?: string
  updated_at?: string
}

export type DownstreamAPIKey = {
  id: string
  name: string
  key?: string | null
  key_prefix: string
  masked_key: string
  key_kind?: 'generated' | 'custom' | string
  scope: string
  status: DownstreamAPIKeyStatus
  model_policy: DownstreamAPIKeyModelPolicy
  site_policy: DownstreamAPIKeyModelPolicy
  model_mappings?: ModelRule[] | null
  image_tool_bridge?: ImageToolBridgeConfig | null
  quota_limit?: number | null
  quota_total_used?: number
  quota_total_available?: number | null
  quota_total_reset_at?: string | null
  quota_used: number
  quota_available?: number | null
  quota_unlimited: boolean
  quota_daily_limit?: number | null
  quota_daily_used?: number
  quota_daily_available?: number | null
  quota_daily_unlimited?: boolean
  quota_daily_reset_at?: string | null
  quota_weekly_limit?: number | null
  quota_weekly_used?: number
  quota_weekly_available?: number | null
  quota_weekly_unlimited?: boolean
  quota_weekly_reset_at?: string | null
  auto_reset_oauth_connection_id?: string | null
  auto_reset_last_reset_at?: string | null
  auto_reset_window?: 'weekly' | string | null
  auto_reset_scope?: 'total' | string | null
  rate_limit?: RateLimitConfig | null
  expires_at?: string | null
  last_used_at?: string | null
  models?: DownstreamAPIKeyModel[]
  site_models?: DownstreamAPIKeyModel[]
  sites: DownstreamAPIKeySite[]
  site_groups?: DownstreamAPIKeySiteGroup[]
  created_at: string
  updated_at: string
}

export type APIKeyUpsertInput = {
  name: string
  customKey?: string
  status?: DownstreamAPIKeyStatus
  modelPolicy: 'allow_all' | 'allow_list'
  siteModelIds: string[]
  sitePolicy: 'allow_all' | 'allow_list'
  siteIds: string[]
  siteGroupIds: string[]
  modelMappings?: ModelRule[]
  imageToolBridge?: ImageToolBridgeConfig | null
  quotaLimit?: number | null
  quotaUnlimited: boolean
  quotaDailyLimit?: number | null
  quotaDailyUnlimited: boolean
  quotaWeeklyLimit?: number | null
  quotaWeeklyUnlimited: boolean
  autoResetOAuthConnectionId?: string | null
  rateLimit?: RateLimitConfig
  expiresAt?: string | null
}

export const downstreamAPIKeyQueryKeys = {
  all: ['downstream-api-keys'] as const,
  list: () => [...downstreamAPIKeyQueryKeys.all, 'list'] as const,
  detail: (apiKeyId: string) => [...downstreamAPIKeyQueryKeys.all, 'detail', apiKeyId] as const,
}

export async function listDownstreamAPIKeys() {
  return apiFetch<{ items: DownstreamAPIKey[]; meta?: { count?: number } }>('/api/v1/api-keys')
}

// List/detail responses only carry masked_key; the plaintext is fetched on
// demand from this audited single-key endpoint when the user copies a key.
export async function revealDownstreamAPIKey(apiKeyId: string) {
  return apiFetch<{ id: string; key: string; masked_key: string }>(`/api/v1/api-keys/${apiKeyId}/reveal`)
}

export async function createDownstreamAPIKey(input: APIKeyUpsertInput) {
  return apiFetch<{
    key: string
    key_prefix: string
    api_key: DownstreamAPIKey
  }>('/api/v1/api-keys', {
    method: 'POST',
    body: apiKeyUpsertBody(input),
  })
}

// Rotates the credential in place: the old secret stops working immediately,
// all configuration and usage counters are preserved. The new plaintext is
// returned exactly once in this response. Custom keys are rejected server-side.
export async function rotateDownstreamAPIKey(apiKeyId: string) {
  return apiFetch<{
    key: string
    key_prefix: string
    api_key: DownstreamAPIKey
  }>(`/api/v1/api-keys/${apiKeyId}/rotate`, {
    method: 'POST',
  })
}

export async function updateDownstreamAPIKey(apiKeyId: string, input: APIKeyUpsertInput) {
  return apiFetch<{ api_key: DownstreamAPIKey }>(`/api/v1/api-keys/${apiKeyId}`, {
    method: 'PUT',
    body: apiKeyUpsertBody(input),
  })
}

export async function deleteDownstreamAPIKey(apiKeyId: string) {
  return apiFetch<void>(`/api/v1/api-keys/${apiKeyId}`, {
    method: 'DELETE',
  })
}

export type QuotaResetScope = 'total' | 'daily' | 'weekly'

export async function resetDownstreamAPIKeyQuota(apiKeyId: string, scopes: QuotaResetScope[]) {
  return apiFetch<{ api_key: DownstreamAPIKey }>(`/api/v1/api-keys/${apiKeyId}/quota/reset`, {
    method: 'POST',
    body: {
      scopes,
    },
  })
}

export async function updateDownstreamAPIKeyModels(
  apiKeyId: string,
  input: {
    modelPolicy: 'allow_all' | 'allow_list'
    siteModelIds: string[]
  },
) {
  return apiFetch<{
    api_key: DownstreamAPIKey
    items: DownstreamAPIKeyModel[]
  }>(`/api/v1/api-keys/${apiKeyId}/site-models`, {
    method: 'PUT',
    body: {
      model_policy: input.modelPolicy,
      site_model_ids: input.siteModelIds,
    },
  })
}

export async function updateDownstreamAPIKeySites(
  apiKeyId: string,
  input: {
    sitePolicy: 'allow_all' | 'allow_list'
    siteIds: string[]
  },
) {
  return apiFetch<{
    api_key: DownstreamAPIKey
    items: DownstreamAPIKeySite[]
  }>(`/api/v1/api-keys/${apiKeyId}/sites`, {
    method: 'PUT',
    body: {
      site_policy: input.sitePolicy,
      site_ids: input.siteIds,
    },
  })
}

export async function updateDownstreamAPIKeySiteGroups(
  apiKeyId: string,
  input: {
    groupIds: string[]
  },
) {
  return apiFetch<{
    api_key: DownstreamAPIKey
    items: DownstreamAPIKeySiteGroup[]
  }>(`/api/v1/api-keys/${apiKeyId}/site-groups`, {
    method: 'PUT',
    body: {
      group_ids: input.groupIds,
    },
  })
}

export async function updateDownstreamAPIKeyModelMappings(
  apiKeyId: string,
  modelMappings: ModelRule[],
) {
  return apiFetch<{
    api_key: DownstreamAPIKey
  }>(`/api/v1/api-keys/${apiKeyId}/model-mappings`, {
    method: 'PUT',
    body: {
      model_mappings: modelMappings,
    },
  })
}

function apiKeyUpsertBody(input: APIKeyUpsertInput) {
  return {
    name: input.name,
    custom_key: input.customKey,
    status: input.status,
    model_policy: input.modelPolicy,
    site_model_ids: input.modelPolicy === 'allow_list' ? input.siteModelIds : [],
    site_policy: input.sitePolicy,
    site_ids: input.sitePolicy === 'allow_list' ? input.siteIds : [],
    site_group_ids: input.sitePolicy === 'allow_list' ? input.siteGroupIds : [],
    model_mappings: input.modelMappings,
    image_tool_bridge: input.imageToolBridge ?? null,
    quota_limit: input.quotaUnlimited ? null : input.quotaLimit,
    quota_unlimited: input.quotaUnlimited,
    quota_daily_limit: input.quotaDailyUnlimited ? null : input.quotaDailyLimit,
    quota_daily_unlimited: input.quotaDailyUnlimited,
    quota_weekly_limit: input.quotaWeeklyUnlimited ? null : input.quotaWeeklyLimit,
    quota_weekly_unlimited: input.quotaWeeklyUnlimited,
    auto_reset_oauth_connection_id: input.autoResetOAuthConnectionId ?? null,
    rate_limit: input.rateLimit,
    expires_at: input.expiresAt ?? null,
  }
}
