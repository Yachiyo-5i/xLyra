import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link, useNavigate } from 'react-router-dom'
import { Folder, Globe, Menu, Paperclip, ShieldCheck, ShieldQuestion, TriangleAlert, TerminalSquare } from 'lucide-react'
import { ThinkingOrb, type OrbState } from 'thinking-orbs'
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
import { Button } from '@/components/ui/button'
import { AppLogo } from '@/components/common/app-logo'
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AgentLiquidGlassPanel } from '@/features/agent/components/liquid-glass/agent-liquid-glass'
import './liquid-glass/agent-liquid-glass.css'
import {
  appendUserMessage,
  reconcileTimeline,
  replaceFromUserMessage,
  reduceAgentEvent,
  timelineFromTranscript,
  type AgentPermissionRequest,
  type AgentRun,
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
  retryAgentSession,
  stopAgentSession,
  type AgentAppearanceSettings,
} from '@/features/agent/api/agent'
import { agentSettingsKey } from '@/features/agent/lib/use-agent-availability'
import { toast } from '@/lib/toast'

const MAX_ATTACHMENTS = 4
const MAX_ATTACHMENT_BYTES = 5 * 1024 * 1024
const NON_CHAT_CATEGORIES = new Set(['image', 'audio', 'embedding'])
const PERMISSION_MODE_KEY = 'xlyra-agent-permission-mode'

type PermissionMode = 'ask' | 'full'

type EditReplaySnapshot = {
  timeline: AgentTimelineData
  draft: string
  attachments: ChatAttachment[]
  attachmentError: string | null
  editingMessage: { messageId: string } | null
}

function loadPermissionMode(): PermissionMode {
  try {
    return window.localStorage.getItem(PERMISSION_MODE_KEY) === 'full' ? 'full' : 'ask'
  } catch {
    return 'ask'
  }
}

/**
 * 外观设置 → 玻璃着色器参数。bright（空会话，背景图可见）与 dark（会话中，
 * 纯黑背景）共用同一组用户滑杆：dark 保留调好的光学常数，但模糊/压暗/厚度/
 * 明暗/透明度仍由滑杆驱动，默认值与原手调的 dark 常数基本一致
 * （blur 0.275↔0.3、darkTint 0.218↔0.22、depth 31.8↔32、opacity 1↔1）。
 */
function glassSettingsForAppearance(appearance: AgentAppearanceSettings, dark: boolean) {
  const clamped = (value: number) => Math.max(0, Math.min(100, value))
  const transparency = clamped(appearance.side_transparency) / 100
  const brightness = (clamped(appearance.side_brightness) - 50) / 500
  const blur = 0.22 + clamped(appearance.backdrop_blur) / 100 * 0.42
  const darkTint = 0.06 + clamped(appearance.backdrop_dim) / 100 * 0.2
  const depth = 20 + clamped(appearance.side_thickness) * 0.42
  return {
    blur,
    refraction: dark ? 0.88 : 0.8,
    chromaticAberration: dark ? 0.065 : 0.06,
    darkTint: dark ? darkTint + 0.02 : darkTint,
    brightness,
    saturation: dark ? 0.06 : 0.04,
    tintStrength: dark ? 0.08 : 0.14,
    edgeHighlight: dark ? 0.16 : 0.2,
    specular: 0.2,
    fresnel: dark ? 1.18 : 1.08,
    bevel: 0,
    depth,
    opacity: dark
      ? Math.min(1, 0.94 + (1 - transparency) * 0.12)
      : 0.82 + (1 - transparency) * 0.12,
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
function orbStateForRun(run?: AgentRun): OrbState {
  const activeStep = run ? [...run.steps].reverse().find((step) => step.status === 'running') : undefined
  if (!activeStep || activeStep.kind === 'thinking') return 'breathing'
  if (activeStep.kind !== 'tool') return 'working'
  const name = activeStep.title.toLowerCase()
  if (name.includes('search') || name.includes('grep') || name.includes('find') || name.includes('query')) return 'searching'
  if (name.includes('fetch') || name.includes('http') || name.includes('web') || name.includes('browse') || name.includes('url') || name.includes('net')) return 'connecting'
  return 'working'
}

function WaitingIndicator({ startedAt, elapsedMs = 0, state }: { startedAt: number; elapsedMs?: number; state: OrbState }) {
  const { t } = useTranslation(['agent', 'playground'])
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 100)
    return () => window.clearInterval(timer)
  }, [startedAt])
  const elapsed = Math.max(0, elapsedMs + now - startedAt)
  const statusLabel = state === 'breathing'
    ? t('agent:work.thinking')
    : state === 'searching'
      ? t('agent:work.searching')
      : state === 'connecting'
        ? t('agent:work.connecting')
        : t('agent:work.working')
  return (
    <div className="mt-4 flex items-center gap-2 text-xs text-faint">
      <ThinkingOrb state={state} size={20} theme="dark" aria-label={statusLabel} className="shrink-0" />
      <span>{statusLabel}</span>
      <span className="tabular-nums">{t('playground:chat.elapsedDuration', { duration: formatResponseDuration(elapsed) })}</span>
    </div>
  )
}

