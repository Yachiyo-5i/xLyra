import { siteTypeIconPath } from '@/components/common/brand-utils'
import { BRAND_ORDER, OTHER_BRAND_KEY, OTHER_BRAND_LABEL, brandOrderIndex, getProviderCatalogEntry, inferFallbackBrand, providerCatalog } from '@/lib/brands'
import type { RouteCooldown, RouteOverviewItem } from '@/features/routes/api/routes'
import type { CanonicalModelItem, Site, SiteModel } from '@/features/sites/api/sites'
import { buildSiteGlyph } from '@/features/routes/lib/route-format'
import type { FilterItem, SortMode } from '@/features/routes/lib/types'

export function routeOverviewSearchText(
  item: RouteOverviewItem,
  sites: Site[],
  modelsMap: Record<string, SiteModel[]>,
  canonical?: CanonicalModelItem,
) {
  return [
    item.canonical_model.model_key,
    item.canonical_model.display_name,
    canonical?.model_key,
    canonical?.display_name,
    item.last_route.site_name,
    routeBrandLabel(item, canonical),
    ...routeSupportedSiteNames(item, sites, modelsMap),
  ].filter(Boolean).join(' ').toLowerCase()
}

export function buildBrandFilterItems(items: RouteOverviewItem[], canonicalMap: Map<string, CanonicalModelItem>, t?: (key: string) => string): FilterItem[] {
  const counts = new Map<string, number>()
  for (const item of items) {
    const key = routeBrandKey(item, canonicalMap.get(item.canonical_model.id))
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }

  const result: FilterItem[] = [
    { key: 'all', label: t ? t('filters.all') : '全部', count: items.length, fallbackText: '✦' },
  ]

  for (const brand of BRAND_ORDER) {
    const count = counts.get(brand) ?? 0
    if (!count) continue
    const provider = providerCatalog.find((p) => p.name === brand)
    const sampleItem = items.find((item) => routeBrandKey(item, canonicalMap.get(item.canonical_model.id)) === brand)
    const canonical = sampleItem ? canonicalMap.get(sampleItem.canonical_model.id) : undefined
    result.push({
      key: brand,
      label: brand,
      count,
      iconPath: canonical?.icon_url ?? provider?.iconPath,
      fallbackText: brand.slice(0, 1),
    })
  }

  const otherCount = counts.get(OTHER_BRAND_KEY) ?? 0
  if (otherCount) {
    result.push({
      key: OTHER_BRAND_KEY,
      label: OTHER_BRAND_LABEL,
      count: otherCount,
      fallbackText: '?',
    })
  }

  return result
}

export function buildSiteFilterItems(
  sites: Site[],
  items: RouteOverviewItem[],
  modelsMap: Record<string, SiteModel[]>,
): FilterItem[] {
  return sites
    .map((site) => ({
      key: site.id,
      label: site.name,
      count: items.filter((item) => routeSupportsSite(item, site.id, modelsMap)).length,
      iconPath: siteTypeIconPath(site.site_type) ?? site.icon_url,
      fallbackText: buildSiteGlyph(site.name),
    }))
    .sort((a, b) => a.label.localeCompare(b.label, 'zh-CN', { numeric: true, sensitivity: 'base' }))
}

export function compareRouteOverviewItems(
  a: RouteOverviewItem,
  b: RouteOverviewItem,
  sortMode: SortMode,
  canonicalMap: Map<string, CanonicalModelItem>,
) {
  if (sortMode === 'success_asc' || sortMode === 'success_desc') {
    const diff = successRate(a) - successRate(b)
    if (diff !== 0) return sortMode === 'success_asc' ? diff : -diff
  }

  if (sortMode === 'candidates_asc' || sortMode === 'candidates_desc') {
    const diff = candidateCount(a) - candidateCount(b)
    if (diff !== 0) return sortMode === 'candidates_asc' ? diff : -diff
  }

  const aCanonical = canonicalMap.get(a.canonical_model.id)
  const bCanonical = canonicalMap.get(b.canonical_model.id)
  const brandDiff = brandOrderIndex(routeBrandKey(a, aCanonical)) - brandOrderIndex(routeBrandKey(b, bCanonical))
  if (brandDiff !== 0) return brandDiff

  const brandNameDiff = routeBrandLabel(a, aCanonical).localeCompare(routeBrandLabel(b, bCanonical), 'zh-CN')
  if (brandNameDiff !== 0) return brandNameDiff

  return routeModelName(a).localeCompare(routeModelName(b), 'zh-CN')
}

