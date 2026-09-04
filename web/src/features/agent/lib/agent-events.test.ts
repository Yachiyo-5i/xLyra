import { describe, expect, it } from 'vitest'

import {
  appendUserMessage,
  canReconcileTimeline,
  extractEventText,
  replaceFromUserMessage,
  reduceAgentEvent,
  reconcileTimeline,
  settleRunningRuns,
  timelineFromTranscript,
  type AgentTimeline,
} from '@/features/agent/lib/agent-events'

function reduceAll(events: Array<{ type: string; data: unknown }>, initial: AgentTimeline = []) {
  return events.reduce((timeline, event) => reduceAgentEvent(timeline, event), initial)
}

describe('extractEventText', () => {
  it('extracts text from strings and common keys', () => {
    expect(extractEventText('hello')).toBe('hello')
    expect(extractEventText({ text: 'a' })).toBe('a')
    expect(extractEventText({ delta: 'b' })).toBe('b')
    expect(extractEventText({ content: 'c' })).toBe('c')
    expect(extractEventText({ other: 1 })).toBe('')
    expect(extractEventText(null)).toBe('')
  })
})

describe('reduceAgentEvent', () => {
  it('accumulates assistant text into the active run', () => {
    const timeline = reduceAll([
      { type: 'message', data: { delta: '你好' } },
      { type: 'message', data: { delta: '，世界' } },
    ])
    expect(timeline).toHaveLength(1)
    const item = timeline[0]
    expect(item.kind).toBe('run')
    if (item.kind !== 'run') return
    expect(item.run.finalText).toBe('你好，世界')
    expect(item.run.status).toBe('running')
    // 连续文本合并为同一个 text step
    expect(item.run.steps).toHaveLength(1)
    expect(item.run.steps[0]).toMatchObject({ kind: 'text', detail: '你好，世界' })
  })

  it('interleaves text segments with tool steps in arrival order', () => {
    const timeline = reduceAll([
      { type: 'message', data: { delta: '先看下文件' } },
      { type: 'tool_call', data: { id: 'c1', tool: 'read_file', args: { path: '/a.ts' } } },
      { type: 'tool_result', data: { id: 'c1', output: 'ok' } },
      { type: 'message', data: { delta: '文件没问题' } },
      { type: 'agent_done', data: {} },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.finalText).toBe('先看下文件文件没问题')
    expect(item.run.steps.map((step) => step.kind)).toEqual(['text', 'tool', 'text'])
    expect(item.run.steps[0]?.detail).toBe('先看下文件')
    expect(item.run.steps[2]?.detail).toBe('文件没问题')
  })

  it('records the step position of permission requests', () => {
    const timeline = reduceAll([
      { type: 'message', data: { delta: '需要执行命令' } },
      { type: 'tool_call', data: { id: 'c1', tool: 'exec_command', args: { command: 'ls' } } },
      { type: 'escalation_request', data: { escalation: { escalation_id: 'esc-1', tool_name: 'exec_command', requested_command: ['ls'] } } },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.permissions[0]?.position).toBe(2)
  })

  it('groups tool calls as work steps and closes them on result', () => {
    const timeline = reduceAll([
      { type: 'tool_call', data: { id: 'c1', tool: 'read_file', args: { path: '/a.ts' } } },
      { type: 'tool_result', data: { id: 'c1', output: 'ok' } },
      { type: 'agent_done', data: {} },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.steps).toHaveLength(1)
    expect(item.run.steps[0].title).toBe('read_file')
    expect(item.run.steps[0].status).toBe('done')
    expect(item.run.steps[0].detail).toBe('ok')
    expect(item.run.status).toBe('done')
  })

  it('keeps the runner-provided elapsed time on completed runs', () => {
    const timeline = appendUserMessage([], '问题', 'message-1', 1_000)
    const completed = reduceAll([
      { type: 'agent_start', data: {} },
      { type: 'agent_done', data: { result: { elapsed_ms: 2_500 } } },
    ], timeline)
    const item = completed[1]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.startedAt).toBe(1_000)
    expect(item.run.elapsedMs).toBe(2_500)
  })

  it('maps runner wire events: nested tool_call/tool_result payloads and argument deltas', () => {
    const timeline = reduceAll([
      // Streaming argument chunks must not produce steps.
      { type: 'tool_call_start', data: { tool_call: { id: 'call_1', name: 'read_file' } } },
      { type: 'tool_call_delta', data: { tool_call_id: 'call_1', delta: '{"path":' } },
      { type: 'tool_call_delta', data: { tool_call_id: 'call_1', delta: '"/a.ts"}' } },
      // The completion event carries full arguments: backfill detail without duplicating the step.
      { type: 'tool_call', data: { tool_call: { id: 'call_1', name: 'read_file', raw_arguments: '{"path":"/a.ts"}' } } },
      { type: 'tool_result', data: { tool_result: { tool_call_id: 'call_1', name: 'read_file', output: 'file body', is_error: false } } },
      { type: 'agent_done', data: { result: { text: '读完了' } } },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.steps).toHaveLength(1)
    expect(item.run.steps[0]).toMatchObject({ title: 'read_file', status: 'done', detail: 'file body' })
    expect(item.run.steps[0].input).toBe('{"path":"/a.ts"}')
    expect(item.run.steps[0].output).toBe('file body')
    expect(item.run.status).toBe('done')
  })

  it('accumulates streaming tool arguments before the call is finalized', () => {
    const timeline = reduceAll([
      { type: 'tool_call_start', data: { tool_call: { id: 'c3', name: 'read_file' } } },
      { type: 'tool_call_delta', data: { tool_call_id: 'c3', delta: '{"path":' } },
      { type: 'tool_call_delta', data: { tool_call_id: 'c3', delta: '"/a.ts"}' } },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.steps[0]).toMatchObject({ input: '{"path":"/a.ts"}', argsDone: false, status: 'running' })
  })

  it('marks tool errors from nested tool_result payloads', () => {
    const timeline = reduceAll([
      { type: 'tool_call_start', data: { tool_call: { id: 'c2', name: 'exec_command' } } },
      { type: 'tool_result', data: { tool_result: { tool_call_id: 'c2', name: 'exec_command', output: 'boom', is_error: true } } },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.steps[0]).toMatchObject({ title: 'exec_command', status: 'error', detail: 'boom' })
  })

  it('starts a new run after a user message', () => {
    let timeline = reduceAll([
      { type: 'message', data: 'first answer' },
      { type: 'agent_done', data: {} },
    ])
    timeline = appendUserMessage(timeline, 'second question')
    timeline = reduceAll([{ type: 'message', data: 'second answer' }], timeline)
    expect(timeline).toHaveLength(3)
    const last = timeline[2]
    if (last.kind !== 'run') throw new Error('expected run')
    expect(last.run.finalText).toBe('second answer')
  })

  it('settles a lingering running run when the next user message arrives', () => {
    // 终态事件丢失（连接提前断开）时 run 停在 running：下一条用户消息必须先落定它，
    // 否则计时会跨轮继续走
    let timeline = reduceAll([
      { type: 'agent_start', data: {} },
      { type: 'message', data: 'answer' },
    ], appendUserMessage([], '问题', 'message-1', 1_000))
    const stuck = timeline[1]
    if (stuck.kind !== 'run') throw new Error('expected run')
    expect(stuck.run.status).toBe('running')
    timeline = appendUserMessage(timeline, '追问', 'message-2', 60_000)
    const settled = timeline[1]
    if (settled.kind !== 'run') throw new Error('expected run')
    expect(settled.run.status).toBe('done')
    expect(settled.run.elapsedMs).toBeDefined()
    // 落定时间以落定时刻为准，不能越过新消息的时间
    expect(settled.run.endedAt).toBeLessThanOrEqual(60_000 + Date.now())
  })

  it('settleRunningRuns closes only the still-running runs', () => {
    const timeline = reduceAll([
      { type: 'message', data: 'first answer' },
      { type: 'agent_done', data: { result: { elapsed_ms: 1_200 } } },
      // 第二个 run 因事件流中断停留在 running
      { type: 'message', data: 'second answer' },
    ], appendUserMessage(appendUserMessage([], 'q1', 'm1', 1_000), 'q2', 'm2', 10_000))
    const settled = settleRunningRuns(timeline)
    const runs = settled.filter((item) => item.kind === 'run')
    expect(runs[0]?.kind === 'run' && runs[0].run.elapsedMs).toBe(1_200)
    expect(runs[1]?.kind === 'run' && runs[1].run.status).toBe('done')
    expect(runs[1]?.kind === 'run' && runs[1].run.elapsedMs).toBeGreaterThanOrEqual(0)
  })

  it('continues elapsed time when an escalation-paused run resumes', () => {
    const initial = appendUserMessage([], '需要授权的操作', 'message-1', 1_000)
    const paused = reduceAll([
      { type: 'agent_start', data: {} },
      { type: 'escalation_request', data: { escalation: { escalation_id: 'esc-1', tool_name: 'exec_command', requested_path: '/tmp', resolved_path: '/tmp' } } },
      { type: 'agent_cancelled', data: {} },
    ], initial)
    const pausedRun = paused[1]
    if (pausedRun.kind !== 'run') throw new Error('expected paused run')
    const approved = paused.map((item) => item.kind === 'run'
      ? { ...item, run: { ...item.run, permissions: item.run.permissions.map((request) => ({ ...request, decision: 'allowed' as const })) } }
      : item)
    const resumed = reduceAgentEvent(approved, { type: 'agent_start', data: {} })
    expect(resumed).toHaveLength(2)
    const resumedRun = resumed[1]
    if (resumedRun.kind !== 'run') throw new Error('expected resumed run')
    expect(resumedRun.id).toBe(pausedRun.id)
    expect(resumedRun.run.elapsedMs).toBe(pausedRun.run.elapsedMs)
    expect(resumedRun.run.startedAt).toBeGreaterThanOrEqual(pausedRun.run.startedAt)
    const completed = reduceAgentEvent(resumed, { type: 'agent_done', data: { result: { elapsed_ms: 500 } } })
    const completedRun = completed[1]
    if (completedRun.kind !== 'run') throw new Error('expected completed run')
    expect(completedRun.run.elapsedMs).toBe((pausedRun.run.elapsedMs ?? 0) + 500)
  })

  it('truncates the timeline when replacing a user message', () => {
    const timeline = appendUserMessage(appendUserMessage([], '第一问', 'message-1'), '第二问', 'message-2')
    const replaced = replaceFromUserMessage(timeline, 'message-1', '改写后的问题')
    expect(replaced).toHaveLength(1)
    expect(replaced[0]).toMatchObject({ kind: 'user', id: 'message-1', messageId: 'message-1', text: '改写后的问题' })
  })

  it('rejects a stale transcript whose edited user message no longer matches', () => {
    const current = appendUserMessage([], '改写后的问题', 'message-1', 1_000)
    const stale = appendUserMessage([], '原始问题', 'message-1', 1_000)
    expect(canReconcileTimeline(current, stale)).toBe(false)
  })

  it('preserves live elapsed time when reconciling the completed transcript', () => {
    const current = reduceAll([
      { type: 'agent_start', data: {} },
      { type: 'agent_done', data: { result: { elapsed_ms: 2_500 } } },
    ], appendUserMessage([], '问题', 'message-1', 1_000))
    const transcript = timelineFromTranscript([
      { type: 'message', timestamp: '1970-01-01T00:00:01.000Z', message_id: 'message-1', message: { role: 'user', content: '问题' } },
      { type: 'message', timestamp: '1970-01-01T00:00:20.000Z', message: { role: 'assistant', content: '回答' } },
    ])
    const reconciled = reconcileTimeline(current, transcript)
    const run = reconciled[1]
    if (run.kind !== 'run') throw new Error('expected run')
    expect(run.run.elapsedMs).toBe(2_500)
  })

  it('collects permission requests and resolves them', () => {
    const timeline = reduceAll([
      { type: 'permission_request', data: { request_id: 'p1', tool: 'bash', input: 'rm -rf /tmp/x' } },
      { type: 'permission_result', data: { request_id: 'p1', decision: 'allow' } },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.permissions).toHaveLength(1)
    expect(item.run.permissions[0].tool).toBe('bash')
    expect(item.run.permissions[0].decision).toBe('allowed')
  })

  it('maps live escalation_request events (nested payload) to pending permission cards', () => {
    const timeline = reduceAll([
      {
        type: 'escalation_request',
        data: {
          escalation: {
            escalation_id: 'esc-1',
            requested_path: '~/secrets',
            resolved_path: '/Users/x/secrets',
            tool_name: 'read_file',
            resource_type: 'path',
            workdir: '/workspace',
          },
        },
      },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.permissions).toHaveLength(1)
    expect(item.run.permissions[0]).toMatchObject({
      id: 'esc-1',
      tool: 'read_file',
      detail: '~/secrets',
      resolvedPath: '/Users/x/secrets',
    })
    expect(item.run.permissions[0].decision).toBeUndefined()
  })

  it('maps command escalation requests with joined command detail', () => {
    const timeline = reduceAll([
      {
        type: 'escalation_request',
        data: {
          escalation: {
            escalation_id: 'esc-2',
            requested_path: 'ls -la',
            resolved_path: 'capability://exec_command',
            tool_name: 'exec_command',
            resource_type: 'command',
            requested_command: ['ls', '-la'],
            workdir: '/workspace',
          },
        },
      },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.permissions[0].detail).toBe('ls -la')
    expect(item.run.permissions[0].resolvedPath).toBe('capability://exec_command')
  })

  it('closes open steps on cancel', () => {
    const timeline = reduceAll([
      { type: 'tool_call', data: { name: 'bash' } },
      { type: 'agent_cancelled', data: {} },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.status).toBe('cancelled')
    expect(item.run.steps[0].status).toBe('done')
  })
})

describe('timelineFromTranscript', () => {
  it('restores user attachments from the persisted transcript', () => {
    const timeline = timelineFromTranscript([
      {
        type: 'message',
        message_id: 'message-attachment',
        timestamp: '2026-09-03T00:00:00.000Z',
        message: {
          role: 'user',
          content: '请查看这个文件',
          attachments: [{
            name: 'report.pdf',
            mime_type: 'application/pdf',
            data_url: 'data:application/pdf;base64,AA==',
          }],
        },
      },
    ])
    expect(timeline[0]).toMatchObject({
      kind: 'user',
      text: '请查看这个文件',
      attachments: [{ name: 'report.pdf', mimeType: 'application/pdf', dataURL: 'data:application/pdf;base64,AA==' }],
    })
  })

  it('keeps attachments when replacing a user message for replay', () => {
    const initial = timelineFromTranscript([{
      type: 'message',
      message_id: 'message-attachment',
      message: {
        role: 'user',
        content: '原始问题',
        attachments: [{ name: 'report.pdf', mime_type: 'application/pdf', data_url: 'data:application/pdf;base64,AA==' }],
      },
    }])
    const replaced = replaceFromUserMessage(initial, 'message-attachment', '重放问题')
    expect(replaced[0]).toMatchObject({
      kind: 'user',
      text: '重放问题',
      attachments: [{ name: 'report.pdf', mimeType: 'application/pdf' }],
    })
  })

  it('maps transcript entries to a closed timeline', () => {
    const timeline = timelineFromTranscript([
      { role: 'user', content: '问题' },
      { role: 'assistant', content: '回答' },
      { role: 'tool', type: 'tool_call', payload: { name: 'read_file' } },
    ])
    expect(timeline[0]).toMatchObject({ kind: 'user', text: '问题' })
    const run = timeline[1]
    if (run.kind !== 'run') throw new Error('expected run')
    expect(run.run.finalText).toBe('回答')
    expect(run.run.steps.map((step) => step.kind)).toEqual(['text', 'tool'])
    expect(run.run.steps[1].title).toBe('read_file')
    expect(run.run.status).toBe('done')
  })

  it('parses runner transcript envelope (type=message with nested ChatMessage)', () => {
    const timeline = timelineFromTranscript([
      { type: 'message', message: { role: 'user', content: '帮我看下 a.ts' } },
      {
        type: 'message',
        message: {
          role: 'assistant',
          content: '',
          thinking: '先读文件',
          tool_calls: [{ id: 'call_1', name: 'read_file', raw_arguments: '{"path":"/a.ts"}' }],
        },
      },
      { type: 'message', message: { role: 'tool', tool_call_id: 'call_1', name: 'read_file', content: 'file body' } },
      { type: 'message', message: { role: 'assistant', content: '文件没问题' } },
    ])
    expect(timeline[0]).toMatchObject({ kind: 'user', text: '帮我看下 a.ts' })
    const run = timeline[1]
    if (run.kind !== 'run') throw new Error('expected run')
    expect(run.run.finalText).toBe('文件没问题')
    // thinking 不进时间线，只有 tool step + text step，按到达顺序交错
    expect(run.run.steps.map((step) => step.kind)).toEqual(['tool', 'text'])
    const tool = run.run.steps.find((step) => step.kind === 'tool')
    expect(tool?.title).toBe('read_file')
    expect(tool?.detail).toBe('file body')
    expect(tool?.status).toBe('done')
    expect(run.run.status).toBe('done')
  })

  it('restores text before tool calls within one assistant message', () => {
    const timeline = timelineFromTranscript([
      { type: 'message', message: { role: 'user', content: 'q' } },
      {
        type: 'message',
        message: {
          role: 'assistant',
          content: '我先读文件',
          tool_calls: [{ id: 'c1', name: 'read_file', raw_arguments: '{}' }],
        },
      },
      { type: 'message', message: { role: 'tool', tool_call_id: 'c1', name: 'read_file', content: 'body' } },
      { type: 'message', message: { role: 'assistant', content: '看完了' } },
    ])
    const run = timeline[1]
    if (run.kind !== 'run') throw new Error('expected run')
    expect(run.run.steps.map((step) => [step.kind, step.detail])).toEqual([
      ['text', '我先读文件'],
      ['tool', 'body'],
      ['text', '看完了'],
    ])
  })

  it('marks historical tool errors and renders compaction/escalation entries', () => {
    const timeline = timelineFromTranscript([
      { type: 'message', message: { role: 'user', content: '跑一下' } },
      {
        type: 'message',
        message: { role: 'assistant', content: '', tool_calls: [{ id: 'c9', name: 'bash', raw_arguments: '{"cmd":"ls"}' }] },
      },
      { type: 'message', message: { role: 'tool', tool_call_id: 'c9', content: 'boom', is_error: true } },
      { type: 'compaction', summary: '前文摘要' },
      {
        type: 'escalation',
        escalation_id: 'e1',
        tool_name: 'bash',
        requested_command: ['rm', '-rf', '/tmp/x'],
        resolved_path: 'capability://exec_command',
        granted: false,
      },
      {
        type: 'escalation',
        escalation_id: 'e2',
        tool_name: 'read_file',
        requested_path: '~/secrets',
        resolved_path: '/Users/x/secrets',
        granted: null,
      },
    ])
    const run = timeline[1]
    if (run.kind !== 'run') throw new Error('expected run')
    expect(run.run.steps.find((step) => step.kind === 'tool')?.status).toBe('error')
    // 压缩条目先收束回复 run，再开独立的压缩 run；后续提权也挂在该 run 上
    const compactRun = timeline[2]
    if (compactRun?.kind !== 'run') throw new Error('expected compact run')
    expect(compactRun.run.steps.find((step) => step.kind === 'status')).toMatchObject({ title: 'compacted', detail: undefined })
    expect(compactRun.run.permissions[0]).toMatchObject({ id: 'e1', tool: 'bash', detail: 'rm -rf /tmp/x', decision: 'denied' })
    // An undecided escalation (granted: null) stays interactive and must not render as denied.
    expect(compactRun.run.permissions[1]).toMatchObject({ id: 'e2', tool: 'read_file', resolvedPath: '/Users/x/secrets' })
    expect(compactRun.run.permissions[1].decision).toBeUndefined()
  })

  it('downgrades legacy interrupted seal receipts to neutral when an escalation paused the run', () => {
    const timeline = timelineFromTranscript([
      { type: 'message', message: { role: 'user', content: '克隆仓库' } },
      {
        type: 'message',
        message: { role: 'assistant', content: '', tool_calls: [{ id: 'c1', name: 'exec_command', raw_arguments: '{"command":["git","clone"]}' }] },
      },
      { type: 'escalation', escalation_id: 'e1', tool_name: 'exec_command', requested_command: ['git', 'clone'], resolved_path: 'capability://exec_command', granted: true },
      // Error receipts written by older versions during an escalation pause must not be marked failed.
      { type: 'message', message: { role: 'tool', tool_call_id: 'c1', name: 'exec_command', content: '操作已被中断，工具未执行完成。', is_error: true } },
    ])
    const run = timeline[1]
    if (run.kind !== 'run') throw new Error('expected run')
    const step = run.run.steps.find((s) => s.kind === 'tool')
    expect(step?.status).toBe('done')
    expect(step?.detail).toBe('等待用户授权，工具暂停执行。')
    // A plain cancellation without escalation still renders as failed.
    const cancelled = timelineFromTranscript([
      { type: 'message', message: { role: 'user', content: '跑一下' } },
      {
        type: 'message',
        message: { role: 'assistant', content: '', tool_calls: [{ id: 'c2', name: 'read', raw_arguments: '{}' }] },
      },
      { type: 'message', message: { role: 'tool', tool_call_id: 'c2', name: 'read', content: '操作已被中断，工具未执行完成。', is_error: true } },
    ])
    const cancelledRun = cancelled[1]
    if (cancelledRun.kind !== 'run') throw new Error('expected run')
    expect(cancelledRun.run.steps.find((s) => s.kind === 'tool')?.status).toBe('error')
  })

  it('multi-turn transcript splits runs at user messages', () => {    const timeline = timelineFromTranscript([
      { type: 'message', message: { role: 'user', content: '第一问' } },
      { type: 'message', message: { role: 'assistant', content: '第一答' } },
      { type: 'message', message: { role: 'user', content: '第二问' } },
      { type: 'message', message: { role: 'assistant', content: '第二答' } },
    ])
    expect(timeline).toHaveLength(4)
    const first = timeline[1]
    const second = timeline[3]
    if (first.kind !== 'run' || second.kind !== 'run') throw new Error('expected runs')
    expect(first.run.finalText).toBe('第一答')
    expect(second.run.finalText).toBe('第二答')
  })

  it('derives stable run timing and preserves transcript message ids', () => {
    const timeline = timelineFromTranscript([
      { type: 'message', message_id: 'message-1', timestamp: '2026-09-03T00:00:00.000Z', message: { role: 'user', content: '第一问' } },
      { type: 'message', timestamp: '2026-09-03T00:00:02.500Z', message: { role: 'assistant', content: '第一答' } },
    ])
    expect(timeline[0]).toMatchObject({ kind: 'user', messageId: 'message-1', id: 'message-1', createdAt: Date.parse('2026-09-03T00:00:00.000Z') })
    const run = timeline[1]
    if (run.kind !== 'run') throw new Error('expected run')
    expect(run.run.startedAt).toBe(Date.parse('2026-09-03T00:00:00.000Z'))
    expect(run.run.elapsedMs).toBe(2_500)
  })

  it('uses timestamps from legacy flat entries for user messages and run duration', () => {
    const timeline = timelineFromTranscript([
      { role: 'user', message_id: 'message-legacy', content: '第一问', timestamp: '2026-09-03T00:00:00.000Z' },
      { role: 'assistant', content: '第一答', timestamp: '2026-09-03T00:00:02.500Z' },
    ])
    expect(timeline[0]).toMatchObject({ createdAt: Date.parse('2026-09-03T00:00:00.000Z'), messageId: 'message-legacy' })
    const run = timeline[1]
    if (run.kind !== 'run') throw new Error('expected run')
    expect(run.run.elapsedMs).toBe(2_500)
  })

  it('excludes the idle gap before the next user message from run duration', () => {
    // 第一轮 0~2.5s 结束，用户 10 分钟后才发第二问：第一轮的用时必须是 2.5s，
    // 而不是「从第一问到第二问」的 10 分钟
    const timeline = timelineFromTranscript([
      { type: 'message', message_id: 'message-1', timestamp: '2026-09-03T00:00:00.000Z', message: { role: 'user', content: '第一问' } },
      { type: 'message', timestamp: '2026-09-03T00:00:02.500Z', message: { role: 'assistant', content: '第一答' } },
      { type: 'message', message_id: 'message-2', timestamp: '2026-09-03T00:10:00.000Z', message: { role: 'user', content: '第二问' } },
      { type: 'message', timestamp: '2026-09-03T00:10:01.000Z', message: { role: 'assistant', content: '第二答' } },
    ])
    const first = timeline[1]
    if (first.kind !== 'run') throw new Error('expected run')
    expect(first.run.endedAt).toBe(Date.parse('2026-09-03T00:00:02.500Z'))
    expect(first.run.elapsedMs).toBe(2_500)
  })
})

describe('stall/budget 提醒', () => {
  it('stall_detected/budget_notice 不进时间线（内部机制不对用户展示）', () => {
    const timeline = reduceAll([
      { type: 'tool_call', data: { id: 'c1', tool: 'read_file', args: { path: '/a.ts' } } },
      { type: 'stall_detected', data: { notice: '检测到重复调用 read_file', repeated_tool: 'read_file' } },
      { type: 'budget_notice', data: { notice: '已使用 50% 步数', step: 100, max_steps: 200 } },
      { type: 'message', data: { delta: '继续处理' } },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.steps.map((step) => step.kind)).toEqual(['tool', 'text'])
    expect(item.run.finalText).toBe('继续处理')
  })

  it('转录里 name=agent_notice 的 user 消息被忽略且不结束 run', () => {
    const timeline = timelineFromTranscript([
      { type: 'message', message: { role: 'user', content: '问题' } },
      { type: 'message', message: { role: 'assistant', content: '先做一步', tool_calls: [{ id: 'c1', name: 'read_file', raw_arguments: '{}' }] } },
      { type: 'message', message: { role: 'tool', tool_call_id: 'c1', name: 'read_file', content: 'ok' } },
      { type: 'message', message: { role: 'user', name: 'agent_notice', content: '检测到停滞' } },
      { type: 'message', message: { role: 'assistant', content: '继续回答' } },
    ])
    // 只有一条真实用户消息
    expect(timeline.filter((item) => item.kind === 'user')).toHaveLength(1)
    const run = timeline[1]
    if (run.kind !== 'run') throw new Error('expected run')
    expect(run.run.steps.map((step) => step.kind)).toEqual(['text', 'tool', 'text'])
    expect(run.run.finalText).toBe('先做一步继续回答')
    expect(run.run.status).toBe('done')
  })
})

describe('上下文压缩状态', () => {
  it('自动压缩：compacting → compacted 状态步骤落定（只显示状态文本）', () => {
    const timeline = reduceAll([
      { type: 'tool_call', data: { id: 'c1', tool: 'read_file', args: {} } },
      { type: 'context_compacting', data: {} },
      { type: 'context_compacted', data: { compaction: { summary: '摘要', tokens_before: 180000, tokens_after: 30000 } } },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.steps.map((step) => [step.kind, step.title, step.status])).toEqual([
      ['tool', 'read_file', 'running'],
      ['status', 'compacted', 'done'],
    ])
    expect(item.run.steps[1]?.detail).toBeUndefined()
  })

  it('压缩失败（无 compaction 载荷）状态步骤落定为跳过', () => {
    const timeline = reduceAll([
      { type: 'context_compacting', data: {} },
      { type: 'context_compacted', data: {} },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.steps[0]).toMatchObject({ kind: 'status', title: 'compaction_skipped', status: 'done' })
  })

  it('手动压缩：残留的 running run 先落定，压缩状态独占新 run', () => {
    const timeline = reduceAll([
      { type: 'message', data: { delta: '上一轮回复' } },
    ])
    const compacting = reduceAll([{ type: 'context_compacting', data: { manual: true } }], timeline)
    expect(compacting).toHaveLength(2)
    const first = compacting[0]
    const second = compacting[1]
    if (first?.kind !== 'run' || second?.kind !== 'run') throw new Error('expected runs')
    // 上一轮 run 被收束为 done，压缩状态在新 run 里
    expect(first.run.status).toBe('done')
    expect(second.run.status).toBe('running')
    expect(second.run.steps[0]).toMatchObject({ kind: 'status', title: 'compacting_manual', status: 'running' })
    expect(first.run.steps.some((step) => step.kind === 'status')).toBe(false)
  })

  it('转录里的 compaction 条目回放为独立的顶层压缩 run（不进上一轮回复）', () => {
    const timeline = timelineFromTranscript([
      { type: 'message', message: { role: 'user', content: '问题' } },
      { type: 'message', message: { role: 'assistant', content: '回答' } },
      { type: 'compaction', summary: '前文摘要', tokens_before: 150000, tokens_after: 20000 },
    ])
    expect(timeline).toHaveLength(3)
    const replyRun = timeline[1]
    const compactRun = timeline[2]
    if (replyRun?.kind !== 'run' || compactRun?.kind !== 'run') throw new Error('expected runs')
    // 回复 run 不含压缩状态；压缩 run 只有一个已压缩状态步骤
    expect(replyRun.run.steps.some((s) => s.kind === 'status')).toBe(false)
    expect(compactRun.run.steps).toHaveLength(1)
    expect(compactRun.run.steps[0]).toMatchObject({ title: 'compacted', status: 'done', detail: undefined })
  })
})
