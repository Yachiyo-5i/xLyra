import { APIError, apiURL } from '@/lib/http'

export type PortalDimensions = {
  site: boolean
  model: boolean
  tokens: boolean
  cost: boolean
  latency: boolean
  endpoint: boolean
  upstream: boolean
  error: boolean
}

export type PortalSettings = {
  enabled: boolean
  show_requests: boolean
  show_summary: boolean
  summary_days: number
  dimensions: PortalDimensions
}

export type PortalPeriodicQuota = {
  limit: number | null
  used: number
  remaining: number | null
  unlimited: boolean
  reset_at: string | null
}

export type PortalOverview = {
  key: {
    name: string
    status: string
    is_active: boolean
    masked_key: string
    expires_at: string | null
    last_used_at: string | null
  }
  quota: {
    unit: string
    limit: number | null
    used: number
    accumulated_used: number
    remaining: number | null
    unlimited: boolean
    daily: PortalPeriodicQuota
    weekly: PortalPeriodicQuota
  }
}

export type PortalUsageBucket = {
  date: string
  requests: number
  success: number
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
  cost?: number
}

export type PortalSummary = {
  range: { from: string; to: string; days: number }
  currency: string
  totals: PortalUsageBucket
  trend: PortalUsageBucket[]
}

export type PortalRequestItem = {
  id: string
  request_id: string
  created_at: string
  status_code: number
  success: boolean
  endpoint?: string
  latency_ms?: number | null
  site?: { name: string | null; slug: string | null; site_type: string | null }
  model?: { canonical_model: string | null; upstream_model: string | null; display_name: string | null }
  usage?: {
    prompt_tokens: number | null
    completion_tokens: number | null
    total_tokens: number | null
    cache_tokens: number | null
    cache_write_tokens: number | null
  }
  cost?: { estimated_cost: number | null; currency: string | null }
  upstream?: { url: unknown; path: unknown }
  error_type?: string | null
}

export type PortalRequestStats = {
  total_tokens?: number
  cost?: number
  currency?: string
}

export type PortalRequests = {
  items: PortalRequestItem[]
  page: number
  page_size: number
  total: number
  total_pages: number
  stats: PortalRequestStats
}

export type PortalRequestFilters = {
  status?: 'success' | 'failed'
  model?: string
  from?: string
  to?: string
}

async function portalFetch<T>(path: string, key?: string): Promise<T> {
  const headers = new Headers({ Accept: 'application/json' })
  if (key) {
    headers.set('Authorization', `Bearer ${key}`)
  }
  const response = await fetch(apiURL(path), { method: 'GET', headers })
  if (!response.ok) {
    let message = `Request failed with status ${response.status}`
    let code: string | undefined
    let requestId: string | undefined
    if ((response.headers.get('content-type') ?? '').includes('application/json')) {
      const payload = (await response.json()) as { error?: { code?: string; message?: string; request_id?: string } }
      message = payload.error?.message ?? message
      code = payload.error?.code
      requestId = payload.error?.request_id
    }
    throw new APIError(message, response.status, code, requestId)
  }
  return (await response.json()) as T
}

export function fetchPortalSettings() {
  return portalFetch<PortalSettings>('/v1/portal/settings')
}

export function fetchPortalOverview(key: string) {
  return portalFetch<PortalOverview>('/v1/portal/overview', key)
}

export function fetchPortalSummary(key: string, days?: number) {
  const query = days ? `?days=${days}` : ''
  return portalFetch<PortalSummary>(`/v1/portal/summary${query}`, key)
}

export function fetchPortalRequests(key: string, page: number, pageSize: number, filters: PortalRequestFilters = {}) {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (filters.status) params.set('status', filters.status)
  if (filters.model) params.set('model', filters.model)
  if (filters.from) params.set('from', filters.from)
  if (filters.to) params.set('to', filters.to)
  return portalFetch<PortalRequests>(`/v1/portal/requests?${params.toString()}`, key)
}

export function fetchPortalModels(key: string) {
  return portalFetch<{ models: string[] }>('/v1/portal/models', key)
}
