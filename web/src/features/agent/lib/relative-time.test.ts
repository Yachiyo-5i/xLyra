import { describe, expect, it } from 'vitest'

import { formatRelativeTime } from '@/features/agent/lib/relative-time'

const NOW = new Date('2026-08-28T12:00:00Z').getTime()

describe('formatRelativeTime', () => {
  it('returns empty string for missing or invalid values', () => {
    expect(formatRelativeTime(undefined, 'zh', NOW)).toBe('')
    expect(formatRelativeTime('not-a-date', 'zh', NOW)).toBe('')
  })

  it('formats recent times in zh', () => {
    expect(formatRelativeTime(new Date(NOW - 30_000).toISOString(), 'zh', NOW)).toBe('30秒钟前')
    expect(formatRelativeTime(new Date(NOW - 7 * 3600_000).toISOString(), 'zh', NOW)).toBe('7小时前')
    expect(formatRelativeTime(new Date(NOW - 86400_000).toISOString(), 'zh', NOW)).toBe('昨天')
    expect(formatRelativeTime(new Date(NOW - 18 * 86400_000).toISOString(), 'zh', NOW)).toBe('18天前')
  })

  it('formats in en and jp locales', () => {
    expect(formatRelativeTime(new Date(NOW - 3600_000).toISOString(), 'en', NOW)).toBe('1 hour ago')
    expect(formatRelativeTime(new Date(NOW - 3600_000).toISOString(), 'jp', NOW)).toBe('1 時間前')
  })

  it('accepts numeric timestamps', () => {
    expect(formatRelativeTime(NOW - 2 * 86400_000, 'en', NOW)).toBe('2 days ago')
  })
})
