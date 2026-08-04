import { describe, expect, it } from 'vitest'
import { formatResponseDuration } from '@/features/playground/lib/response-timing'

describe('formatResponseDuration', () => {
  it('formats milliseconds as seconds with one decimal place', () => {
    expect(formatResponseDuration(6_000)).toBe('6.0s')
    expect(formatResponseDuration(12_345)).toBe('12.3s')
  })

  it('clamps negative durations to zero', () => {
    expect(formatResponseDuration(-1)).toBe('0.0s')
  })
})
