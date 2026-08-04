import { useTranslation } from 'react-i18next'
import { ImageIcon, MessageSquare } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { PlaygroundMode } from '@/features/playground/lib/types'

type PlaygroundModeSwitcherProps = {
  value: PlaygroundMode
  onChange: (mode: PlaygroundMode) => void
  compact?: boolean
  className?: string
}

export function PlaygroundModeSwitcher({
  value,
  onChange,
  compact = false,
  className,
}: PlaygroundModeSwitcherProps) {
  const { t } = useTranslation('playground')
  const items = [
    { value: 'chat' as const, label: t('mode.chat'), icon: MessageSquare },
    { value: 'image' as const, label: t('mode.image'), icon: ImageIcon },
  ]

  return (
    <div className={cn('inline-flex rounded-full bg-[hsl(var(--surface-subtle))]/60 p-1', className)}>
      {items.map((item) => {
        const Icon = item.icon
        return (
          <button
            key={item.value}
            type="button"
            onClick={() => onChange(item.value)}
            aria-label={item.label}
            aria-pressed={value === item.value}
            title={item.label}
            className={cn(
              'inline-flex items-center justify-center gap-1.5 rounded-full text-sm font-medium transition-colors',
              compact ? 'h-11 w-11' : 'h-8 min-w-0 flex-1 px-3',
              value === item.value
                ? 'bg-[hsl(var(--surface-base))] text-foreground shadow-sm'
                : 'text-muted-soft hover:text-foreground',
            )}
          >
            <Icon className="h-4 w-4 shrink-0" />
            {compact ? null : <span className="truncate">{item.label}</span>}
          </button>
        )
      })}
    </div>
  )
}
