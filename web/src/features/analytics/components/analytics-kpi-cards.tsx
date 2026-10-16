import type { ReactNode } from 'react'
import { ArrowDownRight, ArrowUpRight, Gauge, Hash, Timer, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { TokenUsageHoverCard, type TokenUsageLabels } from '@/components/common/token-usage-hover-card'
import { Card } from '@/components/ui/card'
import type { AnalyticsUsage } from '@/features/analytics/api/analytics'
import {
  formatCompactNumber,
  formatDashboardCurrency,
  formatPercent,
} from '@/features/dashboard/lib/dashboard-utils'
import { formatLatencyMs } from '@/features/analytics/lib/analytics-utils'
import { cn } from '@/lib/utils'

type AnalyticsKpiCardsProps = {
  usage: AnalyticsUsage
  /** 移动端：2×2 网格、更紧凑的卡片 */
  compact?: boolean
}

/** 用量分析的 4 张 KPI 卡，桌面与移动端共用 */
export function AnalyticsKpiCards({ usage, compact = false }: AnalyticsKpiCardsProps) {
  const { t } = useTranslation('analytics')
  const { totals, meta } = usage
  const currency = meta.currency || 'USD'
  const previous = totals.previous_period ?? null
  const extraCurrencies = Object.entries(totals.cost_by_currency ?? {}).filter(
    ([code]) => code !== currency,
  )

  const tokenLabels: TokenUsageLabels = {
    total: t('tokens.total'),
    input: t('tokens.input'),
    output: t('tokens.output'),
    cached: t('tokens.cached'),
    hitRate: t('tokens.hitRate'),
  }

  return (
    <div className={cn('grid', compact ? 'grid-cols-2 gap-3' : 'gap-4 sm:grid-cols-2 lg:grid-cols-4')}>
      <KpiCard
        compact={compact}
        icon={<WalletCards />}
        title={t('kpis.cost')}
        value={formatDashboardCurrency(totals.cost, currency)}
        note={extraCurrencies.length ? (
          <span title={extraCurrencies.map(([code, value]) => `${code} ${value.toFixed(4)}`).join(' · ')}>
            {t('kpis.multiCurrency', { count: extraCurrencies.length + 1 })}
          </span>
        ) : undefined}
        delta={previous ? { current: totals.cost, previous: previous.cost, tone: 'neutral' } : undefined}
      />
      <KpiCard
        compact={compact}
        icon={<Hash />}
        title={t('kpis.tokens')}
        value={(
          <TokenUsageHoverCard
            columns={[{
              usage: {
                total: totals.total_tokens,
                input: totals.prompt_tokens,
                output: totals.completion_tokens,
                cached: totals.cached_tokens,
              },
            }]}
            labels={tokenLabels}
          >
            <span>
              {formatCompactNumber(totals.total_tokens)}
            </span>
          </TokenUsageHoverCard>
        )}
        note={t('kpis.cacheHitRate', { value: formatPercent(totals.cache_hit_rate) })}
        delta={previous ? { current: totals.total_tokens, previous: previous.total_tokens } : undefined}
      />
      <KpiCard
        compact={compact}
        icon={<Gauge />}
        title={t('kpis.requests')}
        value={formatCompactNumber(totals.requests)}
        note={t('kpis.successRate', { value: formatPercent(totals.success_rate) })}
        delta={previous ? { current: totals.requests, previous: previous.requests } : undefined}
      />
      <KpiCard
        compact={compact}
        icon={<Timer />}
        title={t('kpis.avgLatency')}
        value={formatLatencyMs(totals.avg_latency_ms)}
        note={t('kpis.maxLatency', { value: formatLatencyMs(totals.max_latency_ms) })}
        delta={previous ? { current: totals.avg_latency_ms, previous: previous.avg_latency_ms, tone: 'up-bad' } : undefined}
      />
    </div>
  )
}

type KpiDelta = {
  current: number
  previous: number
  /** 颜色表示好坏而不是涨跌：up-good 上涨为绿（默认），up-bad 上涨为红（延迟），neutral 不着色（费用） */
  tone?: 'up-good' | 'up-bad' | 'neutral'
}

function KpiCard({
  icon,
  title,
  value,
  note,
  delta,
  compact,
}: {
  icon: ReactNode
  title: string
  value: ReactNode
  note?: ReactNode
  delta?: KpiDelta
  compact?: boolean
}) {
  const { t } = useTranslation('analytics')

  return (
    <Card className={cn('flex min-w-0 flex-col gap-3', compact ? 'p-4' : 'min-h-[128px] justify-between p-5')}>
      <div className="flex items-center gap-2">
        <span className="text-label-3 [&>svg]:size-4">{icon}</span>
        <span className="truncate text-footnote font-medium text-label-2">{title}</span>
      </div>
      <div className={cn('truncate font-mono font-semibold tabular-nums text-label', compact ? 'text-[22px] leading-7' : 'text-kpi')}>{value}</div>
      <div className="flex min-h-4 flex-wrap items-center gap-x-3 gap-y-1 text-caption-1 text-label-3">
        {note}
        {delta ? <DeltaBadge delta={delta} label={t('kpis.vsPrevious')} /> : null}
      </div>
    </Card>
  )
}

function DeltaBadge({ delta, label }: { delta: KpiDelta; label: string }) {
  if (!Number.isFinite(delta.previous) || delta.previous === 0) return null
  const ratio = (delta.current - delta.previous) / Math.abs(delta.previous)
  if (!Number.isFinite(ratio)) return null
  const up = ratio >= 0
  const tone = delta.tone ?? 'up-good'
  const color = tone === 'neutral' ? 'text-label-2' : (tone === 'up-good') === up ? 'text-success' : 'text-danger'
  return (
    <span className={cn('inline-flex items-center gap-0.5 font-medium tabular-nums', color)} title={label}>
      <span className="font-normal text-label-3">{label}</span>
      {up ? <ArrowUpRight className="size-3.5" /> : <ArrowDownRight className="size-3.5" />}
      {`${Math.abs(ratio * 100).toFixed(1)}%`}
    </span>
  )
}
