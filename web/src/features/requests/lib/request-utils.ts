import { localeFromLanguage } from '@/lib/locale'
import type { RequestLogCredential, RequestLogDetail, RequestLogFailoverChannel, RequestLogFailoverCredentialAttempt, RequestLogItem } from '@/features/requests/api/requests'

type TFunction = (key: string, options?: Record<string, unknown>) => string

export type RequestLatencyTone = 'healthy' | 'slow' | 'very-slow' | 'critical' | 'muted'

const firstByteLatencyThresholds = [5_000, 10_000, 20_000] as const
const totalLatencyThresholds = [20_000, 40_000, 60_000] as const

export function requestModelName(item: RequestLogItem) {
  return requestDownstreamModelName(item)
}

function requestDownstreamModelName(item: RequestLogItem) {
  return item.original_model || item.requested_model || item.model.canonical_model || item.mapped_model || item.model.upstream_model || '-'
}

function requestActualModelName(item: RequestLogItem) {
  return item.model.upstream_model || item.mapped_model || item.model.canonical_model || '-'
}

export function requestMappedModel(item: RequestLogItem): string | null {
  const downstream = requestDownstreamModelName(item)
  const actual = requestActualModelName(item)
  if (!actual || actual === '-' || actual === downstream) return null
  return actual
}

export function requestGroupName(item: RequestLogItem) {
  if (item.site.site_type !== 'newapi') return null
  return item.pricing?.group_name || item.pricing_group || null
}

export function requestReasoningEffort(detail: RequestLogDetail | RequestLogItem) {
  const value = detail.reasoning_effort
  return typeof value === 'string' && value.trim() ? value.trim() : null
}

export function requestFirstByteLatency(item: RequestLogItem) {
  if (item.stream !== true && item.response_mode !== 'stream') return null
  if (!isPositiveFiniteNumber(item.first_byte_latency_ms)) return null
  return item.first_byte_latency_ms
}

export function requestCredentialName(detail: RequestLogDetail | RequestLogItem) {
  const value = detail.credential?.name
  return typeof value === 'string' && value.trim() ? value.trim() : null
}

export function requestCredentialMultiplier(detail: RequestLogDetail | RequestLogItem) {
  const value = detail.cost_calculation?.credential_upstream_cost_multiplier
  return isPositiveFiniteNumber(value) ? value : null
}

export function requestServiceTierMultiplier(detail: RequestLogDetail | RequestLogItem) {
  const value = detail.cost_calculation?.service_tier_multiplier
  return isPositiveFiniteNumber(value) ? value : null
}

export function requestAPIKeyBillingMultiplier(detail: RequestLogDetail | RequestLogItem) {
  const value = detail.cost_calculation?.api_key_billing_multiplier
  return isPositiveFiniteNumber(value) && value !== 1 ? value : null
}

export function formatCostMultiplier(value?: number | null) {
  if (!isPositiveFiniteNumber(value)) return null
  return `x${formatDecimal(value)}`
}

function requestCostMultiplierSuffix(detail: RequestLogDetail | RequestLogItem, t: TFunction) {
  const credentialMultiplier = requestCredentialMultiplier(detail)
  const serviceTierMultiplier = requestIsFastBilling(detail) ? requestServiceTierMultiplier(detail) : null
  const apiKeyMultiplier = requestAPIKeyBillingMultiplier(detail)
  const parts: string[] = []
  const credentialSuffix = formatCostMultiplier(credentialMultiplier)
  const serviceTierSuffix = serviceTierMultiplier != null && serviceTierMultiplier > 1
    ? formatCostMultiplier(serviceTierMultiplier)
    : null
  const apiKeySuffix = formatCostMultiplier(apiKeyMultiplier)
  if (credentialSuffix) {
    parts.push(t('detail.formula.credentialMultiplier', { multiplier: credentialSuffix }))
  }
  if (serviceTierSuffix) {
    parts.push(t('detail.formula.serviceTierMultiplier', { multiplier: serviceTierSuffix }))
  }
  if (apiKeySuffix) {
    parts.push(t('detail.formula.apiKeyMultiplier', { multiplier: apiKeySuffix }))
  }
  if (parts.length) {
    return parts.join(' * ')
  }
  const multiplier = detail.cost_calculation?.cost_multiplier
  return isPositiveFiniteNumber(multiplier) && multiplier > 1 ? formatDecimal(multiplier) : null
}

