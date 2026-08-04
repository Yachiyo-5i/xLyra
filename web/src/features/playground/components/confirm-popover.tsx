import { useState, type ReactNode } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

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
        <PopoverPrimitive.Content
          side={side}
          align={align}
          sideOffset={6}
          collisionPadding={12}
          onClick={(event) => event.stopPropagation()}
          className="glass-panel-strong z-[130] max-w-[14rem] rounded-lg p-2 shadow-[var(--shadow-dialog)]"
        >
          <PopoverPrimitive.Arrow width={9} height={4} className="fill-[hsl(var(--surface-strong))]" />
          <p className="px-1 text-base leading-6 text-foreground">{message}</p>
          <div className="mt-2 flex items-center justify-end gap-1">
            <button
              type="button"
              onClick={() => setOpen(false)}
              className="min-h-8 rounded-md px-2 py-1 text-sm leading-5 text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
            >
              {cancelLabel ?? t('actions.cancel')}
            </button>
            <button
              type="button"
              onClick={() => {
                setOpen(false)
                onConfirm()
              }}
              className={cn(
                'min-h-8 rounded-md px-2 py-1 text-sm leading-5 transition-colors',
                destructive
                  ? 'text-red-500 hover:bg-red-500/10'
                  : 'text-primary hover:bg-[hsl(var(--primary)/0.1)]',
              )}
            >
              {confirmLabel ?? t('actions.confirm')}
            </button>
          </div>
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
