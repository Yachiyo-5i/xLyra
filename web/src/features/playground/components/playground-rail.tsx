import { useTranslation } from 'react-i18next'
import { ConversationRail } from '@/components/common/conversation-rail'
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
    <ConversationRail
      sessions={sessions}
      activeId={activeId}
      leading={<PlaygroundModeSwitcher value={mode} onChange={onModeChange} className="min-w-0 flex-1" />}
      labels={{
        new: t('conversations.new'),
        recent: t('conversations.recent'),
        empty: t('conversations.empty'),
        untitled: t('conversations.untitled'),
        delete: t('conversations.delete'),
        deleteConfirm: t('confirm.deleteConversation'),
      }}
      onSelect={onSelect}
      onNew={onNew}
      onDelete={onDelete}
    />
  )
}
