import { useRef, type ReactNode } from 'react'
import { useIOSViewportBounceLock } from '@/hooks/use-ios-viewport-bounce-lock'

/** 独立页（登录、注册、鉴权状态）的全屏容器：固定视口，iOS 上锁住整页回弹 */
export function AuthViewport({ children }: { children: ReactNode }) {
  const viewportRef = useRef<HTMLDivElement>(null)
  useIOSViewportBounceLock(viewportRef)
  return (
    <div ref={viewportRef} className="app-viewport">
      {children}
    </div>
  )
}
