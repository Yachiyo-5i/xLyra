import * as React from 'react'
import { fieldFocusClass, fieldSurfaceClass } from '@/components/ui/field-styles'
import { cn } from '@/lib/utils'

/** 单行输入框：填充色底、无描边；高度取 --control-h，移动端自动放大到 44 */
const Input = React.forwardRef<HTMLInputElement, React.ComponentProps<'input'>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        fieldSurfaceClass,
        fieldFocusClass,
        'h-[var(--control-h)] w-full px-3.5 text-body placeholder:text-label-3 file:mr-3 file:border-0 file:bg-transparent file:text-callout file:font-medium file:text-label',
        className,
      )}
      {...props}
    />
  ),
)
Input.displayName = 'Input'

export { Input }
