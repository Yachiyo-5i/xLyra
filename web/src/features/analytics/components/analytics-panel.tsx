import type { ReactNode } from 'react'
import { Card } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

type AnalyticsPanelProps = {
  title: ReactNode
  description?: string
  action?: ReactNode
  children: ReactNode
  className?: string
  bodyClassName?: string
}

export function AnalyticsPanel({
  title,
  description,
  action,
  children,
  className,
  bodyClassName,
}: AnalyticsPanelProps) {
  return (
    <Card className={cn('flex min-h-0 flex-col rounded-lg p-5', className)}>
      <div className="mb-4 flex shrink-0 flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h3 className="text-base font-semibold text-foreground">{title}</h3>
          {description ? <p className="text-sm text-muted-soft">{description}</p> : null}
        </div>
        {action ? <div className="flex flex-wrap items-center gap-2">{action}</div> : null}
      </div>
      <div className={cn('min-h-0 flex-1', bodyClassName)}>{children}</div>
    </Card>
  )
}

export function AnalyticsSlashTabs<T extends string>({
  value,
  items,
  onValueChange,
}: {
  value: T
  items: Array<{ label: string; value: T }>
  onValueChange: (value: T) => void
}) {
  return (
    <Tabs variant="slash" value={value} onValueChange={(next) => onValueChange(next as T)}>
      <TabsList>
        {items.map((item) => (
          <TabsTrigger key={item.value} value={item.value} className="text-sm">
            {item.label}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}
