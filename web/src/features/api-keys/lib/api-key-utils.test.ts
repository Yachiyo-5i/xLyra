import { describe, expect, it } from 'vitest'
import { formatCompactDollarQuota } from '@/features/api-keys/lib/api-key-utils'

describe('formatCompactDollarQuota', () => {
  it.each([
    [999.5, '$999.50'],
    [1_000, '$1K'],
    [12_500, '$12.5K'],
    [1_250_000, '$1.25M'],
    [2_000_000_000, '$2B'],
  ])('formats %s as %s', (value, expected) => {
    expect(formatCompactDollarQuota(value)).toBe(expected)
  })
})
