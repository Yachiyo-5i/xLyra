import { afterEach, describe, expect, it, vi } from 'vitest'

import { restoreAutomaticBackupFileSSE } from '@/features/settings/api/settings'
import { setCSRFToken } from '@/lib/http'

afterEach(() => {
  setCSRFToken(null)
  vi.unstubAllGlobals()
})

describe('restoreAutomaticBackupFileSSE', () => {
  it('uses the authenticated request path and parses progress events', async () => {
    setCSRFToken('csrf-token')
    const fetchMock = vi.fn().mockResolvedValue(new Response(
      'data: {"step":"download","status":"complete"}\n\n' +
      'data: {"step":"complete","status":"complete","rows":800000,"total_rows":800000,"summary":{"tables":22,"rows":800000,"config_keys":3,"format_version":2}}\n\n',
      { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
    ))
    vi.stubGlobal('fetch', fetchMock)

    const events = []
    for await (const event of restoreAutomaticBackupFileSSE('backups/latest.xlyra')) {
      events.push(event)
    }

    const [, request] = fetchMock.mock.calls[0]
    const headers = new Headers(request.headers)
    expect(headers.get('X-CSRF-Token')).toBe('csrf-token')
    expect(headers.get('Accept')).toBe('text/event-stream')
    expect(JSON.parse(request.body)).toEqual({ key: 'backups/latest.xlyra' })
    expect(events).toHaveLength(2)
    expect(events[1].summary?.rows).toBe(800000)
    expect(events[1].total_rows).toBe(800000)
  })

  it('accepts CRLF frames and rejects a stream without a terminal event', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      'data: {"step":"download","status":"in_progress"}\r\n\r\n',
      { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
    )))

    const consume = async () => {
      for await (const event of restoreAutomaticBackupFileSSE('backups/latest.xlyra')) {
        expect(event.step).toBe('download')
      }
    }

    await expect(consume()).rejects.toThrow('restore response stream ended unexpectedly')
  })
})
