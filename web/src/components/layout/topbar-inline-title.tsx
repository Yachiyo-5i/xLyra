import { useState } from 'react'
import { useInlineTitle } from '@/components/layout/inline-title'
import { cn } from '@/lib/utils'

/**
 * 顶栏中间的行内标题：大标题滚出后从下方 4px 淡入（200ms），回到顶部时淡出。
 * 淡出期间保留上一个标题，避免文字先消失再淡出。
 */
export function TopbarInlineTitle({ className }: { className?: string }) {
  const title = useInlineTitle()
  const [lastTitle, setLastTitle] = useState(title)
  if (title && title !== lastTitle) setLastTitle(title)

  return (
    <div
      aria-hidden={!title}
      className={cn(
        'pointer-events-none absolute left-1/2 top-1/2 max-w-[min(40vw,360px)] -translate-x-1/2 truncate text-headline text-label transition-[opacity,transform] duration-200 ease-(--ease-standard)',
        title ? '-translate-y-1/2 opacity-100' : 'translate-y-[calc(-50%+4px)] opacity-0',
        className,
      )}
    >
      {title ?? lastTitle}
    </div>
  )
}
