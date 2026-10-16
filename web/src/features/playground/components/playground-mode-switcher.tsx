import { useTranslation } from 'react-i18next'
import { ImageIcon, MessageSquare } from 'lucide-react'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'
import type { PlaygroundMode } from '@/features/playground/lib/types'

type PlaygroundModeSwitcherProps = {
  value: PlaygroundMode
  onChange: (mode: PlaygroundMode) => void
  /** 移动端：只显示图标 */
  compact?: boolean
  className?: string
}

/** 对话 / 图片切换：与全局一致的分段控件（滑动的浮起胶囊） */
export function PlaygroundModeSwitcher({ value, onChange, compact = false, className }: PlaygroundModeSwitcherProps) {
  const { t } = useTranslation('playground')
  const items = [
    { value: 'chat' as const, label: t('mode.chat'), icon: MessageSquare },
    { value: 'image' as const, label: t('mode.image'), icon: ImageIcon },
  ]

  return (
    <Tabs value={value} onValueChange={(next) => onChange(next as PlaygroundMode)} className={className}>
      <TabsList className={cn(compact && 'h-11 w-auto')}>
        {items.map((item) => {
          const Icon = item.icon
          return (
            <TabsTrigger
              key={item.value}
              value={item.value}
              aria-label={item.label}
              title={item.label}
              className={cn('gap-1.5', compact && 'w-11 px-0')}
            >
              <Icon className="size-4 shrink-0" />
              {compact ? null : <span className="truncate">{item.label}</span>}
            </TabsTrigger>
          )
        })}
      </TabsList>
    </Tabs>
  )
}
