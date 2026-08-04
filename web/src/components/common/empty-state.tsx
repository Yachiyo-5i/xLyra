import type { ReactNode } from 'react'
import { Inbox } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

type EmptyStateProps = {
  title: string
  description: string
  action?: ReactNode
  className?: string
}

export function EmptyState({ title, description, action, className }: EmptyStateProps) {
  return (
    <Card className={cn('flex min-h-56 flex-col items-center justify-center gap-4 rounded-lg p-6 text-center', className)}>
      <div className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] p-4 text-foreground">
        <Inbox className="h-6 w-6" />
      </div>
      <div className="space-y-1">
        <h3 className="text-lg font-semibold">{title}</h3>
        <p className="text-muted-soft max-w-md text-sm leading-6">{description}</p>
      </div>
      {action}
    </Card>
  )
}