export function requestResponseModeLabel(item: RequestLogItem, t: TFunction) {
  if (item.response_mode === 'stream' || item.stream === true) {
    return t('table.responseMode.stream')
  }
  if (item.response_mode === 'non_stream' || item.stream === false) {
    return t('table.responseMode.nonStream')
  }
  return t('table.responseMode.nonStream')
}

export function requestResponseModeVariant(item: RequestLogItem): 'accent' | 'secondary' {
  if (item.response_mode === 'stream' || item.stream === true) {
    return 'accent'
  }
  return 'secondary'
}

export function requestHasFailover(item: Pick<RequestLogItem, 'failover'>): boolean {
  return item.failover === true
}

export function requestFailoverRouteAttempt(item: Pick<RequestLogItem, 'attempt' | 'failover'>): number | null {
  if (!requestHasFailover(item) || typeof item.attempt !== 'number' || item.attempt <= 1) return null
  return item.attempt
}

export function requestFailoverCredentialAttempt(item: Pick<RequestLogItem, 'credential_attempt' | 'credential_total' | 'failover'>): { attempt: number; total: number | null } | null {
  if (!requestHasFailover(item) || typeof item.credential_attempt !== 'number' || item.credential_attempt <= 1) return null
  return {
    attempt: item.credential_attempt,
    total: typeof item.credential_total === 'number' && item.credential_total > 0 ? item.credential_total : null,
  }
}

export type RequestFailoverTrace = {
  defaultChannel: RequestFailoverChannel
  intermediateChannels: RequestFailoverChannel[]
  finalChannel: RequestFailoverChannel | null
  credentialAttempts: RequestFailoverCredentialAttempt[]
}

export type RequestFailoverChannel = {
  success: boolean
  siteName: string
  siteType: string | null
  errorType: string | null
  statusCode: number | null
}

export type RequestFailoverCredentialAttempt = RequestFailoverChannel & {
  credentialId: string | null
  credentialName: string | null
  attempt: number | null
  total: number | null
}

export function requestFailoverTrace(item: Pick<RequestLogItem, 'failover_trace'>): RequestFailoverTrace | null {
  const source = item.failover_trace
  if (!source) return null
  const defaultChannel = requestFailoverChannel(source.default_channel)
  if (!defaultChannel) return null
  return {
    defaultChannel,
    intermediateChannels: (source.intermediate_channels ?? [])
      .map(requestFailoverChannel)
      .filter((channel): channel is RequestFailoverChannel => channel != null),
    finalChannel: requestFailoverChannel(source.final_channel),
    credentialAttempts: (source.credential_attempts ?? [])
      .map(requestFailoverCredentialAttemptDetail)
      .filter((attempt): attempt is RequestFailoverCredentialAttempt => attempt != null),
  }
}

export function requestFailoverFailureReason(channel: RequestFailoverChannel, t: TFunction): string {
  const statusCode = channel.statusCode
  if (statusCode === 401) return t('detail.failoverFailureAuth')
  if (statusCode === 403) return t('detail.failoverFailurePermission')
  if (statusCode === 404) return t('detail.failoverFailureModel')
  if (statusCode === 429) return t('detail.failoverFailureRateLimited')

  const errorType = (channel.errorType ?? '').toLowerCase()
  if (errorType.includes('timeout')) return t('detail.failoverFailureTimeout')
  if (errorType.includes('limited') || errorType.includes('rate_limit')) return t('detail.failoverFailureRateLimited')
  if (errorType.includes('credential') || errorType.includes('oauth')) return t('detail.failoverFailureCredential')
  if (errorType.includes('overload') || errorType.includes('capacity')) return t('detail.failoverFailureOverloaded')
  if (errorType.includes('transport') || errorType.includes('connection')) return t('detail.failoverFailureConnection')
  if (errorType.includes('stream')) return t('detail.failoverFailureStream')
  if (statusCode != null && statusCode >= 500) return t('detail.failoverFailureUpstream')
  return t('detail.failoverFailureUnknown')
}

function requestFailoverChannel(value?: RequestLogFailoverChannel | null): RequestFailoverChannel | null {
  if (!value) return null
  const siteName = nonEmptyString(value.site?.name)
  if (!siteName) return null
  return {
    success: value.success === true,
    siteName,
    siteType: nonEmptyString(value.site.site_type),
    errorType: nonEmptyString(value.error_type),
    statusCode: requestFailoverStatusCode(value),
  }
}

