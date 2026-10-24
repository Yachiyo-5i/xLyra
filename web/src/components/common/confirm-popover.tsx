import { useState, type ReactNode } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { useTranslation } from 'react-i18next'
import { LiquidGlass } from '@/components/glass/liquid-glass'
import { Button } from '@/components/ui/button'

type ConfirmPopoverProps = {
  message: string
  onConfirm: () => void
  children: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  destructive?: boolean
  side?: 'top' | 'bottom' | 'left' | 'right'
  align?: 'start' | 'center' | 'end'
}

export function ConfirmPopover({
  message,
  onConfirm,
  children,
  confirmLabel,
  cancelLabel,
  destructive = false,
  side = 'top',
  align = 'center',
}: ConfirmPopoverProps) {
  const { t } = useTranslation('playground')
  const [open, setOpen] = useState(false)

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>{children}</PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        {/* 功能层：Liquid Glass 小面板，从触发点放大展开 */}
        <PopoverPrimitive.Content
          asChild
          side={side}
          align={align}
          sideOffset={8}
          collisionPadding={12}
          onClick={(event) => event.stopPropagation()}
        >
          <LiquidGlass className="z-[130] w-max max-w-[16rem] origin-(--radix-popover-content-transform-origin) rounded-card p-1.5 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-90">
            <p className="px-2.5 pb-2 pt-1.5 text-body text-label">{message}</p>
            <div className="flex items-center justify-end gap-1">
              <Button variant="ghost" size="sm" onClick={() => setOpen(false)}>
                {cancelLabel ?? t('actions.cancel')}
              </Button>
              <Button
                variant={destructive ? 'destructive' : 'default'}
                size="sm"
                onClick={() => {
                  setOpen(false)
                  onConfirm()
                }}
              >
                {confirmLabel ?? t('actions.confirm')}
              </Button>
            </div>
          </LiquidGlass>
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
