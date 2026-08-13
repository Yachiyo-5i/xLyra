import { apiFetch } from '@/lib/http'

type SiteType = string

export type SiteTypeInfo = {
  site_type: string
  display_name: string
  subtitle: string
  credential_key: string
  credential_type: 'api_key' | 'system_token' | 'oauth' | 'xlyra' | 'grok_sso'
  requires_base_url: boolean
  official_base_url?: string
  show_in_create_dialog: boolean
  icon_url?: string
}

export type Site = {
  id: string
  name: string
  slug: string
  site_type: SiteType
  icon_url?: string
  base_url: string
  status: string
  sync_status?: string | null
  enabled: boolean
  routing_priority: number
  proxy_id?: string | null
  gateway_config?: SiteGatewayConfig | null
  quota_probe?: SiteQuotaProbeSummary | null
  capabilities?: string[]
  supports_multiple_api_keys?: boolean
  supports_api_key_cost_multiplier?: boolean
  meta: Record<string, unknown>
  model_count?: number
  api_key_count?: number
  grok_account_count?: number
  validation?: SiteValidationResult
  sync_state?: SiteSyncState
  auth_config?: SiteAuthConfig
  oauth_account?: {
    provider?: string
    connection_id?: string
    account_id?: string
    email?: string
    plan_type?: string
  } | null
  usage?: {
    request_count?: number
    success_count?: number
    failed_count?: number
    prompt_tokens?: number
    completion_tokens?: number
    total_tokens?: number
    estimated_cost?: number
    currency?: string
    first_request_at?: string | null
    last_request_at?: string | null
  }
  created_at: string
  updated_at: string
}

export type ResponsesToolPolicy = 'passthrough' | 'compatibility'
export type ResponsesHostedTool = 'image_generation'
export type ResponsesImageGenerationPolicy =
  'passthrough' | 'strip_auto_tool' | 'disabled'
export type QuotaProbeType = 'none' | 'sub2api' | 'newapi' | 'xlyra' | 'kimi'

export type SiteQuotaProbeSummary = {
  status?: string
  probe_type?: string
  remaining_min?: number
  unit?: string
  limit?: number
  used?: number
  unlimited?: boolean
  used_total?: number
  ok_count?: number
  credential_count?: number
  fetched_at?: string
  plan?: string
  entries?: SiteQuotaProbeEntry[]
}

export type SiteQuotaProbeEntry = {
  label: string
  unit?: string
  remaining?: number
  limit?: number
  used?: number
  unlimited?: boolean
  reset_at?: string
}

export type SiteQuotaProbeResult = {
  status?: string
  error?: string
  kind?: string
  plan?: string
  entries?: SiteQuotaProbeEntry[]
  fetched_at?: string
}

export type SiteGatewayConfig = {
  request_timeout_ms?: number
  connect_timeout_ms?: number
  response_header_timeout_ms?: number
  max_concurrency?: number
  max_model_concurrency?: number
  max_credential_concurrency?: number
  max_idle_conns?: number
  max_idle_conns_per_host?: number
  max_conns_per_host?: number
  idle_conn_timeout_ms?: number
  responses_tool_policy?: ResponsesToolPolicy
  disabled_responses_tools?: ResponsesHostedTool[]
  responses_image_generation_policy?: ResponsesImageGenerationPolicy
  impersonate_codex_client?: boolean
  impersonate_claude_code_client?: boolean
  quota_probe?: string
}

type SiteAuthConfig = {
  newapi?: {
    access_token: string
    user_id: number
  }
  xlyra?: {
    auth_mode?: 'access_token' | 'api_key'
    access_token?: string
  }
  codex?: {
    provider?: string
    connection_id?: string
    status?: string
    email?: string | null
    account_id?: string | null
    plan_type?: string | null
    expires_at?: string | null
    last_refresh_at?: string | null
    last_sync_at?: string | null
    quota?: unknown
  }
  antigravity?: {
    provider?: string
    connection_id?: string
    status?: string
    email?: string | null
    account_id?: string | null
    plan_type?: string | null
    expires_at?: string | null
    last_refresh_at?: string | null
    last_sync_at?: string | null
    quota?: unknown
  }
  claude_code?: {
    provider?: string
    connection_id?: string
    status?: string
    email?: string | null
    account_id?: string | null
    plan_type?: string | null
    expires_at?: string | null
    last_refresh_at?: string | null
    last_sync_at?: string | null
    quota?: unknown
  }
}

