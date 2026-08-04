import { useTranslation } from 'react-i18next'
import { SquarePen, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ConfirmPopover } from '@/features/playground/components/confirm-popover'
import { PlaygroundModeSwitcher } from '@/features/playground/components/playground-mode-switcher'
import type { PlaygroundMode } from '@/features/playground/lib/types'

type RailSession = {
  id: string
  title: string
}

type PlaygroundRailProps = {
  sessions: RailSession[]
  activeId: string | null
  mode: PlaygroundMode
  onModeChange: (mode: PlaygroundMode) => void
  onSelect: (id: string) => void
  onNew: () => void
  onDelete: (id: string) => void
}

export function PlaygroundRail({
  sessions,
  activeId,
  mode,
  onModeChange,
  onSelect,
  onNew,
  onDelete,
}: PlaygroundRailProps) {
  const { t } = useTranslation('playground')

  return (
    <div className="flex h-full flex-col gap-1 p-2">
      <div className="flex items-center gap-2">
        <PlaygroundModeSwitcher value={mode} onChange={onModeChange} className="min-w-0 flex-1" />
        <button
          type="button"
          onClick={onNew}
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
          aria-label={t('conversations.new')}
          title={t('conversations.new')}
        >
          <SquarePen className="h-4 w-4" />
        </button>
      </div>

      <div className="mt-2 min-h-0 flex-1 overflow-y-auto">
        {sessions.length > 0 ? (
          <p className="px-3 py-2 text-xs font-medium text-faint">{t('conversations.recent')}</p>
        ) : null}
        <div className="space-y-0.5">
          {sessions.length === 0 ? (
            <p className="px-3 py-6 text-center text-xs text-muted-soft">{t('conversations.empty')}</p>
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
                  className="min-w-0 flex-1 truncate text-left"
                  onClick={() => onSelect(session.id)}
                >
                  {session.title || t('conversations.untitled')}
                </button>
                <ConfirmPopover
                  message={t('confirm.deleteConversation')}
                  confirmLabel={t('actions.delete')}
                  destructive
                  side="right"
                  align="center"
                  onConfirm={() => onDelete(session.id)}
                >
                  <button
                    type="button"
                    className="shrink-0 rounded p-1 text-muted-soft opacity-0 transition-opacity hover:text-red-500 group-hover:opacity-100 data-[state=open]:opacity-100"
                    aria-label={t('conversations.delete')}
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
