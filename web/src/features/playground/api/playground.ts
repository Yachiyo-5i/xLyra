import { apiURL, APIError } from '@/lib/http'
import type {
  ChatAttachment,
  ChatProtocol,
  ChatUsage,
  GatewayModel,
  ImageResultItem,
} from '@/features/playground/lib/types'
import { newId } from '@/features/playground/lib/storage'

const ROUTE_SITE_HEADER = 'X-Xlyra-Route-Site'
const PLAYGROUND_API_BASE = '/api/playground/v1'

export {
  listDownstreamAPIKeys,
  revealDownstreamAPIKey,
  downstreamAPIKeyQueryKeys,
} from '@/features/api-keys/api/api-keys'
export type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'

type GatewayErrorEnvelope = {
  error?: { message?: string; code?: string; request_id?: string }
}

async function toGatewayError(response: Response): Promise<APIError> {
  let message = `Request failed with status ${response.status}`
  let code: string | undefined
  let requestId: string | undefined
  try {
    const payload = (await response.json()) as GatewayErrorEnvelope
    message = payload.error?.message ?? message
    code = payload.error?.code
    requestId = payload.error?.request_id
  } catch {
    return new APIError(message, response.status, code, requestId)
  }
  return new APIError(message, response.status, code, requestId)
}

function authHeaders(apiKey: string): HeadersInit {
  return { Authorization: `Bearer ${apiKey}` }
}

function routeSiteName(response: Response): string | undefined {
  const encoded = response.headers.get(ROUTE_SITE_HEADER)?.trim()
  if (!encoded) return undefined
  try {
    return decodeURIComponent(encoded)
  } catch {
    return encoded
  }
}

async function parseGatewayJSON<T>(response: Response): Promise<T> {
  const contentType = response.headers.get('Content-Type')?.trim() || 'unknown content type'
  if (!contentType.toLowerCase().includes('json')) {
    throw new APIError(
      `Playground API returned ${contentType} instead of JSON`,
      response.status,
      'invalid_response',
    )
  }
  try {
    return await response.json() as T
  } catch {
    throw new APIError('Playground API returned invalid JSON', response.status, 'invalid_response')
  }
}

type GatewayModelPayload = {
  data?: Array<{
    id?: string
    owned_by?: string
    metadata?: { display_name?: string; category?: string; supported_endpoint_types?: unknown }
  }>
}

function normalizeEndpointTypes(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value
    .map((item) => (typeof item === 'string' ? item.trim().toLowerCase() : ''))
    .filter((item) => item.length > 0)
}

export async function listGatewayModels(apiKey: string): Promise<GatewayModel[]> {
  const response = await fetch(apiURL(`${PLAYGROUND_API_BASE}/models`), {
    method: 'GET',
    headers: authHeaders(apiKey),
    cache: 'no-store',
  })
  if (!response.ok) {
    throw await toGatewayError(response)
  }
  const payload = await parseGatewayJSON<GatewayModelPayload>(response)
  const items = Array.isArray(payload.data) ? payload.data : []
  return items
    .filter((item): item is { id: string } & typeof item => typeof item.id === 'string' && item.id.length > 0)
    .map((item) => ({
      id: item.id,
      displayName: item.metadata?.display_name?.trim() || item.id,
      category: item.metadata?.category?.trim().toLowerCase() || 'chat',
      ownedBy: item.owned_by,
      endpointTypes: normalizeEndpointTypes(item.metadata?.supported_endpoint_types),
    }))
    .sort((left, right) => left.id.localeCompare(right.id, undefined, { sensitivity: 'base' }))
}

async function pumpSSE(response: Response, onData: (data: string) => void): Promise<void> {
  const reader = response.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  const flush = (rawEvent: string) => {
    const dataLines = rawEvent
      .split(/\r\n|\r|\n/)
      .map((line) => line.trimStart())
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice('data:'.length).trim())
    if (dataLines.length === 0) return
    const data = dataLines.join('\n')
    if (data === '[DONE]') return
    onData(data)
  }

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    let boundary = /(?:\r\n|\r|\n){2}/.exec(buffer)
    while (boundary) {
      flush(buffer.slice(0, boundary.index))
      buffer = buffer.slice(boundary.index + boundary[0].length)
      boundary = /(?:\r\n|\r|\n){2}/.exec(buffer)
    }
  }
  buffer += decoder.decode()
  if (buffer.trim()) flush(buffer)
}

