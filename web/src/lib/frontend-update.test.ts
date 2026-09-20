import { describe, expect, it, vi } from 'vitest'
import { hasRemoteFrontendUpdate, parseRemoteBuild, readClientBuildId, fetchRemoteBuild, VERSION_URL } from './frontend-update'

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
