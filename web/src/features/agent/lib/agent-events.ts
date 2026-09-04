import type { AgentTranscriptEntry } from '@/features/agent/api/agent'
import type { ChatAttachment } from '@/features/playground/lib/types'

export type AgentStepStatus = 'running' | 'done' | 'error'
export type AgentRunStatus = 'running' | 'done' | 'error' | 'cancelled'

export type AgentWorkStep = {
  id: string
  /** text 是 loop 中途的助手文本段，与工具/状态步骤按到达顺序交错排列 */
  kind: 'tool' | 'status' | 'text'
  title: string
  input?: string
  output?: string
  argsDone?: boolean
  detail?: string
  status: AgentStepStatus
}

export type AgentPermissionRequest = {
  id: string
  tool: string
  detail?: string
  /** Resolved path/capability key recorded by the grant, echoed back to the grant-access API. */
  resolvedPath?: string
  decision?: 'allowed' | 'denied'
  /** 权限请求到达时 run.steps 的长度，用于把它插回流程中的实际位置 */
  position?: number
}

export type AgentRun = {
  id: string
  status: AgentRunStatus
  steps: AgentWorkStep[]
  finalText: string
  permissions: AgentPermissionRequest[]
  startedAt: number
  endedAt?: number
  elapsedMs?: number
}

export type AgentTimelineItem =
  | { kind: 'user'; id: string; text: string; messageId?: string; createdAt: number; attachments?: ChatAttachment[] }
  | { kind: 'run'; id: string; run: AgentRun }

export type AgentTimeline = AgentTimelineItem[]

export type AgentStreamEvent = { type: string; data: unknown }

type EventKind =
  | 'text'
  | 'tool_call'
  | 'tool_call_delta'
  | 'tool_result'
  | 'thinking'
  | 'notice'
  | 'compaction'
  | 'permission'
  | 'permission_result'
  | 'done'
  | 'error'
  | 'cancelled'
  | 'other'

let fallbackCounter = 0

function nextId(prefix: string) {
  fallbackCounter += 1
  return `${prefix}-${fallbackCounter}`
}

function asRecord(data: unknown): Record<string, unknown> | null {
  if (!data || typeof data !== 'object' || Array.isArray(data)) return null
  return data as Record<string, unknown>
}

function pickString(record: Record<string, unknown> | null, keys: string[]): string {
  if (!record) return ''
  for (const key of keys) {
    const value = record[key]
    if (typeof value === 'string' && value.trim()) return value
  }
  return ''
}

function pickId(record: Record<string, unknown> | null, keys: string[]): string {
  if (!record) return ''
  for (const key of keys) {
    const value = record[key]
    if (typeof value === 'string' && value.trim()) return value
    if (typeof value === 'number') return String(value)
  }
  return ''
}

function pickDetail(record: Record<string, unknown> | null, keys: string[]): string {
  if (!record) return ''
  for (const key of keys) {
    const value = record[key]
    if (typeof value === 'string' && value.trim()) return value
    if (value !== undefined && value !== null) {
      try {
        const encoded = JSON.stringify(value)
        return encoded.length > 400 ? `${encoded.slice(0, 400)}…` : encoded
      } catch {
        return ''
      }
    }
  }
  return ''
}

function pickDelta(record: Record<string, unknown> | null): string {
  if (!record) return ''
  return typeof record.delta === 'string' ? record.delta : ''
}

function pickNumber(record: Record<string, unknown> | null, keys: string[]): number | undefined {
  if (!record) return undefined
  for (const key of keys) {
    const value = record[key]
    if (typeof value === 'number' && Number.isFinite(value)) return value
  }
  return undefined
}

function parseTimestamp(value?: string): number {
  const parsed = value ? Date.parse(value) : NaN
  return Number.isFinite(parsed) ? parsed : Date.now()
}

export function extractEventText(data: unknown): string {
  if (typeof data === 'string') return data
  const record = asRecord(data)
  if (!record) return ''
  return pickString(record, ['text', 'content', 'message', 'delta'])
}

