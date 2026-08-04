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
    <Card className={cn('flex min-h-0 flex-col rounded-lg p-5', className)}>
      <div className="mb-4 shrink-0 space-y-1">
        <h3 className="text-base font-semibold text-foreground">{t('risks.title')}</h3>
        <p className="text-sm text-muted-soft">{t('risks.description')}</p>
      </div>
      <div className="scrollbar-hidden min-h-0 flex-1 divide-y divide-[hsl(var(--glass-divider))] overflow-auto pr-1">
        {items.length === 0 ? (
          <div className="flex h-full min-h-[120px] items-center justify-center text-sm text-muted-soft">
            {t('risks.empty')}
          </div>
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
              className="flex items-start gap-3 py-3 transition-colors hover:bg-[hsl(var(--surface-subtle)/0.55)] data-[clickable=true]:cursor-pointer"
              data-clickable={Boolean(onItemClick)}
            >
              <div className="mt-0.5 text-muted-soft">
                {item.icon ?? (tone === 'neutral' ? <CheckCircle2 className="h-4 w-4" /> : <AlertTriangle className="h-4 w-4" />)}
              </div>
              <div className="min-w-0 flex-1 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="text-sm font-medium text-foreground">{item.title}</p>
                  <Badge variant={toneVariant[tone]}>{item.badgeLabel ?? toneLabel[tone]}</Badge>
                </div>
                <p className="text-sm text-muted-soft">{item.detail}</p>
              </div>
              {item.value ? (
                <p className="shrink-0 text-sm font-semibold tabular-nums text-foreground">{item.value}</p>
              ) : null}
            </div>
          )
        })}
      </div>
    </Card>
  )
}
