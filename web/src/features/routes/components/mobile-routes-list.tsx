import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { BrandMark } from '@/components/common/brand-mark'
import { EmptyState } from '@/components/common/empty-state'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { RouteOverviewItem } from '@/features/routes/api/routes'
import type { CanonicalModelItem } from '@/features/sites/api/sites'
import { formatLatency, formatPercent } from '@/features/routes/lib/route-format'
import { routeBrandEntry } from '@/features/routes/lib/route-filters'

type MobileRoutesListProps = {
  items: RouteOverviewItem[]
  canonicalMap: Map<string, CanonicalModelItem>
  expandedModelKey: string | null
  onToggleModel: (modelKey: string) => void
  renderDetails: (item: RouteOverviewItem) => ReactNode
  className?: string
}

export function MobileRoutesList({
  items,
  canonicalMap,
  expandedModelKey,
  onToggleModel,
  renderDetails,
  className,
}: MobileRoutesListProps) {
  const { t } = useTranslation('routes')

  if (!items.length) {
    return (
      <div className={cn('rounded-lg border border-[hsl(var(--glass-border))] px-4 py-8', className)}>
        <EmptyState title={t('workspace.noModels')} description={t('workspace.noModelsDesc')} />
      </div>
    )
  }

  return (
    <div className={cn('space-y-3', className)}>
      {items.map((item) => {
        const modelKey = item.canonical_model.model_key
        const expanded = expandedModelKey === modelKey
        const canonical = canonicalMap.get(item.canonical_model.id)

        return (
          <MobileRouteCard
            key={item.canonical_model.id}
            item={item}
            canonical={canonical}
            expanded={expanded}
            onClick={() => onToggleModel(modelKey)}
          >
            {expanded ? renderDetails(item) : null}
          </MobileRouteCard>
        )
      })}
    </div>
  )
}

function MobileRouteCard({
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
  const hasTraffic = item.traffic_24h.request_count > 0

  return (
    <article
      className={cn(
        'overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))] shadow-[var(--button-secondary-shadow)]',
        expanded && 'border-[hsl(var(--primary)/0.45)]',
      )}
    >
      <button type="button" className="w-full p-3 text-left" onClick={onClick}>
        <div className="flex min-w-0 items-start gap-3">
          <BrandMark
            iconPath={iconPath}
            label={brandEntry?.name ?? modelName}
            fallback={modelName}
            size="md"
          />

          <div className="flex min-w-0 flex-1 items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <h3 className="break-all text-base font-semibold leading-snug text-foreground">{modelName}</h3>
              <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1.5">
                {item.candidate_summary.cooldown_count > 0 ? (
                  <Badge variant="warning" className="px-2 py-0.5 text-[11px]">
                    {t('card.cooldown', { count: item.candidate_summary.cooldown_count })}
                  </Badge>
                ) : null}
              </div>
            </div>
            <span className="mt-0.5 flex size-6 shrink-0 items-center justify-center text-muted-soft">
              <Chevron className="size-4" />
            </span>
          </div>
        </div>

        <div className="mt-3 grid grid-cols-3 gap-1.5 text-xs">
          <MobileMetric label={t('card.routable')} value={String(item.candidate_summary.eligible_count)} priority />
          <MobileMetric label={t('card.configured')} value={String(item.candidate_summary.site_model_count)} priority />
          <MobileMetric label={t('card.siteCount')} value={String(item.candidate_summary.site_count)} priority />
          <MobileMetric label={t('card.request')} value={hasTraffic ? String(item.traffic_24h.request_count) : t('card.noRequest')} />
          <MobileMetric
            label={t('card.successRate')}
            value={hasTraffic ? formatPercent(item.traffic_24h.success_rate, 2) : '-'}
            accent={hasTraffic}
          />
          <MobileMetric label={t('card.latency')} value={formatLatency(item.traffic_24h.avg_latency_ms)} />
        </div>
      </button>
      {children}
    </article>
  )
}

function MobileMetric({ label, value }: { label: string; value: string; accent?: boolean; priority?: boolean }) {
  return (
    <div className="min-w-0 px-2 py-2">
      <span className="block truncate text-[11px] text-muted-soft">{label}</span>
      <span className="mt-1 block truncate text-sm font-semibold text-foreground tabular-nums" title={value}>
        {value}
      </span>
    </div>
  )
}
