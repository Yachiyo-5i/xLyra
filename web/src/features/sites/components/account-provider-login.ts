import type { AccountLoginStart, SiteAccountCapabilities } from '@/features/sites/api/sites'

export type AccountLoginKind = SiteAccountCapabilities['login_kind']
export type AccountCopyPrefix = 'grokAccounts'

export function accountCopyPrefix(_siteType: string | null | undefined): AccountCopyPrefix {
  return 'grokAccounts'
}

export function loginOpenURL(login: Pick<AccountLoginStart, 'kind' | 'verification_uri' | 'verification_uri_complete' | 'auth_url'>): string | undefined {
  if (login.kind === 'custom_poll') return login.auth_url
  return login.verification_uri_complete || login.verification_uri
}

export function loginPollIntervalMs(kind: AccountLoginKind, interval?: number | null): number {
  const fallback = kind === 'custom_poll' ? 3 : 5
  return Math.max(2, interval || fallback) * 1000
}

export function loginPollOutcome(result: { status?: string | null } | null | undefined): 'complete' | 'continue' | 'error' {
  switch (result?.status) {
    case 'complete':
      return 'complete'
    case 'pending':
    case 'slow_down':
      return 'continue'
    default:
      return 'error'
  }
}
