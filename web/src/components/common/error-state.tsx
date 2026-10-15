import type { ReactNode } from 'react'
import { AlertTriangle } from 'lucide-react'
import { cn } from '@/lib/utils'

type ErrorStateProps = {
  title: string
  description: string
  action?: ReactNode
  className?: string
}

/** 错误状态：与 EmptyState 同一结构，图标用系统红；通常给一个「重试」按钮 */
export function ErrorState({ title, description, action, className }: ErrorStateProps) {
  return (
    <div role="alert" className={cn('flex min-h-56 flex-col items-center justify-center gap-3 px-6 py-10 text-center', className)}>
      <AlertTriangle className="size-10 text-danger" strokeWidth={1.5} aria-hidden="true" />
      <div className="space-y-1">
        <h3 className="text-title-3 text-label">{title}</h3>
        <p className="max-w-md break-words text-body text-label-2">{description}</p>
      </div>
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  )
}
