import type { PropsWithChildren } from 'react'
import { useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { MobileDockMenu, type MobileDockMenuItem } from '@/components/layout/mobile-dock-menu'
import { MobileTabbar, type MobileTabbarItem } from '@/components/layout/mobile-tabbar'
import { MobileTopbar } from '@/components/layout/mobile-topbar'
import { useIOSViewportBounceLock } from '@/hooks/use-ios-viewport-bounce-lock'
import { getAppNavSections, type AppNavItem, type AppNavSection } from '@/lib/navigation'

const mobilePrimaryPaths = ['/dashboard', '/sites', '/requests', '/api-keys']

export function MobileAppShell({ children }: PropsWithChildren) {
  const { t } = useTranslation('common')
  const [moreOpen, setMoreOpen] = useState(false)
  const viewportRef = useRef<HTMLDivElement>(null)
  const moreButtonRef = useRef<HTMLButtonElement>(null)
  const navSections = useMemo(() => getAppNavSections(t), [t])
  const primaryItems = useMemo(() => getMobilePrimaryItems(navSections), [navSections])
  const dockItems = useMemo(() => getMobileDockItems(navSections, primaryItems), [navSections, primaryItems])

  useIOSViewportBounceLock(viewportRef)

  return (
    <div ref={viewportRef} className="app-viewport">
      <div className="ambient-background" />
      <div className="noise-overlay" />
      <div className="relative z-0 h-full min-h-0">
        <main
          data-scroll-container="true"
          className="mobile-main-scroll relative z-0 h-full overflow-y-auto px-4 pt-[calc(env(safe-area-inset-top,0px)+4.25rem)]"
        >
          {children}
        </main>
        <MobileTopbar />
        <MobileTabbar
          items={primaryItems}
          moreItems={dockItems}
          moreOpen={moreOpen}
          moreButtonRef={moreButtonRef}
          onMoreClick={() => setMoreOpen((open) => !open)}
        />
      </div>
      <MobileDockMenu open={moreOpen} items={dockItems} anchorRef={moreButtonRef} onOpenChange={setMoreOpen} />
    </div>
  )
}

function getMobilePrimaryItems(sections: AppNavSection[]): MobileTabbarItem[] {
  const items = flattenNavItems(sections)
  return mobilePrimaryPaths
    .map((path) => items.find((item) => item.to === path))
    .filter((item): item is AppNavItem => Boolean(item))
    .map(({ to, label, icon }) => ({ to, label, icon }))
}

function getMobileDockItems(sections: AppNavSection[], primaryItems: MobileTabbarItem[]): MobileDockMenuItem[] {
  const primaryPaths = new Set(primaryItems.map((item) => item.to))

  return sections
    .flatMap((section) =>
      section.items.flatMap((item) => (item.children?.length ? item.children : [item])),
    )
    .filter((item) => !item.desktopOnly && !primaryPaths.has(item.to))
    .map(({ to, label, icon }) => ({ to, label, icon }))
}

function flattenNavItems(sections: AppNavSection[]) {
  return sections.flatMap((section) =>
    section.items.flatMap((item) => [item, ...(item.children ?? [])]),
  )
}
