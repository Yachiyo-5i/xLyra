import { afterEach, describe, expect, it, vi } from 'vitest'

import { cancelAutomaticBackupRestoreTask, fetchActiveAutomaticBackupRestoreTask, fetchAutomaticBackupRestoreTask, startAutomaticBackupRestore } from '@/features/settings/api/settings'
import { setCSRFToken } from '@/lib/http'

afterEach(() => {
  setCSRFToken(null)
  vi.unstubAllGlobals()
})

describe('automatic backup restore tasks', () => {
  it('starts an authenticated background restore task', async () => {
    setCSRFToken('csrf-token')
    const restore = {
      id: 'task-id',
      key: 'backups/latest.xlyra',
      filename: 'latest.xlyra',
      status: 'running',
      started_at: '2026-08-24T00:00:00Z',
      progress: { step: 'download', status: 'in_progress' },
    }
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ restore }), {
      status: 202,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await startAutomaticBackupRestore('backups/latest.xlyra')

    const [, request] = fetchMock.mock.calls[0]
    const headers = new Headers(request.headers)
    expect(headers.get('X-CSRF-Token')).toBe('csrf-token')
    expect(JSON.parse(request.body)).toEqual({ key: 'backups/latest.xlyra' })
    expect(result.restore.id).toBe('task-id')
  })

  it('loads the latest restore task progress', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      restore: {
        id: 'task/id',
        status: 'completed',
        progress: { step: 'complete', status: 'complete' },
      },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await fetchAutomaticBackupRestoreTask('task/id')

    expect(fetchMock.mock.calls[0][0]).toContain('/restore/task%2Fid')
    expect(result.restore.status).toBe('completed')
  })

  it('loads the active restore task after a page refresh', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ restore: null }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await fetchActiveAutomaticBackupRestoreTask()

    expect(fetchMock.mock.calls[0][0]).toContain('/restore/active')
    expect(result.restore).toBeNull()
  })

  it('requests cancellation for a running restore task', async () => {
    setCSRFToken('csrf-token')
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      restore: {
        id: 'task-id',
        status: 'canceling',
        cancellable: false,
        progress: { step: 'import', status: 'in_progress' },
      },
    }), { status: 202, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await cancelAutomaticBackupRestoreTask('task-id')

    const [, request] = fetchMock.mock.calls[0]
    expect(request.method).toBe('DELETE')
    expect(new Headers(request.headers).get('X-CSRF-Token')).toBe('csrf-token')
    expect(result.restore.status).toBe('canceling')
  })
})