function requestFailoverCredentialAttemptDetail(value?: RequestLogFailoverCredentialAttempt | null): RequestFailoverCredentialAttempt | null {
  const channel = requestFailoverChannel(value)
  if (!channel) return null
  return {
    ...channel,
    credentialId: nonEmptyString(value?.credential?.id),
    credentialName: requestCredentialNameValue(value?.credential),
    attempt: positiveInteger(value?.credential_attempt),
    total: positiveInteger(value?.credential_total),
  }
}

function requestCredentialNameValue(credential?: RequestLogCredential | null) {
  return nonEmptyString(credential?.name)
}

function requestFailoverStatusCode(channel: RequestLogFailoverChannel): number | null {
  const upstream = positiveInteger(channel.upstream_status_code)
  if (upstream != null && upstream >= 100 && upstream <= 599) return upstream
  const status = positiveInteger(channel.status_code)
  return status != null && status >= 100 && status <= 599 ? status : null
}

export function requestDownstreamTransportLabel(detail: RequestLogDetail | RequestLogItem) {
  return metadataString(detail, ['downstream_transport']) === 'websocket' ? 'WS' : 'HTTP'
}

export function requestCacheRatio(detail: RequestLogDetail | RequestLogItem) {
  return firstNumber(
    detail.pricing?.cache_ratio,
    metadataNumber(detail, ['pricing', 'cache_ratio']),
  )
}

export function requestCachePrice(detail: RequestLogDetail | RequestLogItem) {
  return firstNumber(
    detail.pricing?.cache_input_value,
    detail.cost_calculation?.cache_input_value,
    metadataNumber(detail, ['pricing', 'cache_input_value']),
    metadataNumber(detail, ['cost_calculation', 'cache_input_value']),
  )
}

export function requestCacheTokens(detail: RequestLogDetail | RequestLogItem) {
  return firstNumber(
    detail.cost_calculation?.cache_tokens,
    detail.cost_calculation?.cached_tokens,
    metadataNumber(detail, ['cost_calculation', 'cache_tokens']),
    metadataNumber(detail, ['cost_calculation', 'cached_tokens']),
    metadataNumber(detail, ['usage', 'cache_tokens']),
    metadataNumber(detail, ['usage', 'cached_tokens']),
  )
}

export function requestCacheWriteTokens(detail: RequestLogDetail | RequestLogItem) {
  // OpenAI reports cache_write_tokens; Anthropic reports cache_creation_tokens.
  // They are mutually exclusive per request, so prefer whichever is positive.
  return firstPositiveNumber(
    detail.cost_calculation?.cache_write_tokens,
    metadataNumber(detail, ['cost_calculation', 'cache_write_tokens']),
    detail.cost_calculation?.cache_creation_tokens,
    metadataNumber(detail, ['cost_calculation', 'cache_creation_tokens']),
  )
}

export function requestCacheWritePrice(detail: RequestLogDetail | RequestLogItem) {
  return firstNumber(
    detail.cost_calculation?.cache_write_input_value,
    metadataNumber(detail, ['cost_calculation', 'cache_write_input_value']),
  )
}

export function requestGroupRatio(detail: RequestLogDetail | RequestLogItem) {
  return firstNumber(
    detail.cost_calculation?.group_ratio,
    detail.pricing?.group_ratio,
    metadataNumber(detail, ['cost_calculation', 'group_ratio']),
  )
}

export function requestIsFastBilling(detail: RequestLogDetail | RequestLogItem) {
  const mode = firstString(
    detail.cost_calculation?.billing_mode,
    detail.cost_calculation?.service_tier,
    metadataString(detail, ['billing_mode']),
    metadataString(detail, ['service_tier']),
    metadataString(detail, ['cost_calculation', 'billing_mode']),
    metadataString(detail, ['cost_calculation', 'service_tier']),
  )
  return mode === 'fast' || mode === 'priority'
}

export function requestHasBillingDetails(detail: RequestLogDetail | RequestLogItem) {
  if (detail.success) return true
  if (detail.cost_calculation?.long_context === true) return true
  return isNonNegativeFiniteNumber(detail.cost_calculation?.estimated_cost) || isNonNegativeFiniteNumber(detail.usage.estimated_cost)
}