type SiteCredentialSummary = {
  id: string
  site_id: string
  credential_type: string
  masked_secret: string
  created_at: string
  updated_at: string
}

export type SiteModel = {
  id: string
  site_id: string
  canonical_model_id?: string | null
  upstream_model_name: string
  display_name: string
  capabilities: Record<string, unknown>
  status: string
  enabled?: boolean
  canonical_match_source?: string
  canonical_match_confidence?: number
  canonical_matched_at?: string | null
  created_at: string
  updated_at: string
}

export type CreateManualSiteModelInput = {
  upstreamModelName: string
  canonicalModelId: string
  supportedEndpointTypes: string[]
  siteCredentialIds?: string[]
  enabled?: boolean
}

export type SiteModelTestResult = {
  ok: boolean
  site: {
    id: string
    name: string
    site_type: string
  }
  model: {
    id: string
    upstream_name: string
    canonical_model?: string | null
  }
  request: {
    downstream_path?: string | null
    upstream_path?: string | null
    upstream_protocol?: string | null
    site_credential_id?: string | null
    credential_name?: string | null
    credential_masked?: string | null
    credential_group?: string | null
    credential_routing_priority?: number | null
    credential_upstream_cost_multiplier?: number | null
    credential_attempt?: number | null
    credential_total?: number | null
  }
  result: {
    status_code?: number | null
    latency_ms?: number | null
    upstream_latency_ms?: number | null
    prompt_tokens?: number | null
    completion_tokens?: number | null
    total_tokens?: number | null
    estimated_cost?: number | null
    currency?: string | null
    response_preview?: string | null
    error_type?: string | null
    error_message?: string | null
    failure_response?: unknown
  }
  request_log_id?: string | null
}

export type SiteModelTestProtocol =
  'auto' | 'chat_completions' | 'responses' | 'messages'

export type SiteModelTestInput = {
  prompt?: string
  timeoutMs?: number
  protocol?: SiteModelTestProtocol
  stream?: boolean
  siteCredentialId?: string
}

export type CanonicalModel = {
  id: string
  model_key: string
  display_name: string
  provider: string
  category: string
  capabilities: Record<string, unknown>
  status: string
  created_at: string
  updated_at: string
}

export type CanonicalModelAlias = {
  id: string
  canonical_model_id: string
  alias: string
  normalized_alias: string
  source: string
  created_at: string
  updated_at: string
}

export type CanonicalModelItem = CanonicalModel & {
  site_model_count: number
  site_count: number
  icon_url?: string
  aliases: CanonicalModelAlias[]
}

export type CreateCanonicalModelInput = {
  modelKey: string
  displayName?: string
  provider?: string
  category?: string
  status?: string
}

type CanonicalModelMatrixPricing = {
  group_name: string
  currency?: string | null
  input_value?: number | null
  output_value?: number | null
  per_request_value?: number | null
  billing_type?: string | null
  available?: boolean | null
  last_synced_at?: string | null
}

export type CanonicalModelMatrixRow = {
  site_id: string
  site_name: string
  site_slug: string
  site_type: string
  site_enabled: boolean
  site_model_id: string
  upstream_model_name: string
  display_name: string
  model_status: string
  canonical_match_source: string
  canonical_match_confidence: number
  canonical_matched_at?: string | null
  api_key_count: number
  available_api_key_count: number
  pricing: CanonicalModelMatrixPricing[]
  created_at: string
  updated_at: string
}

export type CanonicalModelMatrix = {
  canonical_model: CanonicalModel
  items: CanonicalModelMatrixRow[]
  meta?: {
    count?: number
  }
}

