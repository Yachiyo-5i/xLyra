import { createElement, type ReactNode } from 'react'
import type { TFunction } from 'i18next'
import { localeFromLanguage } from '@/lib/locale'
import { BadgeDollarSign, Clock3, Gauge, Route, ShieldAlert } from 'lucide-react'
import type {
  CooldownItem,
  DashboardRiskItem,
  DashboardSeries,
  DashboardTrendDatum,
  SiteCostDatum,
  SiteUptimeItem,
  UptimeStatus,
} from '@/features/dashboard/components'
import type {
  DashboardAttentionItem,
  DashboardDailyAPIKeyUsagePoint,
  DashboardCooldownAPIItem,
  DashboardDailyModelCostPoint,
  DashboardDailyModelRequestPoint,
  DashboardDailySiteCostPoint,
  DashboardDailySiteRequestPoint,
  DashboardDays,
  DashboardOverview,
} from '@/features/dashboard/api/dashboard'

type DailyPoint = {
  date: string
}

type TrendResult = {
  data: DashboardTrendDatum[]
  series: DashboardSeries[]
}

export type ApiKeyContributionKeyOption = {
  id: string
  name: string
  totalTokens: number
}

export type ApiKeyContributionDay = {
  date: string
  tokens: number
  cost: number
  currency: string
  level: 0 | 1 | 2 | 3 | 4
}

export type ApiKeyContributions = {
  keys: ApiKeyContributionKeyOption[]
  defaultKeyId: string
  selectedKey?: ApiKeyContributionKeyOption
  days: ApiKeyContributionDay[]
  currentStreak: number
  longestStreak: number
  monthTokens: number
}

export function formatDashboardCurrency(value: number, currency = 'USD', digits = 2) {
  if (currency === 'USD') return `$${value.toFixed(digits)}`
  return `${value.toFixed(digits)} ${currency}`
}

