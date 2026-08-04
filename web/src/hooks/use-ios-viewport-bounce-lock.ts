import { useEffect, type RefObject } from 'react'

export function useIOSViewportBounceLock(rootRef: RefObject<HTMLElement | null>) {
  useEffect(() => {
    if (!isIOSLike()) return

    const rootElement = rootRef.current
    if (!(rootElement instanceof HTMLElement)) return

    return attachIOSViewportBounceLock(rootElement)
  }, [rootRef])
}

function attachIOSViewportBounceLock(rootElement: HTMLElement) {
    let lastY = 0

    function handleTouchStart(event: TouchEvent) {
      if (event.touches.length !== 1) return
      lastY = event.touches[0]?.clientY ?? 0
    }

    function handleTouchMove(event: TouchEvent) {
      if (event.touches.length !== 1) return

      const target = event.target
      if (!(target instanceof Element) || !rootElement.contains(target)) return

      const currentY = event.touches[0]?.clientY ?? lastY
      const deltaY = currentY - lastY
      lastY = currentY

      if (deltaY === 0) return
      if (canScrollInDirection(target, rootElement, deltaY)) return

      event.preventDefault()
    }

    rootElement.addEventListener('touchstart', handleTouchStart, { passive: true, capture: true })
    rootElement.addEventListener('touchmove', handleTouchMove, { passive: false, capture: true })

    return () => {
      rootElement.removeEventListener('touchstart', handleTouchStart, { capture: true })
      rootElement.removeEventListener('touchmove', handleTouchMove, { capture: true })
    }
}

function canScrollInDirection(target: Element, root: HTMLElement, deltaY: number) {
  let element: Element | null = target

  while (element && root.contains(element)) {
    if (isScrollableY(element)) {
      const { scrollTop, scrollHeight, clientHeight } = element
      const canScrollUp = scrollTop > 0
      const canScrollDown = scrollTop + clientHeight < scrollHeight - 1

      if (deltaY > 0 && canScrollUp) return true
      if (deltaY < 0 && canScrollDown) return true
    }

    if (element === root) break
    element = element.parentElement
  }

  return false
}

function isScrollableY(element: Element) {
  if (!(element instanceof HTMLElement)) return false

  const style = window.getComputedStyle(element)
  const overflowY = style.overflowY
  const allowsScroll = overflowY === 'auto' || overflowY === 'scroll' || overflowY === 'overlay'

  return allowsScroll && element.scrollHeight > element.clientHeight + 1
}

function isIOSLike() {
  if (typeof navigator === 'undefined') return false

  const platform = navigator.platform || ''
  const userAgent = navigator.userAgent || ''
  const maxTouchPoints = navigator.maxTouchPoints || 0

  return /iPad|iPhone|iPod/.test(userAgent) || (platform === 'MacIntel' && maxTouchPoints > 1)
}
