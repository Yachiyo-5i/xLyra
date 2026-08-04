import type { RouteCandidateItem, RouteCooldown } from '@/features/routes/api/routes'
import type { Site, SiteModel } from '@/features/sites/api/sites'

type TFunction = (key: string, options?: Record<string, unknown>) => string

export function isNewAPISite(siteType: string) {
  return siteType === 'newapi' || siteType === 'new_api'
}

export function buildSiteGlyph(name: string) {
  const trimmed = name.trim()
  if (!trimmed) return '?'
  return trimmed.slice(0, 2).toUpperCase()
}

export function formatCooldownScope(scope: string, t?: TFunction) {
  if (scope === 'site') return t ? t('format.scopeSite') : '站点'
  if (scope === 'model') return t ? t('format.scopeModel') : '模型'
  if (scope === 'credential') return t ? t('format.scopeCredential') : '密钥'
  return scope || '-'
}

export function routeCooldownSiteName(item: RouteCooldown, sites: Site[], t?: TFunction) {
  return sites.find((site) => site.id === item.site_id)?.name ?? (t ? t('format.unknownSite') : '未知站点')
}

export function routeCooldownTargetLabel(item: RouteCooldown, modelsMap: Record<string, SiteModel[]>, t?: TFunction) {
  if (item.site_model_id) {
    const siteModels = modelsMap[item.site_id] ?? []
    const model = siteModels.find((entry) => entry.id === item.site_model_id)
    const name = model?.display_name || model?.upstream_model_name
    if (name) return t ? t('format.modelPrefix', { name }) : `模型 ${name}`
    return t ? t('format.specifiedModel') : '指定模型'
  }

  if (item.site_credential_id) {
    return t ? t('format.upstreamCredential') : '上游凭证'
  }

  return t ? t('format.entireSite') : '整个站点'
}

export function routeCooldownSourceLabel(item: RouteCooldown, t?: TFunction) {
  switch (item.source) {
    case 'site_health':
      return t ? t('format.sourceHealthCheck') : '健康检查'
    case 'gateway':
      return t ? t('format.sourceForwardFailed') : '转发失败'
    case 'manual':
      return t ? t('format.sourceManual') : '手动冷却'
    default:
      return item.source || (t ? t('format.sourceUnknown') : '未知来源')
  }
}

export function routeCooldownReasonLabel(item: RouteCooldown, t?: TFunction) {
  const reason = item.reason?.trim()
  const metadata = getRecord(item.metadata)
  const status = getString(metadata?.status)

  if (item.source === 'site_health') {
    if (status === 'unhealthy' || reason === 'site health is unhealthy') {
      return t ? t('format.reasonHealthUnhealthy') : '站点健康检查结果异常'
    }
    return reason || (t ? t('format.reasonHealthDefault') : '站点健康检查触发冷却')
  }

  switch (reason) {
    case 'upstream timeout':
      return t ? t('format.reasonTimeout') : '上游请求超时'
    case 'transport error':
      return t ? t('format.reasonTransport') : '上游连接失败'
    case 'upstream 5xx':
      return t ? t('format.reason5xx') : '上游服务异常'
    case 'upstream 404':
      return t ? t('format.reason404') : '上游模型不存在'
    case 'upstream 429':
      return t ? t('format.reason429') : '上游触发限流'
    case 'upstream 401':
      return t ? t('format.reason401') : '上游认证失效'
    case 'upstream 403':
      return t ? t('format.reason403') : '上游拒绝访问'
    default:
      return reason || (t ? t('format.reasonDefault') : '触发冷却')
  }
}

function getRecord(value: unknown) {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : undefined
}

