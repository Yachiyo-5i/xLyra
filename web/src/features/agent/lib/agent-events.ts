import type { AgentTranscriptEntry } from '@/features/agent/api/agent'

export type AgentStepStatus = 'running' | 'done' | 'error'
export type AgentRunStatus = 'running' | 'done' | 'error' | 'cancelled'

export type AgentWorkStep = {
  id: string
  kind: 'tool' | 'thinking' | 'status'
  title: string
  detail?: string
  status: AgentStepStatus
}

export type AgentPermissionRequest = {
  id: string
  tool: string
  detail?: string
  /** 授权登记的解析路径/能力键（grant-access 接口的回传参数） */
  resolvedPath?: string
  decision?: 'allowed' | 'denied'
}

export type AgentRun = {
  id: string
  status: AgentRunStatus
  steps: AgentWorkStep[]
  finalText: string
  permissions: AgentPermissionRequest[]
}

export type AgentTimelineItem =
  | { kind: 'user'; id: string; text: string }
  | { kind: 'run'; id: string; run: AgentRun }

export type AgentTimeline = AgentTimelineItem[]

export type AgentStreamEvent = { type: string; data: unknown }

type EventKind =
  | 'text'
  | 'tool_call'
  | 'tool_result'
  | 'thinking'
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

export function extractEventText(data: unknown): string {
  if (typeof data === 'string') return data
  const record = asRecord(data)
  if (!record) return ''
  return pickString(record, ['text', 'content', 'message', 'delta'])
}

function classifyEvent(type: string, data: unknown): EventKind {
  const normalized = type.toLowerCase()
  // 提权协商事件即权限请求（载荷嵌套在 data.escalation）
  if (normalized.includes('escalation')) return 'permission'
  // 工具参数流式分片不参与时间线（步骤由 start/end/result 三个事件表达）
  if (normalized === 'tool_call_delta' || normalized === 'toolcall_delta') return 'other'
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
  const run: AgentRun = { id: nextId('run'), status: 'running', steps: [], finalText: '', permissions: [] }
  return [...timeline, { kind: 'run', id: run.id, run }]
}

function updateLastRun(timeline: AgentTimeline, updater: (run: AgentRun) => AgentRun): AgentTimeline {
  const current = lastRun(timeline)
  if (!current) return timeline
  const next = timeline.slice()
  next[current.index] = { kind: 'run', id: current.run.id, run: updater(current.run) }
  return next
}

function closeRun(run: AgentRun, status: AgentRunStatus): AgentRun {
  return {
    ...run,
    status,
    steps: run.steps.map((step) => (step.status === 'running' ? { ...step, status: status === 'error' ? 'error' : 'done' } : step)),
  }
}

export function appendUserMessage(timeline: AgentTimeline, text: string): AgentTimeline {
  return [...timeline, { kind: 'user', id: nextId('user'), text }]
}

export function reduceAgentEvent(timeline: AgentTimeline, event: AgentStreamEvent): AgentTimeline {
  const kind = classifyEvent(event.type, event.data)
  const record = asRecord(event.data)

  switch (kind) {
    case 'text': {
      const text = extractEventText(event.data)
      if (!text) return timeline
      const withRun = ensureRun(timeline)
      return updateLastRun(withRun, (run) => ({ ...run, finalText: run.finalText + text }))
    }
    case 'tool_call': {
      // runner 事件的载荷嵌套在 data.tool_call；平铺的 tool_* 事件直接用 data
      const payload = asRecord(record?.tool_call) ?? record
      const callId = pickId(payload, ['id', 'tool_call_id', 'call_id'])
      if (lastRun(timeline)?.run.steps.some((step) => step.id === callId && callId)) {
        // 完结事件（tool_call）：补齐参数详情
        const detail = pickDetail(payload, ['raw_arguments', 'arguments', 'args', 'input', 'command']) || undefined
        return updateLastRun(timeline, (run) => ({
          ...run,
          steps: run.steps.map((step) => (step.id === callId ? { ...step, detail: detail ?? step.detail } : step)),
        }))
      }
      const withRun = ensureRun(timeline)
      const step: AgentWorkStep = {
        id: callId || nextId('step'),
        kind: 'tool',
        title: pickString(payload, ['name', 'tool', 'tool_name', 'title']) || 'tool',
        detail: pickDetail(payload, ['raw_arguments', 'arguments', 'args', 'input', 'command']) || undefined,
        status: 'running',
      }
      return updateLastRun(withRun, (run) => ({ ...run, steps: [...run.steps, step] }))
    }
    case 'tool_result': {
      // runner 事件的载荷嵌套在 data.tool_result
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
          const step: AgentWorkStep = { id: stepId || nextId('step'), kind: 'tool', title: pickString(payload, ['name', 'tool', 'tool_name']) || 'tool', detail, status: isError ? 'error' : 'done' }
          return { ...run, steps: [...run.steps, step] }
        }
        const steps = run.steps.slice()
        steps[index] = { ...steps[index], detail: detail ?? steps[index].detail, status: isError ? 'error' : 'done' }
        return { ...run, steps }
      })
    }
    case 'thinking': {
      const text = extractEventText(event.data)
      if (!text) return timeline
      const withRun = ensureRun(timeline)
      return updateLastRun(withRun, (run) => {
        const last = run.steps[run.steps.length - 1]
        if (last?.kind === 'thinking') {
          const steps = run.steps.slice()
          steps[steps.length - 1] = { ...last, detail: `${last.detail ?? ''}${text}` }
          return { ...run, steps }
        }
        const step: AgentWorkStep = { id: nextId('thinking'), kind: 'thinking', title: 'thinking', detail: text, status: 'done' }
        return { ...run, steps: [...run.steps, step] }
      })
    }
    case 'permission': {
      const withRun = ensureRun(timeline)
      // escalation_request 的载荷嵌套在 data.escalation；平铺的 permission_* 事件直接用 data
      const payload = asRecord(record?.escalation) ?? record
      const command = payload?.requested_command
      const request: AgentPermissionRequest = {
        id: pickId(payload, ['escalation_id', 'request_id', 'permission_id', 'id']) || nextId('permission'),
        tool: pickString(payload, ['tool_name', 'tool', 'name', 'title']) || 'tool',
        detail: Array.isArray(command)
          ? command.map(String).join(' ')
          : pickDetail(payload, ['requested_command', 'requested_path', 'arguments', 'args', 'input', 'command', 'description', 'message']) || undefined,
        resolvedPath: pickString(payload, ['resolved_path']) || undefined,
      }
      return updateLastRun(withRun, (run) => ({ ...run, permissions: [...run.permissions, request] }))
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
      return updateLastRun(timeline, (run) => closeRun(run, 'done'))
    case 'error':
      return updateLastRun(timeline, (run) => closeRun(run, 'error'))
    case 'cancelled':
      return updateLastRun(timeline, (run) => closeRun(run, 'cancelled'))
    default:
      return timeline
  }
}

