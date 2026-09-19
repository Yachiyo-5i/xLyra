import { useMobileLayout } from '@/hooks/use-media-query'
import { HoverDetails } from '@/components/common/hover-details'
import { useTranslation } from 'react-i18next'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import { formatCompactDollarQuota, formatDollarQuota } from '@/features/api-keys/lib/api-key-utils'

type QuotaRow = {
  id: 'accumulated' | 'total' | 'daily' | 'weekly'
  label: string
  shortLabel: string
  limit: number | null
  used: number
  available: number | null
  unlimited: boolean
}

type APIKeyQuotaCellProps = {
  apiKey: DownstreamAPIKey
  truncateValues?: boolean
  compactValues?: boolean
}

export function APIKeyQuotaCell({ apiKey, truncateValues = true, compactValues = false }: APIKeyQuotaCellProps) {
  const { t } = useTranslation('api-keys')
  const isMobile = useMobileLayout()
  const rows = quotaRows(apiKey, t)
  const configuredRows = rows.filter((row) => row.id !== 'accumulated' && !row.unlimited && row.limit != null)
  const detailRows = rows.filter((row) => row.id === 'accumulated' || configuredRows.includes(row))

  if (configuredRows.length === 0) {
    return (
      <UnlimitedQuotaUsage
        used={apiKey.quota_used}
        truncateValues={truncateValues}
        compactValues={compactValues}
        t={t}
      />
    )
  }

  const trigger = (
    <button
      type="button"
      className="inline-block max-w-full min-w-0 rounded-sm text-left align-top focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-strong))]"
    >
      <span className="sr-only">{t('quota.detailsLabel')}, </span>
      <span className="block space-y-1.5 text-xs tabular-nums">
        {configuredRows.map((row) => (
          <QuotaScopeRow
            key={row.id}
            row={row}
            truncateValues={truncateValues}
            compactValues={compactValues}
          />
        ))}
      </span>
    </button>
  )

  return (
    <HoverDetails
      asChild
      title={t('quota.detailsTitle')}
      description={apiKey.name}
      contentClassName="w-[420px]"
      content={<QuotaDetails rows={detailRows} compactValues={isMobile ? false : compactValues} t={t} />}
    >
      {trigger}
    </HoverDetails>
  )
}

function quotaRows(apiKey: DownstreamAPIKey, t: (key: string, options?: Record<string, unknown>) => string): QuotaRow[] {
  const totalUsed = apiKey.quota_total_used ?? apiKey.quota_used
  return [
    {
      id: 'accumulated',
      label: t('quota.accumulated'),
      shortLabel: t('quota.accumulatedShort'),
      limit: null,
      used: apiKey.quota_used,
      available: null,
      unlimited: true,
    },
    {
      id: 'total',
      label: t('quota.total'),
      shortLabel: t('quota.totalShort'),
      limit: apiKey.quota_limit ?? null,
      used: totalUsed,
      available: apiKey.quota_total_available ?? apiKey.quota_available ?? null,
      unlimited: apiKey.quota_unlimited || apiKey.quota_limit == null,
    },
    {
      id: 'weekly',
      label: t('quota.weekly'),
      shortLabel: t('quota.weeklyShort'),
      limit: apiKey.quota_weekly_limit ?? null,
      used: apiKey.quota_weekly_used ?? 0,
      available: apiKey.quota_weekly_available ?? null,
      unlimited: apiKey.quota_weekly_unlimited || apiKey.quota_weekly_limit == null,
    },
    {
      id: 'daily',
      label: t('quota.daily'),
      shortLabel: t('quota.dailyShort'),
      limit: apiKey.quota_daily_limit ?? null,
      used: apiKey.quota_daily_used ?? 0,
      available: apiKey.quota_daily_available ?? null,
      unlimited: apiKey.quota_daily_unlimited || apiKey.quota_daily_limit == null,
    },
  ]
}

function QuotaDetails({ rows, compactValues, t }: { rows: QuotaRow[]; compactValues: boolean; t: (key: string, options?: Record<string, unknown>) => string }) {
  return (
    <div className="divide-y divide-[hsl(var(--glass-divider))] text-xs tabular-nums">
      {rows.map((row) => <QuotaDetailItem key={row.id} row={row} compactValues={compactValues} t={t} />)}
    </div>
  )
}

function QuotaDetailItem({ row, compactValues, t }: { row: QuotaRow; compactValues: boolean; t: (key: string, options?: Record<string, unknown>) => string }) {
  const formatValue = compactValues ? formatCompactDollarQuota : formatDollarQuota
  const value = row.id === 'accumulated'
    ? formatValue(row.used)
    : row.unlimited
    ? `${t('quota.unlimited')} · ${t('quota.used', { amount: formatValue(row.used) })}`
    : `${formatValue(row.available ?? Math.max(row.limit! - row.used, 0))} / ${formatValue(row.limit!)} · ${t('quota.used', { amount: formatValue(row.used) })}`
  return (
    <div className="flex min-w-0 items-start justify-between gap-4 py-2.5 first:pt-1 last:pb-1">
      <div className="flex shrink-0 items-center gap-1.5 text-muted-soft">
        {row.id !== 'accumulated' && !row.unlimited ? <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${quotaDot(row)}`} /> : null}
        <span>{row.label}</span>
      </div>
      <span className="min-w-0 text-right font-medium text-foreground">{value}</span>
    </div>
  )
}

function QuotaScopeRow({ row, truncateValues, compactValues }: { row: QuotaRow; truncateValues: boolean; compactValues: boolean }) {
  const formatValue = compactValues ? formatCompactDollarQuota : formatDollarQuota
  const available = row.available ?? Math.max(row.limit! - row.used, 0)
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <span className="w-6 shrink-0 text-muted-soft">{row.shortLabel}</span>
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${quotaDot(row)}`} />
      <span className={truncateValues ? 'truncate font-medium text-foreground' : 'whitespace-nowrap font-medium text-foreground'}>
        {formatValue(available)} / {formatValue(row.limit!)}
      </span>
    </span>
  )
}

function UnlimitedQuotaUsage({ used, truncateValues, compactValues, t }: { used: number; truncateValues: boolean; compactValues: boolean; t: (key: string, options?: Record<string, unknown>) => string }) {
  const formatValue = compactValues ? formatCompactDollarQuota : formatDollarQuota
  return (
    <div className="space-y-1.5 text-xs tabular-nums">
      <div className="flex min-w-0 items-center gap-1.5">
        <span className="w-6 shrink-0 text-muted-soft">{t('quota.totalShort')}</span>
        <span className={truncateValues ? 'truncate font-medium text-foreground' : 'whitespace-nowrap font-medium text-foreground'}>
          {t('quota.unlimited')} · {t('quota.used', { amount: formatValue(used) })}
        </span>
      </div>
    </div>
  )
}

function quotaDot(row: QuotaRow) {
  const remainPercent = quotaRemainPercent(row)
  return remainPercent <= 10 ? 'bg-red-500' : remainPercent <= 30 ? 'bg-amber-500' : 'bg-emerald-500'
}

function quotaRemainPercent(row: QuotaRow) {
  if (row.limit == null || row.limit <= 0) return 100
  const available = row.available ?? Math.max(row.limit - row.used, 0)
  return Math.min(Math.max((available / row.limit) * 100, 0), 100)
}
