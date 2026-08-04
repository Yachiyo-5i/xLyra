import { buildModelGlyph } from '@/components/common/brand-utils'
import {
  OTHER_BRAND_LABEL,
  getProviderCatalogEntry,
  inferFallbackBrand,
  providerCatalog,
} from '@/lib/brands'
import type { CanonicalModelItem, Site, SiteModel } from '@/features/sites/api/sites'

export type ModelIconInfo = {
  iconPath?: string
  label: string
  fallback: string
  fallbackText?: string
}

export function siteModelIconInfo(
  model: SiteModel,
  canonicalMap?: ReadonlyMap<string, CanonicalModelItem>,
  site?: Site | null,
): ModelIconInfo {
  const displayName = model.display_name || model.upstream_model_name || model.id
  const canonical = model.canonical_model_id ? canonicalMap?.get(model.canonical_model_id) : undefined
  if (canonical) return canonicalModelIconInfo(canonical, displayName)

  return modelNameIconInfo([
    model.display_name,
    model.upstream_model_name,
    ...extractCapabilityStrings(model.capabilities),
    site?.name,
    site?.site_type,
    site?.base_url,
  ], displayName)
}

export function canonicalModelIconInfo(
  canonical: CanonicalModelItem,
  fallbackName = canonical.model_key,
): ModelIconInfo {
  const provider = canonical.provider?.trim() ?? ''
  const providerEntry = provider ? getProviderCatalogEntry(provider) ?? providerByName(provider) : undefined
  const fallback = fallbackName.trim() || canonical.model_key || canonical.display_name

  return {
    iconPath: canonical.icon_url ?? providerEntry?.iconPath,
    label: providerEntry?.name || provider || fallback,
    fallback,
    fallbackText: buildModelGlyph(fallback),
  }
}

export function modelNameIconInfo(name: string | Array<string | null | undefined>, fallbackName?: string): ModelIconInfo {
  const candidates = (Array.isArray(name) ? name : [name])
    .map((value) => value?.trim())
    .filter((value): value is string => Boolean(value))
  const fallback = fallbackName?.trim() || candidates[0] || '?'
  const brand = inferFallbackBrand(candidates.length ? candidates : [fallback])
  const providerEntry = brand === OTHER_BRAND_LABEL ? undefined : providerByName(brand)

  return {
    iconPath: providerEntry?.iconPath,
    label: providerEntry?.name || brand || fallback,
    fallback,
    fallbackText: buildModelGlyph(fallback),
  }
}

function providerByName(name: string) {
  const normalized = name.trim().toLowerCase()
  return providerCatalog.find((provider) => provider.name.toLowerCase() === normalized)
}

function extractCapabilityStrings(capabilities: Record<string, unknown>): string[] {
  const result: string[] = []
  for (const key of ['owned_by', 'source', 'vendor_name', 'vendor_id', 'name', 'id']) {
    const value = capabilities[key]
    if (typeof value === 'string' && value.trim()) result.push(value.trim())
  }
  return result
}