function getString(value: unknown) {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

export function formatModelHealth(candidate?: RouteCandidateItem, t?: TFunction) {
  const requestCount = candidate?.health.model_request_count ?? 0
  if (!candidate || requestCount <= 0) {
    return t ? t('format.healthNoRequest') : '模型健康 未请求'
  }

  const requestsLabel = t ? t('format.healthRequests', { count: requestCount }) : `模型请求 ${requestCount}`
  const successRateLabel = t ? t('format.healthSuccessRate', { rate: formatPercent(candidate.health.model_success_rate) }) : `成功率 ${formatPercent(candidate.health.model_success_rate)}`
  const latencyLabel = t ? t('format.healthLatency', { latency: formatLatency(candidate.health.model_avg_latency_ms) }) : `延迟 ${formatLatency(candidate.health.model_avg_latency_ms)}`

  return [requestsLabel, successRateLabel, latencyLabel].join(' · ')
}

export function formatLatency(value?: number | null) {
  if (typeof value !== 'number') return '-'
  return `${Math.round(value)} ms`
}

export function formatPercent(value?: number | null, digits = 4) {
  if (typeof value !== 'number') return '-'
  return `${formatDecimal(value * 100, digits, true)}%`
}

function formatPrice(value?: number | null, currency?: string | null) {
  if (typeof value !== 'number') return '-'
  const prefix = currency && currency.toUpperCase() !== 'USD' ? `${currency} ` : '$'
  return `${prefix}${formatDecimal(value)}/1M`
}

export function formatRoutePricing(pricing?: { base_input_value?: number | null; base_output_value?: number | null; base_per_request_value?: number | null; input_value?: number | null; output_value?: number | null; per_request_value?: number | null; currency?: string | null }, t?: TFunction) {
  const effective = formatEffectiveRoutePricing(pricing, t)
  const hasBase = pricing && (
    typeof pricing.base_input_value === 'number'
    || typeof pricing.base_output_value === 'number'
    || typeof pricing.base_per_request_value === 'number'
  )
  const hasDifferentBase = hasBase && (
    pricing.base_input_value !== pricing.input_value
    || pricing.base_output_value !== pricing.output_value
    || pricing.base_per_request_value !== pricing.per_request_value
  )
  if (!hasDifferentBase) return effective
  const base = formatEffectiveRoutePricing({
    input_value: pricing.base_input_value,
    output_value: pricing.base_output_value,
    per_request_value: pricing.base_per_request_value,
    currency: pricing.currency,
  }, t)
  return `${base} → ${effective}`
}

function formatEffectiveRoutePricing(pricing?: { input_value?: number | null; output_value?: number | null; per_request_value?: number | null; currency?: string | null }, t?: TFunction) {
  if (typeof pricing?.per_request_value === 'number') {
    const price = formatPerRequestRoutePrice(pricing.per_request_value, pricing.currency, t)
    return t ? t('format.pricingPerRequest', { price }) : `按次 ${price}`
  }
  const input = formatPrice(pricing?.input_value, pricing?.currency)
  const output = formatPrice(pricing?.output_value, pricing?.currency)
  return t ? t('format.pricingTokens', { input, output }) : `输入 ${input} / 输出 ${output}`
}

function formatPerRequestRoutePrice(value?: number | null, currency?: string | null, t?: TFunction) {
  if (typeof value !== 'number') return '-'
  const prefix = currency && currency.toUpperCase() !== 'USD' ? `${currency} ` : '$'
  const price = `${prefix}${formatDecimal(value)}`
  return t ? t('format.perRequestUnit', { price }) : `${price}/req`
}

export function formatDateTime(value?: string | null) {
  if (!value) return '-'

  try {
    return new Intl.DateTimeFormat('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }).format(new Date(value))
  } catch {
    return value
  }
}

export function formatDuration(seconds?: number | null) {
  if (typeof seconds !== 'number') return '-'
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.ceil(seconds / 60)}m`
  return `${Math.ceil(seconds / 3600)}h`
}

function formatDecimal(value: number, digits = 4, fixed = false) {
  return new Intl.NumberFormat('zh-CN', {
    minimumFractionDigits: fixed ? digits : 0,
    maximumFractionDigits: digits,
  }).format(value)
}