function classifyEvent(type: string, data: unknown): EventKind {
  const normalized = type.toLowerCase()
  // Escalation negotiation events are permission requests (payload nested under data.escalation).
  if (normalized.includes('escalation')) return 'permission'
  // 停滞/预算等软提醒：正文在 notice 字段，渲染为状态步骤而不是正文
  if (normalized.includes('stall') || normalized.includes('budget') || typeof asRecord(data)?.notice === 'string') return 'notice'
  if (normalized.includes('compact')) return 'compaction'
  if (normalized === 'tool_call_delta' || normalized === 'toolcall_delta') return 'tool_call_delta'
  if (normalized.includes('permission') || normalized.includes('approval')) {
    if (normalized.includes('result') || normalized.includes('grant') || normalized.includes('decision') || normalized.includes('resolved')) {
      return 'permission_result'
    }
    return 'permission'
  }
  if (normalized.includes('tool')) {
    if (normalized.includes('result') || normalized.includes('output') || normalized.includes('end') || normalized.includes('done')) {
      return 'tool_result'
    }
    return 'tool_call'
  }
  if (normalized.includes('think') || normalized.includes('reason')) return 'thinking'
  if (normalized.includes('cancel')) return 'cancelled'
  if (normalized.includes('error') || normalized.includes('fail')) return 'error'
  if (normalized.includes('done') || normalized.includes('complete') || normalized.includes('finish')) return 'done'
  if (normalized.includes('message') || normalized.includes('text') || normalized.includes('delta') || normalized.includes('content') || normalized.includes('assistant')) {
    return 'text'
  }
  return extractEventText(data) ? 'text' : 'other'
}

function lastRun(timeline: AgentTimeline): { index: number; run: AgentRun } | null {
  const last = timeline[timeline.length - 1]
  if (last?.kind !== 'run') return null
  return { index: timeline.length - 1, run: last.run }
}

function ensureRun(timeline: AgentTimeline): AgentTimeline {
  const current = lastRun(timeline)
  if (current && current.run.status === 'running') return timeline
  const resumedAfterEscalation = current?.run.status === 'cancelled'
    && current.run.permissions.length > 0
  if (resumedAfterEscalation) {
    return updateLastRun(timeline, (run) => ({
      ...run,
      status: 'running',
      startedAt: Date.now(),
      endedAt: undefined,
    }))
  }
  const lastItem = timeline[timeline.length - 1]
  const startedAt = lastItem?.kind === 'user' ? lastItem.createdAt : Date.now()
  const run: AgentRun = { id: nextId('run'), status: 'running', steps: [], finalText: '', permissions: [], startedAt }
  return [...timeline, { kind: 'run', id: run.id, run }]
}

/**
 * 手动压缩等「必须独占一个 run」的操作用：先落定任何残留的 running run
 * （正常流程下提交被 running 状态拦截，残留只可能来自丢失的终态事件），
 * 再开新 run，避免压缩状态混进上一轮回复的执行过程。
 */
function ensureFreshRun(timeline: AgentTimeline): AgentTimeline {
  const current = lastRun(timeline)
  if (current && current.run.status === 'running') {
    const closed = updateLastRun(timeline, (run) => closeRun(run, 'done'))
    return ensureRun(closed)
  }
  return ensureRun(timeline)
}

function updateLastRun(timeline: AgentTimeline, updater: (run: AgentRun) => AgentRun): AgentTimeline {
  const current = lastRun(timeline)
  if (!current) return timeline
  const next = timeline.slice()
  next[current.index] = { kind: 'run', id: current.run.id, run: updater(current.run) }
  return next
}

function markRunEndedAt(timeline: AgentTimeline, timestamp?: string): AgentTimeline {
  const endedAt = timestamp ? Date.parse(timestamp) : NaN
  return Number.isFinite(endedAt)
    ? updateLastRun(timeline, (run) => ({ ...run, endedAt }))
    : timeline
}

function finishRunAt(timeline: AgentTimeline, timestamp?: string): AgentTimeline {
  const endedAt = timestamp ? Date.parse(timestamp) : NaN
  if (!Number.isFinite(endedAt)) return timeline
  return updateLastRun(timeline, (run) => run.status === 'running'
    // 已有 endedAt（每个条目到达时标记的最后活动时间）优先：用下一条用户消息的
    // 时间收尾会把两轮之间的空闲间隔也算进用时
    ? closeRun({ ...run, endedAt: run.endedAt ?? endedAt }, 'done')
    : run)
}

