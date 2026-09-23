import { afterEach, describe, expect, it, vi } from 'vitest'

import { bootstrapInitializedFromCookie, readBootstrapInitialized, rememberBootstrapInitialized } from '@/lib/bootstrap-state'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('bootstrap initialized cache', () => {
  it('reads the server snapshot from the page cookie', () => {
    expect(bootstrapInitializedFromCookie('xlyra_admin_initialized=1')).toBe(true)
    expect(bootstrapInitializedFromCookie('theme=dark; xlyra_admin_initialized=0')).toBe(false)
    expect(bootstrapInitializedFromCookie('theme=dark')).toBeNull()
  })

  it('prefers a newer cookie over a stale injected snapshot', () => {
    vi.stubGlobal('window', { __XLYRA_BOOTSTRAP__: { initialized: false } })
    vi.stubGlobal('document', { cookie: 'xlyra_admin_initialized=1' })
    expect(readBootstrapInitialized()).toBe(true)
  })

  it('uses the injected snapshot when the cookie is absent', () => {
    vi.stubGlobal('window', { __XLYRA_BOOTSTRAP__: { initialized: false } })
    vi.stubGlobal('document', { cookie: 'theme=dark' })
    expect(readBootstrapInitialized()).toBe(false)
  })

  it('remembers the auth response for the next page open', () => {
    const jar = { cookie: '' }
    const page = {} as Window
    vi.stubGlobal('window', page)
    vi.stubGlobal('document', jar)
    vi.stubGlobal('location', { protocol: 'https:' })

    rememberBootstrapInitialized(false)
    expect(bootstrapInitializedFromCookie(jar.cookie)).toBe(false)
    expect(jar.cookie).toContain('Secure')
    expect(page.__XLYRA_BOOTSTRAP__).toEqual({ initialized: false })

    rememberBootstrapInitialized(true)
    expect(readBootstrapInitialized()).toBe(true)
    expect(page.__XLYRA_BOOTSTRAP__).toEqual({ initialized: true })
  })
})