/** Confirmation shown before enabling full-access permission mode. */
function FullAccessConfirmDialog({
  open,
  onCancel,
  onConfirm,
  backgroundImage,
  dark,
}: {
  open: boolean
  onCancel: () => void
  onConfirm: () => void
  backgroundImage: string
  /** 会话中为 true：与页面一致渲染深色玻璃（背景 URL 里没有可嗅探的标记，必须显式传） */
  dark: boolean
}) {
  const { t } = useTranslation('agent')
  const items = [
    { icon: Folder, title: t('composer.fullConfirmFiles'), description: t('composer.fullConfirmFilesDesc') },
    { icon: TerminalSquare, title: t('composer.fullConfirmCommands'), description: t('composer.fullConfirmCommandsDesc') },
    { icon: Globe, title: t('composer.fullConfirmNetwork'), description: t('composer.fullConfirmNetworkDesc') },
  ]
  const dialogBody = (
    <>
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
    </>
  )

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onCancel() }}>
      <DialogContent className="agent-liquid-access-dialog-host w-[min(92vw,480px)]">
        <AgentLiquidGlassPanel
          backgroundImage={backgroundImage}
          variant={dark ? 'dark' : 'frosted'}
          sampleBackground={!dark}
          className="agent-liquid-access-dialog"
          contentClassName="agent-liquid-access-dialog__content"
          settings={{
            blur: dark ? 0.18 : 0.34,
            refraction: dark ? 0.72 : 0.38,
            chromaticAberration: dark ? 0.045 : 0.025,
            darkTint: dark ? 0.18 : 0.12,
            tintStrength: dark ? 0.06 : 0.1,
            edgeHighlight: dark ? 0.08 : 0.1,
            specular: dark ? 0.14 : 0.12,
            fresnel: dark ? 1.08 : 0.9,
            bevel: 0,
            depth: 32,
            radius: 26,
            opacity: dark ? 1 : 0.96,
          }}
        >
          {dialogBody}
        </AgentLiquidGlassPanel>
      </DialogContent>
    </Dialog>
  )
}

