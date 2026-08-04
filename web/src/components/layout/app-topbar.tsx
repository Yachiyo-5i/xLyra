import { lazy, Suspense, useEffect, useState } from 'react'
import { Menu, Palette, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { LanguageSwitcher } from '@/components/common/language-switcher'
import { useAuthStore } from '@/stores/auth-store'

type AppTopbarProps = {
  onMenuClick?: () => void
}

const CommandMenu = lazy(() =>
  import('@/components/common/command-menu').then((module) => ({ default: module.CommandMenu })),
)

const ThemeSwitcher = lazy(() =>
  import('@/components/layout/theme-switcher').then((module) => ({ default: module.ThemeSwitcher })),
)

function CommandMenuTrigger({ onClick }: { onClick: () => void }) {
  const { t } = useTranslation('common')

  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-2 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))]/50 px-3 py-1.5 text-sm text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
    >
      <Search className="h-3.5 w-3.5" />
      <span className="hidden sm:inline">{t('actions.search')}...</span>
      <kbd className="hidden items-center rounded border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface))] px-1.5 py-0.5 text-[10px] font-medium sm:inline-flex">
        ⌘K
      </kbd>
    </button>
  )
}

function ThemeSwitcherTrigger({ onClick }: { onClick: () => void }) {
  const { t } = useTranslation('components')

  return (
    <Button variant="ghost" size="icon" aria-label={t('themeSwitcher.openLabel')} onClick={onClick}>
      <Palette className="size-4" />
    </Button>
  )
}

export function AppTopbar({ onMenuClick }: AppTopbarProps) {
  const { t } = useTranslation('components')
  const user = useAuthStore((state) => state.user)
  const authStatus = useAuthStore((state) => state.status)
  const [cmdOpen, setCmdOpen] = useState(false)
  const [themeOpen, setThemeOpen] = useState(false)
  const isAuthChecking = !user && (authStatus === 'idle' || authStatus === 'checking')

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setCmdOpen((open) => !open)
      }
    }

    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [])

  return (
    <>
      <header className="sticky top-0 z-40 shrink-0 bg-transparent pt-2">
        <div className="flex items-center justify-between px-4 py-4 lg:px-8 lg:py-5">
          <div className="flex items-center gap-3">
            <Button
              variant="ghost"
              size="icon"
              className="lg:hidden"
              aria-label={t('navigation.open')}
              onClick={onMenuClick}
            >
              <Menu className="h-4 w-4" />
            </Button>
            <CommandMenuTrigger onClick={() => setCmdOpen(true)} />
          </div>

          <div className="flex items-center gap-3">
            <div className="hidden rounded-full bg-[hsl(var(--surface-subtle))] px-3 py-2 text-xs text-[hsl(var(--text-muted-soft))] xl:block">
              {isAuthChecking ? <Skeleton className="h-3 w-28 rounded-full" /> : user?.displayName ?? user?.username ?? 'Authenticated admin'}
            </div>
            <LanguageSwitcher />
            <ThemeSwitcherTrigger onClick={() => setThemeOpen(true)} />
          </div>
        </div>
      </header>
      {cmdOpen ? (
        <Suspense fallback={null}>
          <CommandMenu open={cmdOpen} onOpenChange={setCmdOpen} />
        </Suspense>
      ) : null}
      {themeOpen ? (
        <Suspense fallback={null}>
          <ThemeSwitcher compact hideTrigger open={themeOpen} onOpenChange={setThemeOpen} />
        </Suspense>
      ) : null}
    </>
  )
}
