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
        // 未选中：描边方框；选中：强调色实底 + 深色勾
        'inline-flex size-[var(--check-size)] shrink-0 items-center justify-center rounded-[6px] border-[1.5px] border-white/30 bg-fill-4 text-transparent transition-[background-color,border-color,color,transform] duration-150 active:scale-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tint/55 focus-visible:ring-offset-2 focus-visible:ring-offset-canvas disabled:pointer-events-none disabled:opacity-40',
        isChecked && 'border-tint bg-tint text-tint-foreground',
        className,
      )}
    >
      <Check className="size-[calc(var(--check-size)*0.72)]" strokeWidth={3} />
    </button>
  )

  if (!label && !description) return control

  return (
    <label
      htmlFor={inputId}
      className={cn(
        'flex cursor-pointer items-start gap-3 text-body text-label',
        disabled && 'cursor-not-allowed opacity-40',
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
