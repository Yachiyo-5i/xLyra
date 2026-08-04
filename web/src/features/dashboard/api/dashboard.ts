import { apiFetch } from '@/lib/http'

export type DashboardDays = 7 | 30 | 90

export type DashboardOverview = {
  meta: {
    days: DashboardDays
    available_days: DashboardDays[]
    timezone: string
    generated_at: string
    today_start: string
    range_start: string
    range_end: string
  }
  kpis: {
    cost: {
      today: number
      yesterday: number
      total: number
      currency: string
    }
    requests: {
      today: number
      yesterday: number
      total: number
      today_tokens: number
      yesterday_tokens: number
      total_tokens: number
      today_prompt_tokens: number
      total_prompt_tokens: number
      today_completion_tokens: number
      total_completion_tokens: number
      today_cached_tokens: number
      total_cached_tokens: number
      success_rate?: number | null
    }
    rate_limit: {
      rpm: {
        used: number
        limit?: number | null
      }
      tpm: {
        used: number
        actual: number
        reserved: number
        limit?: number | null
      }
      completed_rpm?: number
    }
  }
  charts: {
    daily_model_cost: DashboardDailyModelCostPoint[]
    daily_model_requests: DashboardDailyModelRequestPoint[]
    daily_site_cost: DashboardDailySiteCostPoint[]
    daily_site_requests: DashboardDailySiteRequestPoint[]
    daily_api_key_usage: DashboardDailyAPIKeyUsagePoint[]
    api_key_contributions: DashboardDailyAPIKeyUsagePoint[]
    site_cost_summary: DashboardSiteCostSummaryItem[]
  }
  windows: Record<string, DashboardOverviewWindow | undefined>
  health: {
    uptime_rows: DashboardUptimeRow[]
  }
  cooldowns: {
    items: DashboardCooldownAPIItem[]
  }
  attention?: {
    items: DashboardAttentionItem[]
  }
  insights: {
    failure_reasons: DashboardFailureReasonItem[]
    insufficient_candidates: DashboardInsufficientCandidateItem[]
    high_latency: DashboardHighLatencyItem[]
  }
}

export type DashboardUsage = Pick<DashboardOverview, 'meta' | 'kpis' | 'charts' | 'windows'>
export type DashboardCooldowns = DashboardOverview['cooldowns']
export type DashboardHealth = DashboardOverview['health']
export type DashboardInsights = Pick<DashboardOverview, 'insights' | 'attention'>

export type DashboardSystemResourcePoint = {
  timestamp: string
  time: string
  cpu_usage_percent: number
  memory_usage_percent: number
  disk_usage_percent: number
  disk_read_bytes_per_sec: number
  disk_write_bytes_per_sec: number
  disk_read_bytes_total: number
  disk_write_bytes_total: number
  network_rx_bytes_per_sec: number
  network_tx_bytes_per_sec: number
  network_rx_bytes_total: number
  network_tx_bytes_total: number
  cpu: {
    usage_percent: number
    cores: number
    load1: number
    load5: number
    load15: number
  }
  memory: {
    total_bytes: number
    used_bytes: number
    available_bytes: number
    usage_percent: number
  }
  disk: {
    path: string
    total_bytes: number
    used_bytes: number
    free_bytes: number
    usage_percent: number
    read_bytes_per_sec: number
    write_bytes_per_sec: number
    read_bytes_total: number
    write_bytes_total: number
  }
  network: {
    rx_bytes_per_sec: number
    tx_bytes_per_sec: number
    rx_bytes_total: number
    tx_bytes_total: number
  }
}

export type DashboardDailyModelCostPoint = {
  date: string
  model_id?: string | null
  model_key: string
  cost: number
  currency: string
}

export type DashboardDailyModelRequestPoint = {
  date: string
  model_id?: string | null
  model_key: string
  request_count: number
}

export type DashboardDailySiteCostPoint = {
  date: string
  site_id?: string | null
  site_name: string
  site_slug: string
  site_type: string
  site_key: string
  cost: number
  currency: string
}

