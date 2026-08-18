import { siteTypeIconPath } from '@/components/common/brand-utils'
import { localeFromLanguage as dateTimeLocale } from '@/lib/locale'
import type { Site, SiteAPIKey, SiteAPIKeyModel, SiteModel, SiteQuotaProbeEntry, SiteQuotaProbeResult, SiteQuotaProbeSummary, SiteTypeInfo, SiteValidationResult } from '@/features/sites/api/sites'
import type { SiteUpdatedAtDisplayMode } from '@/features/sites/lib/types'

const UPDATED_AT_MODE_KEY = 'xlyra:sites:updated-at-display-mode'
const siteNameCollator = new Intl.Collator('zh-CN', { numeric: true, sensitivity: 'base' })

export function readUpdatedAtMode(): SiteUpdatedAtDisplayMode {
  try {
    return window.localStorage.getItem(UPDATED_AT_MODE_KEY) === 'absolute' ? 'absolute' : 'relative'
  } catch {
    return 'relative'
  }
}

export function saveUpdatedAtMode(mode: SiteUpdatedAtDisplayMode) {
  try {
    window.localStorage.setItem(UPDATED_AT_MODE_KEY, mode)
  } catch {
    // ignore
  }
}

export function sortSitesForDisplay(items: Site[]) {
  return [...items].sort((a, b) => {
    const aAbnormal = isSiteAbnormal(a)
    const bAbnormal = isSiteAbnormal(b)
    if (aAbnormal !== bAbnormal) return aAbnormal ? 1 : -1

    if (a.enabled !== b.enabled) return a.enabled ? -1 : 1

    const priorityDelta = (b.routing_priority ?? 0) - (a.routing_priority ?? 0)
    if (priorityDelta !== 0) return priorityDelta

    const aTime = new Date(a.created_at).getTime()
    const bTime = new Date(b.created_at).getTime()
    if (!Number.isNaN(aTime) && !Number.isNaN(bTime) && aTime !== bTime) return aTime - bTime

    return a.name.localeCompare(b.name, 'zh-CN')
  })
}

export function sortSitesByName(items: Site[]) {
  return [...items].sort(compareSitesByName)
}

export function compareSitesByName(a: Site, b: Site) {
  const nameDiff = siteNameCollator.compare(a.name, b.name)
  if (nameDiff !== 0) return nameDiff

  const slugDiff = siteNameCollator.compare(a.slug, b.slug)
  if (slugDiff !== 0) return slugDiff

  return a.id.localeCompare(b.id)
}

export function isSiteAbnormal(site: Site, validationOverride?: SiteValidationResult | null) {
  const validation = validationOverride ?? site.validation
  if (validation && validation.ok === false) return true
  if (site.sync_state?.validation_ok === false) return true

  const badStatuses = new Set(['error', 'failed', 'unhealthy', 'invalid', 'unavailable', 'degraded'])
  const siteStatus = site.status?.trim().toLowerCase()
  if (siteStatus && badStatuses.has(siteStatus)) return true

  const syncStatus = site.sync_state?.status?.trim().toLowerCase()
  if (syncStatus && badStatuses.has(syncStatus)) return true

  if (site.sync_state?.failure_class === 'credential_invalid') return true

  const oauthStatus = (site.auth_config?.codex?.status ?? site.auth_config?.antigravity?.status ?? site.auth_config?.claude_code?.status)?.trim().toLowerCase()
  if (oauthStatus && oauthStatus !== 'connected') return true

  return false
}

export function isSiteEnableBlockedByAbnormalState(site: Site, validationOverride?: SiteValidationResult | null) {
  return !site.enabled && isSiteAbnormal(site, validationOverride)
}

export function formatSiteTypeLabel(siteType: string, siteTypes?: SiteTypeInfo[]): string {
  const fromAPI = siteTypes?.find((siteTypeInfo) => siteTypeInfo.site_type === siteType)?.display_name
  if (fromAPI) return fromAPI
  const t = siteType.trim().toLowerCase()
  if (t === 'codex') return 'Codex OAuth'
  if (t === 'antigravity') return 'Antigravity OAuth'
  if (t === 'claude_code') return 'Claude Code OAuth'
  if (t === 'newapi') return 'NewAPI'
  if (t === 'xlyra') return 'xLyra'
  if (t === 'openai' || t === 'openai_compatible') return 'OpenAI'
  if (t === 'anthropic') return 'Anthropic Claude'
  if (t === 'google_gemini') return 'Google Gemini'
  if (t === 'deepseek') return 'DeepSeek'
  if (t === 'minimax') return 'MiniMax'
  if (t === 'xiaomi_mimo') return 'Xiaomi MiMo'
  if (t === 'moonshot') return 'Moonshot'
  if (t === 'kimi_code') return 'Kimi Code'
  if (t === 'zhipu') return 'GLM'
  if (t === 'glm_code') return 'GLM Code'
  return siteType
}

