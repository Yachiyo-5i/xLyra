import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Copy, Download, ImagePlus, Loader2, Paperclip, Pencil } from 'lucide-react'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { TextArea } from '@/components/ui/textarea'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
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
import { ChatAttachmentItem } from '@/features/playground/components/chat-attachment'
import { ModelParameterPicker } from '@/features/playground/components/model-parameter-picker'
import {
  PlaygroundMessageAction,
  PlaygroundRegenerateAction,
} from '@/features/playground/components/playground-message-actions'
import { PlaygroundModelIcon } from '@/features/playground/components/playground-model-icon'
import { CollapsibleUserMessage } from '@/features/playground/components/collapsible-user-message'
import { newId } from '@/features/playground/lib/storage'
import {
  formatResponseDuration,
  RESPONSE_TIMER_TICK_MS,
  responseTimestamp,
} from '@/features/playground/lib/response-timing'
import { useStickToBottom } from '@/hooks/use-stick-to-bottom'
import type { GatewayModel, ImageConversation, ImageHistoryEntry } from '@/features/playground/lib/types'

type ImageViewProps = {
  apiKeys: Array<{ id: string; name: string }>
  apiKeyId: string | null
  onAPIKeyChange: (id: string) => void
  model: string | null
  models: GatewayModel[]
  onModelChange: (id: string) => void
  conversation: ImageConversation
  onChange: (updater: (conversation: ImageConversation) => ImageConversation) => void
}

const SIZE_OPTIONS = ['auto', '1024x1024', '1024x1536', '1536x1024']

function dataURLToFile(dataURL: string, filename: string): File {
  const [meta, base64] = dataURL.split(',')
  const mime = /data:(.*?);base64/.exec(meta)?.[1] ?? 'image/png'
  const binary = atob(base64)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index)
  }
  return new File([bytes], filename, { type: mime })
}

function fileToDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error ?? new Error('failed to read image'))
    reader.readAsDataURL(file)
  })
}

function ImageAttachment({
  file,
  removeLabel,
  onRemove,
}: {
  file: File
  removeLabel: string
  onRemove: () => void
}) {
  const [src, setSrc] = useState<string | null>(null)

  useEffect(() => {
    const reader = new FileReader()
    reader.onload = () => setSrc(String(reader.result))
    reader.readAsDataURL(file)
    return () => {
      reader.onload = null
      if (reader.readyState === FileReader.LOADING) reader.abort()
    }
  }, [file])

  return (
    <ChatAttachmentItem
      attachment={{
        id: `${file.name}:${file.size}:${file.lastModified}`,
        name: file.name,
        mimeType: file.type,
        size: file.size,
        dataURL: src ?? undefined,
      }}
      removeLabel={removeLabel}
      onRemove={onRemove}
    />
  )
}

async function resyncServerConversation(
  id: string,
  apply: (view: ServerConversationView) => void,
): Promise<'ok' | 'missing' | 'error'> {
  try {
    apply(await getServerConversation(id))
    return 'ok'
  } catch (lookup) {
    return lookup instanceof APIError && lookup.status === 404 ? 'missing' : 'error'
  }
}

