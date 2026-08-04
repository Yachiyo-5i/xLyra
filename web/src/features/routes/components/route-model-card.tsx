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
        'section-soft w-full rounded-lg transition-[background-color,border-color,box-shadow]',
        expanded && 'border-[hsl(var(--primary)/0.45)] bg-[hsl(var(--surface-subtle))] shadow-sm',
      )}
    >
      <button
        type="button"
        onClick={onClick}
        className={cn(
          'flex w-full items-center gap-4 p-4 text-left transition-colors',
          'hover:bg-[hsl(var(--surface-subtle)/0.5)]',
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
            <div className="truncate text-base font-semibold text-foreground" title={modelName}>
              {modelName}
            </div>
            <div className="mt-1.5 flex flex-wrap gap-2">
              <SmallMetric label={t('card.routable')} value={String(routable)} />
              <SmallMetric label={t('card.configured')} value={String(configured)} />
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
            <Chevron className="h-4 w-4 text-muted-soft" />
          </div>
        </div>
      </button>
      {children}
    </div>
  )
}

function SmallMetric({ label, value, tone = 'neutral' }: { label: string; value: string; tone?: 'neutral' | 'accent' }) {
  return (
    <div className={cn(
      'inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs',
      tone === 'accent'
        ? 'bg-[hsl(var(--primary)/0.1)] text-[hsl(var(--primary))]'
        : 'bg-[hsl(var(--surface-subtle))] text-muted-soft',
    )}>
      <span>{label}</span>
      <span className="font-semibold text-foreground">{value}</span>
    </div>
  )
}
