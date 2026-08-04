import type { CSSProperties } from 'react'

export const dashboardChartColors = [
  'hsl(190 88% 42%)',
  'hsl(154 70% 38%)',
  'hsl(42 88% 48%)',
  'hsl(218 74% 58%)',
  'hsl(350 72% 58%)',
  'hsl(268 54% 62%)',
]

export const dashboardTooltipStyle: CSSProperties = {
  border: '1px solid hsl(var(--glass-border))',
  borderRadius: 8,
  background: 'hsl(var(--surface-panel) / 0.92)',
  color: 'hsl(var(--foreground))',
  padding: '8px 10px',
  boxShadow: '0 16px 40px rgb(0 0 0 / 0.16)',
  backdropFilter: 'blur(14px)',
  lineHeight: 1.35,
}

export const dashboardTooltipLabelStyle: CSSProperties = {
  color: 'hsl(var(--text-muted-soft))',
  fontSize: 12,
  marginBottom: 4,
}

export const dashboardTooltipItemStyle: CSSProperties = {
  color: 'hsl(var(--foreground))',
  fontSize: 12,
  padding: '1px 0',
}

export function formatDollarTick(value: number) {
  if (value >= 1000) return `$${(value / 1000).toFixed(1)}k`
  return `$${value}`
}

export function formatNumberTick(value: number) {
  if (value >= 1000) return `${(value / 1000).toFixed(1)}k`
  return String(value)
}
