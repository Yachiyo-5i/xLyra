import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LoaderCircle, RotateCcw, Search } from 'lucide-react'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { PageHeader } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { toast } from '@/lib/toast'
import {
  listCanonicalModels,
  listSiteAPIKeys,
  listSiteModels,
  listSites,
  sitesQueryKeys,
  type CanonicalModelItem,
  type Site,
  type SiteAPIKey,
  type SiteModel,
  updateSiteAPIKeyModelStatus,
  updateSiteModelStatus,
} from '@/features/sites/api/sites'
import { sortSitesByName } from '@/features/sites/lib/site-utils'
import {
  clearRouteCooldown,
  listRouteCandidates,
  listRouteCooldowns,
  listRoutes,
  listRouteTraces,
  routeQueryKeys,
  selectRoute,
  type RouteCooldown,
  type RouteOverviewItem,
} from '@/features/routes/api/routes'
import { FilterColumn, SortColumn } from '@/features/routes/components/route-filter-sidebar'
import { MobileRoutesFilterBar } from '@/features/routes/components/mobile-routes-filter-bar'
import { MobileRoutesList } from '@/features/routes/components/mobile-routes-list'
import { RouteModelCard } from '@/features/routes/components/route-model-card'
import { RouteModelDetails } from '@/features/routes/components/route-model-details'
import { RoutesWorkspaceSkeleton } from '@/features/routes/components/routes-workspace-skeleton'
import { isNewAPISite } from '@/features/routes/lib/route-format'
import {
  buildBrandFilterItems,
  buildSiteFilterItems,
  compareRouteOverviewItems,
  filterCooldownsForCanonicalModel,
  routeBrandKey,
  routeOverviewSearchText,
  routeSupportsSite,
} from '@/features/routes/lib/route-filters'
import type { PendingChannelState, RouteChannelRow, SortMode } from '@/features/routes/lib/types'
import { useMobileLayout } from '@/hooks/use-media-query'

const EMPTY_OVERVIEW: RouteOverviewItem[] = []
const EMPTY_COOLDOWNS: RouteCooldown[] = []
const EMPTY_SITES: Site[] = []
const EMPTY_MODELS_MAP: Record<string, SiteModel[]> = {}
const EMPTY_API_KEYS_MAP: Record<string, SiteAPIKey[]> = {}
const EMPTY_CANONICAL_MODELS: CanonicalModelItem[] = []

function routeURLFilters(params: URLSearchParams) {
  return {
    modelKey: params.get('model_key')?.trim() ?? '',
    siteId: params.get('site_id')?.trim() ?? '',
  }
}

