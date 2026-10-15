import { chartCursorFill, chartGridStroke, chartTick } from '@/components/charts/chart-theme'
import {
  Bar,
  BarChart,
  CartesianGrid,
  LabelList,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { useTranslation } from 'react-i18next'
import {
  dashboardChartColors,
  dashboardTooltipItemStyle,
  dashboardTooltipLabelStyle,
  dashboardTooltipStyle,
  formatDollarTick,
} from './chart-style'
import type { SiteCostDatum } from './dashboard-types'

type SiteCostChartProps = {
  data: SiteCostDatum[]
  height?: number
  compactYAxis?: boolean
}

type SiteCostTickProps = {
  x?: number
  y?: number
  payload?: {
    value?: string
  }
}

export function SiteCostChart({ data, height = 300, compactYAxis = false }: SiteCostChartProps) {
  const { t } = useTranslation('dashboard')
  const yAxisWidth = compactYAxis ? 76 : 112

  return (
    <div style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart accessibilityLayer={false} data={data} layout="vertical" margin={{ top: 8, right: 48, bottom: 0, left: compactYAxis ? 0 : 8 }}>
          <CartesianGrid stroke={chartGridStroke} horizontal={false} />
          <XAxis
            type="number"
            tickFormatter={formatDollarTick}
            tickLine={false}
            axisLine={false}
            tick={chartTick}
          />
          <YAxis
            type="category"
            dataKey="site"
            width={yAxisWidth}
            tickLine={false}
            axisLine={false}
            tick={compactYAxis ? <CompactSiteCostTick /> : chartTick}
          />
          <Tooltip
            cursor={{ fill: chartCursorFill }}
            contentStyle={dashboardTooltipStyle}
            itemStyle={dashboardTooltipItemStyle}
            labelStyle={dashboardTooltipLabelStyle}
            formatter={(value) => [`$${Number(value).toFixed(4)}`, t('charts.cost')]}
          />
          <Bar dataKey="cost" name={t('charts.cost')} fill={dashboardChartColors[1]} radius={[0, 5, 5, 0]}>
            <LabelList
              dataKey="cost"
              position="right"
              formatter={(value) => `$${Number(value ?? 0).toFixed(2)}`}
              fill="var(--label-tertiary)"
              fontSize={12}
            />
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}

function CompactSiteCostTick({ x = 0, y = 0, payload }: SiteCostTickProps) {
  const lines = wrapSiteName(payload?.value ?? '')

  return (
    <text x={x - 8} y={y} textAnchor="end" fill="var(--label-tertiary)" fontSize={11}>
      {lines.map((line, index) => (
        <tspan key={`${line}-${index}`} x={x - 8} dy={index === 0 ? -(lines.length - 1) * 6 + 4 : 13}>
          {line}
        </tspan>
      ))}
    </text>
  )
}

function wrapSiteName(name: string) {
  const normalized = name.trim()
  if (normalized.length <= 9) return [normalized]

  const firstLine = normalized.slice(0, 9)
  const secondLine = normalized.slice(9, 18)
  if (normalized.length <= 18) return [firstLine, secondLine]

  return [firstLine, `${secondLine.slice(0, 8)}…`]
}
