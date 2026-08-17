import type {
  RequestActivityEvent,
  RequestActivityPhase,
  RequestActivityRequest,
  RequestActivitySnapshot,
  RequestLogItem,
  RequestLogListFilters,
} from '@/features/requests/api/requests'

export type RequestActivityState = {
  sequence: number
  requests: Record<string, RequestActivityRequest>
  startedAtByRequestID: Record<string, string>
}

export type RequestLogDisplayItem = RequestLogItem & {
  display_key?: string
  display_started_at?: string | null
}

export function initialRequestActivityState(): RequestActivityState {
  return { sequence: -1, requests: {}, startedAtByRequestID: {} }
}

export function decodeRequestActivitySnapshot(rawData: string): RequestActivitySnapshot | null {
  const raw = parseRecord(rawData)
  if (!raw) return null

  const sequence = safeSequence(raw.sequence)
  if (sequence == null || !Array.isArray(raw.requests)) return null

  return {
    sequence,
    requests: raw.requests
      .map(normalizeRequestActivity)
      .filter((request): request is RequestActivityRequest => request != null),
  }
}

export function decodeRequestActivityEvent(eventName: string, rawData: string): RequestActivityEvent | null {
  const raw = parseRecord(rawData)
  if (!raw) return null

  const sequence = safeSequence(raw.sequence)
  if (sequence == null || !isActivityEventType(eventName)) return null

  if (eventName === 'upsert') {
    const request = normalizeRequestActivity(raw.request)
    return request ? { sequence, type: 'upsert', request } : null
  }

  if (eventName === 'remove') {
    const requestID = stringValue(raw.request_id)
    return requestID ? { sequence, type: 'remove', request_id: requestID } : null
  }

  return { sequence, type: 'usage' }
}

export function reduceRequestActivitySnapshot(
  state: RequestActivityState,
  snapshot: RequestActivitySnapshot,
): RequestActivityState {
  if (snapshot.sequence < state.sequence) return state

  const requests: Record<string, RequestActivityRequest> = {}
  const startedAtByRequestID = { ...state.startedAtByRequestID }
  for (const request of snapshot.requests) {
    requests[request.request_id] = request
    startedAtByRequestID[request.request_id] = request.started_at
  }
  return { sequence: snapshot.sequence, requests, startedAtByRequestID }
}

export function reduceRequestActivityEvent(
  state: RequestActivityState,
  event: RequestActivityEvent,
): RequestActivityState {
  if (event.sequence <= state.sequence) return state

  const requests = { ...state.requests }
  const startedAtByRequestID = { ...state.startedAtByRequestID }
  if (event.type === 'upsert' && event.request) {
    requests[event.request.request_id] = event.request
    startedAtByRequestID[event.request.request_id] = event.request.started_at
  } else if (event.type === 'remove' && event.request_id) {
    delete requests[event.request_id]
  }

  return { sequence: event.sequence, requests, startedAtByRequestID }
}

export function removeRequestActivityRequest(state: RequestActivityState, requestID: string): RequestActivityState {
  if (!state.requests[requestID]) return state
  const requests = { ...state.requests }
  delete requests[requestID]
  return { ...state, requests }
}

export function requestLogItemFromActivity(request: RequestActivityRequest): RequestLogDisplayItem {
  const model = request.model_key || null
  return {
    id: `live:${request.request_id}`,
    request_id: request.request_id,
    parent_request_id: request.request_id,
    state: 'in_progress',
    phase: request.phase,
    is_live: true,
    started_at: request.started_at,
    updated_at: request.updated_at,
    display_started_at: request.started_at,
    requested_model: model,
    original_model: model,
    mapped_model: model,
    attempt: request.attempt > 0 ? request.attempt : null,
    stream: request.stream,
    response_mode: request.stream ? 'stream' : 'non_stream',
    success: false,
    api_key: {
      id: request.api_key_id || null,
      name: request.api_key_name || null,
    },
    site: {
      id: request.upstream_site_id || null,
      name: request.upstream_site_name || null,
      site_type: request.upstream_site_type || null,
    },
    model: {
      canonical_model: model,
      display_name: model,
    },
    usage: {},
    created_at: request.started_at,
  }
}

export function requestLogMatchesFilters(item: RequestLogItem, filters: RequestLogListFilters) {
  if (item.is_live === true && typeof filters.success === 'boolean') return false
  if (filters.siteId && item.site.id !== filters.siteId) return false
  if (filters.apiKeyId && item.api_key.id !== filters.apiKeyId) return false
  if (filters.hideWithoutSite && !item.site.id) return false
  if (filters.errorType || filters.endpoint) return false

  const requestIDFilter = filters.requestId?.trim()
  if (requestIDFilter && !item.request_id.includes(requestIDFilter)) return false

  const modelValues = [
    item.requested_model,
    item.original_model,
    item.mapped_model,
    item.model.canonical_model,
    item.model.upstream_model,
    item.model.display_name,
  ].filter((value): value is string => Boolean(value))
  if (filters.modelKey && !modelValues.some((value) => value.toLowerCase().includes(filters.modelKey!.toLowerCase()))) return false

  if (filters.search) {
    const searchValues = [
      item.request_id,
      ...modelValues,
      item.site.id,
      item.site.name,
      item.api_key.id,
      item.api_key.name,
    ].filter((value): value is string => Boolean(value))
    if (!searchValues.some((value) => value.toLowerCase().includes(filters.search!.toLowerCase()))) return false
  }

  const createdAt = Date.parse(item.created_at)
  if (!Number.isFinite(createdAt)) return false
  if (filters.createdFrom && createdAt < Date.parse(filters.createdFrom)) return false
  if (filters.createdTo && createdAt > Date.parse(filters.createdTo)) return false
  return true
}

