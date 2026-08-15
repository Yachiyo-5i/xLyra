import { describe, expect, it } from 'vitest'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import { buildMappingModelKeys, formatCompactDollarQuota, hasResettableQuota, sortAPIKeysForDisplay } from '@/features/api-keys/lib/api-key-utils'
import type { CanonicalModelItem, SiteModel } from '@/features/sites/api/sites'

function apiKey(id: string, status: string, createdAt: string): DownstreamAPIKey {
  return {
    id,
    name: id,
    key_prefix: '',
    masked_key: '',
    scope: '',
    status,
    model_policy: 'allow_all',
    site_policy: 'allow_all',
    quota_used: 0,
    quota_unlimited: true,
    sites: [],
    created_at: createdAt,
    updated_at: createdAt,
  }
}

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

describe('sortAPIKeysForDisplay', () => {
  it('sorts active keys by creation time ascending and keeps disabled keys last', () => {
    const apiKeys = [
      apiKey('active-new', 'active', '2026-02-01T00:00:00Z'),
      apiKey('disabled-old', 'disabled', '2025-01-01T00:00:00Z'),
      apiKey('active-old', 'active', '2026-01-01T00:00:00Z'),
    ]

    expect(sortAPIKeysForDisplay(apiKeys).map((item) => item.id)).toEqual([
      'active-old',
      'active-new',
      'disabled-old',
    ])
  })
})

describe('buildMappingModelKeys', () => {
  const canonicalModels = [
    { id: 'canonical-live', model_key: 'live-model' },
    { id: 'canonical-built-in', model_key: 'built-in-only' },
  ] as CanonicalModelItem[]
  const siteModels = [
    { id: 'site-model-live', canonical_model_id: 'canonical-live' },
    { id: 'site-model-built-in', canonical_model_id: 'canonical-built-in' },
  ] as SiteModel[]

  it('only includes canonical models backed by the current site models', () => {
    expect(buildMappingModelKeys(canonicalModels, [siteModels[0]], [], [])).toEqual(['live-model'])
  })

  it('respects selected site models and retains saved targets', () => {
    expect(buildMappingModelKeys(canonicalModels, siteModels, ['site-model-live'], ['legacy-model'])).toEqual([
      'legacy-model',
      'live-model',
    ])
  })
})
