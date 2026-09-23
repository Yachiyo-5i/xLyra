import { describe, expect, it } from 'vitest'

import type { SiteAPIKey, SiteModel } from '@/features/sites/api/sites'
import { filterSiteModelTestModels } from '@/features/sites/lib/site-model-test-filters'

const chatModel = model('chat', ['openai'])
const responsesModel = model('responses', ['openai-response'])
const messagesModel = model('messages', ['anthropic-messages'])
const bothModel = model('both', ['openai', 'openai-response'])

describe('filterSiteModelTestModels', () => {
  it('hides models the selected API key does not enable', () => {
    const visible = filterSiteModelTestModels({
      models: [chatModel, responsesModel],
      apiKeys: [apiKey('key-a', [item(chatModel)])],
      protocol: 'auto',
      credentialId: 'key-a',
      supportsMultipleAPIKeys: true,
    })

    expect(visible.map((item) => item.id)).toEqual(['chat'])
  })

  it('hides models that do not support the selected protocol on that key', () => {
    const visible = filterSiteModelTestModels({
      models: [bothModel],
      apiKeys: [apiKey('key-a', [item(bothModel, ['openai'])])],
      protocol: 'responses',
      credentialId: 'key-a',
      supportsMultipleAPIKeys: true,
    })

    expect(visible).toEqual([])
  })

  it('keeps a model when the selected key enables the selected protocol', () => {
    const visible = filterSiteModelTestModels({
      models: [chatModel, responsesModel, messagesModel],
      apiKeys: [apiKey('key-a', [
        item(chatModel, ['openai']),
        item(responsesModel, ['openai-response']),
      ])],
      protocol: 'responses',
      credentialId: 'key-a',
      supportsMultipleAPIKeys: true,
    })

    expect(visible.map((item) => item.id)).toEqual(['responses'])
  })

  it('uses any enabled key when the credential is automatic', () => {
    const visible = filterSiteModelTestModels({
      models: [chatModel, responsesModel],
      apiKeys: [
        apiKey('key-a', [item(chatModel, ['openai'])]),
        apiKey('key-b', [item(responsesModel, ['openai-response'])]),
      ],
      protocol: 'messages',
      credentialId: 'auto',
      supportsMultipleAPIKeys: true,
    })

    expect(visible).toEqual([])

    const responses = filterSiteModelTestModels({
      models: [chatModel, responsesModel],
      apiKeys: [
        apiKey('key-a', [item(chatModel, ['openai'])]),
        apiKey('key-b', [item(responsesModel, ['openai-response'])]),
      ],
      protocol: 'responses',
      credentialId: 'auto',
      supportsMultipleAPIKeys: true,
    })

    expect(responses.map((item) => item.id)).toEqual(['responses'])
  })

  it('filters a single-credential site by the model protocol', () => {
    const visible = filterSiteModelTestModels({
      models: [chatModel, messagesModel],
      apiKeys: [],
      protocol: 'messages',
      credentialId: 'auto',
      supportsMultipleAPIKeys: false,
    })

    expect(visible.map((item) => item.id)).toEqual(['messages'])
  })

  it('hides a key model whose protocols were explicitly cleared', () => {
    const visible = filterSiteModelTestModels({
      models: [bothModel],
      apiKeys: [apiKey('key-a', [item(bothModel, [])])],
      protocol: 'auto',
      credentialId: 'key-a',
      supportsMultipleAPIKeys: true,
    })

    expect(visible).toEqual([])
  })
})

function model(id: string, endpointTypes: string[]): SiteModel {
  return {
    id,
    site_id: 'site-1',
    upstream_model_name: id,
    display_name: id,
    capabilities: { supported_endpoint_types: endpointTypes },
    status: 'active',
    created_at: '',
    updated_at: '',
  }
}

function apiKey(id: string, models: SiteAPIKey['model_items']): SiteAPIKey {
  return {
    id,
    name: id,
    routing_priority: 1,
    upstream_cost_multiplier: 1,
    key: 'sk-***',
    status: 'active',
    enabled: true,
    models: (models ?? []).map((item) => item.name),
    model_items: models,
  }
}

function item(model: SiteModel, effectiveEndpointTypes?: string[]): NonNullable<SiteAPIKey['model_items']>[number] {
  return {
    name: model.upstream_model_name,
    enabled: true,
    site_model_id: model.id,
    supported_endpoint_types: effectiveEndpointTypes ?? ['openai', 'openai-response', 'anthropic-messages'],
    effective_endpoint_types: effectiveEndpointTypes,
  }
}
