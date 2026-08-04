import type { TFunction } from 'i18next'
import { localeFromLanguage } from '@/lib/locale'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import type { OAuthConnectionListItem, OAuthConnectionModel, OAuthProvider } from '@/features/oauth/api/oauth'
import type { OAuthCreateState, OAuthQuotaWindowLike, OAuthSourceItem } from '@/features/oauth/lib/types'

export function getOAuthSources(t?: TFunction): OAuthSourceItem[] {
  return [
    {
      value: 'codex',
      label: 'Codex',
      subtitle: t ? t('sources.codexSubtitle') : '通过 OAuth 接入 OpenAI Codex 账号',
      iconPath: '/oauth-icons/codex.svg',
      enabled: true,
    },
    {
      value: 'antigravity',
      label: 'Antigravity',
      subtitle: t ? t('sources.antigravitySubtitle') : '通过 OAuth 接入 Antigravity 账号',
      iconPath: '/oauth-icons/antigravity.png',
      enabled: true,
    },
    {
      value: 'claude_code',
      label: 'Claude Code',
      subtitle: t ? t('sources.claudeCodeSubtitle') : '通过 OAuth 接入 Claude Code 订阅账号',
      iconPath: '/oauth-icons/claudecode.png',
      enabled: true,
    },
  ]
}

export const DEFAULT_CREATE_STATE: OAuthCreateState = {
  source: 'codex',
  authorize: null,
  callbackInput: '',
}

export const PRIMARY_ANTIGRAVITY_QUOTA_MODEL_KEYS = [
  'geminiproagent',
  'claudeopus46',
] as const

const PRIMARY_ANTIGRAVITY_QUOTA_MODEL_ALIASES: Record<(typeof PRIMARY_ANTIGRAVITY_QUOTA_MODEL_KEYS)[number], string[]> = {
  geminiproagent: [
    'gemini-pro-agent',
    'gemini pro agent',
  ],
  claudeopus46: [
    'claude-opus-4.6',
    'claude opus 4.6',
    'claude-opus-4-6',
    'claude opus 4 6',
    'claude-opus-4.6-thinking',
    'claude opus 4.6 thinking',
    'claude-opus-4-6-thinking',
    'claude opus 4 6 thinking',
  ],
}

const OAUTH_UPDATED_AT_MODE_KEY = 'xlyra:oauth:updated-at-display-mode'

export type OAuthUpdatedAtDisplayMode = 'relative' | 'absolute'

export function readOAuthUpdatedAtMode(): OAuthUpdatedAtDisplayMode {
  try {
    return window.localStorage.getItem(OAUTH_UPDATED_AT_MODE_KEY) === 'absolute' ? 'absolute' : 'relative'
  } catch {
    return 'relative'
  }
}

export function saveOAuthUpdatedAtMode(mode: OAuthUpdatedAtDisplayMode) {
  try {
    window.localStorage.setItem(OAUTH_UPDATED_AT_MODE_KEY, mode)
  } catch {
    // ignore
  }
}

export function sortOAuthConnectionsForDisplay(items: OAuthConnectionListItem[]) {
  return [...items].sort((a, b) => {
    const aConnected = oauthConnectionEffectiveStatus(a) === 'connected'
    const bConnected = oauthConnectionEffectiveStatus(b) === 'connected'
    if (aConnected !== bConnected) return aConnected ? -1 : 1

    const aEnabled = a.site?.enabled ?? true
    const bEnabled = b.site?.enabled ?? true
    if (aEnabled !== bEnabled) return aEnabled ? -1 : 1

    const priorityDelta = (b.site?.routing_priority ?? 0) - (a.site?.routing_priority ?? 0)
    if (priorityDelta !== 0) return priorityDelta

    const aTime = new Date(a.created_at).getTime()
    const bTime = new Date(b.created_at).getTime()

    if (!Number.isNaN(aTime) && !Number.isNaN(bTime) && aTime !== bTime) return aTime - bTime

    return [
      providerLabel(a.provider).localeCompare(providerLabel(b.provider), 'zh-CN'),
      (a.email ?? '').localeCompare(b.email ?? '', 'zh-CN'),
      a.id.localeCompare(b.id, 'zh-CN'),
    ].find((result) => result !== 0) ?? 0
  })
}

export function providerLabel(provider: string) {
  if (provider === 'codex') return 'Codex'
  if (provider === 'antigravity') return 'Antigravity'
  if (provider === 'claude_code') return 'Claude Code'
  return provider || '-'
}

export function connectionStatusLabel(status: string, t?: TFunction) {
  switch (status) {
    case 'connected':
      return t ? t('status.connected') : '已连接'
    case 'pending_sync':
      return t ? t('status.pendingSync') : '检测中'
    case 'expired':
      return t ? t('status.expired') : '已过期'
    case 'error':
      return t ? t('status.error') : '异常'
    case 'reconnect_required':
      return t ? t('status.reconnectRequired') : '需要重新授权'
    default:
      return status || '-'
  }
}

