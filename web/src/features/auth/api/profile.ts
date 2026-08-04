import { apiFetch } from '@/lib/http'
import { normalizeAdmin, type AdminRecord, type AdminResponse } from '@/features/auth/api/auth'

export type AdminSessionItem = {
  id: string
  admin_id: string
  current: boolean
  expires_at?: string | null
  last_seen_at?: string | null
  ip_address?: string | null
  user_agent?: string
  created_at: string
  updated_at: string
}

export type AdminAccessToken = {
  id: string
  admin_id: string
  masked_token: string
  enabled: boolean
  last_used_at?: string | null
  last_used_ip?: string | null
  last_used_user_agent?: string
  token_returned_once?: boolean
  token?: string
  created_at?: string
  updated_at?: string
}

export type AuditLogItem = {
  id: string
  actor_type: string
  admin_id?: string | null
  admin_session_id?: string | null
  access_token_id?: string | null
  action: string
  resource_type: string
  resource_id: string
  ip_address?: string | null
  user_agent?: string
  request_id: string
  success: boolean
  error_code: string
  metadata?: unknown
  created_at: string
}

export type AuditLogFilters = {
  action?: string
  actorType?: string
  success?: 'all' | 'true' | 'false'
  dateFrom?: string
  dateTo?: string
  page?: number
  pageSize?: number
}

export const profileQueryKeys = {
  all: ['profile'] as const,
  current: () => [...profileQueryKeys.all, 'current'] as const,
  sessions: () => [...profileQueryKeys.all, 'sessions'] as const,
  accessToken: () => [...profileQueryKeys.all, 'access-token'] as const,
}

export const auditQueryKeys = {
  all: ['audit-logs'] as const,
  list: (filters: AuditLogFilters) => [...auditQueryKeys.all, filters] as const,
}

export async function getProfile() {
  const response = await apiFetch<{ admin: AdminResponse }>('/api/v1/profile')
  return { admin: normalizeAdmin(response.admin) }
}

export async function updateProfileAccount(input: { username: string; nickname?: string; avatar?: string }) {
  const response = await apiFetch<{ admin: AdminResponse }>('/api/v1/profile/account', {
    method: 'PUT',
    body: input,
  })
  return { admin: normalizeAdmin(response.admin) }
}

export async function updateProfilePassword(input: { currentPassword: string; newPassword: string }) {
  const response = await apiFetch<{ admin: AdminResponse }>('/api/v1/profile/password', {
    method: 'PUT',
    body: {
      current_password: input.currentPassword,
      new_password: input.newPassword,
    },
  })
  return { admin: normalizeAdmin(response.admin) }
}

export async function listProfileSessions() {
  return apiFetch<{ items: AdminSessionItem[]; meta?: { count?: number } }>('/api/v1/profile/sessions')
}

export async function deleteProfileSession(sessionId: string) {
  return apiFetch<void>(`/api/v1/profile/sessions/${sessionId}`, { method: 'DELETE' })
}

export async function deleteOtherProfileSessions() {
  return apiFetch<void>('/api/v1/profile/sessions', { method: 'DELETE' })
}

export async function getProfileAccessToken() {
  return apiFetch<{ access_token: AdminAccessToken | null }>('/api/v1/profile/access-token')
}

export async function createProfileAccessToken() {
  return apiFetch<{ access_token: AdminAccessToken }>('/api/v1/profile/access-token', { method: 'POST' })
}

export async function setProfileAccessTokenEnabled(enabled: boolean) {
  return apiFetch<{ access_token: AdminAccessToken }>('/api/v1/profile/access-token/enabled', {
    method: 'PUT',
    body: { enabled },
  })
}

export async function deleteProfileAccessToken() {
  return apiFetch<void>('/api/v1/profile/access-token', { method: 'DELETE' })
}

export async function setupProfileTOTP() {
  const response = await apiFetch<{ secret: string; otpauth_url: string; admin: AdminResponse }>(
    '/api/v1/profile/totp/setup',
    { method: 'POST' },
  )
  return { ...response, admin: normalizeAdmin(response.admin) satisfies AdminRecord }
}

export async function enableProfileTOTP(code: string) {
  const response = await apiFetch<{ admin: AdminResponse }>('/api/v1/profile/totp/enable', {
    method: 'POST',
    body: { code },
  })
  return { admin: normalizeAdmin(response.admin) }
}

export async function disableProfileTOTP(input: { currentPassword: string; code: string }) {
  const response = await apiFetch<{ admin: AdminResponse }>('/api/v1/profile/totp', {
    method: 'DELETE',
    body: {
      current_password: input.currentPassword,
      code: input.code,
    },
  })
  return { admin: normalizeAdmin(response.admin) }
}

export async function listAuditLogs(filters: AuditLogFilters = {}) {
  const params = new URLSearchParams()
  if (filters.action) params.set('action', filters.action)
  if (filters.actorType && filters.actorType !== 'all') params.set('actor_type', filters.actorType)
  if (filters.success && filters.success !== 'all') params.set('success', filters.success)
  if (filters.dateFrom) params.set('date_from', filters.dateFrom)
  if (filters.dateTo) params.set('date_to', filters.dateTo)
  if (filters.page) params.set('page', String(filters.page))
  if (filters.pageSize) params.set('page_size', String(filters.pageSize))

  const query = params.toString()
  return apiFetch<{
    items: AuditLogItem[]
    meta: { count: number; total: number; page: number; page_size: number }
  }>(`/api/v1/audit-logs${query ? `?${query}` : ''}`)
}
