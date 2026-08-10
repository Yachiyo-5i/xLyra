import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  readRequestsAutoRefreshPreference,
  readRequestsTableColumnWidthsPreference,
  writeRequestsAutoRefreshPreference,
  writeRequestsTableColumnWidthsPreference,
} from '@/features/requests/lib/request-preferences'
import { REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS } from '@/features/requests/lib/request-table-columns'

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

describe('request table column width preference', () => {
  it('falls back to defaults for missing, malformed, stale, or invalid values', () => {
    const getItem = vi.mocked(window.localStorage.getItem)
    const belowMinimum = [...REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS]
    belowMinimum[0] = 1
    belowMinimum[1] = 13

    getItem
      .mockReturnValueOnce(null)
      .mockReturnValueOnce('not-json')
      .mockReturnValueOnce(JSON.stringify(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS.slice(0, -1)))
      .mockReturnValueOnce(JSON.stringify(belowMinimum))

    expect(readRequestsTableColumnWidthsPreference()).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)
    expect(readRequestsTableColumnWidthsPreference()).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)
    expect(readRequestsTableColumnWidthsPreference()).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)
    expect(readRequestsTableColumnWidthsPreference()).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)
  })

  it('reads and writes only validated width ratios', () => {
    const widths = [...REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS]
    widths[1] += 1.5
    widths[2] -= 1.5
    vi.mocked(window.localStorage.getItem).mockReturnValue(JSON.stringify(widths))

    expect(readRequestsTableColumnWidthsPreference()).toEqual(widths)
    expect(writeRequestsTableColumnWidthsPreference(widths)).toBe(true)
    expect(window.localStorage.setItem).toHaveBeenCalledWith('xlyra:requests:table-column-widths:v1', JSON.stringify(widths))
    expect(writeRequestsTableColumnWidthsPreference([...widths, 0])).toBe(false)
    expect(window.localStorage.setItem).toHaveBeenCalledTimes(1)
  })

  it('handles unavailable storage without throwing', () => {
    vi.mocked(window.localStorage.getItem).mockImplementation(() => { throw new Error('denied') })
    expect(readRequestsTableColumnWidthsPreference()).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)

    vi.mocked(window.localStorage.setItem).mockImplementation(() => { throw new Error('denied') })
    expect(writeRequestsTableColumnWidthsPreference(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)).toBe(false)
  })
})