export type SiteAPIKey = {
  id: string
  upstream_id?: number
  name: string
  display_name?: string
  upstream_name?: string
  routing_priority: number
  upstream_cost_multiplier: number
  group?: string | null
  key: string
  copy_key?: string
  status: string
  sync_status?: string | null
  enabled: boolean
  secret_missing?: boolean
  can_complete?: boolean
  models: string[]
  model_items?: SiteAPIKeyModel[]
  usage?: {
    data?: {
      total_available?: number | null
      total_granted?: number | null
      unlimited_quota?: boolean
    }
    source?: string
    official_realtime_available?: boolean
    available?: boolean
    limit_name?: string
    workspace?: string
    retry_after_seconds?: number
    reset_at?: string
    observed_at?: string
  }
  quota_probe?: SiteQuotaProbeResult | null
  message?: string
}

export type SiteAPIKeyModel = {
  name: string
  enabled: boolean
}

export type GrokAccountModel = {
  upstream_name: string
  display_name: string
  enabled: boolean
}

export type GrokAccount = {
  credential_id: string
  masked_token: string
  enabled: boolean
  tier?: string | null
  sync_status?: string | null
  sync_message?: string | null
  remaining_quota?: number | null
  used_quota?: number | null
  model_count?: number
  models?: GrokAccountModel[]
}

export type GrokAccountsResponse = {
  items: GrokAccount[]
  meta?: {
    count?: number
    failed?: number
  }
}

export type SiteModelPricing = {
  id: string
  site_id: string
  site_name: string
  site_slug: string
  site_type: SiteType | string
  site_enabled: boolean
  site_model_id?: string | null
  model_name: string
  group_name: string
  quota_type: number
  billing_type: string
  currency: string
  group_ratio: number
  model_ratio?: number | null
  completion_ratio?: number | null
  cache_ratio?: number | null
  model_price?: number | null
  per_request_value?: number | null
  input_value?: number | null
  output_value?: number | null
  cache_input_value?: number | null
  create_cache_input_value?: number | null
  create_cache_1h_input_value?: number | null
  audio_output_value?: number | null
  vendor_id?: number | null
  vendor_name?: string | null
  vendor_icon?: string | null
  description?: string | null
  owner_by?: string | null
  available: boolean
  raw: unknown
  last_synced_at?: string | null
  created_at: string
  updated_at: string
  credential_pricings?: SiteCredentialPricing[]
}

export type SiteCredentialPricing = {
  credential_id: string
  credential_name: string
  routing_priority: number
  group_ratio: number
  credential_enabled: boolean
  credential_usable: boolean
  model_enabled: boolean
  model_available: boolean
  billing_type?: string | null
  currency?: string | null
  input_value?: number | null
  output_value?: number | null
  cache_input_value?: number | null
  create_cache_input_value?: number | null
  create_cache_1h_input_value?: number | null
  audio_output_value?: number | null
  per_request_value?: number | null
}

export type SiteValidationResult = {
  ok: boolean
  message?: string
}

type SiteSyncState = {
  status: string
  message?: string | null
  failure_class?: 'unknown' | 'limited' | 'transient' | 'credential_invalid' | null
  validation_ok?: boolean | null
  validation_message?: string | null
  last_sync_started_at?: string | null
  last_synced_at?: string | null
  api_key_count?: number
  model_count?: number
  raw_status?: unknown
  user_summary?: unknown
  pricing?: unknown
  checkin?: unknown
  updated_at?: string
}

type NewAPIUserSummaryResult = {
  user: unknown
  api_keys: unknown
  user_models: unknown
  pricing: unknown
  checkin: unknown
  checkin_ready: boolean
}

