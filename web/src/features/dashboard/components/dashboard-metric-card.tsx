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
  /** 旧版每个数值前的小图标，现已不显示，保留参数以兼容调用方 */
  primaryIcon?: ReactNode
  secondaryIcon?: ReactNode
  secondaryTokenUsage?: TokenUsageColumn[]
  tokenUsageLabels?: TokenUsageLabels
  /** 旧版按卡片区分的图标颜色，现统一为单色，保留参数以兼容调用方 */
  accent?: 'cost' | 'traffic' | 'performance'
  note?: ReactNode
  className?: string
}

/**
 * 概览指标卡：标题行（单色图标 + Footnote）→ 两栏数值 → 底部补充说明。
 * 数值用等宽 KPI 字号；「今日 / 累计」这类成对数值只放大前半部分，后半部分用小号 label-3，
 * 一眼先看到今日。卡片足够宽时两栏并排，窄时上下排列（容器查询）。
 */
export function DashboardMetricCard({
  title,
  primaryLabel,
  primaryValue,
  secondaryLabel,
  secondaryValue,
  icon,
  secondaryTokenUsage,
  tokenUsageLabels,
  note,
  className,
}: DashboardMetricCardProps) {
  return (
    <Card className={cn('@container/metric flex flex-col gap-4 p-5', className)}>
      <div className="flex items-center gap-2">
        {icon ? <span className="flex size-4 items-center justify-center text-label-3 [&>svg]:size-4">{icon}</span> : null}
        <p className="text-footnote font-medium text-label-2">{title}</p>
      </div>
      <div className="grid grid-cols-1 gap-3 @[16rem]/metric:grid-cols-[minmax(0,1fr)_1px_minmax(0,1fr)] @[16rem]/metric:gap-4">
        <MetricValue label={primaryLabel} value={primaryValue} />
        <div className="h-px w-full bg-separator @[16rem]/metric:h-auto @[16rem]/metric:w-px" />
        <MetricValue label={secondaryLabel} value={secondaryValue} tokenUsage={secondaryTokenUsage} tokenUsageLabels={tokenUsageLabels} />
      </div>
      {note ? <div className="mt-auto text-caption-1 text-label-3">{note}</div> : null}
    </Card>
  )
}

/** 「1 / 540.4K」→ 主值「1」+ 次值「/ 540.4K」 */
function splitPair(value: string) {
  const index = value.indexOf(' / ')
  if (index < 0) return { main: value, rest: '' }
  return { main: value.slice(0, index), rest: value.slice(index + 1) }
}

function MetricValue({ label, value, tokenUsage, tokenUsageLabels }: { label: string; value: string; tokenUsage?: TokenUsageColumn[]; tokenUsageLabels?: TokenUsageLabels }) {
  const { main, rest } = splitPair(value)
  const content = (
    <div className="flex min-w-0 flex-col" aria-label={`${label} ${value}`}>
      <p className="min-w-0 truncate text-caption-1 text-label-3">{label}</p>
      <p className="mt-1.5 flex min-w-0 items-baseline gap-1.5 whitespace-nowrap font-mono tabular-nums">
        <span className="truncate text-[22px] font-semibold leading-7 text-label @[26rem]/metric:text-kpi">{main}</span>
        {rest ? <span className="shrink-0 text-callout text-label-3">{rest}</span> : null}
      </p>
    </div>
  )
  if (!tokenUsage || !tokenUsageLabels) return content
  return (
    <TokenUsageHoverCard columns={tokenUsage} labels={tokenUsageLabels} className="w-full">
      {content}
    </TokenUsageHoverCard>
  )
}
