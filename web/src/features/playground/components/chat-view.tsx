import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ImageIcon, Lightbulb, Paperclip, Sparkles } from 'lucide-react'
import { APIError } from '@/lib/http'
import { streamChat, type ChatTurn } from '@/features/playground/api/playground'
import { Composer } from '@/features/playground/components/composer'
import { ModelReasoningPicker } from '@/features/playground/components/model-reasoning-picker'
import { ChatMessageItem } from '@/features/playground/components/chat-message'
import { ChatAttachmentItem } from '@/features/playground/components/chat-attachment'
import { normalizeReasoningEffort } from '@/features/playground/lib/reasoning'
import { newId } from '@/features/playground/lib/storage'
import {
  RESPONSE_TIMER_REVEAL_MS,
  RESPONSE_TIMER_TICK_MS,
  responseTimestamp,
} from '@/features/playground/lib/response-timing'
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
const TEXT_ATTACHMENT_EXTENSIONS = new Set([
  'txt', 'md', 'csv', 'xml', 'html', 'js', 'ts', 'tsx', 'jsx', 'py', 'go', 'java', 'c', 'cpp', 'h',
  'hpp', 'rs', 'rb', 'php', 'sh', 'yaml', 'yml', 'toml', 'log',
])

function fileToDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error ?? new Error('failed to read attachment'))
    reader.readAsDataURL(file)
  })
}

function attachmentMimeType(file: File): string {
  if (file.type) return file.type
  const extension = file.name.split('.').pop()?.toLowerCase()
  if (extension === 'pdf') return 'application/pdf'
  if (extension === 'json') return 'application/json'
  if (extension === 'csv') return 'text/csv'
  if (extension && TEXT_ATTACHMENT_EXTENSIONS.has(extension)) return 'text/plain'
  return 'application/octet-stream'
}

type ChatViewProps = {
  apiKey: string | null
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
  apiKey,
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
  const [streaming, setStreaming] = useState(false)
  const [streamingId, setStreamingId] = useState<string | null>(null)
  const [streamingElapsedMs, setStreamingElapsedMs] = useState<number | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const { handleScroll, scrollToBottom, scrollToBottomIfStuck } = useStickToBottom(scrollRef)

  const isEmpty = conversation.messages.length === 0
  const canSend = Boolean(apiKey && model && (input.trim() || attachments.length > 0) && !streaming)

  const lastUserId = useMemo(() => {
    for (let index = conversation.messages.length - 1; index >= 0; index -= 1) {
      if (conversation.messages[index].role === 'user') return conversation.messages[index].id
    }
    return null
  }, [conversation.messages])
  const lastMessageId = conversation.messages[conversation.messages.length - 1]?.id ?? null
  const modelsById = useMemo(() => new Map(models.map((item) => [item.id, item])), [models])

  useEffect(() => {
    scrollToBottom()
  }, [conversation.id, scrollToBottom])

  useEffect(() => {
    scrollToBottomIfStuck()
  }, [conversation.messages, streaming, scrollToBottomIfStuck])

  useEffect(() => () => abortRef.current?.abort(), [])

  const patchMessage = (id: string, patch: (message: ChatMessage) => ChatMessage) => {
    onChange((current) => ({
      ...current,
      updatedAt: Date.now(),
      messages: current.messages.map((message) => (message.id === id ? patch(message) : message)),
    }))
  }

  const buildTurns = (messages: ChatMessage[]): ChatTurn[] => {
    const turns: ChatTurn[] = []
    if (conversation.systemPrompt.trim()) {
      turns.push({ role: 'system', content: conversation.systemPrompt.trim() })
    }
    for (const message of messages) {
      if ((message.content || message.attachments?.some((attachment) => attachment.dataURL)) && !message.error) {
        turns.push({ role: message.role, content: message.content, attachments: message.attachments })
      }
    }
    return turns
  }

  const streamAssistant = async (keptMessages: ChatMessage[], extra?: Partial<Conversation>) => {
    if (!apiKey || !model || streaming) return
    const startedAt = responseTimestamp()
    const activeModel = models.find((item) => item.id === model)
    const protocol = autoProtocol(activeModel)
    const assistantId = newId()
    const assistantMessage: ChatMessage = {
      id: assistantId,
      role: 'assistant',
      content: '',
      model: model ?? undefined,
      createdAt: Date.now(),
    }
    onChange((current) => ({
      ...current,
      ...extra,
      model,
      updatedAt: Date.now(),
      messages: [...keptMessages, assistantMessage],
    }))
    setStreamingElapsedMs(null)
    setStreaming(true)
    setStreamingId(assistantId)
    scrollToBottom()

    let elapsedIntervalId: number | null = null
    let revealTimerId: number | null = null
    const updateElapsed = () => setStreamingElapsedMs(Math.round(responseTimestamp() - startedAt))
    const revealElapsed = () => {
      if (elapsedIntervalId !== null) return
      if (revealTimerId !== null) window.clearTimeout(revealTimerId)
      updateElapsed()
      elapsedIntervalId = window.setInterval(updateElapsed, RESPONSE_TIMER_TICK_MS)
    }
    revealTimerId = window.setTimeout(revealElapsed, RESPONSE_TIMER_REVEAL_MS)

    const controller = new AbortController()
    abortRef.current = controller

    try {
      await streamChat(protocol, {
        apiKey,
        model,
        messages: buildTurns(keptMessages),
        reasoningEffort: normalizeReasoningEffort(activeModel, effort),
        signal: controller.signal,
        onContent: (delta) => {
          if (delta) revealElapsed()
          patchMessage(assistantId, (message) => ({ ...message, content: message.content + delta }))
        },
        onReasoning: (delta) => {
          if (delta) revealElapsed()
          patchMessage(assistantId, (message) => ({
            ...message,
            reasoning: (message.reasoning ?? '') + delta,
          }))
        },
        onUsage: (usage) => patchMessage(assistantId, (message) => ({ ...message, usage })),
        onRouteSite: (siteName) =>
          patchMessage(assistantId, (message) => ({ ...message, siteName })),
      })
    } catch (error) {
      if (!(error instanceof DOMException && error.name === 'AbortError')) {
        const messageText =
          error instanceof APIError || error instanceof Error ? error.message : t('chat.errorGeneric')
        patchMessage(assistantId, (message) => ({
          ...message,
          error: messageText,
        }))
      }
    } finally {
      if (revealTimerId !== null) window.clearTimeout(revealTimerId)
      if (elapsedIntervalId !== null) window.clearInterval(elapsedIntervalId)
      patchMessage(assistantId, (message) => ({
        ...message,
        responseDurationMs: Math.round(responseTimestamp() - startedAt),
      }))
      setStreamingElapsedMs(null)
      setStreaming(false)
      setStreamingId(null)
      abortRef.current = null
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
      const next = await Promise.all(selected.map(async (file): Promise<ChatAttachment> => ({
        id: newId(),
        name: file.name,
        mimeType: attachmentMimeType(file),
        size: file.size,
        dataURL: await fileToDataURL(file),
      })))
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
    if (!apiKey || !model) return
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
      onStop={() => abortRef.current?.abort()}
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
      attachments={attachments.length > 0 || attachmentError ? (
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
              disabled={!apiKey}
            />
          </div>
          <div className="min-w-0 md:hidden">
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
              triggerClassName="h-11 min-w-0 w-[min(13.5rem,calc(100vw-7rem))] max-w-full"
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