export function ImageView({
  apiKeys,
  apiKeyId,
  onAPIKeyChange,
  model,
  models,
  onModelChange,
  conversation,
  onChange,
}: ImageViewProps) {
  const { t } = useTranslation('playground')
  const [prompt, setPrompt] = useState('')
  const [size, setSize] = useState('auto')
  const [files, setFiles] = useState<File[]>([])
  const [starting, setStarting] = useState(false)
  const [sendError, setSendError] = useState<string | null>(null)
  const [submittingElapsedMs, setSubmittingElapsedMs] = useState<number | null>(null)
  const [lightbox, setLightbox] = useState<string | null>(null)
  const [editingEntryId, setEditingEntryId] = useState<string | null>(null)
  const [editDraft, setEditDraft] = useState('')
  const [stopRequested, setStopRequested] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const { handleScroll, scrollToBottom, scrollToBottomIfStuck } = useStickToBottom(scrollRef)

  const isEmpty = conversation.entries.length === 0
  const runActive = conversation.activeRun?.status === 'queued' || conversation.activeRun?.status === 'running'
  const submitting = starting || runActive
  const submittingEntryId = submitting ? conversation.entries[conversation.entries.length - 1]?.id ?? null : null
  const canSubmit = Boolean(apiKeyId && model && prompt.trim() && !submitting)
  const modelsById = useMemo(() => new Map(models.map((item) => [item.id, item])), [models])
  const subscriptionState = useEffectEvent(() => ({
    id: conversation.id,
    lastOrdinal: conversation.lastOrdinal ?? 0,
    run: conversation.activeRun,
  }))

  const lastImageSrc = useMemo(() => {
    for (let index = conversation.entries.length - 1; index >= 0; index -= 1) {
      const images = conversation.entries[index].images
      if (images && images.length > 0) return images[images.length - 1].src
    }
    return null
  }, [conversation.entries])

  const carrySource = files.length === 0 ? lastImageSrc : null

  useEffect(() => {
    scrollToBottom()
  }, [conversation.id, scrollToBottom])

  useEffect(() => {
    scrollToBottomIfStuck()
  }, [conversation.entries, submitting, scrollToBottomIfStuck])

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
    if (!submitting) return
    const startedAt = conversation.activeRun?.created_at ?? Date.now()
    const update = () => setSubmittingElapsedMs(Math.max(0, Date.now() - startedAt))
    const interval = window.setInterval(update, RESPONSE_TIMER_TICK_MS)
    return () => window.clearInterval(interval)
  }, [submitting, conversation.activeRun?.created_at])

  const addFiles = (list: FileList | File[] | null) => {
    if (!list) return
    const images = Array.from(list).filter((file) => file.type.startsWith('image/'))
    setFiles((current) => [...current, ...images].slice(0, 4))
  }

  const handlePasteFiles = (pastedFiles: File[]) => {
    if (submitting) return false
    const images = pastedFiles.filter((file) => file.type.startsWith('image/'))
    if (images.length === 0) return false
    addFiles(images)
    return true
  }

  const patchEntry = (id: string, patch: Partial<ImageHistoryEntry>) => {
    onChange((current) => ({
      ...current,
      updatedAt: Date.now(),
      entries: current.entries.map((entry) => (entry.id === id ? { ...entry, ...patch } : entry)),
    }))
  }

  const applyServerView = useCallback((view: ServerConversationView) => {
    if (!view.image) return
    const next = {
      ...view.image,
      serverPersisted: true,
      lastOrdinal: view.last_ordinal,
      activeRun: view.run,
    }
    onChange(() => next)
  }, [onChange])

  const applyRolloutEvent = useCallback((event: PlaygroundRolloutEvent) => {
    onChange((current) => {
      let entries = current.entries
      if (event.type === 'image_final') {
        const final = event.payload as ImageHistoryEntry
        entries = entries.map((entry) => entry.id === final.id ? final : entry)
      }
      if (event.type === 'turn_failed' || event.type === 'turn_cancelled' || event.type === 'turn_interrupted') {
        const payload = event.payload as { entry_id?: string; error?: string; response_duration_ms?: number }
        entries = entries.map((entry) => entry.id === payload.entry_id ? {
          ...entry,
          pending: false,
          error: payload.error,
          responseDurationMs: payload.response_duration_ms,
        } : entry)
      }
      return { ...current, entries, lastOrdinal: event.ordinal, updatedAt: Date.now() }
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

  const runEntry = async ({
    entryId,
    text,
    requestModel,
    requestSize,
    mode,
    sourceImages,
    append,
    startedAt,
    createdAt,
  }: {
    entryId: string
    text: string
    requestModel: string
    requestSize: string
    mode: ImageHistoryEntry['mode']
    sourceImages: string[]
    append: boolean
    startedAt: number
    createdAt: number
  }) => {
    if (!apiKeyId || submitting) return

    if (append) {
      onChange((current) => ({
        ...current,
        title: current.title || text.slice(0, 40),
        updatedAt: Date.now(),
        entries: [
          ...current.entries,
          {
            id: entryId,
            mode,
            model: requestModel,
            prompt: text,
            size: requestSize,
            sourceImages,
            images: [],
            pending: true,
            createdAt: Date.now(),
          },
        ],
      }))
    } else {
      patchEntry(entryId, {
        mode,
        model: requestModel,
        prompt: text,
        size: requestSize,
        sourceImages,
        images: [],
        siteName: undefined,
        responseDurationMs: undefined,
        pending: true,
        error: undefined,
      })
    }

    setStarting(true)
    setSendError(null)
    scrollToBottom()

    try {
      const nextEntry: ImageHistoryEntry = {
        id: entryId,
        mode,
        model: requestModel,
        prompt: text,
        size: requestSize,
        sourceImages,
        images: [],
        pending: true,
        createdAt,
      }
      const entries = append
        ? [...conversation.entries, nextEntry]
        : conversation.entries.map((entry) => entry.id === entryId ? nextEntry : entry)
      const requestConversation: ImageConversation = {
        ...conversation,
        title: conversation.title || text.slice(0, 40),
        entries,
        updatedAt: createdAt,
      }
      const view = await startServerTurn(conversation.id, {
        mode: 'image',
        api_key_id: apiKeyId,
        model: requestModel,
        idempotency_key: newId(),
        legacy_import: !conversation.serverPersisted,
        image: requestConversation,
      })
      applyServerView(view)
      setStarting(false)
    } catch (err) {
      setStarting(false)
      setStopRequested(false)
      const messageText = err instanceof APIError || err instanceof Error ? err.message : t('image.errorGeneric')
      const outcome = await resyncServerConversation(conversation.id, applyServerView)
      if (outcome === 'missing') {
        onChange((current) => ({ ...current, serverPersisted: false }))
        patchEntry(entryId, { pending: false, error: messageText, responseDurationMs: Math.round(responseTimestamp() - startedAt) })
      } else {
        setSendError(messageText)
      }
    }
  }

  const handleSubmit = async () => {
    if (!apiKeyId || !model || !prompt.trim() || submitting) return
    const startedAt = responseTimestamp()
    const text = prompt.trim()
    const sourceFiles =
      files.length > 0
        ? files
        : carrySource?.startsWith('data:')
          ? [dataURLToFile(carrySource, 'source.png')]
          : []
    const sourceImages = sourceFiles.length > 0
      ? await Promise.all(sourceFiles.map(fileToDataURL))
      : carrySource ? [carrySource] : []
    const mode = sourceImages.length > 0 ? 'edit' : 'generation'
    setPrompt('')
    setFiles([])
    await runEntry({
      entryId: newId(),
      text,
      requestModel: model,
      requestSize: size,
      mode,
      sourceImages,
      append: true,
      startedAt,
      createdAt: Date.now(),
    })
  }

  const handleRetryEntry = async (entry: ImageHistoryEntry, nextPrompt = entry.prompt) => {
    const text = nextPrompt.trim()
    if (!text || submitting) return
    const startedAt = responseTimestamp()
    const entryIndex = conversation.entries.findIndex((item) => item.id === entry.id)
    let previousImages: string[] = []
    for (let index = entryIndex - 1; index >= 0; index -= 1) {
      if (conversation.entries[index].images.length > 0) {
        previousImages = conversation.entries[index].images.map((image) => image.src)
        break
      }
    }
    const fallbackImages = previousImages.length > 0 ? previousImages : entry.images.map((image) => image.src)
    const sourceImages = entry.sourceImages?.length ? entry.sourceImages : fallbackImages
    const mode = entry.mode === 'edit' && sourceImages.length > 0 ? 'edit' : 'generation'
    setEditingEntryId(null)
    await runEntry({
      entryId: entry.id,
      text,
      requestModel: entry.model,
      requestSize: entry.size ?? 'auto',
      mode,
      sourceImages,
      append: false,
      startedAt,
      createdAt: entry.createdAt,
    })
  }

  const attachments =
    files.length > 0 || sendError ? (
      <div className="space-y-2">
        {files.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {files.map((file, index) => (
              <ImageAttachment
                key={`${file.name}-${index}`}
                file={file}
                removeLabel={t('image.removeFile')}
                onRemove={() => setFiles((current) => current.filter((_, i) => i !== index))}
              />
            ))}
          </div>
        ) : null}
        {sendError ? <p className="px-1 text-xs text-red-500">{sendError}</p> : null}
      </div>
    ) : null

  const controls = (
    <>
      <input
        ref={fileInputRef}
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={(event) => {
          addFiles(event.target.files)
          event.target.value = ''
        }}
      />
      <button
        type="button"
        onClick={() => fileInputRef.current?.click()}
        className="flex h-11 w-11 items-center justify-center rounded-full text-muted-soft transition-colors active:bg-[hsl(var(--surface-subtle))] active:text-foreground md:h-8 md:w-8 md:hover:bg-[hsl(var(--surface-subtle))] md:hover:text-foreground"
        aria-label={t('image.attach')}
        title={t('image.attach')}
      >
        <Paperclip className="h-4 w-4" />
      </button>
    </>
  )

  const sizeOptions = SIZE_OPTIONS.map((option) => ({
    value: option,
    label: option === 'auto' ? t('image.sizeAuto') : option,
  }))

  const trailingControls = (
    <>
      <div className="hidden md:block">
        <ModelParameterPicker
          models={models}
          model={model}
          onModelChange={onModelChange}
          parameterLabel={t('image.size')}
          parameterValue={size}
          parameterOptions={sizeOptions}
          onParameterChange={setSize}
          disabled={!apiKeyId}
        />
      </div>
      <div className="md:hidden">
        <ModelParameterPicker
          apiKeys={apiKeys}
          apiKeyId={apiKeyId}
          onAPIKeyChange={onAPIKeyChange}
          models={models}
          model={model}
          onModelChange={onModelChange}
          parameterLabel={t('image.size')}
          parameterValue={size}
          parameterOptions={sizeOptions}
          onParameterChange={setSize}
          disabled={apiKeys.length === 0}
          triggerClassName="h-11"
        />
      </div>
    </>
  )

  const composer = (
    <Composer
      value={prompt}
      onChange={setPrompt}
      onSubmit={() => void handleSubmit()}
      onStop={() => {
        const runId = conversation.activeRun?.id
        if (runId && runActive) {
          void cancelServerRun(runId)
        } else if (starting) {
          setStopRequested(true)
        }
      }}
      streaming={submitting}
      canSubmit={canSubmit}
      placeholder={t('image.promptPlaceholder')}
      onPasteFiles={handlePasteFiles}
      controls={controls}
      trailingControls={trailingControls}
      attachments={attachments}
    />
  )

  const lightboxNode = (
    <Dialog open={lightbox !== null} onOpenChange={(open) => !open && setLightbox(null)}>
      <DialogContent className="w-auto max-w-[92vw] bg-transparent p-0 shadow-none backdrop-blur-0">
        {lightbox ? (
          <div className="flex flex-col items-center gap-3">
            <img src={lightbox} alt="" className="max-h-[80vh] max-w-[92vw] rounded-2xl object-contain" />
            <a
              href={lightbox}
              download={`${newId()}.png`}
              className="inline-flex items-center gap-2 rounded-full bg-[hsl(var(--surface-base))] px-4 py-2 text-sm text-foreground shadow-sm transition-colors hover:bg-[hsl(var(--surface-subtle))]"
            >
              <Download className="h-4 w-4" />
              {t('image.download')}
            </a>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  )

  const renderImages = (entry: ImageHistoryEntry) => {
    if (entry.pending) {
      return (
        <div className="flex h-48 w-48 items-center justify-center rounded-xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))]">
          <Loader2 className="h-6 w-6 animate-spin text-muted-soft" />
        </div>
      )
    }
    if (entry.error) {
      return <p className="text-sm text-red-500">{entry.error}</p>
    }
    return (
      <div className="flex flex-wrap gap-2">
        {entry.images.map((image) => (
          <button
            key={image.id}
            type="button"
            onClick={() => setLightbox(image.src)}
            className="block max-w-full overflow-hidden rounded-xl border border-[hsl(var(--glass-border))] transition-transform hover:scale-[1.01]"
          >
            <img src={image.src} alt={entry.prompt} className="h-auto w-auto max-h-48 max-w-full object-contain" />
          </button>
        ))}
      </div>
    )
  }

  if (isEmpty) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-4">
        <div className="relative w-full max-w-2xl">
          <div className="mb-6 flex h-20 flex-col items-center justify-center gap-2 text-center">
            <ImagePlus className="h-8 w-8 text-muted-soft" />
            <h2 className="text-2xl font-semibold text-foreground">{t('image.greeting')}</h2>
          </div>
          {composer}
        </div>
        {lightboxNode}
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div ref={scrollRef} onScroll={handleScroll} className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl space-y-6 px-4 py-6">
          {conversation.entries.map((entry, index) => {
            const isLast = index === conversation.entries.length - 1
            const editing = editingEntryId === entry.id
            const entryModel = modelsById.get(entry.model)
            const responseDuration = entry.responseDurationMs === undefined
              ? null
              : formatResponseDuration(entry.responseDurationMs)
            const liveDuration = entry.id === submittingEntryId && submittingElapsedMs !== null
              ? formatResponseDuration(submittingElapsedMs)
              : null
            const displayedDuration = entry.pending ? liveDuration : responseDuration
            return (
              <div key={entry.id} className="space-y-2">
                <div className="group flex flex-col items-end gap-1">
                  {entry.mode === 'edit' && entry.sourceImages?.length ? (
                    <div className="flex max-w-[80%] flex-wrap justify-end gap-2">
                      {entry.sourceImages.map((source, sourceIndex) => (
                        <button
                          key={`${entry.id}-source-${sourceIndex}`}
                          type="button"
                          onClick={() => setLightbox(source)}
                          className="block overflow-hidden rounded-xl border border-[hsl(var(--glass-border))] transition-transform hover:scale-[1.01]"
                        >
                          <img
                            src={source}
                            alt={entry.prompt}
                            className="h-20 w-20 object-cover md:h-24 md:w-24"
                          />
                        </button>
                      ))}
                    </div>
                  ) : null}
                  {editing ? (
                    <div className="w-full max-w-[80%] space-y-2">
                      <TextArea
                        value={editDraft}
                        onChange={(event) => setEditDraft(event.target.value)}
                        className="min-h-[72px]"
                      />
                      <div className="flex justify-end gap-2">
                        <Button type="button" variant="ghost" size="sm" onClick={() => setEditingEntryId(null)}>
                          {t('actions.cancel')}
                        </Button>
                        <Button
                          type="button"
                          size="sm"
                          disabled={!editDraft.trim() || submitting}
                          onClick={() => void handleRetryEntry(entry, editDraft)}
                        >
                          {t('actions.send')}
                        </Button>
                      </div>
                    </div>
                  ) : (
                    <CollapsibleUserMessage content={entry.prompt} />
                  )}
                  {!editing ? (
                    <div className="flex items-center gap-0.5 transition-opacity md:opacity-0 md:group-hover:opacity-100">
                      <PlaygroundMessageAction
                        label={t('actions.copy')}
                        onClick={() =>
                          void copyToClipboard(entry.prompt, t('actions.copied'), t('actions.copyFailed'))
                        }
                      >
                        <Copy className="h-3.5 w-3.5" />
                      </PlaygroundMessageAction>
                      {isLast ? (
                        <PlaygroundMessageAction
                          label={t('actions.edit')}
                          onClick={() => {
                            setEditDraft(entry.prompt)
                            setEditingEntryId(entry.id)
                          }}
                          disabled={submitting}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </PlaygroundMessageAction>
                      ) : null}
                    </div>
                  ) : null}
                </div>
                {renderImages(entry)}
                <div className="flex items-center gap-2">
                  {isLast && !entry.pending ? (
                    <PlaygroundRegenerateAction
                      label={t('actions.regenerate')}
                      message={t('confirm.regenerateImage')}
                      disabled={submitting}
                      onConfirm={() => void handleRetryEntry(entry)}
                    />
                  ) : null}
                  <div className="flex min-w-0 flex-1 items-center gap-1.5 text-xs text-faint">
                    <PlaygroundModelIcon
                      modelId={entry.model}
                      displayName={entryModel?.displayName}
                      ownedBy={entryModel?.ownedBy}
                    />
                    <span className="truncate">{entry.model}</span>
                    {entry.siteName ? <span>|</span> : null}
                    {entry.siteName ? <span className="truncate">{entry.siteName}</span> : null}
                    {displayedDuration ? <span>·</span> : null}
                    {displayedDuration ? (
                      <span className="whitespace-nowrap tabular-nums">
                        {t(entry.pending ? 'image.elapsedDuration' : 'image.responseDuration', {
                          duration: displayedDuration,
                        })}
                      </span>
                    ) : null}
                    {entry.size && entry.size !== 'auto' ? <span>· {entry.size}</span> : null}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      </div>
      <div className="shrink-0 px-4 pb-4">
        <div className="mx-auto max-w-3xl">
          {composer}
          <div className="mt-2 h-4" aria-hidden="true" />
        </div>
      </div>
      {lightboxNode}
    </div>
  )
}
