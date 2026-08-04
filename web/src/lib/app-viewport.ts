const APP_VIEWPORT_HEIGHT_VAR = '--app-viewport-height'

/** iOS「添加到主屏幕」standalone 模式 */
export function isStandaloneWebApp(): boolean {
  if (typeof window === 'undefined') return false

  const navigatorWithStandalone = window.navigator as Navigator & { standalone?: boolean }

  return (
    navigatorWithStandalone.standalone === true ||
    window.matchMedia('(display-mode: standalone)').matches ||
    window.matchMedia('(display-mode: fullscreen)').matches
  )
}

/**
 * iOS standalone PWA 冷启动时 100dvh / innerHeight / visualViewport.height
 * 会比真实屏幕矮约 safe-area-inset-top；100vh 才是全屏高度。
 * 浏览器模式继续用 100dvh（由 CSS 回退），不写入变量。
 */
export function installAppViewportSize() {
  if (typeof window === 'undefined') return () => undefined

  let frame = 0

  function syncViewportSize() {
    if (frame) {
      window.cancelAnimationFrame(frame)
    }

    frame = window.requestAnimationFrame(() => {
      frame = 0
      const root = document.documentElement

      if (isStandaloneWebApp()) {
        root.dataset.standalone = 'true'
        // 键盘弹起时 visualViewport 会缩小，跟随可视区；否则用 100vh 填满冷启动的「矮视口」漏洞
        const visualViewport = window.visualViewport
        const isKeyboardLikelyOpen =
          visualViewport != null &&
          visualViewport.height > 0 &&
          visualViewport.height < window.innerHeight - 80

        if (isKeyboardLikelyOpen) {
          root.style.setProperty(APP_VIEWPORT_HEIGHT_VAR, `${Math.round(visualViewport.height)}px`)
        } else {
          root.style.setProperty(APP_VIEWPORT_HEIGHT_VAR, '100vh')
        }
        return
      }

      delete root.dataset.standalone
      root.style.removeProperty(APP_VIEWPORT_HEIGHT_VAR)
    })
  }

  syncViewportSize()

  window.addEventListener('resize', syncViewportSize)
  window.addEventListener('orientationchange', syncViewportSize)
  window.addEventListener('pageshow', syncViewportSize)
  window.visualViewport?.addEventListener('resize', syncViewportSize)
  window.visualViewport?.addEventListener('scroll', syncViewportSize)

  return () => {
    if (frame) {
      window.cancelAnimationFrame(frame)
    }

    window.removeEventListener('resize', syncViewportSize)
    window.removeEventListener('orientationchange', syncViewportSize)
    window.removeEventListener('pageshow', syncViewportSize)
    window.visualViewport?.removeEventListener('resize', syncViewportSize)
    window.visualViewport?.removeEventListener('scroll', syncViewportSize)
  }
}
