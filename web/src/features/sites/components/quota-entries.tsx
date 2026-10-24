import { Fragment } from 'react'
import { useTranslation } from 'react-i18next'
import type { SiteQuotaProbeEntry } from '@/features/sites/api/sites'
import { formatQuotaEntryAmount, quotaEntryKind } from '@/features/sites/lib/site-utils'

export type QuotaEntriesSummary = {
  remaining: number
  limit: number
  unit?: string
}

function formatQuotaDate(value?: string) {
  if (!value) return ''
  const date = new Date(value.includes('T') ? value : value.replace(' ', 'T') + '+08:00')
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString()
}

function QuotaMeter({
  label,
  remaining,
  limit,
  unit,
}: {
  label: string
  remaining: number
  limit: number
  unit?: string
}) {
  const ratio = limit > 0 ? Math.min(Math.max(remaining / limit, 0), 1) : 0
  const low = ratio < 0.1
  const entry = { unit, remaining, limit }
  return (
    <div className="space-y-1">
      <div className="flex items-baseline justify-between text-footnote">
        <span className="text-label-2">{label}</span>
        <span className={`tabular-nums font-medium ${low ? 'text-danger' : 'text-label'}`}>
          {formatQuotaEntryAmount(entry, 'remaining')} / {formatQuotaEntryAmount(entry, 'limit')}
        </span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-fill-1">
        <div className={`h-full rounded-full ${low ? 'bg-danger' : 'bg-success'}`} style={{ width: `${ratio * 100}%` }} />
      </div>
    </div>
  )
}

function balanceAmount(entry: SiteQuotaProbeEntry) {
  if (entry.unlimited) return '∞'
  const remaining = formatQuotaEntryAmount(entry, 'remaining')
  const limit = formatQuotaEntryAmount(entry, 'limit')
  if (remaining && limit) return `${remaining} / ${limit}`
  return remaining || limit || '-'
}

export function QuotaEntries({
  entries,
  summary,
  showUnknown = false,
}: {
  entries: SiteQuotaProbeEntry[]
  summary?: QuotaEntriesSummary
  showUnknown?: boolean
}) {
  const { t } = useTranslation('sites')
  const windows = entries.filter((entry) => quotaEntryKind(entry) === 'window')
  const packages = entries.filter((entry) => quotaEntryKind(entry) === 'package')
  const balances = entries.filter((entry) => quotaEntryKind(entry) === 'balance')
  if (!summary && windows.length === 0 && packages.length === 0 && balances.length === 0) {
    return showUnknown ? <p className="text-footnote text-label-2">{t('quotaEntries.unknown')}</p> : null
  }
  const summaryUnit = summary?.unit?.trim() || 'credits'
  return (
    <div className="space-y-2">
      {summary ? (
        <QuotaMeter
          label={summaryUnit.toLowerCase() === 'credits' ? t('quotaEntries.credits') : t('quotaEntries.balance')}
          remaining={summary.remaining}
          limit={summary.limit}
          unit={summaryUnit}
        />
      ) : null}
      {windows.map((entry, index) => {
        const limit = typeof entry.limit === 'number' ? entry.limit : entry.unit?.trim().toLowerCase() === 'percent' ? 100 : 0
        const remaining = entry.remaining ?? (typeof entry.used === 'number' ? Math.max(limit - entry.used, 0) : 0)
        return (
          <Fragment key={`window:${entry.name ?? entry.label}:${index}`}>
            <QuotaMeter
              label={entry.name || t('quotaEntries.allowance')}
              remaining={remaining}
              limit={limit}
              unit={entry.unit}
            />
            {entry.reset_at ? (
              <p className="text-right text-footnote text-label-2">{t('quotaEntries.resetsOn', { date: formatQuotaDate(entry.reset_at) })}</p>
            ) : null}
          </Fragment>
        )
      })}
      {balances.map((entry, index) => (
        <div key={`balance:${entry.name ?? entry.label}:${index}`} className="flex items-baseline justify-between text-footnote">
          <span className="text-label-2">{entry.name || t('quotaEntries.balance')}</span>
          <span className="tabular-nums font-medium text-label">{balanceAmount(entry)}</span>
        </div>
      ))}
      {packages.length ? (
        <ul className="space-y-0.5 text-footnote text-label-2">
          {packages.map((entry, index) => {
            const date = entry.reset_at
              ? t('quotaEntries.resetsOn', { date: formatQuotaDate(entry.reset_at) })
              : entry.expires_at
                ? t('quotaEntries.expiresOn', { date: formatQuotaDate(entry.expires_at) })
                : ''
            return (
              <li key={`package:${entry.name ?? ''}:${entry.reset_at ?? ''}:${index}`} className="flex items-baseline justify-between gap-3">
                <span className="min-w-0 truncate">{entry.name}</span>
                <span className="shrink-0 tabular-nums">
                  {formatQuotaEntryAmount(entry, 'remaining')} / {formatQuotaEntryAmount(entry, 'limit')}
                  {date ? <>{' · '}{date}</> : null}
                </span>
              </li>
            )
          })}
        </ul>
      ) : null}
    </div>
  )
}
