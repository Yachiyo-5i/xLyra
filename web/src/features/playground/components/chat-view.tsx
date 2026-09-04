import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ImageIcon, Lightbulb, Paperclip, Sparkles } from 'lucide-react'
import { APIError } from '@/lib/http'
import {
  cancelServerRun,
  followServerConversation,
  getServerConversation,
  startServerTurn,
  type PlaygroundRolloutEvent,
  type ServerConversationView,
} from '@/features/playground/api/playground'
import { Composer } from '@/features/playground/components/composer'
import { ModelReasoningPicker } from '@/features/playground/components/model-reasoning-picker'
import { ChatMessageItem } from '@/features/playground/components/chat-message'
import { ChatAttachmentItem } from '@/features/playground/components/chat-attachment'
import { normalizeReasoningEffort } from '@/features/playground/lib/reasoning'
import { attachmentMimeType, normalizeAttachmentDataURL } from '@/features/playground/lib/attachments'
import { newId } from '@/features/playground/lib/storage'
import { RESPONSE_TIMER_TICK_MS } from '@/features/playground/lib/response-timing'
import { useStickToBottom } from '@/hooks/use-stick-to-bottom'
import { saveChatAttachmentDataAsync } from '@/features/playground/lib/attachment-store'
import type {
  ChatMessage,
  ChatAttachment,
  ChatProtocol,
  Conversation,
  GatewayModel,
  ReasoningEffort,
} from '@/features/playground/lib/types'

const MAX_ATTACHMENTS = 4
const MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024
function fileToDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error ?? new Error('failed to read attachment'))
    reader.readAsDataURL(file)
  })
}

type ChatViewProps = {
  apiKeys: Array<{ id: string; name: string }>
  apiKeyId: string | null
  onAPIKeyChange: (id: string) => void
  model: string | null
  models: GatewayModel[]
  onModelChange: (id: string) => void
  effort: ReasoningEffort
  onEffortChange: (effort: ReasoningEffort) => void
  conversation: Conversation
  onChange: (updater: (conversation: Conversation) => Conversation) => void
  onImageMode: () => void
}

function autoProtocol(model: GatewayModel | undefined): ChatProtocol {
  const types = model?.endpointTypes ?? []
  if (types.includes('openai-response')) return 'responses'
  if (types.some((t) => t.startsWith('anthropic'))) return 'messages'
  return 'chat'
}

