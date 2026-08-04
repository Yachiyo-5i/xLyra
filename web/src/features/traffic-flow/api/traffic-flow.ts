import { apiFetch } from '@/lib/http'

export type TrafficFlowNode = {
  id: string
  name: string
  site_type?: string
}

export type TrafficFlowTopology = {
  gateway: TrafficFlowNode
  downstream: TrafficFlowNode[]
  upstream: TrafficFlowNode[]
}

export type TrafficFlowPhase = 'accepted' | 'routed' | 'responding' | 'completed' | 'failed' | 'cancelled'

export type TrafficFlowRequest = {
  request_id: string
  api_key_id: string
  api_key_name: string
  model_key: string
  model_provider: string
  upstream_site_id?: string
  upstream_site_name?: string
  upstream_site_type?: string
  attempt: number
  stream: boolean
  phase: TrafficFlowPhase
  started_at: string
  updated_at: string
}

export type TrafficFlowSnapshot = {
  sequence: number
  requests: TrafficFlowRequest[]
  total_tokens?: number
  downstream_usage?: TrafficFlowUsageTotal[]
  upstream_usage?: TrafficFlowUsageTotal[]
}

export type TrafficFlowUsageTotal = {
  id: string
  name: string
  total_tokens: number
}

export type TrafficFlowEvent = {
  sequence: number
  type: 'upsert' | 'remove' | 'usage'
  request?: TrafficFlowRequest
  request_id?: string
  tokens?: number
  total_tokens?: number
  downstream_usage?: TrafficFlowUsageTotal
  upstream_usage?: TrafficFlowUsageTotal
}

export async function getTrafficFlowTopology() {
  return apiFetch<TrafficFlowTopology>('/api/v1/traffic-flow/topology')
}

export function createTrafficFlowStream() {
  const base = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '')
  return new EventSource(`${base}/api/v1/traffic-flow/stream`, { withCredentials: true })
}
