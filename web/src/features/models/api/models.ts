import {
  listOAuthConnections,
  oauthQueryKeys,
  type OAuthConnectionListItem,
} from '@/features/oauth/api/oauth'
import {
  archiveCanonicalModel,
  bindSiteModelCanonical,
  createCanonicalModel,
  createCanonicalModelAlias,
  deleteCanonicalModelAlias,
  getCanonicalModelMatrix,
  listAllSitePricings,
  listCanonicalModels,
  listSiteAPIKeys,
  listSiteModels,
  listSitesWithOAuth,
  sitesQueryKeys,
  type CanonicalModelItem,
  type CanonicalModelMatrixRow,
  type Site,
  type SiteAPIKey,
  type SiteModel,
  type SiteModelPricing,
} from '@/features/sites/api/sites'
import {
  BRAND_ORDER,
  OTHER_BRAND_KEY,
  OTHER_BRAND_LABEL,
  brandGroupKey,
  brandOrderIndex,
  getProviderCatalogEntry,
  inferFallbackBrand,
} from '@/lib/brands'
import { siteTypeIconPath } from '@/components/common/brand-utils'

export const modelsQueryKeys = {
  all: ['models'] as const,
  marketplace: (siteIds: string[]) =>
    [...sitesQueryKeys.all, 'models-marketplace', siteIds.join(',')] as const,
  pricings: () => [...sitesQueryKeys.all, 'all-pricings'] as const,
  canonical: () => [...sitesQueryKeys.all, 'canonical-models'] as const,
  canonicalMatrix: (modelId: string) =>
    [...modelsQueryKeys.canonical(), modelId, 'matrix'] as const,
} as const

export type MarketplacePricingRow = {
  id: string
  modelName: string
  groupName: string
  keyGroupStatus?: MarketplaceKeyGroupStatus
  billingType: string
  currency: string
  groupRatio?: number | null
  inputValue?: number | null
  outputValue?: number | null
  audioOutputValue?: number | null
  perRequestValue?: number | null
  cacheInputValue?: number | null
  createCacheInputValue?: number | null
  createCache1hInputValue?: number | null
  available: boolean
  apiKeyId?: string
  apiKeyName?: string
  apiKeyEnabled?: boolean
  apiKeyUsable?: boolean
  apiKeyModelEnabled?: boolean
}

export type MarketplaceKeyGroupStatus = {
  enabled: number
  total: number
  source?: 'api_keys' | 'codex_accounts'
}

type MarketplaceModelSite = {
  siteId: string
  siteName: string
  siteType: string
  modelId: string
  canonicalModelId?: string | null
  displayName: string
  upstreamName: string
  enabled: boolean
  supportedEndpointTypes: string[]
  supportsMultipleAPIKeys: boolean
  pricingRows: MarketplacePricingRow[]
}

export type MarketplaceModel = {
  id: string
  canonicalModelId?: string | null
  model_key: string
  name: string
  providerId: string
  brand: string
  iconPath?: string
  supportedSites: MarketplaceModelSite[]
}

export type FilterItem = {
  key: string
  label: string
  count: number
  iconPath?: string
  fallbackText?: string
}

export type UnmatchedSiteModel = {
  siteId: string
  siteName: string
  model: SiteModel
}

export type CreateCanonicalModelDraft = {
  modelKey: string
  displayName: string
  target?: UnmatchedSiteModel
}

// ═══════════════════════════════════════════════════════════════
// API re-exports (so Models feature has its own API layer)
// ═══════════════════════════════════════════════════════════════

export {
  listSitesWithOAuth,
  listSiteModels,
  listSiteAPIKeys,
  listAllSitePricings,
  listCanonicalModels,
  getCanonicalModelMatrix,
  createCanonicalModel,
  createCanonicalModelAlias,
  deleteCanonicalModelAlias,
  archiveCanonicalModel,
  bindSiteModelCanonical,
  listOAuthConnections,
  sitesQueryKeys,
  oauthQueryKeys,
}

