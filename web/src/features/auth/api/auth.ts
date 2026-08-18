import { apiFetch } from '@/lib/http'

export type AdminRecord = {
  id: string
  username: string
  nickname: string
  avatar: string
  role: string
  status: string
  totpEnabled: boolean
  lastLoginAt?: string | null
  createdAt?: string
  updatedAt?: string
}

export type AdminAuthSession = {
  expiresAt: string | null
  csrfToken: string | null
  admin: AdminRecord
}

export type CurrentAdminSession = {
  expiresAt: string | null
  csrfToken: string | null
  admin: AdminRecord
}

export type CurrentAuthState = {
  initialized: boolean
  authenticated: boolean
  session: CurrentAdminSession | null
}

type AdminAuthSessionResponse = {
  expires_at: string | null
  csrf_token?: string | null
  admin: AdminResponse
}

type CurrentAuthStateResponse = {
  initialized: boolean
  authenticated: boolean
  expires_at?: string | null
  csrf_token?: string | null
  auth_type?: string
  admin?: AdminResponse
}

export type AdminResponse = {
  id: string
  username: string
  nickname?: string
  avatar?: string
  role: string
  status: string
  totp_enabled?: boolean
  last_login_at?: string | null
  created_at?: string
  updated_at?: string
}

export type RegisterBootstrapInput = {
  username: string
  password: string
}

export type SessionCredentials = RegisterBootstrapInput & {
  totpCode?: string
}

export function normalizeAdmin(response: AdminResponse): AdminRecord {
  return {
    id: response.id,
    username: response.username,
    nickname: response.nickname ?? '',
    avatar: response.avatar ?? '',
    role: response.role,
    status: response.status,
    totpEnabled: response.totp_enabled ?? false,
    lastLoginAt: response.last_login_at ?? null,
    createdAt: response.created_at,
    updatedAt: response.updated_at,
  }
}

function normalizeAuthSession(response: AdminAuthSessionResponse): AdminAuthSession {
  return {
    expiresAt: response.expires_at ?? null,
    csrfToken: response.csrf_token ?? null,
    admin: normalizeAdmin(response.admin),
  }
}

export async function fetchAuthState() {
  const response = await apiFetch<CurrentAuthStateResponse>('/api/v1/auth/state', {
    auth: 'none',
    cache: 'no-store',
  })

  if (!response.authenticated || !response.admin) {
    return {
      initialized: response.initialized,
      authenticated: false,
      session: null,
    } satisfies CurrentAuthState
  }

  return {
    initialized: response.initialized,
    authenticated: true,
    session: {
      expiresAt: response.expires_at ?? null,
      csrfToken: response.csrf_token ?? null,
      admin: normalizeAdmin(response.admin),
    },
  } satisfies CurrentAuthState
}

export async function registerBootstrapAdmin(input: RegisterBootstrapInput) {
  const response = await apiFetch<AdminAuthSessionResponse>('/api/v1/bootstrap/register', {
    method: 'POST',
    auth: 'none',
    body: input,
  })

  return normalizeAuthSession(response)
}

export async function createAdminSession(input: SessionCredentials) {
  const response = await apiFetch<AdminAuthSessionResponse>('/api/v1/auth/session', {
    method: 'POST',
    auth: 'none',
    body: {
      username: input.username,
      password: input.password,
      ...(input.totpCode ? { totp_code: input.totpCode } : {}),
    },
  })

  return normalizeAuthSession(response)
}

export async function deleteCurrentAdminSession() {
  return apiFetch<void>('/api/v1/auth/session', {
    method: 'DELETE',
  })
}
