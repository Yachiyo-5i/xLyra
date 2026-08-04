import { apiFetch } from '@/lib/http'

type RequestLogAPIKey = {
  id?: string | null
  name?: string | null
  masked_key?: string | null
}

type RequestLogSite = {
  id?: string | null
  name?: string | null
  slug?: string | null
  site_type?: string | null
}

type RequestLogModel = {
  canonical_model_id?: string | null
  canonical_model?: string | null
  site_model_id?: string | null
  upstream_model?: string | null
  display_name?: string | null
}

type RequestLogUsage = {
  prompt_tokens?: number | null
  completion_tokens?: number | null
  total_tokens?: number | null
  estimated_cost?: number | null
  currency?: string | null
}

type RequestLogPricing = {
  group_name?: string | null
  currency?: string | null
  input_value?: number | null
  output_value?: number | null
  cache_input_value?: number | null
  group_ratio?: number | null
  model_ratio?: number | null
  completion_ratio?: number | null
  cache_ratio?: number | null
  model_price?: number | null
  per_request_value?: number | null
  billing_type?: string | null
  quota_type?: number | null
}

type RequestLogCostCalculation = {
  formula?: string | null
  service_tier?: string | null
  billing_mode?: string | null
  long_context?: boolean | null
  long_context_threshold_tokens?: number | null
  long_context_input_multiplier?: number | null
  long_context_output_multiplier?: number | null
  prompt_tokens?: number | null
  completion_tokens?: number | null
  cache_tokens?: number | null
  cached_tokens?: number | null
  cache_write_tokens?: number | null
  cache_creation_tokens?: number | null
  cache_creation_5m_tokens?: number | null
  cache_creation_1h_tokens?: number | null
  input_value?: number | null
  output_value?: number | null
  cache_input_value?: number | null
  cache_write_input_value?: number | null
  cache_write_ratio?: number | null
  cache_write_1h_input_value?: number | null
  cache_write_1h_ratio?: number | null
  model_price?: number | null
  group_ratio?: number | null
  per_request_value?: number | null
  prompt_cost?: number | null
  completion_cost?: number | null
  per_request_cost?: number | null
  cache_cost?: number | null
  cache_input_cost?: number | null
  cached_cost?: number | null
  cache_write_cost?: number | null
  cache_write_5m_cost?: number | null
  cache_write_1h_cost?: number | null
  base_estimated_cost?: number | null
  cost_multiplier?: number | null
  cost_multiplier_reason?: string | null
  estimated_cost?: number | null
  billing_type?: string | null
  quota_type?: number | null
  currency?: string | null
}

type RequestLogRateLimitScope = {
  scope?: string | null
  scope_key?: string | null
  rpm_limit?: number | null
  tpm_limit?: number | null
  rpm_used?: number | null
  tpm_reserved?: number | null
  tpm_actual?: number | null
  reserved_tokens?: number | null
}

type RequestLogRateLimit = {
  estimated_tokens?: number | null
  reserved_tokens?: number | null
  actual_tokens?: number | null
  window_start?: string | null
  scope_results?: RequestLogRateLimitScope[] | null
}

export type RequestLogItem = {
  id: string
  request_id: string
  parent_request_id?: string | null
  scope?: string | null
  is_test?: boolean | null
  requested_model?: string | null
  original_model?: string | null
  mapped_model?: string | null
  mapping_mode?: string | null
  stream?: boolean | null
  response_mode?: string | null
  endpoint?: string | null
  downstream_path?: string | null
  upstream_path?: string | null
  upstream_url?: string | null
  status_code?: number | null
  upstream_status_code?: number | null
  success: boolean
  error_type?: string | null
  latency_ms?: number | null
  upstream_latency_ms?: number | null
  request_tokens?: number | null
  response_tokens?: number | null
  pricing_group?: string | null
  api_key: RequestLogAPIKey
  site: RequestLogSite
  model: RequestLogModel
  usage: RequestLogUsage
  pricing?: RequestLogPricing | null
  cost_calculation?: RequestLogCostCalculation | null
  rate_limit?: RequestLogRateLimit | null
  failure_response?: unknown
  created_at: string
}

export type RequestLogDetail = RequestLogItem & {
  metadata?: Record<string, unknown>
}

type RequestLogListMeta = {
  count?: number
  total?: number
  total_cost?: number
  currency?: string | null
  page?: number
  page_size?: number
  total_pages?: number
  success?: boolean | null
  site_id?: string | null
  api_key_id?: string | null
  search?: string | null
  model_key?: string | null
  error_type?: string | null
  endpoint?: string | null
  request_id?: string | null
  hide_without_site?: boolean
  created_from?: string | null
  created_to?: string | null
  rate_usage?: {
    rpm: number
    tpm: number
    window_seconds: number
  } | null
}

export type RequestLogListResponse = {
  items: RequestLogItem[]
  meta?: RequestLogListMeta
}

export type RequestLogSummary = {
  total_cost?: number | null
  prompt_tokens?: number | null
  completion_tokens?: number | null
  total_tokens?: number | null
  cached_tokens?: number | null
  currency?: string | null
  supported?: boolean
  unsupported_reason?: string | null
}

export type RequestLogSummaryResponse = {
  summary: RequestLogSummary
}

export type RequestLogListFilters = {
  success?: boolean | null
  siteId?: string
  apiKeyId?: string
  search?: string
  modelKey?: string
  errorType?: string
  endpoint?: string
  requestId?: string
  hideWithoutSite?: boolean
  createdFrom?: string
  createdTo?: string
}

export const requestQueryKeys = {
  all: ['requests'] as const,
  list: (filters: RequestLogListFilters & RequestLogListPagination) => [...requestQueryKeys.all, 'list', filters] as const,
  summary: (filters: RequestLogListFilters) => [...requestQueryKeys.all, 'summary', filters] as const,
  detail: (requestLogId: string) => [...requestQueryKeys.all, 'detail', requestLogId] as const,
}

export type RequestLogListPagination = {
  page: number
  pageSize: number
}

export async function listRequestLogs(input: RequestLogListFilters & RequestLogListPagination) {
  const params = requestLogSearchParams(input)
  params.set('page', String(input.page))
  params.set('page_size', String(input.pageSize))

  return apiFetch<RequestLogListResponse>(`/api/v1/requests?${params.toString()}`)
}

export async function getRequestLogSummary(input: RequestLogListFilters) {
  const params = requestLogSearchParams(input)
  return apiFetch<RequestLogSummaryResponse>(`/api/v1/requests/summary?${params.toString()}`)
}

function requestLogSearchParams(input: RequestLogListFilters) {
  const params = new URLSearchParams()
  if (typeof input.success === 'boolean') {
    params.set('success', String(input.success))
  }
  if (input.siteId) params.set('site_id', input.siteId)
  if (input.apiKeyId) params.set('api_key_id', input.apiKeyId)
  if (input.search) params.set('search', input.search)
  if (input.modelKey) params.set('model_key', input.modelKey)
  if (input.errorType) params.set('error_type', input.errorType)
  if (input.endpoint) params.set('endpoint', input.endpoint)
  if (input.requestId) params.set('request_id', input.requestId)
  if (typeof input.hideWithoutSite === 'boolean') params.set('hide_without_site', String(input.hideWithoutSite))
  if (input.createdFrom) params.set('created_from', input.createdFrom)
  if (input.createdTo) params.set('created_to', input.createdTo)
  return params
}

export async function getRequestLog(requestLogId: string) {
  return apiFetch<{ request: RequestLogDetail }>(`/api/v1/requests/${requestLogId}`)
}
