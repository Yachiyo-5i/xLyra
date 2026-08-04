import { Check } from 'lucide-react'
import { useId, useState } from 'react'
import { cn } from '@/lib/utils'

type CheckboxProps = {
  checked?: boolean
  defaultChecked?: boolean
  disabled?: boolean
  id?: string
  label?: string
  description?: string
  className?: string
  ariaLabel?: string
  onCheckedChange?: (checked: boolean) => void
}

export function Checkbox({
  checked,
  defaultChecked = false,
  disabled = false,
  id,
  label,
  description,
  className,
  ariaLabel,
  onCheckedChange,
}: CheckboxProps) {
  const fallbackId = useId()
  const inputId = id ?? fallbackId
  const [internalChecked, setInternalChecked] = useState(defaultChecked)
  const isControlled = checked !== undefined
  const isChecked = isControlled ? checked : internalChecked

  function handleToggle() {
    if (disabled) return

    const next = !isChecked

    if (!isControlled) {
      setInternalChecked(next)
    }

    onCheckedChange?.(next)
  }

  const control = (
    <button
      id={inputId}
      type="button"
      role="checkbox"
      aria-checked={isChecked}
      aria-label={ariaLabel ?? label ?? 'Checkbox'}
      disabled={disabled}
      onClick={handleToggle}
      className={cn(
        'inline-flex size-4 shrink-0 items-center justify-center rounded-[5px] border border-[hsl(var(--glass-border-strong))] bg-[hsl(var(--surface-field))] text-transparent transition-[background-color,border-color,color,box-shadow] duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] disabled:pointer-events-none disabled:opacity-50',
        isChecked && 'border-primary bg-primary text-primary-foreground',
        className,
      )}
    >
      <Check className="size-3" />
    </button>
  )

  if (!label && !description) return control

  return (
    <label
      htmlFor={inputId}
      className={cn(
        'flex cursor-pointer items-start gap-3 rounded-lg text-sm text-foreground',
        disabled && 'cursor-not-allowed opacity-50',
      )}
    >
      <span className="mt-1">{control}</span>
      <span className="space-y-0.5">
        {label ? <span className="block font-medium">{label}</span> : null}
        {description ? <span className="text-muted-soft block">{description}</span> : null}
      </span>
    </label>
  )
}
