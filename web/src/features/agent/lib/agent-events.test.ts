import { describe, expect, it } from 'vitest'

import {
  appendUserMessage,
  extractEventText,
  reduceAgentEvent,
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

  it('maps runner wire events: nested tool_call/tool_result payloads, deltas skipped', () => {
    const timeline = reduceAll([
      // 参数流式分片：不得产生步骤
      { type: 'tool_call_start', data: { tool_call: { id: 'call_1', name: 'read_file' } } },
      { type: 'tool_call_delta', data: { tool_call_id: 'call_1', delta: '{"path":' } },
      { type: 'tool_call_delta', data: { tool_call_id: 'call_1', delta: '"/a.ts"}' } },
      // 完结事件带全量参数：补齐详情，不重复建步骤
      { type: 'tool_call', data: { tool_call: { id: 'call_1', name: 'read_file', raw_arguments: '{"path":"/a.ts"}' } } },
      { type: 'tool_result', data: { tool_result: { tool_call_id: 'call_1', name: 'read_file', output: 'file body', is_error: false } } },
      { type: 'agent_done', data: { result: { text: '读完了' } } },
    ])
    const item = timeline[0]
    if (item.kind !== 'run') throw new Error('expected run')
    expect(item.run.steps).toHaveLength(1)
    expect(item.run.steps[0]).toMatchObject({ title: 'read_file', status: 'done', detail: 'file body' })
    expect(item.run.status).toBe('done')
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
    expect(run.run.steps[0].title).toBe('read_file')
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
    // thinking 步骤 + 工具步骤（tool_call 后紧跟的 tool_result 把它关闭）
    const thinking = run.run.steps.find((step) => step.kind === 'thinking')
    expect(thinking?.detail).toBe('先读文件')
    const tool = run.run.steps.find((step) => step.kind === 'tool')
    expect(tool?.title).toBe('read_file')
    expect(tool?.detail).toBe('file body')
    expect(tool?.status).toBe('done')
    expect(run.run.status).toBe('done')
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
    expect(run.run.steps.find((step) => step.kind === 'status')).toMatchObject({ title: 'compaction', detail: '前文摘要' })
    expect(run.run.permissions[0]).toMatchObject({ id: 'e1', tool: 'bash', detail: 'rm -rf /tmp/x', decision: 'denied' })
    // 未裁决的提权（granted: null）保持可交互，不得显示为已拒绝
    expect(run.run.permissions[1]).toMatchObject({ id: 'e2', tool: 'read_file', resolvedPath: '/Users/x/secrets' })
    expect(run.run.permissions[1].decision).toBeUndefined()
  })

  it('downgrades legacy interrupted seal receipts to neutral when an escalation paused the run', () => {
    const timeline = timelineFromTranscript([
      { type: 'message', message: { role: 'user', content: '克隆仓库' } },
      {
        type: 'message',
        message: { role: 'assistant', content: '', tool_calls: [{ id: 'c1', name: 'exec_command', raw_arguments: '{"command":["git","clone"]}' }] },
      },
      { type: 'escalation', escalation_id: 'e1', tool_name: 'exec_command', requested_command: ['git', 'clone'], resolved_path: 'capability://exec_command', granted: true },
      // 旧版本在提权暂停时写入的错误回执：不应标为失败
      { type: 'message', message: { role: 'tool', tool_call_id: 'c1', name: 'exec_command', content: '操作已被中断，工具未执行完成。', is_error: true } },
    ])
    const run = timeline[1]
    if (run.kind !== 'run') throw new Error('expected run')
    const step = run.run.steps.find((s) => s.kind === 'tool')
    expect(step?.status).toBe('done')
    expect(step?.detail).toBe('等待用户授权，工具暂停执行。')
    // 无提权的普通取消仍按失败展示
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
})