export function siteTypeIcon(siteType: string, siteTypes?: SiteTypeInfo[], resolvedMode: 'light' | 'dark' = 'dark'): string | null {
  const iconPath = siteTypeIconPath(siteType, resolvedMode)
  if (iconPath) return iconPath
  const fromAPI = siteTypes?.find((siteTypeInfo) => siteTypeInfo.site_type === siteType)?.icon_url
  if (fromAPI) return fromAPI
  return null
}

export function siteTypeIconClassName(siteType: string): string | undefined {
  if (siteType === 'grok') return 'bg-white p-1'
  return undefined
}

export function normalizedSiteTypeFilter(siteType: string): string {
  const t = siteType.trim().toLowerCase()
  if (t === 'codex') return 'codex'
  if (t === 'antigravity') return 'antigravity'
  if (t === 'claude_code') return 'claude_code'
  if (t === 'newapi') return 'newapi'
  if (t === 'xlyra') return 'xlyra'
  if (t === 'anthropic') return 'anthropic'
  if (t === 'google_gemini') return 'google_gemini'
  if (t === 'deepseek') return 'deepseek'
  if (t === 'minimax') return 'minimax'
  if (t === 'xiaomi_mimo') return 'xiaomi_mimo'
  if (t === 'moonshot') return 'moonshot'
  if (t === 'kimi_code') return 'kimi_code'
  if (t === 'zhipu') return 'zhipu'
  if (t === 'glm_code') return 'glm_code'
  return 'openai'
}

export function isOAuthSite(site: Site, siteTypes?: SiteTypeInfo[]): boolean {
  const credType = siteTypes?.find((siteTypeInfo) => siteTypeInfo.site_type === site.site_type)?.credential_type
  if (credType) return credType === 'oauth'
  return site.site_type === 'codex' || site.site_type === 'antigravity' || site.site_type === 'claude_code'
}

export function isNewAPISite(site: Site, siteTypes?: SiteTypeInfo[]): boolean {
  const credType = siteTypes?.find((siteTypeInfo) => siteTypeInfo.site_type === site.site_type)?.credential_type
  if (credType) return credType === 'system_token'
  return site.site_type === 'newapi'
}


export function formatDateTime(value: string, language?: string, hourCycle?: 'h23') {
  try {
    return new Intl.DateTimeFormat(dateTimeLocale(language), {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hourCycle,
    }).format(new Date(value))
  } catch {
    return value
  }
}

export function formatRoutingPriority(value?: number) {
  return (typeof value === 'number' && Number.isFinite(value) ? value : 1).toFixed(1)
}

export function formatRelativeDateTime(value: string, now: number, language?: string, t?: (key: string) => string) {
  const time = new Date(value).getTime()
  if (Number.isNaN(time)) return value
  const diffSeconds = Math.round((time - now) / 1000)
  const abs = Math.abs(diffSeconds)
  if (abs < 60) return t ? t('utils.justNow') : 'just now'
  const relativeFormatter = new Intl.RelativeTimeFormat(dateTimeLocale(language), { numeric: 'auto' })
  if (abs < 3600) return relativeFormatter.format(Math.round(diffSeconds / 60), 'minute')
  if (abs < 86400) return relativeFormatter.format(Math.round(diffSeconds / 3600), 'hour')
  if (abs < 2592000) return relativeFormatter.format(Math.round(diffSeconds / 86400), 'day')
  if (abs < 31536000) return relativeFormatter.format(Math.round(diffSeconds / 2592000), 'month')
  return relativeFormatter.format(Math.round(diffSeconds / 31536000), 'year')
}

export function siteDisplayUpdatedAt(site: Site) {
  return site.sync_state?.last_synced_at || site.sync_state?.last_sync_started_at || site.sync_state?.updated_at || site.updated_at
}

