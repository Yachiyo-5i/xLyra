import { apiFetch } from '@/lib/http'

export type AnalyticsGroupBy =
  | 'none'
  | 'site'
  | 'model'
  | 'site_model'
  | 'api_key'
  | 'endpoint'
  | 'error_type'

export type AnalyticsBreakdownItem = {
  key: string
  id?: string | null
  label: string
  requests: number
  success_count: number
  failure_count: number
  success_rate?: number | null
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number
  total_tokens: number
  cost: number
  avg_latency_ms: number
  max_latency_ms: number
}

export type AnalyticsMatrixCell = {
  site: { key: string; id?: string | null; label: string }
  model: { key: string; id?: string | null; label: string }
  requests: number
  total_tokens: number
  cost: number
}

export type AnalyticsSeriesPoint = {
  date: string
  requests: number
  success_count: number
  failure_count: number
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number
  total_tokens: number
  cost: number
  avg_latency_ms: number
  max_latency_ms: number
}

export type AnalyticsSeries = {
  key: string
  id?: string | null
  label: string
  points: AnalyticsSeriesPoint[]
}

export type AnalyticsUsage = {
  meta: {
    from: string
    to: string
    days: number
    timezone: string
    generated_at: string
    group_by: string
    currency: string
    available_currencies: string[]
    data_from?: string | null
    granularity: 'day' | 'hour'
    filters: {
      site_ids: string[]
      model_keys: string[]
      api_key_ids: string[]
      success?: boolean | null
    }
  }
  totals: {
    requests: number
    success_count: number
    failure_count: number
    success_rate?: number | null
    prompt_tokens: number
    completion_tokens: number
    cached_tokens: number
    total_tokens: number
    cache_hit_rate?: number | null
    cost: number
    cost_by_currency: Record<string, number>
    avg_latency_ms: number
    max_latency_ms: number
    avg_upstream_latency_ms: number
    previous_period?: {
      from: string
      to: string
      requests: number
      success_rate?: number | null
      total_tokens: number
      cost: number
      avg_latency_ms: number
    } | null
  }
  breakdowns: {
    site: AnalyticsBreakdownItem[]
    model: AnalyticsBreakdownItem[]
    api_key: AnalyticsBreakdownItem[]
    matrix: AnalyticsMatrixCell[]
  }
  series: AnalyticsSeries[]
  api_key_contributions: Array<{
    date: string
    api_key_id: string
    api_key_name: string
    total_tokens: number
    cost: number
    currency: string
  }>
}

export type AnalyticsUsageParams = {
  from?: string
  to?: string
  group_by?: AnalyticsGroupBy
  site_ids?: string[]
  model_keys?: string[]
  api_key_ids?: string[]
  success?: boolean
  currency?: string
  include_contributions?: boolean
}

export type AnalyticsUsageFact = {
  date: string
  site_key: string
  site_id?: string | null
  site_label: string
  model_key: string
  model_id?: string | null
  model_label: string
  api_key_key: string
  api_key_id?: string | null
  api_key_label: string
  currency: string
  requests: number
  success_count: number
  failure_count: number
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number
  total_tokens: number
  cost: number
  latency_count: number
  latency_total_ms: number
  latency_max_ms: number
  upstream_latency_count: number
  upstream_latency_total_ms: number
}

export type AnalyticsDataset = {
  meta: {
    from: string
    to: string
    previous_from: string
    previous_to: string
    days: number
    timezone: string
    generated_at: string
    data_from?: string | null
    granularity: 'day' | 'hour'
    fact_count: number
    fact_limit: number
  }
  current: AnalyticsUsageFact[]
  previous: AnalyticsUsageFact[]
}

export type AnalyticsOptions = {
  sites: Array<{ id: string; name: string }>
  api_keys: Array<{ id: string; name: string }>
}

export type AnalyticsAPIKeyContributions = {
  generated_at: string
  from: string
  to: string
  points: AnalyticsUsage['api_key_contributions']
}

export type AnalyticsDatasetParams = Pick<AnalyticsUsageParams, 'from' | 'to' | 'success'>

export const analyticsQueryKeys = {
  all: ['analytics'] as const,
  usage: (params: AnalyticsUsageParams) =>
    [...analyticsQueryKeys.all, 'usage', params] as const,
  dataset: (params: AnalyticsDatasetParams) =>
    [...analyticsQueryKeys.all, 'dataset', params] as const,
  options: () => [...analyticsQueryKeys.all, 'options'] as const,
  contributions: () => [...analyticsQueryKeys.all, 'api-key-contributions'] as const,
}

export async function getAnalyticsUsage(params: AnalyticsUsageParams = {}) {
  const search = new URLSearchParams()
  if (params.from) search.set('from', params.from)
  if (params.to) search.set('to', params.to)
  if (params.group_by && params.group_by !== 'none') search.set('group_by', params.group_by)
  if (params.site_ids?.length) search.set('site_ids', params.site_ids.join(','))
  if (params.model_keys?.length) search.set('model_keys', params.model_keys.join(','))
  if (params.api_key_ids?.length) search.set('api_key_ids', params.api_key_ids.join(','))
  if (typeof params.success === 'boolean') search.set('success', String(params.success))
  if (params.currency) search.set('currency', params.currency)
  if (typeof params.include_contributions === 'boolean') search.set('include_contributions', String(params.include_contributions))
  const query = search.toString()
  return apiFetch<AnalyticsUsage>(`/api/v1/analytics/usage${query ? `?${query}` : ''}`)
}

export async function getAnalyticsDataset(params: AnalyticsDatasetParams = {}) {
  const search = new URLSearchParams()
  if (params.from) search.set('from', params.from)
  if (params.to) search.set('to', params.to)
  if (typeof params.success === 'boolean') search.set('success', String(params.success))
  const query = search.toString()
  return apiFetch<AnalyticsDataset>(`/api/v1/analytics/dataset${query ? `?${query}` : ''}`)
}

export async function getAnalyticsOptions() {
  return apiFetch<AnalyticsOptions>('/api/v1/analytics/options')
}

export async function getAnalyticsAPIKeyContributions() {
  return apiFetch<AnalyticsAPIKeyContributions>('/api/v1/analytics/api-key-contributions')
}