function closeRun(run: AgentRun, status: AgentRunStatus, elapsedMs?: number): AgentRun {
  const endedAt = run.endedAt ?? Date.now()
  const phaseElapsedMs = elapsedMs ?? Math.max(0, endedAt - run.startedAt)
  return {
    ...run,
    endedAt,
    elapsedMs: (run.elapsedMs ?? 0) + phaseElapsedMs,
    status,
    steps: run.steps.map((step) => (step.status === 'running' ? { ...step, status: status === 'error' ? 'error' : 'done' } : step)),
  }
}

export function appendUserMessage(timeline: AgentTimeline, text: string, messageId?: string, createdAt = Date.now(), attachments?: ChatAttachment[]): AgentTimeline {
  // 上一轮若因丢失终态事件仍停留在 running，先就地落定：否则它的计时会跨轮
  // 继续走，看起来像「上一条消息的用时被算到了新消息的时间」
  const settled = updateLastRun(timeline, (run) => (run.status === 'running' ? closeRun(run, 'done') : run))
  return [...settled, { kind: 'user', id: messageId ?? nextId('user'), messageId, text, createdAt, ...(attachments?.length ? { attachments } : {}) }]
}

export function replaceFromUserMessage(timeline: AgentTimeline, messageId: string, text: string, attachments?: ChatAttachment[]): AgentTimeline {
  const index = timeline.findIndex((item) => item.kind === 'user' && (item.messageId === messageId || item.id === messageId))
  if (index < 0) return appendUserMessage(timeline, text, messageId, Date.now(), attachments)
  const previous = timeline[index]
  const preservedAttachments = attachments ?? (previous.kind === 'user' ? previous.attachments : undefined)
  return [...timeline.slice(0, index), { kind: 'user', id: messageId, messageId, text, createdAt: Date.now(), ...(preservedAttachments?.length ? { attachments: preservedAttachments } : {}) }]
}

export function canReconcileTimeline(current: AgentTimeline, transcript: AgentTimeline): boolean {
  const currentUsers = current.filter((item): item is Extract<AgentTimeline[number], { kind: 'user' }> => item.kind === 'user')
  const transcriptUsers = transcript.filter((item): item is Extract<AgentTimeline[number], { kind: 'user' }> => item.kind === 'user')
  if (transcriptUsers.length < currentUsers.length) return false
  return currentUsers.every((user, index) => {
    const transcriptUser = transcriptUsers[index]
    if (!transcriptUser || transcriptUser.text !== user.text) return false
    return !user.messageId || !transcriptUser.messageId || user.messageId === transcriptUser.messageId
  })
}

export function reconcileTimeline(current: AgentTimeline, transcript: AgentTimeline): AgentTimeline {
  if (transcript.length < current.length || !canReconcileTimeline(current, transcript)) return current
  const currentRuns = current.filter((item): item is Extract<AgentTimeline[number], { kind: 'run' }> => item.kind === 'run')
  let runIndex = 0
  return transcript.map((item) => {
    if (item.kind !== 'run') return item
    const active = currentRuns[runIndex]
    runIndex += 1
    if (!active || active.run.elapsedMs === undefined) return item
    return {
      ...item,
      run: {
        ...item.run,
        startedAt: active.run.startedAt,
        endedAt: active.run.endedAt,
        elapsedMs: active.run.elapsedMs,
      },
    }
  })
}

/**
 * 事件流结束但没有收到终态事件（连接被提前关闭/中断）时的兜底：
 * 把仍在 running 的 run 就地落定，让计时停在当前值而不是无限走下去。
 */
export function settleRunningRuns(timeline: AgentTimeline): AgentTimeline {
  return timeline.map((item) => item.kind === 'run' && item.run.status === 'running'
    ? { ...item, run: closeRun(item.run, 'done') }
    : item)
}

