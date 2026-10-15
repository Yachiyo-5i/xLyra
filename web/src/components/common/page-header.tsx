import { useEffect, useRef, type ReactNode } from 'react'
import { useSetInlineTitle } from '@/components/layout/inline-title'
import { cn } from '@/lib/utils'

type PageHeaderProps = {
  eyebrow?: ReactNode
  title: string
  description?: string
  actions?: ReactNode
  /** 滚动越过大标题后在顶栏显示行内标题；同一页面只应有一个开启 */
  collapsible?: boolean
  className?: string
}

/** 浮动顶栏占用的高度：大标题底边越过这里即视为被遮住 */
const TOPBAR_INSET = 72

/**
 * 页面标题：Large Title（衬线）+ 说明，右侧主操作。
 * 随内容滚动；越过顶栏后收起为顶栏中间的行内标题（仿 iOS 大标题导航栏）。
 */
export function PageHeader({ eyebrow, title, description, actions, collapsible = true, className }: PageHeaderProps) {
  const titleRef = useRef<HTMLHeadingElement>(null)
  const setInlineTitle = useSetInlineTitle()

  useEffect(() => {
    const node = titleRef.current
    if (!collapsible || !setInlineTitle || !node) return

    const observer = new IntersectionObserver(
      ([entry]) => {
        // 只在标题从上方滚出时收起；标题在视口下方（页面很短被挤下去）不算
        const scrolledPast = !entry.isIntersecting && entry.boundingClientRect.top < TOPBAR_INSET
        setInlineTitle(scrolledPast ? title : null)
      },
      { rootMargin: `-${TOPBAR_INSET}px 0px 0px 0px`, threshold: 0 },
    )
    observer.observe(node)

    return () => {
      observer.disconnect()
      setInlineTitle(null)
    }
  }, [collapsible, setInlineTitle, title])

  return (
    <div className={cn('flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between', className)}>
      <div className="min-w-0 space-y-1">
        {eyebrow ? <p className="text-footnote text-label-3">{eyebrow}</p> : null}
        <h2 ref={titleRef} className="text-large-title text-label">
          {title}
        </h2>
        {description ? <p className="max-w-3xl text-body text-label-2">{description}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}
