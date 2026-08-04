import { apiFetch, apiFetchBlob, type DownloadTicket } from '@/lib/http'
import type { Site, SiteModel } from '@/features/sites/api/sites'
import type { ImportResult } from '@/features/oauth/lib/types'

export type OAuthProvider = 'codex' | 'antigravity' | 'claude_code'

export type OAuthConnectionListItem = {
  id: string
  provider: string
  status: string
  account_id?: string | null
  email?: string | null
  site_id?: string | null
  site?: Site
  expires_at?: string | null
  last_refresh_at?: string | null
  last_sync_at?: string | null
  last_error?: string | null
  last_error_at?: string | null
  refreshable?: boolean | null
  token_mode?: string | null
  refresh_warning?: string | null
  created_at: string
  updated_at: string
  meta?: Record<string, unknown>
}

type OAuthQuotaWindow = {
  used_percent?: number | null
  remaining_percent?: number | null
  reset_at?: number | null
  limit_window_seconds?: number | null
}

type OAuthModelQuota = {
  name?: string | null
  display_name?: string | null
  remaining_percent?: number | null
  used_percent?: number | null
  reset_at?: number | string | null
  reset_time?: string | null
}

export type OAuthConnectionDetail = OAuthConnectionListItem & {
  plan_type?: string | null
  quota?: {
    type?: string | null
    plan_type?: string | null
    five_hour?: OAuthQuotaWindow
    weekly?: OAuthQuotaWindow
    models?: OAuthModelQuota[]
    reset_credits?: {
      available_count?: number | null
    }
  }
  claims?: Record<string, unknown>
  models?: OAuthConnectionModel[]
  reset_credits?: OAuthResetCreditsList | null
}

export type OAuthConnectionModel = {
  id?: string | null
  name?: string | null
  upstream_model_name?: string | null
  display_name?: string | null
  display?: string | null
  site_model_id?: string | null
  status?: string | null
  enabled?: boolean | null
  quota?: OAuthModelQuota | null
  capabilities?: Record<string, unknown>
}

export type StartCodexOAuthInput = {
  siteID?: string | null
  siteName?: string
  siteSlug?: string
  proxyId?: string | null
  redirectURL?: string
  failureRedirectURL?: string
}

export type StartCodexOAuthResponse = {
  provider: OAuthProvider
  session_id: string
  state: string
  authorize_url: string
  callback_url: string
  callback_port?: number
  relay_target_url?: string
  expires_at: string
}

export type CompleteOAuthCallbackResponse = {
  ok: boolean
  provider?: OAuthProvider | string
  status?: 'success' | 'partial' | 'error' | string
  connection_id?: string | null
  site_id?: string | null
  message?: string | null
  redirect_url?: string | null
}

export type OAuthResetCredit = {
  id: string
  status?: string | null
  reset_type?: string | null
  title?: string | null
  description?: string | null
  granted_at?: string | null
  expires_at?: string | null
  redeem_started_at?: string | null
  redeemed_at?: string | null
  profile_user_id?: string | null
  profile_image_url?: string | null
}

export type OAuthResetCreditsList = {
  credits: OAuthResetCredit[]
  available_count: number
  total_earned_count: number
}

export const oauthQueryKeys = {
  all: ['oauth'] as const,
  connections: () => [...oauthQueryKeys.all, 'connections'] as const,
  detail: (connectionId: string) => [...oauthQueryKeys.all, 'connections', connectionId] as const,
  resetCredits: (connectionId: string) => [...oauthQueryKeys.all, 'connections', connectionId, 'reset-credits'] as const,
}

export async function listOAuthConnections() {
  return apiFetch<{ items: OAuthConnectionListItem[]; meta?: { count?: number } }>('/api/v1/oauth/connections')
}

export async function getOAuthConnection(connectionId: string) {
  return apiFetch<{ connection: OAuthConnectionDetail }>(`/api/v1/oauth/connections/${connectionId}`)
}

export async function refreshOAuthConnection(connectionId: string) {
  return apiFetch<{
    connection: OAuthConnectionDetail
    refresh?: {
      ok: boolean
      message?: string | null
      site?: Site
    } | null
  }>(`/api/v1/oauth/connections/${connectionId}/refresh`, {
    method: 'POST',
  })
}

export async function listOAuthConnectionResetCredits(connectionId: string) {
  return apiFetch<OAuthResetCreditsList>(`/api/v1/oauth/connections/${connectionId}/reset-credits`)
}

