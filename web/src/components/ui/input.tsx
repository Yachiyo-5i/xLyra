import * as React from 'react'
import { cn } from '@/lib/utils'

const Input = React.forwardRef<HTMLInputElement, React.ComponentProps<'input'>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        'glass-field h-11 w-full rounded-md px-4 text-base text-foreground outline-none transition-all duration-200 placeholder:text-[hsl(var(--input-placeholder))] focus:ring-2 focus:ring-[hsl(var(--primary)/0.4)] focus:border-[hsl(var(--primary)/0.6)] focus:[box-shadow:0_0_12px_hsl(var(--primary)/0.15)] disabled:cursor-not-allowed disabled:opacity-50 md:h-10 md:text-sm',
        className,
      )}
      {...props}
    />
  ),
)
Input.displayName = 'Input'

export { Input }