export function reduceAgentEvent(timeline: AgentTimeline, event: AgentStreamEvent): AgentTimeline {
  const kind = classifyEvent(event.type, event.data)
  const record = asRecord(event.data)

  switch (kind) {
    case 'other': {
      if (event.type.toLowerCase() === 'agent_start') return ensureRun(timeline)
      return timeline
    }
    case 'text': {
      const text = extractEventText(event.data)
      if (!text) return timeline
      const withRun = ensureRun(timeline)
      // finalText 保留全量拼接（复制/重试判断用）；text step 记录与工具步骤的交错顺序
      return updateLastRun(withRun, (run) => {
        const steps = run.steps.slice()
        const last = steps[steps.length - 1]
        if (last?.kind === 'text') {
          steps[steps.length - 1] = { ...last, detail: `${last.detail ?? ''}${text}` }
        } else {
          steps.push({ id: nextId('text'), kind: 'text', title: 'text', detail: text, status: 'done' })
        }
        return { ...run, finalText: run.finalText + text, steps }
      })
    }
    case 'tool_call': {
      // Runner events nest the payload under data.tool_call; flat tool_* events use data directly.
      const payload = asRecord(record?.tool_call) ?? record
      const callId = pickId(payload, ['id', 'tool_call_id', 'call_id'])
      const normalizedType = event.type.toLowerCase()
      const argsDone = !normalizedType.includes('start')
      const input = pickDetail(payload, ['raw_arguments', 'arguments', 'args', 'input', 'command']) || undefined
      if (lastRun(timeline)?.run.steps.some((step) => step.id === callId && callId)) {
        // Completion event (tool_call): backfill the argument detail.
        return updateLastRun(timeline, (run) => ({
          ...run,
          steps: run.steps.map((step) => (step.id === callId
            ? { ...step, input: input ?? step.input, argsDone: argsDone || step.argsDone }
            : step)),
        }))
      }
      const withRun = ensureRun(timeline)
      const step: AgentWorkStep = {
        id: callId || nextId('step'),
        kind: 'tool',
        title: pickString(payload, ['name', 'tool', 'tool_name', 'title']) || 'tool',
        input,
        argsDone,
        status: 'running',
      }
      return updateLastRun(withRun, (run) => ({ ...run, steps: [...run.steps, step] }))
    }
    case 'tool_call_delta': {
      const delta = pickDelta(record)
      if (!delta) return timeline
      const toolId = pickId(record, ['tool_call_id', 'call_id', 'id'])
      return updateLastRun(timeline, (run) => {
        const index = toolId
          ? run.steps.findIndex((step) => step.kind === 'tool' && step.id === toolId)
          : run.steps.map((step, position) => (step.kind === 'tool' && step.argsDone === false ? position : -1)).filter((position) => position >= 0).pop() ?? -1
        if (index < 0) return run
        const steps = run.steps.slice()
        const step = steps[index]!
        steps[index] = { ...step, input: `${step.input ?? ''}${delta}`, argsDone: false }
        return { ...run, steps }
      })
    }
    case 'tool_result': {
      // Runner events nest the payload under data.tool_result.
      const payload = asRecord(record?.tool_result) ?? record
      const withRun = ensureRun(timeline)
      const stepId = pickId(payload, ['tool_call_id', 'call_id', 'step_id', 'id'])
      const detail = pickDetail(payload, ['output', 'result', 'content', 'detail']) || extractEventText(payload) || undefined
      const isError = /error|fail/i.test(event.type) || payload?.is_error === true || payload?.ok === false
      return updateLastRun(withRun, (run) => {
        const index = stepId
          ? run.steps.findIndex((step) => step.id === stepId)
          : run.steps.map((step, position) => (step.status === 'running' ? position : -1)).filter((position) => position >= 0).pop() ?? -1
        if (index < 0) {
          const step: AgentWorkStep = { id: stepId || nextId('step'), kind: 'tool', title: pickString(payload, ['name', 'tool', 'tool_name']) || 'tool', output: detail, detail, argsDone: true, status: isError ? 'error' : 'done' }
          return { ...run, steps: [...run.steps, step] }
        }
        const steps = run.steps.slice()
        steps[index] = { ...steps[index], output: detail ?? steps[index].output, detail: detail ?? steps[index].detail, argsDone: true, status: isError ? 'error' : 'done' }
        return { ...run, steps }
      })
    }
    case 'thinking': {
      // 思考内容不进时间线：只展示正文与工具调用，thinking 是过程噪音
      return timeline
    }
    case 'notice': {
      // 停滞/预算等运行提醒属于内部机制，不展示给用户（与转录回放对 agent_notice 的处理一致）
      return timeline
    }
    case 'compaction': {
      const normalized = event.type.toLowerCase()
      // 压缩开始：放一个 running 状态步骤（manual 标记来自前端手动 /compact 流程）
      if (normalized.includes('compacting')) {
        const manual = record?.manual === true
        const withRun = manual ? ensureFreshRun(timeline) : ensureRun(timeline)
        const step: AgentWorkStep = {
          id: nextId('status'),
          kind: 'status',
          title: manual ? 'compacting_manual' : 'compacting',
          status: 'running',
        }
        return updateLastRun(withRun, (run) => ({ ...run, steps: [...run.steps, step] }))
      }
      // 压缩收尾：回填最近一个 running 的压缩步骤；失败（无 compaction 载荷）也落定。
      // 只展示状态文本本身，不带 token 明细
      const finished: Omit<AgentWorkStep, 'id'> = {
        kind: 'status',
        title: asRecord(record?.compaction) ? 'compacted' : 'compaction_skipped',
        status: 'done',
      }
      const withRun = ensureRun(timeline)
      return updateLastRun(withRun, (run) => {
        const index = run.steps.map((step, position) => (step.kind === 'status' && (step.title === 'compacting' || step.title === 'compacting_manual') && step.status === 'running' ? position : -1)).filter((position) => position >= 0).pop() ?? -1
        const completed: AgentWorkStep = { id: index >= 0 ? run.steps[index]!.id : nextId('status'), ...finished }
        if (index < 0) return { ...run, steps: [...run.steps, completed] }
        const steps = run.steps.slice()
        steps[index] = completed
        return { ...run, steps }
      })
    }
    case 'permission': {
      const withRun = ensureRun(timeline)
      // escalation_request nests its payload under data.escalation; flat permission_* events use data directly.
      const payload = asRecord(record?.escalation) ?? record
      const command = payload?.requested_command
      return updateLastRun(withRun, (run) => {
        const request: AgentPermissionRequest = {
          id: pickId(payload, ['escalation_id', 'request_id', 'permission_id', 'id']) || nextId('permission'),
          tool: pickString(payload, ['tool_name', 'tool', 'name', 'title']) || 'tool',
          detail: Array.isArray(command)
            ? command.map(String).join(' ')
            : pickDetail(payload, ['requested_command', 'requested_path', 'arguments', 'args', 'input', 'command', 'description', 'message']) || undefined,
          resolvedPath: pickString(payload, ['resolved_path']) || undefined,
          position: run.steps.length,
        }
        return { ...run, permissions: [...run.permissions, request] }
      })
    }
    case 'permission_result': {
      const requestId = pickId(record, ['request_id', 'permission_id', 'id'])
      const decision = record?.allowed === true || record?.decision === 'allow' || record?.decision === 'allowed' ? 'allowed' : 'denied'
      return updateLastRun(timeline, (run) => ({
        ...run,
        permissions: run.permissions.map((request) => (request.id === requestId ? { ...request, decision } : request)),
      }))
    }
    case 'done':
      return updateLastRun(timeline, (run) => {
        const result = asRecord(record?.result)
        return closeRun(run, 'done', pickNumber(result, ['elapsed_ms', 'elapsedMs']))
      })
    case 'error':
      return updateLastRun(timeline, (run) => closeRun(run, 'error'))
    case 'cancelled':
      return updateLastRun(timeline, (run) => closeRun(run, 'cancelled'))
    default:
      return timeline
  }
}

