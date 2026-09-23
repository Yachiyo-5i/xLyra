import { upstreamEndpointTypes } from '@/features/models/lib/model-helpers'
import type { SiteAPIKey, SiteAPIKeyModel, SiteModel, SiteModelTestProtocol } from '@/features/sites/api/sites'
import { apiKeyModels } from '@/features/sites/lib/site-utils'

const PROTOCOL_ENDPOINT_TYPES: Record<Exclude<SiteModelTestProtocol, 'auto'>, readonly string[]> = {
  chat_completions: ['openai', 'google-gemini'],
  responses: ['openai-response'],
  messages: ['anthropic-messages'],
}

export function filterSiteModelTestModels(input: {
  models: SiteModel[]
  apiKeys: SiteAPIKey[]
  protocol: SiteModelTestProtocol
  credentialId: string
  supportsMultipleAPIKeys: boolean
}): SiteModel[] {
  return input.models.filter((model) => siteModelMatchesTestFilters(model, input))
}

function siteModelMatchesTestFilters(model: SiteModel, input: {
  apiKeys: SiteAPIKey[]
  protocol: SiteModelTestProtocol
  credentialId: string
  supportsMultipleAPIKeys: boolean
}): boolean {
  if (!input.supportsMultipleAPIKeys) {
    return endpointTypesMatchProtocol(siteModelEndpointTypes(model), input.protocol)
  }

  if (input.credentialId === 'auto') {
    const declaringKeys = input.apiKeys.filter(apiKeyDeclaresModels)
    if (!declaringKeys.length) {
      return endpointTypesMatchProtocol(siteModelEndpointTypes(model), input.protocol)
    }
    return declaringKeys.some((apiKey) => apiKeySupportsModel(apiKey, model, input.protocol))
  }

  const apiKey = input.apiKeys.find((item) => item.id === input.credentialId)
  if (!apiKey) return false
  if (!apiKeyDeclaresModels(apiKey)) {
    return endpointTypesMatchProtocol(siteModelEndpointTypes(model), input.protocol)
  }
  return apiKeySupportsModel(apiKey, model, input.protocol)
}

function apiKeySupportsModel(apiKey: SiteAPIKey, model: SiteModel, protocol: SiteModelTestProtocol): boolean {
  const item = enabledKeyModel(apiKey, model)
  if (!item) return false
  if (protocol === 'auto' && keyModelHasNoProtocols(item)) return false
  const fallback = siteModelEndpointTypes(model) ?? []
  return endpointTypesMatchProtocol(upstreamEndpointTypes(item, fallback), protocol)
}

function enabledKeyModel(apiKey: SiteAPIKey, model: SiteModel): SiteAPIKeyModel | undefined {
  const items = apiKeyModels(apiKey).filter((item) => item.enabled)
  const byId = items.find((item) => item.site_model_id === model.id)
  if (byId) return byId
  const names = modelNames(model)
  return items.find((item) => names.has(normalizeName(item.name)))
}

function apiKeyDeclaresModels(apiKey: SiteAPIKey): boolean {
  return apiKeyModels(apiKey).length > 0
}

function keyModelHasNoProtocols(item: SiteAPIKeyModel): boolean {
  if (item.endpoint_override?.mode === 'disabled') return true
  return Array.isArray(item.effective_endpoint_types) && item.effective_endpoint_types.length === 0
}

function endpointTypesMatchProtocol(types: string[] | null, protocol: SiteModelTestProtocol): boolean {
  if (protocol === 'auto') return true
  if (types === null) return true
  const normalized = upstreamEndpointTypes({ name: '', enabled: true, supported_endpoint_types: types })
  if (!normalized.length) return false
  return normalized.some((type) => PROTOCOL_ENDPOINT_TYPES[protocol].includes(type))
}

function siteModelEndpointTypes(model: SiteModel): string[] | null {
  const capabilities = model.capabilities
  if (!capabilities || !('supported_endpoint_types' in capabilities)) return null
  const direct = capabilities.supported_endpoint_types
  if (Array.isArray(direct)) {
    return direct.filter((value): value is string => typeof value === 'string')
  }
  const raw = capabilities.raw
  if (raw && typeof raw === 'object' && Array.isArray((raw as Record<string, unknown>).supported_endpoint_types)) {
    return ((raw as Record<string, unknown>).supported_endpoint_types as unknown[]).filter((value): value is string => typeof value === 'string')
  }
  return null
}

function modelNames(model: SiteModel): Set<string> {
  return new Set([model.upstream_model_name, model.display_name].map(normalizeName).filter(Boolean))
}

function normalizeName(value: string | null | undefined): string {
  return value?.trim().toLowerCase() ?? ''
}
