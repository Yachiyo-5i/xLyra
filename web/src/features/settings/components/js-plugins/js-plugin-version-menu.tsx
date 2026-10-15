import { useState } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { EllipsisVertical, LoaderCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'

export type VersionMenuItem = {
  key: string
  label: string
  onSelect: () => void
  disabled?: boolean
  destructive?: boolean
}

/** The actions of one plugin version behind a vertical-dots button, so the column stays one button wide. */
export function JSPluginVersionMenu({
  items,
  pending,
  label,
}: {
  items: VersionMenuItem[]
  /** Something started from the menu is still running; shown on the button while the menu is closed. */
  pending?: boolean
  /** Accessible name of the button. */
  label: string
}) {
  const [open, setOpen] = useState(false)

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        <Button size="icon" variant="ghost" className="h-8 w-8 text-foreground/60 hover:text-foreground" aria-label={label}>
          {pending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <EllipsisVertical className="h-4 w-4" />}
        </Button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="end"
          sideOffset={6}
          collisionPadding={16}
          onOpenAutoFocus={(event) => event.preventDefault()}
          className="z-[120] min-w-32 overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--dialog-surface))] py-1 shadow-[var(--shadow-dialog)] backdrop-blur-xl"
        >
          {items.map((item) => (
            <button
              key={item.key}
              type="button"
              disabled={item.disabled}
              onClick={() => {
                setOpen(false)
                item.onSelect()
              }}
              className={`flex w-full items-center px-3 py-2 text-left text-sm font-medium transition-colors hover:bg-[hsl(var(--surface-subtle))] disabled:cursor-not-allowed disabled:opacity-40 ${
                item.destructive ? 'text-[hsl(var(--destructive))]' : 'text-foreground'
              }`}
            >
              {item.label}
            </button>
          ))}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
