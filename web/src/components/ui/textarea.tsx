import * as React from 'react'
import { fieldFocusClass, fieldSurfaceClass } from '@/components/ui/field-styles'
import { cn } from '@/lib/utils'

/** 多行输入框：与 Input 同一外观，行高放宽便于阅读多行内容 */
const TextArea = React.forwardRef<HTMLTextAreaElement, React.ComponentProps<'textarea'>>(
  ({ className, ...props }, ref) => (
    <textarea
      ref={ref}
      className={cn(
        fieldSurfaceClass,
        fieldFocusClass,
        'min-h-[112px] w-full resize-none px-3.5 py-2.5 text-body leading-relaxed placeholder:text-label-3',
        className,
      )}
      {...props}
    />
  ),
)
TextArea.displayName = 'TextArea'

export { TextArea }