export type {
  Site,
  SiteModel,
  SiteAPIKey,
  SiteModelPricing,
  CanonicalModelItem,
  CanonicalModelMatrixRow,
  OAuthConnectionListItem,
}

// ═══════════════════════════════════════════════════════════════
// Data builders
// ═══════════════════════════════════════════════════════════════

export function buildMarketplaceModels(
  sites: Site[],
  modelsMap: Record<string, SiteModel[]>,
  pricingItems: SiteModelPricing[],
  canonicalModels: CanonicalModelItem[],
  apiKeysMap: Record<string, SiteAPIKey[]>,
  codexAccountStatus: MarketplaceKeyGroupStatus,
): MarketplaceModel[] {
  const siteMap = new Map(sites.map((site) => [site.id, site]))
  const canonicalMap = new Map(
    canonicalModels.map((model) => [model.id, model]),
  )
  const grouped = new Map<string, MarketplaceModel>()

  for (const [siteId, models] of Object.entries(modelsMap)) {
    const site = siteMap.get(siteId)
    if (!site) continue

    for (const model of models) {
      const canonical = model.canonical_model_id
        ? canonicalMap.get(model.canonical_model_id)
        : undefined
      const canonicalName = canonical
        ? `canonical:${canonical.id}`
        : normalizeModelName(model.display_name || model.upstream_model_name)
      const canonicalProvider = canonical
        ? getProviderCatalogEntry(canonical.provider)
        : undefined
      const brand = canonical
        ? (canonicalProvider?.name ?? canonical.provider)
        : inferFallbackBrand([
            model.display_name,
            model.upstream_model_name,
            ...extractCapabilityStrings(model.capabilities),
            site.name,
            site.base_url,
          ])
      const group = grouped.get(canonicalName) ?? {
        id: canonicalName,
        canonicalModelId: canonical?.id,
        model_key:
          canonical?.model_key ||
          model.upstream_model_name ||
          model.display_name,
        name:
          canonical?.display_name ||
          model.display_name ||
          model.upstream_model_name,
        providerId: canonicalProvider?.id ?? 'other',
        brand,
        iconPath: canonical?.icon_url ?? canonicalProvider?.iconPath,
        supportedSites: [],
      }

      group.supportedSites.push({
        siteId: site.id,
        siteName: site.name,
        siteType: site.site_type,
        modelId: model.id,
        canonicalModelId: model.canonical_model_id,
        displayName: model.display_name,
        upstreamName: model.upstream_model_name,
        enabled: model.status === 'active',
        supportedEndpointTypes: normalizeEndpointTypes(
          model.capabilities?.supported_endpoint_types,
        ),
        supportsMultipleAPIKeys: site.supports_multiple_api_keys === true,
        pricingRows: [],
      })

      grouped.set(canonicalName, group)
    }
  }

  for (const model of grouped.values()) {
    model.supportedSites = model.supportedSites.map((site) => {
      const pricingRows = pricingItems
        .filter((pricing) => pricing.site_id === site.siteId)
        .filter((pricing) => {
          if (pricing.site_model_id && pricing.site_model_id === site.modelId)
            return true

          const pricingName = normalizeModelName(pricing.model_name)
          return (
            pricingName === normalizeModelName(site.displayName) ||
            pricingName === normalizeModelName(site.upstreamName)
          )
        })
        .flatMap((pricing) =>
          marketplacePricingRows(site, pricing, apiKeysMap, codexAccountStatus),
        )
      return {
        ...site,
        pricingRows: pricingRows.length
          ? pricingRows
          : marketplaceUnpricedCredentialRows(site, apiKeysMap),
      }
    })
  }

  return [...grouped.values()].sort(compareMarketplaceModel)
}