export function requestCostFormula(detail: RequestLogDetail | RequestLogItem, t: TFunction) {
  const promptTokens = detail.cost_calculation?.prompt_tokens ?? detail.usage.prompt_tokens
  const completionTokens = detail.cost_calculation?.completion_tokens ?? detail.usage.completion_tokens
  const cacheTokens = requestCacheTokens(detail)
  const cacheWriteTokens = requestCacheWriteTokens(detail)
  const cacheWrite5mTokens = detail.cost_calculation?.cache_creation_5m_tokens
  const cacheWrite1hTokens = detail.cost_calculation?.cache_creation_1h_tokens
  const inputPrice = detail.pricing?.input_value
  const outputPrice = detail.pricing?.output_value
  const perRequestValue = detail.cost_calculation?.per_request_value ?? detail.pricing?.per_request_value
  const perRequestCost = detail.cost_calculation?.per_request_cost
  const cachePrice = requestCachePrice(detail)
  const cacheWritePrice = requestCacheWritePrice(detail)
  const cacheWrite1hPrice = firstNumber(
    detail.cost_calculation?.cache_write_1h_input_value,
    metadataNumber(detail, ['cost_calculation', 'cache_write_1h_input_value']),
  )
  const groupRatio = requestGroupRatio(detail)
  const totalCost = detail.cost_calculation?.estimated_cost ?? detail.usage.estimated_cost
  const baseCost = detail.cost_calculation?.base_estimated_cost
  const currency = detail.cost_calculation?.currency ?? detail.usage.currency
  const parts: string[] = []
  const hasGroupRatio = typeof groupRatio === 'number' && groupRatio !== 1
  const multiplierSuffix = requestCostMultiplierSuffix(detail, t)

  if (typeof perRequestValue === 'number') {
    const summary = t('detail.formula.perRequest', { price: formatCurrency(perRequestValue, currency) })
    if (typeof totalCost === 'number' && typeof baseCost === 'number' && multiplierSuffix) {
      return `${summary} = ${formatCurrency(baseCost, currency)} * ${multiplierSuffix} = ${formatCurrency(totalCost, currency)}`
    }
    if (typeof totalCost === 'number' && typeof perRequestCost === 'number') {
      return `${summary} = ${formatCurrency(totalCost, currency)}`
    }
    return summary
  }

  if (typeof promptTokens === 'number' && typeof inputPrice === 'number') {
    parts.push(t('detail.formula.input', { tokens: formatInteger(promptTokens), price: formatCurrency(inputPrice, currency) }))
  }
  if (typeof cacheTokens === 'number' && typeof cachePrice === 'number') {
    parts.push(t('detail.formula.cache', { tokens: formatInteger(cacheTokens), price: formatCurrency(cachePrice, currency) }))
  }
  if (typeof cacheWrite5mTokens === 'number' && cacheWrite5mTokens > 0 && typeof cacheWritePrice === 'number') {
    parts.push(t('detail.formula.cacheWrite5m', { tokens: formatInteger(cacheWrite5mTokens), price: formatCurrency(cacheWritePrice, currency) }))
  }
  if (typeof cacheWrite1hTokens === 'number' && cacheWrite1hTokens > 0 && typeof cacheWrite1hPrice === 'number') {
    parts.push(t('detail.formula.cacheWrite1h', { tokens: formatInteger(cacheWrite1hTokens), price: formatCurrency(cacheWrite1hPrice, currency) }))
  }
  const hasCacheWriteBreakdown = (typeof cacheWrite5mTokens === 'number' && cacheWrite5mTokens > 0) ||
    (typeof cacheWrite1hTokens === 'number' && cacheWrite1hTokens > 0)
  if (!hasCacheWriteBreakdown && typeof cacheWriteTokens === 'number' && cacheWriteTokens > 0 && typeof cacheWritePrice === 'number') {
    parts.push(t('detail.formula.cacheWrite', { tokens: formatInteger(cacheWriteTokens), price: formatCurrency(cacheWritePrice, currency) }))
  }
  if (typeof completionTokens === 'number' && typeof outputPrice === 'number') {
    parts.push(t('detail.formula.output', { tokens: formatInteger(completionTokens), price: formatCurrency(outputPrice, currency) }))
  }

  if (parts.length) {
    let summary = parts.join(' + ')
    if (hasGroupRatio) {
      summary = `(${summary}) * ${formatDecimal(groupRatio)}`
    }
    if (typeof totalCost === 'number' && typeof baseCost === 'number' && multiplierSuffix) {
      return `${summary} = ${formatCurrency(baseCost, currency)} * ${multiplierSuffix} = ${formatCurrency(totalCost, currency)}`
    }
    if (typeof totalCost === 'number') {
      return `${summary} = ${formatCurrency(totalCost, currency)}`
    }
    return summary
  }

  return detail.cost_calculation?.formula || '-'
}

