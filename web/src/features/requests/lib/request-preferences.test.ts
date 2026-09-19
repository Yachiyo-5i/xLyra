import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  readRequestsAutoRefreshPreference,
  writeRequestsAutoRefreshPreference,
} from '@/features/requests/lib/request-preferences'

beforeEach(() => {
  vi.stubGlobal('window', {
    localStorage: {
      getItem: vi.fn(),
      setItem: vi.fn(),
    },
  })
})

describe('request auto-refresh preference', () => {
  it('defaults to false and only accepts strict true', () => {
    const getItem = vi.mocked(window.localStorage.getItem)
    getItem.mockReturnValueOnce(null).mockReturnValueOnce('false').mockReturnValueOnce('true').mockReturnValueOnce('TRUE')

    expect(readRequestsAutoRefreshPreference()).toBe(false)
    expect(readRequestsAutoRefreshPreference()).toBe(false)
    expect(readRequestsAutoRefreshPreference()).toBe(true)
    expect(readRequestsAutoRefreshPreference()).toBe(false)
  })

  it('handles storage failures without throwing', () => {
    vi.mocked(window.localStorage.getItem).mockImplementation(() => { throw new Error('denied') })
    expect(readRequestsAutoRefreshPreference()).toBe(false)

    vi.mocked(window.localStorage.setItem).mockImplementation(() => { throw new Error('denied') })
    expect(() => writeRequestsAutoRefreshPreference(true)).not.toThrow()
  })

  it('writes strict boolean values', () => {
    writeRequestsAutoRefreshPreference(true)
    writeRequestsAutoRefreshPreference(false)
    expect(window.localStorage.setItem).toHaveBeenNthCalledWith(1, 'xlyra:requests:auto-refresh', 'true')
    expect(window.localStorage.setItem).toHaveBeenNthCalledWith(2, 'xlyra:requests:auto-refresh', 'false')
  })
})
