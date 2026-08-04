import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import type { DashboardRange } from './dashboard-types'

type DashboardChartPanelProps = {
  title: ReactNode
  description?: string
  range?: DashboardRange
  onRangeChange?: (range: DashboardRange) => void
  action?: ReactNode
  children: ReactNode
  className?: string
}

export function DashboardChartPanel({
  title,
  description,
  range,
  onRangeChange,
  action,
  children,
  className,
}: DashboardChartPanelProps) {
  const { t } = useTranslation('dashboard')

  const rangeOptions: Array<{ label: string; value: DashboardRange }> = [
    { label: t('ranges.7d'), value: '7d' },
    { label: t('ranges.30d'), value: '30d' },
    { label: t('ranges.90d'), value: '90d' },
  ]

  return (
    <Card className={cn('rounded-lg p-5', className)}>
      <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h3 className="text-base font-semibold text-foreground">{title}</h3>
          {description ? <p className="text-sm text-muted-soft">{description}</p> : null}
        </div>
        <div className="flex items-center gap-2">
          {range ? (
            <Tabs value={range} onValueChange={(value) => onRangeChange?.(value as DashboardRange)}>
              <TabsList className="h-9 w-[184px]">
              {rangeOptions.map((option) => (
                <TabsTrigger
                  key={option.value}
                  value={option.value}
                  className="text-xs"
                >
                  {option.label}
                </TabsTrigger>
              ))}
              </TabsList>
            </Tabs>
          ) : null}
          {action}
        </div>
      </div>
      {children}
    </Card>
  )
}
