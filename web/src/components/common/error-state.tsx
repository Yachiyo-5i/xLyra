import type { ReactNode } from 'react'
import { AlertTriangle } from 'lucide-react'
import { Card } from '@/components/ui/card'

type ErrorStateProps = {
  title: string
  description: string
  action?: ReactNode
}

export function ErrorState({ title, description, action }: ErrorStateProps) {
  return (
    <Card className="flex min-h-56 flex-col items-center justify-center gap-4 rounded-lg p-6 text-center">
      <div className="rounded-lg border border-red-400/25 bg-red-400/14 p-4 text-red-100">
        <AlertTriangle className="h-6 w-6" />
      </div>
      <div className="space-y-1">
        <h3 className="text-lg font-semibold">{title}</h3>
        <p className="text-muted-soft max-w-md text-sm leading-6">{description}</p>
      </div>
      {action}
    </Card>
  )
}
