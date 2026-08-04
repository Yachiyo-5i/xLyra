import type { ReactNode } from 'react'
import { useRef } from 'react'
import { AppLogo } from '@/components/common/app-logo'
import { useIOSViewportBounceLock } from '@/hooks/use-ios-viewport-bounce-lock'

type MobileAuthShellProps = {
  title: string
  description?: string
  children: ReactNode
}

export function MobileAuthShell({ title, children }: MobileAuthShellProps) {
  const viewportRef = useRef<HTMLDivElement>(null)

  useIOSViewportBounceLock(viewportRef)

  return (
    <div ref={viewportRef} className="app-viewport bg-[hsl(var(--surface-base))]">
      <div className="ambient-background" />
      <div className="noise-overlay" />
      <div className="relative z-0 flex h-full min-h-0 flex-col px-5 pb-6 pt-5 mobile-safe-top mobile-safe-bottom">
        <main data-scroll-container="true" className="flex min-h-0 flex-1 flex-col justify-center overflow-y-auto py-8">
          <div className="mx-auto w-full max-w-[450px]">
            <section className="mb-10 flex items-center justify-center gap-4 px-1">
              <AppLogo className="size-14 rounded-[22px] shadow-[0_18px_42px_hsl(var(--primary)/0.24)]" decorative />
              <div className="min-w-0">
                <p className="text-3xl font-semibold tracking-normal text-foreground">xLyra</p>
              </div>
            </section>

            <section className="relative px-1">
              <div className="mb-7 text-center">
                <h1 className="text-2xl font-semibold leading-tight tracking-normal text-foreground">{title}</h1>
              </div>
              {children}
            </section>
          </div>
        </main>

        <footer className="text-center text-xs font-medium text-muted-soft">
          © {new Date().getFullYear()} xLyra. All rights reserved.
        </footer>
      </div>
    </div>
  )
}
