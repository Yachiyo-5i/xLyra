import type { PropsWithChildren } from 'react'
import { useState } from 'react'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AppSidebar } from '@/components/layout/app-sidebar'
import { AppTopbar } from '@/components/layout/app-topbar'
import { MobileAppShell } from '@/components/layout/mobile-app-shell'
import { SystemVersionText } from '@/components/layout/system-version-text'
import { Button } from '@/components/ui/button'
import { useMobileLayout } from '@/hooks/use-media-query'

export function AppShell({ children }: PropsWithChildren) {
  const isMobile = useMobileLayout()

  if (isMobile) {
    return <MobileAppShell>{children}</MobileAppShell>
  }

  return <DesktopAppShell>{children}</DesktopAppShell>
}

function DesktopAppShell({ children }: PropsWithChildren) {
  const { t } = useTranslation('components')
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false)

  return (
    <div className="app-viewport">
      <div className="ambient-background" />
      <div className="noise-overlay" />
      <div className="flex h-full w-full relative z-0">
        <aside className="hidden h-[calc(100vh-2rem)] my-4 ml-4 w-[280px] shrink-0 lg:block rounded-[24px] overflow-hidden shadow-[0_24px_60px_rgba(0,0,0,0.12)] border border-[hsl(var(--glass-border))] z-20">
          <AppSidebar className="border-none rounded-none" />
        </aside>

        <div className="relative flex min-w-0 flex-1 flex-col z-10">
          <AppTopbar onMenuClick={() => setMobileSidebarOpen(true)} />

          <main className="min-h-0 flex-1 overflow-hidden">
            <div className="h-full min-h-0 overflow-y-auto px-4 py-6 md:px-8 lg:py-8 lg:px-10">{children}</div>
          </main>

          <AppFooter />
        </div>
      </div>

      {mobileSidebarOpen ? (
        <div className="fixed inset-0 z-50 lg:hidden">
          <button
            aria-label={t('navigation.closeOverlay')}
            className="absolute inset-0 bg-[rgb(var(--dialog-overlay))] backdrop-blur-md transition-opacity"
            type="button"
            onClick={() => setMobileSidebarOpen(false)}
          />
          <div className="absolute bottom-0 left-0 right-0 h-[85vh] rounded-t-[24px] overflow-hidden shadow-[0_-20px_40px_rgba(0,0,0,0.2)] animate-in slide-in-from-bottom-full duration-300 ease-out bg-[hsl(var(--surface-base))] border-t border-[hsl(var(--glass-border))]">
            <div className="absolute top-3 left-1/2 -translate-x-1/2 w-12 h-1.5 rounded-full bg-[hsl(var(--divider))] z-20" />
            <Button
              variant="ghost"
              size="icon"
              className="absolute top-4 right-4 z-20 bg-background/50 backdrop-blur-md rounded-full size-8 border border-[hsl(var(--glass-border))]"
              aria-label={t('navigation.close')}
              onClick={() => setMobileSidebarOpen(false)}
            >
              <X className="h-4 w-4" />
            </Button>
            <AppSidebar
              className="h-full pt-8 rounded-none border-none"
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
    <footer className="shrink-0 bg-transparent px-4 py-4 lg:px-8 lg:py-5">
      <div className="flex flex-col gap-3 text-xs text-[hsl(var(--text-muted-soft))] sm:flex-row sm:items-center sm:justify-between">
        <a
          href="https://github.com/Yachiyo-5i/xLyra"
          target="_blank"
          rel="noreferrer"
          className="inline-flex w-fit items-center gap-2 rounded-md transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-strong))]"
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