export type SiteUpsertInput = {
  name: string
  slug: string
  siteType: SiteType
  baseUrl: string
  enabled: boolean
  routingPriority: number
  proxyId?: string | null
  openAICompatibleApiKey?: string
  apiKeys?: SiteCreateAPIKeyInput[]
  newapi?: {
    accessToken: string
    userId: number
  }
  xlyra?: {
    authMode: 'access_token' | 'api_key'
    accessToken?: string
    apiKey?: string
  }
  requestHeaders?: { key: string; value: string }[]
  gateway?: {
    responsesToolPolicy?: ResponsesToolPolicy
    disabledResponsesTools?: ResponsesHostedTool[]
    impersonateCodexClient?: boolean
    impersonateClaudeCodeClient?: boolean
    quotaProbe?: QuotaProbeType
  }
  skipRefresh?: boolean
}

export type SiteCreateAPIKeyInput = {
  apiKey: string
  name: string
  routingPriority: number
  upstreamCostMultiplier?: number
}

export type CreateSiteInput = SiteUpsertInput
export type UpdateSiteInput = SiteUpsertInput

type NewAPISetupStep<T> = {
  ok: boolean
  data?: T
  message?: string
}

export type CreateSiteResponse = {
  ok?: boolean
  message?: string
  site: Site
  credential?: SiteCredentialSummary
  validation?: SiteValidationResult
  model_sync?: {
    ok: boolean
    count?: number
    items?: SiteModel[]
    message?: string
  }
  newapi?: {
    api_keys_summary?: {
      ok: boolean
      count: number
      items: SiteAPIKey[]
    }
    user_summary?: NewAPISetupStep<NewAPIUserSummaryResult>
  }
}

export type RefreshSiteResponse = {
  ok: boolean
  message?: string
  site: Site
  state?: SiteSyncState
  model_sync?: {
    ok: boolean
    count?: number
    items?: SiteModel[]
    message?: string
  }
  api_key_sync?: {
    ok: boolean
    count?: number
    message?: string
  }
}

export type RefreshAllSitesResponse = {
  ok: boolean
  items: RefreshSiteResponse[]
  meta?: {
    count?: number
    success_count?: number
    failure_count?: number
  }
}

export const sitesQueryKeys = {
  all: ['sites'] as const,
  list: (
    oauth: SiteOAuthFilter = 'exclude',
    deleted: SiteDeletedFilter = 'exclude',
  ) => [...sitesQueryKeys.all, 'list', oauth, deleted] as const,
  listWithOAuth: () => sitesQueryKeys.list('all'),
  listOAuthOnly: () => sitesQueryKeys.list('only'),
  detail: (siteId: string) =>
    [...sitesQueryKeys.all, 'detail', siteId] as const,
  models: (siteId: string) =>
    [...sitesQueryKeys.all, 'models', siteId] as const,
}

export type SiteOAuthFilter = 'all' | 'exclude' | 'only'
export type SiteDeletedFilter = 'exclude' | 'with_requests'

export async function listSites(
  options: { oauth?: SiteOAuthFilter; deleted?: SiteDeletedFilter } = {},
) {
  const oauth = options.oauth ?? 'exclude'
  const deleted = options.deleted ?? 'exclude'
  const params = new URLSearchParams({ oauth, deleted })
  return apiFetch<{ items: Site[]; meta: { count: number } }>(
    `/api/v1/sites?${params.toString()}`,
  )
}

export async function listSitesWithOAuth() {
  return listSites({ oauth: 'all' })
}

export async function listSiteTypes() {
  return apiFetch<{ items: SiteTypeInfo[]; meta: { count: number } }>(
    '/api/v1/site-types',
  )
}

export async function getSite(siteId: string) {
  return apiFetch<{ site: Site }>(`/api/v1/sites/${siteId}`)
}

export async function createSite(input: CreateSiteInput) {
  return apiFetch<CreateSiteResponse>('/api/v1/sites', {
    method: 'POST',
    body: siteUpsertBody(input),
  })
}

export async function updateSite(siteId: string, input: UpdateSiteInput) {
  return apiFetch<CreateSiteResponse>(`/api/v1/sites/${siteId}`, {
    method: 'PUT',
    body: siteUpsertBody(input),
  })
}

export async function updateSiteEnabled(
  siteId: string,
  input: { enabled: boolean },
) {
  return apiFetch<{ site: Site }>(`/api/v1/sites/${siteId}/enabled`, {
    method: 'PUT',
    body: input,
  })
}

