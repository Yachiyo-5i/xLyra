import { afterEach, describe, expect, it, vi } from 'vitest'

import { fetchAuthState } from '@/features/auth/api/auth'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('fetchAuthState', () => {
  it('loads unauthenticated bootstrap state with one request', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      initialized: true,
      authenticated: false,
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(fetchAuthState()).resolves.toEqual({
      initialized: true,
      authenticated: false,
      session: null,
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/auth/state')
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ cache: 'no-store' })
  })

  it('normalizes an authenticated session from the same response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      initialized: true,
      authenticated: true,
      expires_at: '2026-08-19T00:00:00Z',
      csrf_token: 'csrf-token',
      admin: {
        id: 'admin-1',
        username: 'owner',
        role: 'owner',
        status: 'active',
        totp_enabled: true,
      },
    }), { status: 200, headers: { 'Content-Type': 'application/json' } })))

    await expect(fetchAuthState()).resolves.toEqual({
      initialized: true,
      authenticated: true,
      session: {
        expiresAt: '2026-08-19T00:00:00Z',
        csrfToken: 'csrf-token',
        admin: {
          id: 'admin-1',
          username: 'owner',
          nickname: '',
          avatar: '',
          role: 'owner',
          status: 'active',
          totpEnabled: true,
          lastLoginAt: null,
        },
      },
    })
  })
})
