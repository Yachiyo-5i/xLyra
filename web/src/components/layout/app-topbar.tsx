import { lazy, Suspense, useEffect, useState } from 'react'
import { Menu, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { TopbarInlineTitle } from '@/components/layout/topbar-inline-title'
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
    <Button variant="glass" className="pointer-events-auto gap-2 pl-3.5 pr-2 text-callout font-normal text-label-2" onClick={onClick}>
      <Search className="size-4" />
      <span className="hidden sm:inline">{t('actions.search')}…</span>
      <kbd className="ml-2 hidden h-6 items-center rounded-full bg-fill-3 px-2 font-mono text-caption-1 text-label-2 sm:inline-flex">
        ⌘K
      </kbd>
    </Button>
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
      {/* 顶栏不铺整条背景：只有浮在内容上方的玻璃控件组，内容区顶部由 scroll edge effect 过渡 */}
      <header className="pointer-events-none absolute inset-x-0 top-0 z-30 flex items-center justify-between gap-3 px-4 pt-4 md:px-8 lg:px-10">
        <div className="flex items-center gap-2">
          <Button
            variant="glass"
            size="icon"
            className="pointer-events-auto lg:hidden"
            aria-label={t('navigation.open')}
            onClick={onMenuClick}
          >
            <Menu />
          </Button>
          <CommandMenuTrigger onClick={() => setCmdOpen(true)} />
        </div>

        {/* 大标题滚出后的行内标题；header 顶部有 16 的内边距，所以下移 8 与按钮行居中 */}
        <TopbarInlineTitle className="top-[calc(50%+8px)]" />

        <TopbarUserControls />
      </header>
      {cmdOpen ? (
        <Suspense fallback={null}>
          <CommandMenu open={cmdOpen} onOpenChange={setCmdOpen} />
        </Suspense>
      ) : null}
    </>
  )
}
