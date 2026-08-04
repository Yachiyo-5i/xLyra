import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ExternalLink, Gauge, Network as SystemProxyIcon, SlidersHorizontal, Tags, UserRound } from 'lucide-react'
import { PageHeader } from '@/components/common/page-header'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'

export function GlobalConfigPage() {
  const { t } = useTranslation('settings')
  const location = useLocation()
  const navigate = useNavigate()

  const globalSettingsNav = [
    { to: '/settings/global/general', label: t('global.nav.general'), icon: SlidersHorizontal },
    { to: '/settings/global/profile', label: t('global.nav.profile'), icon: UserRound },
    { to: '/settings/global/system-proxy', label: t('global.nav.systemProxy'), icon: SystemProxyIcon },    { to: '/settings/global/rate-limit', label: t('global.nav.rateLimit'), icon: Gauge },
    { to: '/settings/global/site-groups', label: t('global.nav.siteGroups'), icon: Tags },
    { to: '/settings/global/portal', label: t('global.nav.portal'), icon: ExternalLink },
  ] as const
  const activePath = globalSettingsNav.find((item) => (
    location.pathname === item.to || location.pathname.startsWith(`${item.to}/`)
  ))?.to ?? globalSettingsNav[0].to

  return (
    <div className="flex min-h-full flex-col gap-4 pb-2 lg:h-full lg:min-h-0 lg:gap-7 lg:overflow-hidden lg:pb-0">
      <PageHeader
        eyebrow={t('global.eyebrow')}
        title={t('global.title')}
        description={t('global.description')}
      />

      <div className="lg:hidden">
        <Select value={activePath} onValueChange={(value) => navigate(value)}>
          <SelectTrigger className="h-10 w-full min-w-0">
            <SelectValue />
          </SelectTrigger>
          <SelectContent searchable={false}>
            {globalSettingsNav.map((item) => {
              const Icon = item.icon
              return (
                <SelectItem
                  key={item.to}
                  value={item.to}
                  icon={<Icon className="h-4 w-4 text-muted-soft" />}
                >
                  {item.label}
                </SelectItem>
              )
            })}
          </SelectContent>
        </Select>
      </div>

      <div className="grid min-h-0 flex-1 gap-8 lg:border-t lg:border-[hsl(var(--divider))] lg:pt-6 lg:grid-cols-[220px_minmax(0,1fr)]">
        <aside className="hidden min-h-0 overflow-hidden lg:block">
          <nav className="sticky top-0 grid gap-1">
            {globalSettingsNav.map((item) => {
              const Icon = item.icon
              return (
                <NavLink
                  key={item.to}
                  to={item.to}
                  className={({ isActive }) =>
                    cn(
                      'group flex items-center gap-3 rounded-md px-3 py-2.5 text-sm font-medium transition-colors',
                      isActive
                        ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                        : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
                    )
                  }
                >
                  {({ isActive }) => (
                    <>
                      <Icon
                        className={cn(
                          'h-4 w-4 shrink-0 transition-colors',
                          isActive ? 'text-primary' : 'text-muted-soft group-hover:text-foreground',
                        )}
                      />
                      <span className="min-w-0 truncate">{item.label}</span>
                    </>
                  )}
                </NavLink>
              )
            })}
          </nav>
        </aside>

        <section className="min-w-0 lg:min-h-0 lg:overflow-y-auto lg:overscroll-contain lg:pr-2">
          <Outlet />
        </section>
      </div>
    </div>
  )
}