export function marketplaceUnpricedCredentialRows(
  site: MarketplaceModelSite,
  apiKeysMap: Record<string, SiteAPIKey[]>,
): MarketplacePricingRow[] {
  if (!site.supportsMultipleAPIKeys) return []
  const modelNames = new Set(
    [site.upstreamName, site.displayName]
      .map(normalizeModelName)
      .filter(Boolean),
  )
  return (apiKeysMap[site.siteId] ?? [])
    .filter((apiKey) =>
      apiKey.models.some((name) => modelNames.has(normalizeModelName(name))),
    )
    .map((apiKey) => {
      const modelItem = apiKey.model_items?.find((item) =>
        modelNames.has(normalizeModelName(item.name)),
      )
      return {
        id: `${site.siteId}:${site.modelId}:${apiKey.id}:missing`,
        modelName: site.upstreamName,
        groupName: 'default',
        billingType: '',
        currency: '',
        groupRatio: apiKey.upstream_cost_multiplier ?? 1,
        available: false,
        apiKeyId: apiKey.id,
        apiKeyName: apiKey.name,
        apiKeyEnabled: apiKey.enabled,
        apiKeyUsable: apiKey.enabled,
        apiKeyModelEnabled: modelItem?.enabled ?? true,
      }
    })
}

export function marketplacePricingRows(
  site: MarketplaceModelSite,
  pricing: SiteModelPricing,
  apiKeysMap: Record<string, SiteAPIKey[]>,
  codexAccountStatus: MarketplaceKeyGroupStatus,
): MarketplacePricingRow[] {
  if (site.supportsMultipleAPIKeys && pricing.credential_pricings?.length) {
    return pricing.credential_pricings.map((credential) => ({
      id: `${pricing.id}:${credential.credential_id}`,
      modelName: pricing.model_name,
      groupName: pricing.group_name,
      billingType: credential.billing_type ?? pricing.billing_type,
      currency: credential.currency ?? pricing.currency,
      groupRatio: credential.group_ratio,
      inputValue: credential.input_value,
      outputValue: credential.output_value,
      audioOutputValue: credential.audio_output_value,
      perRequestValue: credential.per_request_value,
      cacheInputValue: credential.cache_input_value,
      createCacheInputValue: credential.create_cache_input_value,
      createCache1hInputValue: credential.create_cache_1h_input_value,
      available: pricing.available && credential.model_available,
      apiKeyId: credential.credential_id,
      apiKeyName: credential.credential_name,
      apiKeyEnabled: credential.credential_enabled,
      apiKeyUsable: credential.credential_usable,
      apiKeyModelEnabled: credential.model_enabled,
    }))
  }
  return [
    {
      id: pricing.id,
      modelName: pricing.model_name,
      groupName: pricing.group_name,
      keyGroupStatus: buildMarketplaceKeyStatus(
        site,
        pricing.group_name,
        apiKeysMap,
        codexAccountStatus,
      ),
      billingType: pricing.billing_type,
      currency: pricing.currency,
      groupRatio: pricing.group_ratio,
      inputValue: pricing.input_value,
      outputValue: pricing.output_value,
      audioOutputValue: pricing.audio_output_value,
      perRequestValue: pricing.per_request_value,
      cacheInputValue: pricing.cache_input_value,
      createCacheInputValue: pricing.create_cache_input_value,
      createCache1hInputValue: pricing.create_cache_1h_input_value,
      available: pricing.available,
    },
  ]
}

type TFunction = (key: string, options?: Record<string, unknown>) => string

export function buildBrandItems(
  models: MarketplaceModel[],
  t?: TFunction,
): FilterItem[] {
  const map = new Map<string, FilterItem>()

  for (const model of models) {
    const key = brandGroupKey(model.brand)
    const label = key === OTHER_BRAND_KEY ? OTHER_BRAND_LABEL : model.brand
    const item = map.get(key) ?? {
      key,
      label,
      count: 0,
      iconPath: key === OTHER_BRAND_KEY ? undefined : model.iconPath,
      fallbackText: key === OTHER_BRAND_KEY ? '?' : undefined,
    }

    item.count += 1
    if (!item.iconPath && key !== OTHER_BRAND_KEY && model.iconPath) {
      item.iconPath = model.iconPath
    }

    map.set(key, item)
  }

  const items = [
    ...BRAND_ORDER.map((brand) => map.get(brand)).filter(Boolean),
    ...(map.has(OTHER_BRAND_KEY) ? [map.get(OTHER_BRAND_KEY)] : []),
  ] as FilterItem[]

  return [
    {
      key: 'all',
      label: t ? t('brands.all', { ns: 'common' }) : 'All',
      count: models.length,
      fallbackText: '☑️',
    },
    ...items,
  ]
}

