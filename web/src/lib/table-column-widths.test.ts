import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defaultTableColumnWidths, isTableColumnWidths, readTableColumnWidths, resizeTableColumnBoundary, writeTableColumnWidths } from '@/lib/table-column-widths'
import { REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS, REQUEST_TABLE_COLUMN_SIZING } from '@/features/requests/lib/request-table-columns'

beforeEach(() => {
  vi.stubGlobal('window', { localStorage: { getItem: vi.fn(), setItem: vi.fn() } })
})

afterEach(() => vi.unstubAllGlobals())

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

    expect(readTableColumnWidths(REQUEST_TABLE_COLUMN_SIZING)).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)
    expect(readTableColumnWidths(REQUEST_TABLE_COLUMN_SIZING)).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)
    expect(readTableColumnWidths(REQUEST_TABLE_COLUMN_SIZING)).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)
    expect(readTableColumnWidths(REQUEST_TABLE_COLUMN_SIZING)).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)
  })

  it('reads and writes only validated width ratios', () => {
    const widths = [...REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS]
    widths[1] += 1.5
    widths[2] -= 1.5
    vi.mocked(window.localStorage.getItem).mockReturnValue(JSON.stringify(widths))

    expect(readTableColumnWidths(REQUEST_TABLE_COLUMN_SIZING)).toEqual(widths)
    expect(writeTableColumnWidths(REQUEST_TABLE_COLUMN_SIZING, widths)).toBe(true)
    expect(window.localStorage.setItem).toHaveBeenCalledWith('xlyra:requests:table-column-widths:v1', JSON.stringify(widths))
    expect(writeTableColumnWidths(REQUEST_TABLE_COLUMN_SIZING, [...widths, 0])).toBe(false)
    expect(window.localStorage.setItem).toHaveBeenCalledTimes(1)
  })

  it('handles unavailable storage without throwing', () => {
    vi.mocked(window.localStorage.getItem).mockImplementation(() => { throw new Error('denied') })
    expect(readTableColumnWidths(REQUEST_TABLE_COLUMN_SIZING)).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)

    vi.mocked(window.localStorage.setItem).mockImplementation(() => { throw new Error('denied') })
    expect(writeTableColumnWidths(REQUEST_TABLE_COLUMN_SIZING, REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)).toBe(false)
  })
})

describe('shared table column widths', () => {
  it('normalizes existing page proportions without changing their ratios', () => {
    const widths = defaultTableColumnWidths([24, 8, 13, 8, 8, 6, 7, 10, 11])
    expect(widths.reduce((total, value) => total + Math.round(value * 100), 0)).toBe(10_000)
    expect(widths[0]).toBeCloseTo(24 / 95 * 100, 2)
    expect(isTableColumnWidths(widths, [10, 5, 7, 5, 5, 4, 5, 7, 7])).toBe(true)
  })

  it('keeps differently sized tables independent in storage and resizing', () => {
    const storage = new Map<string, string>()
    vi.mocked(window.localStorage.getItem).mockImplementation((key) => storage.get(key) ?? null)
    vi.mocked(window.localStorage.setItem).mockImplementation((key, value) => { storage.set(key, value) })
    const first = { storageKey: 'first', defaultWidths: [60, 40], minimumWidths: [20, 10] }
    const second = { storageKey: 'second', defaultWidths: [20, 50, 30], minimumWidths: [10, 15, 10] }
    const resized = resizeTableColumnBoundary(first.defaultWidths, 0, 100, first)
    expect(resized).toEqual([90, 10])
    expect(writeTableColumnWidths(first, resized)).toBe(true)
    expect(readTableColumnWidths(first)).toEqual([90, 10])
    expect(readTableColumnWidths(second)).toEqual(second.defaultWidths)
    storage.set(second.storageKey, JSON.stringify(resized))
    expect(readTableColumnWidths(second)).toEqual(second.defaultWidths)
  })
})