export type DashboardDailySiteRequestPoint = {
  date: string
  site_id?: string | null
  site_name: string
  site_slug: string
  site_type: string
  site_key: string
  request_count: number
}

export type DashboardDailyAPIKeyUsagePoint = {
  date: string
  api_key_id: string
  api_key_name: string
  total_tokens: number
  cost: number
  currency: string
}

type DashboardSiteCostSummaryItem = {
  site_id: string
  site_name: string
  site_slug: string
  site_type: string
  request_count: number
  success_count: number
  success_rate?: number | null
  total_tokens: number
  cost: number
  currency: string
}

type DashboardOverviewWindow = {
  days: DashboardDays
  range_start: string
  range_end: string
  site_cost_summary: DashboardSiteCostSummaryItem[]
  failure_reasons: DashboardFailureReasonItem[]
  high_latency: DashboardHighLatencyItem[]
}

type DashboardUptimeRow = {
  site_id: string
  site_name: string
  site_slug: string
  site_type: string
  buckets: Array<{
    hour: string
    status: 'healthy' | 'degraded' | 'unhealthy' | 'idle' | string
    success_count: number
    failure_count: number
    total_count: number
    success_rate?: number | null
  }>
}

export type DashboardCooldownAPIItem = {
  id: string
  site_id: string
  site_name: string
  site_model_id?: string | null
  canonical_model?: string | null
  upstream_model_name?: string | null
  site_credential_id?: string | null
  credential_name?: string | null
  masked_key?: string | null
  scope: string
  source: string
  reason: string
  active_until: string
  remaining_seconds: number
  metadata?: Record<string, unknown> | null
}

type DashboardFailureReasonItem = {
  reason: string
  request_count: number
}

type DashboardInsufficientCandidateItem = {
  canonical_model_id: string
  model_key: string
  display_name: string
  site_model_count: number
  site_count: number
  eligible_count: number
  cooldown_count: number
}

type DashboardHighLatencyItem = {
  site_id?: string | null
  site_name: string
  model_id?: string | null
  model_key: string
  request_count: number
  avg_latency_ms: number
  p95_latency_ms: number
}

type DashboardAttentionSeverity = 'critical' | 'warning' | 'info' | string

type DashboardAttentionAction = {
  type?: string
  params?: Record<string, string>
  label?: string
  path?: string
}

export type DashboardAttentionItem = {
  id: string
  severity: DashboardAttentionSeverity
  type: string
  title?: string | null
  description?: string | null
  subject?: Record<string, unknown> | null
  metrics?: Record<string, unknown> | null
  action?: DashboardAttentionAction | null
  primary_action?: DashboardAttentionAction | null
  related?: Record<string, unknown> | null
  updated_at?: string | null
  metadata?: Record<string, unknown> | null
}

export const dashboardQueryKeys = {
  all: ['dashboard'] as const,
  usage: () => [...dashboardQueryKeys.all, 'usage'] as const,
  cooldowns: () => [...dashboardQueryKeys.all, 'cooldowns'] as const,
  health: () => [...dashboardQueryKeys.all, 'health'] as const,
  insights: () => [...dashboardQueryKeys.all, 'insights'] as const,
}

export async function getDashboardUsage() {
  return apiFetch<DashboardUsage>('/api/v1/dashboard/usage')
}

export async function getDashboardCooldowns() {
  return apiFetch<DashboardCooldowns>('/api/v1/dashboard/cooldowns')
}

export async function getDashboardHealth() {
  return apiFetch<DashboardHealth>('/api/v1/dashboard/health')
}

export async function getDashboardInsights() {
  return apiFetch<DashboardInsights>('/api/v1/dashboard/insights')
}

export function createDashboardResourceStream() {
  const base = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '')
  return new EventSource(`${base}/api/v1/dashboard/resources/stream`, { withCredentials: true })
}
