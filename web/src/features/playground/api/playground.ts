import { apiFetch, apiFetchResponse, APIError } from '@/lib/http'
import type {
  ChatProtocol,
  Conversation,
  GatewayModel,
  ImageConversation,
  PlaygroundMode,
  PlaygroundRun,
} from '@/features/playground/lib/types'

export {
  listDownstreamAPIKeys,
  downstreamAPIKeyQueryKeys,
} from '@/features/api-keys/api/api-keys'
export type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'

type GatewayModelPayload = {
  data?: Array<{
    id?: string
    owned_by?: string
    metadata?: { display_name?: string; category?: string; mapped_model?: string; supported_endpoint_types?: unknown }
  }>
}

function normalizeEndpointTypes(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value
    .map((item) => (typeof item === 'string' ? item.trim().toLowerCase() : ''))
    .filter((item) => item.length > 0)
}

function gatewayModelsFromPayload(payload: GatewayModelPayload): GatewayModel[] {
  const items = Array.isArray(payload.data) ? payload.data : []
  return items
    .filter((item): item is { id: string } & typeof item => typeof item.id === 'string' && item.id.length > 0)
    .map((item) => ({
      id: item.id,
      mappedModel: item.metadata?.mapped_model?.trim() || undefined,
      displayName: item.metadata?.display_name?.trim() || item.id,
      category: item.metadata?.category?.trim().toLowerCase() || 'chat',
      ownedBy: item.owned_by,
      endpointTypes: normalizeEndpointTypes(item.metadata?.supported_endpoint_types),
    }))
    .sort((left, right) => left.id.localeCompare(right.id, undefined, { sensitivity: 'base' }))
}

export async function listPlaygroundModels(apiKeyId: string): Promise<GatewayModel[]> {
  const payload = await apiFetch<GatewayModelPayload>(`/api/v1/playground/models?api_key_id=${encodeURIComponent(apiKeyId)}`)
  return gatewayModelsFromPayload(payload)
}

export type ServerConversationView = {
  id: string
  mode: PlaygroundMode
  title: string
  chat?: Conversation
  image?: ImageConversation
  run?: PlaygroundRun
  last_ordinal: number
  updated_at: number
}

export type PlaygroundRolloutEvent = {
  timestamp: string
  ordinal: number
  type: string
  run_id?: string
  payload: unknown
}

export async function listServerConversations(mode?: PlaygroundMode): Promise<ServerConversationView[]> {
  const suffix = mode ? `?mode=${encodeURIComponent(mode)}` : ''
  const response = await apiFetch<{ items: ServerConversationView[] }>(`/api/v1/playground/conversations${suffix}`)
  return response.items ?? []
}

export function getServerConversation(id: string): Promise<ServerConversationView> {
  return apiFetch<ServerConversationView>(`/api/v1/playground/conversations/${id}`)
}

export type StartServerTurnInput = {
  mode: PlaygroundMode
  api_key_id: string
  model: string
  protocol?: ChatProtocol
  reasoning_effort?: string
  idempotency_key: string
  legacy_import?: boolean
  chat?: Conversation
  image?: ImageConversation
}

export function startServerTurn(id: string, input: StartServerTurnInput): Promise<ServerConversationView> {
  return apiFetch<ServerConversationView>(`/api/v1/playground/conversations/${id}/turns`, {
    method: 'POST',
    body: input,
  })
}

export function cancelServerRun(id: string): Promise<{ status: string }> {
  return apiFetch<{ status: string }>(`/api/v1/playground/runs/${id}/cancel`, { method: 'POST' })
}

export function deleteServerConversation(id: string): Promise<void> {
  return apiFetch<void>(`/api/v1/playground/conversations/${id}`, { method: 'DELETE' })
}

export async function followServerConversation(
  id: string,
  after: number,
  signal: AbortSignal,
  onEvent: (event: PlaygroundRolloutEvent) => void,
  onRun: (run: PlaygroundRun) => void,
): Promise<void> {
  const response = await apiFetchResponse(`/api/v1/playground/conversations/${id}/events?after=${after}`, { signal })
  if (!response.body) throw new APIError('Playground event stream is unavailable', response.status)
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  const flush = (raw: string) => {
    let eventType = 'message'
    const data: string[] = []
    for (const line of raw.split(/\r\n|\r|\n/)) {
      if (line.startsWith('event:')) eventType = line.slice(6).trim()
      if (line.startsWith('data:')) data.push(line.slice(5).trim())
    }
    if (data.length === 0) return
    const parsed = JSON.parse(data.join('\n')) as unknown
    if (eventType === 'rollout') onEvent(parsed as PlaygroundRolloutEvent)
    if (eventType === 'run_status') onRun(parsed as PlaygroundRun)
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
