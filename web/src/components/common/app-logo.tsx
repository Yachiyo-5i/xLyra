import { APP_LOGO_SRC, APP_NAME } from '@/lib/brand'
import { cn } from '@/lib/utils'

type AppLogoProps = {
  className?: string
  imageClassName?: string
  decorative?: boolean
}

export function AppLogo({ className, imageClassName, decorative = false }: AppLogoProps) {
  return (
    <span className={cn('block shrink-0 overflow-hidden bg-[hsl(var(--surface-subtle))]', className)}>
      <img
        src={APP_LOGO_SRC}
        alt={decorative ? '' : APP_NAME}
        aria-hidden={decorative || undefined}
        className={cn('h-full w-full object-cover', imageClassName)}
      />
    </span>
  )
}