export async function deleteSite(siteId: string) {
  return apiFetch<void>(`/api/v1/sites/${siteId}`, {
    method: 'DELETE',
  })
}

function siteUpsertBody(input: SiteUpsertInput) {
  return {
    name: input.name,
    slug: input.slug,
    site_type: input.siteType,
    base_url: input.baseUrl,
    enabled: input.enabled,
    routing_priority: input.routingPriority,
    proxy_id: input.proxyId ?? '',
    skip_refresh: input.skipRefresh,
    request_headers: input.requestHeaders?.map((h) => ({ key: h.key, value: h.value })),
    gateway: input.gateway
      ? {
          responses_tool_policy: input.gateway.responsesToolPolicy,
          disabled_responses_tools: input.gateway.disabledResponsesTools,
          impersonate_codex_client: input.gateway.impersonateCodexClient,
          impersonate_claude_code_client:
            input.gateway.impersonateClaudeCodeClient,
          quota_probe: input.gateway.quotaProbe ?? 'none',
        }
      : undefined,
    api_key: input.openAICompatibleApiKey || undefined,
    api_keys: input.apiKeys?.map((apiKey) => ({
      api_key: apiKey.apiKey,
      name: apiKey.name,
      routing_priority: apiKey.routingPriority,
      upstream_cost_multiplier: apiKey.upstreamCostMultiplier,
    })),
    newapi: input.newapi
      ? {
          access_token: input.newapi.accessToken,
          user_id: input.newapi.userId,
        }
      : undefined,
    xlyra: input.xlyra
      ? {
          auth_mode: input.xlyra.authMode,
          access_token: input.xlyra.accessToken || undefined,
          api_key: input.xlyra.apiKey || undefined,
        }
      : undefined,
  }
}

export async function refreshSite(siteId: string) {
  return apiFetch<RefreshSiteResponse>(`/api/v1/sites/${siteId}/refresh`, {
    method: 'POST',
  })
}

export async function refreshAllSites() {
  return apiFetch<RefreshAllSitesResponse>('/api/v1/sites/refresh', {
    method: 'POST',
  })
}

export async function listSiteModels(siteId: string) {
  return apiFetch<{ items: SiteModel[]; meta?: { count?: number } }>(
    `/api/v1/sites/${siteId}/models`,
  )
}

export async function createManualSiteModel(
  siteId: string,
  input: CreateManualSiteModelInput,
) {
  return apiFetch<{ model: SiteModel }>(`/api/v1/sites/${siteId}/models`, {
    method: 'POST',
    body: {
      upstream_model_name: input.upstreamModelName,
      canonical_model_id: input.canonicalModelId,
      supported_endpoint_types: input.supportedEndpointTypes,
      site_credential_ids: input.siteCredentialIds ?? [],
      enabled: input.enabled ?? true,
    },
  })
}

export async function deleteSiteModel(siteId: string, modelId: string) {
  return apiFetch<void>(`/api/v1/sites/${siteId}/models/${modelId}`, {
    method: 'DELETE',
  })
}

export async function updateSiteModelStatus(
  siteId: string,
  modelId: string,
  input: { enabled: boolean },
) {
  return apiFetch<{ model: SiteModel }>(
    `/api/v1/sites/${siteId}/models/${modelId}`,
    {
      method: 'PUT',
      body: input,
    },
  )
}

export async function updateSiteModelsStatus(
  siteId: string,
  input: { modelIds: string[]; enabled: boolean },
) {
  return apiFetch<{ items: SiteModel[]; meta?: { count?: number } }>(
    `/api/v1/sites/${siteId}/models/status`,
    {
      method: 'PUT',
      body: {
        model_ids: input.modelIds,
        enabled: input.enabled,
      },
    },
  )
}