export function timelineFromTranscript(entries: AgentTranscriptEntry[]): AgentTimeline {
  // 中断收尾写入的固定回执文案（xlyra-agent sealPendingToolCalls）
  const INTERRUPTED_SEAL = '操作已被中断，工具未执行完成。'
  const AWAITING_SEAL = '等待用户授权，工具暂停执行。'
  let timeline: AgentTimeline = []
  // 当前 turn 是否包含提权请求：包含时，「中断」回执实为提权暂停，降级为中性
  let escalationInTurn = false
  for (const entry of entries) {
    if (entry.type === 'compaction') {
      // 上下文压缩记录：作为状态步骤挂在当前 run 下展示
      const summary = typeof entry.summary === 'string' ? entry.summary : ''
      timeline = appendStatusStep(timeline, 'compaction', summary || undefined)
      continue
    }
    if (entry.type === 'escalation') {
      // 提权请求：展示为权限条目；granted 为 null 表示尚未裁决（保持可交互）
      const withRun = ensureRun(timeline)
      const command = entry.requested_command
      const request: AgentPermissionRequest = {
        id: entry.escalation_id || nextId('permission'),
        tool: entry.tool_name || 'tool',
        detail: Array.isArray(command) && command.length
          ? command.join(' ')
          : entry.requested_path || undefined,
        resolvedPath: entry.resolved_path || undefined,
        decision: entry.granted === true ? 'allowed' : entry.granted === false ? 'denied' : undefined,
      }
      timeline = updateLastRun(withRun, (run) => ({ ...run, permissions: [...run.permissions, request] }))
      escalationInTurn = true
      continue
    }
    const message = entry.message
    if (message) {
      // runner 的真实转录格式：{ type: 'message', message: ChatMessage, ... }
      if (message.role === 'user') {
        if (message.content) timeline = appendUserMessage(timeline, message.content)
        escalationInTurn = false
        continue
      }
      if (message.role === 'system') continue
      if (message.role === 'tool') {
        // 兼容存量数据：提权暂停前写入的「中断」回执降级为中性（不标失败）
        const pausedByEscalation = escalationInTurn && message.is_error === true && message.content === INTERRUPTED_SEAL
        timeline = reduceAgentEvent(timeline, {
          type: 'tool_result',
          data: {
            tool_call_id: message.tool_call_id,
            name: message.name,
            content: pausedByEscalation ? AWAITING_SEAL : message.content,
            is_error: pausedByEscalation ? false : message.is_error,
          },
        })
        continue
      }
      // assistant：思考 → 工具调用 → 正文，按写入顺序还原
      if (message.thinking) timeline = reduceAgentEvent(timeline, { type: 'thinking', data: message.thinking })
      for (const call of message.tool_calls ?? []) {
        timeline = reduceAgentEvent(timeline, {
          type: 'tool_call',
          data: { tool_call_id: call.id, name: call.name, arguments: call.raw_arguments },
        })
        // 历史转录里的工具调用必然已完成：紧跟一条 tool_result 关闭步骤
        timeline = reduceAgentEvent(timeline, { type: 'tool_result', data: { tool_call_id: call.id, name: call.name } })
      }
      if (message.content) timeline = reduceAgentEvent(timeline, { type: 'message', data: message.content })
      continue
    }
    // 兼容早期的扁平格式：{ role, content / payload }
    const role = entry.role ?? ''
    const type = entry.type ?? ''
    const data = typeof entry.content === 'string' && entry.content
      ? entry.content
      : entry.payload ?? entry.content
    if (role === 'user') {
      const text = typeof data === 'string' ? data : extractEventText(data)
      if (text) timeline = appendUserMessage(timeline, text)
      continue
    }
    const eventType = type || (role === 'tool' ? 'tool_result' : 'message')
    timeline = reduceAgentEvent(timeline, { type: eventType, data })
  }
  return timeline.map((item) => (item.kind === 'run' && item.run.status === 'running' ? { ...item, run: closeRun(item.run, 'done') } : item))
}

function appendStatusStep(timeline: AgentTimeline, title: string, detail?: string): AgentTimeline {
  const withRun = ensureRun(timeline)
  const step: AgentWorkStep = { id: nextId('status'), kind: 'status', title, detail, status: 'done' }
  return updateLastRun(withRun, (run) => ({ ...run, steps: [...run.steps, step] }))
}
