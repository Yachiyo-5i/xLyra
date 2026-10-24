import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { LiquidGlass } from '@/components/glass/liquid-glass'
import { cn } from '@/lib/utils'

export type MobileDockMenuItem = {
  to: string
  label: string
  icon: LucideIcon
}

type MobileDockMenuProps = {
  open: boolean
  items: MobileDockMenuItem[]
  /** 「更多」按钮：菜单右边缘与它对齐 */
  anchorRef?: RefObject<HTMLElement | null>
  onOpenChange: (open: boolean) => void
}

/**
 * 「更多」菜单：从「更多」按钮向上、沿一条弧线依次展开的玻璃胶囊。
 * 越靠上的项向左偏移、旋转得越多，形成弧度；每项是一个 Liquid Glass 胶囊（图标 + 文字），
 * 按顺序错开 18ms 弹出。Tab Bar 只放图标，这里补上文字。
 */
export function MobileDockMenu({ open, items, anchorRef, onOpenChange }: MobileDockMenuProps) {
  const { t } = useTranslation('components')
  const location = useLocation()
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') onOpenChange(false)
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onOpenChange, open])

  // 右边缘对齐「更多」按钮。菜单是 position: fixed，先贴住包含块右侧量出其右边界，
  // 再换算偏移量——这样嵌在带 transform 的容器里也能对齐
  useLayoutEffect(() => {
    if (!open) return
    const menu = menuRef.current
    if (!menu) return

    function updateAnchorRight() {
      const anchor = anchorRef?.current
      if (!menu || !anchor) return
      menu.style.right = '0px'
      const containerRight = menu.getBoundingClientRect().right
      const anchorRight = anchor.getBoundingClientRect().right
      menu.style.right = `${Math.max(16, containerRight - anchorRight)}px`
    }

    updateAnchorRight()
    window.addEventListener('resize', updateAnchorRight)
    return () => window.removeEventListener('resize', updateAnchorRight)
  }, [anchorRef, open])

  if (!open) return null

  return (
    <>
      <button
        type="button"
        aria-label={t('navigation.closeOverlay')}
        className="fixed inset-0 z-40 cursor-default bg-black/25 animate-in fade-in-0 duration-200"
        onClick={() => onOpenChange(false)}
      />
      <div
        ref={menuRef}
        role="menu"
        className="mobile-dock-anchor pointer-events-none fixed right-4 z-50 flex max-h-[min(72dvh,640px)] flex-col items-end gap-2 overflow-visible"
        style={{ bottom: 'calc(var(--mobile-dock-safe-bottom, env(safe-area-inset-bottom, 0px)) + 21px + 62px + 12px)' }}
      >
        {items.map((item, index) => {
          const Icon = item.icon
          const active = isRouteActive(location.pathname, item.to)
          // 离「更多」按钮越远（越靠上），向左偏移、旋转越多，排成一条弧线
          const distanceFromAnchor = items.length - index - 1
          const offset = Math.min(distanceFromAnchor * 8, 48)
          const rotate = Math.min(7, 2 + distanceFromAnchor * 0.55)

          return (
            <LiquidGlass
              key={item.to}
              className="pointer-events-auto max-w-[78vw] origin-bottom-right rounded-full"
              style={{
                transform: `translateX(-${offset}px) rotate(${rotate}deg)`,
                animation: `mobile-dock-menu-item-in 220ms ${index * 18}ms cubic-bezier(0.22,1,0.36,1) both`,
              }}
            >
              <NavLink
                to={item.to}
                role="menuitem"
                aria-current={active ? 'page' : undefined}
                onClick={() => onOpenChange(false)}
                className="group flex h-11 items-center gap-2 rounded-full pl-4 pr-1.5 text-subhead text-label transition-transform duration-150 active:scale-95"
              >
                <span className={cn('min-w-0 max-w-[52vw] truncate', active && 'font-semibold')}>{item.label}</span>
                <span
                  className={cn(
                    'grid size-8 shrink-0 place-items-center rounded-full transition-colors duration-150',
                    active ? 'bg-tint text-tint-foreground' : 'text-label-2 group-hover:text-label',
                  )}
                >
                  <Icon className="size-[18px]" strokeWidth={1.75} />
                </span>
              </NavLink>
            </LiquidGlass>
          )
        })}
      </div>
    </>
  )
}

function isRouteActive(pathname: string, to: string) {
  return pathname === to || pathname.startsWith(`${to}/`)
}
