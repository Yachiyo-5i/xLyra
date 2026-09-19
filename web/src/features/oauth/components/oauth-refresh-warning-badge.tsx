import { HoverDetails } from '@/components/common/hover-details'
import { cn } from '@/lib/utils'

export function OAuthRefreshWarningBadge({
  label,
  message,
  className,
}: {
  label: string
  message: string
  className?: string
}) {
  return (
    <HoverDetails
      asChild
      title={label}
      titleClassName="text-amber-300"
      contentClassName="w-[360px]"
      content={<div className="whitespace-pre-wrap break-words text-muted-foreground">{message}</div>}
    >
      <span className={cn('inline-flex size-5 shrink-0 items-center justify-center rounded-full border border-amber-500/35 bg-amber-500/12 text-[13px] font-black leading-none text-amber-300', className)}>!</span>
    </HoverDetails>
  )
}
