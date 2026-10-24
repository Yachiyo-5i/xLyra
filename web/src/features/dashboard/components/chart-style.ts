import {
  chartColors,
  chartTooltipItemStyle,
  chartTooltipLabelStyle,
  chartTooltipStyle,
} from '@/components/charts/chart-theme'

// 图表外观统一在 components/charts/chart-theme.ts；这里保留仪表盘与分析页沿用的名字
export const dashboardChartColors = chartColors
export const dashboardTooltipStyle = chartTooltipStyle
export const dashboardTooltipLabelStyle = chartTooltipLabelStyle
export const dashboardTooltipItemStyle = chartTooltipItemStyle

export function formatDollarTick(value: number) {
  if (value >= 1000) return `$${(value / 1000).toFixed(1)}k`
  return `$${value}`
}

export function formatNumberTick(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(1)}k`
  return String(value)
}
