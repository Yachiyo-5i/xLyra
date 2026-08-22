import { describe, expect, it } from 'vitest'
import type {
  RequestActivityRequest,
  RequestLogItem,
  RequestLogListFilters,
} from '@/features/requests/api/requests'
import {
  decodeRequestActivityEvent,
  decodeRequestActivitySnapshot,
  initialRequestActivityState,
  mergeRequestLogItems,
  removeRequestActivityRequest,
  reduceRequestActivityEvent,
  reduceRequestActivitySnapshot,
  requestLogItemFromActivity,
} from '@/features/requests/lib/request-live'

function activity(overrides: Partial<RequestActivityRequest> = {}): RequestActivityRequest {
  return {
    request_id: 'request-1',
    api_key_id: 'key-1',
    api_key_name: 'Primary key',
    model_key: 'gpt-4o-mini',
    model_provider: 'openai',
    upstream_site_id: 'site-1',
    upstream_site_name: 'Primary site',
    upstream_site_type: 'openai',
    attempt: 1,
    stream: true,
    phase: 'responding',
    started_at: '2026-08-17T08:00:00.000Z',
    updated_at: '2026-08-17T08:00:01.000Z',
    ...overrides,
  }
}

function historyItem(overrides: Partial<RequestLogItem> = {}): RequestLogItem {
  return {
    id: 'history-1',
    request_id: 'request-1',
    success: true,
    api_key: {},
    site: {},
    model: {},
    usage: {},
    created_at: '2026-08-17T07:59:00.000Z',
    ...overrides,
  }
}

function allFilters(overrides: RequestLogListFilters = {}): RequestLogListFilters {
  return { success: null, ...overrides }
}

describe('request activity decoding', () => {
  it('decodes a snapshot and ignores malformed requests', () => {
    const snapshot = decodeRequestActivitySnapshot(JSON.stringify({
      sequence: 7,
      requests: [
        activity(),
        { request_id: 'missing-phase' },
      ],
    }))

    expect(snapshot).toEqual({ sequence: 7, requests: [activity()] })
    expect(decodeRequestActivitySnapshot('{invalid')).toBeNull()
  })

  it('decodes upsert, remove, and usage events', () => {
    expect(decodeRequestActivityEvent('upsert', JSON.stringify({ sequence: 8, request: activity() }))).toEqual({
      sequence: 8,
      type: 'upsert',
      request: activity(),
    })
    expect(decodeRequestActivityEvent('remove', JSON.stringify({ sequence: 9, request_id: 'request-1' }))).toEqual({
      sequence: 9,
      type: 'remove',
      request_id: 'request-1',
    })
    expect(decodeRequestActivityEvent('usage', JSON.stringify({ sequence: 10 }))).toEqual({
      sequence: 10,
      type: 'usage',
    })
  })
})

describe('request activity reducer', () => {
  it('upserts and removes requests while ignoring stale sequences', () => {
    const first = reduceRequestActivitySnapshot(initialRequestActivityState(), {
      sequence: 1,
      requests: [activity()],
    })
    const updated = activity({ phase: 'routed', updated_at: '2026-08-17T08:00:02.000Z' })
    const second = reduceRequestActivityEvent(first, { sequence: 2, type: 'upsert', request: updated })

    expect(second.requests['request-1']).toEqual(updated)
    expect(second.startedAtByRequestID).toEqual({ 'request-1': activity().started_at })
    expect(reduceRequestActivityEvent(second, { sequence: 2, type: 'remove', request_id: 'request-1' })).toBe(second)

    const third = reduceRequestActivityEvent(second, { sequence: 3, type: 'remove', request_id: 'request-1' })
    expect(third.requests).toEqual({})
    expect(third.startedAtByRequestID).toEqual({ 'request-1': activity().started_at })
  })

  it('keeps a terminal request until the history refresh completes', () => {
    const state = reduceRequestActivitySnapshot(initialRequestActivityState(), {
      sequence: 1,
      requests: [activity()],
    })
    const terminal = reduceRequestActivityEvent(state, {
      sequence: 2,
      type: 'upsert',
      request: activity({ phase: 'completed' }),
    })

    expect(terminal.requests['request-1']).toEqual(activity({ phase: 'completed' }))
    expect(removeRequestActivityRequest(terminal, 'request-1').requests).toEqual({})
  })

  it('does not let an older snapshot replace newer state', () => {
    const state = reduceRequestActivitySnapshot(initialRequestActivityState(), {
      sequence: 3,
      requests: [activity({ phase: 'routed' })],
    })
    const newer = reduceRequestActivityEvent(state, {
      sequence: 4,
      type: 'upsert',
      request: activity({ phase: 'responding' }),
    })

    expect(reduceRequestActivitySnapshot(newer, {
      sequence: 3,
      requests: [],
    })).toBe(newer)
  })
})

