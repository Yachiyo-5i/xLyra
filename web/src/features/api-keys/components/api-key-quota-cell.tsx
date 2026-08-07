import { useCallback, useId, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
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
  detailsMode?: 'popover' | 'sheet'
}

const QUOTA_TOOLTIP_WIDTH = 420
const QUOTA_TOOLTIP_OFFSET = 14
const QUOTA_TOOLTIP_MARGIN = 12

export function APIKeyQuotaCell({ apiKey, truncateValues = true, compactValues = false, detailsMode = 'popover' }: APIKeyQuotaCellProps) {
  const { t } = useTranslation('api-keys')
  const [open, setOpen] = useState(false)
  const [mousePos, setMousePos] = useState<{ x: number; y: number } | null>(null)
  const [tooltipHeight, setTooltipHeight] = useState<number | null>(null)
  const tooltipId = useId()
  const rows = quotaRows(apiKey, t)
  const configuredRows = rows.filter((row) => row.id !== 'accumulated' && !row.unlimited && row.limit != null)
  const detailRows = rows.filter((row) => row.id === 'accumulated' || configuredRows.includes(row))
  const measureTooltip = useCallback((node: HTMLDivElement | null) => {
    const nextHeight = node?.offsetHeight ?? null
    setTooltipHeight((current) => current === nextHeight ? current : nextHeight)
  }, [])

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
      onMouseEnter={detailsMode === 'popover' ? (event) => setMousePos({ x: event.clientX, y: event.clientY }) : undefined}
      onMouseMove={detailsMode === 'popover' ? (event) => setMousePos({ x: event.clientX, y: event.clientY }) : undefined}
      onMouseLeave={detailsMode === 'popover' ? () => {
        setMousePos(null)
        setTooltipHeight(null)
      } : undefined}
      onFocus={detailsMode === 'popover' ? (event) => {
        const rect = event.currentTarget.getBoundingClientRect()
        setMousePos({ x: rect.left + rect.width / 2, y: rect.bottom })
      } : undefined}
      onBlur={detailsMode === 'popover' ? () => setMousePos(null) : undefined}
      aria-describedby={detailsMode === 'popover' && mousePos ? tooltipId : undefined}
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

  if (detailsMode === 'sheet') {
    return (
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetTrigger asChild>{trigger}</SheetTrigger>
        <SheetContent side="bottom" className="rounded-t-2xl">
          <SheetHeader>
            <SheetTitle>{t('quota.detailsTitle')}</SheetTitle>
            <SheetDescription>{apiKey.name}</SheetDescription>
          </SheetHeader>
          <SheetBody><QuotaDetails rows={detailRows} compactValues={false} t={t} /></SheetBody>
        </SheetContent>
      </Sheet>
    )
  }

  const tooltip = mousePos && detailsMode === 'popover'
    ? (
      <div
        id={tooltipId}
        ref={measureTooltip}
        role="tooltip"
        className="glass-panel-strong pointer-events-none fixed z-[160] w-[min(420px,calc(100vw-24px))] rounded-lg px-4 py-2 text-xs text-foreground shadow-lg"
        style={getQuotaTooltipLayout(mousePos, detailRows.length, tooltipHeight)}
      >
        <QuotaDetails rows={detailRows} compactValues={compactValues} t={t} />
      </div>
    )
    : null

  return <>{trigger}{tooltip}</>
}

function getQuotaTooltipLayout(mousePos: { x: number; y: number }, rowCount: number, measuredHeight: number | null): CSSProperties {
  if (typeof window === 'undefined') {
    return { left: mousePos.x + QUOTA_TOOLTIP_OFFSET, top: mousePos.y + QUOTA_TOOLTIP_OFFSET, width: QUOTA_TOOLTIP_WIDTH }
  }

  const width = Math.min(QUOTA_TOOLTIP_WIDTH, Math.max(240, window.innerWidth - QUOTA_TOOLTIP_MARGIN * 2))
  const tooltipHeight = measuredHeight ?? 44 + 36 * Math.max(1, rowCount)
  const preferredLeft = mousePos.x + QUOTA_TOOLTIP_OFFSET
  const preferredTop = mousePos.y + QUOTA_TOOLTIP_OFFSET
  const left = Math.max(QUOTA_TOOLTIP_MARGIN, Math.min(preferredLeft, window.innerWidth - width - QUOTA_TOOLTIP_MARGIN))
  const top = preferredTop + tooltipHeight > window.innerHeight - QUOTA_TOOLTIP_MARGIN
    ? Math.max(QUOTA_TOOLTIP_MARGIN, mousePos.y - tooltipHeight - QUOTA_TOOLTIP_OFFSET)
    : preferredTop

  return { left, top, width }
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
