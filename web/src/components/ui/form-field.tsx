import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '@/lib/utils'

type FormFieldProps = HTMLAttributes<HTMLDivElement> & {
  label?: ReactNode
  description?: ReactNode
  error?: ReactNode
  htmlFor?: string
  required?: boolean
}

/**
 * 表单字段：标签 → 控件 → 说明 / 错误。
 * 标签与控件间距 6；有错误时用错误替换说明，避免两段小字堆在一起。
 */
export function FormField({
  className,
  label,
  description,
  error,
  htmlFor,
  required,
  children,
  ...props
}: FormFieldProps) {
  return (
    <div className={cn('flex flex-col gap-1.5', className)} {...props}>
      {label ? (
        <label htmlFor={htmlFor} className="flex items-center gap-1 text-subhead text-label">
          <span>{label}</span>
          {required ? (
            <span className="text-danger" aria-hidden="true">
              *
            </span>
          ) : null}
        </label>
      ) : null}
      {children}
      {error ? (
        <p role="alert" className="text-footnote text-danger">
          {error}
        </p>
      ) : description ? (
        <p className="text-footnote text-label-2">{description}</p>
      ) : null}
    </div>
  )
}