export async function testSiteModel(
  siteId: string,
  modelId: string,
  input?: SiteModelTestInput,
) {
  return apiFetch<SiteModelTestResult>(
    `/api/v1/sites/${siteId}/models/${modelId}/test`,
    {
      method: 'POST',
      body: input
        ? {
            prompt: input.prompt,
            timeout_ms: input.timeoutMs,
            protocol: input.protocol,
            stream: input.stream,
            site_credential_id: input.siteCredentialId,
          }
        : undefined,
    },
  )
}

export async function listSiteAPIKeys(siteId: string) {
  return apiFetch<{ items: SiteAPIKey[]; meta?: { count?: number } }>(
    `/api/v1/sites/${siteId}/api-keys`,
  )
}

// List responses only carry the masked secret; the plaintext is fetched on
// demand from this audited single-key endpoint when the user copies a key.
export async function revealSiteAPIKey(siteId: string, apiKeyId: string) {
  return apiFetch<{ id: string; copy_key: string }>(
    `/api/v1/sites/${siteId}/api-keys/${apiKeyId}/reveal`,
  )
}

export type SiteAPIKeyConfigInput = {
  name?: string
  routingPriority?: number
  upstreamCostMultiplier?: number
}

export async function createSiteAPIKey(
  siteId: string,
  input: { apiKey: string } & SiteAPIKeyConfigInput,
) {
  return apiFetch<{ api_key: SiteAPIKey }>(`/api/v1/sites/${siteId}/api-keys`, {
    method: 'POST',
    body: {
      api_key: input.apiKey,
      name: input.name,
      routing_priority: input.routingPriority,
      upstream_cost_multiplier: input.upstreamCostMultiplier,
    },
  })
}

export async function deleteSiteAPIKey(siteId: string, apiKeyId: string) {
  return apiFetch<void>(`/api/v1/sites/${siteId}/api-keys/${apiKeyId}`, {
    method: 'DELETE',
  })
}

export async function listAllSitePricings() {
  return apiFetch<{ items: SiteModelPricing[]; meta?: { count?: number } }>(
    '/api/v1/site-pricings',
  )
}

export async function listCanonicalModels() {
  return apiFetch<{ items: CanonicalModelItem[]; meta?: { count?: number } }>(
    '/api/v1/models',
  )
}

export async function createCanonicalModel(input: CreateCanonicalModelInput) {
  return apiFetch<{ model: CanonicalModel }>('/api/v1/models', {
    method: 'POST',
    body: {
      model_key: input.modelKey,
      display_name: input.displayName,
      provider: input.provider,
      category: input.category,
      status: input.status ?? 'active',
    },
  })
}

export async function createCanonicalModelAlias(
  modelId: string,
  alias: string,
) {
  return apiFetch<{ alias: CanonicalModelAlias }>(
    `/api/v1/models/${modelId}/aliases`,
    {
      method: 'POST',
      body: { alias },
    },
  )
}

export async function deleteCanonicalModelAlias(
  modelId: string,
  aliasId: string,
) {
  return apiFetch<void>(`/api/v1/models/${modelId}/aliases/${aliasId}`, {
    method: 'DELETE',
  })
}

export async function archiveCanonicalModel(modelId: string) {
  return apiFetch<void>(`/api/v1/models/${modelId}`, {
    method: 'DELETE',
  })
}

export async function getCanonicalModelMatrix(modelId: string) {
  return apiFetch<CanonicalModelMatrix>(`/api/v1/models/${modelId}/matrix`)
}

export async function bindSiteModelCanonical(
  siteModelId: string,
  canonicalModelId: string | null,
) {
  return apiFetch<{ model: SiteModel }>(
    `/api/v1/site-models/${siteModelId}/canonical`,
    {
      method: 'PUT',
      body: { canonical_model_id: canonicalModelId },
    },
  )
}

export async function updateSiteAPIKeyStatus(
  siteId: string,
  apiKeyId: string,
  input: { enabled: boolean },
) {
  return apiFetch<{ api_key: SiteAPIKey }>(
    `/api/v1/sites/${siteId}/api-keys/${apiKeyId}`,
    {
      method: 'PUT',
      body: input,
    },
  )
}

