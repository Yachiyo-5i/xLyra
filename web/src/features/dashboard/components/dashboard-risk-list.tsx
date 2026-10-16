import { AlertTriangle, CheckCircle2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { DashboardRiskItem } from './dashboard-types'

type DashboardRiskListProps = {
  items: DashboardRiskItem[]
  onItemClick?: (item: DashboardRiskItem) => void
  className?: string
}

const toneVariant = {
  neutral: 'neutral',
  warning: 'warning',
  error: 'accent',
} as const

export function DashboardRiskList({ items, onItemClick, className }: DashboardRiskListProps) {
  const { t } = useTranslation('dashboard')

  const toneLabel = {
    neutral: t('risks.severity.neutral'),
    warning: t('risks.severity.warning'),
    error: t('risks.severity.error'),
  }

  return (
    <Card className={cn('flex min-h-0 flex-col p-5', className)}>
      <div className="mb-4 shrink-0 space-y-1">
        <h3 className="text-headline text-label">{t('risks.title')}</h3>
        <p className="text-footnote text-label-3">{t('risks.description')}</p>
      </div>
      <div className="scrollbar-hidden min-h-0 flex-1 divide-y divide-separator overflow-auto pr-1">
        {items.length === 0 ? (
          <p className="flex h-full min-h-[120px] items-center justify-center text-footnote text-label-3">{t('risks.empty')}</p>
        ) : items.map((item) => {
          const tone = item.tone ?? 'neutral'
          return (
            <div
              key={item.id}
              role={onItemClick ? 'button' : undefined}
              tabIndex={onItemClick ? 0 : undefined}
              onClick={() => onItemClick?.(item)}
              onKeyDown={(event) => {
                if (!onItemClick) return
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  onItemClick(item)
                }
              }}
              className="flex items-start gap-3 py-3 transition-colors hover:bg-fill-4 data-[clickable=true]:cursor-pointer"
              data-clickable={Boolean(onItemClick)}
            >
              <div className="mt-0.5 text-label-2">
                {item.icon ?? (tone === 'neutral' ? <CheckCircle2 className="h-4 w-4" /> : <AlertTriangle className="h-4 w-4" />)}
              </div>
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="text-body font-medium text-label">{item.title}</p>
                  <Badge variant={toneVariant[tone]}>{item.badgeLabel ?? toneLabel[tone]}</Badge>
                </div>
                <p className="text-body text-label-2">{item.detail}</p>
              </div>
              {item.value ? (
                <p className="shrink-0 text-body font-semibold tabular-nums text-label">{item.value}</p>
              ) : null}
            </div>
          )
        })}
      </div>
    </Card>
  )
}
