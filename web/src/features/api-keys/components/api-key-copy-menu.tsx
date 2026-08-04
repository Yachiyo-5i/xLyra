import { useState } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { useTranslation } from 'react-i18next'
import { ChevronDown, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import {
  apiKeyCopyVariants,
  copyAPIKeyVariantToClipboard,
  maskedAPIKeyForDisplay,
} from '@/features/api-keys/lib/api-key-utils'

export function APIKeyCopyMenu({ apiKey, triggerClassName }: { apiKey: DownstreamAPIKey; triggerClassName?: string }) {
  const { t } = useTranslation('api-keys')
  const [open, setOpen] = useState(false)
  const variants = apiKeyCopyVariants(apiKey, t)
  const display = maskedAPIKeyForDisplay(apiKey)

  if (variants.length <= 1) {
    const variant = variants[0]
    return (
      <button
        type="button"
        className={cn('inline-flex max-w-full items-center gap-2 text-muted-soft transition-colors hover:text-foreground', triggerClassName)}
        onClick={() => {
          if (variant) {
            void copyAPIKeyVariantToClipboard(apiKey, variant.id, t)
          }
        }}
        title={display}
        disabled={!variant}
      >
        <span className="min-w-0 truncate font-mono text-xs">{display}</span>
        <Copy className="h-3.5 w-3.5 shrink-0" />
      </button>
    )
  }

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        <button
          type="button"
          className={cn('inline-flex max-w-full items-center gap-1.5 text-muted-soft transition-colors hover:text-foreground', triggerClassName)}
          title={display}
          disabled={variants.length === 0}
        >
          <span className="min-w-0 truncate font-mono text-xs">{display}</span>
          <ChevronDown className="h-3 w-3 shrink-0" />
        </button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="start"
          sideOffset={6}
          className="glass-panel-strong z-[130] min-w-36 rounded-lg p-1 shadow-[var(--shadow-dialog)]"
        >
          {variants.map((variant) => (
            <button
              key={variant.id}
              type="button"
              className="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-xs leading-5 text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
              onClick={() => {
                void copyAPIKeyVariantToClipboard(apiKey, variant.id, t)
                setOpen(false)
              }}
            >
              <Copy className="h-3 w-3 shrink-0" />
              <span className="min-w-0 truncate">{variant.label}</span>
            </button>
          ))}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
