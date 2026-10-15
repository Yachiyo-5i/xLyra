import { useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { getAppNavSections } from '@/lib/navigation'
import { useMobileDevice } from '@/hooks/use-media-query'
import { AppLogo } from '@/components/common/app-logo'
import { SystemVersionText } from '@/components/layout/system-version-text'
import { UserAccountMenu } from '@/components/layout/user-account-menu'
import { useAgentAvailability } from '@/features/agent/lib/use-agent-availability'

type AppSidebarProps = {
  className?: string
  onNavigate?: () => void
}

export function AppSidebar({ className, onNavigate }: AppSidebarProps) {
  const { t } = useTranslation('common')
  const location = useLocation()
  const isMobileDevice = useMobileDevice()
  const agentAvailable = useAgentAvailability()
  const [expandedMenus, setExpandedMenus] = useState<Record<string, boolean>>({})

  function handleNavigate() {
    setExpandedMenus({})
    onNavigate?.()
  }

  // 尺寸沿用原侧边栏：14 / 500、行高 20、内边距 12 × 12
  const navItemClass =
    'relative flex w-full appearance-none items-center gap-3 rounded-lg px-3 py-3 text-left text-sm font-medium leading-5 transition-colors duration-150'
  const navItemActiveClass = 'bg-fill-3 text-label'
  const navItemIdleClass = 'text-label-2 hover:bg-fill-4 hover:text-label'
  const navLabelClass = 'truncate text-sm font-medium leading-5'

  const navSections = getAppNavSections(t, { agent: agentAvailable }).map((section) => ({
    ...section,
    items: section.items.filter((item) => !item.desktopOnly || !isMobileDevice),
  }))

  return (
    // 材质由外层容器提供（桌面为 Liquid Glass 浮动侧边栏），这里只负责内容
    <div className={cn('flex h-full min-h-0 flex-col overflow-hidden', className)}>
      <div className="flex items-center gap-3 border-b border-separator px-6 py-6">
        <AppLogo className="size-10 rounded-2xl shadow-[0_14px_32px_rgba(0,0,0,0.12)]" decorative />
        <div className="min-w-0 space-y-1">
          <p className="truncate text-base font-semibold text-label">xLyra</p>
          <p className="truncate text-xs text-label-3">
            <SystemVersionText />
          </p>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-5">
        <div className="space-y-8">
          {navSections.map((section) => (
            <div key={section.key} className="space-y-2">
              <p className="px-3 text-[11px] font-semibold text-label-3">
                {section.label}
              </p>
              <nav className="grid gap-1">
                {section.items.map((item) => {
                  const Icon = item.icon
                  const hasChildren = item.children && item.children.length > 0
                  const isChildActive = item.children?.some(
                    (child) => location.pathname === child.to || location.pathname.startsWith(`${child.to}/`),
                  )
                  const isExpanded = isChildActive || (expandedMenus[item.to] ?? false)

                  if (hasChildren) {
                    return (
                      <div key={item.to}>
                        <button
                          type="button"
                          onClick={() =>
                            setExpandedMenus((prev) => ({
                              ...prev,
                              [item.to]: !isExpanded,
                            }))
                          }
                          className={cn(navItemClass, isChildActive ? navItemActiveClass : navItemIdleClass)}
                        >
                          <Icon className={cn('h-4 w-4 shrink-0', isChildActive && 'text-tint')} />
                          <span className={navLabelClass}>{item.label}</span>
                          <ChevronDown
                            className={cn(
                              'ml-auto h-4 w-4 shrink-0 text-label-3 transition-transform duration-200',
                              isExpanded ? 'rotate-0' : '-rotate-90',
                            )}
                          />
                        </button>
                        {isExpanded && (
                          <div className="mt-1 ml-4 grid gap-1 border-l border-separator pl-4">
                            {item.children!.map((child) => {
                              const ChildIcon = child.icon
                              return (
                                <NavLink
                                  key={child.to}
                                  to={child.to}
                                  onClick={handleNavigate}
                                  className={({ isActive }) =>
                                    cn(
                                      'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors duration-150',
                                      isActive ? navItemActiveClass : navItemIdleClass,
                                    )
                                  }
                                >
                                  {({ isActive }) => (
                                    <>
                                      <ChildIcon className={cn('h-3.5 w-3.5 shrink-0', isActive && 'text-tint')} />
                                      <span className="truncate">{child.label}</span>
                                    </>
                                  )}
                                </NavLink>
                              )
                            })}
                          </div>
                        )}
                      </div>
                    )
                  }

                  return (
                    <NavLink
                      key={item.to}
                      to={item.to}
                      onClick={handleNavigate}
                      className={({ isActive }) => cn(navItemClass, isActive ? navItemActiveClass : navItemIdleClass)}
                    >
                      {({ isActive }) => (
                        <>
                          <Icon className={cn('h-4 w-4 shrink-0', isActive && 'text-tint')} />
                          <span className={navLabelClass}>{item.label}</span>
                        </>
                      )}
                    </NavLink>
                  )
                })}
              </nav>
            </div>
          ))}
        </div>
      </div>

      <div className="px-4 py-4">
        <UserAccountMenu onNavigate={onNavigate} />
      </div>
    </div>
  )
}
