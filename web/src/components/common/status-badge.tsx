import type { HTMLAttributes } from 'react'
import { Badge, type BadgeProps } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

type Status = 'healthy' | 'success' | 'warning' | 'error' | 'syncing' | 'idle' | 'disabled'

const statusVariant: Record<Status, NonNullable<BadgeProps['variant']>> = {
  healthy: 'success',
  success: 'success',
  warning: 'warning',
  error: 'danger',
  syncing: 'info',
  idle: 'neutral',
  disabled: 'neutral',
}

type StatusBadgeProps = HTMLAttributes<HTMLDivElement> & {
  status?: Status | null
  children: string
}

/** 状态标签：Badge + 圆点，颜色由状态决定 */
export function StatusBadge({ className, status, children, ...props }: StatusBadgeProps) {
  const resolved = status ?? 'idle'

  return (
    <Badge
      variant={statusVariant[resolved]}
      dot
      className={cn(resolved === 'disabled' && 'text-label-3', resolved === 'syncing' && '[&>span:first-child]:animate-pulse', className)}
      {...props}
    >
      {children}
    </Badge>
  )
}
