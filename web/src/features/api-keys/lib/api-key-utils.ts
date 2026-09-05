import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { getProviderCatalogEntry } from '@/lib/brands'
import { localeFromLanguage } from '@/lib/locale'
import { revealDownstreamAPIKey, type APIKeyUpsertInput, type DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import type { CanonicalModelItem, SiteModel } from '@/features/sites/api/sites'
import type { APIKeyFormValues, TimeDisplayMode } from '@/features/api-keys/lib/types'

type TFunction = (key: string, options?: Record<string, unknown>) => string

const LAST_USED_MODE_KEY = 'xlyra:api-keys:last-used-display-mode'

const defaultFormValues: APIKeyFormValues = {
  name: '',
  enabled: true,
  useCustomKey: false,
  customKey: '',
  sitePolicy: 'allow_all',
  siteIds: [],
  siteGroupIds: [],
  modelPolicy: 'allow_all',
  siteModelIds: [],
  modelMappings: [],
  imageBridgeEnabled: false,
  imageBridgeModel: '',
  imageBridgeSiteId: 'auto',
  billingMultiplier: '1',
  quotaLimit: '',
  quotaDailyLimit: '',
  quotaWeeklyLimit: '',
  rateLimitEnabled: false,
  rpmLimit: '',
  tpmLimit: '',
  expiresPermanent: true,
  expiresAt: undefined,
}

const OTHER_BRAND_LABEL = 'Other'

export function formValuesFromAPIKey(apiKey: DownstreamAPIKey | null): APIKeyFormValues {
  if (!apiKey) return defaultFormValues
  const sitePolicy = apiKey.site_policy === 'allow_list' ? 'allow_list' : 'allow_all'
  const modelPolicy = sitePolicy === 'allow_list'
    ? 'allow_list'
    : apiKey.model_policy === 'allow_list' ? 'allow_list' : 'allow_all'

  return {
    name: apiKey.name,
    enabled: isAPIKeyActive(apiKey),
    useCustomKey: false,
    customKey: '',
    sitePolicy,
    siteIds: enabledSiteIds(apiKey),
    siteGroupIds: enabledSiteGroupIds(apiKey),
    modelPolicy,
    siteModelIds: apiKey.model_policy === 'allow_list' ? enabledSiteModelIds(apiKey) : [],
    modelMappings: apiKey.model_mappings ?? [],
    imageBridgeEnabled: apiKey.image_tool_bridge?.enabled ?? false,
    imageBridgeModel: apiKey.image_tool_bridge?.model ?? '',
    imageBridgeSiteId: apiKey.image_tool_bridge?.site_id ?? 'auto',
    billingMultiplier: apiKey.billing_multiplier == null ? '1' : String(apiKey.billing_multiplier),
    quotaLimit: apiKey.quota_unlimited || apiKey.quota_limit == null ? '' : String(apiKey.quota_limit),
    quotaDailyLimit: apiKey.quota_daily_unlimited || apiKey.quota_daily_limit == null ? '' : String(apiKey.quota_daily_limit),
    quotaWeeklyLimit: apiKey.quota_weekly_unlimited || apiKey.quota_weekly_limit == null ? '' : String(apiKey.quota_weekly_limit),
    rateLimitEnabled: apiKey.rate_limit?.status === 'enabled',
    rpmLimit: apiKey.rate_limit?.rpm_limit == null ? '' : String(apiKey.rate_limit.rpm_limit),
    tpmLimit: apiKey.rate_limit?.tpm_limit == null ? '' : String(apiKey.rate_limit.tpm_limit),
    expiresPermanent: !apiKey.expires_at,
    expiresAt: parseAPIKeyExpiresAt(apiKey.expires_at),
  }
}

export function readLastUsedMode(): TimeDisplayMode {
  try {
    return window.localStorage.getItem(LAST_USED_MODE_KEY) === 'absolute' ? 'absolute' : 'relative'
  } catch {
    return 'relative'
  }
}

export function saveLastUsedMode(mode: TimeDisplayMode) {
  try {
    window.localStorage.setItem(LAST_USED_MODE_KEY, mode)
  } catch {
    // ignore
  }
}

export function isAPIKeyActive(apiKey: DownstreamAPIKey) {
  return apiKey.status === 'active'
}

export function sortAPIKeysForDisplay(apiKeys: DownstreamAPIKey[]) {
  return [...apiKeys].sort((a, b) => {
    const activeDelta = Number(isAPIKeyActive(b)) - Number(isAPIKeyActive(a))
    if (activeDelta !== 0) return activeDelta

    const createdDelta = dateValue(a.created_at) - dateValue(b.created_at)
    if (createdDelta !== 0) return createdDelta

    return a.id.localeCompare(b.id)
  })
}

export function buildMappingModelKeys(
  canonicalModels: CanonicalModelItem[],
  siteModels: SiteModel[],
  selectedSiteModelIds: string[],
  currentTargets: string[],
) {
  const canonicalById = new Map(canonicalModels.map((model) => [model.id, model.model_key]))
  const selected = new Set(selectedSiteModelIds)
  const keys = new Set<string>()

  for (const model of siteModels) {
    if (selected.size > 0 && !selected.has(model.id)) continue
    if (!model.canonical_model_id) continue
    const key = canonicalById.get(model.canonical_model_id)
    if (key) keys.add(key)
  }

  const available = [...keys].sort()
  const retained = [...new Set(currentTargets.map((target) => target.trim()).filter(Boolean))]
    .filter((target) => !keys.has(target))
    .sort()
  return [...retained, ...available]
}

function enabledModelKeys(apiKey: DownstreamAPIKey | null) {
  return enabledSiteModels(apiKey)
    .filter((model) => model.enabled !== false)
    .map((model) => model.canonical_model_key || model.model_key || model.upstream_model_name || '')
    .filter(Boolean)
}

function enabledSiteModelIds(apiKey: DownstreamAPIKey | null) {
  return enabledSiteModels(apiKey)
    .filter((model) => model.enabled !== false && model.site_model_id)
    .map((model) => model.site_model_id!)
}

export function enabledSiteModels(apiKey: DownstreamAPIKey | null) {
  return apiKey?.site_models ?? apiKey?.models ?? []
}

function enabledSiteIds(apiKey: DownstreamAPIKey | null) {
  return (apiKey?.sites ?? [])
    .filter((site) => site.enabled !== false)
    .map((site) => site.site_id)
}

function enabledSiteGroupIds(apiKey: DownstreamAPIKey | null) {
  return (apiKey?.site_groups ?? [])
    .filter((group) => group.enabled !== false)
    .map((group) => group.group_id)
}

function dateValue(value: string | null | undefined) {
  const timestamp = value ? Date.parse(value) : NaN
  return Number.isNaN(timestamp) ? Number.MAX_SAFE_INTEGER : timestamp
}

export function toUpdateInput(
  apiKey: DownstreamAPIKey,
  overrides: Partial<APIKeyUpsertInput & { status: 'active' | 'disabled' }>,
): APIKeyUpsertInput {
  const modelPolicy = apiKey.model_policy === 'allow_list' ? 'allow_list' : 'allow_all'
  const sitePolicy = apiKey.site_policy === 'allow_list' ? 'allow_list' : 'allow_all'

  return {
    name: apiKey.name,
    status: overrides.status ?? (isAPIKeyActive(apiKey) ? 'active' : 'disabled'),
    modelPolicy,
    siteModelIds: modelPolicy === 'allow_list' ? enabledSiteModelIds(apiKey) : [],
    sitePolicy,
    siteIds: sitePolicy === 'allow_list' ? enabledSiteIds(apiKey) : [],
    siteGroupIds: sitePolicy === 'allow_list' ? enabledSiteGroupIds(apiKey) : [],
    billingMultiplier: apiKey.billing_multiplier ?? null,
    quotaLimit: apiKey.quota_limit ?? null,
    quotaUnlimited: apiKey.quota_unlimited || apiKey.quota_limit == null,
    quotaDailyLimit: apiKey.quota_daily_limit ?? null,
    quotaDailyUnlimited: apiKey.quota_daily_unlimited || apiKey.quota_daily_limit == null,
    quotaWeeklyLimit: apiKey.quota_weekly_limit ?? null,
    quotaWeeklyUnlimited: apiKey.quota_weekly_unlimited || apiKey.quota_weekly_limit == null,
    rateLimit: {
      status: apiKey.rate_limit?.status === 'enabled' ? 'enabled' : 'disabled',
      rpm_limit: apiKey.rate_limit?.rpm_limit ?? null,
      tpm_limit: apiKey.rate_limit?.tpm_limit ?? null,
    },
    expiresAt: apiKey.expires_at ?? null,
    ...overrides,
  }
}

export function apiKeySearchText(apiKey: DownstreamAPIKey) {
  return [
    apiKey.name,
    apiKey.key,
    apiKey.key_prefix,
    apiKey.masked_key,
    apiKey.status,
    apiKey.model_policy,
    apiKey.site_policy,
    ...enabledSiteModels(apiKey).map((model) => `${model.canonical_model_key ?? model.model_key ?? ''} ${model.upstream_model_name ?? ''} ${model.site_name ?? ''}`),
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()
}

function plaintextOrMaskedKey(apiKey: DownstreamAPIKey) {
  return apiKey.key || apiKey.masked_key || apiKey.key_prefix || '-'
}

export function maskedAPIKeyForDisplay(apiKey: DownstreamAPIKey) {
  if (apiKey.masked_key) return apiKey.masked_key
  if (apiKey.key) return maskSensitiveKey(apiKey.key)
  if (apiKey.key_prefix) return `${apiKey.key_prefix}...`

  return '-'
}

function maskSensitiveKey(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return '-'
  if (trimmed.length <= 14) return `${trimmed.slice(0, 4)}...`

  return `${trimmed.slice(0, 10)}...${trimmed.slice(-6)}`
}

export async function copyAPIKeyToClipboard(apiKey: DownstreamAPIKey, t: TFunction) {
  const value = plaintextOrMaskedKey(apiKey)
  if (value === '-') return
  await copyToClipboard(value, t('table.copySuccess'), t('table.copyFailed'))
}

export type APIKeyCopyVariantId = 'xlyra' | 'sk' | 'original'

export type APIKeyCopyVariant = {
  id: APIKeyCopyVariantId
  label: string
}

// Variant labels are derived from key_kind alone (no plaintext), so the copy
// menu can render without disclosing the key. The plaintext is fetched on demand
// when a variant is actually copied. Generated keys expose xlyra-/sk- prefixes;
// everything else copies the raw key.
export function apiKeyCopyVariants(apiKey: DownstreamAPIKey, t: TFunction): APIKeyCopyVariant[] {
  if ((apiKey.key_kind ?? 'generated') === 'generated') {
    return [
      { id: 'xlyra', label: t('table.copyMenu.xlyra') },
      { id: 'sk', label: t('table.copyMenu.sk') },
    ]
  }
  return [{ id: 'original', label: t('table.copyMenu.original') }]
}

function apiKeyVariantValue(plaintext: string, apiKey: DownstreamAPIKey, variantId: APIKeyCopyVariantId): string {
  const value = plaintext.trim()
  if (!value) return ''
  if ((apiKey.key_kind ?? 'generated') === 'generated') {
    const body = generatedAPIKeyBody(value)
    if (body) {
      if (variantId === 'xlyra') return `xlyra-${body}`
      if (variantId === 'sk') return `sk-${body}`
    }
  }
  return value
}

// Fetches the plaintext on demand (audited server-side), derives the requested
// variant, and copies it. Used by the copy menu instead of reading a plaintext
// key from the list response.
export async function copyAPIKeyVariantToClipboard(apiKey: DownstreamAPIKey, variantId: APIKeyCopyVariantId, t: TFunction) {
  try {
    const revealed = await revealDownstreamAPIKey(apiKey.id)
    const value = apiKeyVariantValue(revealed.key ?? '', apiKey, variantId)
    if (!value) {
      await copyToClipboard('', t('table.copySuccess'), t('table.copyFailed'))
      return
    }
    await copyToClipboard(value, t('table.copySuccess'), t('table.copyFailed'))
  } catch {
    await copyToClipboard('', t('table.copySuccess'), t('table.copyFailed'))
  }
}

function generatedAPIKeyBody(value: string) {
  const trimmed = value.trim()
  const body = trimmed.startsWith('xlyra-')
    ? trimmed.slice('xlyra-'.length)
    : trimmed.startsWith('sk-')
      ? trimmed.slice('sk-'.length)
      : ''
  if (body.length !== 48) return ''
  return /^[A-Za-z0-9]+$/.test(body) ? body : ''
}

export function formatModelPolicy(apiKey: DownstreamAPIKey, t: TFunction) {
  if (apiKey.model_policy !== 'allow_list') {
    if (apiKey.site_policy === 'allow_list') {
      return t('table.policy.allModelsInScope')
    }
    return t('table.policy.allModels')
  }

  return t('table.policy.selectedModels', { count: enabledSiteModelIds(apiKey).length || enabledModelKeys(apiKey).length })
}

export function formatSitePolicy(apiKey: DownstreamAPIKey, t: TFunction) {
  if (apiKey.site_policy !== 'allow_list') {
    return t('table.policy.allSites')
  }

  const groupCount = enabledSiteGroupIds(apiKey).length
  const siteCount = enabledSiteIds(apiKey).length
  if (groupCount > 0) {
    return t('table.policy.selectedSitesAndGroups', { siteCount, groupCount })
  }
  return t('table.policy.selectedSites', { count: siteCount })
}

export function formatSiteTypeLabel(siteType: string) {
  const t = siteType.trim().toLowerCase()
  if (t === 'codex') return 'Codex'
  if (t === 'antigravity') return 'Antigravity'
  if (t === 'newapi') return 'NewAPI'
  if (t === 'openai') return 'OpenAI'
  if (t === 'anthropic') return 'Anthropic'
  if (t === 'deepseek') return 'DeepSeek'
  if (t === 'minimax') return 'MiniMax'
  if (t === 'xiaomi_mimo') return 'Xiaomi MiMo'
  if (t === 'moonshot') return 'Moonshot'
  if (t === 'kimi_code') return 'Kimi Code'
  if (t === 'zhipu') return 'GLM'
  if (t === 'glm_code') return 'GLM Code'
  return siteType
}

export function formatDateTime(value?: string | null) {
  if (!value) return '-'
  try {
    const d = new Date(value)
    const y = d.getFullYear()
    const m = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    const h = String(d.getHours()).padStart(2, '0')
    const min = String(d.getMinutes()).padStart(2, '0')
    return `${y}/${m}/${day} ${h}:${min}`
  } catch {
    return value
  }
}

export function formatRelativeDateTime(value: string, now: number, language: string, t: TFunction) {
  const time = new Date(value).getTime()
  if (Number.isNaN(time)) return value
  const diffSeconds = Math.round((time - now) / 1000)
  const abs = Math.abs(diffSeconds)
  if (abs < 60) return t('table.relative.justNow')

  const relativeFormatter = new Intl.RelativeTimeFormat(localeFromLanguage(language), { numeric: 'auto' })
  if (abs < 3600) return relativeFormatter.format(Math.round(diffSeconds / 60), 'minute')
  if (abs < 86400) return relativeFormatter.format(Math.round(diffSeconds / 3600), 'hour')
  if (abs < 2592000) return relativeFormatter.format(Math.round(diffSeconds / 86400), 'day')
  if (abs < 31536000) return relativeFormatter.format(Math.round(diffSeconds / 2592000), 'month')
  return relativeFormatter.format(Math.round(diffSeconds / 31536000), 'year')
}


function formatQuota(value: number) {
  return new Intl.NumberFormat('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 3,
  }).format(value)
}

export function formatDollarQuota(value: number) {
  return `$${formatQuota(value)}`
}

export function formatCompactDollarQuota(value: number) {
  const absValue = Math.abs(value)
  if (absValue < 1_000) return formatDollarQuota(value)
  if (absValue >= 1_000_000_000) return `$${(value / 1_000_000_000).toLocaleString('en-US', { maximumFractionDigits: 2 })}B`
  if (absValue >= 1_000_000) return `$${(value / 1_000_000).toLocaleString('en-US', { maximumFractionDigits: 2 })}M`
  return `$${(value / 1_000).toLocaleString('en-US', { maximumFractionDigits: 2 })}K`
}

export function hasResettableQuota(apiKey: DownstreamAPIKey) {
  return (!apiKey.quota_unlimited && apiKey.quota_limit != null)
    || (!apiKey.quota_daily_unlimited && apiKey.quota_daily_limit != null)
    || (!apiKey.quota_weekly_unlimited && apiKey.quota_weekly_limit != null)
}

export function formatRateLimitSummary(apiKey: DownstreamAPIKey, t: TFunction) {
  const config = apiKey.rate_limit
  if (!config || config.status !== 'enabled') return t('table.rateLimit.disabled')

  const parts = [
    typeof config.rpm_limit === 'number' ? `${formatCompactLimit(config.rpm_limit)} RPM` : null,
    typeof config.tpm_limit === 'number' ? `${formatCompactLimit(config.tpm_limit)} TPM` : null,
  ].filter(Boolean)

  return parts.length ? parts.join(' / ') : t('table.rateLimit.unlimited')
}

function formatCompactLimit(value: number) {
  return new Intl.NumberFormat('en-US', {
    notation: value >= 10000 ? 'compact' : 'standard',
    maximumFractionDigits: 1,
  }).format(value)
}

export function modelProviderLabel(model: CanonicalModelItem) {
  return getProviderCatalogEntry(model.provider)?.name ?? OTHER_BRAND_LABEL
}

export function mergeModelKeys(current: string[], next: string[]) {
  const result = [...current]
  const seen = new Set(current)

  for (const item of next) {
    if (seen.has(item)) continue
    seen.add(item)
    result.push(item)
  }

  return result
}

function parseAPIKeyExpiresAt(value?: string | null) {
  if (!value) return undefined

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return undefined

  return date
}

export function dateToRFC3339(value?: Date) {
  if (!value) return null
  if (Number.isNaN(value.getTime())) return null

  return value.toISOString()
}

export function isAPIKeyExpired(apiKey: DownstreamAPIKey) {
  if (!apiKey.expires_at) return false
  const time = new Date(apiKey.expires_at).getTime()
  return !Number.isNaN(time) && time <= Date.now()
}

export function sortCanonicalModels(items: CanonicalModelItem[]) {
  return [...items].sort((a, b) => (
    (a.display_name || a.model_key).localeCompare(b.display_name || b.model_key)
  ))
}