export type ChatTurn = {
  role: 'system' | 'user' | 'assistant'
  content: string
  attachments?: ChatAttachment[]
}

function availableAttachments(turn: ChatTurn): Array<ChatAttachment & { dataURL: string }> {
  return (turn.attachments ?? []).filter(
    (attachment): attachment is ChatAttachment & { dataURL: string } => Boolean(attachment.dataURL),
  )
}

function openAIChatContent(turn: ChatTurn): unknown {
  const attachments = availableAttachments(turn)
  if (attachments.length === 0) return turn.content
  const parts: Array<Record<string, unknown>> = []
  if (turn.content) parts.push({ type: 'text', text: turn.content })
  for (const attachment of attachments) {
    if (attachment.mimeType.startsWith('image/')) {
      parts.push({ type: 'image_url', image_url: { url: attachment.dataURL } })
    } else {
      parts.push({
        type: 'file',
        file: { filename: attachment.name, file_data: attachment.dataURL },
      })
    }
  }
  return parts
}

function responsesContent(turn: ChatTurn): unknown {
  const attachments = availableAttachments(turn)
  if (attachments.length === 0) return turn.content
  const parts: Array<Record<string, unknown>> = []
  if (turn.content) parts.push({ type: turn.role === 'assistant' ? 'output_text' : 'input_text', text: turn.content })
  for (const attachment of attachments) {
    if (attachment.mimeType.startsWith('image/')) {
      parts.push({ type: 'input_image', image_url: attachment.dataURL })
    } else {
      parts.push({
        type: 'input_file',
        filename: attachment.name,
        file_data: attachment.dataURL,
      })
    }
  }
  return parts
}

function dataURLSource(dataURL: string, fallbackMimeType: string): { mediaType: string; data: string } {
  const match = /^data:([^;,]+)?;base64,(.*)$/s.exec(dataURL)
  return {
    mediaType: match?.[1] || fallbackMimeType || 'application/octet-stream',
    data: match?.[2] || dataURL,
  }
}

function anthropicContent(turn: ChatTurn): unknown {
  const attachments = availableAttachments(turn)
  if (attachments.length === 0) return turn.content
  const parts: Array<Record<string, unknown>> = []
  if (turn.content) parts.push({ type: 'text', text: turn.content })
  for (const attachment of attachments) {
    const source = dataURLSource(attachment.dataURL, attachment.mimeType)
    if (attachment.mimeType.startsWith('image/')) {
      parts.push({
        type: 'image',
        source: { type: 'base64', media_type: source.mediaType, data: source.data },
      })
    } else {
      parts.push({
        type: 'document',
        title: attachment.name,
        source: { type: 'base64', media_type: source.mediaType, data: source.data },
      })
    }
  }
  return parts
}

export type StreamChatInput = {
  apiKey: string
  model: string
  messages: ChatTurn[]
  reasoningEffort?: string
  signal?: AbortSignal
  onContent: (delta: string) => void
  onReasoning: (delta: string) => void
  onUsage: (usage: ChatUsage) => void
  onRouteSite: (siteName: string) => void
}

type ChatStreamChunk = {
  choices?: Array<{
    delta?: { content?: string | null; reasoning_content?: string | null }
  }>
  usage?: ChatUsage | null
  error?: { message?: string } | null
}

