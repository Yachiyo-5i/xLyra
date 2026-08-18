import { describe, expect, it } from 'vitest'

import type { Site } from '@/features/sites/api/sites'
import { formatCompactTokens, formatDateTime, formatSiteBalance, isSiteAbnormal, siteBalanceDetails, sortSitesForDisplay, sub2APIKeyQuotaDetails } from '@/features/sites/lib/site-utils'

function siteWithSyncState(failureClass: 'unknown' | 'limited' | 'transient' | 'credential_invalid'): Site {
  return {
    id: 'site-1',
    name: 'Upstream',
    slug: 'upstream',
    site_type: 'openai',
    base_url: 'https://example.invalid',
    status: 'active',
    enabled: true,
    routing_priority: 0,
    meta: {},
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    sync_state: {
      status: 'partial',
      validation_ok: true,
      failure_class: failureClass,
    },
  }
}

describe('isSiteAbnormal', () => {
  it.each(['unknown', 'limited', 'transient'] as const)('keeps %s failures available', (failureClass) => {
    expect(isSiteAbnormal(siteWithSyncState(failureClass))).toBe(false)
  })

  it('marks a confirmed invalid API key as abnormal', () => {
    expect(isSiteAbnormal(siteWithSyncState('credential_invalid'))).toBe(true)
  })
})

describe('sortSitesForDisplay', () => {
  it('sorts enabled sites by routing priority and keeps disabled sites last', () => {
    const sites = [
      { ...siteWithSyncState('unknown'), id: 'low', name: 'Low', slug: 'low', routing_priority: 1 },
      { ...siteWithSyncState('unknown'), id: 'disabled', name: 'Disabled', slug: 'disabled', routing_priority: 100, enabled: false },
      { ...siteWithSyncState('unknown'), id: 'high', name: 'High', slug: 'high', routing_priority: 10 },
    ]

    expect(sortSitesForDisplay(sites).map((site) => site.id)).toEqual(['high', 'low', 'disabled'])
  })
})

describe('formatCompactTokens', () => {
  it('can omit the unit for compact mobile metrics', () => {
    expect(formatCompactTokens(3_200_000)).toBe('3.2M tokens')
    expect(formatCompactTokens(3_200_000, false)).toBe('3.2M')
  })
})

describe('Kimi quota formatting', () => {
  const site: Site = {
    ...siteWithSyncState('unknown'),
    site_type: 'kimi_code',
    quota_probe: {
      probe_type: 'kimi',
      remaining_min: 0,
      unit: 'percent',
      entries: [
        { label: 'five_hour', unit: 'percent', remaining: 0, limit: 100, used: 100 },
        { label: 'weekly', unit: 'percent', remaining: 80, limit: 100, used: 20 },
      ],
    },
  }

  it('shows remaining quota for both windows in the table', () => {
    expect(formatSiteBalance(site)).toBe('0% / 80%')
  })

  it('marks the remaining quota values for details', () => {
    expect(siteBalanceDetails(site).map((detail) => ({ value: detail.value, valuePrefix: detail.valuePrefix }))).toEqual([
      { value: '0%', valuePrefix: 'remaining' },
      { value: '80%', valuePrefix: 'remaining' },
    ])
  })

  it('formats reset times without a 12-hour suffix', () => {
    const value = formatDateTime('2026-07-30T01:12:00Z', 'en', 'h23')
    expect(value).toMatch(/\b\d{2}:\d{2}\b/)
    expect(value).not.toMatch(/\b(?:AM|PM)\b/i)
  })
})

describe('sub2api key quota formatting', () => {
  it('formats Plan windows as remaining percentages in display order', () => {
    const details = sub2APIKeyQuotaDetails({
      status: 'ok',
      kind: 'subscription_plan',
      plan: '待宵计划',
      entries: [
        { label: 'monthly', unit: 'percent', remaining: 0, limit: 100, used: 100 },
        { label: 'daily', unit: 'percent', remaining: 100, limit: 100, used: 0, reset_at: '2026-08-19T00:00:00+08:00' },
        { label: 'weekly', unit: 'percent', remaining: 74.5, limit: 100, used: 25.5 },
      ],
    }, 'zh')

    expect(details.map((detail) => ({ label: detail.label, value: detail.value, valuePrefix: detail.valuePrefix }))).toEqual([
      { label: 'dailyQuota', value: '100%', valuePrefix: 'remaining' },
      { label: 'weeklyQuota', value: '74.5%', valuePrefix: 'remaining' },
      { label: 'monthlyQuota', value: '0%', valuePrefix: 'remaining' },
    ])
    expect(details[0]?.extra).toBeTruthy()
  })

  it('keeps the direct value on the account balance fallback', () => {
    const site: Site = {
      ...siteWithSyncState('unknown'),
      gateway_config: { quota_probe: 'sub2api' },
      quota_probe: {
        probe_type: 'sub2api',
        remaining_min: 15,
        unit: 'percent',
        plan: '待宵计划',
        entries: [
          { label: 'daily', unit: 'percent', remaining: 100, limit: 100, used: 0 },
        ],
      },
      sync_state: {
        status: 'synced',
        user_summary: { data: { quota: 21_000_000, currency: 'USD' } },
      },
    }

    expect(formatSiteBalance(site)).toBe('$42.00')
  })
})
