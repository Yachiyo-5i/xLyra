import { apiFetch } from '@/lib/http'

export type ModelPriceBillingType = 'tokens' | 'per_request'
type ModelPriceStatus = 'priced' | 'missing' | 'manual' | 'synced'

export type ModelPriceFilters = {
  q?: string
  siteId?: string
  siteType?: string
  provider?: string
  modelKey?: string
  canonicalModelId?: string
  billingType?: ModelPriceBillingType | 'all'
  pricingStatus?: ModelPriceStatus
}

type ModelPriceSite = {
  id: string
  name: string
  slug: string
  type: string
  enabled: boolean
}

type ModelPriceModel = {
  site_model_id: string
  upstream_model_name: string
  display_name: string
  status: string
  canonical_model_id?: string | null
  canonical_model_key?: string | null
  canonical_display_name?: string | null
  provider?: string | null
  category?: string | null
}

type ModelPricePricing = {
  id: string
  group_name: string
  billing_type: ModelPriceBillingType | string
  quota_type: number
  currency: string
  group_ratio?: number | null
  input_value?: number | null
  output_value?: number | null
  audio_output_value?: number | null
  cache_ratio?: number | null
  cache_input_value?: number | null
  create_cache_ratio?: number | null
  create_cache_input_value?: number | null
  create_cache_1h_ratio?: number | null
  create_cache_1h_input_value?: number | null
  per_request_value?: number | null
  pricing_source?: string | null
  manual_override?: boolean
  manual_updated_at?: string | null
  manual_note?: string | null
  available: boolean
  last_synced_at?: string | null
  created_at?: string | null
  updated_at?: string | null
}

export type ModelPriceCredentialPricing = {
  credential_id: string
  credential_name: string
  routing_priority: number
  group_ratio: number
  credential_enabled: boolean
  credential_usable: boolean
  model_enabled: boolean
  model_available: boolean
  billing_type?: string | null
  currency?: string | null
  input_value?: number | null
  output_value?: number | null
  audio_output_value?: number | null
  cache_input_value?: number | null
  create_cache_input_value?: number | null
  create_cache_1h_input_value?: number | null
  per_request_value?: number | null
}

export type ModelPriceItem = {
  site_model_id: string
  pricing_id?: string | null
  site: ModelPriceSite
  model: ModelPriceModel
  pricing: ModelPricePricing | null
  editable: boolean
  edit_reason?: string | null
  pricing_status: ModelPriceStatus | string
  credential_pricings?: ModelPriceCredentialPricing[]
}

export type ModelPriceListMeta = {
  count?: number
  priced_count?: number
  missing_count?: number
  manual_count?: number
}

export type ModelPriceInput = {
  group_name?: string
  billing_type: ModelPriceBillingType
  currency?: string
  input_value?: number | null
  output_value?: number | null
  audio_output_value?: number | null
  cache_input_value?: number | null
  create_cache_input_value?: number | null
  create_cache_1h_input_value?: number | null
  per_request_value?: number | null
  group_ratio?: number | null
  manual_note?: string
}

export type BulkModelPriceInput = ModelPriceInput & {
  canonical_model_id: string
  site_model_ids: string[]
}

export type ModelPriceUpdateResponse = {
  item: ModelPriceItem
  meta?: {
    created?: boolean
  }
}

export type BulkModelPriceResponse = {
  items: Array<{
    site_model_id: string
    pricing_id?: string | null
    site_id: string
    site_name: string
    pricing_status: string
  }>
  meta?: {
    count?: number
    created_count?: number
    updated_count?: number
  }
}

export const modelPriceQueryKeys = {
  all: ['settings', 'model-prices'] as const,
  list: (filters: ModelPriceFilters) =>
    [...modelPriceQueryKeys.all, filters] as const,
}

export async function listModelPrices(filters: ModelPriceFilters = {}) {
  const params = new URLSearchParams()
  if (filters.q) params.set('q', filters.q)
  if (filters.siteId) params.set('site_id', filters.siteId)
  if (filters.siteType) params.set('site_type', filters.siteType)
  if (filters.provider) params.set('provider', filters.provider)
  if (filters.modelKey) params.set('model_key', filters.modelKey)
  if (filters.canonicalModelId)
    params.set('canonical_model_id', filters.canonicalModelId)
  if (filters.billingType && filters.billingType !== 'all')
    params.set('billing_type', filters.billingType)
  if (filters.pricingStatus) params.set('pricing_status', filters.pricingStatus)

  const query = params.toString()
  return apiFetch<{ items: ModelPriceItem[]; meta?: ModelPriceListMeta }>(
    `/api/v1/model-prices${query ? `?${query}` : ''}`,
  )
}

export async function updateSiteModelPricing(
  siteModelId: string,
  input: ModelPriceInput,
) {
  return apiFetch<ModelPriceUpdateResponse>(
    `/api/v1/site-models/${siteModelId}/pricing`,
    {
      method: 'PUT',
      body: input,
    },
  )
}

export async function bulkUpdateModelPrices(input: BulkModelPriceInput) {
  return apiFetch<BulkModelPriceResponse>('/api/v1/model-prices/bulk', {
    method: 'PUT',
    body: input,
  })
}

export async function resetSiteModelPricing(siteModelId: string) {
  return apiFetch<{ item: ModelPriceItem }>(
    `/api/v1/site-models/${siteModelId}/pricing/reset`,
    {
      method: 'POST',
    },
  )
}

export async function resetSitePricing(siteId: string) {
  return apiFetch<{ items: ModelPriceItem[]; meta: { reset_count: number } }>(
    `/api/v1/sites/${siteId}/pricing/reset-all`,
    {
      method: 'POST',
    },
  )
}
