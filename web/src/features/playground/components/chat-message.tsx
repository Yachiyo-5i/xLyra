import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronRight, Copy, Loader2, Pencil } from 'lucide-react'
import { cn } from '@/lib/utils'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { TextArea } from '@/components/ui/textarea'
import { Button } from '@/components/ui/button'
import { MarkdownMessage } from '@/features/playground/components/markdown-message'
import {
  PlaygroundMessageAction,
  PlaygroundRegenerateAction,
} from '@/features/playground/components/playground-message-actions'
import { PlaygroundModelIcon } from '@/features/playground/components/playground-model-icon'
import { ChatAttachmentItem } from '@/features/playground/components/chat-attachment'
import { CollapsibleUserMessage } from '@/features/playground/components/collapsible-user-message'
import { formatResponseDuration } from '@/features/playground/lib/response-timing'
import type { ChatMessage, GatewayModel } from '@/features/playground/lib/types'

type ChatMessageItemProps = {
  message: ChatMessage
  streaming?: boolean
  streamingElapsedMs?: number | null
  busy?: boolean
  canEdit?: boolean
  isLast?: boolean
  gatewayModel?: GatewayModel
  onRegenerate: (messageId: string) => void
  onEditSubmit: (messageId: string, text: string) => void
}

export function ChatMessageItem({
  message,
  streaming = false,
  streamingElapsedMs = null,
  busy = false,
  canEdit = false,
  isLast = false,
  gatewayModel,
  onRegenerate,
  onEditSubmit,
}: ChatMessageItemProps) {
  const { t } = useTranslation('playground')
  const [reasoningManualOpen, setReasoningManualOpen] = useState<boolean | null>(null)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')

  const isThinking = streaming && !message.content
  const reasoningOpen = reasoningManualOpen ?? isThinking
  const responseDuration = message.responseDurationMs === undefined
    ? null
    : formatResponseDuration(message.responseDurationMs)
  const liveDuration = streamingElapsedMs === null ? null : formatResponseDuration(streamingElapsedMs)
  const hasSource = Boolean(message.model || message.siteName)
  const hasUsage = Boolean(message.usage?.total_tokens)

  const startEdit = () => {
    setDraft(message.content)
    setEditing(true)
  }
  const submitEdit = () => {
    const text = draft.trim()
    if (!text && !message.attachments?.length) return
    setEditing(false)
    onEditSubmit(message.id, text)
  }

  const copy = () => void copyToClipboard(message.content, t('actions.copied'), t('actions.copyFailed'))

  if (message.role === 'user') {
    if (editing) {
      return (
        <div className="flex justify-end">
          <div className="w-full max-w-[80%] space-y-2">
            {message.attachments?.length ? (
              <div className="flex flex-wrap justify-end gap-2">
                {message.attachments.map((attachment) => (
                  <ChatAttachmentItem key={attachment.id} attachment={attachment} />
                ))}
              </div>
            ) : null}
            <TextArea value={draft} onChange={(event) => setDraft(event.target.value)} className="min-h-[72px]" />
            <div className="flex justify-end gap-2">
              <Button type="button" variant="ghost" size="sm" onClick={() => setEditing(false)}>
                {t('actions.cancel')}
              </Button>
              <Button type="button" size="sm" onClick={submitEdit} disabled={!draft.trim() && !message.attachments?.length}>
                {t('actions.send')}
              </Button>
            </div>
          </div>
        </div>
      )
    }
    return (
      <div className="group flex flex-col items-end gap-1">
        {message.attachments?.length ? (
          <div className="flex max-w-[80%] flex-wrap justify-end gap-2">
            {message.attachments.map((attachment) => (
              <ChatAttachmentItem key={attachment.id} attachment={attachment} />
            ))}
          </div>
        ) : null}
        {message.content ? (
          <CollapsibleUserMessage content={message.content} />
        ) : null}
        <div className="flex items-center gap-0.5 transition-opacity md:opacity-0 md:group-hover:opacity-100">
          {message.content ? (
            <PlaygroundMessageAction label={t('actions.copy')} onClick={copy}>
              <Copy className="h-3.5 w-3.5" />
            </PlaygroundMessageAction>
          ) : null}
          {canEdit ? (
            <PlaygroundMessageAction label={t('actions.edit')} onClick={startEdit} disabled={busy}>
              <Pencil className="h-3.5 w-3.5" />
            </PlaygroundMessageAction>
          ) : null}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-2">
      {message.reasoning ? (
        <div className="rounded-xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))]/60">
          <button
            type="button"
            className="flex w-full items-center gap-1.5 px-3 py-2 text-xs font-medium text-muted-soft"
            onClick={() => setReasoningManualOpen(!reasoningOpen)}
          >
            {isThinking ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <ChevronRight className={cn('h-3.5 w-3.5 transition-transform', reasoningOpen && 'rotate-90')} />
            )}
            {isThinking ? t('chat.thinking') : t('chat.reasoning')}
          </button>
          {reasoningOpen ? (
            <MarkdownMessage
              content={message.reasoning}
              className="playground-markdown-reasoning px-3 pb-3 text-xs leading-5 text-muted-soft"
            />
          ) : null}
        </div>
      ) : null}

      {message.content ? (
        <div>
          <MarkdownMessage content={message.content} className="text-[15px] leading-7" />
          {streaming && liveDuration ? (
            <div className="mt-1 flex items-center gap-1.5 whitespace-nowrap text-xs text-faint">
              <span className="inline-block h-3 w-1.5 animate-pulse rounded-sm bg-foreground align-middle" />
              <span className="tabular-nums">{t('chat.elapsedDuration', { duration: liveDuration })}</span>
            </div>
          ) : null}
        </div>
      ) : streaming ? (
        <span className="inline-flex items-center gap-2 text-xs text-faint">
          <span className="inline-block h-4 w-2 animate-pulse rounded-sm bg-foreground align-middle" />
          {liveDuration ? (
            <span className="tabular-nums">{t('chat.elapsedDuration', { duration: liveDuration })}</span>
          ) : null}
        </span>
      ) : null}

      {message.error ? <p className="text-sm text-red-500">{message.error}</p> : null}

      {(message.content || message.error || isLast) && !streaming ? (
        <>
          <div className="hidden items-center gap-2 md:flex">
            <div className="flex items-center gap-0.5">
              {isLast ? (
                <PlaygroundRegenerateAction
                  label={t('actions.regenerate')}
                  message={t('confirm.regenerate')}
                  disabled={busy}
                  onConfirm={() => onRegenerate(message.id)}
                />
              ) : null}
              {message.content ? (
                <PlaygroundMessageAction label={t('actions.copy')} onClick={copy}>
                  <Copy className="h-3.5 w-3.5" />
                </PlaygroundMessageAction>
              ) : null}
            </div>
            {hasSource || hasUsage || responseDuration ? (
              <div className="flex min-w-0 items-center gap-1.5 text-xs text-faint">
                {message.model ? (
                  <span className="flex min-w-0 items-center gap-1.5">
                    <PlaygroundModelIcon
                      modelId={message.model}
                      displayName={gatewayModel?.displayName}
                      ownedBy={gatewayModel?.ownedBy}
                    />
                    <span className="truncate">{message.model}</span>
                    {message.siteName ? <span>|</span> : null}
                    {message.siteName ? <span className="truncate">{message.siteName}</span> : null}
                  </span>
                ) : message.siteName ? (
                  <span className="truncate">{message.siteName}</span>
                ) : null}
                {hasSource && (hasUsage || responseDuration) ? <span>·</span> : null}
                {hasUsage ? (
                  <span>
                    {t('chat.usage', {
                      prompt: message.usage?.prompt_tokens ?? 0,
                      completion: message.usage?.completion_tokens ?? 0,
                      total: message.usage?.total_tokens ?? 0,
                    })}
                  </span>
                ) : null}
                {hasUsage && responseDuration ? <span>·</span> : null}
                {responseDuration ? (
                  <span className="whitespace-nowrap tabular-nums">
                    {t('chat.responseDuration', { duration: responseDuration })}
                  </span>
                ) : null}
              </div>
            ) : null}
          </div>

          <div className="flex min-w-0 items-center gap-1 md:hidden">
            {isLast ? (
              <PlaygroundRegenerateAction
                label={t('actions.regenerate')}
                message={t('confirm.regenerate')}
                disabled={busy}
                onConfirm={() => onRegenerate(message.id)}
              />
            ) : null}
            {message.content ? (
              <PlaygroundMessageAction label={t('actions.copy')} onClick={copy}>
                <Copy className="h-3.5 w-3.5" />
              </PlaygroundMessageAction>
            ) : null}
            <div className="flex min-w-0 flex-1 items-center gap-1.5 text-xs text-faint">
              {message.model ? (
                <span className="flex min-w-0 items-center gap-1.5">
                  <PlaygroundModelIcon
                    modelId={message.model}
                    displayName={gatewayModel?.displayName}
                    ownedBy={gatewayModel?.ownedBy}
                  />
                  <span className="truncate">{message.model}</span>
                  {message.siteName ? <span>|</span> : null}
                  {message.siteName ? <span className="truncate">{message.siteName}</span> : null}
                </span>
              ) : message.siteName ? (
                <span className="truncate">{message.siteName}</span>
              ) : null}
            </div>
          </div>
          {hasUsage || responseDuration ? (
            <div className="flex flex-wrap items-center gap-1.5 text-xs leading-5 text-faint md:hidden">
              {hasUsage ? (
                <span>
                  {t('chat.usage', {
                    prompt: message.usage?.prompt_tokens ?? 0,
                    completion: message.usage?.completion_tokens ?? 0,
                    total: message.usage?.total_tokens ?? 0,
                  })}
                </span>
              ) : null}
              {hasUsage && responseDuration ? <span>·</span> : null}
              {responseDuration ? (
                <span className="whitespace-nowrap tabular-nums">
                  {t('chat.responseDuration', { duration: responseDuration })}
                </span>
              ) : null}
            </div>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
