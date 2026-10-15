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

/**
 * 开关：关为填充色、开为系统绿。尺寸取 --switch-w / --switch-h，移动端为 iOS 标准 51×31。
 * 按住时滑块向内拉长并变为半透明玻璃（HIG：滑块在交互时呈现 Liquid Glass 外观），松开恢复。
 */
const SwitchControl = React.forwardRef<
  React.ElementRef<typeof SwitchPrimitives.Root>,
  React.ComponentPropsWithoutRef<typeof SwitchPrimitives.Root>
>(({ className, ...props }, ref) => (
  <SwitchPrimitives.Root
    className={cn(
      'group peer relative inline-flex h-[var(--switch-h)] w-[var(--switch-w)] shrink-0 cursor-pointer items-center rounded-full bg-fill-1 p-0.5 transition-colors duration-200 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tint/55 focus-visible:ring-offset-2 focus-visible:ring-offset-canvas disabled:cursor-not-allowed disabled:opacity-40 data-[state=checked]:bg-success',
      className,
    )}
    {...props}
    ref={ref}
  >
    <SwitchPrimitives.Thumb
      className={cn(
        'pointer-events-none block h-[calc(var(--switch-h)-4px)] w-[calc(var(--switch-h)-4px)] rounded-full bg-white shadow-[0_2px_6px_rgb(0_0_0_/_0.28),0_0_0_0.5px_rgb(0_0_0_/_0.06)] transition-[transform,width,background-color,box-shadow] duration-200 ease-[cubic-bezier(0.34,1.3,0.64,1)]',
        'data-[state=checked]:translate-x-[calc(var(--switch-w)-var(--switch-h))]',
        // 按住：滑块拉长 6px（开启状态向左拉长），并变成半透明玻璃
        'group-active:w-[calc(var(--switch-h)+2px)] group-active:bg-white/75 group-active:shadow-[inset_0_0_0_0.5px_rgb(255_255_255_/_0.7),inset_0_1px_0_rgb(255_255_255_/_0.9),0_3px_10px_rgb(0_0_0_/_0.3)]',
        'group-active:data-[state=checked]:translate-x-[calc(var(--switch-w)-var(--switch-h)-6px)]',
      )}
    />
  </SwitchPrimitives.Root>
))
SwitchControl.displayName = SwitchPrimitives.Root.displayName

const Switch = React.forwardRef<React.ElementRef<typeof SwitchPrimitives.Root>, SwitchProps>(
  ({ className, label, description, disabled, ...props }, ref) => {
    const control = <SwitchControl ref={ref} disabled={disabled} className={className} {...props} />

    if (!label && !description) return control

    // 带文字的开关是一整行：点击整行都能切换。用填充色分组，不再嵌套材质
    return (
      <label
        className={cn(
          'flex cursor-pointer items-center justify-between gap-4 rounded-inner bg-fill-4 px-4 py-3',
          disabled && 'cursor-not-allowed opacity-40',
        )}
      >
        <span className="min-w-0 space-y-0.5">
          {label ? <span className="block text-body text-label">{label}</span> : null}
          {description ? <span className="block text-footnote text-label-2">{description}</span> : null}
        </span>
        {control}
      </label>
    )
  },
)
Switch.displayName = 'Switch'

export { Switch }
