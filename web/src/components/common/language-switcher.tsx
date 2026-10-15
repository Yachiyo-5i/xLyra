import { useState } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { Check, ChevronDown, Globe } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useLiquidGlassRef } from '@/components/glass/use-liquid-glass'
import { Button } from '@/components/ui/button'
import { menuButtonClass, popoverPanelClass } from '@/components/ui/field-styles'
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
  const glassRef = useLiquidGlassRef<HTMLDivElement>('regular')
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
          className="gap-1.5 px-3"
          aria-label={t('topbar.language.label')}
        >
          <Globe className="size-3.5" />
          {currentLabel}
          <ChevronDown className={cn('size-3 text-label-3 transition-transform', open && 'rotate-180')} />
        </Button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          ref={glassRef}
          align="end"
          sideOffset={8}
          className={cn(popoverPanelClass, 'z-[120] w-40 origin-(--radix-popover-content-transform-origin)')}
        >
          {languages.map((language) => (
            <button
              key={language.value}
              type="button"
              onClick={() => selectLanguage(language.value)}
              className={menuButtonClass}
            >
              <span className="flex size-4 shrink-0 items-center justify-center">
                {language.value === current ? <Check className="size-3.5" strokeWidth={2.5} /> : null}
              </span>
              <span className="min-w-0 flex-1">{language.label}</span>
            </button>
          ))}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
