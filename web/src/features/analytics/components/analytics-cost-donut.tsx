import { useEffect, useMemo, useRef, useState } from 'react'
import { Cell, Pie, PieChart, ResponsiveContainer, Tooltip, type TooltipContentProps } from 'recharts'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { AnalyticsUsage } from '@/features/analytics/api/analytics'
import type { AnalyticsBreakdownDimension } from '@/features/analytics/lib/analytics-utils'
import { dashboardChartColors, dashboardTooltipStyle } from '@/features/dashboard/components/chart-style'
import { formatDashboardCurrency, formatDashboardTokens } from '@/features/dashboard/lib/dashboard-utils'
import { AnalyticsPanel, AnalyticsSlashTabs } from './analytics-panel'

// 玫瑰图参数
const ROSE_OUTER_MIN_RATIO = 0.42 // 最小外径不低于最大外径的此比例，防止极端值消失

/**
 * 用对数刻度把值域 [minVal, maxVal] 映射到外径 [outerMin, outerMax]。
 * 对数压缩在极端分布（如 58% vs 0.4%）时比 sqrt 更均匀。
 */
function roseOuterRadius(value: number, minVal: number, maxVal: number, outerMax: number): number {
  if (maxVal <= 0) return outerMax
  const outerMin = outerMax * ROSE_OUTER_MIN_RATIO
  if (minVal >= maxVal) return outerMax
  // log 压缩：log(1 + v/min) / log(1 + max/min)
  const scale =
    Math.log(1 + value / minVal) / Math.log(1 + maxVal / minVal)
  return Math.round(outerMin + (outerMax - outerMin) * scale)
}

type DonutSlice = {
  key: string
  id?: string | null
  name: string
  value: number
  tokens: number
  color: string
  outerRadius: number
}

type AnalyticsCostDonutProps = {
  className?: string
  usage: AnalyticsUsage
  onDrillDown: (dimension: AnalyticsBreakdownDimension, item: { key: string; id?: string | null }) => void
  onClearDrillDown: (dimension: AnalyticsBreakdownDimension) => void
  activeSiteIds: string[]
  activeModelKeys: string[]
  activeApiKeyIds: string[]
}

