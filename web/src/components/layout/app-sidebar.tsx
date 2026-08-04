import { useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { getAppNavSections } from '@/lib/navigation'
import { useMobileDevice } from '@/hooks/use-media-query'
import { AppLogo } from '@/components/common/app-logo'
import { UserAccountMenu } from '@/components/layout/user-account-menu'

type AppSidebarProps = {
  className?: string
  onNavigate?: () => void
}

export function AppSidebar({ className, onNavigate }: AppSidebarProps) {
  const { t } = useTranslation('common')
  const location = useLocation()
  const isMobileDevice = useMobileDevice()
  const [expandedMenus, setExpandedMenus] = useState<Record<string, boolean>>({})

  function handleNavigate() {
    setExpandedMenus({})
    onNavigate?.()
  }

  const navItemClass =
    'text-muted-soft relative flex w-full appearance-none items-center gap-3 rounded-lg px-3 py-3 text-left font-sans text-sm font-medium leading-5 transition-all duration-150 group'
  const navLabelClass = 'truncate text-sm font-medium leading-5'

  const navSections = getAppNavSections(t).map((section) => ({
    ...section,
    items: section.items.filter((item) => !item.desktopOnly || !isMobileDevice),
  }))

  return (
    <div
      className={cn(
        'glass-panel-strong flex h-full min-h-0 flex-col overflow-hidden rounded-[18px]',
        className,
      )}
    >
      <div className="flex items-center gap-3 border-b border-[hsl(var(--divider))] px-6 py-6">
        <AppLogo className="size-10 rounded-2xl shadow-[0_14px_32px_rgba(0,0,0,0.12)]" decorative />
        <div className="min-w-0 space-y-1">
          <p className="truncate text-base font-semibold text-foreground">xLyra</p>
          <p className="text-muted-soft truncate text-xs">Gateway Dark Admin</p>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-5">
        <div className="space-y-8">
          {navSections.map((section) => (
            <div key={section.key} className="space-y-2">
              <p className="text-faint px-3 text-[11px] font-semibold uppercase tracking-[0.24em]">
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
                          className={cn(
                            navItemClass,
                            isChildActive
                              ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                              : 'hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
                          )}
                        >
                          {isChildActive && (
                            <div className="absolute left-0 top-1/2 -translate-y-1/2 w-1 h-5 rounded-r-md bg-[hsl(var(--primary))] shadow-[0_0_12px_hsl(var(--primary)/0.6)]" />
                          )}
                          <Icon className="h-4 w-4 shrink-0 transition-transform duration-200 group-hover:scale-110" />
                          <span className={navLabelClass}>{item.label}</span>
                          <ChevronDown
                            className={cn(
                              'ml-auto h-4 w-4 shrink-0 transition-transform duration-200',
                              isExpanded ? 'rotate-0' : '-rotate-90',
                            )}
                          />
                        </button>
                        {isExpanded && (
                          <div className="mt-1 ml-4 grid gap-1 border-l border-[hsl(var(--divider))] pl-4">
                            {item.children!.map((child) => {
                              const ChildIcon = child.icon
                              return (
                                <NavLink
                                  key={child.to}
                                  to={child.to}
                                  onClick={handleNavigate}
                                  className={({ isActive }) =>
                                    cn(
                                      'text-muted-soft flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-all duration-150 relative group',
                                      isActive
                                        ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                                        : 'hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
                                    )
                                  }
                                >
                                  {({ isActive }) => (
                                    <>
                                      {isActive && (
                                        <div className="absolute left-0 top-1/2 -translate-y-1/2 w-1 h-4 rounded-r-md bg-[hsl(var(--primary))] shadow-[0_0_12px_hsl(var(--primary)/0.6)]" />
                                      )}
                                      <ChildIcon className="h-3.5 w-3.5 shrink-0 transition-transform duration-200 group-hover:scale-110" />
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
                      className={({ isActive }) =>
                        cn(
                          navItemClass,
                          isActive
                            ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                            : 'hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
                        )
                      }
                    >
                      {({ isActive }) => (
                        <>
                          {isActive && (
                            <div className="absolute left-0 top-1/2 -translate-y-1/2 w-1 h-5 rounded-r-md bg-[hsl(var(--primary))] shadow-[0_0_12px_hsl(var(--primary)/0.6)]" />
                          )}
                          <Icon className="h-4 w-4 shrink-0 transition-transform duration-200 group-hover:scale-110" />
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