function EditReplayConfirmDialog({
  open,
  onCancel,
  onConfirm,
}: {
  open: boolean
  onCancel: () => void
  onConfirm: () => void
}) {
  const { t } = useTranslation('agent')
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => { if (!nextOpen) onCancel() }}>
      <DialogContent className="w-[min(92vw,440px)]">
        <DialogHeader>
          <DialogTitle>{t('chat.editReplayTitle')}</DialogTitle>
          <DialogDescription>{t('chat.editReplayDescription')}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>{t('chat.editReplayCancel')}</Button>
          <Button className="border-white bg-white text-black hover:bg-white/90 hover:text-black" onClick={onConfirm}>{t('chat.editReplayConfirm')}</Button>
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
  const runtimeSettingsQuery = useQuery({ queryKey: [...agentSettingsKey, 'runtime'], queryFn: fetchAgentRuntimeSettings, retry: false })

  const [activeId, setActiveId] = useState<string | null>(null)
  const [newSession, setNewSession] = useState(false)
  const [timeline, setTimeline] = useState<AgentTimelineData>([])
  const [draft, setDraft] = useState('')
  const [editingMessage, setEditingMessage] = useState<{ messageId: string } | null>(null)
  const [editReplayConfirmation, setEditReplayConfirmation] = useState<{ messageId: string; content: string } | null>(null)
  const [waitingFallbackStartedAt] = useState(() => Date.now())
  const [model, setModel] = useState<string | null>(null)
  const [effort, setEffort] = useState<ReasoningEffort>('high')
  const [permissionMode, setPermissionMode] = useState<PermissionMode>(loadPermissionMode)
  const [confirmFullAccess, setConfirmFullAccess] = useState(false)
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [attachmentError, setAttachmentError] = useState<string | null>(null)
  const [running, setRunning] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false)
  const eventAbort = useRef<AbortController | null>(null)
  const retryTimelineBeforeEdit = useRef<EditReplaySnapshot | null>(null)
  // A just-created session usually has no transcript persisted on the runner yet;
  // skip the reload that follows so the optimistically appended timeline survives.
  const skipTranscriptLoadFor = useRef<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  const sessions = sessionsQuery.data ?? []
  // Stay on the new-chat screen by default; only an explicit selection enters a session.
  const selectedId = newSession ? null : activeId
  const activeSession = selectedId ? sessions.find((session) => session.session_id === selectedId) : undefined
  const hasMessages = timeline.length > 0
  const appearance = runtimeSettingsQuery.data?.appearance
  const configuredBackground = appearance?.background_image || '/agent-backdrop.png'
  const agentBackgroundImage = configuredBackground
  const sidebarGlassSettings = glassSettingsForAppearance(appearance ?? {
    background_image: '/agent-backdrop.png',
    custom_background_images: [],
    side_transparency: 49,
    side_brightness: 32,
    side_thickness: 28,
    backdrop_blur: 13,
    backdrop_dim: 69,
  }, hasMessages)

  const gatewayModels = useMemo<GatewayModel[]>(() => {
    const data = availableModelsQuery.data
    if (!data) return []
    const models = new Map<string, GatewayModel>()
    for (const site of data.availableSites) {
      if (!site.enabled || (data.allowedSites.length > 0 && !data.allowedSites.includes(site.site_id))) continue
      for (const item of site.models) {
        if ((data.allowedModels.length > 0 && !data.allowedModels.includes(item.id)) || NON_CHAT_CATEGORIES.has(item.category ?? '')) continue
        const id = (item.model_key || item.upstream_model_name).trim()
        if (!id) continue
        if (!models.has(id)) {
          models.set(id, {
            id,
            displayName: item.display_name || id,
            category: item.category ?? 'chat',
            endpointTypes: [],
          })
        }
      }
    }
    return [...models.values()].sort((left, right) => left.id.localeCompare(right.id, undefined, { sensitivity: 'base' }))
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
              return reconcileTimeline(current, fromTranscript)
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
    onSuccess: async ({ session_id, message_id }) => {
      skipTranscriptLoadFor.current = session_id
      setActiveId(session_id)
      setNewSession(false)
      setRunning(true)
      if (message_id) {
        setTimeline((current) => {
          const next = current.slice()
          for (let index = next.length - 1; index >= 0; index -= 1) {
            const item = next[index]
            if (item?.kind !== 'user') continue
            next[index] = { ...item, id: message_id, messageId: message_id }
            break
          }
          return next
        })
      }
      await queryClient.invalidateQueries({ queryKey: ['agent', 'sessions'] })
      void followSession(session_id)
    },
    onError: (error) => {
      setRunning(false)
      toast.error(t('agent:chat.sendFailed'), { description: error.message })
    },
  })

  const retryMutation = useMutation({
    mutationFn: (input: { messageId: string; content: string }) => {
      if (!selectedId) throw new Error('会话不存在')
      return retryAgentSession(selectedId, {
        message_id: input.messageId,
        content: input.content,
        model: effectiveModel ?? undefined,
        reasoning_effort: normalizeReasoningEffort(gatewayModels.find((item) => item.id === effectiveModel) ?? null, effort),
        permission_mode: permissionMode,
      })
    },
    onSuccess: async ({ session_id, message_id }, variables) => {
      retryTimelineBeforeEdit.current = null
      setEditingMessage(null)
      setRunning(true)
      if (message_id) {
        setTimeline((current) => current.map((item) => item.kind === 'user' && (item.messageId === variables.messageId || item.id === variables.messageId)
          ? { ...item, id: message_id, messageId: message_id }
          : item))
      }
      await queryClient.invalidateQueries({ queryKey: ['agent', 'sessions'] })
      void followSession(session_id)
    },
    onError: (error) => {
      if (retryTimelineBeforeEdit.current) {
        const snapshot = retryTimelineBeforeEdit.current
        setTimeline(snapshot.timeline)
        setDraft(snapshot.draft)
        setAttachments(snapshot.attachments)
        setAttachmentError(snapshot.attachmentError)
        setEditingMessage(snapshot.editingMessage)
        retryTimelineBeforeEdit.current = null
      }
      setRunning(false)
      toast.error(t('agent:chat.sendFailed'), { description: error.message })
    },
  })

  const canSubmit = Boolean((draft.trim() || attachments.length > 0) && !running && !sendMutation.isPending && !retryMutation.isPending)
  // Busy = session creation pending or a run in flight; drives the send/stop button state.
  const awaiting = sendMutation.isPending || retryMutation.isPending || running
  // Waiting indicator shows while busy before the final response, including active tool calls.
  const lastItem = timeline[timeline.length - 1]
  const runNeedsIndicator = lastItem?.kind === 'run'
    && lastItem.run.status === 'running'
    && (!lastItem.run.finalText
      || lastItem.run.steps.some((step) => step.status === 'running')
      || lastItem.run.permissions.some((request) => request.decision !== undefined))
  const waitingReply = awaiting && (!lastItem || lastItem.kind === 'user' || Boolean(runNeedsIndicator))
  const waitingStartedAt = lastItem?.kind === 'run' ? lastItem.run.startedAt : lastItem?.kind === 'user' ? lastItem.createdAt : waitingFallbackStartedAt
  const waitingElapsedMs = lastItem?.kind === 'run' ? lastItem.run.elapsedMs : undefined
  const waitingOrbState = lastItem?.kind === 'run' ? orbStateForRun(lastItem.run) : 'breathing'

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
    setEditingMessage(null)
    setEditReplayConfirmation(null)
    retryTimelineBeforeEdit.current = null
    setAttachments([])
    setRunning(false)
    setModel(null) // fall back to the globally last-used model from server memory
  }

  function handleSelect(sessionId: string) {
    eventAbort.current?.abort()
    skipTranscriptLoadFor.current = null
    setRunning(false)
    setEditingMessage(null)
    setEditReplayConfirmation(null)
    retryTimelineBeforeEdit.current = null
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
    if (editingMessage && selectedId) {
      setEditReplayConfirmation({ messageId: editingMessage.messageId, content })
      return
    }
    setTimeline((current) => appendUserMessage(current, content))
    const files = attachments
    setDraft('')
    setAttachments([])
    setAttachmentError(null)
    sendMutation.mutate({ content, files })
  }

  function confirmEditReplay() {
    if (!editReplayConfirmation || !selectedId) return
    const { messageId, content } = editReplayConfirmation
    retryTimelineBeforeEdit.current = {
      timeline,
      draft,
      attachments,
      attachmentError,
      editingMessage,
    }
    setTimeline((current) => replaceFromUserMessage(current, messageId, content))
    setEditReplayConfirmation(null)
    setEditingMessage(null)
    setDraft('')
    setAttachments([])
    setAttachmentError(null)
    retryMutation.mutate({ messageId, content })
  }

  function handleUserEdit(messageId: string, text: string) {
    if (awaiting) return
    setEditingMessage({ messageId })
    setDraft(text)
    setAttachments([])
    setAttachmentError(null)
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

  const renderPickerSurface = useCallback((children: React.ReactNode, kind: 'panel' | 'subpanel') => (
    <AgentLiquidGlassPanel
      backgroundImage={agentBackgroundImage}
      variant="dark"
      sampleBackground={hasMessages ? false : 0.82}
      flat={hasMessages}
      className={kind === 'panel' ? 'agent-liquid-picker-surface' : 'agent-liquid-picker-surface agent-liquid-picker-surface--sub'}
      contentClassName="agent-liquid-picker-surface__content"
      settings={{ ...sidebarGlassSettings, radius: 14, depth: Math.min(sidebarGlassSettings.depth, 28) }}
    >
      {children}
    </AgentLiquidGlassPanel>
  ), [agentBackgroundImage, hasMessages, sidebarGlassSettings])

  // 会话删除菜单等小型弹层：与模型选择器共用玻璃表面模式（空会话采样背景、会话中 flat）
  const renderMenuSurface = useCallback((children: React.ReactNode) => (
    <AgentLiquidGlassPanel
      backgroundImage={agentBackgroundImage}
      variant="dark"
      sampleBackground={hasMessages ? false : 0.82}
      flat={hasMessages}
      className="agent-liquid-picker-surface agent-liquid-picker-surface--menu"
      contentClassName="agent-liquid-picker-surface__content"
      settings={{ ...sidebarGlassSettings, radius: 14, depth: Math.min(sidebarGlassSettings.depth, 28) }}
    >
      {children}
    </AgentLiquidGlassPanel>
  ), [agentBackgroundImage, hasMessages, sidebarGlassSettings])

  const composerContent = (
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
      stopButtonClassName="agent-stop-button bg-white text-black hover:bg-white/90"
      stopIconClassName="text-black"
      placeholder={t('agent:chat.inputPlaceholder')}
      onPasteFiles={(files) => {
        if (running || editingMessage) return false
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
            disabled={running || Boolean(editingMessage) || attachments.length >= MAX_ATTACHMENTS}
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
          panelClassName="agent-liquid-picker-host"
          subPanelClassName="agent-liquid-picker-subpanel"
          panelRenderer={renderPickerSurface}
          modelIconTransparent
        />
      }
    />
  )

  const composer = (
    <AgentLiquidGlassPanel
      backgroundImage={agentBackgroundImage}
      variant="dark"
      // 会话中页面背景是纯黑（canvas 纹理却始终是背景图），继续采样会让图片
      // 从玻璃里透出来；与侧栏一致改为纯中性深色玻璃
      sampleBackground={hasMessages ? false : 1}
      className="agent-liquid-composer"
      contentClassName="agent-liquid-composer__content"
      settings={{ ...sidebarGlassSettings, radius: 26, depth: Math.min(sidebarGlassSettings.depth, 30) }}
    >
      {composerContent}
    </AgentLiquidGlassPanel>
  )

  const sidebarContent = <AgentSidebar
    sessions={sessions}
    activeId={selectedId}
    menuRenderer={renderMenuSurface}
    onBack={() => navigate(-1)}
    onSelect={handleSelect}
    onNew={createNew}
    onDelete={handleDelete}
    onOpenSettings={() => setSettingsOpen(true)}
  />

  return (
    <div className="agent-liquid-shell agent-page" data-agent-visual={hasMessages ? 'dark' : 'bright'}>
      <AgentMobileHeader
        title={selectedId ? (activeSession?.title || activeSession?.preview || t('sidebar.untitled')) : undefined}
        onOpenMenu={() => setMobileSidebarOpen(true)}
      />
      <AgentMobileDrawer
        open={mobileSidebarOpen}
        backgroundImage={agentBackgroundImage}
        hasMessages={hasMessages}
        glassSettings={sidebarGlassSettings}
        onClose={() => setMobileSidebarOpen(false)}
      >
        <AgentSidebar
          className="agent-mobile-sidebar"
          sessions={sessions}
          activeId={selectedId}
          menuRenderer={renderMenuSurface}
          onBack={() => {
            setMobileSidebarOpen(false)
            navigate(-1)
          }}
          onSelect={(id) => {
            setMobileSidebarOpen(false)
            handleSelect(id)
          }}
          onNew={() => {
            setMobileSidebarOpen(false)
            createNew()
          }}
          onDelete={handleDelete}
          onOpenSettings={() => {
            setMobileSidebarOpen(false)
            setSettingsOpen(true)
          }}
        />
      </AgentMobileDrawer>
      <div
          className={hasMessages ? 'agent-liquid-backdrop agent-liquid-backdrop--plain' : 'agent-liquid-backdrop'}
          style={!hasMessages ? { backgroundImage: `url("${configuredBackground}")` } : undefined}
          data-liquid-glass-background="true"
          aria-hidden="true"
        />
      <div className="relative z-10 flex h-full min-h-0 overflow-hidden">
      <FullAccessConfirmDialog
        open={confirmFullAccess}
        backgroundImage={agentBackgroundImage}
        dark={hasMessages}
        onCancel={() => setConfirmFullAccess(false)}
        onConfirm={() => {
          applyPermissionMode('full')
          setConfirmFullAccess(false)
        }}
      />
      <EditReplayConfirmDialog
        open={Boolean(editReplayConfirmation)}
        onCancel={() => setEditReplayConfirmation(null)}
        onConfirm={confirmEditReplay}
      />
      <aside className="my-4 ml-4 hidden h-[calc(100vh-2rem)] w-[280px] shrink-0 overflow-hidden md:block">
        <AgentLiquidGlassPanel
          backgroundImage={agentBackgroundImage}
          variant={hasMessages ? 'dark' : 'frosted'}
          sampleBackground={hasMessages ? false : 0.72}
          className="agent-liquid-sidebar"
          contentClassName="agent-liquid-sidebar__content"
          settings={{ ...sidebarGlassSettings, radius: 24 }}
        >
          {sidebarContent}
        </AgentLiquidGlassPanel>
      </aside>
      <AgentSettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} backgroundImage={agentBackgroundImage} dark={hasMessages} />

      <div className="relative flex min-w-0 flex-1 flex-col">
        {hasMessages ? (
          <>
            <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto">
              <div className="mx-auto max-w-3xl px-4 pb-6 pt-16">
                <AgentTimeline items={timeline} onPermissionDecision={handlePermissionDecision} onUserEdit={handleUserEdit} />
                {waitingReply ? <WaitingIndicator startedAt={waitingStartedAt} elapsedMs={waitingElapsedMs} state={waitingOrbState} /> : null}
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
    </div>
  )
}

function AgentMobileHeader({ title, onOpenMenu }: { title?: string; onOpenMenu: () => void }) {
  const { t } = useTranslation('agent')

  return (
    <header className="agent-mobile-header mobile-safe-top" style={{ '--mobile-safe-top-extra': '0.75rem' } as React.CSSProperties}>
      <div className="flex items-center gap-2">
        <Button
          variant="ghost"
          size="icon"
          className="agent-mobile-header__button"
          aria-label={t('sidebar.menu')}
          onClick={onOpenMenu}
        >
          <Menu className="size-5" />
        </Button>
        {title ? (
          <p className="agent-mobile-header__title">{title}</p>
        ) : (
          <Link to="/dashboard" aria-label={t('header.back')} className="agent-mobile-header__logo">
            <AppLogo className="size-full !bg-transparent" decorative />
          </Link>
        )}
      </div>
    </header>
  )
}

function AgentMobileDrawer({
  open,
  backgroundImage,
  hasMessages,
  glassSettings,
  onClose,
  children,
}: {
  open: boolean
  backgroundImage: string
  hasMessages: boolean
  glassSettings: ReturnType<typeof glassSettingsForAppearance>
  onClose: () => void
  children: React.ReactNode
}) {
  const { t } = useTranslation('agent')

  useEffect(() => {
    if (!open) return
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [onClose, open])

  if (!open) return null

  return (
    <>
      <button
        type="button"
        aria-label={t('settings.close')}
        className="agent-mobile-drawer__overlay"
        onClick={onClose}
      />
      <aside className="agent-mobile-drawer" aria-label={t('sidebar.recent')}>
        <AgentLiquidGlassPanel
          backgroundImage={backgroundImage}
          variant={hasMessages ? 'dark' : 'frosted'}
          sampleBackground={hasMessages ? false : 0.72}
          className="agent-liquid-sidebar agent-mobile-drawer__panel"
          contentClassName="agent-liquid-sidebar__content"
          settings={{ ...glassSettings, radius: 24 }}
        >
          {children}
        </AgentLiquidGlassPanel>
      </aside>
    </>
  )
}
