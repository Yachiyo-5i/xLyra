import type { ReactNode } from 'react'
import { TokenUsageHoverCard, type TokenUsageColumn, type TokenUsageLabels } from '@/components/common/token-usage-hover-card'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

type DashboardMetricCardProps = {
  title: string
  primaryLabel: string
  primaryValue: string
  secondaryLabel: string
  secondaryValue: string
  icon?: ReactNode
  primaryIcon?: ReactNode
  secondaryIcon?: ReactNode
  secondaryTokenUsage?: TokenUsageColumn[]
  tokenUsageLabels?: TokenUsageLabels
  accent?: 'cost' | 'traffic' | 'performance'
  note?: ReactNode
  className?: string
}

const accentClasses = {
  cost: 'text-[hsl(190_88%_42%)]',
  traffic: 'text-[hsl(154_70%_38%)]',
  performance: 'text-[hsl(42_88%_48%)]',
}

export function DashboardMetricCard({
  title,
  primaryLabel,
  primaryValue,
  secondaryLabel,
  secondaryValue,
  icon,
  primaryIcon,
  secondaryIcon,
  secondaryTokenUsage,
  tokenUsageLabels,
  accent = 'cost',
  note,
  className,
}: DashboardMetricCardProps) {
  return (
    <Card className={cn('@container/metric rounded-lg p-4', className)}>
      <div className="relative space-y-3">
        <div className="flex items-center gap-2">
          {icon ? (
            <span className={cn('flex size-5 items-center justify-center [&>svg]:size-4', accentClasses[accent])}>
              {icon}
            </span>
          ) : null}
          <p className="text-sm font-medium text-muted-soft">{title}</p>
        </div>
        <div className="grid grid-cols-1 gap-3 @[21rem]/metric:grid-cols-[minmax(0,1fr)_1px_minmax(0,1fr)] @[21rem]/metric:items-stretch">
          <MetricValue label={primaryLabel} value={primaryValue} icon={primaryIcon} />
          <div className="h-px w-full bg-[hsl(var(--glass-divider))] @[21rem]/metric:my-1 @[21rem]/metric:h-auto @[21rem]/metric:w-px" />
          <MetricValue label={secondaryLabel} value={secondaryValue} icon={secondaryIcon} tokenUsage={secondaryTokenUsage} tokenUsageLabels={tokenUsageLabels} />
        </div>
        {note ? <div className="text-xs text-muted-soft">{note}</div> : null}
      </div>
    </Card>
  )
}

function MetricValue({ label, value, icon, tokenUsage, tokenUsageLabels }: { label: string; value: string; icon?: ReactNode; tokenUsage?: TokenUsageColumn[]; tokenUsageLabels?: TokenUsageLabels }) {
  const content = (
    <div className="flex min-w-0 flex-col px-1 py-1">
      <div className="mb-1.5 flex min-w-0 items-start gap-1.5 text-faint">
        {icon ? <span className="flex size-4 items-center justify-center [&>svg]:size-3.5">{icon}</span> : null}
        <p className="min-w-0 break-words text-xs font-medium leading-4">{label}</p>
      </div>
      <p className="mt-auto whitespace-nowrap text-lg font-semibold tracking-tight text-foreground">{value}</p>
    </div>
  )
  if (!tokenUsage || !tokenUsageLabels) return content
  return (
    <TokenUsageHoverCard columns={tokenUsage} labels={tokenUsageLabels} className="w-full">
      {content}
    </TokenUsageHoverCard>
  )
}
