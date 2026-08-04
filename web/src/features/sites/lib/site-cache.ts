import type { Site, SiteAPIKey, SiteModel } from '@/features/sites/api/sites'
import { sortSitesForDisplay } from '@/features/sites/lib/site-utils'

export function mergeListSite(current: { items: Site[]; meta: { count: number } } | undefined, site: Site) {
  if (!isValidSite(site)) return current ?? { items: [], meta: { count: 0 } }
  if (!current) return { items: [site], meta: { count: 1 } }
  const currentItems = current.items.filter(isValidSite)
  const exists = currentItems.some((item) => item.id === site.id)
  const next = exists ? currentItems.map((item) => (item.id === site.id ? site : item)) : [...currentItems, site]
  return { items: sortSitesForDisplay(next), meta: { count: next.length } }
}

export function removeListSite(current: { items: Site[]; meta: { count: number } } | undefined, siteId: string) {
  if (!current) return current
  const items = current.items.filter((site) => isValidSite(site) && site.id !== siteId)
  return { items, meta: { count: items.length } }
}

export function sanitizeSiteList(items: Site[]) {
  return items.filter(isValidSite)
}

export function siteModelItems(current: { items?: SiteModel[] } | undefined) {
  return current?.items ?? []
}

function isValidSite(site: Site | null | undefined): site is Site {
  return Boolean(
    site &&
    typeof site.id === 'string' &&
    site.id.trim() &&
    site.id !== '00000000-0000-0000-0000-000000000000' &&
    typeof site.name === 'string' &&
    site.name.trim() &&
    typeof site.site_type === 'string' &&
    site.site_type.trim() &&
    typeof site.updated_at === 'string' &&
    site.updated_at.trim() &&
    !site.updated_at.startsWith('0001-01-01'),
  )
}

export function replaceAPIKey(current: { items: SiteAPIKey[] } | undefined, apiKey: SiteAPIKey) {
  return { ...(current ?? {}), items: (current?.items ?? []).map((item) => (item.id === apiKey.id ? apiKey : item)) }
}

export function upsertAPIKey(current: { items: SiteAPIKey[] } | undefined, apiKey: SiteAPIKey) {
  const items = current?.items ?? []
  const exists = items.some((item) => item.id === apiKey.id)
  return { ...(current ?? {}), items: exists ? items.map((item) => (item.id === apiKey.id ? apiKey : item)) : [...items, apiKey] }
}

export function removeAPIKey(current: { items: SiteAPIKey[]; meta?: { count?: number } } | undefined, apiKeyId: string) {
  if (!current) return current
  const items = current.items.filter((item) => item.id !== apiKeyId)
  return { ...current, items, meta: current.meta ? { ...current.meta, count: items.length } : current.meta }
}
