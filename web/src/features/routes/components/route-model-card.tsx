import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { BrandMark } from '@/components/common/brand-mark'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { RouteOverviewItem } from '@/features/routes/api/routes'
import type { CanonicalModelItem } from '@/features/sites/api/sites'
import { formatPercent } from '@/features/routes/lib/route-format'
import { routeBrandEntry } from '@/features/routes/lib/route-filters'

export function RouteModelCard({
  item,
  canonical,
  expanded,
  onClick,
  children,
}: {
  item: RouteOverviewItem
  canonical?: CanonicalModelItem
  expanded: boolean
  onClick: () => void
  children?: ReactNode
}) {
  const { t } = useTranslation('routes')
  const modelName = item.canonical_model.model_key
  const brandEntry = routeBrandEntry(item, canonical)
  const iconPath = canonical?.icon_url ?? brandEntry?.iconPath
  const Chevron = expanded ? ChevronDown : ChevronRight
  const configured = item.candidate_summary.site_model_count
  const routable = item.candidate_summary.eligible_count

  return (
    <div
      className={cn(
        'mat-regular w-full rounded-card transition-[background-color,border-color,box-shadow]',
        expanded && 'border-tint/45 bg-fill-3 shadow-sm',
      )}
    >
      <button
        type="button"
        onClick={onClick}
        className={cn(
          'flex w-full items-center gap-4 p-4 text-left transition-colors',
          'hover:bg-fill-4',
        )}
      >
        <BrandMark
          iconPath={iconPath}
          label={brandEntry?.name ?? modelName}
          fallback={modelName}
          size="md"
        />
        <div className="flex min-w-0 flex-1 items-center justify-between gap-4">
          <div className="min-w-0 flex-1">
            <div className="truncate text-headline text-label" title={modelName}>
              {modelName}
            </div>
            <div className="mt-1.5 flex flex-wrap gap-2">
              <SmallMetric
                label={t('card.channels')}
                value={t('card.channelsValue', { routable, configured })}
                hint={t('card.channelsHint', { routable, configured })}
              />
              <SmallMetric label={t('card.request')} value={item.traffic_24h.request_count > 0 ? String(item.traffic_24h.request_count) : t('card.noRequest')} />
              <SmallMetric
                label={t('card.successRate')}
                value={item.traffic_24h.request_count > 0 ? formatPercent(item.traffic_24h.success_rate, 2) : '-'}
                tone="accent"
              />
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-3">
            {item.candidate_summary.cooldown_count > 0 ? (
              <Badge variant="warning">{t('card.cooldown', { count: item.candidate_summary.cooldown_count })}</Badge>
            ) : null}
            <Chevron className="h-4 w-4 text-label-2" />
          </div>
        </div>
      </button>
      {children}
    </div>
  )
}

function SmallMetric({ label, value, tone = 'neutral', hint }: { label: string; value: string; tone?: 'neutral' | 'accent'; hint?: string }) {
  return (
    <div
      title={hint}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-control px-2 py-1 text-footnote',
        tone === 'accent'
          ? 'bg-tint/10 text-tint'
          : 'bg-fill-3 text-label-2',
      )}
    >
      <span>{label}</span>
      <span className="font-semibold text-label">{value}</span>
    </div>
  )
}
