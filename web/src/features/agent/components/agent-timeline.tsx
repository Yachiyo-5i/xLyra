import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Brain, Check, ChevronDown, ChevronRight, CircleCheck, CircleX, Copy, FilePenLine, FileText, Globe, Info, LoaderCircle, Pencil, Search, ShieldAlert, TerminalSquare, Wrench } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { PlaygroundMessageAction } from '@/features/playground/components/playground-message-actions'
import { MarkdownMessage } from '@/features/playground/components/markdown-message'
import { formatResponseDuration } from '@/features/playground/lib/response-timing'
import type { AgentPermissionRequest, AgentRun, AgentTimelineItem, AgentWorkStep } from '@/features/agent/lib/agent-events'

type AgentTimelineProps = {
  items: AgentTimelineItem[]
  onPermissionDecision?: (request: AgentPermissionRequest, decision: 'allow' | 'deny') => void
  onUserEdit?: (messageId: string, text: string) => void
}

export function AgentTimeline({ items, onPermissionDecision, onUserEdit }: AgentTimelineProps) {
  const lastUserIndex = items.reduce((index, item, current) => item.kind === 'user' ? current : index, -1)
  return (
    <div className="space-y-6">
      {items.map((item, index) => (
        item.kind === 'user'
          ? <UserMessage key={item.id} text={item.text} editable={index === lastUserIndex && Boolean(item.messageId) && Boolean(onUserEdit)} onEdit={item.messageId && onUserEdit ? () => onUserEdit(item.messageId!, item.text) : undefined} />
          : <RunBlock key={item.id} run={item.run} onPermissionDecision={onPermissionDecision} />
      ))}
    </div>
  )
}

function UserMessage({ text, editable, onEdit }: { text: string; editable?: boolean; onEdit?: () => void }) {
  const { t } = useTranslation(['agent', 'playground'])
  return (
    <div className="group flex flex-col items-end gap-1">
      <div className="agent-liquid-user-message max-w-[82%] whitespace-pre-wrap rounded-2xl px-4 py-3 text-sm leading-6">
        {text}
      </div>
      <div className="flex items-center gap-0.5 opacity-100 transition-opacity md:opacity-0 md:group-hover:opacity-100">
        <CopyMessageAction text={text} />
        {editable && onEdit ? (
          <PlaygroundMessageAction label={t('playground:actions.edit')} onClick={onEdit}>
            <Pencil className="h-3.5 w-3.5" />
          </PlaygroundMessageAction>
        ) : null}
      </div>
    </div>
  )
}

function CopyMessageAction({ text }: { text: string }) {
  const { t } = useTranslation('playground')
  const [copied, setCopied] = useState(false)
  const timerRef = useRef<number | null>(null)

  useEffect(() => () => {
    if (timerRef.current !== null) window.clearTimeout(timerRef.current)
  }, [])

  async function copy() {
    const success = await copyToClipboard(text, null, null)
    if (!success) return
    setCopied(true)
    if (timerRef.current !== null) window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => setCopied(false), 1_400)
  }

  return (
    <PlaygroundMessageAction label={copied ? t('actions.copied') : t('actions.copy')} onClick={() => void copy()}>
      {copied ? <Check className="h-3.5 w-3.5 text-emerald-500" /> : <Copy className="h-3.5 w-3.5" />}
    </PlaygroundMessageAction>
  )
}

