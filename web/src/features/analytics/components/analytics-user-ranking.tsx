import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import type { AnalyticsUsage } from '@/features/analytics/api/analytics'
import type { AnalyticsTrendMetric } from '@/features/analytics/lib/analytics-utils'
import { formatLatencyMs } from '@/features/analytics/lib/analytics-utils'
import { dashboardChartColors } from '@/features/dashboard/components/chart-style'
import { formatCompactNumber, formatDashboardCurrency, formatPercent } from '@/features/dashboard/lib/dashboard-utils'
import { AnalyticsPanel } from './analytics-panel'

const MAX_ROWS = 20

type AnalyticsUserRankingProps = {
  className?: string
  usage: AnalyticsUsage
  metric: AnalyticsTrendMetric
}

export function AnalyticsUserRanking({ className, usage, metric }: AnalyticsUserRankingProps) {
  const { t } = useTranslation('analytics')
  const currency = usage.meta.currency || 'USD'

  const rows = useMemo(() => {
    return [...usage.breakdowns.api_key]
      // 过滤掉无 API Key 的匿名请求（id 为 null 且 label 为 unknown）
      .filter((item) => !(item.id === null && item.label === 'unknown'))
      // label 是 unknown 但有 id 的（已删除的 Key），显示为 Key ID 后 8 位
      .map((item) => ({
        ...item,
        label: item.label === 'unknown' && item.id
          ? `Key …${item.id.slice(-8)}`
          : item.label,
      }))
      .sort((a, b) => {
        switch (metric) {
          case 'tokens': return b.total_tokens - a.total_tokens
          case 'requests': return b.requests - a.requests
          case 'latency': return b.avg_latency_ms - a.avg_latency_ms
          default: return b.cost - a.cost
        }
      })
      .slice(0, MAX_ROWS)
  }, [usage.breakdowns.api_key, metric])

  // 计算占比分母（同指标的总和）
  const totalValue = useMemo(() => {
    return rows.reduce((sum, item) => sum + metricValue(item, metric), 0)
  }, [rows, metric])

  const maxValue = rows[0] ? metricValue(rows[0], metric) : 0

  function formatValue(value: number) {
    switch (metric) {
      case 'cost': return formatDashboardCurrency(value, currency, 2)
      case 'latency': return formatLatencyMs(value)
      default: return formatCompactNumber(value)
    }
  }

  const metricLabel = t(`trend.metrics.${
    metric === 'cost' ? 'cost'
    : metric === 'tokens' ? 'tokens'
    : metric === 'requests' ? 'requests'
    : 'latency'
  }`)

  // 标题：排行榜 · {指标维度}
  // 副标题：筛选的时间范围
  const titleText = `${t('ranking.title')} · ${metricLabel}`
  const dateRange = usage.meta.from === usage.meta.to
    ? usage.meta.from
    : `${usage.meta.from} – ${usage.meta.to}`

  return (
    <AnalyticsPanel
      className={className}
      title={titleText}
      description={dateRange}
      bodyClassName="relative"
    >
      {rows.length ? (
        <div className="absolute inset-0 flex flex-col gap-3 overflow-y-auto pr-1">
          {rows.map((item, index) => {
            const value = metricValue(item, metric)
            const barWidth = maxValue > 0 ? (value / maxValue) * 100 : 0
            const pct = totalValue > 0 ? value / totalValue : 0
            const color = dashboardChartColors[index % dashboardChartColors.length]
            return (
              <div key={item.key} className="flex min-w-0 flex-col gap-1">
                <div className="flex items-center gap-1.5">
                  <span className="shrink-0 text-[11px] tabular-nums text-muted-soft">
                    {index + 1}
                  </span>
                  <span className="min-w-0 flex-1 truncate text-sm text-foreground" title={item.label}>
                    {item.label}
                  </span>
                  {/* 右侧合并为一个 span：数值 · 占比（延迟指标无占比），间距稳定 */}
                  <span className="shrink-0 text-xs tabular-nums text-muted-soft">
                    {metric !== 'latency'
                      ? `${formatValue(value)} · ${formatPercent(pct, 1)}`
                      : formatValue(value)}
                  </span>
                </div>
                {/* 进度条宽度与文字行完全一致，无额外缩进 */}
                <div className="h-1.5 w-full overflow-hidden rounded-full bg-[hsl(var(--surface-subtle))]">
                  <div
                    className="h-full rounded-full"
                    style={{ width: `${barWidth}%`, backgroundColor: color }}
                  />
                </div>
              </div>
            )
          })}
        </div>
      ) : (
        <div className="flex h-full items-center justify-center text-sm text-muted-soft">
          {t('ranking.empty')}
        </div>
      )}
    </AnalyticsPanel>
  )
}

function metricValue(item: AnalyticsUsage['breakdowns']['api_key'][number], metric: AnalyticsTrendMetric) {
  switch (metric) {
    case 'tokens': return item.total_tokens
    case 'requests': return item.requests
    case 'latency': return item.avg_latency_ms
    default: return item.cost
  }
}
