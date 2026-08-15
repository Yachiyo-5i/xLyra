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
}

export function AnalyticsKpiCards({ usage }: AnalyticsKpiCardsProps) {
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
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <KpiCard
        icon={<WalletCards />}
        title={t('kpis.cost')}
        value={formatDashboardCurrency(totals.cost, currency)}
        note={extraCurrencies.length ? (
          <span title={extraCurrencies.map(([code, value]) => `${code} ${value.toFixed(4)}`).join(' · ')}>
            {t('kpis.multiCurrency', { count: extraCurrencies.length + 1 })}
          </span>
        ) : undefined}
        delta={previous ? { current: totals.cost, previous: previous.cost } : undefined}
      />
      <KpiCard
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
        icon={<Gauge />}
        title={t('kpis.requests')}
        value={formatCompactNumber(totals.requests)}
        note={t('kpis.successRate', { value: formatPercent(totals.success_rate) })}
        delta={previous ? { current: totals.requests, previous: previous.requests } : undefined}
      />
      <KpiCard
        icon={<Timer />}
        title={t('kpis.avgLatency')}
        value={formatLatencyMs(totals.avg_latency_ms)}
        note={t('kpis.maxLatency', { value: formatLatencyMs(totals.max_latency_ms) })}
        delta={previous ? { current: totals.avg_latency_ms, previous: previous.avg_latency_ms, invert: true } : undefined}
      />
    </div>
  )
}

type KpiDelta = {
  current: number
  previous: number
  invert?: boolean
}

function KpiCard({
  icon,
  title,
  value,
  note,
  delta,
}: {
  icon: ReactNode
  title: string
  value: ReactNode
  note?: ReactNode
  delta?: KpiDelta
}) {
  const { t } = useTranslation('analytics')

  return (
    <Card className="flex min-h-[120px] flex-col justify-between gap-3 rounded-lg p-4">
      <div className="flex items-center gap-2 text-muted-soft">
        <span className="[&>svg]:h-4 [&>svg]:w-4">{icon}</span>
        <span className="text-xs font-medium">{title}</span>
      </div>
      <div className="text-2xl font-semibold tabular-nums tracking-tight text-foreground">{value}</div>
      <div className="flex min-h-5 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-soft">
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
  const good = delta.invert ? !up : up
  return (
    <span
      className={cn(
        'inline-flex items-center gap-0.5 font-medium tabular-nums',
        good ? 'text-emerald-500' : 'text-rose-500',
      )}
      title={label}
    >
      <span className="text-muted-soft">{label}</span>
      {up ? <ArrowUpRight className="h-3.5 w-3.5" /> : <ArrowDownRight className="h-3.5 w-3.5" />}
      {`${Math.abs(ratio * 100).toFixed(1)}%`}
    </span>
  )
}
