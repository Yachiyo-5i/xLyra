import type { ReactNode } from 'react'
import { Inbox, type LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

type EmptyStateProps = {
  title: string
  description: string
  action?: ReactNode
  icon?: LucideIcon
  className?: string
}

/**
 * 空状态（仿 iOS ContentUnavailableView）：没有容器，居中；
 * 40 的线性图标 label-3 → Title 3 → Body label-2 → 可选的主按钮。
 * 放在表格、卡片里时由外层提供容器。
 */
export function EmptyState({ title, description, action, icon: Icon = Inbox, className }: EmptyStateProps) {
  return (
    <div className={cn('flex min-h-56 flex-col items-center justify-center gap-3 px-6 py-10 text-center', className)}>
      <Icon className="size-10 text-label-3" strokeWidth={1.5} aria-hidden="true" />
      <div className="space-y-1">
        <h3 className="text-title-3 text-label">{title}</h3>
        <p className="max-w-md text-body text-label-2">{description}</p>
      </div>
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  )
}
