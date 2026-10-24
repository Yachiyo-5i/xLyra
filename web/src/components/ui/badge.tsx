import { cva, type VariantProps } from 'class-variance-authority'
import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

/**
 * 标签：胶囊、系统色 16% 底 + 系统色文字，高 20、Caption 1。
 * 只用颜色区分不可靠，所以必须带文字；`dot` 在文字前加 6px 圆点。
 */
const badgeVariants = cva(
  'inline-flex h-5 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full px-2 text-caption-1',
  {
    variants: {
      variant: {
        neutral: 'bg-fill-3 text-label-2',
        default: 'bg-fill-3 text-label-2',
        outline: 'border border-separator bg-transparent text-label-2',
        accent: 'bg-tint/14 text-tint',
        secondary: 'bg-info/16 text-info',
        info: 'bg-info/16 text-info',
        success: 'bg-success/16 text-success',
        warning: 'bg-warning/16 text-warning',
        danger: 'bg-danger/16 text-danger',
      },
    },
    defaultVariants: {
      variant: 'neutral',
    },
  },
)

export interface BadgeProps extends HTMLAttributes<HTMLDivElement>, VariantProps<typeof badgeVariants> {
  dot?: boolean
}

export function Badge({ className, variant, dot = false, children, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props}>
      {dot ? <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full bg-current" /> : null}
      {children}
    </div>
  )
}