export function siteAccountEmail(site: Site) {
  return getString(site.auth_config?.codex?.email) || getString(site.auth_config?.antigravity?.email) || getString(site.auth_config?.claude_code?.email) || stringFromRecord(site.meta, 'oauth_email') || stringFromRecord(site.meta, 'email') || '-'
}

function getObject(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined
}

function getString(value: unknown) {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

function getNumber(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string') {
    const parsed = Number(value)
    return Number.isFinite(parsed) ? parsed : undefined
  }
  return undefined
}

function stringFromRecord(src: Record<string, unknown> | undefined, key: string) {
  return getString(src?.[key])
}

export function formatDisplayQuota(quota: number) {
  const amount = quota / 500_000
  return new Intl.NumberFormat('zh-CN', {
    minimumFractionDigits: amount >= 1 ? 2 : 4,
    maximumFractionDigits: amount >= 1 ? 2 : 4,
  }).format(amount)
}

export function formatProbeAmount(value: number) {
  return new Intl.NumberFormat('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: Math.abs(value) < 1 ? 4 : 2,
  }).format(value)
}

function quotaProbeCurrency(unit?: string) {
  const normalized = (unit ?? 'usd').trim().toUpperCase()
  if (normalized === '' || normalized === 'USD') return { prefix: '$', suffix: '' }
  if (normalized === 'CNY' || normalized === 'RMB') return { prefix: '¥', suffix: '' }
  return { prefix: '', suffix: ` ${normalized.toLowerCase()}` }
}

export function isQuotaProbePercent(probe: SiteQuotaProbeSummary | null | undefined) {
  return probe?.unit?.trim().toLowerCase() === 'percent'
}

function formatProbePercent(value: number) {
  const rounded = Math.round(value * 10) / 10
  return `${new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 }).format(Math.max(rounded, 0))}%`
}

export function formatQuotaProbeBalance(probe: SiteQuotaProbeSummary | null | undefined) {
  if (!probe) return undefined
  if (probe.probe_type === 'kimi') {
    return formatKimiQuotaBalance(probe)
  }
  if (typeof probe.remaining_min !== 'number') {
    if (probe.unlimited !== true) return undefined
    if (typeof probe.used_total === 'number') {
      const { prefix, suffix } = quotaProbeCurrency(probe.unit)
      return `${prefix}${formatProbeAmount(probe.used_total)} / ∞${suffix}`
    }
    return '∞'
  }
  if (isQuotaProbePercent(probe)) {
    return formatProbePercent(probe.remaining_min)
  }
  const { prefix, suffix } = quotaProbeCurrency(probe.unit)
  const remaining = formatProbeAmount(Math.max(probe.remaining_min, 0))
  if (probe.probe_type === 'xlyra' && typeof probe.limit === 'number') {
    return `${prefix}${remaining} / ${prefix}${formatProbeAmount(probe.limit)}${suffix}`
  }
  return `${prefix}${remaining}${suffix}`
}

/** Kimi Coding Plan：额度列显示 "5 小时剩余 / 周剩余" 两个百分比 */
function formatKimiQuotaBalance(probe: SiteQuotaProbeSummary) {
  const entries = probe.entries ?? []
  if (entries.length === 0) {
    return typeof probe.remaining_min === 'number' ? formatProbePercent(probe.remaining_min) : undefined
  }
  const percentOf = (label: string) => {
    const entry = entries.find((item) => item.label === label)
    return typeof entry?.remaining === 'number' ? formatProbePercent(entry.remaining) : '-'
  }
  return `${percentOf('five_hour')} / ${percentOf('weekly')}`
}

export type SiteBalanceDetailLabel =
  | 'keyAvailable'
  | 'totalLimit'
  | 'keyUsed'
  | 'tokenBalance'
  | 'tokenUsed'
  | 'accountBalance'
  | 'used'
  | 'remaining'
  | 'unlimited'
  | 'fiveHourQuota'
  | 'weeklyQuota'
  | 'dailyQuota'
  | 'monthlyQuota'

export type SiteBalanceDetail = {
  /** i18n key 后缀（table.quotaDetails.*）；entry 类行用 labelText 直传 */
  label: SiteBalanceDetailLabel
  labelText?: string
  value: string
  valuePrefix?: SiteBalanceDetailLabel
  extra?: string
}

export function isSub2APIQuotaSite(site: Site) {
  return site.gateway_config?.quota_probe === 'sub2api'
}

