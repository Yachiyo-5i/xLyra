import { HoverDetails } from '@/components/common/hover-details'
import { cn } from '@/lib/utils'

export function OAuthErrorPreview({
  label,
  message,
  compact = false,
}: {
  label: string
  message: string
  compact?: boolean
}) {
  return (
    <>
      <div className={cn('font-medium text-foreground', compact ? 'mb-1 text-xs' : 'mb-2 text-sm')}>{label}</div>
      <HoverDetails
        asChild
        title={label}
        titleClassName="text-red-300"
        contentClassName="w-[560px]"
        content={<div className="whitespace-pre-wrap break-words text-muted-foreground">{message}</div>}
      >
        <div className={cn('rounded-md border border-red-500/20 bg-red-500/5 px-3 py-2', compact ? 'h-[54px]' : 'h-[64px]')}>
          <div
            className={cn(
              'overflow-hidden break-words text-red-300',
              compact ? 'line-clamp-2 text-xs leading-5' : 'line-clamp-2 text-sm leading-5',
            )}
          >
            {message}
          </div>
        </div>
      </HoverDetails>
    </>
  )
}
