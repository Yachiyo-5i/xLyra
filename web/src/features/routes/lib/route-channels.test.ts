import { describe, expect, it } from 'vitest'
import { routeChannelRowsFromMatrix } from '@/features/routes/lib/route-channels'
import type { CanonicalModelMatrixRow, Site, SiteAPIKey } from '@/features/sites/api/sites'

describe('routeChannelRowsFromMatrix', () => {
  it('applies the credential cost multiplier to token and per-request prices', () => {
    const site: Site = {
      id: 'site-1',
      name: 'Primary',
      slug: 'primary',
      site_type: 'openai',
      base_url: 'https://example.test',
      status: 'active',
      enabled: true,
      routing_priority: 1,
      supports_multiple_api_keys: true,
      supports_api_key_cost_multiplier: true,
      meta: {},
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }
    const apiKey: SiteAPIKey = {
      id: 'credential-1',
      name: 'Primary key',
      routing_priority: 5,
      upstream_cost_multiplier: 1.5,
      group: 'default',
      key: 'sk-***',
      status: 'active',
      enabled: true,
      models: ['gpt-test'],
    }
    const matrixRow: CanonicalModelMatrixRow = {
      site_id: site.id,
      site_name: site.name,
      site_slug: site.slug,
      site_type: site.site_type,
      site_enabled: true,
      site_model_id: 'site-model-1',
      upstream_model_name: 'gpt-test',
      display_name: 'GPT Test',
      model_status: 'active',
      canonical_match_source: 'manual',
      canonical_match_confidence: 100,
      api_key_count: 1,
      available_api_key_count: 1,
      pricing: [{
        group_name: 'default',
        currency: 'USD',
        input_value: 2,
        output_value: 4,
        per_request_value: 0.2,
        available: true,
      }],
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    }

    const rows = routeChannelRowsFromMatrix([matrixRow], [site], { [site.id]: [apiKey] }, undefined, [], [])

    expect(rows).toHaveLength(1)
    expect(rows[0]?.pricing).toMatchObject({
      base_input_value: 2,
      base_output_value: 4,
      base_per_request_value: 0.2,
      input_value: 3,
      output_value: 6,
      upstream_cost_multiplier: 1.5,
    })
    expect(rows[0]?.pricing?.per_request_value).toBeCloseTo(0.3)

    site.supports_api_key_cost_multiplier = false
    const fixedMultiplierRows = routeChannelRowsFromMatrix(
      [matrixRow],
      [site],
      { [site.id]: [apiKey] },
      undefined,
      [],
      [],
    )
    expect(fixedMultiplierRows[0]?.apiKeyUpstreamCostMultiplier).toBeUndefined()
    expect(fixedMultiplierRows[0]?.pricing).toMatchObject({
      input_value: 2,
      output_value: 4,
      per_request_value: 0.2,
      upstream_cost_multiplier: 1,
    })
  })
})
