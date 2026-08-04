import { Circle } from 'lucide-react'
import { createContext, useId, useState, useContext } from 'react'
import { cn } from '@/lib/utils'

type RadioGroupContextValue = {
  value: string | undefined
  onChange: (val: string) => void
  disabled: boolean
  name: string
}

const RadioGroupContext = createContext<RadioGroupContextValue | null>(null)

type RadioGroupProps = {
  value?: string
  defaultValue?: string
  disabled?: boolean
  className?: string
  children: React.ReactNode
  onValueChange?: (value: string) => void
}

export function RadioGroup({
  value,
  defaultValue,
  disabled = false,
  className,
  children,
  onValueChange,
}: RadioGroupProps) {
  const fallbackName = useId()
  const [internalValue, setInternalValue] = useState(defaultValue)
  const isControlled = value !== undefined
  const currentValue = isControlled ? value : internalValue

  function handleChange(val: string) {
    if (disabled) return
    if (!isControlled) {
      setInternalValue(val)
    }
    onValueChange?.(val)
  }

  return (
    <RadioGroupContext.Provider
      value={{ value: currentValue, onChange: handleChange, disabled, name: fallbackName }}
    >
      <div role="radiogroup" className={cn('grid gap-3', className)}>
        {children}
      </div>
    </RadioGroupContext.Provider>
  )
}

type RadioProps = {
  value: string
  id?: string
  label?: string
  description?: string
  className?: string
  ariaLabel?: string
}

export function Radio({
  value,
  id,
  label,
  description,
  className,
  ariaLabel,
}: RadioProps) {
  const ctx = useContext(RadioGroupContext)
  if (!ctx) {
    throw new Error('Radio must be used within a RadioGroup')
  }

  const fallbackId = useId()
  const inputId = id ?? fallbackId
  const isChecked = ctx.value === value

  function handleSelect() {
    if (ctx?.disabled) return
    ctx?.onChange(value)
  }

  const control = (
    <button
      id={inputId}
      type="button"
      role="radio"
      aria-checked={isChecked}
      aria-label={ariaLabel ?? label ?? 'Radio'}
      disabled={ctx.disabled}
      onClick={handleSelect}
      className={cn(
        'inline-flex size-4 shrink-0 items-center justify-center rounded-full border border-[hsl(var(--glass-border-strong))] bg-[hsl(var(--surface-field))] text-transparent transition-[background-color,border-color,color,box-shadow] duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] disabled:pointer-events-none disabled:opacity-50',
        isChecked && 'border-primary bg-primary text-primary-foreground',
        className,
      )}
    >
      <Circle className="size-2 fill-current" />
    </button>
  )

  if (!label && !description) return control

  return (
    <label
      htmlFor={inputId}
      className={cn(
        'flex cursor-pointer items-center gap-3 rounded-lg text-sm text-foreground',
        ctx.disabled && 'cursor-not-allowed opacity-50',
      )}
    >
      <span>{control}</span>
      <span className="space-y-0.5">
        {label ? <span className="block font-medium">{label}</span> : null}
        {description ? <span className="text-muted-soft block">{description}</span> : null}
      </span>
    </label>
  )
}
