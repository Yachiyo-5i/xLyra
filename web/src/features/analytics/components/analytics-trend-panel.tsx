import { useMemo, useState } from 'react'
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  type TooltipContentProps,
  XAxis,
  YAxis,
} from 'recharts'
import { useTranslation } from 'react-i18next'
import type { AnalyticsUsage } from '@/features/analytics/api/analytics'
import {
  buildTrendChart,
  formatCompactTick,
  formatHourLabel,
  formatLatencyMs,
  formatShortDate,
  formatTrendMetricValue,
  type AnalyticsChartType,
  type AnalyticsTrendDatum,
  type AnalyticsTrendDimension,
  type AnalyticsTrendMetric,
  type AnalyticsTrendSeries,
} from '@/features/analytics/lib/analytics-utils'
import {
  dashboardChartColors,
  dashboardTooltipStyle,
  formatDollarTick,
} from '@/features/dashboard/components/chart-style'
import { AnalyticsPanel, AnalyticsSlashTabs } from './analytics-panel'

type AnalyticsTrendPanelProps = {
  className?: string
  usage: AnalyticsUsage
  metric: AnalyticsTrendMetric
  dimension: AnalyticsTrendDimension
  onMetricChange: (metric: AnalyticsTrendMetric) => void
  onDimensionChange: (dimension: AnalyticsTrendDimension) => void
}

export function AnalyticsTrendPanel({
  className,
  usage,
  metric,
  dimension,
  onMetricChange,
  onDimensionChange,
}: AnalyticsTrendPanelProps) {
  const { t } = useTranslation('analytics')
  const [chartType, setChartType] = useState<AnalyticsChartType>('stacked-area')

  const lineOnly = metric === 'latency'
  const effectiveChartType: AnalyticsChartType = lineOnly ? 'line' : chartType

  const isHour = usage.meta.granularity === 'hour'
  const xTickFormatter = isHour ? formatHourLabel : formatShortDate

  const { data, series } = useMemo(
    () => buildTrendChart(usage, metric),
    [usage, metric],
  )
  const seriesWithColor = series.map((item, index) => ({
    ...item,
    color: dashboardChartColors[index % dashboardChartColors.length],
  }))

  const formatValue = (value: number) => formatTrendMetricValue(value, metric, usage.meta.currency)
  const tickFormatter = (value: number) => {
    if (metric === 'cost') return formatDollarTick(value)
    if (metric === 'latency') return formatLatencyMs(value)
    return formatCompactTick(value)
  }

  const hasData = series.length > 0 && data.length > 0

  return (
    <AnalyticsPanel
      className={className}
      title={(
        <AnalyticsSlashTabs
          value={metric}
          onValueChange={onMetricChange}
          items={[
            { label: t('trend.metrics.cost'), value: 'cost' as const },
            { label: t('trend.metrics.tokens'), value: 'tokens' as const },
            { label: t('trend.metrics.requests'), value: 'requests' as const },
            { label: t('trend.metrics.latency'), value: 'latency' as const },
          ]}
        />
      )}
      description={isHour
        ? t('trend.hourDescription', { date: usage.meta.from })
        : t('trend.description', { from: usage.meta.from, to: usage.meta.to })}
      action={(
        <div className="flex flex-wrap items-center gap-4">
          <AnalyticsSlashTabs
            value={dimension}
            onValueChange={onDimensionChange}
            items={[
              { label: t('trend.dimensions.site'), value: 'site' as const },
              { label: t('trend.dimensions.model'), value: 'model' as const },
            ]}
          />
          {!lineOnly ? (
            <>
              <div className="h-4 w-px bg-[hsl(var(--glass-divider))]" />
              <AnalyticsSlashTabs
                value={chartType}
                onValueChange={setChartType}
                items={[
                  { label: t('trend.chartTypes.stackedArea'), value: 'stacked-area' as const },
                  { label: t('trend.chartTypes.percentArea'), value: 'percent-area' as const },
                  { label: t('trend.chartTypes.stackedBar'), value: 'stacked-bar' as const },
                  { label: t('trend.chartTypes.line'), value: 'line' as const },
                ]}
              />
            </>
          ) : null}
        </div>
      )}
    >
      {hasData ? (
        <TrendChart
          data={data}
          series={seriesWithColor}
          chartType={effectiveChartType}
          tickFormatter={tickFormatter}
          formatValue={formatValue}
          xTickFormatter={xTickFormatter}
          isHour={isHour}
        />
      ) : (
        <div className="flex h-full items-center justify-center text-sm text-muted-soft">
          {t('trend.empty')}
        </div>
      )}
    </AnalyticsPanel>
  )
}

type ColoredSeries = AnalyticsTrendSeries & { color: string }