export async function updateSiteAPIKeyConfig(
  siteId: string,
  apiKeyId: string,
  input: SiteAPIKeyConfigInput,
) {
  return apiFetch<{ api_key: SiteAPIKey }>(
    `/api/v1/sites/${siteId}/api-keys/${apiKeyId}`,
    {
      method: 'PUT',
      body: {
        name: input.name,
        routing_priority: input.routingPriority,
        upstream_cost_multiplier: input.upstreamCostMultiplier,
      },
    },
  )
}

export async function updateSiteAPIKeySecret(
  siteId: string,
  apiKeyId: string,
  input: { apiKey: string },
) {
  return apiFetch<{ api_key: SiteAPIKey }>(
    `/api/v1/sites/${siteId}/api-keys/${apiKeyId}/secret`,
    {
      method: 'PUT',
      body: { api_key: input.apiKey },
    },
  )
}

export async function updateSiteAPIKeyModelStatus(
  siteId: string,
  apiKeyId: string,
  input: { model: string; enabled: boolean },
) {
  return apiFetch<{ api_key: SiteAPIKey }>(
    `/api/v1/sites/${siteId}/api-keys/${apiKeyId}/models`,
    {
      method: 'PUT',
      body: input,
    },
  )
}

export async function refreshSiteAPIKey(siteId: string, apiKeyId: string) {
  return apiFetch<{ api_key: SiteAPIKey }>(
    `/api/v1/sites/${siteId}/api-keys/${apiKeyId}/refresh`,
    {
      method: 'POST',
    },
  )
}

export async function listGrokAccounts(siteId: string) {
  return apiFetch<GrokAccountsResponse>(`/api/v1/sites/${siteId}/grok/accounts`)
}

export async function updateGrokAccount(
  siteId: string,
  credentialId: string,
  input: { enabled?: boolean },
) {
  return apiFetch<{ account: GrokAccount }>(
    `/api/v1/sites/${siteId}/grok/accounts/${credentialId}`,
    {
      method: 'PUT',
      body: { enabled: input.enabled },
    },
  )
}

export async function updateGrokAccountModel(
  siteId: string,
  credentialId: string,
  input: { model: string; enabled: boolean },
) {
  return apiFetch<{ account: GrokAccount }>(
    `/api/v1/sites/${siteId}/grok/accounts/${credentialId}/models`,
    {
      method: 'PUT',
      body: input,
    },
  )
}

export async function deleteGrokAccount(siteId: string, credentialId: string) {
  return apiFetch<void>(
    `/api/v1/sites/${siteId}/grok/accounts/${credentialId}`,
    {
      method: 'DELETE',
    },
  )
}

export async function refreshGrokAccount(siteId: string, credentialId: string) {
  return apiFetch<{ account: GrokAccount }>(
    `/api/v1/sites/${siteId}/grok/accounts/${credentialId}/refresh`,
    {
      method: 'POST',
    },
  )
}

export async function refreshGrokAccounts(siteId: string) {
  return apiFetch<GrokAccountsResponse>(
    `/api/v1/sites/${siteId}/grok/accounts/refresh`,
    {
      method: 'POST',
    },
  )
}

export type GrokDeviceStart = {
  device_code: string
  user_code: string
  verification_uri: string
  verification_uri_complete: string
  interval: number
  expires_at: string
}

export type GrokDevicePollResult =
  | { status: 'pending' | 'slow_down' }
  | { status: 'complete'; account: GrokAccount }

export async function startGrokDeviceLogin(siteId: string) {
  return apiFetch<{ device: GrokDeviceStart }>(
    `/api/v1/sites/${siteId}/grok/device/start`,
    {
      method: 'POST',
    },
  )
}

export async function pollGrokDeviceLogin(siteId: string, deviceCode: string, credentialId?: string) {
  return apiFetch<{ device: GrokDevicePollResult }>(
    `/api/v1/sites/${siteId}/grok/device/poll`,
    {
      method: 'POST',
      body: { device_code: deviceCode, ...(credentialId ? { credential_id: credentialId } : {}) },
    },
  )
}