export function formatDashboardTokens(value: number) {
  const absValue = Math.abs(value)
  if (absValue >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(2)} B`
  if (absValue >= 1_000_000) return `${(value / 1_000_000).toFixed(2)} M`
  if (absValue >= 1_000) return `${(value / 1_000).toFixed(2)} K`
  return String(Math.round(value))
}

export function formatCompactNumber(value: number) {
  return new Intl.NumberFormat('en', {
    notation: value >= 10000 ? 'compact' : 'standard',
    maximumFractionDigits: value >= 10000 ? 1 : 0,
  }).format(value)
}

export function formatPercent(value?: number | null, digits = 1) {
  if (typeof value !== 'number') return '-'
  return `${(value * 100).toFixed(digits)}%`
}

export function formatLimitValue(used: number, limit?: number | null) {
  if (!limit) return formatCompactNumber(used)
  return `${formatCompactNumber(used)} / ${formatCompactNumber(limit)}`
}

export function formatDashboardRefreshTime(value?: string | null, language?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleTimeString(localeFromLanguage(language), {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}


export function dashboardRangeToDays(range: string): DashboardDays {
  if (range === '30d') return 30
  if (range === '90d') return 90
  return 7
}

export function dashboardDaysToRange(days: DashboardDays) {
  return `${days}d` as '7d' | '30d' | '90d'
}

export function buildRangedDailyModelCost(
  points: DashboardDailyModelCostPoint[],
  overview: DashboardOverview,
  days: DashboardDays,
): TrendResult {
  return buildDailyTrend(points, overview, days, (point) => point.model_key, (point) => point.cost)
}

export function buildRangedDailyModelRequests(
  points: DashboardDailyModelRequestPoint[],
  overview: DashboardOverview,
  days: DashboardDays,
): TrendResult {
  return buildDailyTrend(points, overview, days, (point) => point.model_key, (point) => point.request_count)
}

export function buildRangedDailySiteCost(
  points: DashboardDailySiteCostPoint[],
  overview: DashboardOverview,
  days: DashboardDays,
): TrendResult {
  return buildDailyTrend(points, overview, days, (point) => point.site_key, (point) => point.cost)
}

export function buildRangedDailySiteRequests(
  points: DashboardDailySiteRequestPoint[],
  overview: DashboardOverview,
  days: DashboardDays,
): TrendResult {
  return buildDailyTrend(points, overview, days, (point) => point.site_key, (point) => point.request_count)
}

export function buildSiteCosts(items: DashboardOverview['charts']['site_cost_summary']): SiteCostDatum[] {
  return items.map((item) => ({
    site: item.site_name || item.site_slug || item.site_id,
    cost: item.cost,
  }))
}

export function buildApiKeyContributions(
  points: DashboardDailyAPIKeyUsagePoint[],
  overview: DashboardOverview,
  days: number,
  selectedKeyId?: string,
): ApiKeyContributions {
  const keysById = new Map<string, ApiKeyContributionKeyOption>()
  for (const point of points) {
    const apiKeyId = point.api_key_id
    if (!apiKeyId) continue
    const existing = keysById.get(apiKeyId)
    if (existing) {
      existing.totalTokens += point.total_tokens
      if (!existing.name && point.api_key_name) existing.name = point.api_key_name
      continue
    }
    keysById.set(apiKeyId, {
      id: apiKeyId,
      name: point.api_key_name || apiKeyId,
      totalTokens: point.total_tokens,
    })
  }

  const keys = [...keysById.values()].sort((a, b) => {
    if (b.totalTokens !== a.totalTokens) return b.totalTokens - a.totalTokens
    return a.name.localeCompare(b.name)
  })
  const defaultKeyId = keys[0]?.id ?? ''
  const effectiveKeyId = selectedKeyId && keysById.has(selectedKeyId) ? selectedKeyId : defaultKeyId
  const selectedKey = effectiveKeyId ? keysById.get(effectiveKeyId) : undefined
  const rangeDates = enumerateDateKeys(contributionRangeStart(overview, days), days)
  const byDate = new Map<string, { tokens: number; cost: number; currency: string }>()
  const monthStart = overview.meta.today_start.slice(0, 8) + '01'
  const today = overview.meta.today_start.slice(0, 10)
  let monthTokens = 0

  for (const point of points) {
    if (point.api_key_id !== effectiveKeyId) continue
    const current = byDate.get(point.date) ?? { tokens: 0, cost: 0, currency: point.currency || 'USD' }
    current.tokens += point.total_tokens
    current.cost += point.cost
    if (point.currency) current.currency = point.currency
    byDate.set(point.date, current)
    if (point.date >= monthStart && point.date <= today) {
      monthTokens += point.total_tokens
    }
  }

  const maxTokens = Math.max(0, ...rangeDates.map((date) => byDate.get(date)?.tokens ?? 0))
  const daysOut = rangeDates.map((date) => {
    const item = byDate.get(date)
    const tokens = item?.tokens ?? 0
    return {
      date,
      tokens,
      cost: item?.cost ?? 0,
      currency: item?.currency ?? 'USD',
      level: contributionLevel(tokens, maxTokens),
    }
  })

  return {
    keys,
    defaultKeyId,
    selectedKey,
    days: daysOut,
    currentStreak: currentContributionStreak(daysOut),
    longestStreak: longestContributionStreak(daysOut),
    monthTokens,
  }
}

export function buildUptimeRows(items: DashboardOverview['health']['uptime_rows']): SiteUptimeItem[] {
  return items.map((row) => {
    let successCount = 0
    let totalCount = 0
    for (const bucket of row.buckets) {
      successCount += bucket.success_count
      totalCount += bucket.total_count
    }
    const uptime = totalCount > 0 ? `${((successCount / totalCount) * 100).toFixed(1)}%` : '-'
    return {
      id: row.site_id,
      name: row.site_name || row.site_slug || row.site_id,
      siteType: row.site_type,
      uptime,
      buckets: row.buckets.map((bucket) => ({
        time: formatHourLabel(bucket.hour),
        status: normalizeUptimeStatus(bucket.status),
      })),
    }
  })
}

export function buildCooldownItems(items: DashboardCooldownAPIItem[], t?: TFunction): CooldownItem[] {
  return items.map((item) => {
    const scope = normalizeCooldownScope(item)
    const modelName = item.canonical_model || item.upstream_model_name
    const credentialName = item.credential_name || item.masked_key
    const target = scope === 'model' ? modelName : scope === 'credential' ? credentialName : undefined

    return {
      id: item.id,
      scope,
      name: item.site_name || item.site_id,
      target: target ?? undefined,
      reason: formatCooldownReason(item.reason, item.source, t),
      remaining: formatRemainingSeconds(item.remaining_seconds),
      severity: scope === 'site' ? 'error' : 'warning',
      siteId: item.site_id,
      siteModelId: item.site_model_id,
      siteCredentialId: item.site_credential_id,
      source: item.source,
    }
  })
}

export function buildAttentionRiskItems(overview: DashboardOverview, t?: TFunction): DashboardRiskItem[] {
  const attentionItems = overview.attention?.items ?? []
  if (attentionItems.length === 0) return []

  return attentionItems.slice(0, 10).map((item) =>
    riskItem({
      id: item.id,
      title: formatAttentionTitle(item, t),
      value: formatAttentionValue(item, t),
      detail: formatAttentionDetail(item, t),
      tone: attentionSeverityTone(item.severity),
      badgeLabel: attentionSeverityLabel(item.severity, t),
      actionPath: formatAttentionActionPath(item),
      actionLabel: formatAttentionActionLabel(item, t),
      icon: attentionIcon(item.type),
    }),
  )
}

export function selectedSiteCosts(overview: DashboardOverview, days: DashboardDays) {
  return selectedOverviewWindow(overview, days)?.site_cost_summary ?? overview.charts.site_cost_summary
}

function buildDailyTrend<T extends DailyPoint>(
  points: T[],
  overview: DashboardOverview,
  days: DashboardDays,
  getSeriesName: (point: T) => string,
  getValue: (point: T) => number,
): TrendResult {
  const dates = enumerateDateKeys(selectedRangeStart(overview, days), days)
  const dateSet = new Set(dates)
  const totals = new Map<string, number>()
  for (const point of points) {
    if (!dateSet.has(point.date)) continue
    const seriesName = getSeriesName(point) || 'unknown'
    totals.set(seriesName, (totals.get(seriesName) ?? 0) + getValue(point))
  }
  const seriesNames = [...totals.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([seriesName]) => seriesName)

  const keyByName = new Map(seriesNames.map((seriesName, index) => [seriesName, `m${index}`]))
  const series = seriesNames.map((seriesName, index) => ({ key: `m${index}`, name: seriesName }))
  const rows = dates.map((date) => {
    const row: DashboardTrendDatum = { date: formatDateLabel(date) }
    for (const seriesItem of series) row[seriesItem.key] = 0
    return row
  })
  const rowByDate = new Map(dates.map((date, index) => [date, rows[index]]))

  for (const point of points) {
    if (!dateSet.has(point.date)) continue
    const seriesKey = keyByName.get(getSeriesName(point) || 'unknown')
    const row = rowByDate.get(point.date)
    if (!seriesKey || !row) continue
    row[seriesKey] = Number(row[seriesKey] ?? 0) + getValue(point)
  }

  return { data: rows, series }
}

function selectedOverviewWindow(overview: DashboardOverview, days: DashboardDays) {
  return overview.windows?.[String(days)]
}

function selectedRangeStart(overview: DashboardOverview, days: DashboardDays) {
  return selectedOverviewWindow(overview, days)?.range_start ?? overview.meta.range_start
}

function contributionRangeStart(overview: DashboardOverview, days: number) {
  const today = overview.meta.today_start.slice(0, 10)
  const [year, month, day] = today.split('-').map(Number)
  const cursor = new Date(Date.UTC(year, month - 1, day))
  cursor.setUTCDate(cursor.getUTCDate() - days + 1)
  return cursor.toISOString().slice(0, 10)
}

function enumerateDateKeys(rangeStart: string, days: number) {
  const start = rangeStart.slice(0, 10)
  const [year, month, day] = start.split('-').map(Number)
  const cursor = new Date(Date.UTC(year, month - 1, day))
  return Array.from({ length: days }, (_, index) => {
    const date = new Date(cursor)
    date.setUTCDate(cursor.getUTCDate() + index)
    return date.toISOString().slice(0, 10)
  })
}

function contributionLevel(tokens: number, maxTokens: number): ApiKeyContributionDay['level'] {
  if (tokens <= 0 || maxTokens <= 0) return 0
  const ratio = tokens / maxTokens
  if (ratio <= 0.25) return 1
  if (ratio <= 0.5) return 2
  if (ratio <= 0.75) return 3
  return 4
}

function currentContributionStreak(days: ApiKeyContributionDay[]) {
  let streak = 0
  for (let index = days.length - 1; index >= 0; index--) {
    if (days[index].tokens <= 0) break
    streak++
  }
  return streak
}

function longestContributionStreak(days: ApiKeyContributionDay[]) {
  let current = 0
  let longest = 0
  for (const day of days) {
    if (day.tokens > 0) {
      current++
      longest = Math.max(longest, current)
    } else {
      current = 0
    }
  }
  return longest
}

function formatDateLabel(date: string) {
  return date.slice(5).replace('-', '/')
}

function formatHourLabel(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return `${String(date.getHours()).padStart(2, '0')}:00`
}

function normalizeUptimeStatus(status: string): UptimeStatus {
  if (status === 'healthy' || status === 'degraded' || status === 'idle') return status
  if (status === 'unhealthy' || status === 'down') return 'down'
  return 'idle'
}

function normalizeCooldownScope(item: DashboardCooldownAPIItem): CooldownItem['scope'] {
  if (item.scope === 'credential' || item.site_credential_id) return 'credential'
  if (item.scope === 'model' || item.site_model_id || item.canonical_model || item.upstream_model_name) return 'model'
  return 'site'
}

function formatRemainingSeconds(seconds: number) {
  if (seconds <= 0) return '0m'
  const minutes = Math.ceil(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60
  return rest > 0 ? `${hours}h ${rest}m` : `${hours}h`
}

function formatCooldownReason(reason: string, source: string, t?: TFunction) {
  const reasonMap: Record<string, string> = {
    site_unhealthy: t ? t('cooldownReasons.site_unhealthy') : '站点健康检查异常',
    usage_limit_reached: t ? t('cooldownReasons.usage_limit_reached') : '账号额度达到限制',
    upstream_http_error: t ? t('cooldownReasons.upstream_http_error') : '上游请求异常',
    upstream_timeout: t ? t('cooldownReasons.upstream_timeout') : '上游请求超时',
  }
  const sourceMap: Record<string, string> = {
    site_health: t ? t('cooldownSources.site_health') : '健康检查',
    gateway: t ? t('cooldownSources.gateway') : '网关请求',
    manual: t ? t('cooldownSources.manual') : '手动设置',
  }
  const label = reasonMap[reason] ?? reason
  const sourceLabel = sourceMap[source] ?? source
  return `${label} / ${sourceLabel}`
}

function formatLatency(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(1)}s`
  return `${Math.round(value)}ms`
}

