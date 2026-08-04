import * as React from 'react'
import * as SwitchPrimitives from '@radix-ui/react-switch'
import { cn } from '@/lib/utils'

type SwitchProps = Omit<
  React.ComponentPropsWithoutRef<typeof SwitchPrimitives.Root>,
  'checked' | 'defaultChecked' | 'onCheckedChange'
> & {
  checked?: boolean
  defaultChecked?: boolean
  disabled?: boolean
  id?: string
  label?: string
  description?: string
  onCheckedChange?: (checked: boolean) => void
}

const SwitchControl = React.forwardRef<
  React.ElementRef<typeof SwitchPrimitives.Root>,
  React.ComponentPropsWithoutRef<typeof SwitchPrimitives.Root>
>(({ className, ...props }, ref) => (
  <SwitchPrimitives.Root
    className={cn(
      'peer inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full border bg-[hsl(var(--surface-subtle))] transition-[background-color,border-color,box-shadow] duration-200 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-strong))] data-[state=checked]:bg-primary data-[state=checked]:[box-shadow:var(--switch-checked-shadow)] disabled:cursor-not-allowed disabled:opacity-50',
      className,
    )}
    style={{ borderColor: 'hsl(var(--glass-border))' }}
    {...props}
    ref={ref}
  >
    <SwitchPrimitives.Thumb className="pointer-events-none block size-5 rounded-full bg-card shadow-lg ring-0 transition-[transform,background-color,box-shadow] duration-200 ease-out data-[state=checked]:translate-x-5 data-[state=checked]:bg-primary-foreground data-[state=unchecked]:translate-x-0" />
  </SwitchPrimitives.Root>
))
SwitchControl.displayName = SwitchPrimitives.Root.displayName

const Switch = React.forwardRef<React.ElementRef<typeof SwitchPrimitives.Root>, SwitchProps>(
  ({ className, label, description, disabled, ...props }, ref) => {
    const control = <SwitchControl ref={ref} disabled={disabled} className={className} {...props} />

    if (!label && !description) return control

    return (
      <label
        className={cn(
          'section-soft flex cursor-pointer items-center justify-between gap-4 rounded-lg px-4 py-3',
          disabled && 'cursor-not-allowed opacity-50',
        )}
      >
        <span className="space-y-0.5">
          {label ? <span className="block font-medium text-foreground">{label}</span> : null}
          {description ? <span className="text-muted-soft block text-sm">{description}</span> : null}
        </span>
        {control}
      </label>
    )
  },
)
Switch.displayName = 'Switch'

export { Switch }
