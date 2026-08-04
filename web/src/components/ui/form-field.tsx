import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '@/lib/utils'

type FormFieldProps = HTMLAttributes<HTMLDivElement> & {
  label?: ReactNode
  description?: ReactNode
  error?: ReactNode
  htmlFor?: string
  required?: boolean
}

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
    <div className={cn('space-y-2', className)} {...props}>
      {label ? (
        <label htmlFor={htmlFor} className="flex items-center gap-2 text-sm font-medium text-foreground">
          <span>{label}</span>
          {required ? <span className="text-[hsl(var(--badge-warning-text))]">*</span> : null}
        </label>
      ) : null}
      {children}
      {description ? <p className="text-muted-soft text-xs leading-5">{description}</p> : null}
      {error ? (
        <p className="text-xs leading-5 text-[hsl(var(--destructive))]">{error}</p>
      ) : null}
    </div>
  )
}
