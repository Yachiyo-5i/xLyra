import { useCallback, useRef, type RefObject } from 'react'

const BOTTOM_THRESHOLD_PX = 40

export function useStickToBottom(scrollRef: RefObject<HTMLElement | null>) {
  const stickRef = useRef(true)

  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight <= BOTTOM_THRESHOLD_PX
  }, [scrollRef])

  const scrollToBottom = useCallback(() => {
    stickRef.current = true
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [scrollRef])

  const scrollToBottomIfStuck = useCallback(() => {
    const el = scrollRef.current
    if (el && stickRef.current) el.scrollTop = el.scrollHeight
  }, [scrollRef])

  return { handleScroll, scrollToBottom, scrollToBottomIfStuck }
}
