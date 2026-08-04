import type { ChangeEventHandler, ReactNode } from 'react'
import { Search } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

type TableToolbarProps = {
  searchValue?: string
  onSearchChange?: ChangeEventHandler<HTMLInputElement>
  searchPlaceholder?: string
  searchClassName?: string
  filters?: ReactNode
  filtersClassName?: string
  actions?: ReactNode
  meta?: ReactNode
  className?: string
}

export function TableToolbar({
  searchValue,
  onSearchChange,
  searchPlaceholder = 'Search',
  searchClassName,
  filters,
  filtersClassName,
  actions,
  meta,
  className,
}: TableToolbarProps) {
  return (
    <div className={cn('space-y-3', className)}>
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex min-w-0 flex-1 flex-col gap-3 md:flex-row">
          <label className={cn('relative min-w-0 flex-1', searchClassName)}>
            <Search className="text-muted-soft pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
            <Input
              className="pl-10"
              value={searchValue}
              onChange={onSearchChange}
              placeholder={searchPlaceholder}
            />
          </label>
          {filters ? (
            <div className={cn('grid gap-3 md:grid-flow-col md:auto-cols-fr', filtersClassName)}>
              {filters}
            </div>
          ) : null}
        </div>
        {(meta || actions) ? (
          <div className="flex flex-wrap items-center gap-3">
            {meta}
            {actions}
          </div>
        ) : null}
      </div>
    </div>
  )
}