export function buildSiteItems(
  sites: Site[],
  modelsMap: Record<string, SiteModel[]>,
): FilterItem[] {
  return sites
    .map((site) => ({
      key: site.id,
      label: site.name,
      count: modelsMap[site.id]?.length ?? 0,
      iconPath: siteTypeIconPath(site.site_type) ?? site.icon_url,
    }))
    .sort((a, b) => a.label.localeCompare(b.label))
}

export function modelSearchText(model: MarketplaceModel): string {
  return [
    model.model_key,
    model.name,
    model.brand,
    ...model.supportedSites.flatMap((site) => [
      site.siteName,
      site.displayName,
      site.upstreamName,
    ]),
  ]
    .join(' ')
    .toLowerCase()
}

export function canonicalModelSearchText(model: CanonicalModelItem): string {
  return [
    model.display_name,
    model.model_key,
    model.provider,
    model.category,
    ...model.aliases.map((alias) => alias.alias),
  ]
    .join(' ')
    .toLowerCase()
}

export function isManualCreatedCanonicalModel(
  model: CanonicalModelItem,
): boolean {
  return (
    model.capabilities?.manual_created === true ||
    model.capabilities?.source === 'manual_create'
  )
}

export function buildUnmatchedSiteModels(
  sites: Site[],
  modelsMap: Record<string, SiteModel[]>,
  search: string,
): UnmatchedSiteModel[] {
  const keyword = search.trim().toLowerCase()
  const siteMap = new Map(sites.map((site) => [site.id, site]))
  const items: UnmatchedSiteModel[] = []

  for (const [siteId, models] of Object.entries(modelsMap)) {
    const site = siteMap.get(siteId)
    if (!site) continue

    for (const model of models) {
      if (!isUnmatchedSiteModel(model)) continue

      const item = {
        siteId,
        siteName: site.name,
        model,
      }
      const text = [
        item.siteName,
        model.display_name,
        model.upstream_model_name,
      ]
        .join(' ')
        .toLowerCase()

      if (!keyword || text.includes(keyword)) {
        items.push(item)
      }
    }
  }

  return items.sort((a, b) => {
    const siteDiff = a.siteName.localeCompare(b.siteName, 'zh-CN')
    if (siteDiff !== 0) return siteDiff

    return (a.model.display_name || a.model.upstream_model_name).localeCompare(
      b.model.display_name || b.model.upstream_model_name,
      'zh-CN',
    )
  })
}

function isUnmatchedSiteModel(model: SiteModel): boolean {
  return (
    !model.canonical_model_id || model.canonical_match_source === 'unmatched'
  )
}

export function replaceSiteModelInMap(
  current: Record<string, SiteModel[]> | undefined,
  model: SiteModel,
): Record<string, SiteModel[]> | undefined {
  if (!current) return current

  return {
    ...current,
    [model.site_id]: (current[model.site_id] ?? []).map((item) =>
      item.id === model.id ? model : item,
    ),
  }
}

export function buildCodexAccountStatus(
  connections: OAuthConnectionListItem[],
): MarketplaceKeyGroupStatus {
  const codexConnections = connections.filter(
    (connection) => connection.provider.trim().toLowerCase() === 'codex',
  )

  return {
    enabled: codexConnections.filter(isUsableCodexConnection).length,
    total: codexConnections.length,
    source: 'codex_accounts',
  }
}

function isUsableCodexConnection(connection: OAuthConnectionListItem): boolean {
  return connection.status === 'connected' && connection.site?.enabled !== false
}

