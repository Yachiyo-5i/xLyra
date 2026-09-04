import { apiFetch, apiFetchResponse, apiURL, APIError } from '@/lib/http'
import { listCanonicalModels, listSiteModels, listSitesWithOAuth, type SiteModel } from '@/features/sites/api/sites'

export type AgentSession = {
  session_id: string
  title?: string
  preview?: string
  updated_at?: string | number
  running?: boolean
}

export type AgentHealth = {
  status?: string
  runner?: string
  agent?: string
  /** Runner version, injected at image build time. */
  version?: string
  /** Installed xlyra-agent npm version; absent when AGENT_MANAGED=false (self-managed). */
  agent_version?: string
}

export type AgentRuntimeSettings = {
  runner_base_url: string
  runner_token_configured: boolean
  allowed_site_ids: string[]
  allowed_site_model_ids: string[]
  site_policy: 'allow_all' | 'allow_list'
  model_policy: 'allow_all' | 'allow_list'
  appearance: AgentAppearanceSettings
}

export type AgentAppearanceSettings = {
  background_image: string
  custom_background_images: string[]
  side_transparency: number
  side_brightness: number
  side_thickness: number
  backdrop_blur: number
  backdrop_dim: number
}

export const defaultAgentAppearance: AgentAppearanceSettings = {
  background_image: '/agent-backdrop.png',
  custom_background_images: [],
  side_transparency: 49,
  side_brightness: 32,
  side_thickness: 28,
  backdrop_blur: 13,
  backdrop_dim: 69,
}

export type AgentAvailableSite = {
  site_id: string
  site_name: string
  site_type: string
  enabled: boolean
  models: Array<Pick<SiteModel, 'id' | 'upstream_model_name' | 'display_name' | 'canonical_model_id'> & { model_key?: string; category?: string }>
}

// The runner returns 503 with a body that still carries version fields when
// unhealthy, so this bypasses apiFetch (which throws on non-2xx) and parses the
// body manually; network or proxy failures yield null.
export async function fetchAgentHealth(): Promise<AgentHealth | null> {
  try {
    const response = await fetch(apiURL('/api/v1/agent/health'), { credentials: 'include' })
    const payload: unknown = await response.json().catch(() => null)
    if (!payload || typeof payload !== 'object' || Array.isArray(payload)) {
      return { status: response.ok ? 'healthy' : 'unhealthy' }
    }
    return payload as AgentHealth
  } catch {
    return null
  }
}

export type AgentUpgradeState = {
  status: 'installing' | 'restarting' | 'failed'
  target: string
  error?: string
}

export type AgentVersionInfo = {
  managed: boolean
  current?: string
  latest: string | null
  update_available: boolean
  checked_at: string | null
  check_error: string | null
  active_runs: number
  upgrade: AgentUpgradeState | null
}

// Older runners lack this endpoint (404) or may be unreachable; null degrades the UI to the current version only.
export async function fetchAgentVersion(refresh = false): Promise<AgentVersionInfo | null> {
  try {
    return await apiFetch<AgentVersionInfo>(`/api/v1/agent/version${refresh ? '?refresh=true' : ''}`)
  } catch {
    return null
  }
}

// Business rejections (agent_busy, upgrade_in_progress, ...) surface as APIError with the runner-proxied error code.
export async function upgradeAgent(input: { force?: boolean } = {}) {
  return apiFetch<{ success?: boolean; target?: string }>('/api/v1/agent/upgrade', { method: 'POST', body: input })
}

export async function fetchAgentRuntimeSettings() {
  const response = await apiFetch<{ data?: AgentRuntimeSettings }>('/api/v1/agent/settings')
  return {
    runner_base_url: response.data?.runner_base_url ?? '',
    runner_token_configured: response.data?.runner_token_configured ?? false,
    allowed_site_ids: response.data?.allowed_site_ids ?? [],
    allowed_site_model_ids: response.data?.allowed_site_model_ids ?? [],
    site_policy: response.data?.site_policy ?? 'allow_all',
    model_policy: response.data?.model_policy ?? 'allow_all',
    appearance: {
      ...defaultAgentAppearance,
      ...(response.data?.appearance ?? {}),
      custom_background_images: response.data?.appearance?.custom_background_images ?? [],
    },
  }
}

// The settings endpoint is a partial update: absent fields stay unchanged, so runner connection and site/model scope are saved independently.
export async function updateAgentRunnerSettings(settings: { runner_base_url: string; runner_token?: string }) {
  const response = await apiFetch<{ data?: AgentRuntimeSettings }>('/api/v1/agent/settings', {
    method: 'PUT',
    body: settings,
  })
  return response.data
}

