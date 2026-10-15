import type { CSSProperties, PropsWithChildren } from 'react'
import { useState } from 'react'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'
import { LiquidGlass } from '@/components/glass/liquid-glass'
import { AppSidebar } from '@/components/layout/app-sidebar'
import { AppTopbar } from '@/components/layout/app-topbar'
import { InlineTitleProvider } from '@/components/layout/inline-title-provider'
import { MobileAppShell } from '@/components/layout/mobile-app-shell'
import { SystemVersionText } from '@/components/layout/system-version-text'
import { Button } from '@/components/ui/button'
import { useMobileLayout } from '@/hooks/use-media-query'

export function AppShell({ children }: PropsWithChildren) {
  const isMobile = useMobileLayout()

  // 大标题收起：PageHeader 写入、顶栏读取
  return (
    <InlineTitleProvider>
      {isMobile ? <MobileAppShell>{children}</MobileAppShell> : <DesktopAppShell>{children}</DesktopAppShell>}
    </InlineTitleProvider>
  )
}

function DesktopAppShell({ children }: PropsWithChildren) {
  const { t } = useTranslation('components')
  const location = useLocation()
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false)
  const isAgentWorkspace = location.pathname === '/agent' || location.pathname.startsWith('/agent/')

  if (isAgentWorkspace) {
    return (
      <div className="app-viewport">
        <div className="ambient-background" />
        <main className="relative z-10 h-full min-h-0 overflow-hidden">{children}</main>
      </div>
    )
  }

  return (
    <div className="app-viewport">
      <div className="ambient-background" />
      <div className="relative z-0 flex h-full w-full">
        {/* 功能层：浮动的 Liquid Glass 侧边栏，280 宽、距窗口 16、圆角 24 */}
        <LiquidGlass
          as="aside"
          className="z-20 my-4 ml-4 hidden h-[calc(100%-2rem)] w-[280px] shrink-0 overflow-hidden rounded-shell lg:flex lg:flex-col"
        >
          <AppSidebar />
        </LiquidGlass>

        <div className="relative z-10 flex min-w-0 flex-1 flex-col">
          {/* 内容在浮动顶栏下方滚过；页脚跟随内容滚动 */}
          <main data-scroll-container="true" className="min-h-0 flex-1 overflow-y-auto">
            <div className="px-4 pb-6 pt-[84px] md:px-8 lg:px-10">{children}</div>
            <AppFooter />
          </main>
          <div className="scroll-edge-top z-20" style={{ '--scroll-edge-height': '84px' } as CSSProperties} />
          <AppTopbar onMenuClick={() => setMobileSidebarOpen(true)} />
        </div>
      </div>

      {mobileSidebarOpen ? (
        <div className="fixed inset-0 z-50 lg:hidden">
          <button
            aria-label={t('navigation.closeOverlay')}
            className="absolute inset-0 bg-black/45 transition-opacity"
            type="button"
            onClick={() => setMobileSidebarOpen(false)}
          />
          <div className="lg-thick absolute inset-x-2 bottom-2 h-[85vh] overflow-hidden rounded-shell animate-in slide-in-from-bottom-full duration-300 ease-out">
            <div className="absolute left-1/2 top-2 z-20 h-[5px] w-9 -translate-x-1/2 rounded-full bg-fill-1" />
            <Button
              variant="secondary"
              size="icon-sm"
              className="absolute right-4 top-4 z-20"
              aria-label={t('navigation.close')}
              onClick={() => setMobileSidebarOpen(false)}
            >
              <X className="h-4 w-4" />
            </Button>
            <AppSidebar
              className="h-full pt-4"
              onNavigate={() => setMobileSidebarOpen(false)}
            />
          </div>
        </div>
      ) : null}
    </div>
  )
}

function AppFooter() {
  return (
    <footer className="px-4 pb-6 pt-2 md:px-8 lg:px-10">
      <div className="flex flex-col gap-3 text-xs text-label-3 sm:flex-row sm:items-center sm:justify-between">
        <a
          href="https://github.com/Yachiyo-5i/xLyra"
          target="_blank"
          rel="noreferrer"
          className="inline-flex w-fit items-center gap-2 rounded-md transition-colors hover:text-label focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tint/55"
        >
          <GitHubIcon className="h-4 w-4" />
          <span>GitHub</span>
        </a>

        <div className="flex items-center gap-3 sm:justify-end">
          <span>© {new Date().getFullYear()} xLyra</span>
          <span className="h-1 w-1 rounded-full bg-current opacity-40" />
          <SystemVersionText />
        </div>
      </div>
    </footer>
  )
}

function GitHubIcon({ className }: { className?: string }) {
  return (
    <svg
      aria-hidden="true"
      className={className}
      fill="currentColor"
      viewBox="0 0 24 24"
    >
      <path d="M12 2C6.48 2 2 6.58 2 12.24c0 4.52 2.87 8.35 6.84 9.7.5.1.68-.22.68-.49 0-.24-.01-.88-.01-1.73-2.78.62-3.37-1.37-3.37-1.37-.45-1.18-1.11-1.49-1.11-1.49-.91-.64.07-.63.07-.63 1 .07 1.53 1.06 1.53 1.06.9 1.57 2.36 1.12 2.93.86.09-.67.35-1.12.63-1.38-2.22-.26-4.56-1.14-4.56-5.07 0-1.12.39-2.03 1.03-2.75-.1-.26-.45-1.3.1-2.71 0 0 .84-.28 2.75 1.05A9.38 9.38 0 0 1 12 6.95c.85 0 1.7.12 2.5.34 1.9-1.33 2.74-1.05 2.74-1.05.55 1.41.2 2.45.1 2.71.64.72 1.03 1.63 1.03 2.75 0 3.94-2.34 4.81-4.57 5.06.36.32.68.95.68 1.91 0 1.38-.01 2.49-.01 2.83 0 .27.18.59.69.49A10.07 10.07 0 0 0 22 12.24C22 6.58 17.52 2 12 2Z" />
    </svg>
  )
}
