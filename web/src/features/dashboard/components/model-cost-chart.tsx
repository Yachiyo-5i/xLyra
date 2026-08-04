import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  type TooltipContentProps,
  XAxis,
  YAxis,
} from 'recharts'
import {
  dashboardChartColors,
  dashboardTooltipStyle,
  formatDollarTick,
} from './chart-style'
import type { DashboardSeries, DashboardTrendDatum } from './dashboard-types'

type ModelCostChartProps = {
  data: DashboardTrendDatum[]
  series: DashboardSeries[]
  height?: number
}

export function ModelCostChart({ data, series, height = 300 }: ModelCostChartProps) {
  const xAxisInterval = data.length > 60 ? 8 : data.length > 20 ? 2 : 0
  const seriesWithColor = series.map((item, index) => ({
    ...item,
    color: item.color ?? dashboardChartColors[index % dashboardChartColors.length],
  }))

  return (
    <div style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart accessibilityLayer={false} data={data} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
          <CartesianGrid stroke="hsl(var(--glass-border))" vertical={false} />
          <XAxis
            dataKey="date"
            interval={xAxisInterval}
            tickLine={false}
            axisLine={false}
            tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 12 }}
          />
          <YAxis tickFormatter={formatDollarTick} tickLine={false} axisLine={false} width={46} tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 12 }} />
          <Tooltip
            cursor={{ fill: 'hsl(var(--surface-subtle))' }}
            content={(props) => <ModelCostTooltip {...props} series={seriesWithColor} />}
          />
          {seriesWithColor.map((item, index) => (
            <Bar
              key={item.key}
              dataKey={item.key}
              name={item.name}
              stackId="cost"
              fill={item.color}
              radius={index === seriesWithColor.length - 1 ? [5, 5, 0, 0] : [0, 0, 0, 0]}
            />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}

type ModelCostTooltipProps = TooltipContentProps & {
  series: Array<DashboardSeries & { color: string }>
}

function ModelCostTooltip({ active, label, payload, series }: ModelCostTooltipProps) {
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
        color: seriesItem?.color ?? item.color ?? item.fill ?? 'hsl(var(--text-muted-soft))',
        value,
      }
    })
    .filter((item) => item.value > 0)
    .sort((a, b) => b.value - a.value)

  if (items.length === 0) return null

  const total = items.reduce((sum, item) => sum + item.value, 0)

  return (
    <div style={dashboardTooltipStyle} className="min-w-[164px]">
      <div className="mb-2 flex items-center justify-between gap-5 text-xs">
        <span className="text-muted-soft">{label}</span>
        <span className="font-medium tabular-nums text-foreground">${total.toFixed(4)}</span>
      </div>
      <div className="space-y-1">
        {items.map((item) => (
          <div key={item.key} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 text-xs text-foreground">
            <span className="flex min-w-0 items-center gap-1.5">
              <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
              <span className="truncate">{item.name}</span>
            </span>
            <span className="tabular-nums">${item.value.toFixed(4)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