function TrendChart({
  data,
  series,
  chartType,
  tickFormatter,
  formatValue,
  xTickFormatter,
  isHour,
}: {
  data: AnalyticsTrendDatum[]
  series: ColoredSeries[]
  chartType: AnalyticsChartType
  tickFormatter: (value: number) => string
  formatValue: (value: number) => string
  xTickFormatter: (value: string) => string
  isHour: boolean
}) {
  const xAxisInterval = isHour
    ? 0
    : data.length > 60 ? Math.floor(data.length / 10) : data.length > 20 ? 2 : 0
  const margin = { top: 8, right: 8, bottom: 0, left: 0 }

  const xAxis = (
    <XAxis
      dataKey="date"
      interval={xAxisInterval}
      tickFormatter={xTickFormatter}
      tickLine={false}
      axisLine={false}
      tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 12 }}
    />
  )
  const yAxis = (
    <YAxis
      tickFormatter={tickFormatter}
      tickLine={false}
      axisLine={false}
      width={56}
      tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 12 }}
    />
  )
  const grid = <CartesianGrid stroke="hsl(var(--glass-border))" vertical={false} />
  const tooltip = (
    <Tooltip
      cursor={{ fill: 'hsl(var(--surface-subtle))' }}
      content={(props) => (
        <TrendTooltip {...props} series={series} formatValue={formatValue} />
      )}
    />
  )
  const legend = (
    <Legend
      iconType="square"
      iconSize={8}
      wrapperStyle={{ fontSize: 12, paddingTop: 4, color: 'hsl(var(--text-muted-soft))' }}
      formatter={(value) => (
        <span style={{ color: 'hsl(var(--foreground))' }}>{value}</span>
      )}
    />
  )

  if (chartType === 'line') {
    return (
      <ResponsiveContainer width="100%" height="100%">
        <LineChart accessibilityLayer={false} data={data} margin={margin}>
          {grid}
          {xAxis}
          {yAxis}
          {tooltip}
          {legend}
          {series.map((item) => (
            <Line
              key={item.key}
              type="monotone"
              dataKey={item.key}
              name={item.name}
              stroke={item.color}
              strokeWidth={2}
              dot={false}
              connectNulls
              activeDot={{ r: 4 }}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    )
  }

  if (chartType === 'stacked-bar') {
    return (
      <ResponsiveContainer width="100%" height="100%">
        <BarChart accessibilityLayer={false} data={data} margin={margin}>
          {grid}
          {xAxis}
          {yAxis}
          {tooltip}
          {legend}
          {series.map((item, index) => (
            <Bar
              key={item.key}
              dataKey={item.key}
              name={item.name}
              stackId="analytics"
              fill={item.color}
              radius={index === series.length - 1 ? [5, 5, 0, 0] : [0, 0, 0, 0]}
            />
          ))}
        </BarChart>
      </ResponsiveContainer>
    )
  }

  const percent = chartType === 'percent-area'
  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart
        accessibilityLayer={false}
        data={data}
        margin={margin}
        stackOffset={percent ? 'expand' : 'none'}
      >
        {grid}
        {xAxis}
        <YAxis
          tickFormatter={percent ? (value: number) => `${Math.round(value * 100)}%` : tickFormatter}
          tickLine={false}
          axisLine={false}
          width={56}
          domain={percent ? [0, 1] : undefined}
          tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 12 }}
        />
        {tooltip}
        {legend}
        {series.map((item) => (
          <Area
            key={item.key}
            type="monotone"
            dataKey={item.key}
            name={item.name}
            stackId="analytics"
            stroke={item.color}
            fill={item.color}
            fillOpacity={0.32}
          />
        ))}
      </AreaChart>
    </ResponsiveContainer>
  )
}

type TrendTooltipProps = TooltipContentProps & {
  series: ColoredSeries[]
  formatValue: (value: number) => string
}

function TrendTooltip({ active, label, payload, series, formatValue }: TrendTooltipProps) {
  if (!active || !payload?.length) return null

  const seriesByKey = new Map(series.map((item) => [item.key, item]))
  const items = payload
    .map((item) => {
      const raw = item.value
      // 堆叠面积图中 recharts 传入的 value 是 [base, top] 数组，取差值得到该系列自身的值
      let value: number | null = null
      if (typeof raw === 'number') {
        value = raw
      } else if (Array.isArray(raw) && raw.length === 2 && typeof raw[0] === 'number' && typeof raw[1] === 'number') {
        value = raw[1] - raw[0]
      }
      const dataKey = String(item.dataKey ?? '')
      const seriesItem = seriesByKey.get(dataKey)
      return {
        key: dataKey,
        name: seriesItem?.name ?? String(item.name ?? dataKey),
        color: seriesItem?.color ?? item.color ?? 'hsl(var(--text-muted-soft))',
        value,
      }
    })
    .filter((item): item is typeof item & { value: number } => item.value !== null && item.value > 0)
    .sort((a, b) => b.value - a.value)

  if (!items.length) return null
  const total = items.reduce((sum, item) => sum + item.value, 0)

  return (
    <div style={dashboardTooltipStyle} className="min-w-[164px] max-w-[280px]">
      <div className="mb-2 flex items-center justify-between gap-5 text-xs">
        <span className="text-muted-soft">{label}</span>
        <span className="font-medium tabular-nums text-foreground">{formatValue(total)}</span>
      </div>
      <div className="space-y-1">
        {items.map((item) => (
          <div key={item.key} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 text-xs text-foreground">
            <span className="flex min-w-0 items-center gap-1.5">
              <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
              <span className="truncate">{item.name}</span>
            </span>
            <span className="tabular-nums">
              {formatValue(item.value)}
              {total > 0 ? ` · ${((item.value / total) * 100).toFixed(1)}%` : ''}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
