import { useCallback, useRef, useState, type UIEvent } from 'react'

/** 向下滚动超过该距离（px）才收起，避免手指轻微抖动 */
const DIRECTION_THRESHOLD = 6
/** 距顶部小于该值视为「在顶部」，此时总是展开 */
const TOP_THRESHOLD = 24

/**
 * 胶囊 Tab Bar 的「滚动收起」（HIG：Tab Bar 在滚动时最小化）：
 * - 页面在顶部时展开完整胶囊；
 * - 向下滚动即收起为左侧单个图标；
 * - 向上滚动不恢复，只有点击收起后的图标（expand）或滚回顶部才展开。
 * 把 onScroll 挂到主滚动容器上即可。
 */
export function useCollapseOnScroll() {
  const [collapsed, setCollapsed] = useState(false)
  const lastScrollTopRef = useRef(0)

  const onScroll = useCallback((event: UIEvent<HTMLElement>) => {
    const scrollTop = event.currentTarget.scrollTop
    const delta = scrollTop - lastScrollTopRef.current
    if (scrollTop < TOP_THRESHOLD) setCollapsed(false)
    else if (delta > DIRECTION_THRESHOLD) setCollapsed(true)
    lastScrollTopRef.current = scrollTop
  }, [])

  const expand = useCallback(() => setCollapsed(false), [])

  return { collapsed, onScroll, expand }
}