export function mergeRequestLogItems(
  history: RequestLogItem[],
  liveRequests: RequestActivityRequest[],
  filters: RequestLogListFilters,
  page: number,
  handoffRequestIDs: ReadonlySet<string> = new Set(),
  startedAtByRequestID: Readonly<Record<string, string>> = {},
): RequestLogDisplayItem[] {
  if (page !== 1 || filters.success !== null && filters.success !== undefined) return history

  const live = liveRequests
    .map(requestLogItemFromActivity)
    .filter((item) => requestLogMatchesFilters(item, filters))

  const renderedLive = live.map((item) => {
    if (!handoffRequestIDs.has(item.request_id)) return item
    const replacement = history.find((historyItem) => (
      requestLogParentID(historyItem) === requestLogParentID(item) && requestLogMatchesFilters(historyItem, filters)
    ))
    return replacement
      ? {
          ...replacement,
          display_key: requestLogDisplayKey(item),
          display_started_at: item.started_at,
        }
      : item
  })
  const liveParentIDs = new Set(live.map(requestLogParentID))
  const historyItems = history
    .filter((item) => !liveParentIDs.has(requestLogParentID(item)))
    .map((item) => applyDisplayStartedAt(item, startedAtByRequestID))
  return [
    ...renderedLive,
    ...historyItems,
  ].sort(compareRequestLogItems)
}

export function requestLogParentID(item: Pick<RequestLogItem, 'parent_request_id' | 'request_id'>) {
  return item.parent_request_id || item.request_id
}

export function requestLogDisplayKey(item: Pick<RequestLogItem, 'id'> & { display_key?: string }) {
  return item.display_key || item.id
}

function compareRequestLogItems(left: RequestLogDisplayItem, right: RequestLogDisplayItem) {
  const startedAtDifference = requestLogTimestamp(right) - requestLogTimestamp(left)
  if (startedAtDifference !== 0) return startedAtDifference

  return requestLogDisplayKey(left).localeCompare(requestLogDisplayKey(right))
}

function requestLogTimestamp(item: RequestLogDisplayItem) {
  return requestTimestamp(item.display_started_at)
    ?? requestTimestamp(item.started_at)
    ?? requestTimestamp(item.created_at)
    ?? 0
}

function requestTimestamp(value?: string | null): number | null {
  const timestamp = value ? Date.parse(value) : Number.NaN
  return Number.isFinite(timestamp) ? timestamp : null
}

function applyDisplayStartedAt(
  item: RequestLogDisplayItem,
  startedAtByRequestID: Readonly<Record<string, string>>,
): RequestLogDisplayItem {
  const startedAt = startedAtByRequestID[requestLogParentID(item)]
  if (!startedAt || item.display_started_at === startedAt) return item
  return { ...item, display_started_at: startedAt }
}

function normalizeRequestActivity(value: unknown): RequestActivityRequest | null {
  const raw = asRecord(value)
  if (!raw) return null

  const requestID = stringValue(raw.request_id)
  const startedAt = stringValue(raw.started_at)
  const updatedAt = stringValue(raw.updated_at)
  const phase = activityPhase(raw.phase)
  if (!requestID || !startedAt || !updatedAt || !phase) return null

  return {
    request_id: requestID,
    api_key_id: stringValue(raw.api_key_id) ?? '',
    api_key_name: stringValue(raw.api_key_name) ?? '',
    model_key: stringValue(raw.model_key) ?? '',
    model_provider: stringValue(raw.model_provider) ?? '',
    upstream_site_id: stringValue(raw.upstream_site_id) ?? undefined,
    upstream_site_name: stringValue(raw.upstream_site_name) ?? undefined,
    upstream_site_type: stringValue(raw.upstream_site_type) ?? undefined,
    attempt: numberValue(raw.attempt) ?? 0,
    stream: raw.stream === true,
    phase,
    started_at: startedAt,
    updated_at: updatedAt,
  }
}

function parseRecord(value: string) {
  try {
    return asRecord(JSON.parse(value) as unknown)
  } catch {
    return null
  }
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null
}

function stringValue(value: unknown) {
  return typeof value === 'string' && value.trim() ? value.trim() : null
}

function numberValue(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function safeSequence(value: unknown) {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 ? value : null
}

function activityPhase(value: unknown): RequestActivityPhase | null {
  return value === 'accepted' || value === 'routed' || value === 'responding' || value === 'completed' || value === 'failed' || value === 'cancelled'
    ? value
    : null
}

function isActivityEventType(value: string): value is 'upsert' | 'remove' | 'usage' {
  return value === 'upsert' || value === 'remove' || value === 'usage'
}