export function timelineFromTranscript(entries: AgentTranscriptEntry[]): AgentTimeline {
  // Fixed receipt text written on interruption teardown (xlyra-agent sealPendingToolCalls).
  const INTERRUPTED_SEAL = '操作已被中断，工具未执行完成。'
  const AWAITING_SEAL = '等待用户授权，工具暂停执行。'
  let timeline: AgentTimeline = []
  // When the turn contains an escalation, the "interrupted" receipt is actually a pause; downgrade it to neutral.
  let escalationInTurn = false
  const reduceAt = (event: AgentStreamEvent, timestamp?: string) => {
    timeline = markRunEndedAt(reduceAgentEvent(timeline, event), timestamp)
  }
  for (const entry of entries) {
    if (entry.type === 'compaction') {
      // 先收束前一个 run（assistant 回复），再开独立的压缩 run：
      // 压缩是顶层事件，不混进上一轮回复的执行过程
      timeline = updateLastRun(timeline, (run) => (run.status === 'running' ? closeRun(run, 'done') : run))
      timeline = markRunEndedAt(appendStatusStep(timeline, 'compacted'), entry.timestamp)
      continue
    }
    if (entry.type === 'escalation') {
      // Escalations render as permission entries; granted null means undecided (stays interactive).
      const withRun = ensureRun(timeline)
      const command = entry.requested_command
      timeline = markRunEndedAt(updateLastRun(withRun, (run) => {
        const request: AgentPermissionRequest = {
          id: entry.escalation_id || nextId('permission'),
          tool: entry.tool_name || 'tool',
          detail: Array.isArray(command) && command.length
            ? command.join(' ')
            : entry.requested_path || undefined,
          resolvedPath: entry.resolved_path || undefined,
          decision: entry.granted === true ? 'allowed' : entry.granted === false ? 'denied' : undefined,
          position: run.steps.length,
        }
        return { ...run, permissions: [...run.permissions, request] }
      }), entry.timestamp)
      escalationInTurn = true
      continue
    }
    const message = entry.message
    if (message) {
      // The runner's transcript format: { type: 'message', message: ChatMessage, ... }
      if (message.role === 'user') {
        // agent 注入的停滞/预算提醒属于内部机制，前端不展示；也不结束当前 run
        if (message.name === 'agent_notice') continue
        timeline = finishRunAt(timeline, entry.timestamp)
        if (message.content || message.attachments?.length) {
          const attachments = message.attachments?.map((attachment, index) => ({
            id: `${entry.message_id ?? entry.timestamp ?? 'attachment'}-${index}`,
            name: attachment.name,
            mimeType: attachment.mime_type,
            size: attachment.data_url ? Math.max(0, Math.floor((attachment.data_url.length * 3) / 4)) : 0,
            ...(attachment.data_url ? { dataURL: attachment.data_url } : {}),
          }))
          timeline = appendUserMessage(timeline, message.content ?? '', entry.message_id, parseTimestamp(entry.timestamp), attachments)
        }
        escalationInTurn = false
        continue
      }
      if (message.role === 'system') continue
      if (message.role === 'tool') {
        // Backward compat: "interrupted" receipts written before an escalation pause downgrade to neutral (not failed).
        const pausedByEscalation = escalationInTurn && message.is_error === true && message.content === INTERRUPTED_SEAL
        reduceAt({
          type: 'tool_result',
          data: {
            tool_call_id: message.tool_call_id,
            name: message.name,
            content: pausedByEscalation ? AWAITING_SEAL : message.content,
            is_error: pausedByEscalation ? false : message.is_error,
          },
        }, entry.timestamp)
        continue
      }
      // Assistant: text → tool calls，按消息内的自然顺序（文本在工具调用之前）恢复；thinking 不展示
      if (message.content) reduceAt({ type: 'message', data: message.content }, entry.timestamp)
      for (const call of message.tool_calls ?? []) {
        reduceAt({
          type: 'tool_call',
          data: { tool_call_id: call.id, name: call.name, arguments: call.raw_arguments },
        }, entry.timestamp)
        // Tool calls in a historical transcript are necessarily finished; a synthetic tool_result closes the step.
        reduceAt({ type: 'tool_result', data: { tool_call_id: call.id, name: call.name } }, entry.timestamp)
      }
      continue
    }
    // Compat with the early flat format: { role, content / payload }.
    const role = entry.role ?? ''
    const type = entry.type ?? ''
    const data = typeof entry.content === 'string' && entry.content
      ? entry.content
      : entry.payload ?? entry.content
    if (role === 'user') {
      timeline = finishRunAt(timeline, entry.timestamp)
      const text = typeof data === 'string' ? data : extractEventText(data)
      if (text) {
        const timestamp = parseTimestamp(entry.timestamp)
        timeline = appendUserMessage(timeline, text, entry.message_id, Number.isFinite(timestamp) ? timestamp : undefined)
      }
      continue
    }
    const eventType = type || (role === 'tool' ? 'tool_result' : 'message')
    reduceAt({ type: eventType, data }, entry.timestamp)
  }
  return timeline.map((item) => {
    if (item.kind !== 'run' || item.run.status !== 'running') return item
    const status = item.run.permissions.some((request) => !request.decision) ? 'cancelled' : 'done'
    return { ...item, run: closeRun(item.run, status) }
  })
}

function appendStatusStep(timeline: AgentTimeline, title: string, detail?: string): AgentTimeline {
  const withRun = ensureRun(timeline)
  const step: AgentWorkStep = { id: nextId('status'), kind: 'status', title, detail, status: 'done' }
  return updateLastRun(withRun, (run) => ({ ...run, steps: [...run.steps, step] }))
}
