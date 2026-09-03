import { useState } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { useTranslation } from 'react-i18next'
import { Ellipsis, MoveLeft, Plus, Settings2, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { formatRelativeTime } from '@/features/agent/lib/relative-time'
import type { AgentSession } from '@/features/agent/api/agent'

type AgentSidebarProps = {
  className?: string
  sessions: AgentSession[]
  activeId: string | null
  /** 弹层表面渲染器（液态玻璃面板），与模型选择器同一模式，由 workspace 提供 */
  menuRenderer: (children: React.ReactNode) => React.ReactNode
  onBack: () => void
  onSelect: (id: string) => void
  onNew: () => void
  onDelete: (id: string) => void
  onOpenSettings: () => void
}

export function AgentSidebar({ className, sessions, activeId, menuRenderer, onBack, onSelect, onNew, onDelete, onOpenSettings }: AgentSidebarProps) {
  const { t, i18n } = useTranslation('agent')
  const [pendingDelete, setPendingDelete] = useState<string | null>(null)

  return (
    <div className={cn('flex h-full min-h-0 flex-col p-3', className)}>
      <button
        type="button"
        onClick={onBack}
        className="flex h-10 shrink-0 items-center gap-2 rounded-xl px-3 text-sm text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
      >
        <MoveLeft className="h-4 w-4 shrink-0" />
        {t('header.back')}
      </button>

      <button
        type="button"
        onClick={onNew}
        className="mt-1 flex h-10 shrink-0 items-center gap-2 rounded-xl px-3 text-sm font-medium text-foreground transition-colors hover:bg-[hsl(var(--surface-subtle))]"
      >
        <Plus className="h-4 w-4 shrink-0" />
        {t('sidebar.new')}
      </button>

      <p className="mt-4 px-3 pb-2 text-xs font-medium text-faint">{t('sidebar.recent')}</p>

      <div className="min-h-0 flex-1 space-y-0.5 overflow-y-auto">
        {sessions.length === 0 ? (
          <p className="px-3 py-6 text-center text-xs text-muted-soft">{t('sidebar.empty')}</p>
        ) : (
          sessions.map((session) => (
            <SessionRow
              key={session.session_id}
              session={session}
              active={session.session_id === activeId}
              menuRenderer={menuRenderer}
              locale={i18n.language}
              untitledLabel={t('sidebar.untitled')}
              menuLabel={t('sidebar.menu')}
              deleteLabel={t('sidebar.delete')}
              onSelect={onSelect}
              onDelete={setPendingDelete}
            />
          ))
        )}
      </div>

      <button
        type="button"
        onClick={onOpenSettings}
        className="mt-2 flex h-10 shrink-0 items-center gap-2 rounded-xl px-3 text-sm text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
      >
        <Settings2 className="h-4 w-4 shrink-0" />
        {t('settings.entry')}
      </button>

      <ConfirmDialog
        open={pendingDelete !== null}
        title={t('sidebar.delete')}
        description={t('sidebar.deleteConfirm')}
        confirmLabel={t('sidebar.delete')}
        destructive
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => {
          if (pendingDelete) onDelete(pendingDelete)
          setPendingDelete(null)
        }}
      />
    </div>
  )
}

function SessionRow({
  session,
  active,
  menuRenderer,
  locale,
  untitledLabel,
  menuLabel,
  deleteLabel,
  onSelect,
  onDelete,
}: {
  session: AgentSession
  active: boolean
  menuRenderer: (children: React.ReactNode) => React.ReactNode
  locale: string
  untitledLabel: string
  menuLabel: string
  deleteLabel: string
  onSelect: (id: string) => void
  onDelete: (id: string) => void
}) {
  const [menuOpen, setMenuOpen] = useState(false)

  return (
    <div
      data-agent-session-row="true"
      data-active={active}
      className={cn(
        'group flex items-center gap-1 rounded-xl px-3 py-2 text-sm transition-colors',
        active
          ? 'bg-[hsl(var(--surface-selected))] text-foreground'
          : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
      )}
    >
      <button
        type="button"
        className="min-w-0 flex-1 text-left"
        onClick={() => onSelect(session.session_id)}
      >
        <span className="block truncate">{session.title || session.preview || untitledLabel}</span>
      </button>
      <span className="shrink-0 text-[11px] text-faint">{formatRelativeTime(session.updated_at, locale)}</span>
      <PopoverPrimitive.Root open={menuOpen} onOpenChange={setMenuOpen}>
        <PopoverPrimitive.Trigger asChild>
          <button
            type="button"
            className={cn(
              'shrink-0 rounded p-1 text-muted-soft transition-opacity hover:text-foreground',
              menuOpen || active ? 'opacity-100' : 'opacity-0 group-hover:opacity-100',
            )}
            aria-label={menuLabel}
          >
            <Ellipsis className="h-4 w-4" />
          </button>
        </PopoverPrimitive.Trigger>
        <PopoverPrimitive.Portal>
          <PopoverPrimitive.Content
            align="end"
            sideOffset={6}
            onClick={(event) => event.stopPropagation()}
            className="agent-liquid-picker-host z-[120]"
          >
            {menuRenderer(
              <button
                type="button"
                data-agent-delete="true"
                onClick={() => {
                  setMenuOpen(false)
                  onDelete(session.session_id)
                }}
                className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm font-medium transition-colors"
              >
                <Trash2 className="h-4 w-4 text-current" />
                {deleteLabel}
              </button>
            )}
          </PopoverPrimitive.Content>
        </PopoverPrimitive.Portal>
      </PopoverPrimitive.Root>
    </div>
  )
}