function attentionSeverityTone(severity: string): DashboardRiskItem['tone'] {
  if (severity === 'critical') return 'error'
  if (severity === 'warning') return 'warning'
  return 'neutral'
}

function attentionSeverityLabel(severity: string, t?: TFunction) {
  if (severity === 'critical') return t ? t('severityLabels.critical') : '严重'
  if (severity === 'warning') return t ? t('severityLabels.warning') : '预警'
  if (severity === 'info') return t ? t('severityLabels.info') : '提醒'
  return t ? t('severityLabels.default') : '关注'
}

function attentionIcon(type: string): ReactNode {
  if (type === 'missing_model_pricing') return createElement(BadgeDollarSign, { className: 'h-4 w-4' })
  if (type === 'rate_limit_exceeded') return createElement(Gauge, { className: 'h-4 w-4' })
  if (type === 'active_cooldown' || type === 'high_latency') return createElement(Clock3, { className: 'h-4 w-4' })
  if (type === 'no_route_candidates' || type === 'low_route_candidates') return createElement(Route, { className: 'h-4 w-4' })
  return createElement(ShieldAlert, { className: 'h-4 w-4' })
}

function formatAttentionType(type: string, t?: TFunction) {
  const map: Record<string, string> = {
    no_route_candidates: t ? t('attentionTypes.no_route_candidates') : '没有可用上游候选',
    low_route_candidates: t ? t('attentionTypes.low_route_candidates') : '可用上游候选偏少',
    active_cooldown: t ? t('attentionTypes.active_cooldown') : '当前存在冷却中的路由',
    site_unhealthy: t ? t('attentionTypes.site_unhealthy') : '站点健康检查失败',
    missing_model_pricing: t ? t('attentionTypes.missing_model_pricing') : '模型价格缺失',
    high_failure_rate: t ? t('attentionTypes.high_failure_rate') : '站点请求失败率偏高',
    rate_limit_exceeded: t ? t('attentionTypes.rate_limit_exceeded') : '请求触发限流',
    high_latency: t ? t('attentionTypes.high_latency') : '请求延迟偏高',
  }
  return map[type] ?? type
}

