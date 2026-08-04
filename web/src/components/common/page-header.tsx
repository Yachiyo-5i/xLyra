import type { ReactNode } from 'react'

type PageHeaderProps = {
  eyebrow?: ReactNode
  title: string
  description?: string
  actions?: ReactNode
}

export function PageHeader({ eyebrow, title, description, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
      <div className="space-y-2">
        {eyebrow && (
          <p className="text-faint text-xs font-medium uppercase tracking-[0.24em]">
            {eyebrow}
          </p>
        )}
        <div className="space-y-1.5">
          <h2 className="text-3xl font-semibold tracking-tight text-foreground">
            {title}
          </h2>
          {description && (
            <p className="text-muted-soft max-w-3xl text-sm leading-6">{description}</p>
          )}
        </div>
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-3">{actions}</div> : null}
    </div>
  )
}
