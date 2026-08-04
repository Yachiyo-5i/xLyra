import { useTranslation } from 'react-i18next'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import { formatCompactDollarQuota, formatDollarQuota } from '@/features/api-keys/lib/api-key-utils'

type QuotaRow = {
  label: string
  limit: number | null
  used: number
  available: number | null
  unlimited: boolean
}

export function APIKeyQuotaCell({ apiKey, truncateValues = true, compactValues = false }: { apiKey: DownstreamAPIKey; truncateValues?: boolean; compactValues?: boolean }) {
  const { t } = useTranslation('api-keys')
  const rows: QuotaRow[] = [{
    label: t('quota.total'),
    limit: apiKey.quota_limit ?? null,
    used: apiKey.quota_used,
    available: apiKey.quota_available ?? null,
    unlimited: apiKey.quota_unlimited || apiKey.quota_limit == null,
  }]

  if (!apiKey.quota_daily_unlimited && apiKey.quota_daily_limit != null) {
    rows.push({
      label: t('quota.daily'),
      limit: apiKey.quota_daily_limit,
      used: apiKey.quota_daily_used ?? 0,
      available: apiKey.quota_daily_available ?? null,
      unlimited: false,
    })
  }
  if (!apiKey.quota_weekly_unlimited && apiKey.quota_weekly_limit != null) {
    rows.push({
      label: t('quota.weekly'),
      limit: apiKey.quota_weekly_limit,
      used: apiKey.quota_weekly_used ?? 0,
      available: apiKey.quota_weekly_available ?? null,
      unlimited: false,
    })
  }

  return (
    <div className="space-y-1.5 text-xs tabular-nums">
      {rows.map((row) => <QuotaScopeRow key={row.label} row={row} truncateValues={truncateValues} compactValues={compactValues} />)}
    </div>
  )
}

function QuotaScopeRow({ row, truncateValues, compactValues }: { row: QuotaRow; truncateValues: boolean; compactValues: boolean }) {
  const { t } = useTranslation('api-keys')
  const formatValue = compactValues ? formatCompactDollarQuota : formatDollarQuota
  if (row.unlimited || row.limit == null) {
    return (
      <div className="flex min-w-0 items-center gap-1.5" title={t('quota.used', { amount: formatDollarQuota(row.used) })}>
        <span className="w-6 shrink-0 text-muted-soft">{row.label}</span>
        <span className={truncateValues ? 'truncate font-medium text-foreground' : 'whitespace-nowrap font-medium text-foreground'}>
          {t('quota.unlimited')} · {t('quota.used', { amount: formatValue(row.used) })}
        </span>
      </div>
    )
  }

  const available = row.available ?? Math.max(row.limit - row.used, 0)
  const remainPercent = row.limit > 0 ? Math.min(Math.max((available / row.limit) * 100, 0), 100) : 0
  const dotColor = remainPercent <= 10 ? 'bg-red-500' : remainPercent <= 30 ? 'bg-amber-500' : 'bg-emerald-500'

  return (
    <div
      className="flex min-w-0 items-center gap-1.5"
      title={t('quota.used', { amount: formatDollarQuota(row.used) })}
    >
      <span className="w-6 shrink-0 text-muted-soft">{row.label}</span>
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${dotColor}`} />
      <span className={truncateValues ? 'truncate font-medium text-foreground' : 'whitespace-nowrap font-medium text-foreground'}>
        {formatValue(available)} / {formatValue(row.limit)}
      </span>
    </div>
  )
}
