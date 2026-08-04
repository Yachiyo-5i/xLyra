import { describe, expect, it } from 'vitest'
import {
  marketplacePricingRows,
  marketplaceUnpricedCredentialRows,
} from './models'

const site = {
  siteId: 'site-1',
  siteName: 'Aggregate',
  siteType: 'openai',
  modelId: 'model-1',
  displayName: 'GPT',
  upstreamName: 'gpt-test',
  enabled: true,
  supportedEndpointTypes: ['openai'],
  supportsMultipleAPIKeys: true,
  pricingRows: [],
}

const pricing = {
  id: 'pricing-1',
  site_id: 'site-1',
  site_name: 'Aggregate',
  site_slug: 'aggregate',
  site_type: 'openai',
  site_enabled: true,
  site_model_id: 'model-1',
  model_name: 'gpt-test',
  group_name: 'default',
  quota_type: 0,
  billing_type: 'tokens',
  currency: 'USD',
  group_ratio: 1,
  input_value: 2,
  output_value: 6,
  available: true,
  raw: {},
  created_at: '',
  updated_at: '',
  credential_pricings: [
    {
      credential_id: 'key-1',
      credential_name: 'Primary',
      routing_priority: 5,
      group_ratio: 1.2,
      credential_enabled: true,
      credential_usable: true,
      model_enabled: true,
      model_available: true,
      billing_type: 'tokens',
      currency: 'USD',
      input_value: 2.4,
      output_value: 7.2,
    },
  ],
}

describe('marketplacePricingRows', () => {
  it('expands OpenAI aggregate pricing by API key', () => {
    const rows = marketplacePricingRows(
      site,
      pricing,
      {},
      { enabled: 0, total: 0 },
    )

    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({
      id: 'pricing-1:key-1',
      apiKeyId: 'key-1',
      apiKeyName: 'Primary',
      groupRatio: 1.2,
      inputValue: 2.4,
      outputValue: 7.2,
    })
  })

  it('keeps NewAPI pricing at upstream group dimension', () => {
    const rows = marketplacePricingRows(
      { ...site, siteType: 'newapi', supportsMultipleAPIKeys: false },
      { ...pricing, site_type: 'newapi', group_name: 'vip', group_ratio: 2 },
      {},
      { enabled: 0, total: 0 },
    )

    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({
      id: 'pricing-1',
      groupName: 'vip',
      groupRatio: 2,
    })
    expect(rows[0].apiKeyId).toBeUndefined()
  })

  it('keeps API key dimension when base pricing is missing', () => {
    const rows = marketplaceUnpricedCredentialRows(site, {
      'site-1': [
        {
          id: 'key-1',
          name: 'Primary',
          routing_priority: 5,
          upstream_cost_multiplier: 1.2,
          group: null,
          key: 'sk-***',
          status: 'active',
          enabled: true,
          models: ['gpt-test'],
          model_items: [{ name: 'gpt-test', enabled: true }],
        },
      ],
    })

    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({
      apiKeyId: 'key-1',
      apiKeyName: 'Primary',
      groupRatio: 1.2,
      available: false,
    })
  })
})
