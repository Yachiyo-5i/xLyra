import { describe, expect, it } from 'vitest'
import {
  REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS,
  REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS,
  isRequestTableColumnWidths,
  resizeRequestTableColumnBoundary,
} from '@/features/requests/lib/request-table-columns'

describe('request table column resizing', () => {
  it('uses a compact timing column by default', () => {
    expect(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS[2]).toBe(18)
    expect(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS[6]).toBe(10)
  })

  it('resizes the target and proportionally links all other columns', () => {
    const initial = [...REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS]
    const resized = resizeRequestTableColumnBoundary(initial, 2, 4.25)

    expect(resized[2]).toBe(initial[2] + 4.25)
    expect(resized[1]).toBeLessThan(initial[1])
    expect(resized[5]).toBeLessThan(initial[5])
    expect(resized.every((width, index) => width >= REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS[index])).toBe(true)
    expect(totalWidthUnits(resized)).toBe(10_000)
    expect(initial).toEqual(REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS)
  })

  it('distributes released width to every other column', () => {
    const initial = [...REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS]
    const resized = resizeRequestTableColumnBoundary(initial, 2, -4.25)

    expect(resized[2]).toBe(initial[2] - 4.25)
    expect(resized[1]).toBeGreaterThan(initial[1])
    expect(resized[5]).toBeGreaterThan(initial[5])
    expect(totalWidthUnits(resized)).toBe(10_000)
  })

  it('clamps growth against every other column minimum and permits a zero-width status column', () => {
    const initial = [...REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS]
    const resized = resizeRequestTableColumnBoundary(initial, 2, 100)
    const statusCollapsed = [...REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS]
    statusCollapsed[1] += statusCollapsed[5]
    statusCollapsed[5] = 0

    expect(resized[2]).toBe(100 - REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS.reduce((total, minimum, index) => index === 2 ? total : total + minimum, 0))
    expect(resized.every((width, index) => width >= REQUEST_TABLE_COLUMN_MINIMUM_WIDTHS[index])).toBe(true)
    expect(isRequestTableColumnWidths(statusCollapsed)).toBe(true)
    expect(totalWidthUnits(resized)).toBe(10_000)
  })

  it('accepts stored ratios with valid hundredth-percent floating-point representations', () => {
    const widths = [...REQUEST_TABLE_COLUMN_DEFAULT_WIDTHS]
    widths[5] = 0.07
    widths[9] = 24.93

    expect(isRequestTableColumnWidths(widths)).toBe(true)
  })
})

function totalWidthUnits(widths: readonly number[]) {
  return widths.reduce((total, width) => total + Math.round(width * 100), 0)
}
