import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  followServerConversation,
  listPlaygroundModels,
  type PlaygroundRolloutEvent,
} from '@/features/playground/api/playground'
import type { PlaygroundRun } from '@/features/playground/lib/types'

afterEach(() => {
  vi.unstubAllGlobals()
})

function jsonResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('listPlaygroundModels', () => {
  it('requests models through the admin endpoint with the API key id', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ data: [] }))
    vi.stubGlobal('fetch', fetchMock)

    await listPlaygroundModels('key-id-1')

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/playground/models?api_key_id=key-id-1',
      expect.objectContaining({ credentials: 'include' }),
    )
  })

  it('sorts models alphabetically without case sensitivity', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      data: [
        { id: 'zeta-model' },
        { id: 'Beta-model' },
        { id: 'alpha-model' },
      ],
    })))

    const models = await listPlaygroundModels('key-id-1')

    expect(models.map((model) => model.id)).toEqual(['alpha-model', 'Beta-model', 'zeta-model'])
  })

  it('preserves the mapped target for model aliases', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      data: [{
        id: 'codex-pro',
        metadata: { mapped_model: ' gpt-5.6-sol ' },
      }],
    })))

    const models = await listPlaygroundModels('key-id-1')

    expect(models[0].mappedModel).toBe('gpt-5.6-sol')
  })

  it('normalizes supported endpoint types', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      data: [{
        id: 'gpt-5.6-sol',
        metadata: { supported_endpoint_types: [' Chat ', 'RESPONSES', 42, ''] },
      }],
    })))

    const models = await listPlaygroundModels('key-id-1')

    expect(models[0].endpointTypes).toEqual(['chat', 'responses'])
  })
})

describe('followServerConversation', () => {
  function sseResponse(body: string): Response {
    return new Response(body, {
      status: 200,
      headers: { 'Content-Type': 'text/event-stream' },
    })
  }

  it('dispatches rollout events and terminal run status', async () => {
    const event: PlaygroundRolloutEvent = {
      timestamp: '2026-08-24T00:00:00Z',
      ordinal: 7,
      type: 'assistant_delta',
      payload: { message_id: 'm1', content: 'hello' },
    }
    const run: PlaygroundRun = { id: 'run-1', status: 'completed', created_at: 1 }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sseResponse(
      `id: 7\nevent: rollout\ndata: ${JSON.stringify(event)}\n\n` +
      `event: run_status\ndata: ${JSON.stringify(run)}\n\n`,
    )))

    const events: PlaygroundRolloutEvent[] = []
    const runs: PlaygroundRun[] = []
    await followServerConversation('conv-1', 0, new AbortController().signal, (item) => events.push(item), (item) => runs.push(item))

    expect(events).toHaveLength(1)
    expect(events[0].ordinal).toBe(7)
    expect(runs).toHaveLength(1)
    expect(runs[0].status).toBe('completed')
  })

  it('parses CRLF-delimited events and skips keepalive comments', async () => {
    const event: PlaygroundRolloutEvent = {
      timestamp: '2026-08-24T00:00:00Z',
      ordinal: 3,
      type: 'turn_completed',
      payload: { response_duration_ms: 12 },
    }
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(sseResponse(
      `: keepalive\r\n\r\nevent: rollout\r\ndata: ${JSON.stringify(event)}\r\n\r\n`,
    )))

    const events: PlaygroundRolloutEvent[] = []
    await followServerConversation('conv-1', 0, new AbortController().signal, (item) => events.push(item), vi.fn())

    expect(events).toHaveLength(1)
    expect(events[0].type).toBe('turn_completed')
  })
})
