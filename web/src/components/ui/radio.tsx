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
        // 未选中：描边圆；选中：强调色实底 + 深色圆点
        'inline-flex size-[var(--check-size)] shrink-0 items-center justify-center rounded-full border-[1.5px] border-white/30 bg-fill-4 text-transparent transition-[background-color,border-color,color,transform] duration-150 active:scale-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tint/55 focus-visible:ring-offset-2 focus-visible:ring-offset-canvas disabled:pointer-events-none disabled:opacity-40',
        isChecked && 'border-tint bg-tint text-tint-foreground',
        className,
      )}
    >
      <Circle className="size-[calc(var(--check-size)*0.4)] fill-current" strokeWidth={0} />
    </button>
  )

  if (!label && !description) return control

  return (
    <label
      htmlFor={inputId}
      className={cn(
        'flex cursor-pointer items-start gap-3 text-body text-label',
        ctx.disabled && 'cursor-not-allowed opacity-40',
      )}
    >
      <span className="flex h-[1lh] items-center">{control}</span>
      <span className="min-w-0 space-y-0.5">
        {label ? <span className="block">{label}</span> : null}
        {description ? <span className="block text-footnote text-label-2">{description}</span> : null}
      </span>
    </label>
  )
}
