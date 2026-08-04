import * as React from 'react'
import { cn } from '@/lib/utils'

const TextArea = React.forwardRef<HTMLTextAreaElement, React.ComponentProps<'textarea'>>(
  ({ className, ...props }, ref) => (
    <textarea
      ref={ref}
      className={cn(
        'glass-field min-h-[112px] w-full resize-none rounded-md px-4 py-3 text-base text-foreground outline-none transition-all duration-150 placeholder:text-[hsl(var(--input-placeholder))] focus:ring-2 focus:ring-[hsl(var(--ring-soft))] disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
        className,
      )}
      {...props}
    />
  ),
)
TextArea.displayName = 'TextArea'

export { TextArea }
