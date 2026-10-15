import type { CSSProperties, Ref } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { MoreHorizontal, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { LiquidTabBar, type LiquidTabBarItem } from '@/components/layout/liquid-tab-bar'

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
  /** 向下滚动时收起：只保留当前项 */
  collapsed?: boolean
  onExpand?: () => void
}

const MORE_KEY = '__more__'

/**
 * 移动端胶囊 Tab Bar：距左右下各 21，4 个常用入口 +「更多」。
 * 外观与动效来自 LiquidTabBar；这里只负责路由与「更多」按钮。
 */
export function MobileTabbar({
  items,
  moreItems = [],
  moreOpen = false,
  onMoreClick,
  moreButtonRef,
  collapsed = false,
  onExpand,
}: MobileTabbarProps) {
  const { t } = useTranslation('common')
  const location = useLocation()
  const activeMoreItem = moreItems.find((item) => isRouteActive(location.pathname, item.to))
  const activePrimary = items.find((item) => isRouteActive(location.pathname, item.to))
  const moreLabel = activeMoreItem?.label ?? t('nav.settings')

  const tabItems: LiquidTabBarItem[] = [
    ...items.map((item) => ({ key: item.to, label: item.label, icon: item.icon })),
    // 当前页面在「更多」里时，「更多」显示该页面的图标
    { key: MORE_KEY, label: moreLabel, icon: activeMoreItem?.icon ?? MoreHorizontal },
  ]

  return (
    <nav
      aria-label={t('nav.workspace')}
      className="mobile-tabbar-shell mobile-safe-bottom pointer-events-none absolute inset-x-0 bottom-0 z-30 px-[21px] transition-[opacity,transform] duration-200"
      style={{ '--mobile-safe-bottom-extra': '21px' } as CSSProperties}
    >
      <LiquidTabBar
        items={tabItems}
        activeKey={activePrimary?.to ?? MORE_KEY}
        collapsed={collapsed}
        onExpand={onExpand}
        renderItem={(item, slotProps) =>
          item.key === MORE_KEY ? (
            <button key={item.key} ref={moreButtonRef} type="button" onClick={onMoreClick} aria-expanded={moreOpen} {...slotProps} />
          ) : (
            <NavLink key={item.key} to={item.key} {...slotProps} />
          )
        }
      />
    </nav>
  )
}

function isRouteActive(pathname: string, to: string) {
  return pathname === to || pathname.startsWith(`${to}/`)
}
