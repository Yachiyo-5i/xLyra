import { apiFetch } from '@/lib/http'

type RouteCanonicalModel = {
  id: string
  model_key: string
  display_name: string
  provider?: string
  category?: string
  capabilities?: Record<string, unknown>
  status: string
  created_at?: string
  updated_at?: string
}

export type RouteOverviewItem = {
  canonical_model: RouteCanonicalModel
  candidate_summary: {
    site_model_count: number
    site_count: number
    eligible_count: number
    cooldown_count: number
  }
  traffic_24h: {
    request_count: number
    success_count: number
    success_rate?: number | null
    avg_latency_ms?: number | null
    prompt_tokens?: number | null
    completion_tokens?: number | null
    estimated_cost?: number | null
  }
  last_route: {
    routed_at?: string | null
    site_name?: string | null
    status_code?: number | null
    success?: boolean | null
  }
}

type RouteCandidateSite = {
  id: string
  name: string
  slug: string
  site_type: string
  base_url: string
  routing_priority: number
  supports_api_key_cost_multiplier?: boolean
}

type RouteCandidateModel = {
  site_model_id: string
  upstream_model_name: string
  display_name: string
  canonical_match_source: string
  canonical_match_confidence: number
}

type RouteCandidateHealth = {
  status: string
  recent_success_rate?: number | null
  recent_avg_latency_ms?: number | null
  consecutive_failures: number
  model_success_rate?: number | null
  model_avg_latency_ms?: number | null
  model_request_count?: number | null
}

type RouteCandidateAvailability = {
  available_api_key_count: number
  total_api_key_count: number
}

type RouteCandidatePricing = {
  group_name?: string | null
  currency?: string | null
  base_input_value?: number | null
  base_output_value?: number | null
  base_per_request_value?: number | null
  input_value?: number | null
  output_value?: number | null
  per_request_value?: number | null
  upstream_cost_multiplier?: number | null
  billing_type?: string | null
  quota_type?: number | null
}

export type RouteCandidateItem = {
  rank: number
  score: number
  site: RouteCandidateSite
  model: RouteCandidateModel
  health: RouteCandidateHealth
  availability: RouteCandidateAvailability
  credential: {
    id?: string | null
    name?: string | null
    routing_priority: number
    group_name?: string | null
    upstream_cost_multiplier: number
  }
  pricing: RouteCandidatePricing
  score_breakdown?: Record<string, number>
}

export type RouteCandidateResponse = {
  canonical_model: RouteCanonicalModel
  items: RouteCandidateItem[]
  meta: {
    count: number
    debug: boolean
  }
}

export type RouteSelectionResponse = {
  canonical_model: RouteCanonicalModel
  selected: RouteCandidateItem
  failover: RouteCandidateItem[]
  meta: {
    debug: boolean
    failover_count: number
    excluded_site_ids?: string[]
    excluded_site_model_ids?: string[]
  }
}

type RouteTraceAttempt = {
  id: string
  request_id: string
  attempt: number
  credential_attempt: number
  credential_total: number
  status_code?: number | null
  upstream_status_code?: number | null
  success: boolean
  error_type?: string | null
  latency_ms?: number | null
  upstream_latency_ms?: number | null
  stream?: boolean | null
  response_mode?: string | null
  downstream_path?: string | null
  upstream_url?: string | null
  pricing_group?: string | null
  api_key: {
    id?: string | null
    name?: string | null
    masked_key?: string | null
  }
  credential: {
    id?: string | null
    name?: string | null
    masked_secret?: string | null
    routing_priority?: number | null
    group_name?: string | null
    upstream_cost_multiplier?: number | null
  }
  cost?: Record<string, unknown> | null
  site: {
    id?: string | null
    name?: string | null
    slug?: string | null
    site_type?: string | null
  }
  model: {
    canonical_model_id?: string | null
    canonical_model?: string | null
    site_model_id?: string | null
    upstream_model?: string | null
    display_name?: string | null
  }
  failure_response?: unknown
  created_at: string
}

export type RouteTrace = {
  parent_request_id: string
  started_at: string
  finished_at: string
  success: boolean
  attempt_count: number
  failover_count: number
  total_latency_ms: number
  attempts: RouteTraceAttempt[]
}

export type RouteCooldown = {
  id: string
  site_id: string
  site_model_id?: string | null
  site_credential_id?: string | null
  scope: string
  source: string
  reason: string
  active_until: string
  remaining_seconds: number
  cleared_at?: string | null
  metadata?: unknown
  created_at: string
  updated_at: string
}

export const routeQueryKeys = {
  all: ['routes'] as const,
  overview: (filters: { windowHours: number }) => [...routeQueryKeys.all, 'overview', filters] as const,
  selection: (input: { modelKey: string; debug: boolean; failoverLimit: number }) =>
    [...routeQueryKeys.all, 'selection', input] as const,
  candidates: (input: { modelKey: string; debug?: boolean }) => [...routeQueryKeys.all, 'candidates', input] as const,
  traces: (input: { limit: number; modelKey?: string | null }) => [...routeQueryKeys.all, 'traces', input] as const,
  cooldowns: () => [...routeQueryKeys.all, 'cooldowns'] as const,
}

export async function listRoutes(input?: { windowHours?: number }) {
  const params = new URLSearchParams()
  if (input?.windowHours) {
    params.set('window_hours', String(input.windowHours))
  }

  return apiFetch<{ items: RouteOverviewItem[]; meta?: { count?: number; window_hours?: number } }>(
    `/api/v1/routes${params.toString() ? `?${params.toString()}` : ''}`,
  )
}

export async function listRouteCandidates(modelKey: string, options?: { debug?: boolean }) {
  const params = new URLSearchParams({ model_key: modelKey })
  if (options?.debug) {
    params.set('debug', 'true')
  }

  return apiFetch<RouteCandidateResponse>(`/api/v1/routes/candidates?${params.toString()}`)
}

export async function selectRoute(input: {
  modelKey: string
  debug?: boolean
  failoverLimit?: number
  excludeSiteIds?: string[]
  excludeSiteModelIds?: string[]
}) {
  return apiFetch<RouteSelectionResponse>('/api/v1/routes/select', {
    method: 'POST',
    body: JSON.stringify({
      model_key: input.modelKey,
      debug: input.debug ?? true,
      failover_limit: input.failoverLimit ?? 3,
      exclude_site_ids: input.excludeSiteIds,
      exclude_site_model_ids: input.excludeSiteModelIds,
    }),
  })
}

export async function listRouteTraces(input?: { limit?: number; modelKey?: string | null }) {
  const params = new URLSearchParams()
  params.set('limit', String(input?.limit ?? 20))
  if (input?.modelKey) {
    params.set('model_key', input.modelKey)
  }

  return apiFetch<{ items: RouteTrace[]; meta?: { count?: number; limit?: number; model_key?: string | null } }>(
    `/api/v1/routes/traces?${params.toString()}`,
  )
}

export async function listRouteCooldowns() {
  return apiFetch<{ items: RouteCooldown[]; meta?: { count?: number } }>('/api/v1/routes/cooldowns')
}

export async function clearRouteCooldown(input: {
  siteId: string
  siteModelId?: string | null
  siteCredentialId?: string | null
  source?: string | null
}) {
  return apiFetch<{ ok: boolean }>('/api/v1/routes/cooldowns/clear', {
    method: 'POST',
    body: JSON.stringify({
      site_id: input.siteId,
      site_model_id: input.siteModelId || undefined,
      site_credential_id: input.siteCredentialId || undefined,
      source: input.source || undefined,
    }),
  })
}
