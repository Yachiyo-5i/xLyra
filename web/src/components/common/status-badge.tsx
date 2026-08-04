import type { HTMLAttributes } from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

const statusBadgeVariants = cva('', {
  variants: {
    status: {
      healthy: '',
      success: '',
      warning: '',
      error: 'border-[hsl(var(--destructive)/0.25)] bg-[hsl(var(--destructive)/0.12)] text-[hsl(var(--destructive))]',
      syncing: '',
      idle: '',
      disabled: 'opacity-70',
    },
  },
  defaultVariants: {
    status: 'idle',
  },
})

type StatusBadgeProps = HTMLAttributes<HTMLDivElement> &
  VariantProps<typeof statusBadgeVariants> & {
    children: string
  }

export function StatusBadge({
  className,
  status,
  children,
  ...props
}: StatusBadgeProps) {
  const variant =
    status === 'healthy' || status === 'success'
      ? 'success'
      : status === 'warning'
        ? 'warning'
        : status === 'syncing'
          ? 'accent'
          : 'neutral'

  return (
    <Badge variant={variant} className={cn(statusBadgeVariants({ status }), className)} {...props}>
      {children}
    </Badge>
  )
}
