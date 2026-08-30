import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Folder, Globe, Paperclip, ShieldCheck, ShieldQuestion, TriangleAlert, TerminalSquare } from 'lucide-react'
import { Composer } from '@/features/playground/components/composer'
import { ChatAttachmentItem } from '@/features/playground/components/chat-attachment'
import { ModelReasoningPicker } from '@/features/playground/components/model-reasoning-picker'
import { normalizeReasoningEffort } from '@/features/playground/lib/reasoning'
import { formatResponseDuration } from '@/features/playground/lib/response-timing'
import type { ChatAttachment, GatewayModel, ReasoningEffort } from '@/features/playground/lib/types'
import { newId } from '@/features/playground/lib/storage'
import { AgentSidebar } from '@/features/agent/components/agent-sidebar'
import { AgentSettingsDialog } from '@/features/agent/components/agent-settings-dialog'
import { AgentTimeline } from '@/features/agent/components/agent-timeline'
import { TopbarUserControls } from '@/components/layout/topbar-user-controls'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
  appendUserMessage,
  reduceAgentEvent,
  timelineFromTranscript,
  type AgentPermissionRequest,
  type AgentTimeline as AgentTimelineData,
} from '@/features/agent/lib/agent-events'
import {
  createAgentSession,
  deleteAgentSession,
  fetchAgentAvailableModels,
  fetchAgentModelMemory,
  fetchAgentRuntimeSettings,
  fetchAgentTranscript,
  followAgentEvents,
  grantAgentAccess,
  listAgentSessions,
  stopAgentSession,
} from '@/features/agent/api/agent'
import { toast } from '@/lib/toast'

const MAX_ATTACHMENTS = 4
const MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024
const NON_CHAT_CATEGORIES = new Set(['image', 'audio', 'embedding'])
const PERMISSION_MODE_KEY = 'xlyra-agent-permission-mode'

type PermissionMode = 'ask' | 'full'

function loadPermissionMode(): PermissionMode {
  try {
    return window.localStorage.getItem(PERMISSION_MODE_KEY) === 'full' ? 'full' : 'ask'
  } catch {
    return 'ask'
  }
}

function fileToDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error ?? new Error('failed to read attachment'))
    reader.readAsDataURL(file)
  })
}

/** Placeholder shown while awaiting an agent reply: pulsing caret, label, live elapsed time. */
function WaitingIndicator() {
  const { t } = useTranslation(['agent', 'playground'])
  const [startedAt] = useState(() => Date.now())
  const [elapsedMs, setElapsedMs] = useState(0)
  useEffect(() => {
    const timer = window.setInterval(() => setElapsedMs(Date.now() - startedAt), 100)
    return () => window.clearInterval(timer)
  }, [startedAt])
  return (
    <div className="mt-4 flex items-center gap-2 text-xs text-faint">
      <span className="inline-block h-4 w-2 animate-pulse rounded-sm bg-foreground align-middle" />
      <span>{t('agent:chat.thinking')}</span>
      <span className="tabular-nums">{t('playground:chat.elapsedDuration', { duration: formatResponseDuration(elapsedMs) })}</span>
    </div>
  )
}

