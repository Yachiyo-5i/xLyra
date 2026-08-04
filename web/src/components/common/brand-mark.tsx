import { useResolvedTheme } from '@/hooks/theme-context'
import { cn } from '@/lib/utils'
import { resolveThemedIconPath } from '@/components/common/brand-utils'

export function BrandMark({
  iconPath,
  label,
  fallback,
  fallbackText,
  highlightedFallback,
  size = 'md',
}: {
  iconPath?: string
  label: string
  fallback: string
  fallbackText?: string
  highlightedFallback?: boolean
  size?: 'xs' | 'sm' | 'md'
}) {
  const resolvedMode = useResolvedTheme()
  const boxClassName = size === 'xs' ? 'h-4 w-4 rounded-sm' : size === 'sm' ? 'h-6 w-6 rounded-md' : 'h-11 w-11 rounded-md'
  const imageClassName = size === 'xs' ? 'h-3 w-3' : size === 'sm' ? 'h-4 w-4' : 'h-7 w-7'
  const textClassName = size === 'xs' ? 'text-[8px]' : size === 'sm' ? 'text-[11px]' : 'text-sm font-semibold'
  const text = fallbackText ?? fallback.slice(0, 1).toUpperCase()

  return (
    <span
      className={cn(
        'flex shrink-0 items-center justify-center',
        highlightedFallback
          ? '[background-color:hsl(var(--primary)/0.18)] text-primary'
          : 'bg-[hsl(var(--surface-subtle))]',
        boxClassName,
      )}
    >
      {iconPath ? (
        <img
          src={resolveThemedIconPath(iconPath, label, resolvedMode)}
          alt={label}
          className={cn('object-contain', imageClassName)}
        />
      ) : (
        <span className={cn(highlightedFallback ? 'text-primary' : 'text-foreground', textClassName)}>{text}</span>
      )}
    </span>
  )
}