export async function streamChatCompletion(input: StreamChatInput): Promise<void> {
  const body: Record<string, unknown> = {
    model: input.model,
    messages: input.messages.map((message) => ({
      role: message.role,
      content: openAIChatContent(message),
    })),
    stream: true,
    stream_options: { include_usage: true },
  }
  if (input.reasoningEffort) body.reasoning_effort = input.reasoningEffort
  const response = await fetch(apiURL(`${PLAYGROUND_API_BASE}/chat/completions`), {
    method: 'POST',
    headers: { ...authHeaders(input.apiKey), 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: input.signal,
  })
  if (!response.ok || !response.body) {
    throw await toGatewayError(response)
  }

  const siteName = routeSiteName(response)
  if (siteName) input.onRouteSite(siteName)

  let failure: string | null = null
  await pumpSSE(response, (data) => {
    let chunk: ChatStreamChunk
    try {
      chunk = JSON.parse(data) as ChatStreamChunk
    } catch {
      return
    }
    if (chunk.error?.message) {
      failure = chunk.error.message
      return
    }
    const delta = chunk.choices?.[0]?.delta
    if (delta?.content) input.onContent(delta.content)
    if (delta?.reasoning_content) input.onReasoning(delta.reasoning_content)
    if (chunk.usage) input.onUsage(chunk.usage)
  })
  if (failure) throw new APIError(failure, response.status)
}

type ResponsesStreamEvent = {
  type?: string
  delta?: string
  response?: {
    usage?: { input_tokens?: number; output_tokens?: number; total_tokens?: number } | null
    error?: { message?: string } | null
  } | null
  message?: string
}

export async function streamResponses(input: StreamChatInput): Promise<void> {
  const inputItems = input.messages
    .filter((message) => message.role !== 'system')
    .map((message) => ({ type: 'message', role: message.role, content: responsesContent(message) }))
  const instructions = input.messages
    .filter((message) => message.role === 'system')
    .map((message) => message.content)
    .join('\n\n')

  const body: Record<string, unknown> = {
    model: input.model,
    input: inputItems,
    stream: true,
  }
  if (instructions.trim()) body.instructions = instructions.trim()
  if (input.reasoningEffort) body.reasoning = { effort: input.reasoningEffort, summary: 'auto' }

  const response = await fetch(apiURL(`${PLAYGROUND_API_BASE}/responses`), {
    method: 'POST',
    headers: { ...authHeaders(input.apiKey), 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: input.signal,
  })
  if (!response.ok || !response.body) {
    throw await toGatewayError(response)
  }

  const siteName = routeSiteName(response)
  if (siteName) input.onRouteSite(siteName)

  let failure: string | null = null
  await pumpSSE(response, (data) => {
    let event: ResponsesStreamEvent
    try {
      event = JSON.parse(data) as ResponsesStreamEvent
    } catch {
      return
    }
    switch (event.type) {
      case 'response.output_text.delta':
        if (event.delta) input.onContent(event.delta)
        break
      case 'response.reasoning_summary_text.delta':
      case 'response.reasoning_text.delta':
        if (event.delta) input.onReasoning(event.delta)
        break
      case 'response.completed':
      case 'response.incomplete': {
        const usage = event.response?.usage
        if (usage) {
          input.onUsage({
            prompt_tokens: usage.input_tokens,
            completion_tokens: usage.output_tokens,
            total_tokens: usage.total_tokens,
          })
        }
        break
      }
      case 'response.failed':
      case 'error':
        failure = event.response?.error?.message || event.message || 'response failed'
        break
      default:
        break
    }
  })

  if (failure) {
    throw new APIError(failure, response.status)
  }
}

type MessagesStreamEvent = {
  type?: string
  delta?: { type?: string; text?: string; thinking?: string }
  message?: { usage?: { input_tokens?: number; output_tokens?: number } | null }
  usage?: { output_tokens?: number } | null
  error?: { message?: string } | null
}

export async function streamMessages(input: StreamChatInput): Promise<void> {
  const system = input.messages
    .filter((message) => message.role === 'system')
    .map((message) => message.content)
    .join('\n\n')
  const messages = input.messages
    .filter((message) => message.role !== 'system')
    .map((message) => ({ role: message.role, content: anthropicContent(message) }))

  const body: Record<string, unknown> = {
    model: input.model,
    max_tokens: 4096,
    messages,
    stream: true,
  }
  if (system.trim()) body.system = system.trim()

  const response = await fetch(apiURL(`${PLAYGROUND_API_BASE}/messages`), {
    method: 'POST',
    headers: {
      ...authHeaders(input.apiKey),
      'Content-Type': 'application/json',
      'anthropic-version': '2023-06-01',
    },
    body: JSON.stringify(body),
    signal: input.signal,
  })
  if (!response.ok || !response.body) {
    throw await toGatewayError(response)
  }

  const siteName = routeSiteName(response)
  if (siteName) input.onRouteSite(siteName)

  let inputTokens = 0
  let failure: string | null = null
  await pumpSSE(response, (data) => {
    let event: MessagesStreamEvent
    try {
      event = JSON.parse(data) as MessagesStreamEvent
    } catch {
      return
    }
    switch (event.type) {
      case 'message_start':
        inputTokens = event.message?.usage?.input_tokens ?? 0
        break
      case 'content_block_delta':
        if (event.delta?.type === 'text_delta' && event.delta.text) input.onContent(event.delta.text)
        if (event.delta?.type === 'thinking_delta' && event.delta.thinking) input.onReasoning(event.delta.thinking)
        break
      case 'message_delta':
        if (event.usage?.output_tokens != null) {
          input.onUsage({
            prompt_tokens: inputTokens,
            completion_tokens: event.usage.output_tokens,
            total_tokens: inputTokens + event.usage.output_tokens,
          })
        }
        break
      case 'error':
        failure = event.error?.message || 'message failed'
        break
      default:
        break
    }
  })

  if (failure) {
    throw new APIError(failure, response.status)
  }
}

export function streamChat(protocol: ChatProtocol, input: StreamChatInput): Promise<void> {
  if (protocol === 'responses') return streamResponses(input)
  if (protocol === 'messages') return streamMessages(input)
  return streamChatCompletion(input)
}

type ImagePayload = {
  data?: Array<{ b64_json?: string; url?: string }>
}

function imageItemsFromPayload(payload: ImagePayload): ImageResultItem[] {
  const items = Array.isArray(payload.data) ? payload.data : []
  return items
    .map((item) => {
      if (item.b64_json) return { id: newId(), src: `data:image/png;base64,${item.b64_json}` }
      if (item.url) return { id: newId(), src: item.url }
      return null
    })
    .filter((item): item is ImageResultItem => item !== null)
}

export type GenerateImageInput = {
  apiKey: string
  model: string
  prompt: string
  size?: string
  n?: number
  signal?: AbortSignal
}

export type ImageRequestResult = {
  images: ImageResultItem[]
  siteName?: string
}

export async function generateImage(input: GenerateImageInput): Promise<ImageRequestResult> {
  const body: Record<string, unknown> = {
    model: input.model,
    prompt: input.prompt,
    n: input.n ?? 1,
    response_format: 'b64_json',
  }
  if (input.size && input.size !== 'auto') body.size = input.size
  const response = await fetch(apiURL(`${PLAYGROUND_API_BASE}/images/generations`), {
    method: 'POST',
    headers: { ...authHeaders(input.apiKey), 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: input.signal,
  })
  if (!response.ok) {
    throw await toGatewayError(response)
  }
  return {
    images: imageItemsFromPayload(await parseGatewayJSON<ImagePayload>(response)),
    siteName: routeSiteName(response),
  }
}

export type EditImageInput = {
  apiKey: string
  model: string
  prompt: string
  images: File[]
  size?: string
  n?: number
  signal?: AbortSignal
}

export async function editImage(input: EditImageInput): Promise<ImageRequestResult> {
  const form = new FormData()
  form.append('model', input.model)
  form.append('prompt', input.prompt)
  form.append('n', String(input.n ?? 1))
  if (input.size && input.size !== 'auto') form.append('size', input.size)
  for (const file of input.images) {
    form.append('image', file)
  }
  const response = await fetch(apiURL(`${PLAYGROUND_API_BASE}/images/edits`), {
    method: 'POST',
    headers: authHeaders(input.apiKey),
    body: form,
    signal: input.signal,
  })
  if (!response.ok) {
    throw await toGatewayError(response)
  }
  return {
    images: imageItemsFromPayload(await parseGatewayJSON<ImagePayload>(response)),
    siteName: routeSiteName(response),
  }
}