export function siteBalanceDetails(site: Site, language?: string): SiteBalanceDetail[] {
  const probe = site.quota_probe
  if (probe) {
    const probeType = probe.probe_type
    if (probeType === 'kimi') {
      return kimiQuotaDetails(probe, language)
    }
    const { prefix, suffix } = quotaProbeCurrency(probe.unit)
    const amount = (value: number) => `${prefix}${formatProbeAmount(value)}${suffix}`
    if (typeof probe.remaining_min !== 'number') {
      if (probe.unlimited !== true) return []
      const usedLabel: SiteBalanceDetailLabel = probeType === 'xlyra' ? 'keyUsed' : probeType === 'newapi' ? 'tokenUsed' : 'used'
      const rows: SiteBalanceDetail[] = []
      if (typeof probe.used_total === 'number') rows.push({ label: usedLabel, value: amount(probe.used_total) })
      rows.push({ label: 'unlimited', value: '∞' })
      return rows
    }
    const remaining = amount(Math.max(probe.remaining_min, 0))
    if (probeType === 'xlyra') {
      const rows: SiteBalanceDetail[] = [{ label: 'keyAvailable', value: remaining }]
      if (typeof probe.limit === 'number') rows.push({ label: 'totalLimit', value: amount(probe.limit) })
      return rows
    }
    if (probeType === 'newapi') return [{ label: 'tokenBalance', value: remaining }]
    return [{ label: 'accountBalance', value: remaining }]
  }
  const fallback = formatSiteBalance(site)
  if (fallback === '-') return []
  return [{ label: 'accountBalance', value: fallback }]
}

const KIMI_ENTRY_ORDER = ['five_hour', 'weekly'] as const

const KIMI_ENTRY_LABELS: Record<string, SiteBalanceDetailLabel> = {
  five_hour: 'fiveHourQuota',
  weekly: 'weeklyQuota',
}

function kimiQuotaDetails(probe: SiteQuotaProbeSummary, language?: string): SiteBalanceDetail[] {
  const entries = probe.entries ?? []
  const ordered = KIMI_ENTRY_ORDER
    .map((key) => entries.find((item) => item.label === key))
    .filter((entry): entry is SiteQuotaProbeEntry => !!entry)
  const rows: SiteBalanceDetail[] = []
  for (const entry of ordered) {
    const label = KIMI_ENTRY_LABELS[entry.label]
    if (!label || typeof entry.remaining !== 'number') continue
    const value = formatProbePercent(entry.remaining)
    let extra: string | undefined
    if (entry.reset_at) {
      const resetAt = formatDateTime(entry.reset_at, language, 'h23')
      if (resetAt) extra = resetAt
    }
    rows.push({ label, value, valuePrefix: 'remaining', extra })
  }
  return rows
}

export function sub2APIKeyQuotaDetails(probe: SiteQuotaProbeResult | null | undefined, language?: string): SiteBalanceDetail[] {
  if (!probe?.entries?.length) return []
  const order = ['balance', 'daily', 'weekly', 'monthly']
  const entries = [...probe.entries].sort((left, right) => {
    const leftIndex = order.indexOf(left.label)
    const rightIndex = order.indexOf(right.label)
    return (leftIndex < 0 ? order.length : leftIndex) - (rightIndex < 0 ? order.length : rightIndex)
  })
  const labels: Record<string, SiteBalanceDetailLabel> = {
    balance: 'accountBalance',
    daily: 'dailyQuota',
    weekly: 'weeklyQuota',
    monthly: 'monthlyQuota',
  }
  return entries.flatMap((entry) => {
    const label = labels[entry.label]
    const labelText = label ? undefined : entry.label
    let value: string
    let valuePrefix: SiteBalanceDetailLabel | undefined
    if (entry.unlimited) {
      value = '∞'
    } else if (typeof entry.remaining === 'number') {
      if (entry.unit?.trim().toLowerCase() === 'percent') {
        value = formatProbePercent(entry.remaining)
        valuePrefix = 'remaining'
      } else {
        const { prefix, suffix } = quotaProbeCurrency(entry.unit)
        value = `${prefix}${formatProbeAmount(Math.max(entry.remaining, 0))}${suffix}`
        if (typeof entry.limit === 'number') {
          value = `${value} / ${prefix}${formatProbeAmount(entry.limit)}${suffix}`
        }
      }
    } else {
      return []
    }
    let extra: string | undefined
    if (entry.reset_at) {
      extra = formatDateTime(entry.reset_at, language, 'h23') || undefined
    }
    return [{ label: label ?? 'accountBalance', labelText, value, valuePrefix, extra }]
  })
}

