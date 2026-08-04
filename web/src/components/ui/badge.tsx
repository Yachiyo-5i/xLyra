import { cva, type VariantProps } from 'class-variance-authority'
import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex shrink-0 items-center whitespace-nowrap rounded-full border px-2.5 py-1 text-xs font-medium tracking-wide',
  {
    variants: {
      variant: {
        default:
          'text-[hsl(var(--badge-neutral-text))] bg-[hsl(var(--badge-neutral-bg))] border-[hsl(var(--badge-neutral-border))]',
        neutral:
          'text-[hsl(var(--badge-neutral-text))] bg-[hsl(var(--badge-neutral-bg))] border-[hsl(var(--badge-neutral-border))]',
        secondary:
          'text-[hsl(var(--badge-info-text))] bg-[hsl(var(--badge-info-bg))] border-[hsl(var(--badge-info-border))]',
        outline:
          'text-[hsl(var(--badge-neutral-text))] bg-[hsl(var(--badge-neutral-bg))] border-[hsl(var(--badge-neutral-border))]',
        success:
          'text-[hsl(var(--badge-success-text))] bg-[hsl(var(--badge-success-bg))] border-[hsl(var(--badge-success-border))]',
        info:
          'text-[hsl(var(--badge-info-text))] bg-[hsl(var(--badge-info-bg))] border-[hsl(var(--badge-info-border))]',
        warning:
          'text-[hsl(var(--badge-warning-text))] bg-[hsl(var(--badge-warning-bg))] border-[hsl(var(--badge-warning-border))]',
        accent:
          'text-[hsl(var(--badge-accent-text))] bg-[hsl(var(--badge-accent-bg))] border-[hsl(var(--badge-accent-border))]',
      },
    },
    defaultVariants: {
      variant: 'neutral',
    },
  },
)

export interface BadgeProps extends HTMLAttributes<HTMLDivElement>, VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <div className={cn(badgeVariants({ variant }), className)} {...props} />
}