// ═══════════════════════════════════════════════════════════════
// Pricing formatters
// ═══════════════════════════════════════════════════════════════

export function formatPricingValue(
  value: number | null | undefined,
  billingType?: string | null,
  currency?: string | null,
  t?: TFunction,
): string {
  if (value === null || value === undefined) {
    return '--'
  }

  if (isPerRequestBilling(billingType)) {
    const price = `${currencySymbol(currency)}${formatDecimal(value)}`
    return t ? t('card.perRequestUnit', { price }) : `${price}/req`
  }

  return `${currencySymbol(currency)}${formatDecimal(value)}/1M tokens`
}

export function formatGroupRatio(value: number | null | undefined): string {
  if (value === null || value === undefined) {
    return '--'
  }

  return `x${formatDecimal(value)}`
}

// ═══════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════

function compareMarketplaceModel(
  a: MarketplaceModel,
  b: MarketplaceModel,
): number {
  const brandOrderDiff = brandOrderIndex(a.brand) - brandOrderIndex(b.brand)
  if (brandOrderDiff !== 0) {
    return brandOrderDiff
  }

  const brandDiff = a.brand.localeCompare(b.brand, 'zh-CN')
  if (brandDiff !== 0) {
    return brandDiff
  }

  return a.name.localeCompare(b.name, 'zh-CN')
}

function buildKeyGroupStatus(
  apiKeys: SiteAPIKey[],
  groupName: string,
): MarketplaceKeyGroupStatus | undefined {
  const normalizedGroup = normalizeGroupName(groupName)
  if (!normalizedGroup) return undefined

  const keys = apiKeys.filter(
    (apiKey) => normalizeGroupName(apiKey.group ?? '') === normalizedGroup,
  )

  return {
    enabled: keys.filter((apiKey) => apiKey.enabled).length,
    total: keys.length,
    source: 'api_keys',
  }
}

function buildMarketplaceKeyStatus(
  site: MarketplaceModelSite,
  groupName: string,
  apiKeysMap: Record<string, SiteAPIKey[]>,
  codexAccountStatus: MarketplaceKeyGroupStatus,
): MarketplaceKeyGroupStatus | undefined {
  if (isCodexSiteType(site.siteType)) {
    return codexAccountStatus
  }
  if (isNewAPISiteType(site.siteType)) {
    return buildKeyGroupStatus(apiKeysMap[site.siteId] ?? [], groupName)
  }
  return undefined
}

function normalizeEndpointTypes(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value
    .map((item) => (typeof item === 'string' ? item.trim() : ''))
    .filter(Boolean)
}

function normalizeGroupName(value: string): string {
  return value.trim().toLowerCase()
}

function normalizeModelName(value: string): string {
  return value.trim().toLowerCase()
}

function isCodexSiteType(siteType: string): boolean {
  return siteType.trim().toLowerCase() === 'codex'
}

function isNewAPISiteType(siteType: string): boolean {
  return siteType.trim().toLowerCase() === 'newapi'
}

function isPerRequestBilling(value?: string | null): boolean {
  const normalized = value?.trim().toLowerCase()
  return normalized === 'per_request' || normalized === 'fixed'
}

function currencySymbol(currency?: string | null): string {
  switch ((currency || '').toUpperCase()) {
    case 'USD':
      return '$'
    case 'CNY':
      return '¥'
    default:
      return currency ? `${currency} ` : '$'
  }
}

function formatDecimal(value: number): string {
  return new Intl.NumberFormat('zh-CN', {
    minimumFractionDigits: value < 1 ? 2 : 0,
    maximumFractionDigits: 6,
  }).format(value)
}

function extractCapabilityStrings(
  capabilities: Record<string, unknown>,
): string[] {
  const result: string[] = []
  const keys = ['owned_by', 'source', 'vendor_name', 'vendor_id', 'name', 'id']
  for (const key of keys) {
    const val = capabilities[key]
    if (typeof val === 'string' && val.trim()) {
      result.push(val.trim())
    }
  }
  return result
}
