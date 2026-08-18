import { describe, expect, it } from 'vitest'
import type { AnalyticsDataset, AnalyticsUsageFact } from '@/features/analytics/api/analytics'
import type { AnalyticsFiltersState } from '@/features/analytics/components/analytics-filter-bar'
import { analyticsModelKeys, buildAnalyticsUsage, filterAnalyticsContributionPoints } from './analytics-dataset'

function fact(apiKeyId: string, requests: number, cost: number): AnalyticsUsageFact {
  return {
    date: '2026-08-17',
    site_key: 'site-1',
    site_id: 'site-1',
    site_label: 'Site 1',
    model_key: 'model-1',
    model_id: 'model-1',
    model_label: 'Model 1',
    api_key_key: apiKeyId,
    api_key_id: apiKeyId,
    api_key_label: apiKeyId,
    currency: 'USD',
    requests,
    success_count: requests,
    failure_count: 0,
    prompt_tokens: requests * 10,
    completion_tokens: requests * 5,
    cached_tokens: 0,
    total_tokens: requests * 15,
    cost,
    latency_count: requests,
    latency_total_ms: requests * 100,
    latency_max_ms: 100,
    upstream_latency_count: requests,
    upstream_latency_total_ms: requests * 80,
  }
}

const dataset: AnalyticsDataset = {
  meta: {
    from: '2026-08-17',
    to: '2026-08-17',
    previous_from: '2026-08-16',
    previous_to: '2026-08-16',
    days: 1,
    timezone: 'UTC',
    generated_at: '2026-08-17T12:00:00Z',
    granularity: 'hour',
    fact_count: 4,
    fact_limit: 50000,
  },
  current: [fact('user-a', 2, 0.2), fact('user-b', 3, 0.3)],
  previous: [fact('user-a', 1, 0.1), fact('user-b', 4, 0.4)],
}

function filters(apiKeyIds: string[]): AnalyticsFiltersState {
  return {
    preset: 'today',
    from: '2026-08-17',
    to: '2026-08-17',
    siteIds: [],
    modelKeys: [],
    apiKeyIds,
    currency: '',
  }
}

describe('analytics dataset', () => {
  it('filters current and previous usage by user locally', () => {
    const usage = buildAnalyticsUsage(dataset, filters(['user-a']), 'model')

    expect(usage.totals.requests).toBe(2)
    expect(usage.totals.cost).toBeCloseTo(0.2)
    expect(usage.totals.previous_period?.requests).toBe(1)
    expect(usage.breakdowns.api_key).toHaveLength(1)
    expect(usage.breakdowns.api_key[0]?.id).toBe('user-a')
  })

  it('filters contribution points by the global user selection', () => {
    const points = [
      { date: '2026-08-17', api_key_id: 'user-a', api_key_name: 'A', total_tokens: 10, cost: 0.1, currency: 'USD' },
      { date: '2026-08-17', api_key_id: 'user-b', api_key_name: 'B', total_tokens: 20, cost: 0.2, currency: 'USD' },
    ]

    expect(filterAnalyticsContributionPoints(points, ['user-b'])).toEqual([points[1]])
    expect(filterAnalyticsContributionPoints(points, [])).toBe(points)
  })

  it('lists only models present in the selected range', () => {
    const facts = [
      { ...fact('user-a', 1, 0.1), model_key: 'model-z' },
      { ...fact('user-b', 1, 0.1), model_key: 'model-a' },
      { ...fact('user-c', 1, 0.1), model_key: 'unknown' },
    ]

    expect(analyticsModelKeys(facts)).toEqual(['model-a', 'model-z'])
    expect(analyticsModelKeys(facts, {
      siteIds: [],
      apiKeyIds: ['user-a'],
      currency: 'USD',
    })).toEqual(['model-z'])
  })
})
