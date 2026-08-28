import { lazy, Suspense, useState } from 'react'
import { Palette } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { LanguageSwitcher } from '@/components/common/language-switcher'
import { useAuthStore } from '@/stores/auth-store'

const ThemeSwitcher = lazy(() =>
  import('@/components/layout/theme-switcher').then((module) => ({ default: module.ThemeSwitcher })),
)

function ThemeSwitcherTrigger({ onClick }: { onClick: () => void }) {
  const { t } = useTranslation('components')

  return (
    <Button variant="ghost" size="icon" aria-label={t('themeSwitcher.openLabel')} onClick={onClick}>
      <Palette className="size-4" />
    </Button>
  )
}

export function TopbarUserControls() {
  const user = useAuthStore((state) => state.user)
  const authStatus = useAuthStore((state) => state.status)
  const [themeOpen, setThemeOpen] = useState(false)
  const isAuthChecking = !user && (authStatus === 'idle' || authStatus === 'checking')

  return (
    <>
      <div className="flex items-center gap-3">
        <div className="hidden rounded-full bg-[hsl(var(--surface-subtle))] px-3 py-2 text-xs text-[hsl(var(--text-muted-soft))] xl:block">
          {isAuthChecking ? <Skeleton className="h-3 w-28 rounded-full" /> : user?.displayName ?? user?.username ?? 'Authenticated admin'}
        </div>
        <LanguageSwitcher />
        <ThemeSwitcherTrigger onClick={() => setThemeOpen(true)} />
      </div>
      {themeOpen ? (
        <Suspense fallback={null}>
          <ThemeSwitcher compact hideTrigger open={themeOpen} onOpenChange={setThemeOpen} />
        </Suspense>
      ) : null}
    </>
  )
}
