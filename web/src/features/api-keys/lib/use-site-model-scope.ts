import { useMemo } from 'react'
import { useQueries } from '@tanstack/react-query'
import {
  listSiteModels,
  sitesQueryKeys,
  type Site,
  type SiteModel,
} from '@/features/sites/api/sites'
import { siteModelItems } from '@/features/sites/lib/site-cache'
import { sortSitesByName } from '@/features/sites/lib/site-utils'
import type { SiteGroup } from '@/features/settings/api/site-groups'

export type SiteModelScopePolicy = 'allow_all' | 'allow_list'

export type SiteModelScopePatch = {
  sitePolicy?: SiteModelScopePolicy
  modelPolicy?: SiteModelScopePolicy
  siteIds?: string[]
  siteGroupIds?: string[]
  siteModelIds?: string[]
  autoSelectSiteModels?: boolean
}

export function useSiteModelScope({
  sites,
  siteGroups = [],
  sitePolicy,
  modelPolicy,
  siteIds,
  siteGroupIds = [],
  siteModelIds,
  autoSelectSiteModels = false,
  imageBridgeEnabled = false,
  advancedExpanded = false,
}: {
  sites: Site[]
  siteGroups?: SiteGroup[]
  sitePolicy: SiteModelScopePolicy
  modelPolicy: SiteModelScopePolicy
  siteIds: string[]
  siteGroupIds?: string[]
  siteModelIds: string[]
  autoSelectSiteModels?: boolean
  imageBridgeEnabled?: boolean
  advancedExpanded?: boolean
}) {
  const sortedSites = useMemo(() => sortSitesByName(sites), [sites])
  const enabledSites = useMemo(() => sortedSites.filter((site) => site.enabled), [sortedSites])
  const sitesById = useMemo(() => new Map(sortedSites.map((site) => [site.id, site])), [sortedSites])
  const inheritedSiteIds = useMemo(() => {
    if (sitePolicy !== 'allow_list') return []
    const ids = new Set<string>()
    const selectedGroups = new Set(siteGroupIds)
    for (const group of siteGroups) {
      if (!selectedGroups.has(group.id) || !group.enabled) continue
      for (const site of group.sites) {
        if (sitesById.has(site.site_id)) ids.add(site.site_id)
      }
    }
    return [...ids]
  }, [siteGroups, sitesById, siteGroupIds, sitePolicy])
  const inheritedSiteIdSet = useMemo(() => new Set(inheritedSiteIds), [inheritedSiteIds])
  const selectedDirectSiteIdSet = useMemo(() => new Set(siteIds), [siteIds])
  const effectiveSiteIds = useMemo(() => {
    if (sitePolicy !== 'allow_list') return enabledSites.map((site) => site.id)
    const ids = new Set([...inheritedSiteIds, ...siteIds])
    return sortedSites.filter((site) => ids.has(site.id)).map((site) => site.id)
  }, [enabledSites, inheritedSiteIds, siteIds, sitePolicy, sortedSites])

  const effectiveModelPolicy: SiteModelScopePolicy = sitePolicy === 'allow_list' ? 'allow_list' : modelPolicy
  const requestedSiteModelIds = useMemo(() => {
    const ids = new Set<string>()
    if (effectiveModelPolicy === 'allow_list' || advancedExpanded) {
      for (const siteID of effectiveSiteIds) ids.add(siteID)
    }
    if (imageBridgeEnabled) {
      for (const site of enabledSites) ids.add(site.id)
    }
    return [...ids]
  }, [advancedExpanded, effectiveModelPolicy, effectiveSiteIds, enabledSites, imageBridgeEnabled])
  const siteModelQueries = useQueries({
    queries: requestedSiteModelIds.map((siteID) => ({
      queryKey: sitesQueryKeys.models(siteID),
      queryFn: async () => {
        try {
          return await listSiteModels(siteID)
        } catch {
          return { items: [] as SiteModel[] }
        }
      },
    })),
  })
  const siteModelsMap = useMemo(() => {
    const result: Record<string, SiteModel[]> = {}
    requestedSiteModelIds.forEach((siteID, index) => {
      result[siteID] = siteModelItems(siteModelQueries[index]?.data)
    })
    return result
  }, [requestedSiteModelIds, siteModelQueries])
  const loadingSiteModelIds = useMemo(() => new Set(
    requestedSiteModelIds.filter((_, index) => siteModelQueries[index]?.isLoading),
  ), [requestedSiteModelIds, siteModelQueries])
  const siteModelsLoading = effectiveSiteIds.some((siteID) => loadingSiteModelIds.has(siteID))

  const siteModelRows = useMemo(() => {
    return effectiveSiteIds.flatMap((siteId) => {
      const site = sitesById.get(siteId)
      return (siteModelsMap[siteId] ?? [])
        .filter((model) => model.status === 'active')
        .map((model) => ({ site, model }))
        .filter((item): item is { site: Site; model: SiteModel } => Boolean(item.site))
    })
  }, [effectiveSiteIds, siteModelsMap, sitesById])
  const availableSiteModelIds = useMemo(
    () => new Set(siteModelRows.map(({ model }) => model.id)),
    [siteModelRows],
  )
  const allSiteModelIds = useMemo(
    () => siteModelRows.map(({ model }) => model.id),
    [siteModelRows],
  )
  const selectedSiteModelIds = useMemo(() => {
    if (effectiveModelPolicy === 'allow_list' && autoSelectSiteModels) {
      return allSiteModelIds
    }
    return siteModelIds
  }, [allSiteModelIds, autoSelectSiteModels, effectiveModelPolicy, siteModelIds])
  const validSelectedSiteModelCount = useMemo(
    () => selectedSiteModelIds.filter((id) => availableSiteModelIds.has(id)).length,
    [availableSiteModelIds, selectedSiteModelIds],
  )

  return {
    sortedSites,
    enabledSites,
    sitesById,
    inheritedSiteIds,
    inheritedSiteIdSet,
    selectedDirectSiteIdSet,
    effectiveSiteIds,
    effectiveModelPolicy,
    siteModelsMap,
    loadingSiteModelIds,
    siteModelsLoading,
    siteModelRows,
    availableSiteModelIds,
    allSiteModelIds,
    selectedSiteModelIds,
    validSelectedSiteModelCount,
  }
}


export type SiteModelScope = ReturnType<typeof useSiteModelScope>
