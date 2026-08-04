import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
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
  const tooltipId = useId()
  const triggerRef = useRef<HTMLDivElement | null>(null)
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState<{ left: number; top: number } | null>(null)

  function openFromElement(element: HTMLElement) {
    if (disabled) return
    const rect = element.getBoundingClientRect()
    const width = columns.length > 1 ? 420 : 300
    const estimatedHeight = 216
    const left = Math.min(Math.max(12, rect.right - width), Math.max(12, window.innerWidth - width - 12))
    const top = rect.bottom + 8 + estimatedHeight <= window.innerHeight
      ? rect.bottom + 8
      : Math.max(12, rect.top - estimatedHeight - 8)
    setPosition({ left, top })
    setOpen(true)
  }

  useEffect(() => {
    if (!open) return
    function closeOnPointerDown(event: PointerEvent) {
      if (triggerRef.current?.contains(event.target as Node)) return
      setOpen(false)
    }
    function closeOnViewportChange() {
      setOpen(false)
    }
    document.addEventListener('pointerdown', closeOnPointerDown)
    window.addEventListener('resize', closeOnViewportChange)
    window.addEventListener('scroll', closeOnViewportChange, true)
    return () => {
      document.removeEventListener('pointerdown', closeOnPointerDown)
      window.removeEventListener('resize', closeOnViewportChange)
      window.removeEventListener('scroll', closeOnViewportChange, true)
    }
  }, [open])

  const tooltip = open && position && !disabled && typeof document !== 'undefined'
    ? createPortal(
        <div
          id={tooltipId}
          role="tooltip"
          className={cn(
            'glass-panel-strong pointer-events-none fixed z-[170] rounded-lg border border-[hsl(var(--glass-border))] p-3 text-xs text-foreground shadow-xl',
            columns.length > 1 ? 'w-[min(420px,calc(100vw-24px))]' : 'w-[min(300px,calc(100vw-24px))]',
          )}
          style={position}
        >
          <div className={cn('grid items-center gap-x-5 gap-y-2 tabular-nums', columns.length > 1 ? 'grid-cols-[minmax(0,1fr)_auto_auto]' : 'grid-cols-[minmax(0,1fr)_auto]')}>
            {columns.length > 1 ? <span /> : null}
            {columns.length > 1 ? columns.map((column, index) => <strong key={`${column.label ?? ''}-${index}`} className="text-right font-medium text-muted-soft">{column.label}</strong>) : null}
            <UsageRow label={labels.total} values={columns.map(({ usage }) => formatInteger(usage.total))} />
            <UsageRow label={labels.input} values={columns.map(({ usage }) => formatInteger(usage.input))} />
            <UsageRow label={labels.output} values={columns.map(({ usage }) => formatInteger(usage.output))} />
            <UsageRow label={labels.cached} values={columns.map(({ usage }) => formatInteger(usage.cached))} />
            <UsageRow label={labels.hitRate} values={columns.map(({ usage }) => formatCacheHitRate(usage))} />
          </div>
        </div>,
        document.body,
      )
    : null

  return (
    <>
      <div
        ref={triggerRef}
        className={cn(!disabled && 'inline-flex', className)}
        tabIndex={disabled ? undefined : 0}
        aria-describedby={open && !disabled ? tooltipId : undefined}
        onMouseEnter={(event) => openFromElement(event.currentTarget)}
        onMouseLeave={() => setOpen(false)}
        onFocus={(event) => openFromElement(event.currentTarget)}
        onBlur={() => setOpen(false)}
        onClick={(event) => openFromElement(event.currentTarget)}
      >
        {children}
      </div>
      {tooltip}
    </>
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
