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

export type SavedSiteAuthInput = {
  newapi?: {
    accessToken: string
    userId: number
  }
  xlyra?: {
    authMode: 'access_token' | 'api_key'
    accessToken?: string
  }
}

// The update response carries the saved site row, but not NewAPI or xLyra
// credentials. Those stay on the detail snapshot the edit form already loaded,
// unless this submit replaced them.
export function mergeSavedSiteDetail(previous: Site | undefined, saved: Site, input: SavedSiteAuthInput = {}): Site {
  const auth: NonNullable<Site['auth_config']> = {
    ...(previous?.auth_config ?? {}),
    ...(saved.auth_config ?? {}),
  }
  if (input.newapi) {
    auth.newapi = {
      access_token: input.newapi.accessToken,
      user_id: input.newapi.userId,
    }
  }
  if (input.xlyra) {
    auth.xlyra = input.xlyra.authMode === 'access_token'
      ? { auth_mode: 'access_token', access_token: input.xlyra.accessToken ?? '' }
      : { auth_mode: 'api_key' }
  }

  return {
    ...(previous ?? {}),
    ...saved,
    auth_config: Object.keys(auth).length > 0 ? auth : undefined,
  }
}

export function selectEditingSite(detail: Site | undefined, listed: Site | null): Site | null {
  if (!listed) return detail ?? null
  if (!detail) return listed
  if (siteUpdatedAt(detail) >= siteUpdatedAt(listed)) return detail
  return mergeSavedSiteDetail(detail, listed)
}

export function editSiteFormResetKey(site: Site, groupIds: string[]) {
  return [
    site.id,
    site.updated_at,
    site.auth_config?.newapi?.access_token ?? '',
    String(site.auth_config?.newapi?.user_id ?? ''),
    site.auth_config?.xlyra?.auth_mode ?? '',
    site.auth_config?.xlyra?.access_token ?? '',
    [...groupIds].sort().join(','),
  ].join('\n')
}

function siteUpdatedAt(site: Site) {
  const time = Date.parse(site.updated_at)
  return Number.isFinite(time) ? time : 0
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
