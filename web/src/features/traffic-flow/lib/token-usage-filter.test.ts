import { describe, expect, it } from 'vitest'
import type { TrafficFlowUsageCell } from '@/features/traffic-flow/api/traffic-flow'
import { VENDOR_FLOW_COLORS } from '@/features/traffic-flow/lib/model-visual'
import {
  deriveTokenUsage,
  emptyTokenUsageFilter,
  isTokenUsageFilterActive,
  toggleTokenUsageFilter,
} from '@/features/traffic-flow/lib/token-usage-filter'

function cell(partial: Partial<TrafficFlowUsageCell> & Pick<TrafficFlowUsageCell, 'api_key_id' | 'total_tokens'>): TrafficFlowUsageCell {
  return {
    api_key_name: partial.api_key_name ?? partial.api_key_id,
    site_id: partial.site_id ?? '',
    site_name: partial.site_name ?? partial.site_id ?? '',
    model_key: partial.model_key ?? '',
    model_provider: partial.model_provider ?? '',
    input_tokens: partial.input_tokens ?? 0,
    output_tokens: partial.output_tokens ?? 0,
    cached_tokens: partial.cached_tokens ?? 0,
    ...partial,
  }
}

const sample = [
  cell({ api_key_id: 'key-a', api_key_name: 'Alpha', site_id: 'site-1', site_name: 'Primary', model_key: 'gpt-5.4', model_provider: 'openai', total_tokens: 100, input_tokens: 80, output_tokens: 20, cached_tokens: 30 }),
  cell({ api_key_id: 'key-a', api_key_name: 'Alpha', site_id: 'site-2', site_name: 'Secondary', model_key: 'claude-sonnet-4.6', model_provider: 'anthropic', total_tokens: 50, input_tokens: 40, output_tokens: 10, cached_tokens: 5 }),
  cell({ api_key_id: 'key-b', api_key_name: 'Beta', site_id: 'site-1', site_name: 'Primary', model_key: 'gpt-5.4', model_provider: 'openai', total_tokens: 20, input_tokens: 12, output_tokens: 8, cached_tokens: 4 }),
]

describe('token-usage-filter', () => {
  it('ranks every dimension from the unfiltered window and keeps split totals', () => {
    const breakdown = deriveTokenUsage(sample, emptyTokenUsageFilter())
    expect(breakdown.total_tokens).toBe(170)
    expect(breakdown.input_tokens).toBe(132)
    expect(breakdown.output_tokens).toBe(38)
    expect(breakdown.cached_tokens).toBe(39)
    expect(breakdown.keys.map((item) => item.id)).toEqual(['key-a', 'key-b'])
    expect(breakdown.keys[0]?.total_tokens).toBe(150)
    expect(breakdown.keys[0]?.siteCount).toBe(2)
    expect(breakdown.keys[0]?.modelCount).toBe(2)
    expect(breakdown.sites.map((item) => item.id)).toEqual(['site-1', 'site-2'])
    expect(breakdown.models.map((item) => item.id)).toEqual(['gpt-5.4', 'claude-sonnet-4.6'])
    expect(breakdown.vendors.map((item) => item.id)).toEqual(['OpenAI', 'Anthropic'])
    expect(breakdown.vendors[0]?.color).toBe(VENDOR_FLOW_COLORS.OpenAI)
    expect(breakdown.keys[0]?.share).toBeCloseTo(150 / 170)
  })

  it('cross-filters sites and models when a key is selected, and keeps other keys visible', () => {
    const breakdown = deriveTokenUsage(sample, { ...emptyTokenUsageFilter(), keyIds: ['key-a'] })
    expect(breakdown.total_tokens).toBe(150)
    expect(breakdown.keys.map((item) => item.id)).toEqual(['key-a', 'key-b'])
    expect(breakdown.sites.map((item) => item.id)).toEqual(['site-1', 'site-2'])
    expect(breakdown.sites.find((item) => item.id === 'site-1')?.total_tokens).toBe(100)
    expect(breakdown.models.map((item) => item.id)).toEqual(['gpt-5.4', 'claude-sonnet-4.6'])
    expect(breakdown.vendors.map((item) => item.id)).toEqual(['OpenAI', 'Anthropic'])
  })

  it('intersects multiple keys and a site for the remaining model list', () => {
    const breakdown = deriveTokenUsage(sample, { ...emptyTokenUsageFilter(), keyIds: ['key-a', 'key-b'], siteIds: ['site-1'] })
    expect(breakdown.total_tokens).toBe(120)
    expect(breakdown.models.map((item) => item.id)).toEqual(['gpt-5.4'])
    expect(breakdown.sites.map((item) => item.id)).toEqual(['site-1', 'site-2'])
    expect(breakdown.keys.map((item) => item.id)).toEqual(['key-a', 'key-b'])
  })

  it('filters by inferred vendor and can cancel the same toggle', () => {
    const withVendor = toggleTokenUsageFilter(emptyTokenUsageFilter(), 'vendors', 'Anthropic')
    expect(isTokenUsageFilterActive(withVendor)).toBe(true)
    const breakdown = deriveTokenUsage(sample, withVendor)
    expect(breakdown.total_tokens).toBe(50)
    expect(breakdown.keys.map((item) => item.id)).toEqual(['key-a'])
    expect(breakdown.sites.map((item) => item.id)).toEqual(['site-2'])
    expect(toggleTokenUsageFilter(withVendor, 'vendors', 'Anthropic')).toEqual(emptyTokenUsageFilter())
  })

  it('keeps empty model keys as their own rank', () => {
    const breakdown = deriveTokenUsage([
      cell({ api_key_id: 'key-a', api_key_name: 'Alpha', total_tokens: 8, input_tokens: 8 }),
      cell({ api_key_id: 'key-a', api_key_name: 'Alpha', model_key: 'gpt-5.4', model_provider: 'openai', total_tokens: 2, input_tokens: 1, output_tokens: 1 }),
    ], emptyTokenUsageFilter())
    expect(breakdown.models.map((item) => item.id)).toEqual(['', 'gpt-5.4'])
    expect(breakdown.models[0]?.total_tokens).toBe(8)
    const filtered = deriveTokenUsage([
      cell({ api_key_id: 'key-a', total_tokens: 8, input_tokens: 8 }),
      cell({ api_key_id: 'key-a', model_key: 'gpt-5.4', model_provider: 'openai', total_tokens: 2, output_tokens: 2 }),
    ], { ...emptyTokenUsageFilter(), modelKeys: [''] })
    expect(filtered.total_tokens).toBe(8)
    expect(filtered.models).toHaveLength(2)
  })
})