function RunBlock({ run, onPermissionDecision }: { run: AgentRun; onPermissionDecision?: AgentTimelineProps['onPermissionDecision'] }) {
  const { t } = useTranslation('agent')
  const done = run.status !== 'running'
  const pausedForPermission = run.status === 'cancelled' && run.permissions.length > 0
  const [expanded, setExpanded] = useState(false)
  const showSteps = run.steps.length > 0
  const stepsVisible = showSteps && expanded
  const errorCount = run.steps.filter((step) => step.status === 'error').length
  const activeStep = [...run.steps].reverse().find((step) => step.status === 'running') ?? run.steps[run.steps.length - 1]
  const statusLabel = !done && activeStep?.kind === 'tool' && activeStep.status === 'running'
    ? t(activeStep.argsDone === false ? 'work.preparingTool' : 'work.runningTool', { tool: activeStep.title })
    : !done && activeStep?.kind === 'thinking'
      ? t('work.thinking')
      : done
        ? t('work.summary', { count: run.steps.length })
        : t('work.running', { count: run.steps.length })

  return (
    <div className="space-y-3">
      {showSteps ? (
        <div>
          <button
            type="button"
            onClick={() => setExpanded((value) => !value)}
            className="group flex w-full items-center gap-1.5 py-0.5 text-left text-[11px] leading-5 text-muted-soft transition-colors hover:text-foreground"
          >
            {done ? null : (
              <LoaderCircle className="h-3.5 w-3.5 shrink-0 animate-spin" />
            )}
            <span className="min-w-0 truncate">{statusLabel}</span>
            <ChevronRight className={cn('h-3 w-3 shrink-0 transition-transform', stepsVisible && 'rotate-90')} />
            {errorCount > 0 ? (
              <span className="ml-auto shrink-0 text-red-500">{t('work.errors', { count: errorCount })}</span>
            ) : null}
          </button>
          {stepsVisible ? (
            <div className="mt-1.5 space-y-1 border-l-2 border-[hsl(var(--glass-divider))] pl-3">
              {run.steps.map((step) => (
                <StepRow key={step.id} step={step} />
              ))}
            </div>
          ) : null}
        </div>
      ) : null}

      {run.permissions.map((request) => (
        <PermissionCard key={request.id} request={request} onDecision={onPermissionDecision} />
      ))}

      {run.finalText ? (
        <>
          <MarkdownMessage content={run.finalText} className="text-[15px] leading-7" />
          {done && !pausedForPermission ? (
            <div className="flex items-center gap-2">
              <CopyMessageAction text={run.finalText} />
              {run.elapsedMs !== undefined ? (
                <span className="text-xs tabular-nums text-faint">
                  {t('playground:chat.responseDuration', { duration: formatResponseDuration(run.elapsedMs) })}
                </span>
              ) : null}
            </div>
          ) : null}
        </>
      ) : null}

      {!run.finalText && done && !pausedForPermission && run.elapsedMs !== undefined ? (
        <div className="text-xs tabular-nums text-faint">
          {t('playground:chat.responseDuration', { duration: formatResponseDuration(run.elapsedMs) })}
        </div>
      ) : null}

      {run.status === 'error' ? (
        <p className="text-xs text-red-500">{t('work.failed')}</p>
      ) : null}
      {/* A cancellation caused by a pending escalation is a pause, not a stop — hide the label to avoid misleading. */}
      {run.status === 'cancelled' && run.permissions.length === 0 ? (
        <p className="text-xs text-muted-soft">{t('work.cancelled')}</p>
      ) : null}
    </div>
  )
}

type ToolCategory = 'command' | 'fileEdit' | 'network' | 'fileRead' | 'search' | 'other'

/** Bucket work steps by tool name: command / file edit / network / file read / search / other. */
function toolCategory(name: string): ToolCategory {
  const n = name.toLowerCase()
  if (n.includes('exec') || n.includes('command') || n.includes('stdin') || n.includes('shell') || n.includes('bash') || n.includes('process')) return 'command'
  if (n.includes('create') || n.includes('edit') || n.includes('write') || n.includes('patch') || n.includes('delete') || n.includes('rename') || n.includes('move')) return 'fileEdit'
  if (n.includes('fetch') || n.includes('http') || n.includes('web') || n.includes('browse') || n.includes('url') || n.includes('net')) return 'network'
  if (n.includes('search') || n.includes('grep') || n.includes('find') || n.includes('query')) return 'search'
  if (n.includes('read') || n.includes('list') || n.includes('view') || n.includes('open') || n.includes('stat')) return 'fileRead'
  return 'other'
}

const CATEGORY_ICONS: Record<ToolCategory, typeof Wrench> = {
  command: TerminalSquare,
  fileEdit: FilePenLine,
  network: Globe,
  fileRead: FileText,
  search: Search,
  other: Wrench,
}