export async function consumeOAuthConnectionResetCredit(connectionId: string, idempotencyKey: string, creditId?: string) {
  return apiFetch<{
    result: {
      outcome: 'reset' | 'alreadyRedeemed' | 'nothingToReset' | 'noCredit' | string
    }
    connection: OAuthConnectionDetail
  }>(`/api/v1/oauth/connections/${connectionId}/reset-credit/consume`, {
    method: 'POST',
    body: {
      idempotency_key: idempotencyKey,
      ...(creditId ? { credit_id: creditId } : {}),
    },
  })
}

export async function exportOAuthConnection(connectionId: string) {
  const result = await apiFetch<{ download: DownloadTicket }>(`/api/v1/oauth/connections/${connectionId}/export`, {
    method: 'POST',
  })
  return apiFetchBlob(result.download.url)
}

export async function updateOAuthConnectionModelStatus(
  connectionId: string,
  input: { siteModelId?: string | null; model?: string | null; enabled: boolean },
) {
  return apiFetch<{ model: SiteModel; connection?: OAuthConnectionDetail }>(`/api/v1/oauth/connections/${connectionId}/models`, {
    method: 'PUT',
    body: {
      site_model_id: input.siteModelId,
      model: input.model,
      enabled: input.enabled,
    },
  })
}

export async function updateOAuthConnectionModelsStatus(
  connectionId: string,
  input: { siteModelIds: string[]; enabled: boolean },
) {
  return apiFetch<{ items: SiteModel[]; meta?: { count?: number }; connection?: OAuthConnectionDetail }>(`/api/v1/oauth/connections/${connectionId}/models/status`, {
    method: 'PUT',
    body: {
      site_model_ids: input.siteModelIds,
      enabled: input.enabled,
    },
  })
}

export async function startCodexOAuth(input: StartCodexOAuthInput) {
  return apiFetch<StartCodexOAuthResponse>('/api/v1/oauth/providers/codex/authorize', {
    method: 'POST',
    body: {
      redirect_url: input.redirectURL,
      failure_redirect_url: input.failureRedirectURL,
      site: {
        site_id: input.siteID,
        name: input.siteName,
        slug: input.siteSlug,
        proxy_id: input.proxyId,
        enabled: true,
      },
    },
  })
}

export async function startAntigravityOAuth(input: StartCodexOAuthInput) {
  return apiFetch<StartCodexOAuthResponse>('/api/v1/oauth/providers/antigravity/authorize', {
    method: 'POST',
    body: {
      redirect_url: input.redirectURL,
      failure_redirect_url: input.failureRedirectURL,
      site: {
        site_id: input.siteID,
        name: input.siteName,
        slug: input.siteSlug,
        proxy_id: input.proxyId,
        enabled: true,
      },
    },
  })
}

export async function startClaudeCodeOAuth(input: StartCodexOAuthInput) {
  return apiFetch<StartCodexOAuthResponse>('/api/v1/oauth/providers/claude_code/authorize', {
    method: 'POST',
    body: {
      redirect_url: input.redirectURL,
      failure_redirect_url: input.failureRedirectURL,
      site: {
        site_id: input.siteID,
        name: input.siteName,
        slug: input.siteSlug,
        proxy_id: input.proxyId,
        enabled: true,
      },
    },
  })
}

export async function completeOAuthFromCallback(provider: OAuthProvider, callbackURL: string, proxyId?: string | null) {
  return apiFetch<CompleteOAuthCallbackResponse>(`/api/v1/oauth/providers/${provider}/callback-url`, {
    method: 'POST',
    body: {
      callback_url: callbackURL,
      proxy_id: proxyId,
    },
  })
}

export async function completeClaudeCodeOAuth(sessionId: string, authorizationResult: string, proxyId?: string | null) {
  return apiFetch<CompleteOAuthCallbackResponse>('/api/v1/oauth/providers/claude_code/complete', {
    method: 'POST',
    body: {
      session_id: sessionId,
      authorization_result: authorizationResult,
      proxy_id: proxyId,
    },
  })
}

export type ImportOAuthAccountsInput =
  | { mode: 'files'; files: File[]; proxyId?: string | null }
  | { mode: 'json'; jsonText: string; proxyId?: string | null }

export async function importOAuthAccounts(input: ImportOAuthAccountsInput) {
  const proxyQuery = input.proxyId ? `?proxy_id=${encodeURIComponent(input.proxyId)}` : ''
  if (input.mode === 'json') {
    return apiFetch<ImportResult>(`/api/v1/oauth/import${proxyQuery}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: input.jsonText.trim(),
    })
  }

  const form = new FormData()
  for (const file of input.files) {
    form.append('files', file)
  }
  if (input.proxyId) {
    form.set('proxy_id', input.proxyId)
  }
  return apiFetch<ImportResult>('/api/v1/oauth/import', {
    method: 'POST',
    body: form,
  })
}