function formatAttentionTitle(item: DashboardAttentionItem, t?: TFunction) {
  const subject = attentionSubject(item)
  const metrics = attentionMetrics(item)
  const model = readString(subject, 'model_key') ?? '-'
  const site = readString(subject, 'site_name') ?? (t ? t('unknownSite') : '未知站点')
  const scope = readString(subject, 'scope')
  const missingCount = readNumber(metrics, 'missing_count')

  switch (item.type) {
    case 'no_route_candidates':
      return t ? t('attention.title.no_route_candidates', { model }) : `${model} has no available upstream`
    case 'low_route_candidates':
      return t ? t('attention.title.low_route_candidates', { model }) : `${model} has few available upstreams`
    case 'active_cooldown':
      if (scope === 'model' && model !== '-') return t ? t('attention.title.active_cooldown_model', { model, site }) : `${model} is cooling down on ${site}`
      if (scope === 'credential') return t ? t('attention.title.active_cooldown_credential', { site }) : `${site} API key is cooling down`
      return t ? t('attention.title.active_cooldown_site', { site }) : `${site} is cooling down`
    case 'site_unhealthy':
      return t ? t('attention.title.site_unhealthy', { site }) : `${site} health check failed`
    case 'missing_model_pricing':
      return t ? t('attention.title.missing_model_pricing', { count: missingCount ?? 0 }) : `${missingCount ?? 0} site models are missing pricing`
    case 'high_failure_rate':
      return t ? t('attention.title.high_failure_rate', { site }) : `${site} has a high failure rate`
    case 'rate_limit_exceeded':
      return t ? t('attention.title.rate_limit_exceeded') : 'Requests were rate limited'
    case 'high_latency':
      return readString(subject, 'site_name')
        ? (t ? t('attention.title.high_latency_site_model', { site, model }) : `${site} / ${model} has high request latency`)
        : (t ? t('attention.title.high_latency_model', { model }) : `${model} has high request latency`)
    default:
      return item.title || formatAttentionType(item.type, t)
  }
}