function routeBrandLabel(item: RouteOverviewItem, canonical?: CanonicalModelItem) {
  const provider = item.canonical_model.provider?.trim() || canonical?.provider?.trim() || ''
  const entry = provider ? getProviderCatalogEntry(provider) : undefined
  if (entry) return entry.name
  const byName = provider ? providerCatalog.find((p) => p.name === provider) : undefined
  if (byName) return byName.name

  return inferFallbackBrand([
    item.canonical_model.model_key,
    item.canonical_model.display_name,
    canonical?.model_key,
    canonical?.display_name,
    ...(canonical?.aliases.map((alias) => alias.alias) ?? []),
  ].filter((value): value is string => Boolean(value)))
}

export function routeBrandEntry(item: RouteOverviewItem, canonical?: CanonicalModelItem) {
  const provider = item.canonical_model.provider?.trim() || canonical?.provider?.trim() || ''
  const entry = provider ? getProviderCatalogEntry(provider) : undefined
  if (entry) return entry
  const label = routeBrandLabel(item, canonical)
  return providerCatalog.find((p) => p.name === label)
}

export function routeBrandKey(item: RouteOverviewItem, canonical?: CanonicalModelItem) {
  const label = routeBrandLabel(item, canonical)
  return BRAND_ORDER.includes(label as (typeof BRAND_ORDER)[number]) ? label : OTHER_BRAND_KEY
}

function routeModelName(item: RouteOverviewItem) {
  return item.canonical_model.model_key
}

export function routeSupportsSite(
  item: RouteOverviewItem,
  siteId: string,
  modelsMap: Record<string, SiteModel[]>,
) {
  return (modelsMap[siteId] ?? []).some((model) => model.canonical_model_id === item.canonical_model.id)
}

export function filterCooldownsForCanonicalModel(
  cooldowns: RouteCooldown[],
  canonicalModelId: string,
  modelsMap: Record<string, SiteModel[]>,
) {
  if (!canonicalModelId) return []

  const siteModelIdsBySite = new Map<string, Set<string>>()
  for (const [siteId, models] of Object.entries(modelsMap)) {
    const matched = models
      .filter((model) => model.canonical_model_id === canonicalModelId)
      .map((model) => model.id)
    if (matched.length) {
      siteModelIdsBySite.set(siteId, new Set(matched))
    }
  }

  return cooldowns.filter((cooldown) => {
    const modelsAtSite = siteModelIdsBySite.get(cooldown.site_id)
    if (!modelsAtSite) return false
    if (cooldown.site_model_id) {
      return modelsAtSite.has(cooldown.site_model_id)
    }
    return true
  })
}

function routeSupportedSiteNames(
  item: RouteOverviewItem,
  sites: Site[],
  modelsMap: Record<string, SiteModel[]>,
) {
  const siteMap = new Map(sites.map((site) => [site.id, site.name]))

  return Object.entries(modelsMap)
    .filter(([, models]) => models.some((model) => model.canonical_model_id === item.canonical_model.id))
    .map(([siteId]) => siteMap.get(siteId))
    .filter(Boolean)
}

function successRate(item: RouteOverviewItem) {
  return item.traffic_24h.success_rate ?? -1
}

function candidateCount(item: RouteOverviewItem) {
  return item.candidate_summary.eligible_count
}
