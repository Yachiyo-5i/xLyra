import { lazy, Suspense, useState, type CSSProperties, type ReactNode } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { Link } from 'react-router-dom'
import { Check, Globe, Palette, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { AppLogo } from '@/components/common/app-logo'
import { UserAccountMenu } from '@/components/layout/user-account-menu'
import { cn } from '@/lib/utils'

const CommandMenu = lazy(() =>
  import('@/components/common/command-menu').then((module) => ({ default: module.CommandMenu })),
)

const ThemeSwitcher = lazy(() =>
  import('@/components/layout/theme-switcher').then((module) => ({ default: module.ThemeSwitcher })),
)

export function MobileTopbar() {
  const { t } = useTranslation(['common', 'components'])
  const [cmdOpen, setCmdOpen] = useState(false)
  const [themeOpen, setThemeOpen] = useState(false)

  return (
    <>
      <header
        className="mobile-safe-top pointer-events-none absolute inset-x-0 top-0 z-30 px-4"
        style={{ '--mobile-safe-top-extra': '0.75rem' } as CSSProperties}
      >
        <div className="flex items-center justify-between gap-3">
          <div className="pointer-events-auto flex items-center gap-2">
            <Link
              to="/dashboard"
              aria-label={t('common:nav.dashboard')}
              className="grid size-11 shrink-0 place-items-center rounded-full border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-1 shadow-[0_14px_34px_rgba(0,0,0,0.12)] backdrop-blur-[32px] backdrop-saturate-150 transition-transform active:scale-95"
            >
              <AppLogo className="size-full rounded-full" decorative />
            </Link>
            <FloatingIconButton ariaLabel={t('common:actions.search')} onClick={() => setCmdOpen(true)}>
              <Search className="size-4" />
            </FloatingIconButton>
          </div>

          <div className="pointer-events-auto flex items-center gap-2">
            <MobileLanguageButton />
            <FloatingIconButton ariaLabel={t('components:themeSwitcher.openLabel')} onClick={() => setThemeOpen(true)}>
              <Palette className="size-4" />
            </FloatingIconButton>
            <UserAccountMenu mode="avatar" />
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

function MobileLanguageButton() {
  const { i18n } = useTranslation()
  const { t } = useTranslation('components')
  const [open, setOpen] = useState(false)
  const current = normalizeLanguage(i18n.language)
  const languages: Array<{ value: SupportedLanguage; label: string }> = [
    { value: 'zh', label: t('topbar.language.zh') },
    { value: 'en', label: t('topbar.language.en') },
    { value: 'jp', label: t('topbar.language.jp') },
  ]

  function selectLanguage(value: SupportedLanguage) {
    if (value !== current) {
      void i18n.changeLanguage(value)
    }
    setOpen(false)
  }

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        <Button
          variant="ghost"
          size="icon"
          className="size-10 rounded-full border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] text-foreground shadow-[0_14px_34px_rgba(0,0,0,0.1)] backdrop-blur-[32px] backdrop-saturate-150 hover:bg-[hsl(var(--card)/0.34)]"
          aria-label={t('topbar.language.label')}
        >
          <Globe className="size-4" />
        </Button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="end"
          sideOffset={8}
          className="z-[120] w-36 overflow-hidden rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--dialog-surface))] p-1 shadow-[var(--shadow-dialog)] backdrop-blur-xl"
        >
          {languages.map((language) => (
            <button
              key={language.value}
              type="button"
              onClick={() => selectLanguage(language.value)}
              className={cn(
                'flex w-full items-center gap-2 rounded-xl px-3 py-2 text-sm transition-colors',
                language.value === current
                  ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                  : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
              )}
            >
              <span className="min-w-0 flex-1 text-left">{language.label}</span>
              {language.value === current ? <Check className="size-3.5 text-primary" /> : null}
            </button>
          ))}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}

type SupportedLanguage = 'zh' | 'en' | 'jp'

function normalizeLanguage(language?: string): SupportedLanguage {
  if (language?.startsWith('zh')) return 'zh'
  if (language?.startsWith('jp') || language?.startsWith('ja')) return 'jp'
  return 'en'
}

function FloatingIconButton({
  ariaLabel,
  children,
  onClick,
}: {
  ariaLabel: string
  children: ReactNode
  onClick: () => void
}) {
  return (
    <Button
      variant="ghost"
      size="icon"
      className="size-10 rounded-full border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] text-foreground shadow-[0_14px_34px_rgba(0,0,0,0.1)] backdrop-blur-[32px] backdrop-saturate-150 hover:bg-[hsl(var(--card)/0.34)]"
      aria-label={ariaLabel}
      onClick={onClick}
    >
      {children}
    </Button>
  )
}
