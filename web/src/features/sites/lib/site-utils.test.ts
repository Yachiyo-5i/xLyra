import { describe, expect, it } from 'vitest'

import type { Site, SiteAccountCapabilities, SiteQuotaProbeEntry, SiteTypeInfo } from '@/features/sites/api/sites'
import { accountBalanceDetails, accountQuotaEntries, formatAccountBalance, formatCompactTokens, formatDateTime, formatQuotaEntryAmount, formatQuotaProbeBalance, formatSiteBalance, formatSiteTypeLabel, isAccountLoginSiteType, isSiteAbnormal, legacyGrokQuotaEntries, normalizedSiteTypeFilter, quotaEntryKind, siteAccountCounts, siteBalanceDetails, siteTypeColor, siteTypeIcon, sortSitesForDisplay, sub2APIKeyQuotaDetails } from '@/features/sites/lib/site-utils'

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

describe('normalizedSiteTypeFilter', () => {
  it('keeps Grok and OpenAI on separate tabs', () => {
    expect(normalizedSiteTypeFilter('grok')).toBe('grok')
    expect(normalizedSiteTypeFilter('openai')).toBe('openai')
    expect(normalizedSiteTypeFilter('grok')).not.toBe(normalizedSiteTypeFilter('openai'))
  })

  it('groups only the OpenAI-compatible alias with OpenAI', () => {
    expect(normalizedSiteTypeFilter('openai_compatible')).toBe('openai')
    expect(normalizedSiteTypeFilter(' OpenAI ')).toBe('openai')
  })

  it('gives later site types their own tab instead of folding them into OpenAI', () => {
    expect(normalizedSiteTypeFilter('opencode_go')).toBe('opencode_go')
    expect(normalizedSiteTypeFilter('anthropic')).toBe('anthropic')
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

describe('GLM quota formatting', () => {
  const glmSite = (entries: SiteQuotaProbeEntry[]): Site => ({
    ...siteWithSyncState('unknown'),
    site_type: 'glm_code',
    quota_probe: {
      probe_type: 'glm',
      remaining_min: 0,
      unit: 'percent',
      plan: 'Pro',
      entries,
    },
  })

  it('shows remaining quota for both windows in the table', () => {
    const site = glmSite([
      { label: 'five_hour', unit: 'percent', remaining: 100, limit: 100, used: 0 },
      { label: 'weekly', unit: 'percent', remaining: 0, limit: 100, used: 100, reset_at: '2026-09-10T01:59:59Z' },
    ])
    expect(formatSiteBalance(site)).toBe('100% / 0%')
  })

  it('shows a dash for a missing window', () => {
    const site = glmSite([
      { label: 'five_hour', unit: 'percent', remaining: 97, limit: 100, used: 3 },
    ])
    expect(formatSiteBalance(site)).toBe('97% / -')
  })

  it('marks the remaining quota values for details', () => {
    const site = glmSite([
      { label: 'five_hour', unit: 'percent', remaining: 100, limit: 100, used: 0 },
      { label: 'weekly', unit: 'percent', remaining: 0, limit: 100, used: 100 },
    ])
    expect(siteBalanceDetails(site).map((detail) => ({ label: detail.label, value: detail.value, valuePrefix: detail.valuePrefix }))).toEqual([
      { label: 'fiveHourQuota', value: '100%', valuePrefix: 'remaining' },
      { label: 'weeklyQuota', value: '0%', valuePrefix: 'remaining' },
    ])
  })

  it('shows the monthly MCP window when present', () => {
    const site = glmSite([
      { label: 'five_hour', unit: 'percent', remaining: 99, limit: 100, used: 1 },
      { label: 'weekly', unit: 'percent', remaining: 80, limit: 100, used: 20 },
      { label: 'monthly', unit: 'percent', remaining: 99.3, limit: 100, used: 0.7 },
    ])
    expect(formatSiteBalance(site)).toBe('99% / 80%')
    expect(siteBalanceDetails(site).map((detail) => ({ label: detail.label, value: detail.value, valuePrefix: detail.valuePrefix }))).toEqual([
      { label: 'fiveHourQuota', value: '99%', valuePrefix: 'remaining' },
      { label: 'weeklyQuota', value: '80%', valuePrefix: 'remaining' },
      { label: 'mcpMonthlyQuota', value: '99.3%', valuePrefix: 'remaining' },
    ])
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

describe('DeepSeek balance formatting', () => {
  it.each([
    [47.44, '¥47.44'],
    [0, '¥0.00'],
    [-0.5, '-¥0.50'],
    [-0.0001, '-¥0.0001'],
  ])('preserves balance %s', (amount, expected) => {
    const entries = [{ label: 'balance', unit: 'cny', remaining: amount }]
    const site = { ...siteWithSyncState('unknown'), site_type: 'deepseek',
      quota_probe: { probe_type: 'deepseek', remaining_min: amount, unit: 'cny', entries },
    }
    expect(formatSiteBalance(site)).toBe(expected)
    expect(siteBalanceDetails(site)).toEqual([{ label: 'accountBalance', value: expected }])
  })

  it('shows each currency and its balance components without treating them as usage or limits', () => {
    const entries = [
      { label: 'balance', unit: 'cny', remaining: -0.5, granted_balance: 0, topped_up_balance: -0.5 },
      { label: 'balance', unit: 'usd', remaining: 2.5, granted_balance: 1, topped_up_balance: 1.5 },
    ]
    expect(formatAccountBalance(entries)).toBe('-¥0.50 / $2.50')
    const details = accountBalanceDetails(entries)
    expect(details).toEqual([
      { label: 'accountBalance', value: '-¥0.50' },
      { label: 'grantedBalance', value: '¥0.00' },
      { label: 'toppedUpBalance', value: '-¥0.50' },
      { label: 'accountBalance', value: '$2.50' },
      { label: 'grantedBalance', value: '$1.00' },
      { label: 'toppedUpBalance', value: '$1.50' },
    ])
    expect(siteBalanceDetails({ ...siteWithSyncState('unknown'), quota_probe: { probe_type: 'deepseek', entries } })).toEqual(details)
  })

  it('keeps missing balance data distinct from a zero balance', () => {
    expect(formatAccountBalance([])).toBeUndefined()
    expect(accountBalanceDetails([{ label: 'balance', unit: 'usd' }])).toEqual([])
  })
})

describe('Moonshot balance formatting', () => {
  it.each([0, -0.5, 2.5])('preserves available balance %s independently of cash debt', (available) => {
    const entries = [{ label: 'balance', unit: 'cny', remaining: available, cash_balance: -31.14918, voucher_balance: Math.max(available, 0) }]
    const site = { ...siteWithSyncState('unknown'), site_type: 'moonshot', quota_probe: { probe_type: 'moonshot', entries, remaining_min: available, unit: 'cny' } }
    const amount = available < 0 ? '-¥0.50' : available === 0 ? '¥0.00' : '¥2.50'
    expect(formatSiteBalance(site)).toBe(amount)
    expect(siteBalanceDetails(site)).toEqual([
      { label: 'accountBalance', value: amount },
      { label: 'cashBalance', value: '-¥31.15' },
      { label: 'voucherBalance', value: available > 0 ? '¥2.50' : '¥0.00' },
    ])
    expect(accountBalanceDetails(entries).filter((detail) => detail.label === 'accountBalance')).toEqual([{ label: 'accountBalance', value: amount }])
  })
})

describe('grok billing summary', () => {
  it('shows the remaining plan allowance as a percent', () => {
    const site = {
      id: 's1',
      site_type: 'grok',
      quota_probe: { probe_type: 'grok', status: 'ok', remaining_min: 95, limit: 100, unit: 'percent' },
    } as unknown as Parameters<typeof formatSiteBalance>[0]
    expect(formatSiteBalance(site)).toBe('95%')
  })
})

describe('generic credits and percent quota summary', () => {
  function summarySite(probeType: string, unit: 'credits' | 'percent' | 'usd', remaining: number, limit: number, used: number): Site {
    return {
      ...siteWithSyncState('unknown'),
      site_type: probeType,
      quota_probe: { probe_type: probeType, status: 'ok', remaining_min: remaining, limit, used, unit },
    }
  }

  it('formats a non-builtin credits probe as remaining over limit', () => {
    const acme = summarySite('acme.example/chat', 'credits', 1999, 2000, 1)
    expect(formatQuotaProbeBalance(acme.quota_probe)).toBe('1,999 / 2,000')
    expect(siteBalanceDetails(acme)).toEqual([
      { label: 'accountBalance', value: '1,999 credits' },
      { label: 'totalLimit', value: '2,000 credits' },
    ])
  })

  it('formats a non-builtin percent probe the same as grok', () => {
    const grok = summarySite('grok', 'percent', 95, 100, 5)
    const acme = summarySite('acme', 'percent', 95, 100, 5)
    expect(formatQuotaProbeBalance(acme.quota_probe)).toBe(formatQuotaProbeBalance(grok.quota_probe))
    expect(formatQuotaProbeBalance(acme.quota_probe)).toBe('95%')
    expect(siteBalanceDetails(acme)).toEqual(siteBalanceDetails(grok))
    expect(siteBalanceDetails(acme)).toEqual([{ label: 'accountBalance', value: '95%' }])
  })

  it('keeps usd amounts on the currency path', () => {
    const acme = summarySite('acme', 'usd', 12.5, 100, 1)
    expect(formatQuotaProbeBalance(acme.quota_probe)).toBe('$12.50')
    expect(siteBalanceDetails(acme)).toEqual([{ label: 'accountBalance', value: '$12.50' }])
  })
})

describe('siteAccountCounts', () => {
  it('shows valid accounts out of the stored total even when site state counts reset to zero', () => {
    expect(siteAccountCounts({ account_count: 1, valid_account_count: 0, api_key_count: 0 })).toEqual({ valid: 0, total: 1 })
    expect(siteAccountCounts({ account_count: 3, valid_account_count: 2 })).toEqual({ valid: 2, total: 3 })
  })

  it('falls back to the cached count and treats those accounts as valid', () => {
    expect(siteAccountCounts({ api_key_count: 2 })).toEqual({ valid: 2, total: 2 })
    expect(siteAccountCounts({})).toEqual({ valid: 0, total: 0 })
  })

  it('never reports more valid accounts than total', () => {
    expect(siteAccountCounts({ account_count: 1, valid_account_count: 5 })).toEqual({ valid: 1, total: 1 })
  })
})

describe('quotaEntryKind', () => {
  it('uses an explicit kind', () => {
    expect(quotaEntryKind({ kind: 'package', label: 'balance' })).toBe('package')
    expect(quotaEntryKind({ kind: 'balance' })).toBe('balance')
    expect(quotaEntryKind({ kind: 'window', label: 'usage' })).toBe('window')
    expect(quotaEntryKind({ kind: 'other', label: 'balance' })).toBeUndefined()
  })

  it('infers kind from the label when kind is absent', () => {
    expect(quotaEntryKind({ label: 'balance' })).toBe('balance')
    expect(quotaEntryKind({ label: 'Balance' })).toBe('balance')
    for (const label of ['five_hour', 'weekly', 'daily', 'monthly', '5h', 'WEEKLY']) {
      expect(quotaEntryKind({ label })).toBe('window')
    }
    expect(quotaEntryKind({ label: 'usage' })).toBeUndefined()
    expect(quotaEntryKind({ label: 'package' })).toBeUndefined()
    expect(quotaEntryKind({})).toBeUndefined()
  })
})

describe('formatQuotaEntryAmount', () => {
  it('formats percent, credits, currency, and plain numbers', () => {
    expect(formatQuotaEntryAmount({ unit: 'percent', remaining: 85.7 }, 'remaining')).toBe('85.7%')
    expect(formatQuotaEntryAmount({ unit: 'percent', limit: 100 }, 'limit')).toBe('100%')
    expect(formatQuotaEntryAmount({ unit: 'credits', remaining: 1999 }, 'remaining')).toBe('1,999')
    expect(formatQuotaEntryAmount({ unit: 'credits', remaining: 499.5 }, 'remaining')).toBe('500')
    expect(formatQuotaEntryAmount({ unit: 'usd', remaining: 12.5 }, 'remaining')).toBe('$12.50')
    expect(formatQuotaEntryAmount({ unit: 'CNY', used: 3 }, 'used')).toBe('¥3.00')
    expect(formatQuotaEntryAmount({ remaining: 12.5 }, 'remaining')).toBe('12.5')
    expect(formatQuotaEntryAmount({ unit: 'tokens', remaining: 12 }, 'remaining')).toBe('12')
    expect(formatQuotaEntryAmount({ unit: 'percent' }, 'remaining')).toBe('')
  })
})

describe('legacy quota entries', () => {
  it('synthesizes a Grok window from the old percent fields', () => {
    expect(legacyGrokQuotaEntries({
      quota_known: true,
      remaining_percent: 95,
      used_percent: 5,
      quota_product: 'GrokBuild',
      billing_period_end: '2026-10-19T17:04:45Z',
    })).toEqual([{
      label: 'usage',
      kind: 'window',
      unit: 'percent',
      name: 'GrokBuild',
      remaining: 95,
      used: 5,
      limit: 100,
      reset_at: '2026-10-19T17:04:45Z',
    }])
  })

  it('returns no Grok entries when the quota is unknown', () => {
    expect(legacyGrokQuotaEntries({ quota_known: false, remaining_percent: 95, quota_product: 'GrokBuild' })).toEqual([])
  })
})

function catalogEntry(siteType: string, accounts?: SiteAccountCapabilities): SiteTypeInfo {
  return {
    site_type: siteType,
    display_name: siteType,
    subtitle: '',
    credential_key: 'key',
    credential_type: 'api_key',
    requires_base_url: false,
    show_in_create_dialog: true,
    accounts,
  }
}

const deviceAccounts: SiteAccountCapabilities = {
  login_kind: 'oauth2_device_code',
  supports_import: false,
  supports_regions: false,
}

const pollAccounts: SiteAccountCapabilities = {
  login_kind: 'custom_poll',
  supports_import: true,
  supports_regions: true,
}

describe('isAccountLoginSiteType', () => {
  it('is true when the catalog lists accounts for the type', () => {
    const catalog = [catalogEntry('grok', deviceAccounts), catalogEntry('plugin', pollAccounts)]
    expect(isAccountLoginSiteType('grok', catalog)).toBe(true)
    expect(isAccountLoginSiteType('plugin', catalog)).toBe(true)
  })

  it('is false when the catalog lists the type without accounts', () => {
    const catalog = [catalogEntry('openai'), catalogEntry('grok')]
    expect(isAccountLoginSiteType('openai', catalog)).toBe(false)
    expect(isAccountLoginSiteType('grok', catalog)).toBe(false)
  })

  it('falls back to grok when the catalog is missing or does not list the type', () => {
    expect(isAccountLoginSiteType('grok')).toBe(true)
    expect(isAccountLoginSiteType('grok', null)).toBe(true)
    expect(isAccountLoginSiteType('grok', [])).toBe(true)
    expect(isAccountLoginSiteType('grok', [catalogEntry('openai')])).toBe(true)
  })

  it('treats credential_type account as an account-login site when accounts is omitted', () => {
    const info = { ...catalogEntry('acme.pool/chat'), credential_type: 'account' as const }
    expect(isAccountLoginSiteType('acme.pool/chat', [info])).toBe(true)
  })

  it('is false for an unknown type', () => {
    expect(isAccountLoginSiteType('plugin')).toBe(false)
    expect(isAccountLoginSiteType('openai')).toBe(false)
    expect(isAccountLoginSiteType(null)).toBe(false)
    expect(isAccountLoginSiteType('plugin', [catalogEntry('openai')])).toBe(false)
  })
})

describe('plugin site type fallback', () => {
  const pluginType = 'acme.example/chat'
  const info: SiteTypeInfo = {
    site_type: pluginType,
    display_name: '示例',
    subtitle: '示例站点',
    credential_key: 'api_key',
    credential_type: 'api_key',
    requires_base_url: true,
    show_in_create_dialog: true,
    icon_url: '',
    plugin_id: 'acme.example',
    provider_status: 'enabled',
  }

  it('uses a neutral color and no icon when a plugin site type has no local mapping', () => {
    expect(() => {
      siteTypeColor(pluginType)
      siteTypeIcon(pluginType)
      siteTypeIcon(pluginType, [info])
      formatSiteTypeLabel(pluginType, [info])
    }).not.toThrow()
    expect(siteTypeColor(pluginType)).toBeNull()
    expect(siteTypeColor('openai')).toBe('#10a37f')
    expect(siteTypeIcon(pluginType)).toBeNull()
    expect(siteTypeIcon(pluginType, [info])).toBeNull()
    expect(siteTypeIcon(pluginType, [{ ...info, icon_url: 'https://cdn.example/icon.webp' }])).toBe('https://cdn.example/icon.webp')
    expect(formatSiteTypeLabel(pluginType)).toBe(pluginType)
    expect(formatSiteTypeLabel(pluginType, [info])).toBe('示例')
  })
})

describe('accountQuotaEntries', () => {
  const fallback = [{ label: 'usage', kind: 'window' as const, unit: 'percent', remaining: 2, limit: 100 }]

  it('prefers stored entries and falls back when they are missing', () => {
    const entries = [{ label: 'usage', kind: 'window' as const, unit: 'percent', remaining: 1, limit: 100 }]
    expect(accountQuotaEntries(entries, fallback)).toBe(entries)
    expect(accountQuotaEntries([], fallback)).toEqual(fallback)
    expect(accountQuotaEntries(undefined, fallback)).toEqual(fallback)
    expect(accountQuotaEntries(null, fallback)).toEqual(fallback)
  })
})
