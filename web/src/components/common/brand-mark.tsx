import { cn } from '@/lib/utils'

export function BrandMark({
  iconPath,
  label,
  fallback,
  fallbackText,
  highlightedFallback,
  transparent,
  size = 'md',
}: {
  iconPath?: string
  label: string
  fallback: string
  fallbackText?: string
  highlightedFallback?: boolean
  transparent?: boolean
  size?: 'xs' | 'sm' | 'md'
}) {
  const boxClassName = size === 'xs' ? 'h-4 w-4 rounded-sm' : size === 'sm' ? 'h-6 w-6 rounded-control' : 'h-11 w-11 rounded-control'
  const imageClassName = size === 'xs' ? 'h-3 w-3' : size === 'sm' ? 'h-4 w-4' : 'h-7 w-7'
  const textClassName = size === 'xs' ? 'text-[8px]' : size === 'sm' ? 'text-[11px]' : 'text-headline'
  const text = fallbackText ?? fallback.slice(0, 1).toUpperCase()

  return (
    <span
      className={cn(
        'flex shrink-0 items-center justify-center',
        transparent
          ? 'bg-transparent'
          : highlightedFallback
            ? 'bg-tint/18 text-tint'
            : 'bg-fill-3',
        boxClassName,
      )}
    >
      {iconPath ? (
        <img
          src={iconPath}
          alt={label}
          className={cn('object-contain', imageClassName)}
        />
      ) : (
        <span className={cn(highlightedFallback ? 'text-tint' : 'text-label', textClassName)}>{text}</span>
      )}
    </span>
  )
}
