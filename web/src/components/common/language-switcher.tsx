import { useState } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { Check, ChevronDown, Globe } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type SupportedLanguage = 'zh' | 'en' | 'jp'

function normalizeLanguage(language?: string): SupportedLanguage {
  if (language?.startsWith('zh')) return 'zh'
  if (language?.startsWith('jp') || language?.startsWith('ja')) return 'jp'
  return 'en'
}

export function LanguageSwitcher() {
  const { i18n } = useTranslation()
  const { t } = useTranslation('components')
  const [open, setOpen] = useState(false)
  const current = normalizeLanguage(i18n.language)

  function selectLanguage(value: SupportedLanguage) {
    if (value !== current) {
      void i18n.changeLanguage(value)
    }
    setOpen(false)
  }

  const languages: Array<{ value: SupportedLanguage; label: string }> = [
    { value: 'zh', label: t('topbar.language.zh') },
    { value: 'en', label: t('topbar.language.en') },
    { value: 'jp', label: t('topbar.language.jp') },
  ]
  const currentLabel = languages.find((language) => language.value === current)?.label ?? 'EN'

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="gap-1.5 px-2 text-xs"
          aria-label={t('topbar.language.label')}
        >
          <Globe className="size-3.5" />
          {currentLabel}
          <ChevronDown className={cn('size-3 transition-transform', open && 'rotate-180')} />
        </Button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="end"
          sideOffset={8}
          className="z-[120] w-36 overflow-hidden rounded-xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--dialog-surface))] p-1 shadow-[var(--shadow-dialog)] backdrop-blur-xl"
        >
          {languages.map((language) => (
            <button
              key={language.value}
              type="button"
              onClick={() => selectLanguage(language.value)}
              className={cn(
                'flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors',
                language.value === current
                  ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                  : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
              )}
            >
              <span className="min-w-0 flex-1 text-left">{language.label}</span>
              {language.value === current ? <Check className="h-3.5 w-3.5 text-primary" /> : null}
            </button>
          ))}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
