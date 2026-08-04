import { useEffect, useLayoutEffect, useState, type RefObject } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

export type MobileDockMenuItem = {
  to: string
  label: string
  icon: LucideIcon
}

type MobileDockMenuProps = {
  open: boolean
  items: MobileDockMenuItem[]
  anchorRef?: RefObject<HTMLElement | null>
  onOpenChange: (open: boolean) => void
}

export function MobileDockMenu({ open, items, anchorRef, onOpenChange }: MobileDockMenuProps) {
  const location = useLocation()
  const [anchorRight, setAnchorRight] = useState<number | null>(null)

  useEffect(() => {
    if (!open) return

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onOpenChange(false)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onOpenChange, open])

  useLayoutEffect(() => {
    if (!open) return

    function updateAnchorRight() {
      const anchor = anchorRef?.current
      if (!anchor) {
        setAnchorRight(null)
        return
      }

      const rect = anchor.getBoundingClientRect()
      setAnchorRight(Math.max(16, window.innerWidth - rect.right))
    }

    updateAnchorRight()
    window.addEventListener('resize', updateAnchorRight)
    window.addEventListener('scroll', updateAnchorRight, true)

    return () => {
      window.removeEventListener('resize', updateAnchorRight)
      window.removeEventListener('scroll', updateAnchorRight, true)
    }
  }, [anchorRef, open])

  if (!open) return null

  return (
    <>
      <button
        type="button"
        aria-label="Close menu"
        className="fixed inset-0 z-40 cursor-default bg-transparent"
        onClick={() => onOpenChange(false)}
      />
      <div
        role="menu"
        className="mobile-dock-anchor pointer-events-none fixed bottom-[calc(env(safe-area-inset-bottom,0px)+6.6rem)] z-50 flex max-h-[min(68dvh,620px)] flex-col items-end gap-2 overflow-visible"
        style={{ right: anchorRight == null ? '1rem' : `${anchorRight}px` }}
      >
        {items.map((item, index) => {
          const Icon = item.icon
          const active = isRouteActive(location.pathname, item.to)
          const distanceFromAnchor = items.length - index - 1
          const offset = Math.min(distanceFromAnchor * 8, 48)
          const rotate = Math.min(7, 2 + distanceFromAnchor * 0.55)

          return (
            <NavLink
              key={item.to}
              to={item.to}
              role="menuitem"
              onClick={() => onOpenChange(false)}
              className={cn(
                'pointer-events-auto group flex h-10 max-w-[78vw] origin-bottom-right items-center gap-2 rounded-full bg-[hsl(var(--card)/0.18)] px-3 pl-4 text-sm shadow-[0_12px_34px_rgba(0,0,0,0.12)] backdrop-blur-[32px] backdrop-saturate-150 transition-[color,filter,transform] duration-150 active:scale-95',
                active
                  ? 'text-primary'
                  : 'text-foreground hover:text-primary',
              )}
              style={{
                transform: `translateX(-${offset}px) rotate(${rotate}deg)`,
                animation: `mobile-dock-menu-item-in 180ms ${index * 18}ms cubic-bezier(0.22,1,0.36,1) both`,
              }}
            >
              <span className="min-w-0 max-w-[52vw] truncate font-medium">
                {item.label}
              </span>
              <span
                className={cn(
                  'grid size-7 shrink-0 place-items-center transition-transform duration-150 group-hover:scale-105',
                  active ? 'text-primary' : 'text-muted-soft group-hover:text-primary',
                )}
              >
                <Icon className="size-5" />
              </span>
            </NavLink>
          )
        })}
      </div>
    </>
  )
}

function isRouteActive(pathname: string, to: string) {
  return pathname === to || pathname.startsWith(`${to}/`)
}
