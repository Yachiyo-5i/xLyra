import { useState } from 'react'
import { Check, Monitor, MoonStar, Palette, SunMedium } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Draw, DrawBody, DrawContent, DrawDescription, DrawHeader, DrawTitle, DrawTrigger } from '@/components/ui/draw'
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { useMobileLayout } from '@/hooks/use-media-query'
import { useTheme } from '@/hooks/theme-context'
import {
  getResolvedThemeLabel,
  getThemeModeOptions,
  themeAccentOptions,
  themeBaseOptions,
  type ThemeMode,
} from '@/lib/theme'
import { cn } from '@/lib/utils'

type ThemeSwitcherProps = {
  compact?: boolean
  className?: string
  hideTrigger?: boolean
  open?: boolean
  onOpenChange?: (open: boolean) => void
}

const modeIcons: Record<ThemeMode, typeof SunMedium> = {
  light: SunMedium,
  dark: MoonStar,
  system: Monitor,
}

export function ThemeSwitcher({
  compact = false,
  className,
  hideTrigger = false,
  open: openProp,
  onOpenChange,
}: ThemeSwitcherProps) {
  const [openState, setOpenState] = useState(false)
  const { t } = useTranslation('components')
  const { t: tCommon } = useTranslation('common')
  const { mode, base, accent, resolvedMode, setMode, setBase, setAccent } = useTheme()
  const isMobile = useMobileLayout()
  const modeOptions = getThemeModeOptions(tCommon)
  const open = openProp ?? openState
  const setOpen = onOpenChange ?? setOpenState
  const description = t('themeSwitcher.description', { mode: getResolvedThemeLabel(resolvedMode, tCommon) })
  const trigger = hideTrigger ? null : (
    compact ? (
      <Button variant="ghost" size="icon" className={className} aria-label={t('themeSwitcher.openLabel')}>
        <Palette className="size-4" />
      </Button>
    ) : (
      <Button variant="secondary" className={cn('w-full justify-start', className)}>
        <Palette className="size-4" />
        <span className="flex min-w-0 flex-col items-start text-left">
          <span className="text-sm font-medium text-foreground">{t('themeSwitcher.appearance')}</span>
          <span className="text-faint text-xs">
            {modeOptions.find((option) => option.value === mode)?.label} ·{' '}
            {themeBaseOptions.find((option) => option.value === base)?.label}
          </span>
        </span>
      </Button>
    )
  )

  const content = (
    <>
      <section className="space-y-3">
        <h3 className="text-sm font-semibold text-foreground">{t('themeSwitcher.mode')}</h3>
        <div className="grid grid-cols-3 gap-2 rounded-xl bg-[hsl(var(--surface-subtle))] p-1">
          {modeOptions.map((option) => {
            const Icon = modeIcons[option.value]
            const active = option.value === mode

            return (
              <button
                key={option.value}
                type="button"
                onClick={() => setMode(option.value)}
                className={cn(
                  'flex h-9 items-center justify-center gap-1.5 rounded-lg px-2 text-sm font-medium transition-colors duration-150',
                  active
                    ? 'bg-[hsl(var(--surface-panel))] text-foreground shadow-[0_8px_18px_rgba(4,8,18,0.08)]'
                    : 'text-muted-soft hover:bg-[hsl(var(--surface-soft-hover))] hover:text-foreground',
                )}
              >
                <Icon className="size-4 shrink-0" />
                <span className="truncate">{option.shortLabel}</span>
              </button>
            )
          })}
        </div>
        {mode === 'system' ? (
          <p className="text-faint text-xs">{t('themeSwitcher.systemHint', { mode: getResolvedThemeLabel(resolvedMode, tCommon) })}</p>
        ) : null}
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold text-foreground">{t('themeSwitcher.base')}</h3>
        <div className="space-y-2">
          {themeBaseOptions.map((option) => {
            const active = option.value === base

            return (
              <button
                key={option.value}
                type="button"
                onClick={() => setBase(option.value)}
                className={cn(
                  'flex w-full items-center justify-between rounded-xl px-3 py-3 text-left transition-colors duration-150',
                  active
                    ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                    : 'bg-[hsl(var(--surface-subtle))] text-foreground hover:bg-[hsl(var(--surface-soft-hover))]',
                )}
              >
                <span className="flex items-center gap-3">
                  <span
                    className="size-4 rounded-full border border-[hsl(var(--glass-border-strong))]"
                    style={{ backgroundColor: option.swatch }}
                  />
                  <span className="text-sm font-medium">{option.label}</span>
                </span>
                <Check className={cn('size-4 transition-opacity', active ? 'opacity-100' : 'opacity-0')} />
              </button>
            )
          })}
        </div>
      </section>

      <section className="space-y-3">
        <h3 className="text-sm font-semibold text-foreground">{t('themeSwitcher.accent')}</h3>
        <div className="grid grid-cols-2 gap-2">
          {themeAccentOptions.map((option) => {
            const active = option.value === accent
            const swatch =
              option.value === 'default'
                ? themeBaseOptions.find((baseOption) => baseOption.value === base)?.swatch ?? option.swatch
                : option.swatch

            return (
              <button
                key={option.value}
                type="button"
                onClick={() => setAccent(option.value)}
                className={cn(
                  'flex items-center justify-between rounded-xl px-3 py-3 text-left transition-colors duration-150',
                  active
                    ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                    : 'bg-[hsl(var(--surface-subtle))] text-foreground hover:bg-[hsl(var(--surface-soft-hover))]',
                )}
              >
                <span className="flex items-center gap-3">
                  <span
                    className="size-4 rounded-full border border-[hsl(var(--glass-border-strong))]"
                    style={{ backgroundColor: swatch }}
                  />
                  <span className="text-sm font-medium">{option.label}</span>
                </span>
                <Check className={cn('size-4 transition-opacity', active ? 'opacity-100' : 'opacity-0')} />
              </button>
            )
          })}
        </div>
      </section>
    </>
  )

  if (isMobile) {
    return (
      <Draw open={open} onOpenChange={setOpen}>
        {trigger ? <DrawTrigger asChild>{trigger}</DrawTrigger> : null}
        <DrawContent side="bottom">
          <DrawHeader>
            <DrawTitle>{t('themeSwitcher.title')}</DrawTitle>
            <DrawDescription>{description}</DrawDescription>
          </DrawHeader>
          <DrawBody className="space-y-6">{content}</DrawBody>
        </DrawContent>
      </Draw>
    )
  }

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      {trigger ? <SheetTrigger asChild>{trigger}</SheetTrigger> : null}
      <SheetContent side="right">
        <SheetHeader>
          <SheetTitle>{t('themeSwitcher.title')}</SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        <SheetBody className="space-y-6">{content}</SheetBody>
      </SheetContent>
    </Sheet>
  )
}
