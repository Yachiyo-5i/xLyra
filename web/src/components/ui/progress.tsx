import * as React from 'react'
import * as ProgressPrimitive from '@radix-ui/react-progress'
import { cn } from '@/lib/utils'

type ProgressVariant = 'auto' | 'success' | 'info' | 'warning' | 'danger' | 'accent' | 'muted'

type ProgressThreshold = {
  max: number
  variant: Exclude<ProgressVariant, 'auto'>
}

type ProgressProps = React.ComponentPropsWithoutRef<typeof ProgressPrimitive.Root> & {
  value?: number | null
  max?: number
  variant?: ProgressVariant
  thresholds?: ProgressThreshold[]
  showValue?: boolean
  valueLabel?: string
  indicatorClassName?: string
}

const defaultThresholds: ProgressThreshold[] = [
  { max: 39, variant: 'danger' },
  { max: 69, variant: 'warning' },
  { max: 89, variant: 'info' },
  { max: 100, variant: 'success' },
]

const progressVariantClassName: Record<Exclude<ProgressVariant, 'auto'>, string> = {
  success: 'bg-[hsl(var(--badge-success-text))]',
  info: 'bg-[hsl(var(--badge-info-text))]',
  warning: 'bg-[hsl(var(--badge-warning-text))]',
  danger: 'bg-[hsl(var(--destructive))]',
  accent: 'bg-primary',
  muted: 'bg-[hsl(var(--text-muted-soft))]',
}

function clampProgress(value: number, max: number) {
  if (!Number.isFinite(value) || !Number.isFinite(max) || max <= 0) {
    return 0
  }

  return Math.min(Math.max((value / max) * 100, 0), 100)
}

function resolveProgressVariant(
  percentage: number,
  variant: ProgressVariant,
  thresholds: ProgressThreshold[],
) {
  if (variant !== 'auto') return variant

  return thresholds.find((threshold) => percentage <= threshold.max)?.variant ?? 'success'
}

const Progress = React.forwardRef<
  React.ElementRef<typeof ProgressPrimitive.Root>,
  ProgressProps
>(
  (
    {
      className,
      value = 0,
      max = 100,
      variant = 'auto',
      thresholds = defaultThresholds,
      showValue = false,
      valueLabel,
      indicatorClassName,
      ...props
    },
    ref,
  ) => {
    const safeValue = value ?? 0
    const percentage = clampProgress(safeValue, max)
    const resolvedVariant = resolveProgressVariant(percentage, variant, thresholds)
    const displayValue = valueLabel ?? `${Math.round(percentage)}%`

    return (
      <ProgressPrimitive.Root
        ref={ref}
        value={safeValue}
        max={max}
        className={cn(
          'relative h-2.5 w-full overflow-hidden rounded-full border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))]',
          className,
        )}
        {...props}
      >
        <ProgressPrimitive.Indicator
          className={cn(
            'h-full rounded-full transition-[transform,background-color] duration-500 ease-out',
            progressVariantClassName[resolvedVariant],
            indicatorClassName,
          )}
          style={{ transform: `translateX(-${100 - percentage}%)` }}
        />
        {showValue ? (
          <span className="pointer-events-none absolute inset-0 flex items-center justify-center text-[10px] font-semibold text-foreground">
            {displayValue}
          </span>
        ) : null}
      </ProgressPrimitive.Root>
    )
  },
)
Progress.displayName = ProgressPrimitive.Root.displayName

export { Progress, type ProgressThreshold, type ProgressVariant }
