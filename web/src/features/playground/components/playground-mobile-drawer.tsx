import { useTranslation } from 'react-i18next'
import { SquarePen, Trash2 } from 'lucide-react'
import { ConfirmPopover } from '@/components/common/confirm-popover'
import { Select, SelectContent, SelectItem, SelectTrigger } from '@/components/ui/select'
import { Sheet, SheetContent, SheetDescription, SheetTitle } from '@/components/ui/sheet'
import { PlaygroundModeSwitcher } from '@/features/playground/components/playground-mode-switcher'
import type { PlaygroundMode } from '@/features/playground/lib/types'
import { cn } from '@/lib/utils'

type DrawerSession = { id: string; title: string; updatedAt: number }

type PlaygroundMobileDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  mode: PlaygroundMode
  onModeChange: (mode: PlaygroundMode) => void
  apiKeys: Array<{ id: string; name: string }>
  apiKeyId: string | null
  onAPIKeyChange: (id: string) => void
  sessions: DrawerSession[]
  activeId: string | null
  onSelect: (id: string) => void
  onNew: () => void
  onDelete: (id: string) => void
}

/**
 * 移动端模型体验的左侧抽屉：顶部「对话 / 图片」切换，下面一行是 API Key 选择与新建对话，
 * 再下面是会话列表（每行右侧一个删除按钮）。新建或点开某个会话后抽屉自动收起。
 * 原来对话区上方的所有操作控件都集中到这里，对话区只保留消息与输入框。
 */
export function PlaygroundMobileDrawer({
  open,
  onOpenChange,
  mode,
  onModeChange,
  apiKeys,
  apiKeyId,
  onAPIKeyChange,
  sessions,
  activeId,
  onSelect,
  onNew,
  onDelete,
}: PlaygroundMobileDrawerProps) {
  const { t } = useTranslation('playground')
  const activeKey = apiKeys.find((key) => key.id === apiKeyId)

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="left" className="w-[min(calc(100vw-48px),340px)] pt-[env(safe-area-inset-top,0px)]">
        <SheetTitle className="sr-only">{t('mobile.menu')}</SheetTitle>
        <SheetDescription className="sr-only">{t('mobile.menu')}</SheetDescription>

        <div className="space-y-3 border-b border-separator p-4">
          <PlaygroundModeSwitcher value={mode} onChange={onModeChange} />
          <div className="flex items-center gap-2">
            {/* 与桌面工具行一致：虚线胶囊的筛选器样式 */}
            <div className="min-w-0 flex-1">
              <Select value={apiKeyId ?? undefined} onValueChange={onAPIKeyChange} disabled={apiKeys.length === 0}>
                <SelectTrigger
                  variant="filter"
                  filterLabel={t('credential.keyLabel')}
                  active={Boolean(activeKey)}
                  className="h-11 max-w-full px-4"
                >
                  <span className="truncate">{activeKey?.name ?? t('credential.keyPlaceholder')}</span>
                </SelectTrigger>
                <SelectContent searchable={false} widthMode="content">
                  {apiKeys.map((key) => (
                    <SelectItem key={key.id} value={key.id}>
                      {key.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <button
              type="button"
              onClick={() => {
                onNew()
                onOpenChange(false)
              }}
              aria-label={t('mobile.newChat')}
              title={t('mobile.newChat')}
              className="flex size-11 shrink-0 items-center justify-center rounded-full bg-fill-3 text-label transition-colors active:bg-fill-2"
            >
              <SquarePen className="size-[18px]" />
            </button>
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-2 pb-[calc(env(safe-area-inset-bottom,0px)+8px)]">
          {sessions.length > 0 ? <p className="px-3 pb-1 pt-2 text-caption-1 text-label-3">{t('conversations.recent')}</p> : null}
          {sessions.length === 0 ? <p className="px-3 py-8 text-center text-footnote text-label-3">{t('conversations.empty')}</p> : null}
          <div className="space-y-0.5">
            {sessions.map((session) => (
              <div
                key={session.id}
                className={cn(
                  'flex min-h-11 items-center gap-1 rounded-full pl-4 pr-1 transition-colors',
                  session.id === activeId ? 'nav-item-selected text-label' : 'text-label-2 active:bg-fill-4',
                )}
              >
                <button
                  type="button"
                  className="min-w-0 flex-1 truncate py-2.5 text-left text-body"
                  onClick={() => {
                    onSelect(session.id)
                    onOpenChange(false)
                  }}
                >
                  {session.title || t('conversations.untitled')}
                </button>
                <ConfirmPopover
                  message={t('confirm.deleteConversation')}
                  confirmLabel={t('conversations.delete')}
                  destructive
                  side="bottom"
                  align="end"
                  onConfirm={() => onDelete(session.id)}
                >
                  <button
                    type="button"
                    aria-label={t('mobile.delete')}
                    className="flex size-9 shrink-0 items-center justify-center rounded-full text-label-3 transition-colors active:bg-fill-3 active:text-danger"
                  >
                    <Trash2 className="size-4" />
                  </button>
                </ConfirmPopover>
              </div>
            ))}
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}