describe('request log live projection', () => {
  it('projects an in-progress request without token data', () => {
    const item = requestLogItemFromActivity(activity({ attempt: 2, stream: false }))

    expect(item).toMatchObject({
      id: 'live:request-1',
      request_id: 'request-1',
      parent_request_id: 'request-1',
      state: 'in_progress',
      phase: 'responding',
      is_live: true,
      display_started_at: '2026-08-17T08:00:00.000Z',
      success: false,
      attempt: 2,
      stream: false,
      response_mode: 'non_stream',
      api_key: { id: 'key-1', name: 'Primary key' },
      site: { id: 'site-1', name: 'Primary site', site_type: 'openai' },
      model: { canonical_model: 'gpt-4o-mini', display_name: 'gpt-4o-mini' },
      usage: {},
    })
    expect(item.request_tokens).toBeUndefined()
    expect(item.response_tokens).toBeUndefined()
  })

  it('merges live rows only on the first all-status page', () => {
    const live = activity()
    const sameRequestHistory = historyItem()
    const otherHistory = historyItem({
      id: 'history-2',
      request_id: 'request-2',
      created_at: '2026-08-17T07:58:00.000Z',
    })

    expect(mergeRequestLogItems([sameRequestHistory, otherHistory], [live], allFilters(), 1).map((item) => item.id)).toEqual([
      'live:request-1',
      'history-2',
    ])
    expect(mergeRequestLogItems([sameRequestHistory, otherHistory], [live], allFilters(), 2)).toEqual([
      sameRequestHistory,
      otherHistory,
    ])
  })

  it('hands a completed live row to history in place without a duplicate parent row', () => {
    const terminal = activity({ phase: 'completed' })
    const otherHistory = historyItem({
      id: 'history-2',
      request_id: 'request-2',
      parent_request_id: 'request-2',
      created_at: '2026-08-17T08:00:02.000Z',
    })
    const completedHistory = historyItem({
      id: 'history-completed',
      parent_request_id: 'request-1',
      created_at: '2026-08-17T08:00:03.000Z',
    })

    const items = mergeRequestLogItems(
      [otherHistory, completedHistory],
      [terminal],
      allFilters(),
      1,
      new Set(['request-1']),
    )

    expect(items[1]).toMatchObject({
      id: 'history-completed',
      display_key: 'live:request-1',
      display_started_at: '2026-08-17T08:00:00.000Z',
    })
    expect(items.map((item) => item.id)).toEqual(['history-2', 'history-completed'])
    expect(items.filter((item) => (item.parent_request_id ?? item.request_id) === 'request-1')).toHaveLength(1)
  })

  it('sorts live rows by their start time instead of their most recently updated time', () => {
    const olderRequest = activity({
      request_id: 'request-older',
      started_at: '2026-08-17T08:00:00.000Z',
      updated_at: '2026-08-17T08:05:00.000Z',
    })
    const newerRequest = activity({
      request_id: 'request-newer',
      started_at: '2026-08-17T08:04:00.000Z',
      updated_at: '2026-08-17T08:04:30.000Z',
    })

    expect(mergeRequestLogItems([], [newerRequest, olderRequest], allFilters(), 1).map((item) => item.request_id)).toEqual([
      'request-newer',
      'request-older',
    ])
  })

  it('sorts live and history rows together by their start time', () => {
    const live = activity({
      request_id: 'request-live',
      started_at: '2026-08-17T08:03:00.000Z',
      updated_at: '2026-08-17T08:09:00.000Z',
    })
    const history = [
      historyItem({
        id: 'history-newer',
        request_id: 'request-newer',
        started_at: '2026-08-17T08:05:00.000Z',
        created_at: '2026-08-17T08:05:30.000Z',
      }),
      historyItem({
        id: 'history-older',
        request_id: 'request-older',
        started_at: '2026-08-17T08:01:00.000Z',
        created_at: '2026-08-17T08:08:00.000Z',
      }),
    ]

    expect(mergeRequestLogItems(history, [live], allFilters(), 1).map((item) => item.request_id)).toEqual([
      'request-newer',
      'request-live',
      'request-older',
    ])
  })

  it('keeps a completed history row at its observed live start position', () => {
    const completed = historyItem({
      id: 'history-completed',
      request_id: 'request-1',
      parent_request_id: 'request-1',
      created_at: '2026-08-17T08:10:00.000Z',
    })
    const other = historyItem({
      id: 'history-other',
      request_id: 'request-2',
      started_at: '2026-08-17T08:05:00.000Z',
      created_at: '2026-08-17T08:05:30.000Z',
    })

    const items = mergeRequestLogItems(
      [completed, other],
      [],
      allFilters(),
      1,
      new Set(),
      { 'request-1': '2026-08-17T08:00:00.000Z' },
    )

    expect(items.map((item) => item.id)).toEqual(['history-other', 'history-completed'])
    expect(items[1]).toMatchObject({ display_started_at: '2026-08-17T08:00:00.000Z' })
  })

  it('does not merge live rows into success or failure filters', () => {
    const live = [activity()]
    const history = [historyItem()]

    expect(mergeRequestLogItems(history, live, allFilters({ success: true }), 1)).toBe(history)
    expect(mergeRequestLogItems(history, live, allFilters({ success: false }), 1)).toBe(history)
  })
})
