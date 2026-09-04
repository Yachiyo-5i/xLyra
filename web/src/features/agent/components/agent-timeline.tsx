import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, ChevronDown, ChevronRight, CircleCheck, CircleX, Copy, FilePenLine, FileText, Globe, Info, LoaderCircle, Pencil, Search, ShieldAlert, TerminalSquare, Wrench } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { PlaygroundMessageAction } from '@/features/playground/components/playground-message-actions'
import { ChatAttachmentItem } from '@/features/playground/components/chat-attachment'
import { MarkdownMessage } from '@/features/playground/components/markdown-message'
import type { ChatAttachment } from '@/features/playground/lib/types'
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
          ? <UserMessage key={item.id} text={item.text} attachments={item.attachments} editable={index === lastUserIndex && Boolean(item.messageId) && Boolean(onUserEdit)} onEdit={item.messageId && onUserEdit ? () => onUserEdit(item.messageId!, item.text) : undefined} />
          : <RunBlock key={item.id} run={item.run} onPermissionDecision={onPermissionDecision} />
      ))}
    </div>
  )
}

function UserMessage({ text, attachments, editable, onEdit }: { text: string; attachments?: ChatAttachment[]; editable?: boolean; onEdit?: () => void }) {
  const { t } = useTranslation(['agent', 'playground'])
  return (
    <div className="group flex flex-col items-end gap-1">
      {attachments?.length ? (
        <div className="flex max-w-[82%] flex-wrap justify-end gap-2">
          {attachments.map((attachment) => <ChatAttachmentItem key={attachment.id} attachment={attachment} />)}
        </div>
      ) : null}
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

type RunFlowItem =
  | { kind: 'group'; id: string; steps: AgentWorkStep[] }
  | { kind: 'status'; id: string; step: AgentWorkStep }
  | { kind: 'text'; id: string; text: string }
  | { kind: 'permission'; id: string; request: AgentPermissionRequest }

/**
 * 把 run 的步骤与权限请求组装成按实际发生顺序的渲染流：
 * 连续的工具调用归为一个折叠组；状态步骤（压缩、提醒）单独成行直接可见；
 * 文本段与权限卡按位置插入。
 */
function buildRunFlow(run: AgentRun): RunFlowItem[] {
  const pendingPermissions = new Map<number, AgentPermissionRequest[]>()
  for (const request of run.permissions) {
    const position = request.position ?? Number.MAX_SAFE_INTEGER
    pendingPermissions.set(position, [...(pendingPermissions.get(position) ?? []), request])
  }
  const items: RunFlowItem[] = []
  const flushPermissions = (position: number) => {
    for (const request of pendingPermissions.get(position) ?? []) {
      items.push({ kind: 'permission', id: request.id, request })
    }
    pendingPermissions.delete(position)
  }
  run.steps.forEach((step, index) => {
    // position === 到达时已有的步骤数，即排在第 index 步之前的请求先出列
    flushPermissions(index)
    if (step.kind === 'text') {
      if (step.detail) items.push({ kind: 'text', id: step.id, text: step.detail })
      return
    }
    if (step.kind === 'status') {
      items.push({ kind: 'status', id: step.id, step })
      return
    }
    const last = items[items.length - 1]
    if (last?.kind === 'group') last.steps.push(step)
    else items.push({ kind: 'group', id: step.id, steps: [step] })
  })
  for (const position of [...pendingPermissions.keys()].sort((a, b) => a - b)) flushPermissions(position)
  return items
}

function RunBlock({ run, onPermissionDecision }: { run: AgentRun; onPermissionDecision?: AgentTimelineProps['onPermissionDecision'] }) {
  const { t } = useTranslation('agent')
  const done = run.status !== 'running'
  const pausedForPermission = run.status === 'cancelled' && run.permissions.length > 0
  const flow = buildRunFlow(run)
  const lastTextIndex = flow.reduce((found, item, index) => (item.kind === 'text' ? index : found), -1)
  // 有最终回复的完成 run 才把工作过程自动折叠进「用时」行；纯状态步骤的 run
  // （如手动压缩）没有回复文本，保持展开让「已压缩上下文」直接可见
  const settled = run.status === 'done' && lastTextIndex >= 0
  // 结束后最后一段文本是最终回复，其余（中间输出 + 工具组 + 权限卡）全部归入工作过程
  const finalIndex = settled ? lastTextIndex : -1
  const processItems = finalIndex >= 0 ? flow.filter((_, index) => index !== finalIndex) : flow
  const finalItem = finalIndex >= 0 ? flow[finalIndex] : undefined
  // 复制内容与展示口径一致：只复制作为回复展示的最后一段，不带中间输出
  const replyText = finalItem?.kind === 'text' ? finalItem.text : run.finalText
  const [override, setOverride] = useState<boolean | null>(null)
  const processExpanded = override ?? !settled
  const now = useTickingNow(!done)
  const elapsedMs = done
    ? (run.elapsedMs ?? Math.max(0, (run.endedAt ?? now) - run.startedAt))
    : (run.elapsedMs ?? 0) + Math.max(0, now - run.startedAt)
  const hasProcess = processItems.length > 0
  // 纯状态 run（如手动压缩）：没有用时行、没有折叠区，状态行直接顶层显示
  const onlyStatus = flow.length > 0 && flow.every((item) => item.kind === 'status')
  if (onlyStatus) {
    return (
      <div className="space-y-1">
        {flow.map((item) => item.kind === 'status' ? <StatusStepLine key={item.id} step={item.step} /> : null)}
        {run.status === 'error' ? (
          <p className="px-1.5 text-xs text-red-500">{t('work.failed')}</p>
        ) : null}
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div>
        <button
          type="button"
          disabled={!hasProcess}
          onClick={() => setOverride(processExpanded ? false : true)}
          className="group flex w-full items-center gap-1.5 py-0.5 text-left text-[15px] leading-6 text-muted-soft transition-colors hover:text-foreground disabled:cursor-default disabled:hover:text-muted-soft"
        >
          <span className="min-w-0 truncate text-[15px] tabular-nums">{t('work.elapsed', { duration: formatElapsed(elapsedMs) })}</span>
          {hasProcess ? (
            <ChevronRight className={cn('h-3 w-3 shrink-0 transition-transform', processExpanded && 'rotate-90')} />
          ) : null}
        </button>
        <div aria-hidden className="mt-1.5 border-t border-[hsl(var(--glass-divider))]" />
        {hasProcess && processExpanded ? (
          <div className="mt-1.5 space-y-3 border-l-2 border-[hsl(var(--glass-divider))] pl-3">
            {processItems.map((item, index) => {
              if (item.kind === 'text') {
                return <MarkdownMessage key={item.id} content={item.text} className="text-[15px] leading-7" />
              }
              if (item.kind === 'permission') {
                return <PermissionCard key={item.id} request={item.request} onDecision={onPermissionDecision} />
              }
              if (item.kind === 'status') {
                return <StatusStepLine key={item.id} step={item.step} />
              }
              return <StepGroup key={item.id} steps={item.steps} active={!done && index === processItems.length - 1} />
            })}
          </div>
        ) : null}
      </div>

      {finalItem?.kind === 'text' ? (
        <MarkdownMessage content={finalItem.text} className="text-[15px] leading-7" />
      ) : null}

      {done && !pausedForPermission && replyText ? (
        <div className="flex items-center gap-2">
          <CopyMessageAction text={replyText} />
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

/** 运行期间每秒刷新一次，驱动「用时」行的实时计时 */
function useTickingNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active) return
    const timer = window.setInterval(() => setNow(Date.now()), 1_000)
    return () => window.clearInterval(timer)
  }, [active])
  return now
}

/** 用时格式：1 分钟内「42s」，之后「7m 59s」（对齐 Codex 桌面端的计时行） */
function formatElapsed(ms: number): string {
  const totalSeconds = Math.floor(Math.max(0, ms) / 1_000)
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return minutes > 0 ? `${minutes}m ${seconds}s` : `${seconds}s`
}

function StepGroup({ steps, active }: { steps: AgentWorkStep[]; active?: boolean }) {
  const { t } = useTranslation('agent')
  const [expanded, setExpanded] = useState(false)
  // 有行展开详情时放宽限高，避免详情被压进 5 行高度里
  const [openRows, setOpenRows] = useState(0)
  const runningStep = [...steps].reverse().find((step) => step.status === 'running')
  const statusLabel = runningStep
    ? t(runningStep.argsDone === false ? 'work.preparingTool' : 'work.runningTool', { tool: runningStep.title })
    : active
      ? t('work.running', { count: steps.length })
      : summarizeSteps(steps, t)

  return (
    <div>
      <button
        type="button"
        onClick={() => setExpanded((value) => !value)}
        className="group flex w-full items-center gap-1.5 py-0.5 text-left text-[10px] leading-4 text-muted-soft transition-colors hover:text-foreground"
      >
        {/* 字号钉在 span 上：button 上的字号类在部分场景会被继承值覆盖，导致汇总行比步骤行大 */}
        <span className="min-w-0 truncate text-[10px]">{statusLabel}</span>
        <ChevronRight className={cn('h-3 w-3 shrink-0 transition-transform', expanded && 'rotate-90')} />
      </button>
      {expanded ? (
        /* 展开区限高内部滚动：默认约 5 行（每行 20px + 4px 间距）；有行展开详情时放宽 */
        <div className={cn(
          'mt-1.5 space-y-1 overflow-y-auto border-l-2 border-[hsl(var(--glass-divider))] pl-3 transition-[max-height] duration-200',
          openRows > 0 ? 'max-h-96' : 'max-h-[116px]',
        )}>
          {steps.map((step) => (
            <StepRow
              key={step.id}
              step={step}
              onOpenChange={(open) => setOpenRows((count) => count + (open ? 1 : -1))}
            />
          ))}
        </div>
      ) : null}
    </div>
  )
}

type ToolCategory = 'command' | 'fileEdit' | 'network' | 'fileRead' | 'search' | 'other'

/** 压缩类状态标题：走 15px 大字状态行（与「用时」行同级），不用工具行的 10px 样式 */
const COMPACTION_STATUS_TITLES = new Set(['compacting', 'compacting_manual', 'compacted', 'compaction', 'compaction_skipped'])

/** 状态步骤的统一出口：压缩类用大字状态行，其余（提醒等）用工具行样式 */
function StatusStepLine({ step }: { step: AgentWorkStep }) {
  if (COMPACTION_STATUS_TITLES.has(step.title)) return <CompactionStatusLine step={step} />
  return <StepRow step={step} />
}

function CompactionStatusLine({ step }: { step: AgentWorkStep }) {
  const { t } = useTranslation('agent')
  const title = STATUS_TITLE_KEYS[step.title] ? t(STATUS_TITLE_KEYS[step.title]) : step.title
  return (
    <div className="flex items-center gap-1.5 py-0.5 text-[15px] leading-6 text-muted-soft">
      {step.status === 'running' ? (
        <LoaderCircle className="h-3.5 w-3.5 shrink-0 animate-spin" />
      ) : step.status === 'error' ? (
        <CircleX className="h-3.5 w-3.5 shrink-0 text-red-500" />
      ) : (
        <CircleCheck className="h-3.5 w-3.5 shrink-0 text-emerald-500" />
      )}
      <span className="min-w-0 truncate">{title}</span>
    </div>
  )
}

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

/** 汇总展示顺序：读文件 → 改文件 → 命令 → 搜索 → 网络 → 其他 */
const SUMMARY_ORDER = ['fileRead', 'fileEdit', 'command', 'search', 'network', 'other'] as const

/**
 * 折叠组的完成汇总：按类别拼成「查看了 2 个文件、运行了 1 条命令」。
 * 只有状态步骤（如压缩提示）时退回原来的步数汇总。
 */
function summarizeSteps(steps: AgentWorkStep[], t: (key: string, options?: Record<string, unknown>) => string): string {
  const counts = new Map<string, number>()
  for (const step of steps) {
    if (step.kind !== 'tool') continue
    const key = toolCategory(step.title)
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  if (counts.size === 0) return t('work.summary', { count: steps.length })
  const parts = SUMMARY_ORDER
    .filter((key) => counts.has(key))
    .map((key) => t(`work.summaryParts.${key}`, { count: counts.get(key) }))
  return parts.join(t('work.summaryJoin'))
}

/** 状态步骤标题 → 本地化键（历史回放里的 'compaction' 与 'compacted' 同义） */
const STATUS_TITLE_KEYS: Record<string, string> = {
  compacting: 'work.statusCompacting',
  compacting_manual: 'work.statusCompactingManual',
  compacted: 'work.statusCompacted',
  compaction: 'work.statusCompacted',
  compaction_skipped: 'work.statusCompactSkipped',
}

function StepRow({ step, onOpenChange }: { step: AgentWorkStep; onOpenChange?: (open: boolean) => void }) {
  const { t } = useTranslation('agent')
  const [open, setOpen] = useState(false)
  const category = step.kind === 'tool' ? toolCategory(step.title) : null
  const Icon = step.kind === 'status' ? Info : CATEGORY_ICONS[category ?? 'other']
  const title = category
    ? t(`work.categories.${category}`)
    : STATUS_TITLE_KEYS[step.title]
      ? t(STATUS_TITLE_KEYS[step.title])
      : step.title
  const summary = step.kind === 'tool' ? summarizeToolInput(step.input) : ''
  const inputPreview = step.kind === 'tool' ? previewToolInput(step.input) : ''
  const inputDetail = step.kind === 'tool' ? summarizeToolInput(step.input) : ''
  const detail = step.kind === 'tool' ? (step.output ?? step.detail ?? '') : (step.detail ?? '')
  const canExpand = Boolean(inputDetail || detail)

  const toggle = () => {
    const next = !open
    setOpen(next)
    onOpenChange?.(next)
  }

  return (
    <div className="rounded-md">
      <button
        type="button"
        disabled={!canExpand}
        onClick={toggle}
        className="flex w-full min-w-0 items-center gap-1.5 rounded-md px-1.5 py-0.5 text-left text-[10px] leading-4 transition-colors hover:bg-[hsl(var(--surface-subtle))] disabled:cursor-default"
      >
        <Icon className="h-3 w-3 shrink-0 text-muted-soft" />
        {/* 标题与参数摘要统一 10px：CJK 字面满字身会视觉上比拉丁等宽字大，必须显式钉住字号 */}
        <span className="min-w-0 shrink-0 truncate text-[10px] text-foreground">
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
        <div className="agent-step-detail mx-1 mb-1 min-w-0 rounded-md px-2 py-1.5">
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