export async function clearAgentRunnerSettings() {
  const response = await apiFetch<{ data?: AgentRuntimeSettings }>('/api/v1/agent/settings', {
    method: 'PUT',
    body: { clear_runner: true },
  })
  return response.data
}

export async function updateAgentScopeSettings(settings: { site_policy: 'allow_all' | 'allow_list'; model_policy: 'allow_all' | 'allow_list'; allowed_site_ids: string[]; allowed_site_model_ids: string[] }) {
  const response = await apiFetch<{ data?: AgentRuntimeSettings }>('/api/v1/agent/settings', {
    method: 'PUT',
    body: settings,
  })
  return response.data
}

export async function updateAgentAppearanceSettings(appearance: AgentAppearanceSettings) {
  const response = await apiFetch<{ data?: AgentRuntimeSettings }>('/api/v1/agent/settings', {
    method: 'PUT',
    body: { appearance },
  })
  return response.data
}

export async function fetchAgentAvailableModels(): Promise<AgentAvailableSite[]> {
  const [sitesResponse, canonicalResponse] = await Promise.all([listSitesWithOAuth(), listCanonicalModels()])
  const canonicalById = new Map((canonicalResponse.items ?? []).map((model) => [model.id, model]))
  const rows = await Promise.all((sitesResponse.items ?? []).map(async (site) => {
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
        models: (response.items ?? []).filter((model) => model.enabled !== false && model.status === 'active').map(({ id, upstream_model_name, display_name, canonical_model_id }) => {
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

// ---- Capabilities: Skills / AGENTS.md (xLyra → runner → agent) ----

export type AgentSkill = {
  name: string
  description: string
  /** Project scope is editable/deletable (stored in the workspace); user/extra scopes can only be toggled. */
  scope?: 'project' | 'user' | 'extra'
  enabled: boolean
  path?: string
  license?: string
}

export type AgentSkillsInfo = {
  /** Global toggle (agent enable_skills). */
  enabled: boolean
  skills: AgentSkill[]
  warnings?: string[]
}

// Older runners/agents lack this endpoint (404) or may be unreachable; null degrades the UI.
export async function listAgentSkills(): Promise<AgentSkillsInfo | null> {
  try {
    const response = await apiFetch<{ data?: AgentSkillsInfo }>('/api/v1/agent/skills')
    return response.data ?? { enabled: true, skills: [] }
  } catch {
    return null
  }
}

/** Reads a workspace file; null when the file does not exist. */
export async function fetchWorkspaceFile(path: string): Promise<string | null> {
  try {
    const response = await apiFetch<{ data?: { content?: string } }>(`/api/v1/agent/workspace/file?path=${encodeURIComponent(path)}`)
    return response.data?.content ?? ''
  } catch (error) {
    if (error instanceof APIError && error.status === 404) return null
    throw error
  }
}

export async function putWorkspaceFile(path: string, content: string) {
  return apiFetch<{ success?: boolean }>('/api/v1/agent/workspace/file', { method: 'PUT', body: { path, content } })
}

export async function deleteWorkspaceFile(path: string) {
  return apiFetch<{ success?: boolean }>(`/api/v1/agent/workspace/file?path=${encodeURIComponent(path)}`, { method: 'DELETE' })
}

export type AgentSkillDetail = AgentSkill & {
  metadata?: Record<string, string>
  /** Full SKILL.md including frontmatter. */
  content: string
  contentTruncated?: boolean
  /** Resource file paths relative to the skill root. */
  resources: string[]
}

// Older runners/agents lack this endpoint; returns null.
export async function fetchAgentSkillDetail(name: string): Promise<AgentSkillDetail | null> {
  try {
    const response = await apiFetch<{ data?: AgentSkillDetail }>(`/api/v1/agent/skills/${encodeURIComponent(name)}`)
    return response.data ?? null
  } catch {
    return null
  }
}

/** Reads a single resource file inside a skill directory (doc/script preview). */
export async function fetchAgentSkillFile(name: string, path: string): Promise<{ content: string; truncated?: boolean } | null> {
  try {
    const response = await apiFetch<{ data?: { content: string; truncated?: boolean } }>(
      `/api/v1/agent/skills/${encodeURIComponent(name)}/file?path=${encodeURIComponent(path)}`,
    )
    return response.data ?? null
  } catch {
    return null
  }
}

export type AgentCapabilitiesConfig = {
  enable_agents_md?: boolean
  enable_skills?: boolean
  disabled_skills?: string[]
  /** 自动压缩水位线 0~1：上下文占用达到窗口的该比例时触发压缩（默认 0.9） */
  compact_trigger_ratio?: number
  /** 全局上下文窗口回退（token）：模型未在端点 models 声明时使用；再缺省按 200k 兜底 */
  context_window?: number
}

export type AgentEndpointConfig = {
  name: string
  models?: Record<string, { context_window?: number }>
} & Record<string, unknown>

type AgentConfigEnvelope = {
  endpoints?: AgentEndpointConfig[]
  agent?: AgentCapabilitiesConfig & Record<string, unknown>
  /** server 段必须原样回带：agent /config 是整体保存，漏掉会丢 server 配置 */
  server?: Record<string, unknown>
}

export async function fetchAgentCapabilities(): Promise<AgentCapabilitiesConfig | null> {
  try {
    const response = await apiFetch<{ data?: AgentConfigEnvelope }>('/api/v1/agent/config')
    return response.data?.agent ?? {}
  } catch {
    return null
  }
}

export async function fetchAgentConfigEnvelope(): Promise<AgentConfigEnvelope | null> {
  try {
    const response = await apiFetch<{ data?: AgentConfigEnvelope }>('/api/v1/agent/config')
    return response.data ?? null
  } catch {
    return null
  }
}

// agent /config saves as a whole: read the full config, merge the agent section, write back (masked credentials are preserved server-side).
export async function updateAgentCapabilities(patch: AgentCapabilitiesConfig) {
  const current = await apiFetch<{ data?: AgentConfigEnvelope }>('/api/v1/agent/config')
  const data = current.data ?? {}
  const agent = { ...(data.agent ?? {}), ...patch }
  await apiFetch('/api/v1/agent/config', { method: 'PUT', body: { endpoints: data.endpoints ?? [], agent, ...(data.server ? { server: data.server } : {}) } })
}

/** 上下文设置：压缩水位线 + 全局上下文窗口（undefined 表示删除回退，恢复默认 200k 兜底） */
export async function updateAgentContextSettings(settings: {
  compactTriggerRatio: number
  contextWindow?: number
}) {
  const current = await apiFetch<{ data?: AgentConfigEnvelope }>('/api/v1/agent/config')
  const data = current.data ?? {}
  const agent: AgentCapabilitiesConfig & Record<string, unknown> = { ...(data.agent ?? {}), compact_trigger_ratio: settings.compactTriggerRatio }
  if (settings.contextWindow !== undefined && Number.isFinite(settings.contextWindow) && settings.contextWindow > 0) {
    agent.context_window = Math.floor(settings.contextWindow)
  } else {
    delete agent.context_window
  }
  await apiFetch('/api/v1/agent/config', { method: 'PUT', body: { endpoints: data.endpoints ?? [], agent, ...(data.server ? { server: data.server } : {}) } })
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

export async function retryAgentSession(sessionId: string, input: { message_id: string; content?: string; model?: string; reasoning_effort?: string; permission_mode?: 'ask' | 'full'; attachments?: AgentAttachment[] }) {
  const response = await apiFetch<{ data: { session_id: string; message_id?: string; run_id: string } }>(`/api/v1/agent/sessions/${encodeURIComponent(sessionId)}/retry`, {
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
  message_id?: string
  /** Early flat-format fields (compat). */
  role?: string
  content?: string
  payload?: unknown
  timestamp?: string
  /** Runner transcript envelope when type === 'message'. */
  message?: {
    role?: 'system' | 'user' | 'assistant' | 'tool'
    content?: string
    attachments?: Array<{ name: string; mime_type: string; data_url?: string }>
    tool_calls?: Array<{ id: string; name: string; raw_arguments: string }>
    tool_call_id?: string
    name?: string
    is_error?: boolean
    thinking?: string
  }
  /** type === 'compaction' */
  summary?: string
  tokens_before?: number
  tokens_after?: number
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

/** 手动压缩指令：会话输入框发送的原文，前端识别后走压缩接口而不是普通消息 */
export const AGENT_COMPACT_COMMAND = '/compact'

/** 手动压缩：经 /sessions 命令路径（runner 拦截后登记合成 run 再转发压缩接口），同步返回摘要与 token 变化 */
export async function compactAgentSession(sessionId: string, model?: string) {
  const response = await apiFetch<{ data?: { summary?: string; tokens_before?: number; tokens_after?: number; compaction_id?: string } }>('/api/v1/agent/sessions', {
    method: 'POST',
    body: { content: AGENT_COMPACT_COMMAND, session_id: sessionId, ...(model ? { model } : {}) },
  })
  return response.data ?? {}
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
