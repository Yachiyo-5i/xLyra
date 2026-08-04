import { useState } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { History, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { ConfirmPopover } from '@/features/playground/components/confirm-popover'

type ConversationOption = {
  id: string
  title: string
}

type MobileConversationPickerProps = {
  conversations: ConversationOption[]
  value: string | null
  onChange: (id: string) => void
  onDelete: (id: string) => void
}

export function MobileConversationPicker({
  conversations,
  value,
  onChange,
  onDelete,
}: MobileConversationPickerProps) {
  const { t } = useTranslation('playground')
  const [open, setOpen] = useState(false)
  const active = conversations.find((conversation) => conversation.id === value)
  const activeTitle = active?.title || t('conversations.untitled')

  return (
    <>
      <p className="min-w-0 flex-1 truncate px-1 text-sm font-medium text-foreground">
        {activeTitle}
      </p>
      <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
        <PopoverPrimitive.Trigger asChild>
          <button
            type="button"
            className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg text-muted-soft outline-none transition-colors active:bg-[hsl(var(--surface-subtle))] active:text-foreground focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))]"
            aria-label={t('conversations.history')}
            title={t('conversations.history')}
          >
            <History className="h-4 w-4" />
          </button>
        </PopoverPrimitive.Trigger>
        <PopoverPrimitive.Portal>
          <PopoverPrimitive.Content
            side="bottom"
            align="end"
            sideOffset={6}
            collisionPadding={8}
            className="glass-panel-strong z-[130] max-h-[min(60vh,26rem)] w-[calc(100vw-1rem)] max-w-md overflow-y-auto rounded-xl p-1.5 shadow-[var(--shadow-dialog)]"
          >
            {conversations.length === 0 ? (
              <p className="px-3 py-6 text-center text-xs text-muted-soft">
                {t('conversations.empty')}
              </p>
            ) : (
              conversations.map((conversation) => {
                const selected = conversation.id === value
                return (
                  <div
                    key={conversation.id}
                    className={cn(
                      'flex min-h-10 items-center gap-1 rounded-lg px-2 transition-colors',
                      selected
                        ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                        : 'text-muted-soft active:bg-[hsl(var(--surface-subtle))] active:text-foreground',
                    )}
                  >
                    <button
                      type="button"
                      className="min-w-0 flex-1 truncate px-1 py-2 text-left text-sm"
                      aria-current={selected ? 'true' : undefined}
                      onClick={() => {
                        onChange(conversation.id)
                        setOpen(false)
                      }}
                    >
                      {conversation.title || t('conversations.untitled')}
                    </button>
                    <ConfirmPopover
                      message={t('confirm.deleteConversation')}
                      confirmLabel={t('actions.delete')}
                      destructive
                      side="left"
                      align="center"
                      onConfirm={() => onDelete(conversation.id)}
                    >
                      <button
                        type="button"
                        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted-soft transition-colors active:bg-red-500/10 active:text-red-500"
                        aria-label={t('conversations.delete')}
                        title={t('conversations.delete')}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </ConfirmPopover>
                  </div>
                )
              })
            )}
          </PopoverPrimitive.Content>
        </PopoverPrimitive.Portal>
      </PopoverPrimitive.Root>
    </>
  )
}
