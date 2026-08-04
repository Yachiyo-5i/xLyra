import { NavLink, useLocation } from 'react-router-dom'
import { MoreHorizontal, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { CSSProperties, Ref } from 'react'

export type MobileTabbarItem = {
  to: string
  label: string
  icon: LucideIcon
}

type MobileTabbarProps = {
  items: MobileTabbarItem[]
  moreItems?: MobileTabbarItem[]
  moreOpen?: boolean
  onMoreClick: () => void
  moreButtonRef?: Ref<HTMLButtonElement>
}

export function MobileTabbar({ items, moreItems = [], moreOpen = false, onMoreClick, moreButtonRef }: MobileTabbarProps) {
  const { t } = useTranslation('common')
  const location = useLocation()
  const activeMoreItem = moreItems.find((item) => isRouteActive(location.pathname, item.to))
  const MoreIcon = activeMoreItem?.icon ?? MoreHorizontal
  const moreActive = moreOpen || Boolean(activeMoreItem) || isMoreActive(location.pathname, items)

  return (
    <nav
      className="mobile-tabbar-shell mobile-safe-bottom pointer-events-none absolute inset-x-0 bottom-0 z-30 px-4 transition-[opacity,transform] duration-200"
      style={{ '--mobile-safe-bottom-extra': '1.25rem' } as CSSProperties}
    >
      <div className="mx-auto grid h-14 max-w-[360px] grid-cols-5 gap-1 rounded-full border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.2)] p-1 shadow-[0_16px_38px_rgba(0,0,0,0.12)] backdrop-blur-[32px] backdrop-saturate-150">
        {items.map((tab) => {
          const Icon = tab.icon

          return (
            <NavLink
              key={tab.to}
              to={tab.to}
              aria-label={tab.label}
              className={({ isActive }) =>
                cn(
                  'pointer-events-auto flex min-w-0 items-center justify-center rounded-full transition-colors',
                  isActive
                    ? 'bg-primary text-white shadow-[0_10px_24px_hsl(var(--primary)/0.22)]'
                    : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
                )
              }
            >
              {({ isActive }) => (
                <Icon className="size-5 shrink-0" style={isActive ? { color: '#fff' } : undefined} />
              )}
            </NavLink>
          )
        })}

        <button
          ref={moreButtonRef}
          type="button"
          onClick={onMoreClick}
          aria-label={activeMoreItem?.label ?? t('nav.settings')}
          className={cn(
            'pointer-events-auto flex min-w-0 items-center justify-center rounded-full transition-colors',
            moreActive
              ? 'bg-primary text-white shadow-[0_10px_24px_hsl(var(--primary)/0.22)]'
              : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
          )}
        >
          <MoreIcon className="size-5 shrink-0" style={moreActive ? { color: '#fff' } : undefined} />
        </button>
      </div>
    </nav>
  )
}

function isMoreActive(pathname: string, items: MobileTabbarItem[]) {
  return !items.some((item) => pathname === item.to || pathname.startsWith(`${item.to}/`))
}

function isRouteActive(pathname: string, to: string) {
  return pathname === to || pathname.startsWith(`${to}/`)
}
