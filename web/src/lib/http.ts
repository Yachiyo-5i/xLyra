type APIErrorEnvelope = {
  error?: {
    code?: string
    message?: string
    request_id?: string
  }
  message?: string
  code?: string | number
}

type CurrentSessionResponse = {
  csrf_token?: string | null
}

export class APIError extends Error {
  status: number
  code?: string
  requestId?: string

  constructor(message: string, status: number, code?: string, requestId?: string) {
    super(message)
    this.name = 'APIError'
    this.status = status
    this.code = code
    this.requestId = requestId
  }
}

export type RequestOptions = Omit<RequestInit, 'body'> & {
  body?: BodyInit | Record<string, unknown> | null
  auth?: 'none' | 'session'
  csrf?: false
}

export type DownloadTicket = {
  id: string
  url: string
  filename: string
  content_type: string
  expires_at: string
}

const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '')
const CSRF_HEADER_NAME = 'X-CSRF-Token'
const CSRF_ERROR_CODES = new Set(['csrf_token_required', 'csrf_token_invalid'])

let csrfToken: string | null = null
let csrfRefreshPromise: Promise<string | null> | null = null

export function setCSRFToken(token: string | null | undefined) {
  csrfToken = token?.trim() || null
}

function buildURL(path: string) {
  if (/^https?:\/\//.test(path)) {
    return path
  }

  return `${API_BASE}${path}`
}

export function apiURL(path: string) {
  return buildURL(path)
}

function isBodyInit(value: RequestOptions['body']): value is BodyInit {
  return (
    value instanceof Blob ||
    value instanceof FormData ||
    value instanceof URLSearchParams ||
    typeof value === 'string' ||
    value instanceof ArrayBuffer
  )
}

function isMutationMethod(method?: string) {
  const normalized = (method ?? 'GET').toUpperCase()
  return !['GET', 'HEAD', 'OPTIONS', 'TRACE'].includes(normalized)
}

function shouldAttachCSRF(path: string, options: RequestOptions, headers: Headers) {
  if (options.csrf === false || !isMutationMethod(options.method)) {
    return false
  }
  if (headers.has(CSRF_HEADER_NAME) || headers.has('X-Access-Token')) {
    return false
  }
  if (path === '/api/v1/bootstrap/register' || (path === '/api/v1/auth/session' && (options.method ?? 'GET').toUpperCase() === 'POST')) {
    return false
  }
  return true
}

async function toAPIError(response: Response) {
  const contentType = response.headers.get('content-type') ?? ''
  const isJSON = contentType.includes('application/json')

  let message = `Request failed with status ${response.status}`
  let code: string | undefined
  let requestId: string | undefined

  if (isJSON) {
    const payload = (await response.json()) as APIErrorEnvelope
    message = payload.error?.message ?? payload.message ?? message
    code = payload.error?.code ?? (payload.code !== undefined ? String(payload.code) : undefined)
    requestId = payload.error?.request_id
  } else {
    const text = (await response.text()).trim()
    if (text) {
      message = text
    }
  }

  return new APIError(message, response.status, code, requestId)
}

async function parseResponse<T>(response: Response): Promise<T> {
  const contentType = response.headers.get('content-type') ?? ''
  const isJSON = contentType.includes('application/json')

  if (response.status === 204) {
    return undefined as T
  }

  if (!isJSON) {
    return (await response.text()) as T
  }

  return (await response.json()) as T
}

async function refreshCSRFToken() {
  if (!csrfRefreshPromise) {
    csrfRefreshPromise = (async () => {
      const response = await fetch(buildURL('/api/v1/auth/session'), {
        method: 'GET',
        credentials: 'include',
      })
      if (!response.ok) {
        throw await toAPIError(response)
      }
      const session = await parseResponse<CurrentSessionResponse>(response)
      setCSRFToken(session.csrf_token ?? null)
      return csrfToken
    })().finally(() => {
      csrfRefreshPromise = null
    })
  }

  return csrfRefreshPromise
}

async function syncAuthStoreCSRFToken(token: string | null) {
  try {
    const { useAuthStore } = await import('@/stores/auth-store')
    useAuthStore.getState().setCSRFToken(token)
  } catch {
    setCSRFToken(token)
  }
}

async function sendRequest(path: string, options: RequestOptions, retryOnCSRFError: boolean) {
  const { body: rawBody, headers: headerInit, ...requestInit } = options
  const headers = new Headers(headerInit)
  let body = rawBody
  const attachCSRF = shouldAttachCSRF(path, options, headers)

  if (body && !isBodyInit(body)) {
    headers.set('Content-Type', 'application/json')
    body = JSON.stringify(body)
  }
  if (attachCSRF && csrfToken) {
    headers.set(CSRF_HEADER_NAME, csrfToken)
  }

  const response = await fetch(buildURL(path), {
    ...requestInit,
    body: body ?? undefined,
    headers,
    credentials: 'include',
  })

  if (!response.ok) {
    const error = await toAPIError(response)
    if (retryOnCSRFError && attachCSRF && error.status === 403 && error.code && CSRF_ERROR_CODES.has(error.code)) {
      const refreshedToken = await refreshCSRFToken()
      await syncAuthStoreCSRFToken(refreshedToken)
      if (refreshedToken) {
        return sendRequest(path, { ...options, headers: new Headers(headerInit) }, false)
      }
    }
    throw error
  }

  return response
}

export async function apiFetch<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const response = await sendRequest(path, options, true)
  return parseResponse<T>(response)
}

export async function apiFetchResponse(path: string, options: RequestOptions = {}) {
  return sendRequest(path, options, true)
}

export async function apiFetchBlob(path: string, options: RequestOptions = {}) {
  const response = await sendRequest(path, options, true)

  return {
    blob: await response.blob(),
    filename: response.headers.get('x-download-filename') ?? response.headers.get('x-backup-filename') ?? undefined,
  }
}
