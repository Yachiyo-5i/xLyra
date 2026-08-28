import { apiFetch, apiFetchResponse, APIError } from '@/lib/http'
import { listCanonicalModels, listSiteModels, listSitesWithOAuth, type SiteModel } from '@/features/sites/api/sites'

export type AgentSession = {
  session_id: string
  title?: string
  preview?: string
  updated_at?: string | number
  running?: boolean
}

export type AgentMeta = {
  runner?: string
  agent?: string
  version?: string
  package?: string
  models?: string[]
  channels?: string[]
  [key: string]: unknown
}

export type AgentHealth = {
  status: string
  runner?: string
  agent?: string
}

export type AgentRuntimeSettings = {
  runner_base_url: string
  runner_token_configured: boolean
  allowed_site_ids: string[]
  allowed_site_model_ids: string[]
  site_policy: 'allow_all' | 'allow_list'
  model_policy: 'allow_all' | 'allow_list'
}

export type AgentAvailableSite = {
  site_id: string
  site_name: string
  site_type: string
  enabled: boolean
  models: Array<Pick<SiteModel, 'id' | 'upstream_model_name' | 'display_name' | 'canonical_model_id'> & { model_key?: string; category?: string }>
}

export async function fetchAgentHealth() {
  return apiFetch<AgentHealth>('/api/v1/agent/health')
}

export async function fetchAgentRuntimeSettings() {
  const response = await apiFetch<{ data?: AgentRuntimeSettings }>('/api/v1/agent/settings')
  return response.data ?? { runner_base_url: '', runner_token_configured: false, allowed_site_ids: [], allowed_site_model_ids: [], site_policy: 'allow_all', model_policy: 'allow_all' }
}

export async function updateAgentRuntimeSettings(settings: { runner_base_url: string; runner_token?: string; allowed_site_ids: string[]; allowed_site_model_ids: string[]; site_policy: 'allow_all' | 'allow_list'; model_policy: 'allow_all' | 'allow_list' }) {
  const response = await apiFetch<{ data?: AgentRuntimeSettings }>('/api/v1/agent/settings', {
    method: 'PUT',
    body: settings,
  })
  return response.data ?? { ...settings, runner_token_configured: Boolean(settings.runner_token) }
}

export async function fetchAgentAvailableModels(): Promise<AgentAvailableSite[]> {
  const [sitesResponse, canonicalResponse] = await Promise.all([listSitesWithOAuth(), listCanonicalModels()])
  const canonicalById = new Map(canonicalResponse.items.map((model) => [model.id, model]))
  const rows = await Promise.all(sitesResponse.items.map(async (site) => {
    if (!site.enabled) {
      return { site_id: site.id, site_name: site.name, site_type: site.site_type, enabled: false, models: [] }
    }
    try {
      const response = await listSiteModels(site.id)
      return {
        site_id: site.id,
        site_name: site.name,
        site_type: site.site_type,
        enabled: true,
        models: response.items.filter((model) => model.enabled !== false && model.status === 'active').map(({ id, upstream_model_name, display_name, canonical_model_id }) => {
          const canonical = canonical_model_id ? canonicalById.get(canonical_model_id) : undefined
          return { id, upstream_model_name, display_name, canonical_model_id, model_key: canonical?.model_key, category: canonical?.category }
        }),
      }
    } catch {
      return { site_id: site.id, site_name: site.name, site_type: site.site_type, enabled: true, models: [] }
    }
  }))
  return rows.filter((site) => !site.enabled || site.models.length > 0)
}

export async function fetchAgentMeta() {
  return apiFetch<AgentMeta>('/api/v1/agent/meta')
}

export async function listAgentSessions() {
  const response = await apiFetch<{ data?: AgentSession[] }>('/api/v1/agent/sessions')
  return response.data ?? []
}

export type AgentAttachment = {
  name: string
  mime_type: string
  data_url?: string
}

export type AgentModelMemory = {
  default_model: string
  sessions: Record<string, string>
}

export async function fetchAgentModelMemory(): Promise<AgentModelMemory> {
  const response = await apiFetch<{ data?: AgentModelMemory }>('/api/v1/agent/model-memory')
  return response.data ?? { default_model: '', sessions: {} }
}

export async function createAgentSession(input: { content: string; model?: string; session_id?: string; reasoning_effort?: string; permission_mode?: 'ask' | 'full'; attachments?: AgentAttachment[] }) {
  const response = await apiFetch<{ data: { session_id: string; message_id?: string; run_id: string } }>('/api/v1/agent/sessions', {
    method: 'POST',
    body: input,
  })
  return response.data
}

export async function fetchAgentTranscript(sessionId: string) {
  const response = await apiFetch<{ data?: { entries?: AgentTranscriptEntry[] } }>(`/api/v1/agent/sessions/${encodeURIComponent(sessionId)}/transcript`)
  return response.data?.entries ?? []
}

export type AgentTranscriptEntry = {
  type?: string
  /** 早期扁平格式字段（兼容） */
  role?: string
  content?: string
  payload?: unknown
  timestamp?: string
  /** runner 转录格式：type === 'message' 时的消息信封 */
  message?: {
    role?: 'system' | 'user' | 'assistant' | 'tool'
    content?: string
    tool_calls?: Array<{ id: string; name: string; raw_arguments: string }>
    tool_call_id?: string
    name?: string
    is_error?: boolean
    thinking?: string
  }
  /** type === 'compaction' */
  summary?: string
  /** type === 'escalation' */
  escalation_id?: string
  tool_name?: string
  requested_path?: string
  resolved_path?: string
  requested_command?: string[]
  granted?: boolean | null
}

export async function stopAgentSession(sessionId: string) {
  return apiFetch<Record<string, unknown>>(`/api/v1/agent/sessions/${encodeURIComponent(sessionId)}/stop`, { method: 'POST' })
}

export async function deleteAgentSession(sessionId: string) {
  return apiFetch<Record<string, unknown>>(`/api/v1/agent/sessions/${encodeURIComponent(sessionId)}`, { method: 'DELETE' })
}

export async function grantAgentAccess(sessionId: string, input: { escalation_id: string; granted: boolean; resolved_path: string }) {
  return apiFetch<Record<string, unknown>>(`/api/v1/agent/sessions/${encodeURIComponent(sessionId)}/grant-access`, {
    method: 'POST',
    body: input,
  })
}

export async function followAgentEvents(sessionId: string, signal: AbortSignal, onEvent: (event: { type: string; data: unknown }) => void) {
  const response = await apiFetchResponse(`/api/v1/agent/sessions/${encodeURIComponent(sessionId)}/events`, { signal })
  if (!response.body) throw new APIError('Agent event stream is unavailable', response.status)
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  const flush = (raw: string) => {
    let type = 'message'
    const data: string[] = []
    for (const line of raw.split(/\r\n|\r|\n/)) {
      if (line.startsWith('event:')) type = line.slice(6).trim()
      if (line.startsWith('data:')) data.push(line.slice(5).trim())
    }
    if (!data.length) return
    let parsed: unknown = data.join('\n')
    try { parsed = JSON.parse(data.join('\n')) as unknown } catch { parsed = data.join('\n') }
    onEvent({ type, data: parsed })
  }
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    let boundary = /\r\n\r\n|\n\n|\r\r/.exec(buffer)
    while (boundary) {
      flush(buffer.slice(0, boundary.index))
      buffer = buffer.slice(boundary.index + boundary[0].length)
      boundary = /\r\n\r\n|\n\n|\r\r/.exec(buffer)
    }
  }
}