export function ChatView({
  apiKeys,
  apiKeyId,
  onAPIKeyChange,
  model,
  models,
  onModelChange,
  effort,
  onEffortChange,
  conversation,
  onChange,
  onImageMode,
}: ChatViewProps) {
  const { t } = useTranslation('playground')
  const [input, setInput] = useState('')
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [attachmentError, setAttachmentError] = useState<string | null>(null)
  const [sendError, setSendError] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const [streamingElapsedMs, setStreamingElapsedMs] = useState<number | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const [stopRequested, setStopRequested] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const { handleScroll, scrollToBottom, scrollToBottomIfStuck } = useStickToBottom(scrollRef)

  const isEmpty = conversation.messages.length === 0
  const runActive = conversation.activeRun?.status === 'queued' || conversation.activeRun?.status === 'running'
  const streaming = starting || runActive
  const canSend = Boolean(apiKeyId && model && (input.trim() || attachments.length > 0) && !streaming)

  const lastUserId = useMemo(() => {
    for (let index = conversation.messages.length - 1; index >= 0; index -= 1) {
      if (conversation.messages[index].role === 'user') return conversation.messages[index].id
    }
    return null
  }, [conversation.messages])
  const lastMessageId = conversation.messages[conversation.messages.length - 1]?.id ?? null
  const streamingId = streaming
    ? [...conversation.messages].reverse().find((message) => message.role === 'assistant')?.id ?? null
    : null
  const modelsById = useMemo(() => new Map(models.map((item) => [item.id, item])), [models])
  const subscriptionState = useEffectEvent(() => ({
    id: conversation.id,
    lastOrdinal: conversation.lastOrdinal ?? 0,
    run: conversation.activeRun,
  }))

  useEffect(() => {
    scrollToBottom()
  }, [conversation.id, scrollToBottom])

  useEffect(() => {
    scrollToBottomIfStuck()
  }, [conversation.messages, streaming, scrollToBottomIfStuck])

  useEffect(() => () => abortRef.current?.abort(), [])

  useEffect(() => {
    if (!stopRequested) return
    const run = conversation.activeRun
    if (!run) return
    let cancelled = false
    queueMicrotask(() => {
      if (cancelled) return
      if (run.status === 'queued' || run.status === 'running') {
        void cancelServerRun(run.id)
      }
      setStopRequested(false)
    })
    return () => {
      cancelled = true
    }
  }, [stopRequested, conversation.activeRun])

  useEffect(() => {
    if (!streaming) return
    const startedAt = conversation.activeRun?.created_at ?? Date.now()
    const update = () => setStreamingElapsedMs(Math.max(0, Date.now() - startedAt))
    const interval = window.setInterval(update, RESPONSE_TIMER_TICK_MS)
    return () => window.clearInterval(interval)
  }, [streaming, conversation.activeRun?.created_at])

  const applyServerView = useCallback((view: ServerConversationView) => {
    if (!view.chat) return
    const next = {
      ...view.chat,
      serverPersisted: true,
      lastOrdinal: view.last_ordinal,
      activeRun: view.run,
    }
    onChange(() => next)
  }, [onChange])

  const applyRolloutEvent = useCallback((event: PlaygroundRolloutEvent) => {
    onChange((current) => {
      let messages = current.messages
      if (event.type === 'assistant_delta') {
        const payload = event.payload as { message_id?: string; content?: string; reasoning?: string }
        messages = messages.map((message) => message.id === payload.message_id ? {
          ...message,
          content: message.content + (payload.content ?? ''),
          reasoning: (message.reasoning ?? '') + (payload.reasoning ?? ''),
        } : message)
      }
      if (event.type === 'assistant_final') {
        const final = event.payload as ChatMessage
        messages = messages.map((message) => message.id === final.id ? final : message)
      }
      if (event.type === 'turn_failed' || event.type === 'turn_cancelled' || event.type === 'turn_interrupted') {
        const payload = event.payload as { message_id?: string; error?: string; response_duration_ms?: number }
        messages = messages.map((message) => message.id === payload.message_id ? {
          ...message,
          error: payload.error,
          responseDurationMs: payload.response_duration_ms,
        } : message)
      }
      return { ...current, messages, lastOrdinal: event.ordinal, updatedAt: Date.now() }
    })
  }, [onChange])

  useEffect(() => {
    const current = subscriptionState()
    const run = current.run
    const active = run?.status === 'queued' || run?.status === 'running'
    if (!active || !run) return
    const controller = new AbortController()
    abortRef.current?.abort()
    abortRef.current = controller
    let cursor = current.lastOrdinal ?? 0
    let terminal = false
    const follow = async () => {
      while (!controller.signal.aborted && !terminal) {
        try {
          await followServerConversation(
            current.id,
            cursor,
            controller.signal,
            (event) => {
              cursor = Math.max(cursor, event.ordinal)
              applyRolloutEvent(event)
            },
            (nextRun) => {
              terminal = nextRun.status !== 'queued' && nextRun.status !== 'running'
            },
          )
          const view = await getServerConversation(current.id)
          applyServerView(view)
          terminal = view.run?.status !== 'queued' && view.run?.status !== 'running'
        } catch {
          if (controller.signal.aborted) return
          await new Promise((resolve) => window.setTimeout(resolve, 750))
        }
      }
    }
    void follow()
    return () => controller.abort()
  }, [conversation.id, conversation.activeRun?.id, conversation.activeRun?.status, applyRolloutEvent, applyServerView])

  const streamAssistant = async (keptMessages: ChatMessage[], extra?: Partial<Conversation>) => {
    if (!apiKeyId || !model || streaming) return
    const activeModel = models.find((item) => item.id === model)
    const protocol = autoProtocol(activeModel)
    const nextConversation: Conversation = {
      ...conversation,
      ...extra,
      model,
      updatedAt: Date.now(),
      messages: keptMessages,
    }
    onChange(() => nextConversation)
    setSendError(null)
    setStarting(true)
    scrollToBottom()
    try {
      const view = await startServerTurn(conversation.id, {
        mode: 'chat',
        api_key_id: apiKeyId,
        model,
        protocol,
        reasoning_effort: normalizeReasoningEffort(activeModel, effort),
        idempotency_key: newId(),
        legacy_import: !conversation.serverPersisted,
        chat: nextConversation,
      })
      applyServerView(view)
      setStarting(false)
    } catch (error) {
      setStarting(false)
      setStopRequested(false)
      const messageText = error instanceof APIError || error instanceof Error ? error.message : t('chat.errorGeneric')
      try {
        const view = await getServerConversation(conversation.id)
        applyServerView(view)
        setSendError(messageText)
      } catch (lookup) {
        if (lookup instanceof APIError && lookup.status === 404) {
          onChange((current) => ({
            ...current,
            serverPersisted: false,
            messages: [...current.messages, { id: newId(), role: 'assistant', content: '', error: messageText, model: model ?? undefined, createdAt: Date.now() }],
          }))
        } else {
          setSendError(messageText)
        }
      }
    }
  }

  const addAttachments = async (files: FileList | File[] | null) => {
    if (!files) return
    const remaining = MAX_ATTACHMENTS - attachments.length
    const selected = Array.from(files).slice(0, Math.max(remaining, 0))
    if (selected.length === 0) {
      setAttachmentError(t('chat.attachmentLimit', { count: MAX_ATTACHMENTS }))
      return
    }
    const oversized = selected.find((file) => file.size > MAX_ATTACHMENT_BYTES)
    if (oversized) {
      setAttachmentError(t('chat.attachmentTooLarge', { name: oversized.name }))
      return
    }
    try {
      const next = await Promise.all(selected.map(async (file): Promise<ChatAttachment> => {
        const mimeType = attachmentMimeType(file)
        const dataURL = await fileToDataURL(file)
        return {
          id: newId(),
          name: file.name,
          mimeType,
          size: file.size,
          dataURL: normalizeAttachmentDataURL(dataURL, mimeType),
        }
      }))
      setAttachments((current) => [...current, ...next].slice(0, MAX_ATTACHMENTS))
      setAttachmentError(files.length > remaining ? t('chat.attachmentLimit', { count: MAX_ATTACHMENTS }) : null)
    } catch {
      setAttachmentError(t('chat.attachmentReadFailed'))
    }
  }

  const handlePasteFiles = (files: File[]) => {
    if (streaming) return false
    void addAttachments(files)
    return true
  }

  const handleSend = () => {
    if (!apiKeyId || !model) return
    const text = input.trim()
    if ((!text && attachments.length === 0) || streaming) return
    const userMessage: ChatMessage = {
      id: newId(),
      role: 'user',
      content: text,
      attachments,
      createdAt: Date.now(),
    }
    void saveChatAttachmentDataAsync(attachments)
    setInput('')
    setAttachments([])
    setAttachmentError(null)
    void streamAssistant([...conversation.messages, userMessage], {
      title: conversation.title || text.slice(0, 40) || attachments[0]?.name || '',
    })
  }

  const handleRegenerate = (messageId: string) => {
    if (streaming) return
    const index = conversation.messages.findIndex((message) => message.id === messageId)
    if (index < 0) return
    let userIndex = index
    if (conversation.messages[index].role === 'assistant') {
      userIndex = -1
      for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
        if (conversation.messages[cursor].role === 'user') {
          userIndex = cursor
          break
        }
      }
    }
    if (userIndex < 0) return
    void streamAssistant(conversation.messages.slice(0, userIndex + 1))
  }

  const handleEditSubmit = (messageId: string, text: string) => {
    if (streaming) return
    const index = conversation.messages.findIndex((message) => message.id === messageId)
    if (index < 0) return
    const edited: ChatMessage = { ...conversation.messages[index], content: text }
    void streamAssistant([...conversation.messages.slice(0, index), edited])
  }

  const composer = (
    <Composer
      value={input}
      onChange={setInput}
      onSubmit={handleSend}
      onStop={() => {
        const runId = conversation.activeRun?.id
        if (runId && runActive) {
          void cancelServerRun(runId)
        } else if (starting) {
          setStopRequested(true)
        }
      }}
      streaming={streaming}
      canSubmit={canSend}
      placeholder={t('chat.inputPlaceholder')}
      onPasteFiles={handlePasteFiles}
      controls={
        <>
          <input
            ref={fileInputRef}
            type="file"
            multiple
            className="hidden"
            onChange={(event) => {
              void addAttachments(event.target.files)
              event.target.value = ''
            }}
          />
          <button
            type="button"
            onClick={() => fileInputRef.current?.click()}
            disabled={streaming || attachments.length >= MAX_ATTACHMENTS}
            className="flex h-11 w-11 items-center justify-center rounded-full text-muted-soft transition-colors active:bg-[hsl(var(--surface-subtle))] active:text-foreground disabled:cursor-not-allowed disabled:opacity-40 md:h-8 md:w-8 md:hover:bg-[hsl(var(--surface-subtle))] md:hover:text-foreground"
            aria-label={t('chat.attach')}
            title={t('chat.attach')}
          >
            <Paperclip className="h-4 w-4" />
          </button>
        </>
      }
      attachments={attachments.length > 0 || attachmentError || sendError ? (
        <div className="space-y-2">
          {attachments.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {attachments.map((attachment) => (
                <ChatAttachmentItem
                  key={attachment.id}
                  attachment={attachment}
                  removeLabel={t('chat.removeAttachment')}
                  onRemove={() => setAttachments((current) => current.filter((item) => item.id !== attachment.id))}
                />
              ))}
            </div>
          ) : null}
          {attachmentError ? <p className="px-1 text-xs text-red-500">{attachmentError}</p> : null}
          {sendError ? <p className="px-1 text-xs text-red-500">{sendError}</p> : null}
        </div>
      ) : undefined}
      trailingControls={
        <>
          <div className="hidden md:block">
            <ModelReasoningPicker
              models={models}
              model={model}
              onModelChange={onModelChange}
              effort={effort}
              onEffortChange={onEffortChange}
              disabled={!apiKeyId}
            />
          </div>
          <div className="md:hidden">
            <ModelReasoningPicker
              apiKeys={apiKeys}
              apiKeyId={apiKeyId}
              onAPIKeyChange={onAPIKeyChange}
              models={models}
              model={model}
              onModelChange={onModelChange}
              effort={effort}
              onEffortChange={onEffortChange}
              disabled={apiKeys.length === 0}
              triggerClassName="h-11"
            />
          </div>
        </>
      }
    />
  )

  if (isEmpty) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-4">
        <div className="relative w-full max-w-2xl">
          <div className="mb-6 flex h-20 items-center justify-center text-center">
            <h2 className="text-2xl font-semibold text-foreground">{t('chat.greeting')}</h2>
          </div>
          {composer}
          <div className="absolute left-0 right-0 top-full mt-4 flex flex-wrap justify-center gap-2">
            <button
              type="button"
              onClick={onImageMode}
              className="inline-flex items-center gap-1.5 rounded-full border border-[hsl(var(--glass-border))] px-3.5 py-1.5 text-sm text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
            >
              <ImageIcon className="h-4 w-4" />
              {t('chat.suggestImage')}
            </button>
            <button
              type="button"
              onClick={() => setInput(t('chat.suggestWritePrompt'))}
              className="inline-flex items-center gap-1.5 rounded-full border border-[hsl(var(--glass-border))] px-3.5 py-1.5 text-sm text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
            >
              <Sparkles className="h-4 w-4" />
              {t('chat.suggestWrite')}
            </button>
            <button
              type="button"
              onClick={() => setInput(t('chat.suggestExplainPrompt'))}
              className="inline-flex items-center gap-1.5 rounded-full border border-[hsl(var(--glass-border))] px-3.5 py-1.5 text-sm text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
            >
              <Lightbulb className="h-4 w-4" />
              {t('chat.suggestExplain')}
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div ref={scrollRef} onScroll={handleScroll} className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl space-y-6 px-4 py-6">
          {conversation.messages.map((message) => (
            <ChatMessageItem
              key={message.id}
              message={message}
              streaming={streaming && message.id === streamingId}
              streamingElapsedMs={message.id === streamingId ? streamingElapsedMs : null}
              busy={streaming}
              canEdit={message.id === lastUserId}
              isLast={message.id === lastMessageId}
              gatewayModel={message.model ? modelsById.get(message.model) : undefined}
              onRegenerate={handleRegenerate}
              onEditSubmit={handleEditSubmit}
            />
          ))}
        </div>
      </div>
      <div className="shrink-0 px-4 pb-4">
        <div className="mx-auto max-w-3xl">
          {composer}
          <p className="mt-2 text-center text-xs text-faint">{t('chat.disclaimer')}</p>
        </div>
      </div>
    </div>
  )
}
