import { chartGridStroke, chartTick } from '@/components/charts/chart-theme'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  type TooltipContentProps,
  XAxis,
  YAxis,
} from 'recharts'
import { useTranslation } from 'react-i18next'
import {
  dashboardChartColors,
  dashboardTooltipStyle,
  formatNumberTick,
} from './chart-style'
import type { DashboardSeries, DashboardTrendDatum } from './dashboard-types'

type ModelRequestsChartProps = {
  data: DashboardTrendDatum[]
  series: DashboardSeries[]
  height?: number
}

export function ModelRequestsChart({ data, series, height = 300 }: ModelRequestsChartProps) {
  const { t } = useTranslation('dashboard')
  const seriesWithColor = series.map((item, index) => ({
    ...item,
    color: item.color ?? dashboardChartColors[index % dashboardChartColors.length],
  }))

  return (
    <div style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart accessibilityLayer={false} data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid stroke={chartGridStroke} vertical={false} />
          <XAxis
            dataKey="date"
            interval="preserveStartEnd"
            minTickGap={12}
            tickLine={false}
            axisLine={false}
            tick={chartTick}
          />
          <YAxis tickFormatter={formatNumberTick} tickLine={false} axisLine={false} width={46} tick={chartTick} />
          <Tooltip
            content={(props) => <ModelRequestsTooltip {...props} series={seriesWithColor} unit={t('charts.times')} />}
          />
          {seriesWithColor.map((item) => (
            <Line
              key={item.key}
              type="monotone"
              dataKey={item.key}
              name={item.name}
              stroke={item.color}
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    </div>
  )
}

type ModelRequestsTooltipProps = TooltipContentProps & {
  series: Array<DashboardSeries & { color: string }>
  unit: string
}

function ModelRequestsTooltip({ active, label, payload, series, unit }: ModelRequestsTooltipProps) {
  if (!active || !payload?.length) return null

  const seriesByKey = new Map(series.map((item) => [item.key, item]))
  const items = payload
    .map((item) => {
      const value = Number(item.value ?? 0)
      const dataKey = String(item.dataKey ?? '')
      const seriesItem = seriesByKey.get(dataKey)
      return {
        key: dataKey,
        name: seriesItem?.name ?? String(item.name ?? dataKey),
        color: seriesItem?.color ?? item.color ?? item.stroke ?? 'var(--label-tertiary)',
        value,
      }
    })
    .filter((item) => item.value > 0)
    .sort((a, b) => b.value - a.value)

  if (items.length === 0) return null

  const total = items.reduce((sum, item) => sum + item.value, 0)

  return (
    <div style={dashboardTooltipStyle} className="min-w-[164px]">
      <div className="mb-2 flex items-center justify-between gap-5 text-footnote">
        <span className="text-label-2">{label}</span>
        <span className="font-medium tabular-nums text-label">
          {total.toLocaleString()} {unit}
        </span>
      </div>
      <div className="space-y-1">
        {items.map((item) => (
          <div key={item.key} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 text-footnote text-label">
            <span className="flex min-w-0 items-center gap-1.5">
              <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
              <span className="truncate">{item.name}</span>
            </span>
            <span className="tabular-nums">
              {item.value.toLocaleString()} {unit}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