export function oauthConnectionEffectiveStatus(item: OAuthConnectionListItem) {
  if (oauthConnectionQuotaAuthError(item)) {
    return 'reconnect_required'
  }
  return item.status
}

export function oauthConnectionEnableBlockedByStatus(item: OAuthConnectionListItem) {
  const status = oauthConnectionEffectiveStatus(item)
  return status !== 'connected' && status !== 'pending_sync'
}

export function oauthConnectionErrorMessage(item: OAuthConnectionListItem) {
  return item.last_error || oauthConnectionQuotaAuthError(item)
}

export function planBadgeVariant(planType: string): 'neutral' | 'success' | 'info' | 'accent' | 'warning' {
  switch (planType.trim().toLowerCase()) {
    case 'free':
      return 'neutral'
    case 'plus':
      return 'info'
    case 'pro':
      return 'accent'
    case 'team':
    case 'enterprise':
      return 'success'
    default:
      return 'warning'
  }
}

export function oauthConnectionSearchText(item: OAuthConnectionListItem) {
  return [
    item.provider,
    item.email,
    item.account_id,
    item.site?.name,
    item.site?.slug,
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase()
}

export function oauthModelEnabled(model: OAuthConnectionModel) {
  if (typeof model.enabled === 'boolean') return model.enabled
  return model.status === 'active'
}

export function oauthEnabledModelCount(models: OAuthConnectionModel[] | undefined | null) {
  return (models ?? []).filter(oauthModelEnabled).length
}

export function oauthRefreshWarning(item: OAuthConnectionListItem, t?: TFunction) {
  const meta = item.meta ?? {}
  const metaRefreshable = typeof meta.refreshable === 'boolean' ? meta.refreshable : undefined
  const metaTokenMode = typeof meta.token_mode === 'string' ? meta.token_mode : undefined
  const metaWarning = typeof meta.refresh_warning === 'string' ? meta.refresh_warning : undefined
  const refreshable = typeof item.refreshable === 'boolean' ? item.refreshable : metaRefreshable
  const tokenMode = item.token_mode ?? metaTokenMode
  const warning = item.refresh_warning ?? metaWarning

  if (refreshable === false || tokenMode === 'access_token_only') {
    return warning || (t ? t('card.nonRefreshableDescription') : '不可自动续期，access token 过期后需要重新导入或重新登录。')
  }
  return ''
}

function oauthConnectionQuotaAuthError(item: OAuthConnectionListItem) {
  const quota = recordFromUnknown(item.meta?.quota)
  if (!quota) return ''
  if (quota.available === true) return ''
  const message = stringFromUnknown(quota.error) || stringFromUnknown(quota.message) || stringFromUnknown(quota.detail)
  return message && oauthAuthFailureMessage(message) ? message : ''
}

function oauthAuthFailureMessage(message: string) {
  const lower = message.trim().toLowerCase()
  if (!lower) return false
  if (lower.includes('token_invalidated') || lower.includes('refresh_token_reused') || lower.includes('invalid_grant')) return true
  return lower
    .split(/[^0-9]+/)
    .some((token) => token.length === 3 && token.startsWith('4'))
}

function recordFromUnknown(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null
  return value as Record<string, unknown>
}

function stringFromUnknown(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

export function generateOAuthSiteName(source: OAuthProvider) {
  return providerLabel(source)
}

export function generateOAuthSiteSlug(source: OAuthProvider) {
  return `${source}-${Math.random().toString(36).slice(2, 8)}`
}

export function oauthPageURL() {
  if (typeof window === 'undefined') return '/oauth'
  return `${window.location.origin}/oauth`
}

export function parseOAuthCallbackInput(raw: string, t?: TFunction):
  | { kind: 'callback_url'; callbackURL: string }
  | { kind: 'authorization_result'; authorizationResult: string }
  | { kind: 'redirect_result'; connectionId: string | null; siteId: string | null; message: string; status: 'success' | 'partial' }
  | { kind: 'callback_error'; message: string } {
  const trimmed = raw.trim()
  if (!trimmed) {
    throw new Error(t ? t('callback.emptyUrl') : '请先粘贴登录后的结果 URL')
  }

  if (/^[^#\s]+#[^#\s]+$/.test(trimmed) && !/^https?:\/\//i.test(trimmed)) {
    return {
      kind: 'authorization_result',
      authorizationResult: trimmed,
    }
  }

  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    throw new Error(t ? t('callback.invalidUrl') : 'URL 格式不正确')
  }

  const status = parsed.searchParams.get('status')
  if (status === 'success' || status === 'partial') {
    return {
      kind: 'redirect_result',
      connectionId: parsed.searchParams.get('connection_id'),
      siteId: parsed.searchParams.get('site_id'),
      message: parsed.searchParams.get('message') ?? '',
      status,
    }
  }

  if (status === 'error') {
    return {
      kind: 'callback_error',
      message: parsed.searchParams.get('message') ?? (t ? t('callback.authFailed') : '认证失败'),
    }
  }

  const state = parsed.searchParams.get('state')
  const code = parsed.searchParams.get('code')
  const error = parsed.searchParams.get('error')
  if (state && (code || error)) {
    return {
      kind: 'callback_url',
      callbackURL: parsed.toString(),
    }
  }

  throw new Error(t ? t('callback.noParams') : '未识别到可用的 OAuth 结果参数')
}

export async function copyText(value: string, successMessage: string) {
  await copyToClipboard(value, successMessage)
}

function formatDateTime(value?: string | null, language?: string) {
  if (!value) return '-'
  try {
    return new Intl.DateTimeFormat(localeFromLanguage(language), {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }).format(new Date(value))
  } catch {
    return value
  }
}

export function formatOAuthUpdatedAt(
  value: string | null | undefined,
  mode: OAuthUpdatedAtDisplayMode,
  now: number,
  language?: string,
  t?: TFunction,
) {
  if (!value) return '-'
  return mode === 'absolute' ? formatDateTime(value, language) : formatRelativeDateTime(value, now, language, t)
}

function formatRelativeDateTime(value: string, now: number, language?: string, t?: TFunction) {
  const time = new Date(value).getTime()
  if (Number.isNaN(time)) return value
  const diffSeconds = Math.round((time - now) / 1000)
  const abs = Math.abs(diffSeconds)
  if (abs < 60) return t ? t('utils.justNow') : 'just now'
  const relativeFormatter = new Intl.RelativeTimeFormat(localeFromLanguage(language), { numeric: 'auto' })
  if (abs < 3600) return relativeFormatter.format(Math.round(diffSeconds / 60), 'minute')
  if (abs < 86400) return relativeFormatter.format(Math.round(diffSeconds / 3600), 'hour')
  if (abs < 2592000) return relativeFormatter.format(Math.round(diffSeconds / 86400), 'day')
  if (abs < 31536000) return relativeFormatter.format(Math.round(diffSeconds / 2592000), 'month')
  return relativeFormatter.format(Math.round(diffSeconds / 31536000), 'year')
}

export function formatUsage(usage?: { total_tokens?: number; estimated_cost?: number; currency?: string } | null) {
  if (!usage) return '-'
  const tokens = typeof usage.total_tokens === 'number' ? formatTokenCount(usage.total_tokens) : null
  const cost = typeof usage.estimated_cost === 'number' ? formatCost(usage.estimated_cost, usage.currency) : null
  if (!tokens && !cost) return '-'
  if (tokens && cost) return `${tokens} / ${cost}`
  return tokens ?? cost ?? '-'
}

function formatTokenCount(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return String(value)
}

function formatCost(value: number, currency?: string) {
  const unit = (currency ?? 'USD').toUpperCase()
  return `$${value.toFixed(2)} ${unit}`
}

export function formatPercent(value?: number | null, language?: string) {
  if (typeof value !== 'number') return '-'
  return `${new Intl.NumberFormat(localeFromLanguage(language), {
    maximumFractionDigits: 2,
  }).format(value)}%`
}

export function formatQuotaResetTime(value?: number | null, language?: string) {
  if (typeof value !== 'number') return '-'

  const date = new Date(value * 1000)
  if (Number.isNaN(date.getTime())) return '-'

  const now = new Date()
  const sameDay =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate()

  return new Intl.DateTimeFormat(localeFromLanguage(language), sameDay
    ? {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      }
    : {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      }).format(date)
}


export function parseQuotaResetAt(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim()) {
    const numeric = Number(value)
    if (Number.isFinite(numeric)) return numeric
    const timestamp = Date.parse(value)
    return Number.isFinite(timestamp) ? Math.floor(timestamp / 1000) : null
  }
  return null
}

export function clampPercent(value?: number | null) {
  if (typeof value !== 'number' || Number.isNaN(value)) return null
  return Math.min(100, Math.max(0, value))
}

export function isAntigravityProvider(provider: string) {
  return provider.trim().toLowerCase() === 'antigravity'
}

export function getPrimaryAntigravityQuotaModelKey(item: OAuthQuotaWindowLike) {
  const labels = [
    item.display_name,
    item.name,
  ].map(canonicalQuotaModelName)

  return PRIMARY_ANTIGRAVITY_QUOTA_MODEL_KEYS.find((key) => {
    const aliases = PRIMARY_ANTIGRAVITY_QUOTA_MODEL_ALIASES[key].map(canonicalQuotaModelName)
    return labels.some((label) => aliases.includes(label))
  }) ?? null
}

function canonicalQuotaModelName(value?: string | null) {
  return value
    ?.trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '')
    ?? ''
}
