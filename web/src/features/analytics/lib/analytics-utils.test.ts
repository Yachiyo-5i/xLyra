import { afterEach, describe, expect, it, vi } from 'vitest'
import { presetRange } from './analytics-utils'

afterEach(() => {
  vi.useRealTimers()
})

describe('analytics date presets', () => {
  it('returns yesterday as a single-day range', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(2026, 7, 18, 12, 0, 0))

    expect(presetRange('yesterday')).toEqual({
      from: '2026-08-17',
      to: '2026-08-17',
    })
  })
})
