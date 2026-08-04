import { apiFetch } from '@/lib/http'

type SiteGroupSite = {
  id: string
  group_id: string
  site_id: string
  created_at?: string
  updated_at?: string
}

export type SiteGroup = {
  id: string
  name: string
  slug: string
  description: string
  enabled: boolean
  sort_order: number
  sites: SiteGroupSite[]
  created_at: string
  updated_at: string
}

export type SiteGroupUpsertInput = {
  name: string
  slug: string
  description?: string
  enabled: boolean
  sortOrder?: number
  siteIds: string[]
}

export const siteGroupQueryKeys = {
  all: ['site-groups'] as const,
  list: () => [...siteGroupQueryKeys.all, 'list'] as const,
  detail: (groupId: string) => [...siteGroupQueryKeys.all, 'detail', groupId] as const,
}

export async function listSiteGroups() {
  return apiFetch<{ items: SiteGroup[]; meta?: { count?: number } }>('/api/v1/settings/site-groups')
}

export async function createSiteGroup(input: SiteGroupUpsertInput) {
  return apiFetch<{ site_group: SiteGroup }>('/api/v1/settings/site-groups', {
    method: 'POST',
    body: siteGroupBody(input),
  })
}

export async function updateSiteGroup(groupId: string, input: SiteGroupUpsertInput) {
  return apiFetch<{ site_group: SiteGroup }>(`/api/v1/settings/site-groups/${groupId}`, {
    method: 'PUT',
    body: siteGroupBody(input),
  })
}

export async function deleteSiteGroup(groupId: string) {
  return apiFetch<void>(`/api/v1/settings/site-groups/${groupId}`, {
    method: 'DELETE',
  })
}

export async function updateSiteGroupMembershipForSite(groups: SiteGroup[], siteId: string, groupIds: string[]) {
  const selected = new Set(groupIds)
  await Promise.all(
    groups.map((group) => {
      const currentSiteIds = group.sites.map((site) => site.site_id)
      const currentlySelected = currentSiteIds.includes(siteId)
      const shouldSelect = selected.has(group.id)
      if (currentlySelected === shouldSelect) return Promise.resolve()

      const nextSiteIds = shouldSelect
        ? [...currentSiteIds, siteId]
        : currentSiteIds.filter((id) => id !== siteId)

      return updateSiteGroup(group.id, {
        name: group.name,
        slug: group.slug,
        description: group.description,
        enabled: group.enabled,
        sortOrder: group.sort_order,
        siteIds: nextSiteIds,
      })
    }),
  )
}

function siteGroupBody(input: SiteGroupUpsertInput) {
  return {
    name: input.name,
    slug: input.slug,
    description: input.description ?? '',
    enabled: input.enabled,
    sort_order: input.sortOrder ?? 0,
    site_ids: input.siteIds,
  }
}