function formatAttentionDetail(item: DashboardAttentionItem, t?: TFunction) {
  const metrics = attentionMetrics(item)
  const eligible = readNumber(metrics, 'eligible_count')
  const total = readNumber(metrics, 'site_model_count')
  const requestCount24h = readNumber(metrics, 'request_count_24h')
  const successRate = readNumber(metrics, 'success_rate')
  const p95Latency = readNumber(metrics, 'p95_latency_ms')

  switch (item.type) {
    case 'no_route_candidates':
      if (requestCount24h && requestCount24h > 0) return t ? t('attention.detail.no_route_candidates_active') : 'This model had downstream traffic in the last 24 hours, but currently has no available upstream.'
      return t ? t('attention.detail.no_route_candidates_idle') : 'This model is configured, but currently has no available upstream.'
    case 'low_route_candidates':
      return t ? t('attention.detail.low_route_candidates', { eligible: eligible ?? 0, total: total ?? '-' }) : `${eligible ?? 0} of ${total ?? '-'} upstream models are available.`
    case 'active_cooldown':
      return readString(attentionSubject(item), 'scope') === 'credential'
        ? (t ? t('attention.detail.active_cooldown_credential') : 'This upstream credential is temporarily excluded from routing.')
        : (t ? t('attention.detail.active_cooldown') : 'This route target is temporarily excluded from routing.')
    case 'site_unhealthy':
      return t ? t('attention.detail.site_unhealthy') : 'This site is unavailable and may affect all model routes under it.'
    case 'missing_model_pricing':
      return t ? t('attention.detail.missing_model_pricing') : 'Missing prices can make cost statistics and route policies inaccurate.'
    case 'high_failure_rate':
      return t ? t('attention.detail.high_failure_rate', { rate: successRate != null ? formatPercent(successRate) : '-' }) : `The recent success rate is ${successRate != null ? formatPercent(successRate) : '-'}.`
    case 'rate_limit_exceeded':
      return t ? t('attention.detail.rate_limit_exceeded') : 'Downstream callers received 429 responses. Check global or API key RPM/TPM limits.'
    case 'high_latency':
      return t ? t('attention.detail.high_latency', { latency: p95Latency != null ? formatLatency(p95Latency) : '-' }) : `P95 latency is ${p95Latency != null ? formatLatency(p95Latency) : '-'}.`
    default:
      return item.description || formatAttentionType(item.type, t)
  }
}