/** Confirmation shown before enabling full-access permission mode. */
function FullAccessConfirmDialog({ open, onCancel, onConfirm }: { open: boolean; onCancel: () => void; onConfirm: () => void }) {
  const { t } = useTranslation('agent')
  const items = [
    { icon: Folder, title: t('composer.fullConfirmFiles'), description: t('composer.fullConfirmFilesDesc') },
    { icon: TerminalSquare, title: t('composer.fullConfirmCommands'), description: t('composer.fullConfirmCommandsDesc') },
    { icon: Globe, title: t('composer.fullConfirmNetwork'), description: t('composer.fullConfirmNetworkDesc') },
  ]
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onCancel() }}>
      <DialogContent className="w-[min(92vw,480px)]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <TriangleAlert className="h-4 w-4 text-amber-500" />
            {t('composer.fullConfirmTitle')}
          </DialogTitle>
          <DialogDescription>{t('composer.fullConfirmDescription')}</DialogDescription>
        </DialogHeader>
        <DialogBody className="space-y-4">
          <div className="space-y-1 rounded-xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))]/60 p-2">
            {items.map((item) => (
              <div key={item.title} className="flex items-start gap-3 rounded-lg px-3 py-2.5">
                <item.icon className="mt-0.5 h-4 w-4 shrink-0 text-muted-soft" />
                <div className="min-w-0">
                  <p className="text-sm font-medium text-foreground">{item.title}</p>
                  <p className="mt-0.5 text-xs leading-5 text-muted-soft">{item.description}</p>
                </div>
              </div>
            ))}
          </div>
          <p className="text-xs leading-5 text-muted-soft">{t('composer.fullConfirmRisk')}</p>
        </DialogBody>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>{t('composer.fullConfirmCancel')}</Button>
          <Button variant="destructive" onClick={onConfirm}>{t('composer.fullConfirmAccept')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function AgentWorkspace() {
  const { t } = useTranslation(['agent', 'playground'])
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const sessionsQuery = useQuery({ queryKey: ['agent', 'sessions'], queryFn: listAgentSessions, retry: false })
  // Model memory lives server-side: a fresh session defaults to the last globally used model, an existing session to its own last model.
  const modelMemoryQuery = useQuery({ queryKey: ['agent', 'model-memory'], queryFn: fetchAgentModelMemory, retry: false })
  const availableModelsQuery = useQuery({
    queryKey: ['agent', 'available-models'],
    queryFn: async () => {
      const [{ allowed_site_ids: allowedSites, allowed_site_model_ids: allowedModels }, availableSites] = await Promise.all([fetchAgentRuntimeSettings(), fetchAgentAvailableModels()])
      return { allowedSites, allowedModels, availableSites }
    },
    retry: false,
  })

  const [activeId, setActiveId] = useState<string | null>(null)
  const [newSession, setNewSession] = useState(false)
  const [timeline, setTimeline] = useState<AgentTimelineData>([])
  const [draft, setDraft] = useState('')
  const [model, setModel] = useState<string | null>(null)
  const [effort, setEffort] = useState<ReasoningEffort>('high')
  const [permissionMode, setPermissionMode] = useState<PermissionMode>(loadPermissionMode)
  const [confirmFullAccess, setConfirmFullAccess] = useState(false)
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [attachmentError, setAttachmentError] = useState<string | null>(null)
  const [running, setRunning] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const eventAbort = useRef<AbortController | null>(null)
  // A just-created session usually has no transcript persisted on the runner yet;
  // skip the reload that follows so the optimistically appended timeline survives.
  const skipTranscriptLoadFor = useRef<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  const sessions = sessionsQuery.data ?? []
  // Stay on the new-chat screen by default; only an explicit selection enters a session.
  const selectedId = newSession ? null : activeId
  const hasMessages = timeline.length > 0

  const gatewayModels = useMemo<GatewayModel[]>(() => {
    const data = availableModelsQuery.data
    if (!data) return []
    return data.availableSites
      .filter((site) => site.enabled)
      .filter((site) => !data.allowedSites.length || data.allowedSites.includes(site.site_id))
      .flatMap((site) => site.models
        .filter((item) => !data.allowedModels.length || data.allowedModels.includes(item.id))
        .filter((item) => !NON_CHAT_CATEGORIES.has(item.category ?? ''))
        .map((item) => ({
          id: item.model_key || item.upstream_model_name,
          displayName: item.display_name || item.model_key || item.upstream_model_name,
          category: item.category ?? 'chat',
          endpointTypes: [],
        })))
  }, [availableModelsQuery.data])

  const effectiveModel = useMemo(() => {
    const available = (id: string | null | undefined) => Boolean(id && gatewayModels.some((item) => item.id === id))
    // Manual pick > session last used > global last used > first available.
    if (available(model)) return model
    const remembered = selectedId ? modelMemoryQuery.data?.sessions[selectedId] : undefined
    if (available(remembered)) return remembered ?? null
    if (available(modelMemoryQuery.data?.default_model)) return modelMemoryQuery.data?.default_model ?? null
    return gatewayModels[0]?.id ?? null
  }, [gatewayModels, model, modelMemoryQuery.data, selectedId])

  const followSession = useCallback(async (sessionId: string) => {
    eventAbort.current?.abort()
    const controller = new AbortController()
    eventAbort.current = controller
    try {
      await followAgentEvents(sessionId, controller.signal, (event) => {
        setTimeline((current) => reduceAgentEvent(current, event))
        if (['agent_done', 'agent_error', 'agent_cancelled'].includes(event.type)) {
          setRunning(false)
          void queryClient.invalidateQueries({ queryKey: ['agent', 'sessions'] })
          // On terminal status, reconcile the timeline against the authoritative
          // transcript (covers events missed before the SSE connection); keep the
          // current timeline when the transcript lags behind.
          void fetchAgentTranscript(sessionId).then((entries) => {
            setTimeline((current) => {
              const fromTranscript = timelineFromTranscript(entries)
              return fromTranscript.length >= current.length ? fromTranscript : current
            })
          }).catch(() => undefined)
        }
      })
    } catch {
      if (!controller.signal.aborted) setRunning(false)
    }
  }, [queryClient])

  useEffect(() => {
    if (!selectedId) return
    if (skipTranscriptLoadFor.current) {
      const skip = skipTranscriptLoadFor.current === selectedId
      skipTranscriptLoadFor.current = null
      if (skip) return
    }
    let cancelled = false
    void fetchAgentTranscript(selectedId).then((entries) => {
      if (cancelled) return
      const built = timelineFromTranscript(entries)
      // Selecting a still-running session resumes the SSE follow. The event
      // registry is per-run, so reconnecting replays the current run from its
      // start; trim the timeline to the latest user message to avoid duplicating
      // what the transcript already persisted (user messages are not in the
      // event stream, so that entry is kept).
      const isRunning = sessions.find((item) => item.session_id === selectedId)?.running
      if (isRunning) {
        let lastUserIndex = -1
        built.forEach((item, index) => { if (item.kind === 'user') lastUserIndex = index })
        setTimeline(lastUserIndex >= 0 ? built.slice(0, lastUserIndex + 1) : [])
        setRunning(true)
        void followSession(selectedId)
        return
      }
      setTimeline(built)
    }).catch(() => setTimeline([]))
    return () => { cancelled = true }
  }, [selectedId, followSession]) // eslint-disable-line react-hooks/exhaustive-deps -- sessions only gates the running check; following is idempotent

  useEffect(() => () => eventAbort.current?.abort(), [])

  const sendMutation = useMutation({
    mutationFn: (input: { content: string; files: ChatAttachment[] }) => createAgentSession({
      content: input.content,
      model: effectiveModel ?? undefined,
      session_id: selectedId ?? undefined,
      reasoning_effort: normalizeReasoningEffort(gatewayModels.find((item) => item.id === effectiveModel) ?? null, effort),
      permission_mode: permissionMode,
      attachments: input.files.length
        ? input.files.map((file) => ({ name: file.name, mime_type: file.mimeType, data_url: file.dataURL }))
        : undefined,
    }),
    onSuccess: async ({ session_id }) => {
      skipTranscriptLoadFor.current = session_id
      setActiveId(session_id)
      setNewSession(false)
      setRunning(true)
      await queryClient.invalidateQueries({ queryKey: ['agent', 'sessions'] })
      void followSession(session_id)
    },
    onError: (error) => {
      setRunning(false)
      toast.error(t('agent:chat.sendFailed'), { description: error.message })
    },
  })

  const canSubmit = Boolean((draft.trim() || attachments.length > 0) && !running && !sendMutation.isPending)
  // Busy = session creation pending or a run in flight; drives the send/stop button state.
  const awaiting = sendMutation.isPending || running
  // Waiting indicator shows while busy and the last run has produced nothing yet.
  const lastItem = timeline[timeline.length - 1]
  const lastRunIdle = lastItem?.kind === 'run'
    && lastItem.run.status === 'running'
    && !lastItem.run.finalText
    && lastItem.run.steps.every((step) => step.status !== 'running')
  const waitingReply = awaiting && (!lastItem || lastItem.kind === 'user' || Boolean(lastRunIdle))

  useEffect(() => {
    const container = scrollRef.current
    if (container) container.scrollTop = container.scrollHeight
  }, [timeline, awaiting])

  function createNew() {
    eventAbort.current?.abort()
    skipTranscriptLoadFor.current = null
    setActiveId(null)
    setNewSession(true)
    setTimeline([])
    setDraft('')
    setAttachments([])
    setRunning(false)
    setModel(null) // fall back to the globally last-used model from server memory
  }

  function handleSelect(sessionId: string) {
    eventAbort.current?.abort()
    skipTranscriptLoadFor.current = null
    setRunning(false)
    setNewSession(false)
    setActiveId(sessionId)
    setModel(null) // fall back to the session's last-used model
  }

  function handleDelete(sessionId: string) {
    void deleteAgentSession(sessionId).catch(() => undefined)
    void queryClient.invalidateQueries({ queryKey: ['agent', 'sessions'] })
    if (sessionId === selectedId) createNew()
  }

  function submit() {
    const content = draft.trim() || attachments[0]?.name || ''
    if (!canSubmit || !content) return
    setTimeline((current) => appendUserMessage(current, content))
    const files = attachments
    setDraft('')
    setAttachments([])
    setAttachmentError(null)
    sendMutation.mutate({ content, files })
  }

  function handlePermissionDecision(request: AgentPermissionRequest, decision: 'allow' | 'deny') {
    if (!selectedId || !request.resolvedPath) return
    setTimeline((current) => current.map((item) => {
      if (item.kind !== 'run') return item
      return {
        ...item,
        run: {
          ...item.run,
          permissions: item.run.permissions.map((entry) => entry.id === request.id
            ? { ...entry, decision: decision === 'allow' ? 'allowed' as const : 'denied' as const }
            : entry),
        },
      }
    }))
    void grantAgentAccess(selectedId, {
      escalation_id: request.id,
      granted: decision === 'allow',
      resolved_path: request.resolvedPath,
    }).then(() => {
      // Allow/deny both resume the run agent-side; follow the new run's event
      // stream (the registry is per-run, so only the new run replays).
      setRunning(true)
      void followSession(selectedId)
    }).catch((error: unknown) => {
      toast.error(t('agent:permission.failed'), { description: error instanceof Error ? error.message : String(error) })
    })
  }

  function applyPermissionMode(next: PermissionMode) {
    setPermissionMode(next)
    try {
      window.localStorage.setItem(PERMISSION_MODE_KEY, next)
    } catch { /* ignore */ }
  }

  function togglePermissionMode() {
    if (permissionMode === 'ask') {
      // Full access requires an explicit confirmation.
      setConfirmFullAccess(true)
      return
    }
    applyPermissionMode('ask')
  }

  const addAttachments = async (files: FileList | File[] | null) => {
    if (!files) return
    const remaining = MAX_ATTACHMENTS - attachments.length
    const selected = Array.from(files).slice(0, Math.max(remaining, 0))
    if (selected.length === 0) {
      setAttachmentError(t('playground:chat.attachmentLimit', { count: MAX_ATTACHMENTS }))
      return
    }
    const oversized = selected.find((file) => file.size > MAX_ATTACHMENT_BYTES)
    if (oversized) {
      setAttachmentError(t('playground:chat.attachmentTooLarge', { name: oversized.name }))
      return
    }
    try {
      const next = await Promise.all(selected.map(async (file): Promise<ChatAttachment> => ({
        id: newId(),
        name: file.name,
        mimeType: file.type || 'application/octet-stream',
        size: file.size,
        dataURL: await fileToDataURL(file),
      })))
      setAttachments((current) => [...current, ...next].slice(0, MAX_ATTACHMENTS))
      setAttachmentError(files.length > remaining ? t('playground:chat.attachmentLimit', { count: MAX_ATTACHMENTS }) : null)
    } catch {
      setAttachmentError(t('playground:chat.attachmentReadFailed'))
    }
  }

  const composer = (
    <Composer
      value={draft}
      onChange={setDraft}
      onSubmit={submit}
      onStop={() => {
        if (selectedId) {
          void stopAgentSession(selectedId)
          setRunning(false)
        }
      }}
      streaming={awaiting}
      canSubmit={canSubmit}
      placeholder={t('agent:chat.inputPlaceholder')}
      onPasteFiles={(files) => {
        if (running) return false
        void addAttachments(files)
        return true
      }}
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
            disabled={running || attachments.length >= MAX_ATTACHMENTS}
            className="flex h-11 w-11 items-center justify-center rounded-full text-muted-soft transition-colors active:bg-[hsl(var(--surface-subtle))] active:text-foreground disabled:cursor-not-allowed disabled:opacity-40 md:h-8 md:w-8 md:hover:bg-[hsl(var(--surface-subtle))] md:hover:text-foreground"
            aria-label={t('playground:chat.attach')}
            title={t('playground:chat.attach')}
          >
            <Paperclip className="h-4 w-4" />
          </button>
          <button
            type="button"
            onClick={togglePermissionMode}
            className="flex h-11 items-center gap-1.5 rounded-full px-2.5 text-xs text-muted-soft transition-colors active:bg-[hsl(var(--surface-subtle))] active:text-foreground md:h-8 md:hover:bg-[hsl(var(--surface-subtle))] md:hover:text-foreground"
            aria-label={t('agent:composer.permissionMode')}
            title={permissionMode === 'ask' ? t('agent:composer.permissionAskHint') : t('agent:composer.permissionFullHint')}
          >
            {permissionMode === 'ask' ? <ShieldQuestion className="h-4 w-4" /> : <ShieldCheck className="h-4 w-4" />}
            <span className="hidden sm:inline">{permissionMode === 'ask' ? t('agent:composer.permissionAsk') : t('agent:composer.permissionFull')}</span>
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
                  removeLabel={t('playground:chat.removeAttachment')}
                  onRemove={() => setAttachments((current) => current.filter((item) => item.id !== attachment.id))}
                />
              ))}
            </div>
          ) : null}
          {attachmentError ? <p className="px-1 text-xs text-red-500">{attachmentError}</p> : null}
        </div>
      ) : undefined}
      trailingControls={
        <ModelReasoningPicker
          models={gatewayModels}
          model={effectiveModel}
          onModelChange={(id) => {
            setModel(id)
            setEffort((current) => normalizeReasoningEffort(gatewayModels.find((item) => item.id === id) ?? null, current))
          }}
          effort={effort}
          onEffortChange={setEffort}
          disabled={gatewayModels.length === 0}
        />
      }
    />
  )

  return (
    <div className="flex h-full min-h-0 overflow-hidden">
      <FullAccessConfirmDialog
        open={confirmFullAccess}
        onCancel={() => setConfirmFullAccess(false)}
        onConfirm={() => {
          applyPermissionMode('full')
          setConfirmFullAccess(false)
        }}
      />
      <aside className="my-4 ml-4 hidden h-[calc(100vh-2rem)] w-[280px] shrink-0 overflow-hidden rounded-[24px] border border-[hsl(var(--glass-border))] shadow-[0_24px_60px_rgba(0,0,0,0.12)] md:block">
        <AgentSidebar
          sessions={sessions}
          activeId={selectedId}
          onBack={() => navigate(-1)}
          onSelect={handleSelect}
          onNew={createNew}
          onDelete={handleDelete}
          onOpenSettings={() => setSettingsOpen(true)}
        />
      </aside>
      <AgentSettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />

      <div className="relative flex min-w-0 flex-1 flex-col">
        <div className="absolute right-5 top-5 z-20">
          <TopbarUserControls />
        </div>
        {hasMessages ? (
          <>
            <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
              <div className="mx-auto max-w-3xl px-4 pb-6 pt-16">
                <AgentTimeline items={timeline} onPermissionDecision={handlePermissionDecision} />
                {waitingReply ? <WaitingIndicator /> : null}
              </div>
            </div>
            <div className="shrink-0 px-4 pb-4">
              <div className="mx-auto max-w-3xl">
                {composer}
                <p className="mt-2 text-center text-xs text-faint">{t('agent:chat.hint')}</p>
              </div>
            </div>
          </>
        ) : (
          <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-4">
            <div className="w-full max-w-2xl">
              <div className="mb-6 flex h-20 items-center justify-center text-center">
                <h2 className="text-2xl font-semibold text-foreground">{t('agent:chat.emptyTitle')}</h2>
              </div>
              {composer}
              <p className="mt-3 text-center text-xs text-faint">{t('agent:chat.hint')}</p>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
