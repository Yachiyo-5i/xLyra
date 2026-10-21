import type { ReactNode } from 'react'
import { Badge } from '@/components/ui/badge'
import { resolveText } from '@/features/settings/lib/localized-text'
import { QuotaEntries } from '@/features/sites/components/quota-entries'
import { accountQuotaEntries, legacyGrokQuotaEntries } from '@/features/sites/lib/site-utils'
import type { AccountRowPill, AccountUIText, SiteAccount } from '@/features/sites/api/sites'

export type AccountRowExtras = {
  pills?: ReactNode
  detail?: ReactNode
}

export function grokRowView(account: SiteAccount): AccountRowExtras {
  const tier = 'tier' in account ? account.tier : undefined
  const quotaEntries = accountQuotaEntries(account.entries, legacyGrokQuotaEntries('quota_known' in account ? account : {}))
  return {
    pills: tier ? <Badge variant="neutral">{tier}</Badge> : undefined,
    detail: quotaEntries.length ? <QuotaEntries entries={quotaEntries} /> : undefined,
  }
}

function plainFieldValue(value: unknown): string | undefined {
  if (typeof value === 'string') return value.trim() || undefined
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  if (typeof value === 'boolean') return value ? 'true' : 'false'
  return undefined
}

function readAccountField(account: SiteAccount, field: string): unknown {
  const bag = account.fields
  if (bag && typeof bag === 'object' && Object.prototype.hasOwnProperty.call(bag, field)) return bag[field]
  const direct = (account as unknown as Record<string, unknown>)[field]
  return plainFieldValue(direct) === undefined ? undefined : direct
}

function pillLabel(labels: Record<string, AccountUIText> | undefined, raw: string, language: string) {
  const entry = labels?.[raw]
  if (typeof entry === 'string' || (entry && typeof entry === 'object')) return resolveText(entry, language)
  return undefined
}

/** Plugin account row: pills from `ui.row_pills`, plus remaining/limit and an unsynced hint when quota is unknown. */
export function pluginRowView(
  account: SiteAccount,
  pills: readonly AccountRowPill[] | null | undefined,
  language: string,
): AccountRowExtras {
  const specs = Array.isArray(pills) ? pills : []
  const rendered = specs.flatMap((pill, index) => {
    const field = pill?.field?.trim()
    if (!field) return []
    const raw = plainFieldValue(readAccountField(account, field))
    if (!raw) return []
    const text = pillLabel(pill.labels, raw, language) || raw
    return [<Badge key={`${field}-${index}`} variant="neutral">{text}</Badge>]
  })
  const quotaEntries = accountQuotaEntries(account.entries, [])
  const summary = account.quota_limit != null && account.remaining_quota != null
    ? { remaining: account.remaining_quota, limit: account.quota_limit, unit: account.quota_unit || 'credits' }
    : undefined
  return {
    pills: rendered.length ? <>{rendered}</> : undefined,
    detail: <QuotaEntries entries={quotaEntries} summary={summary} showUnknown />,
  }
}