function metadataNumber(detail: RequestLogDetail | RequestLogItem, path: string[]) {
  const value = metadataValue(detail, path)
  return typeof value === 'number' ? value : null
}

function metadataString(detail: RequestLogDetail | RequestLogItem, path: string[]) {
  const value = metadataValue(detail, path)
  return typeof value === 'string' ? value.trim().toLowerCase() : null
}

function metadataValue(detail: RequestLogDetail | RequestLogItem, path: string[]) {
  if (!('metadata' in detail) || !detail.metadata) {
    return null
  }

  let current: unknown = detail.metadata
  for (const key of path) {
    if (!current || typeof current !== 'object' || !(key in current)) {
      return null
    }
    current = (current as Record<string, unknown>)[key]
  }

  return current
}

function firstNumber(...values: Array<number | null | undefined>) {
  for (const value of values) {
    if (typeof value === 'number') {
      return value
    }
  }
  return null
}

function isPositiveFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function positiveInteger(value: unknown): number | null {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 ? value : null
}

function nonEmptyString(value: unknown): string | null {
  return typeof value === 'string' && value.trim() ? value.trim() : null
}

function isNonNegativeFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0
}

function firstPositiveNumber(...values: Array<number | null | undefined>) {
  for (const value of values) {
    if (typeof value === 'number' && value > 0) {
      return value
    }
  }
  return null
}

function firstString(...values: Array<string | null | undefined>) {
  for (const value of values) {
    const normalized = value?.trim().toLowerCase()
    if (normalized) {
      return normalized
    }
  }
  return null
}

export function formatDateTime(value?: string | null, language?: string) {
  if (!value) return '-'

  try {
    return new Intl.DateTimeFormat(localeFromLanguage(language), {
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

export function formatLatency(value?: number | null) {
  if (!isNonNegativeFiniteNumber(value)) return '-'
  return `${Math.round(value)} ms`
}

export function requestFirstByteLatencyTone(value?: number | null): RequestLatencyTone {
  return latencyToneForThresholds(value, firstByteLatencyThresholds)
}

export function requestTotalLatencyTone(value?: number | null): RequestLatencyTone {
  return latencyToneForThresholds(value, totalLatencyThresholds)
}

function latencyToneForThresholds(
  value: number | null | undefined,
  [healthyMax, slowMax, verySlowMax]: readonly [number, number, number],
): RequestLatencyTone {
  if (!isNonNegativeFiniteNumber(value)) return 'muted'
  if (value <= healthyMax) return 'healthy'
  if (value <= slowMax) return 'slow'
  if (value <= verySlowMax) return 'very-slow'
  return 'critical'
}

export function formatInteger(value?: number | null) {
  if (typeof value !== 'number') return '-'
  return new Intl.NumberFormat('zh-CN').format(value)
}

export function formatCurrency(value?: number | null, currency?: string | null) {
  if (typeof value !== 'number') return '-'
  const prefix = currency && currency.toUpperCase() !== 'USD' ? `${currency} ` : '$'
  return `${prefix}${formatDecimal(value)}`
}

export function formatPrice(value?: number | null, currency?: string | null) {
  if (typeof value !== 'number') return '-'
  return `${formatCurrency(value, currency)}/1M tokens`
}

export function formatPerRequestPrice(value?: number | null, currency?: string | null, t?: TFunction) {
  if (typeof value !== 'number') return '-'
  const price = formatCurrency(value, currency)
  return t ? t('detail.formula.perRequestUnit', { price }) : `${price}/req`
}

export function formatMetricValue(value?: number | null, suffix = '') {
  if (typeof value !== 'number') return null
  return `${formatDecimal(value)}${suffix}`
}

export function formatTokenMetric(value?: number | null) {
  if (typeof value !== 'number') return null
  return `${formatInteger(value)} tokens`
}

export function formatTokenCompact(value?: number | null): string | null {
  if (typeof value !== 'number') return null
  if (value >= 1_000_000) return `${+(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${+(value / 1_000).toFixed(1)}K`
  return String(value)
}

export function compactJoin(parts: Array<string | null | undefined>, separator = ' / ') {
  return parts.filter(Boolean).join(separator)
}

function formatDecimal(value?: number | null) {
  if (typeof value !== 'number') return '-'

  return new Intl.NumberFormat('zh-CN', {
    maximumFractionDigits: 8,
  }).format(value)
}


export function formatFailureResponse(value: unknown) {
  if (value == null) return '-'
  if (typeof value === 'string') return value

  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}
