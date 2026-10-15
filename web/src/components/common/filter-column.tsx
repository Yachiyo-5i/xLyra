import type { ReactNode } from 'react'
import { BrandMark } from '@/components/common/brand-mark'
import { filterOptionClass } from '@/components/common/filter-styles'
import { cn } from '@/lib/utils'

export type FilterColumnItem = {
  key: string
  label: string
  count: number
  iconPath?: string
  fallbackText?: string
}

/** 筛选栏中的一组：Subhead 小标题 + 选项列表（内容层 plain 列表，不用玻璃） */
export function FilterGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <h2 className="px-3 text-subhead text-label-2">{title}</h2>
      <div className="space-y-0.5">{children}</div>
    </div>
  )
}

/** 品牌 / 站点筛选：图标 + 名称 + 右侧计数 */
export function FilterColumn({
  title,
  items,
  selectedKey,
  onSelect,
}: {
  title: string
  items: FilterColumnItem[]
  selectedKey: string | null
  onSelect: (key: string) => void
}) {
  return (
    <FilterGroup title={title}>
      {items.map((item) => {
        const active = selectedKey === item.key

        return (
          <button key={item.key} type="button" onClick={() => onSelect(item.key)} className={filterOptionClass(active)}>
            <BrandMark iconPath={item.iconPath} label={item.label} fallback={item.label} fallbackText={item.fallbackText} size="sm" />
            <span className="min-w-0 flex-1 truncate">{item.label}</span>
            <span className={cn('min-w-5 text-right font-mono text-caption-1 tabular-nums', active ? 'text-label-2' : 'text-label-3')}>
              {item.count}
            </span>
          </button>
        )
      })}
    </FilterGroup>
  )
}
