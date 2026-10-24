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

/**
 * 表格上方的筛选工具条：搜索框 + 筛选胶囊 + 右侧的计数与次要操作。
 * 内容层元素，不用玻璃；控件之间间距 8，与表格容器之间 12。
 */
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
      <div className="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex min-w-0 flex-1 flex-col gap-2 md:flex-row md:items-center">
          <label className={cn('relative min-w-0 flex-1', searchClassName)}>
            <Search className="pointer-events-none absolute left-3 top-1/2 z-10 size-4 -translate-y-1/2 text-label-3" />
            <Input
              className="pl-9"
              value={searchValue}
              onChange={onSearchChange}
              placeholder={searchPlaceholder}
            />
          </label>
          {filters ? (
            <div className={cn('grid gap-2 md:grid-flow-col md:auto-cols-fr', filtersClassName)}>
              {filters}
            </div>
          ) : null}
        </div>
        {(meta || actions) ? (
          <div className="flex flex-wrap items-center gap-2 text-footnote text-label-3">
            {meta}
            {actions}
          </div>
        ) : null}
      </div>
    </div>
  )
}