function StepRow({ step }: { step: AgentWorkStep }) {
  const { t } = useTranslation('agent')
  const [open, setOpen] = useState(false)
  const category = step.kind === 'tool' ? toolCategory(step.title) : null
  const Icon = step.kind === 'thinking' ? Brain : step.kind === 'status' ? Info : CATEGORY_ICONS[category ?? 'other']
  const title = category ? t(`work.categories.${category}`) : step.title
  const summary = step.kind === 'tool' ? summarizeToolInput(step.input) : ''
  const inputPreview = step.kind === 'tool' ? previewToolInput(step.input) : ''
  const inputDetail = step.kind === 'tool' ? summarizeToolInput(step.input) : ''
  const detail = step.kind === 'tool' ? (step.output ?? step.detail ?? '') : (step.detail ?? '')
  const canExpand = Boolean(inputDetail || detail)

  return (
    <div className="rounded-md">
      <button
        type="button"
        disabled={!canExpand}
        onClick={() => setOpen((value) => !value)}
        className="flex w-full min-w-0 items-center gap-1.5 rounded-md px-1.5 py-0.5 text-left text-[10px] leading-4 transition-colors hover:bg-[hsl(var(--surface-subtle))] disabled:cursor-default"
      >
        <Icon className="h-3 w-3 shrink-0 text-muted-soft" />
        <span className="min-w-0 shrink-0 truncate text-foreground">
          {title}
          {category ? <span className="ml-1.5 text-faint">{step.title}</span> : null}
        </span>
        {summary ? <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-muted-soft">{summary}</span> : null}
        {step.status === 'running' ? (
          <LoaderCircle className="h-3 w-3 shrink-0 animate-spin text-muted-soft" />
        ) : step.status === 'error' ? (
          <CircleX className="h-3 w-3 shrink-0 text-red-500" />
        ) : (
          <CircleCheck className="h-3 w-3 shrink-0 text-emerald-500" />
        )}
        {canExpand ? (
          <ChevronDown className={cn('h-3 w-3 shrink-0 text-muted-soft transition-transform', open ? 'rotate-0' : '-rotate-90')} />
        ) : null}
      </button>
      {open && canExpand ? (
        <div className="mx-1 mb-1 min-w-0 rounded-md bg-[hsl(var(--surface-base))] px-2 py-1.5">
          {inputPreview ? (
            <pre className="max-h-40 overflow-y-auto whitespace-pre-wrap break-all font-mono text-[10px] leading-4 text-muted-soft">{inputDetail}</pre>
          ) : null}
          {detail ? (
            <pre className="mt-1 max-h-48 overflow-y-auto whitespace-pre-wrap break-all border-t border-[hsl(var(--glass-divider))] pt-1 font-mono text-[10px] leading-4 text-muted-soft">{detail}</pre>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function summarizeToolInput(input?: string): string {
  if (!input) return ''
  try {
    const parsed = JSON.parse(input) as unknown
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return Object.entries(parsed as Record<string, unknown>)
        .map(([key, value]) => `${key}: ${typeof value === 'string' ? value : JSON.stringify(value)}`)
        .join(', ')
    }
  } catch {
    return input.replace(/\s+/g, ' ').trim()
  }
  return input.replace(/\s+/g, ' ').trim()
}

function previewToolInput(input?: string): string {
  if (!input) return ''
  try {
    const parsed = JSON.parse(input) as Record<string, unknown>
    const command = parsed.command
    if (typeof command === 'string') return previewText(command, 360)
    if (Array.isArray(command)) return previewText(command.map(String).join(' '), 360)
    return previewText(summarizeToolInput(input), 360)
  } catch {
    return previewText(input, 360)
  }
}

function previewText(value: string | undefined, limit: number): string {
  if (!value) return ''
  const compact = value.replace(/\s+/g, ' ').trim()
  return compact.length > limit ? `${compact.slice(0, limit)}…` : compact
}

function PermissionCard({ request, onDecision }: { request: AgentPermissionRequest; onDecision?: AgentTimelineProps['onPermissionDecision'] }) {
  const { t } = useTranslation('agent')

  return (
    <div className="rounded-xl border border-amber-500/30 bg-amber-500/5 px-4 py-3">
      <div className="flex items-center gap-2 text-sm font-medium text-foreground">
        <ShieldAlert className="h-4 w-4 shrink-0 text-amber-500" />
        {t('permission.title')}
      </div>
      <p className="mt-1.5 text-xs text-muted-soft">
        {t('permission.description', { tool: request.tool })}
      </p>
      {request.detail ? (
        <pre className="mt-2 max-h-40 overflow-y-auto whitespace-pre-wrap break-all rounded-md bg-[hsl(var(--surface-base))] px-2 py-1.5 font-mono text-[11px] leading-5 text-muted-soft">
          {request.detail}
        </pre>
      ) : null}
      {request.decision ? (
        <p className={cn('mt-2 text-xs font-medium', request.decision === 'allowed' ? 'text-emerald-500' : 'text-red-500')}>
          {request.decision === 'allowed' ? t('permission.allowed') : t('permission.denied')}
        </p>
      ) : (
        <div className="mt-3 flex gap-2">
          <Button size="sm" onClick={() => onDecision?.(request, 'allow')}>{t('permission.allow')}</Button>
          <Button size="sm" variant="outline" onClick={() => onDecision?.(request, 'deny')}>{t('permission.deny')}</Button>
        </div>
      )}
    </div>
  )
}
