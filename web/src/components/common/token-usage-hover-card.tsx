import type { ReactNode } from 'react'
import { HoverDetails } from '@/components/common/hover-details'
import { cn } from '@/lib/utils'

export type TokenUsageBreakdown = {
  total: number
  input: number
  output: number
  cached: number
}

export type TokenUsageColumn = {
  label?: string
  usage: TokenUsageBreakdown
}

export type TokenUsageLabels = {
  total: string
  input: string
  output: string
  cached: string
  hitRate: string
}

type TokenUsageHoverCardProps = {
  children: ReactNode
  columns: TokenUsageColumn[]
  labels: TokenUsageLabels
  disabled?: boolean
  className?: string
}

export function TokenUsageHoverCard({ children, columns, labels, disabled = false, className }: TokenUsageHoverCardProps) {
  return (
    <HoverDetails
      title={labels.total}
      disabled={disabled}
      className={className}
      contentClassName={columns.length > 1 ? 'w-[420px]' : 'w-[300px]'}
      content={
        <div className={cn('grid items-center gap-x-5 gap-y-2 tabular-nums', columns.length > 1 ? 'grid-cols-[minmax(0,1fr)_auto_auto]' : 'grid-cols-[minmax(0,1fr)_auto]')}>
          {columns.length > 1 ? <span /> : null}
          {columns.length > 1 ? columns.map((column, index) => <strong key={`${column.label ?? ''}-${index}`} className="text-right font-medium text-muted-soft">{column.label}</strong>) : null}
          <UsageRow label={labels.total} values={columns.map(({ usage }) => formatInteger(usage.total))} />
          <UsageRow label={labels.input} values={columns.map(({ usage }) => formatInteger(usage.input))} />
          <UsageRow label={labels.output} values={columns.map(({ usage }) => formatInteger(usage.output))} />
          <UsageRow label={labels.cached} values={columns.map(({ usage }) => formatInteger(usage.cached))} />
          <UsageRow label={labels.hitRate} values={columns.map(({ usage }) => formatCacheHitRate(usage))} />
        </div>
      }
    >
      {children}
    </HoverDetails>
  )
}

function UsageRow({ label, values }: { label: string; values: string[] }) {
  return (
    <>
      <span className="text-muted-soft">{label}</span>
      {values.map((value, index) => <strong key={`${value}-${index}`} className="text-right font-semibold">{value}</strong>)}
    </>
  )
}

function formatInteger(value: number) {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(value)
}

function formatCacheHitRate(usage: TokenUsageBreakdown) {
  if (usage.input <= 0) return '--'
  return new Intl.NumberFormat(undefined, { style: 'percent', maximumFractionDigits: 1 }).format(usage.cached / usage.input)
}
