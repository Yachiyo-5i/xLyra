import type { TrafficFlowUsageTotal } from '@/features/traffic-flow/api/traffic-flow'

export function sortUsageTotals(usage: Record<string, TrafficFlowUsageTotal>) {
  return Object.values(usage).sort((left, right) => right.total_tokens - left.total_tokens || left.name.localeCompare(right.name))
}

export function formatCompactTokens(value: number) {
  if (value < 1000) return value.toString()
  const units = [{ threshold: 1_000_000_000, suffix: 'B' }, { threshold: 1_000_000, suffix: 'M' }, { threshold: 1000, suffix: 'K' }]
  const unit = units.find((candidate) => value >= candidate.threshold) ?? units[units.length - 1]
  const scaled = value / unit.threshold
  const decimals = scaled >= 100 ? 0 : scaled >= 10 ? 1 : 2
  return `${Number(scaled.toFixed(decimals))}${unit.suffix}`
}

export function formatWindowTime(value: Date, withDate: boolean) {
  const pad = (unit: number) => String(unit).padStart(2, '0')
  const time = `${pad(value.getHours())}:${pad(value.getMinutes())}:${pad(value.getSeconds())}`
  return withDate ? `${pad(value.getMonth() + 1)}-${pad(value.getDate())} ${time}` : time
}

export function formatPaddedCount(value: number) {
  return Math.max(0, value).toString().padStart(2, '0')
}

export function formatDuration(ms: number) {
  const seconds = Math.max(0, Math.floor(ms / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${String(seconds % 60).padStart(2, '0')}s`
}

export function formatLatency(ms: number) {
  if (!Number.isFinite(ms) || ms < 0) return '—'
  if (ms >= 1000) {
    const seconds = ms / 1000
    return `${Number(seconds.toFixed(seconds >= 10 ? 0 : 1))}s`
  }
  return `${Math.round(ms)}ms`
}

export function formatPercent(value: number) {
  if (!Number.isFinite(value)) return '—'
  const percent = value * 100
  if (percent >= 99.95) return '100%'
  if (percent >= 10) return `${Math.round(percent)}%`
  return `${percent.toFixed(1)}%`
}
