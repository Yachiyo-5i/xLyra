import { describe, expect, it, vi } from 'vitest'
import {
  applyFrontendUpdate,
  cacheBustedHref,
  fetchRemoteBuild,
  hasRemoteFrontendUpdate,
  hrefWithoutReloadParam,
  parseRemoteBuild,
  readClientBuildId,
  VERSION_URL,
} from './frontend-update'

describe('readClientBuildId', () => {
  it('falls back to development when the build id is missing', () => {
    expect(readClientBuildId('')).toBe('development')
  })

  it('returns the injected build id', () => {
    expect(readClientBuildId('1710000000')).toBe('1710000000')
  })
})

describe('parseRemoteBuild', () => {
  it('reads a non-empty build field', () => {
    expect(parseRemoteBuild({ build: '1710000000' })).toBe('1710000000')
  })

  it('ignores malformed payloads', () => {
    expect(parseRemoteBuild(null)).toBeUndefined()
    expect(parseRemoteBuild('1710000000')).toBeUndefined()
    expect(parseRemoteBuild({ build: '' })).toBeUndefined()
    expect(parseRemoteBuild({ version: '1' })).toBeUndefined()
  })
})

describe('hasRemoteFrontendUpdate', () => {
  it('detects a different production build', () => {
    expect(hasRemoteFrontendUpdate('2', '1')).toBe(true)
  })

  it('ignores matching builds, missing payloads, and local development', () => {
    expect(hasRemoteFrontendUpdate('1', '1')).toBe(false)
    expect(hasRemoteFrontendUpdate(undefined, '1')).toBe(false)
    expect(hasRemoteFrontendUpdate('2', 'development')).toBe(false)
  })
})

describe('fetchRemoteBuild', () => {
  it('requests version.json without using the HTTP cache', async () => {
    const fetcher = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ build: '1710000000' }),
    })

    await expect(fetchRemoteBuild(fetcher)).resolves.toBe('1710000000')
    expect(fetcher).toHaveBeenCalledWith(VERSION_URL, { cache: 'no-store' })
  })

  it('returns undefined when the request fails', async () => {
    const fetcher = vi.fn().mockRejectedValue(new Error('offline'))
    await expect(fetchRemoteBuild(fetcher)).resolves.toBeUndefined()
  })
})

describe('frontend reload', () => {
  it('cache-busts the document url and can strip that param again', () => {
    const busted = cacheBustedHref('https://xlyra.example/dashboard?tab=usage#top', 42)
    expect(busted).toBe('https://xlyra.example/dashboard?tab=usage&__xlyra_reload=42#top')
    expect(hrefWithoutReloadParam(busted)).toBe('https://xlyra.example/dashboard?tab=usage#top')
    expect(hrefWithoutReloadParam('https://xlyra.example/dashboard')).toBeNull()
  })

  it('activates a waiting worker before reloading', async () => {
    const worker = fakeWorker('installed')
    let controllerListener: (() => void) | undefined
    const reloads: string[] = []
    worker.postMessage = () => {
      controllerListener?.()
    }

    await applyFrontendUpdate({
      registration: fakeRegistration(worker),
      href: 'https://xlyra.example/dashboard',
      now: 7,
      reload: (href) => reloads.push(href),
      waitForControllerChange: () => new Promise((resolve) => {
        controllerListener = () => resolve()
      }),
      clearCaches: vi.fn(),
      unregister: vi.fn(),
    })

    expect(reloads).toEqual(['https://xlyra.example/dashboard?__xlyra_reload=7'])
  })

  it('drops cached shells when no replacement worker exists', async () => {
    const cleared = vi.fn(async () => undefined)
    const unregister = vi.fn(async () => undefined)
    const registration = fakeRegistration(null)
    registration.update = vi.fn(async () => undefined)

    await applyFrontendUpdate({
      registration,
      href: 'https://xlyra.example/',
      now: 9,
      reload: (href) => {
        expect(cleared).toHaveBeenCalledOnce()
        expect(unregister).toHaveBeenCalledWith(registration)
        expect(href).toBe('https://xlyra.example/?__xlyra_reload=9')
      },
      waitForControllerChange: () => Promise.resolve(),
      clearCaches: cleared,
      unregister,
    })
  })
})

function fakeWorker(state: string) {
  return {
    state,
    postMessage: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }
}

function fakeRegistration(worker: ReturnType<typeof fakeWorker> | null) {
  return {
    waiting: worker,
    installing: null,
    update: vi.fn(async () => undefined),
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }
}
