import { describe, expect, it } from 'vitest'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import { formatCompactDollarQuota, hasResettableQuota } from '@/features/api-keys/lib/api-key-utils'

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

describe('hasResettableQuota', () => {
  const base = {
    quota_unlimited: true,
    quota_limit: null,
    quota_daily_unlimited: true,
    quota_daily_limit: null,
    quota_weekly_unlimited: true,
    quota_weekly_limit: null,
  } as DownstreamAPIKey

  it.each([
    { quota_unlimited: false, quota_limit: 100 },
    { quota_daily_unlimited: false, quota_daily_limit: 10 },
    { quota_weekly_unlimited: false, quota_weekly_limit: 50 },
  ])('returns true when a resettable quota is configured', (quota) => {
    expect(hasResettableQuota({ ...base, ...quota })).toBe(true)
  })

  it('returns false when every quota is unlimited', () => {
    expect(hasResettableQuota(base)).toBe(false)
  })
})