export function RoutesWorkspace({ initialSearch = '' }: { initialSearch?: string }) {
  const { t } = useTranslation('routes')
  const queryClient = useQueryClient()
  const isMobile = useMobileLayout()
  const searchParams = useMemo(() => new URLSearchParams(initialSearch), [initialSearch])
  const initialURLFilters = routeURLFilters(searchParams)
  const [search, setSearch] = useState(initialURLFilters.modelKey)
  const [selectedBrand, setSelectedBrand] = useState('all')
  const [selectedSite, setSelectedSite] = useState<string | null>(initialURLFilters.siteId || null)
  const [sortMode, setSortMode] = useState<SortMode>('default')
  const [expandedModelKey, setExpandedModelKey] = useState<string | null>(initialURLFilters.modelKey || null)

  const overviewQuery = useQuery({
    queryKey: routeQueryKeys.overview({ windowHours: 24 }),
    queryFn: () => listRoutes(),
  })
  const sitesQuery = useQuery({
    queryKey: sitesQueryKeys.list('all'),
    queryFn: () => listSites({ oauth: 'all' }),
  })
  const cooldownsQuery = useQuery({
    queryKey: routeQueryKeys.cooldowns(),
    queryFn: listRouteCooldowns,
  })
  const canonicalModelsQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'canonical-models'],
    queryFn: listCanonicalModels,
  })

  const sites = useMemo(() => sortSitesByName(sitesQuery.data?.items ?? EMPTY_SITES), [sitesQuery.data?.items])
  const siteIds = sites.map((site) => site.id).join(',')
  const modelsMapQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'routes-models', siteIds],
    queryFn: async () => {
      const entries = await Promise.all(
        sites.map(async (site) => {
          const result = await listSiteModels(site.id)
          return [site.id, result.items] as const
        }),
      )

      return Object.fromEntries(entries) as Record<string, SiteModel[]>
    },
    enabled: sites.length > 0,
  })
  const apiKeysMapQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'routes-api-keys', siteIds],
    queryFn: async () => {
      const entries = await Promise.all(
        sites.map(async (site) => {
          if (!isNewAPISite(site.site_type)) return [site.id, []] as const
          const result = await listSiteAPIKeys(site.id)
          return [site.id, result.items] as const
        }),
      )

      return Object.fromEntries(entries) as Record<string, SiteAPIKey[]>
    },
    enabled: sites.length > 0,
  })

  const overviewItems = overviewQuery.data?.items ?? EMPTY_OVERVIEW
  const modelsMap = modelsMapQuery.data ?? EMPTY_MODELS_MAP
  const apiKeysMap = apiKeysMapQuery.data ?? EMPTY_API_KEYS_MAP
  const canonicalModels = canonicalModelsQuery.data?.items ?? EMPTY_CANONICAL_MODELS
  const canonicalMap = useMemo(
    () => new Map(canonicalModels.map((model) => [model.id, model])),
    [canonicalModels],
  )
  const brandItems = useMemo(() => buildBrandFilterItems(overviewItems, canonicalMap, t), [canonicalMap, overviewItems, t])
  const siteItems = useMemo(() => buildSiteFilterItems(sites, overviewItems, modelsMap), [modelsMap, overviewItems, sites])
  const filteredOverviewItems = useMemo(() => {
    const keyword = search.trim().toLowerCase()

    return overviewItems
      .filter((item) => {
        const canonical = canonicalMap.get(item.canonical_model.id)
        const matchesSearch = !keyword || routeOverviewSearchText(item, sites, modelsMap, canonical).includes(keyword)
        const matchesBrand = selectedBrand === 'all' || routeBrandKey(item, canonical) === selectedBrand
        const matchesSite = !selectedSite || routeSupportsSite(item, selectedSite, modelsMap)

        return matchesSearch && matchesBrand && matchesSite
      })
      .toSorted((a, b) => compareRouteOverviewItems(a, b, sortMode, canonicalMap))
  }, [canonicalMap, modelsMap, overviewItems, search, selectedBrand, selectedSite, sites, sortMode])
  const activeModelKey = expandedModelKey ?? ''

  const selectionQuery = useQuery({
    queryKey: routeQueryKeys.selection({
      modelKey: activeModelKey,
      debug: true,
      failoverLimit: 3,
    }),
    queryFn: () => selectRoute({
      modelKey: activeModelKey,
      debug: true,
      failoverLimit: 3,
    }),
    enabled: Boolean(activeModelKey),
  })
  const candidatesQuery = useQuery({
    queryKey: routeQueryKeys.candidates({
      modelKey: activeModelKey,
      debug: false,
    }),
    queryFn: () => listRouteCandidates(activeModelKey),
    enabled: Boolean(activeModelKey),
  })
  const tracesQuery = useQuery({
    queryKey: routeQueryKeys.traces({
      limit: 20,
      modelKey: activeModelKey || null,
    }),
    queryFn: () => listRouteTraces({
      limit: 20,
      modelKey: activeModelKey || null,
    }),
    enabled: Boolean(activeModelKey),
  })

  const updateAPIKeyModelMutation = useMutation({
    mutationFn: ({ row, enabled }: { row: RouteChannelRow; enabled: boolean }) => {
      if (!row.apiKeyId || !row.apiKeyModelName) {
        throw new Error('api key model is required')
      }
      return updateSiteAPIKeyModelStatus(row.siteId, row.apiKeyId, {
        model: row.apiKeyModelName,
        enabled,
      })
    },
    onSuccess: async () => {
      await invalidateRouteModelQueries()
      await queryClient.invalidateQueries({ queryKey: [...sitesQueryKeys.all, 'routes-api-keys'] })
    },
    onError: (error) => {
      toast.error(t('workspace.toast.modelToggleFailed'), {
        description: error.message,
      })
    },
  })
  const updateSiteModelMutation = useMutation({
    mutationFn: ({ row, enabled }: { row: RouteChannelRow; enabled: boolean }) =>
      updateSiteModelStatus(row.siteId, row.siteModelId, { enabled }),
    onSuccess: invalidateRouteModelQueries,
    onError: (error) => {
      toast.error(t('workspace.toast.modelToggleFailed'), {
        description: error.message,
      })
    },
  })
  const pendingChannelState: PendingChannelState | undefined = updateAPIKeyModelMutation.isPending
    ? {
      id: updateAPIKeyModelMutation.variables.row.id,
      enabled: updateAPIKeyModelMutation.variables.enabled,
    }
    : updateSiteModelMutation.isPending
      ? {
        id: updateSiteModelMutation.variables.row.id,
        enabled: updateSiteModelMutation.variables.enabled,
      }
      : undefined

  const clearCooldownMutation = useMutation({
    mutationFn: clearRouteCooldown,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.cooldowns() })
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.overview({ windowHours: 24 }) })
      if (activeModelKey) {
        await queryClient.invalidateQueries({ queryKey: routeQueryKeys.selection({ modelKey: activeModelKey, debug: true, failoverLimit: 3 }) })
        await queryClient.invalidateQueries({ queryKey: routeQueryKeys.candidates({ modelKey: activeModelKey, debug: false }) })
      }
      toast.success(t('workspace.toast.cooldownCleared'))
    },
    onError: (error) => {
      toast.error(t('workspace.toast.cooldownClearFailed'), {
        description: error.message,
      })
    },
  })

  function toggleModel(modelKey: string) {
    setExpandedModelKey((current) => (current === modelKey ? null : modelKey))
  }

  function refreshAll() {
    overviewQuery.refetch()
    cooldownsQuery.refetch()
    selectionQuery.refetch()
    candidatesQuery.refetch()
    tracesQuery.refetch()
  }

  function handleToggleChannel(row: RouteChannelRow, enabled: boolean) {
    if (row.kind === 'api_key') {
      updateAPIKeyModelMutation.mutate({ row, enabled })
      return
    }

    updateSiteModelMutation.mutate({ row, enabled })
  }

  async function invalidateRouteModelQueries() {
    await queryClient.invalidateQueries({ queryKey: [...sitesQueryKeys.all, 'routes-models'] })
    await queryClient.invalidateQueries({ queryKey: routeQueryKeys.overview({ windowHours: 24 }) })
    if (activeModelKey) {
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.selection({ modelKey: activeModelKey, debug: true, failoverLimit: 3 }) })
      await queryClient.invalidateQueries({ queryKey: routeQueryKeys.candidates({ modelKey: activeModelKey, debug: false }) })
    }
  }

  function renderRouteDetails(item?: RouteOverviewItem, compact = false) {
    const allCooldowns = cooldownsQuery.data?.items ?? EMPTY_COOLDOWNS
    const canonicalId = item?.canonical_model.id ?? ''
    const cooldowns = canonicalId
      ? filterCooldownsForCanonicalModel(allCooldowns, canonicalId, modelsMap)
      : EMPTY_COOLDOWNS

    return (
      <RouteModelDetails
        item={item}
        selectionQuery={selectionQuery}
        candidatesQuery={candidatesQuery}
        tracesQuery={tracesQuery}
        cooldowns={cooldowns}
        sites={sites}
        modelsMap={modelsMap}
        apiKeysMap={apiKeysMap}
        cooldownsLoading={cooldownsQuery.isLoading}
        clearingId={clearCooldownMutation.variables?.siteId}
        pendingChannelState={pendingChannelState}
        compact={compact}
        onToggleChannel={handleToggleChannel}
        onClearCooldown={(cooldown) => clearCooldownMutation.mutate({
          siteId: cooldown.site_id,
          siteModelId: cooldown.site_model_id,
          siteCredentialId: cooldown.site_credential_id,
          source: cooldown.source,
        })}
      />
    )
  }

  if (overviewQuery.isLoading || sitesQuery.isLoading || modelsMapQuery.isLoading || apiKeysMapQuery.isLoading) {
    return <RoutesWorkspaceSkeleton />
  }

  if (overviewQuery.isError || sitesQuery.isError || modelsMapQuery.isError || apiKeysMapQuery.isError) {
    const message = overviewQuery.error?.message
      ?? sitesQuery.error?.message
      ?? modelsMapQuery.error?.message
      ?? apiKeysMapQuery.error?.message
      ?? t('workspace.loadFailed')

    return (
      <ErrorState
        title={t('workspace.loadFailed')}
        description={message}
      />
    )
  }

  if (isMobile) {
    return (
      <div className="relative space-y-4 pb-2">
        <div>
          <PageHeader
            eyebrow={t('workspace.eyebrow')}
            title={t('workspace.title')}
            description={t('workspace.description')}
          />
        </div>

        <div className="sticky top-0 z-20 -mx-1">
          <div className="rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-2 shadow-[0_18px_38px_rgba(0,0,0,0.10)] backdrop-blur-[32px] backdrop-saturate-150">
            <MobileRoutesFilterBar
              search={search}
              brandItems={brandItems}
              siteItems={siteItems}
              selectedBrand={selectedBrand}
              selectedSite={selectedSite}
              sortMode={sortMode}
              isFetching={overviewQuery.isFetching || selectionQuery.isFetching || tracesQuery.isFetching || cooldownsQuery.isFetching}
              onSearchChange={setSearch}
              onBrandChange={setSelectedBrand}
              onSiteChange={setSelectedSite}
              onSortModeChange={setSortMode}
              onRefresh={refreshAll}
            />
          </div>
        </div>

        <MobileRoutesList
          items={filteredOverviewItems}
          canonicalMap={canonicalMap}
          expandedModelKey={expandedModelKey}
          onToggleModel={toggleModel}
          renderDetails={(item) => renderRouteDetails(item, true)}
        />

        {expandedModelKey && !filteredOverviewItems.some((item) => item.canonical_model.model_key === expandedModelKey) ? (
          <div className="overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))]">
            {renderRouteDetails(undefined, true)}
          </div>
        ) : null}
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col gap-6">
      <PageHeader
        eyebrow={t('workspace.eyebrow')}
        title={t('workspace.title')}
        description={t('workspace.description')}
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="relative w-72 shrink-0">
          <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-foreground/40" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('workspace.search')}
            className="pl-10"
          />
        </div>
        <Button
          variant="outline"
          onClick={refreshAll}
          disabled={overviewQuery.isFetching || selectionQuery.isFetching || tracesQuery.isFetching || cooldownsQuery.isFetching}
        >
          {overviewQuery.isFetching || selectionQuery.isFetching || tracesQuery.isFetching || cooldownsQuery.isFetching
            ? <LoaderCircle className="h-4 w-4 animate-spin" />
            : <RotateCcw className="h-4 w-4" />}
          {t('workspace.refresh')}
        </Button>
      </div>

      <div className="grid min-h-0 flex-1 gap-6 xl:grid-cols-[248px_minmax(0,1fr)]">
        <aside className="min-h-0 space-y-4 overflow-y-auto pr-1">
          <FilterColumn
            title={t('workspace.brand')}
            items={brandItems}
            selectedKey={selectedBrand}
            onSelect={setSelectedBrand}
          />
          <FilterColumn
            title={t('workspace.site')}
            items={siteItems}
            selectedKey={selectedSite}
            onSelect={(key) => setSelectedSite((current) => (current === key ? null : key))}
          />
          <SortColumn value={sortMode} onChange={setSortMode} />
        </aside>

        <section className="min-h-0 overflow-y-auto pr-1">
          <div className="space-y-4">
            {filteredOverviewItems.length ? (
              filteredOverviewItems.map((item) => {
                const modelKey = item.canonical_model.model_key
                const expanded = expandedModelKey === modelKey
                const canonical = canonicalMap.get(item.canonical_model.id)

                return (
                  <RouteModelCard
                    key={item.canonical_model.id}
                    item={item}
                    canonical={canonical}
                    expanded={expanded}
                    onClick={() => toggleModel(modelKey)}
                  >
                    {expanded ? renderRouteDetails(item) : null}
                  </RouteModelCard>
                )
              })
            ) : (
              <EmptyState title={t('workspace.noModels')} description={t('workspace.noModelsDesc')} />
            )}

            {expandedModelKey && !filteredOverviewItems.some((item) => item.canonical_model.model_key === expandedModelKey) ? (
              renderRouteDetails(undefined)
            ) : null}
          </div>
        </section>
      </div>
    </div>
  )
}