export function AnalyticsCostDonut({
  className,
  usage,
  onDrillDown,
  onClearDrillDown,
  activeSiteIds,
  activeModelKeys,
  activeApiKeyIds,
}: AnalyticsCostDonutProps) {
  const { t } = useTranslation('analytics')
  const [dimension, setDimension] = useState<AnalyticsBreakdownDimension>('model')
  const bodyRef = useRef<HTMLDivElement>(null)
  const [bodyWidth, setBodyWidth] = useState(0)
  const currency = usage.meta.currency || 'USD'
  const roseSize = bodyWidth > 0 ? Math.min(240, Math.max(160, Math.round(bodyWidth * 0.3))) : 160
  const roseInnerRadius = Math.round(roseSize * 0.2)
  const roseOuterMax = Math.round(roseSize * 0.45)
  const roseCenterLabelSize = Math.min(12, Math.max(10, Math.round(roseSize * 0.05)))
  const roseCenterValueSize = Math.min(14, Math.max(12, Math.round(roseSize * 0.06)))

  useEffect(() => {
    const element = bodyRef.current
    if (!element) return
    const updateWidth = () => setBodyWidth(element.getBoundingClientRect().width)
    updateWidth()
    const observer = new ResizeObserver(updateWidth)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  const slices = useMemo<DonutSlice[]>(() => {
    const items = [...usage.breakdowns[dimension]].sort((a, b) => b.cost - a.cost)
    const all = items.map((item, index) => ({
      key: item.key,
      id: item.id,
      name: item.label,
      value: item.cost,
      tokens: item.total_tokens,
      color: dashboardChartColors[index % dashboardChartColors.length],
      outerRadius: 0,
    }))
    const validSlices = all.filter((s) => s.value > 0)
    if (validSlices.length === 0) return []

    // 对数刻度计算外径，极端分布下小扇形仍可见
    const maxVal = validSlices[0].value
    const minVal = validSlices[validSlices.length - 1].value
    return validSlices.map((s) => ({
      ...s,
      outerRadius: roseOuterRadius(s.value, minVal, maxVal, roseOuterMax),
    }))
  }, [dimension, roseOuterMax, usage.breakdowns])

  const total = slices.reduce((sum, slice) => sum + slice.value, 0)

  const activeKeys = dimension === 'site'
    ? activeSiteIds
    : dimension === 'model'
      ? activeModelKeys
      : activeApiKeyIds
  const hasActive = activeKeys.length > 0

  const activeLabels = useMemo(() => {
    if (!hasActive) return []
    return activeKeys.map((key) => {
      const found = slices.find((s) => s.id === key || s.key === key)
      return found?.name ?? key
    })
  }, [activeKeys, hasActive, slices])

  return (
    <AnalyticsPanel
      className={className}
      title={t('donut.title')}
      description={hasActive ? undefined : t('donut.description')}
      bodyClassName="flex min-h-0 flex-col"
      action={(
        <div className="flex flex-wrap items-center gap-3">
          {hasActive && (
            <span className="flex flex-wrap items-center gap-1 text-xs text-muted-soft">
              {activeLabels.slice(0, 2).map((label) => (
                <span
                  key={label}
                  className="inline-flex max-w-[10rem] items-center gap-1 truncate rounded-full bg-[hsl(var(--surface-subtle))] px-2 py-0.5 text-foreground"
                  title={label}
                >
                  {label}
                </span>
              ))}
              {activeLabels.length > 2 && (
                <span className="rounded-full bg-[hsl(var(--surface-subtle))] px-2 py-0.5 text-foreground">
                  +{activeLabels.length - 2}
                </span>
              )}
              <button
                type="button"
                onClick={() => onClearDrillDown(dimension)}
                className="ml-1 inline-flex shrink-0 items-center gap-0.5 rounded-full bg-[hsl(var(--surface-subtle))] px-1.5 py-0.5 text-xs text-muted-soft hover:text-foreground"
                title={t('donut.clearFilter')}
              >
                <X className="h-3 w-3" />
                {t('donut.clear')}
              </button>
            </span>
          )}
          <AnalyticsSlashTabs
            value={dimension}
            onValueChange={setDimension}
            items={[
              { label: t('dimensions.site'), value: 'site' as const },
              { label: t('dimensions.model'), value: 'model' as const },
              { label: t('dimensions.apiKey'), value: 'api_key' as const },
            ]}
          />
        </div>
      )}
    >
      {slices.length ? (
        <div ref={bodyRef} className="flex min-h-0 flex-1 items-center gap-6">
          <div
            className="relative shrink-0"
            style={{ width: roseSize, height: roseSize }}
          >
            <ResponsiveContainer width="100%" height="100%">
              <PieChart accessibilityLayer={false}>
                <Pie
                  data={slices}
                  dataKey="value"
                  nameKey="name"
                  cx="50%"
                  cy="50%"
                  innerRadius={roseInnerRadius}
                  outerRadius={(dataPoint: DonutSlice) => dataPoint.outerRadius}
                  paddingAngle={slices.length > 8 ? 1 : 2}
                  stroke="hsl(var(--surface-panel))"
                  strokeWidth={1.5}
                  isAnimationActive={true}
                  animationBegin={0}
                  animationDuration={500}
                  animationEasing="ease-out"
                  onClick={(entry) => {
                    const slice = entry as unknown as DonutSlice
                    if (slice.key === 'unknown') return
                    onDrillDown(dimension, { key: slice.key, id: slice.id })
                  }}
                >
                  {slices.map((slice) => (
                    <Cell
                      key={slice.key}
                      fill={slice.color}
                      className={slice.key === 'unknown' ? 'opacity-40' : 'cursor-pointer'}
                    />
                  ))}
                </Pie>
                <Tooltip
                  isAnimationActive={false}
                  wrapperStyle={{ zIndex: 10 }}
                  content={(props) => (
                    <DonutTooltip {...props} currency={currency} total={total} />
                  )}
                />
              </PieChart>
            </ResponsiveContainer>
            {/* 中心总费用：pointer-events-none + z-0，Tooltip 在其上方 */}
            <div className="pointer-events-none absolute inset-0 z-0 flex flex-col items-center justify-center">
              <span className="leading-tight text-muted-soft" style={{ fontSize: roseCenterLabelSize }}>
                {t('donut.total')}
              </span>
              <span
                className="font-semibold tabular-nums leading-tight text-foreground"
                style={{ fontSize: roseCenterValueSize }}
              >
                {formatDashboardCurrency(total, currency)}
              </span>
            </div>
          </div>

          <div className="min-h-0 min-w-0 flex-1 self-center overflow-y-auto pr-2" style={{ maxHeight: roseSize }}>
            <div className="grid grid-cols-1 divide-y divide-[hsl(var(--glass-divider))]">
              {slices.map((slice) => (
                <button
                  key={slice.key}
                  type="button"
                  onClick={() => {
                    if (slice.key === 'unknown') return
                    onDrillDown(dimension, { key: slice.key, id: slice.id })
                  }}
                  className="flex min-w-0 items-center gap-2 py-2 text-left text-xs first:pt-0 last:pb-0 disabled:cursor-default"
                  disabled={slice.key === 'unknown'}
                >
                  <span
                    className="size-2 shrink-0 rounded-[2px]"
                    style={{
                      backgroundColor: slice.color,
                      opacity: slice.key === 'unknown' ? 0.5 : 1,
                    }}
                  />
                  <span className="min-w-0 flex-1 truncate text-foreground" title={slice.name}>
                    {slice.name}
                  </span>
                  <span className="shrink-0 tabular-nums text-muted-soft">
                    {formatDashboardCurrency(slice.value, currency, 2)}
                    {` · ${formatDashboardTokens(slice.tokens)}`}
                    {total > 0 ? ` · ${((slice.value / total) * 100).toFixed(1)}%` : ''}
                  </span>
                </button>
              ))}
            </div>
          </div>
        </div>
      ) : (
        <div className="flex h-full items-center justify-center text-sm text-muted-soft">
          {t('donut.empty')}
        </div>
      )}
    </AnalyticsPanel>
  )
}

function DonutTooltip({ active, payload, currency, total }: TooltipContentProps & { currency: string; total: number }) {
  if (!active || !payload?.length) return null
  const item = payload[0]
  const value = Number(item.value ?? 0)
  const tokens = Number((item.payload as DonutSlice | undefined)?.tokens ?? 0)
  return (
    <div style={dashboardTooltipStyle} className="min-w-[160px] text-xs">
      <div className="mb-1 flex items-center gap-1.5 text-foreground">
        <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: item.payload?.fill ?? item.color }} />
        <span className="truncate font-medium">{item.name}</span>
      </div>
      <div className="tabular-nums text-muted-soft">
        {formatDashboardCurrency(value, currency, 4)}
        {` · ${formatDashboardTokens(tokens)}`}
        {total > 0 ? ` · ${((value / total) * 100).toFixed(1)}%` : ''}
      </div>
    </div>
  )
}