function formatAttentionValue(item: DashboardAttentionItem, t?: TFunction) {
  const metadata = attentionMetrics(item)

  if (item.type === 'no_route_candidates' || item.type === 'low_route_candidates') {
    const eligibleCount = readNumber(metadata, 'eligible_count')
    const siteModelCount = readNumber(metadata, 'site_model_count')
    if (eligibleCount != null && siteModelCount != null) return `${eligibleCount}/${siteModelCount}`
  }

  if (item.type === 'active_cooldown') {
    const remainingSeconds = readNumber(metadata, 'remaining_seconds')
    if (remainingSeconds != null) return formatRemainingSeconds(remainingSeconds)
  }

  if (item.type === 'site_unhealthy') {
    const failures = readNumber(metadata, 'consecutive_failures')
    if (failures != null) return t ? t('failureCount', { count: failures }) : `${failures} times`
  }

  if (item.type === 'missing_model_pricing') {
    const missingCount = readNumber(metadata, 'missing_count')
    if (missingCount != null) return String(missingCount)
  }

  if (item.type === 'high_failure_rate') {
    const requestCount = readNumber(metadata, 'request_count')
    if (requestCount != null) return formatCompactNumber(requestCount)
  }

  if (item.type === 'rate_limit_exceeded') {
    const requestCount = readNumber(metadata, 'request_count')
    if (requestCount != null) return formatCompactNumber(requestCount)
  }

  if (item.type === 'high_latency') {
    const p95Latency = readNumber(metadata, 'p95_latency_ms')
    if (p95Latency != null) return formatLatency(p95Latency)
  }

  return item.primary_action?.label
}

function formatAttentionActionLabel(item: DashboardAttentionItem, t?: TFunction) {
  const actionType = item.action?.type
  if (!actionType) return item.primary_action?.label
  const key = `attention.actions.${item.type}`
  const translated = t ? t(key) : key
  if (translated !== key) return translated

  const fallbackKey = `attention.actions.${actionType}`
  const fallbackTranslated = t ? t(fallbackKey) : fallbackKey
  if (fallbackTranslated !== fallbackKey) return fallbackTranslated
  return item.primary_action?.label
}

function formatAttentionActionPath(item: DashboardAttentionItem) {
  const action = item.action
  if (!action?.type) return item.primary_action?.path
  const params = action.params ?? {}
  const query = new URLSearchParams(params)
  const suffix = query.size > 0 ? `?${query.toString()}` : ''

  switch (action.type) {
    case 'open_routes':
      return `/routes${suffix}`
    case 'open_sites':
      return `/sites${suffix}`
    case 'open_site_api_keys': {
      const siteQuery = new URLSearchParams(params)
      siteQuery.set('panel', 'api_keys')
      return `/sites?${siteQuery.toString()}`
    }
    case 'open_model_prices':
      return `/settings/models-price${suffix}`
    case 'open_requests':
      return `/requests${suffix}`
    default:
      return item.primary_action?.path
  }
}

function attentionSubject(item: DashboardAttentionItem) {
  return item.subject ?? item.related ?? {}
}

function attentionMetrics(item: DashboardAttentionItem) {
  return item.metrics ?? item.metadata ?? {}
}

function readNumber(source: Record<string, unknown>, key: string) {
  const value = source[key]
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return null
}

function readString(source: Record<string, unknown>, key: string) {
  const value = source[key]
  if (typeof value === 'string' && value.trim() !== '') return value.trim()
  return null
}

function riskItem(input: {
  id: string
  title: string
  value?: string
  detail: string
  tone: DashboardRiskItem['tone']
  badgeLabel?: string
  actionPath?: string
  actionLabel?: string
  icon: ReactNode
}): DashboardRiskItem {
  return input
}
