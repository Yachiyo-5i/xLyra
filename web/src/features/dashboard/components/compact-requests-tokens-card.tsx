import { Activity, Hash, Sigma, type LucideIcon } from 'lucide-react'
import { TokenUsageHoverCard, type TokenUsageColumn, type TokenUsageLabels } from '@/components/common/token-usage-hover-card'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

type CompactRequestsTokensCardProps = {
  title: string
  todayLabel: string
  totalLabel: string
  requestsLabel: string
  tokensLabel: string
  requestsToday: string
  requestsTotal: string
  tokensToday: string
  tokensTotal: string
  successRateLabel: string
  successRate: string
  yesterdayRequestsLabel: string
  requestsYesterday: string
  yesterdayTokensLabel: string
  tokensYesterday: string
  tokenUsage: TokenUsageColumn[]
  tokenUsageLabels: TokenUsageLabels
  className?: string
}

export function CompactRequestsTokensCard({
  title,
  todayLabel,
  totalLabel,
  requestsLabel,
  tokensLabel,
  requestsToday,
  requestsTotal,
  tokensToday,
  tokensTotal,
  successRateLabel,
  successRate,
  yesterdayRequestsLabel,
  requestsYesterday,
  yesterdayTokensLabel,
  tokensYesterday,
  tokenUsage,
  tokenUsageLabels,
  className,
}: CompactRequestsTokensCardProps) {
  return (
    <Card className={cn('rounded-lg p-4', className)} role="group" aria-label={title}>
      <div className="flex h-full flex-col gap-3">
        <div className="flex items-center gap-2">
          <span className="flex size-5 items-center justify-center text-[hsl(154_70%_38%)] [&>svg]:size-4">
            <Activity aria-hidden="true" />
          </span>
          <p className="text-sm font-medium text-muted-soft">{title}</p>
        </div>

        <div className="grid grid-cols-[minmax(0,1fr)_minmax(4.5rem,0.8fr)_minmax(4.5rem,0.8fr)] items-center gap-x-3 gap-y-1.5">
          <span aria-hidden="true" />
          <span className="text-right text-[11px] font-medium text-faint">{todayLabel}</span>
          <span className="text-right text-[11px] font-medium text-faint">{totalLabel}</span>

          <MetricLabel icon={Hash}>{requestsLabel}</MetricLabel>
          <MetricNumber>{requestsToday}</MetricNumber>
          <MetricNumber>{requestsTotal}</MetricNumber>

          <MetricLabel icon={Sigma}>{tokensLabel}</MetricLabel>
          <TokenUsageHoverCard
            columns={tokenUsage}
            labels={tokenUsageLabels}
            className="col-span-2 grid min-h-11 grid-cols-2 items-center gap-3 rounded-md outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-strong))]"
          >
            <MetricNumber>{tokensToday}</MetricNumber>
            <MetricNumber>{tokensTotal}</MetricNumber>
          </TokenUsageHoverCard>
        </div>

        <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-soft">
          <MetadataItem label={successRateLabel} value={successRate} />
          <MetadataItem label={yesterdayRequestsLabel} value={requestsYesterday} />
          <MetadataItem label={yesterdayTokensLabel} value={tokensYesterday} />
        </div>
      </div>
    </Card>
  )
}

function MetricLabel({ icon: Icon, children }: { icon: LucideIcon; children: string }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5 text-xs font-medium text-muted-soft">
      <Icon className="size-3.5 shrink-0" aria-hidden="true" />
      <span className="truncate">{children}</span>
    </span>
  )
}

function MetricNumber({ children }: { children: string }) {
  return (
    <strong className="whitespace-nowrap text-right text-base font-semibold tabular-nums text-foreground">
      {children}
    </strong>
  )
}

function MetadataItem({ label, value }: { label: string; value: string }) {
  return (
    <span className="whitespace-nowrap">
      {label} <strong className="font-medium tabular-nums text-foreground">{value}</strong>
    </span>
  )
}
