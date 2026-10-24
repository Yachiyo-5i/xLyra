import type { AccountLoginStart, AccountUIText, SiteAccountCapabilities, SiteTypeInfo } from '@/features/sites/api/sites'
import { resolveText } from '@/features/settings/lib/localized-text'
import { isBuiltinAccountSite } from '@/features/sites/lib/site-utils'

export type AccountLoginKind = SiteAccountCapabilities['login_kind']
export type AccountCopyPrefix = 'grokAccounts' | 'accounts'
export type AccountOperation = 'add' | 'reauth' | 'refresh' | 'delete' | 'toggle' | 'models' | 'import'

/**
 * Which account actions to show.
 * A missing operation list keeps every action (import still needs supports_import).
 * An explicit list is an allowlist. `refresh` covers the row refresh and refresh-all.
 */
export function accountOperationFlags(
  operations: readonly string[] | null | undefined,
  supportsImport: boolean,
): Record<AccountOperation, boolean> {
  const listed = Array.isArray(operations) ? operations : null
  const allow = (operation: AccountOperation) => listed == null || listed.includes(operation)
  return {
    add: allow('add'),
    reauth: allow('reauth'),
    refresh: allow('refresh'),
    delete: allow('delete'),
    toggle: allow('toggle'),
    models: allow('models'),
    import: allow('import') && supportsImport,
  }
}

export function accountUIText(
  texts: Record<string, AccountUIText> | null | undefined,
  key: string,
  language: string,
): string | undefined {
  if (!texts || typeof texts !== 'object') return undefined
  const value = texts[key]
  if (typeof value === 'string' || (value && typeof value === 'object')) return resolveText(value, language)
  return undefined
}

export function fillAccountText(template: string, vars?: Record<string, string | number>) {
  if (!vars) return template
  return template.replace(/\{\{\s*([A-Za-z0-9_]+)\s*\}\}/g, (_, name: string) => (
    vars[name] == null ? '' : String(vars[name])
  ))
}

/** Built-in Grok keeps its device hint. Plugin pools use generic copy, or `texts.login_hint`. */
export function accountCreateHint(
  siteType: string,
  info: SiteTypeInfo | null | undefined,
  language: string,
): { key: 'form.grokDeviceHint' | 'form.accountLoginHint'; override?: string } {
  if (isBuiltinAccountSite(siteType)) {
    return { key: 'form.grokDeviceHint' }
  }
  const override = accountUIText(info?.accounts?.ui?.texts, 'login_hint', language)
  return { key: 'form.accountLoginHint', override }
}

/** Built-in account sites use the Grok copy. Plugin pools pass `accounts` themselves. */
export function accountCopyPrefix(siteType: string | null | undefined): AccountCopyPrefix {
  void siteType
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
