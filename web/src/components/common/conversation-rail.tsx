import type { ReactNode } from 'react'
import { SquarePen, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ConfirmPopover } from '@/components/common/confirm-popover'

export type ConversationRailSession = {
  id: string
  title: string
  statusLabel?: string
}

export type ConversationRailLabels = {
  new: string
  recent: string
  empty: string
  untitled: string
  delete: string
  deleteConfirm: string
}

type ConversationRailProps = {
  sessions: ConversationRailSession[]
  activeId: string | null
  labels: ConversationRailLabels
  leading?: ReactNode
  onSelect: (id: string) => void
  onNew: () => void
  onDelete: (id: string) => void
}

export function ConversationRail({
  sessions,
  activeId,
  labels,
  leading,
  onSelect,
  onNew,
  onDelete,
}: ConversationRailProps) {
  return (
    <div className="flex h-full flex-col gap-1 p-2">
      <div className="flex items-center gap-2">
        {leading}
        <button
          type="button"
          onClick={onNew}
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
          aria-label={labels.new}
          title={labels.new}
        >
          <SquarePen className="h-4 w-4" />
        </button>
      </div>

      <div className="mt-2 min-h-0 flex-1 overflow-y-auto">
        {sessions.length > 0 ? (
          <p className="px-3 py-2 text-xs font-medium text-faint">{labels.recent}</p>
        ) : null}
        <div className="space-y-0.5">
          {sessions.length === 0 ? (
            <p className="px-3 py-6 text-center text-xs text-muted-soft">{labels.empty}</p>
          ) : (
            sessions.map((session) => (
              <div
                key={session.id}
                className={cn(
                  'group flex items-center gap-1 rounded-lg px-3 py-2 text-sm transition-colors',
                  session.id === activeId
                    ? 'bg-[hsl(var(--surface-selected))] text-foreground'
                    : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
                )}
              >
                <button
                  type="button"
                  className="min-w-0 flex-1 text-left"
                  onClick={() => onSelect(session.id)}
                >
                  <span className="block truncate">{session.title || labels.untitled}</span>
                  {session.statusLabel ? (
                    <span className="mt-0.5 block text-[11px] text-primary">{session.statusLabel}</span>
                  ) : null}
                </button>
                <ConfirmPopover
                  message={labels.deleteConfirm}
                  confirmLabel={labels.delete}
                  destructive
                  side="right"
                  align="center"
                  onConfirm={() => onDelete(session.id)}
                >
                  <button
                    type="button"
                    className="shrink-0 rounded p-1 text-muted-soft opacity-0 transition-opacity hover:text-red-500 group-hover:opacity-100 data-[state=open]:opacity-100"
                    aria-label={labels.delete}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </ConfirmPopover>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
