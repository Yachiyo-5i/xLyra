import type {
  AnalyticsSeriesPoint,
  AnalyticsUsage,
} from '@/features/analytics/api/analytics'
import { formatCompactNumber, formatDashboardCurrency } from '@/features/dashboard/lib/dashboard-utils'

export type AnalyticsRangePreset = 'today' | 'yesterday' | '7d' | '30d' | '90d' | 'all' | 'custom'

export type AnalyticsTrendMetric = 'cost' | 'tokens' | 'requests' | 'latency'

export type AnalyticsTrendDimension = 'site' | 'model'

export type AnalyticsChartType = 'stacked-area' | 'percent-area' | 'stacked-bar' | 'line'

export type AnalyticsBreakdownDimension = 'site' | 'model' | 'api_key'

export type AnalyticsTrendSeries = {
  key: string
  name: string
}

export type AnalyticsTrendDatum = {
  date: string
  [seriesKey: string]: string | number | null
}

export function formatDateInput(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

export function parseDateInputToDate(value: string | undefined | null): Date | undefined {
  if (!value) return undefined
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return undefined
  const date = new Date(year, month - 1, day)
  return Number.isNaN(date.getTime()) ? undefined : date
}

export function presetRange(preset: Exclude<AnalyticsRangePreset, 'custom'>) {
  const to = new Date()
  if (preset === 'all') {
    // 远端会 clamp 到支持的最大范围，这里给一个足够早的起点即可。
    return { from: '1970-01-01', to: formatDateInput(to) }
  }
  if (preset === 'yesterday') to.setDate(to.getDate() - 1)
  const from = new Date(to)
  const offset = preset === 'today' || preset === 'yesterday' ? 0 : preset === '7d' ? 6 : preset === '30d' ? 29 : 89
  from.setDate(from.getDate() - offset)
  return { from: formatDateInput(from), to: formatDateInput(to) }
}

export function defaultAnalyticsRange() {
  return presetRange('30d')
}

export function enumerateHours(date: string): string[] {
  const hours: string[] = []
  for (let h = 0; h < 24; h++) {
    hours.push(`${date} ${String(h).padStart(2, '0')}:00`)
  }
  return hours
}

export function enumerateDates(from: string, to: string): string[] {
  const dates: string[] = []
  const start = parseDateInput(from)
  const end = parseDateInput(to)
  if (!start || !end || start > end) return dates
  const cursor = new Date(start)
  for (let guard = 0; guard < 4000 && cursor <= end; guard += 1) {
    dates.push(formatDateInput(cursor))
    cursor.setDate(cursor.getDate() + 1)
  }
  return dates
}

function parseDateInput(value: string) {
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return null
  const date = new Date(year, month - 1, day)
  return Number.isNaN(date.getTime()) ? null : date
}

export function formatShortDate(value: string) {
  return value.length >= 10 ? value.slice(5) : value
}

export function formatHourLabel(value: string): string {
  // "2026-08-14 15:00" → "15:00"
  const parts = value.split(' ')
  return parts[1] ?? value
}

export function formatLatencyMs(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(2)} s`
  return `${Math.round(value)} ms`
}

/** Compact K/M/B formatter for chart axis ticks (tokens, requests). */
export function formatCompactTick(value: number) {
  const abs = Math.abs(value)
  if (abs >= 1_000_000_000) return `${(value / 1_000_000_000).toFixed(1)}B`
  if (abs >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (abs >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return String(Math.round(value))
}

export function formatTrendMetricValue(value: number, metric: AnalyticsTrendMetric, currency: string) {
  switch (metric) {
    case 'cost':
      return formatDashboardCurrency(value, currency, 4)
    case 'latency':
      return formatLatencyMs(value)
    default:
      return formatCompactNumber(value)
  }
}

export function pointMetricValue(point: AnalyticsSeriesPoint, metric: AnalyticsTrendMetric) {
  switch (metric) {
    case 'cost':
      return point.cost
    case 'tokens':
      return point.total_tokens
    case 'requests':
      return point.requests
    case 'latency':
      return point.requests > 0 ? point.avg_latency_ms : null
  }
}

export function buildTrendChart(
  usage: AnalyticsUsage,
  metric: AnalyticsTrendMetric,
): { data: AnalyticsTrendDatum[]; series: AnalyticsTrendSeries[] } {
  const isHour = usage.meta.granularity === 'hour'

  // hour 粒度：只保留实际有数据的时间槽，不补全 24 小时空槽
  // day 粒度：枚举完整日期序列（保留空日期以维持趋势连续性）
  let slots: string[]
  if (isHour) {
    const activeSlots = new Set<string>()
    for (const item of usage.series) {
      for (const point of item.points) {
        activeSlots.add(point.date)
      }
    }
    slots = [...activeSlots].sort()
  } else {
    slots = enumerateDates(usage.meta.from, usage.meta.to)
  }

  const series = usage.series.map((item, index) => ({ key: `s${index}`, name: item.label }))
  const pointsBySeries = usage.series.map((item) => {
    const map = new Map<string, AnalyticsSeriesPoint>()
    for (const point of item.points) map.set(point.date, point)
    return map
  })
  const nullable = metric === 'latency'
  const data = slots.map((date) => {
    const row: AnalyticsTrendDatum = { date }
    pointsBySeries.forEach((points, index) => {
      const point = points.get(date)
      row[`s${index}`] = point ? pointMetricValue(point, metric) : nullable ? null : 0
    })
    return row
  })
  return { data, series }
}
