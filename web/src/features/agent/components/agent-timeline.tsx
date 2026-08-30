import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Brain, ChevronDown, ChevronRight, CircleCheck, CircleX, FilePenLine, FileText, Globe, Info, LoaderCircle, Search, ShieldAlert, TerminalSquare, Wrench } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { MarkdownMessage } from '@/features/playground/components/markdown-message'
import type { AgentPermissionRequest, AgentRun, AgentTimelineItem, AgentWorkStep } from '@/features/agent/lib/agent-events'

type AgentTimelineProps = {
  items: AgentTimelineItem[]
  onPermissionDecision?: (request: AgentPermissionRequest, decision: 'allow' | 'deny') => void
}

export function AgentTimeline({ items, onPermissionDecision }: AgentTimelineProps) {
  return (
    <div className="space-y-6">
      {items.map((item) => (
        item.kind === 'user'
          ? <UserMessage key={item.id} text={item.text} />
          : <RunBlock key={item.id} run={item.run} onPermissionDecision={onPermissionDecision} />
      ))}
    </div>
  )
}

function UserMessage({ text }: { text: string }) {
  return (
    <div className="flex justify-end">
      <div className="max-w-[82%] whitespace-pre-wrap rounded-2xl bg-primary px-4 py-3 text-sm leading-6 text-primary-foreground">
        {text}
      </div>
    </div>
  )
}

function RunBlock({ run, onPermissionDecision }: { run: AgentRun; onPermissionDecision?: AgentTimelineProps['onPermissionDecision'] }) {
  const { t } = useTranslation('agent')
  const done = run.status !== 'running'
  const [expanded, setExpanded] = useState(false)
  const showSteps = run.steps.length > 0
  const stepsVisible = showSteps && (!done || expanded)
  const errorCount = run.steps.filter((step) => step.status === 'error').length

  return (
    <div className="space-y-3">
      {showSteps ? (
        <div>
          <button
            type="button"
            onClick={() => setExpanded((value) => !value)}
            className="group flex w-full items-center gap-1.5 py-1.5 text-left text-xs text-muted-soft transition-colors hover:text-foreground"
          >
            {done ? null : (
              <LoaderCircle className="h-3.5 w-3.5 shrink-0 animate-spin" />
            )}
            <span className="min-w-0 truncate">
              {done
                ? t('work.summary', { count: run.steps.length })
                : t('work.running', { count: run.steps.length })}
            </span>
            <ChevronRight className={cn('h-3 w-3 shrink-0 transition-transform', stepsVisible && 'rotate-90')} />
            {errorCount > 0 ? (
              <span className="ml-auto shrink-0 text-red-500">{t('work.errors', { count: errorCount })}</span>
            ) : null}
          </button>
          <div className="border-t border-[hsl(var(--glass-divider))]" />
          {stepsVisible ? (
            <div className="space-y-1 py-2">
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
        <MarkdownMessage content={run.finalText} />
      ) : null}

      {run.status === 'error' ? (
        <p className="text-xs text-red-500">{t('work.failed')}</p>
      ) : null}
      {/* A cancellation caused by a pending escalation is a pause, not a stop — hide the label to avoid misleading. */}
      {run.status === 'cancelled' && !run.permissions.some((request) => !request.decision) ? (
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

  return (
    <div className="rounded-md">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs transition-colors hover:bg-[hsl(var(--surface-subtle))]"
      >
        <Icon className="h-3.5 w-3.5 shrink-0 text-muted-soft" />
        <span className="min-w-0 flex-1 truncate text-foreground">
          {title}
          {category ? <span className="ml-1.5 text-faint">{step.title}</span> : null}
        </span>
        {step.status === 'running' ? (
          <LoaderCircle className="h-3 w-3 shrink-0 animate-spin text-muted-soft" />
        ) : step.status === 'error' ? (
          <CircleX className="h-3 w-3 shrink-0 text-red-500" />
        ) : (
          <CircleCheck className="h-3 w-3 shrink-0 text-emerald-500" />
        )}
        {step.detail ? (
          <ChevronDown className={cn('h-3 w-3 shrink-0 text-muted-soft transition-transform', open ? 'rotate-0' : '-rotate-90')} />
        ) : null}
      </button>
      {open && step.detail ? (
        <pre className="mx-2 mb-1 max-h-48 overflow-y-auto whitespace-pre-wrap break-all rounded-md bg-[hsl(var(--surface-base))] px-2 py-1.5 font-mono text-[11px] leading-5 text-muted-soft">
          {step.detail}
        </pre>
      ) : null}
    </div>
  )
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
