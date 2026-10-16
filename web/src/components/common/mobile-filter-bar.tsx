import type { ChangeEventHandler, FocusEventHandler, KeyboardEventHandler, ReactNode } from 'react'
import { Search } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

type MobileFilterBarProps = {
  search?: {
    value: string
    onChange: ChangeEventHandler<HTMLInputElement>
    placeholder: string
    /** 需要「回车或失焦再生效」的搜索（如请求日志）用这两个 */
    onBlur?: FocusEventHandler<HTMLInputElement>
    onKeyDown?: KeyboardEventHandler<HTMLInputElement>
  }
  /** 搜索框右侧的圆形图标按钮（刷新、调整顺序、保存等），1–2 个 */
  actions?: ReactNode
  /**
   * 页面唯一的分类维度：与桌面一致的分段横条，不论选项多少（多于一屏时可横向滑动、右侧渐隐），
   * 如站点类型、模型价格的「全部 / 缺失」。由调用方传入已带滚动与渐隐的 Tabs
   */
  segments?: ReactNode
  /** 有多个筛选维度时：每个维度一枚 filter 样式的 Select / MultiSelect / DatePicker，排成一行可横向滑动 */
  filters?: ReactNode
  /** 结果摘要与「清除筛选」、汇总胶囊等 */
  summary?: ReactNode
  className?: string
}

/**
 * 移动端列表页的筛选区：
 * 不吸顶、不套方框，直接排在页面标题下面并随内容滚走。由上到下：
 * 搜索 + 图标按钮 → 分类横条（只有一个分类维度时）→ 筛选胶囊（多个维度时，横向滑动、右侧淡出）→ 结果摘要（可选）。
 * 筛选胶囊直接复用桌面同款的虚线 filter 下拉，选中后变为实线并显示所选值。
 */
export function MobileFilterBar({ search, actions, segments, filters, summary, className }: MobileFilterBarProps) {
  return (
    <div className={cn('space-y-3', className)}>
      {search || actions ? (
        <div className="flex items-center gap-2">
          {search ? (
            <div className="relative min-w-0 flex-1">
              <Search className="pointer-events-none absolute left-3.5 top-1/2 z-10 size-4 -translate-y-1/2 text-label-3" />
              <Input
                type="search"
                value={search.value}
                onChange={search.onChange}
                onBlur={search.onBlur}
                onKeyDown={search.onKeyDown}
                placeholder={search.placeholder}
                className="rounded-full pl-10"
              />
            </div>
          ) : (
            <div className="flex-1" />
          )}
          {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
        </div>
      ) : null}

      {segments}

      {filters ? (
        // 横向滑动：右侧淡出提示还有更多，末尾留白让最后一个胶囊能完整滑出；左右延伸到屏幕边缘
        <div className="scrollbar-hidden -mx-4 overflow-x-auto px-4 [mask-image:linear-gradient(to_right,transparent,#000_16px,#000_calc(100%-40px),transparent)]">
          <div className="flex w-max items-center gap-2 pr-6">{filters}</div>
        </div>
      ) : null}

      {summary ? <div className="flex min-h-5 items-center justify-between gap-3 text-footnote text-label-3">{summary}</div> : null}
    </div>
  )
}
