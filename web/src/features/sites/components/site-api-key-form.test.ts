import { describe, expect, it } from 'vitest'
import {
  DEFAULT_API_KEY_FORM_DRAFT,
  parseSiteAPIKeyForm,
} from '@/features/sites/components/site-api-key-form-data'

describe('parseSiteAPIKeyForm', () => {
  it('includes the multiplier for OpenAI aggregation keys', () => {
    expect(
      parseSiteAPIKeyForm(
        {
          ...DEFAULT_API_KEY_FORM_DRAFT,
          name: ' Primary ',
          apiKey: ' sk-test ',
          routingPriority: '4.5',
          upstreamCostMultiplier: '1.25',
        },
        { includeCostMultiplier: true, requireAPIKey: true },
      ),
    ).toEqual({
      name: 'Primary',
      apiKey: 'sk-test',
      routingPriority: 4.5,
      upstreamCostMultiplier: 1.25,
    })
  })

  it('omits the multiplier for other API key providers', () => {
    expect(
      parseSiteAPIKeyForm(
        {
          ...DEFAULT_API_KEY_FORM_DRAFT,
          apiKey: 'official-key',
          upstreamCostMultiplier: '9',
        },
        { includeCostMultiplier: false, requireAPIKey: true },
      ),
    ).toEqual({
      name: '',
      apiKey: 'official-key',
      routingPriority: 1,
    })
  })

  it('rejects missing keys and invalid routing configuration', () => {
    expect(
      parseSiteAPIKeyForm(DEFAULT_API_KEY_FORM_DRAFT, {
        includeCostMultiplier: true,
        requireAPIKey: true,
      }),
    ).toBeNull()
    expect(
      parseSiteAPIKeyForm(
        { ...DEFAULT_API_KEY_FORM_DRAFT, apiKey: 'key', routingPriority: '5.1' },
        { includeCostMultiplier: false, requireAPIKey: true },
      ),
    ).toBeNull()
  })
})
