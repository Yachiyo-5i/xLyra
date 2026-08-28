import { lazy, Suspense, useEffect, useState } from 'react'
import { Menu, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { TopbarUserControls } from '@/components/layout/topbar-user-controls'

type AppTopbarProps = {
  onMenuClick?: () => void
}

const CommandMenu = lazy(() =>
  import('@/components/common/command-menu').then((module) => ({ default: module.CommandMenu })),
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

export function AppTopbar({ onMenuClick }: AppTopbarProps) {
  const { t } = useTranslation('components')
  const [cmdOpen, setCmdOpen] = useState(false)

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

          <TopbarUserControls />
        </div>
      </header>
      {cmdOpen ? (
        <Suspense fallback={null}>
          <CommandMenu open={cmdOpen} onOpenChange={setCmdOpen} />
        </Suspense>
      ) : null}
    </>
  )
}