export function formatSiteBalance(site: Site) {
  const probeBalance = site.quota_probe?.probe_type === 'sub2api' && isQuotaProbePercent(site.quota_probe)
    ? undefined
    : formatQuotaProbeBalance(site.quota_probe)
  if (probeBalance !== undefined) return probeBalance
  if (isOAuthSite(site)) return '-'
  const syncState = getObject(site.sync_state)
  const userSummary = getObject(syncState?.user_summary)
  const userNode = getObject(userSummary?.user) ?? getObject(userSummary?.User)
  const userData = getObject(userNode?.data) ?? getObject(userSummary?.data)
  const quota = getNumber(userData?.quota)
  if (quota === undefined) return '-'
  const amount = formatDisplayQuota(Math.max(quota, 0))
  if (isNewAPISite(site)) return `$${amount}`
  const currency = getString(userData?.currency) ?? getString(userData?.quota_currency)
  if (!currency) return amount
  const c = currency.trim().toUpperCase()
  if (c === 'USD') return `$${amount}`
  if (c === 'CNY' || c === 'RMB') return `¥${amount}`
  return `${amount} ${currency}`
}

export function formatCompactTokens(value: number, includeUnit = true): string {
  const formatted = value >= 1_000_000_000
    ? `${(value / 1_000_000_000).toFixed(2)}B`
    : value >= 1_000_000
      ? `${(value / 1_000_000).toFixed(1)}M`
      : value >= 1_000
        ? `${(value / 1_000).toFixed(1)}k`
        : `${value}`
  return includeUnit ? `${formatted} tokens` : formatted
}

export function siteAPIKeyGroupBadgeVariant(group: string) {
  const normalized = group.trim().toLowerCase()
  const map: Record<string, 'neutral' | 'secondary' | 'success' | 'info' | 'warning' | 'accent'> = {
    default: 'neutral',
    downstream: 'info',
    svip: 'accent',
    vip: 'warning',
    root: 'success',
  }
  if (map[normalized]) return map[normalized]
  const variants = ['secondary', 'success', 'info', 'warning', 'accent', 'neutral'] as const
  let hash = 0
  for (let i = 0; i < normalized.length; i++) hash = (hash * 31 + normalized.charCodeAt(i)) >>> 0
  return variants[hash % variants.length]
}

export function formatAPIKeyValue(value: string, options?: { ensureSKPrefix?: boolean }) {
  const trimmed = value.trim()
  if (!trimmed) return '-'
  if (!options?.ensureSKPrefix) return trimmed
  return trimmed.toLowerCase().startsWith('sk-') ? trimmed : `sk-${trimmed}`
}

export function canCompleteAPIKey(apiKey: SiteAPIKey) {
  return Boolean(apiKey.can_complete || apiKey.secret_missing)
}

export function apiKeyModels(apiKey: SiteAPIKey | null): SiteAPIKeyModel[] {
  if (!apiKey) return []
  if (apiKey.model_items?.length) return apiKey.model_items
  return apiKey.models.map((model) => ({ name: model, enabled: true }))
}

export function siteModelsDisplayCount(site: Site, models: SiteModel[], apiKeys: SiteAPIKey[]) {
  const activeModelCount = models.filter((model) => model.status === 'active').length
  if (!isNewAPISite(site)) return models.length ? activeModelCount : site.model_count ?? 0
  if (!models.length) return site.model_count ?? 0

  const deduped = new Set<string>()
  const activeModelNames = new Set<string>()
  for (const model of models) {
    if (model.status !== 'active') continue
    for (const name of [model.upstream_model_name, model.display_name]) {
      const normalized = name?.trim()
      if (normalized) activeModelNames.add(normalized)
    }
  }

  for (const apiKey of apiKeys) {
    if (!apiKey.enabled) continue
    for (const model of apiKeyModels(apiKey)) {
      const name = model.name.trim()
      if (!model.enabled || !name) continue
      if (!activeModelNames.has(name)) continue
      deduped.add(name)
    }
  }
  return apiKeys.length ? deduped.size : activeModelCount
}
