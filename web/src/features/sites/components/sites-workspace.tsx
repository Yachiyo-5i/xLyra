import { useEffect, useMemo, useRef, useState } from 'react'
import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { LoaderCircle, Plus, RotateCcw, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { PageHeader } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'
import {
  createSite,
  deleteSite,
  getSite,
  listSiteAPIKeys,
  listSiteModels,
  listSites,
  listSiteTypes,
  refreshAllSites,
  refreshSite,
  sitesQueryKeys,
  updateSite,
  updateSiteEnabled,
  type Site,
  type SiteAPIKey,
  type SiteModel,
  type SiteTypeInfo,
} from '@/features/sites/api/sites'
import {
  listOAuthConnections,
  oauthQueryKeys,
  type OAuthConnectionListItem,
} from '@/features/oauth/api/oauth'
import { downstreamAPIKeyQueryKeys } from '@/features/api-keys/api/api-keys'
import {
  listSiteGroups,
  siteGroupQueryKeys,
  updateSiteGroupMembershipForSite,
  type SiteGroup,
} from '@/features/settings/api/site-groups'
import { fetchSystemProxyConfig } from '@/features/settings/api/settings'
import { ChannelSplitDialog } from '@/features/usage/components/channel-split-dialog'
import type { ChannelSplitTarget } from '@/features/usage/api/channel-split'
import { SiteAPIKeysDraw } from '@/features/sites/components/site-api-keys-draw'
import { GrokAccountsDraw } from '@/features/sites/components/grok-accounts-draw'
import {
  SiteCreateSheet,
  type SiteCreateSubmitInput,
} from '@/features/sites/components/site-create-sheet'
import { SiteModelTestSheet } from '@/features/sites/components/site-model-test-sheet'
import { SiteModelsDraw } from '@/features/sites/components/site-models-draw'
import { MobileSitesList } from '@/features/sites/components/mobile-sites-list'
import { SitesTable } from '@/features/sites/components/sites-table'
import {
  mergeListSite,
  removeListSite,
  sanitizeSiteList,
  siteModelItems,
} from '@/features/sites/lib/site-cache'
import {
  formatSiteTypeLabel,
  isSiteAbnormal,
  isSiteEnableBlockedByAbnormalState,
  isOAuthSite,
  normalizedSiteTypeFilter,
  readUpdatedAtMode,
  saveUpdatedAtMode,
  siteAccountEmail,
  siteTypeIcon,
  siteTypeIconClassName,
  sortSitesForDisplay,
} from '@/features/sites/lib/site-utils'
import type {
  SiteUpdatedAtDisplayMode,
  ValidationSnapshot,
} from '@/features/sites/lib/types'
import { useMobileLayout } from '@/hooks/use-media-query'
import { useResolvedTheme } from '@/hooks/theme-context'

const EMPTY_SITES: Site[] = []
const EMPTY_MODELS: SiteModel[] = []
const EMPTY_API_KEYS: SiteAPIKey[] = []
const EMPTY_SITE_TYPES: SiteTypeInfo[] = []
const EMPTY_SITE_GROUPS: SiteGroup[] = []
const EMPTY_OAUTH_CONNECTIONS: OAuthConnectionListItem[] = []
const SITE_SYNC_POLL_INTERVAL = 2000

function syncStatusActive(status?: string | null) {
  const normalized = status?.trim().toLowerCase()
  return normalized === 'pending' || normalized === 'syncing'
}

function siteURLTarget(params: URLSearchParams) {
  return {
    siteId: params.get('site_id')?.trim() ?? '',
    panel: params.get('panel')?.trim() ?? '',
  }
}

export function SitesWorkspace({
  initialSearch = '',
}: {
  initialSearch?: string
}) {
  const queryClient = useQueryClient()
  const { t } = useTranslation('sites')
  const resolvedMode = useResolvedTheme()
  const isMobile = useMobileLayout()
  const [initialURLTarget] = useState(() =>
    siteURLTarget(new URLSearchParams(initialSearch)),
  )
  const [search, setSearch] = useState('')
  const [urlSiteFilterActive, setURLSiteFilterActive] = useState(
    Boolean(initialURLTarget.siteId),
  )
  const [typeTab, setTypeTab] = useState('all')
  const [createOpen, setCreateOpen] = useState(false)
  const [editingSite, setEditingSite] = useState<Site | null>(null)
  const [modelsSite, setModelsSite] = useState<Site | null>(null)
  const [testSite, setTestSite] = useState<Site | null>(null)
  const [apiKeysSite, setAPIKeysSite] = useState<Site | null>(null)
  const [grokAccountsSite, setGrokAccountsSite] = useState<Site | null>(null)
  const [usageSplitTarget, setUsageSplitTarget] =
    useState<ChannelSplitTarget | null>(null)
  const [urlAPIKeysOpen, setURLAPIKeysOpen] = useState(true)
  const [deleteTargetSite, setDeleteTargetSite] = useState<Site | null>(null)
  const [validationSnapshots, setValidationSnapshots] = useState<
    Record<string, ValidationSnapshot>
  >({})
  const [refreshingSiteIds, setRefreshingSiteIds] = useState<string[]>([])
  const [abnormalCollapsed, setAbnormalCollapsed] = useState(true)
  const [updatedAtMode, setUpdatedAtMode] =
    useState<SiteUpdatedAtDisplayMode>(readUpdatedAtMode)
  const [now, setNow] = useState(() => Date.now())
  const previousSiteSyncStatuses = useRef<Record<string, string>>({})
  const previousAPIKeySyncStatuses = useRef<Record<string, string>>({})

  const sitesQuery = useQuery({
    queryKey: sitesQueryKeys.list(),
    queryFn: () => listSites(),
    refetchInterval: (query) =>
      query.state.data?.items.some((site) =>
        syncStatusActive(site.sync_state?.status),
      )
        ? SITE_SYNC_POLL_INTERVAL
        : false,
  })
  const splitSitesQuery = useQuery({
    queryKey: sitesQueryKeys.list('all', 'with_requests'),
    queryFn: () => listSites({ oauth: 'all', deleted: 'with_requests' }),
  })
  const oauthConnectionsQuery = useQuery({
    queryKey: oauthQueryKeys.connections(),
    queryFn: listOAuthConnections,
  })
  const siteTypesQuery = useQuery({
    queryKey: [...sitesQueryKeys.all, 'site-types'],
    queryFn: listSiteTypes,
  })
  const siteGroupsQuery = useQuery({
    queryKey: siteGroupQueryKeys.list(),
    queryFn: listSiteGroups,
  })
  const systemProxyQuery = useQuery({
    queryKey: ['settings', 'system-proxy'],
    queryFn: fetchSystemProxyConfig,
  })
  const sites = useMemo(
    () =>
      sortSitesForDisplay(
        sanitizeSiteList(sitesQuery.data?.items ?? EMPTY_SITES),
      ),
    [sitesQuery.data?.items],
  )
  const oauthConnections =
    oauthConnectionsQuery.data?.items ?? EMPTY_OAUTH_CONNECTIONS
  const urlTargetSite = useMemo(
    () => sites.find((site) => site.id === initialURLTarget.siteId) ?? null,
    [sites, initialURLTarget.siteId],
  )
  const siteIds = useMemo(() => sites.map((site) => site.id), [sites])

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    saveUpdatedAtMode(updatedAtMode)
  }, [updatedAtMode])

  const siteModelQueries = useQueries({
    queries: sites.map((site) => ({
      queryKey: sitesQueryKeys.models(site.id),
      queryFn: () => listSiteModels(site.id),
    })),
  })
  const sitesWithAPIKeys = useMemo(
    () => sites.filter((site) => !isOAuthSite(site) && site.site_type !== 'grok'),
    [sites],
  )
  const siteAPIKeyQueries = useQueries({
    queries: sitesWithAPIKeys.map((site) => ({
      queryKey: [...sitesQueryKeys.detail(site.id), 'api-keys'],
      queryFn: () => listSiteAPIKeys(site.id),
      refetchInterval: (query: {
        state: { data?: { items?: SiteAPIKey[] } }
      }) =>
        query.state.data?.items?.some((apiKey) =>
          syncStatusActive(apiKey.sync_status),
        )
          ? SITE_SYNC_POLL_INTERVAL
          : false,
    })),
  })

  const editingSiteDetailQuery = useQuery({
    queryKey: editingSite
      ? sitesQueryKeys.detail(editingSite.id)
      : [...sitesQueryKeys.all, 'detail', 'idle'],
    queryFn: () => getSite(editingSite!.id),
    enabled: createOpen && Boolean(editingSite),
  })
  const editingInitialSite = editingSiteDetailQuery.data?.site ?? editingSite
  const siteModelsMap = useMemo(
    () =>
      Object.fromEntries(
        sites.map((site, index) => [
          site.id,
          siteModelItems(siteModelQueries[index]?.data),
        ]),
      ) as Record<string, SiteModel[]>,
    [siteModelQueries, sites],
  )
  const siteAPIKeysMap = useMemo(
    () =>
      Object.fromEntries(
        sitesWithAPIKeys.map((site, index) => [
          site.id,
          siteAPIKeyQueries[index]?.data?.items ?? EMPTY_API_KEYS,
        ]),
      ) as Record<string, SiteAPIKey[]>,
    [siteAPIKeyQueries, sitesWithAPIKeys],
  )
  const siteTypes = siteTypesQuery.data?.items ?? EMPTY_SITE_TYPES
  const siteGroups = siteGroupsQuery.data?.items ?? EMPTY_SITE_GROUPS
  const proxies = systemProxyQuery.data?.proxies ?? []

  useEffect(() => {
    const nextStatuses: Record<string, string> = {}
    const completedSiteIds: string[] = []
    for (const site of sites) {
      const status = site.sync_state?.status?.trim().toLowerCase() ?? ''
      nextStatuses[site.id] = status
      if (
        syncStatusActive(previousSiteSyncStatuses.current[site.id]) &&
        !syncStatusActive(status)
      ) {
        completedSiteIds.push(site.id)
      }
    }
    previousSiteSyncStatuses.current = nextStatuses
    if (!completedSiteIds.length) return
    void Promise.all([
      ...completedSiteIds.map((siteId) =>
        queryClient.invalidateQueries({
          queryKey: sitesQueryKeys.models(siteId),
        }),
      ),
      ...completedSiteIds.map((siteId) =>
        queryClient.invalidateQueries({
          queryKey: [...sitesQueryKeys.detail(siteId), 'api-keys'],
        }),
      ),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'canonical-models'],
      }),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
      }),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'all-pricings'],
      }),
      queryClient.invalidateQueries({
        queryKey: ['settings', 'model-prices'],
      }),
      queryClient.invalidateQueries({
        queryKey: downstreamAPIKeyQueryKeys.list(),
      }),
    ])
  }, [queryClient, sites])

  useEffect(() => {
    const nextStatuses: Record<string, string> = {}
    const completedSiteIds = new Set<string>()
    for (const [siteId, apiKeys] of Object.entries(siteAPIKeysMap)) {
      for (const apiKey of apiKeys) {
        const key = `${siteId}:${apiKey.id}`
        const status = apiKey.sync_status?.trim().toLowerCase() ?? ''
        nextStatuses[key] = status
        if (
          syncStatusActive(previousAPIKeySyncStatuses.current[key]) &&
          !syncStatusActive(status)
        ) {
          completedSiteIds.add(siteId)
        }
      }
    }
    previousAPIKeySyncStatuses.current = nextStatuses
    if (!completedSiteIds.size) return
    void Promise.all([
      ...Array.from(completedSiteIds).map((siteId) =>
        queryClient.invalidateQueries({
          queryKey: sitesQueryKeys.models(siteId),
        }),
      ),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'canonical-models'],
      }),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
      }),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'all-pricings'],
      }),
      queryClient.invalidateQueries({
        queryKey: ['settings', 'model-prices'],
      }),
      queryClient.invalidateQueries({
        queryKey: downstreamAPIKeyQueryKeys.list(),
      }),
    ])
  }, [queryClient, siteAPIKeysMap])

  const urlTargetSearch = urlTargetSite
    ? urlTargetSite.name || urlTargetSite.slug
    : ''
  const effectiveSearch = urlSiteFilterActive ? urlTargetSearch : search
  const effectiveTypeTab =
    urlSiteFilterActive && urlTargetSite ? 'all' : typeTab
  const effectiveAbnormalCollapsed =
    urlSiteFilterActive &&
    urlTargetSite &&
    isSiteAbnormal(urlTargetSite, validationSnapshots[urlTargetSite.id])
      ? false
      : abnormalCollapsed

  const filterTabs = useMemo(() => {
    const seen = new Set<string>()
    const tabs: { value: string; label: string; icon: string | null }[] = []
    for (const site of sites) {
      const key = normalizedSiteTypeFilter(site.site_type)
      if (seen.has(key)) continue
      seen.add(key)
      tabs.push({
        value: key,
        label: formatSiteTypeLabel(site.site_type, siteTypes),
        icon: siteTypeIcon(site.site_type, siteTypes, resolvedMode),
      })
    }
    return tabs
  }, [resolvedMode, siteTypes, sites])

  const filteredSites = useMemo(() => {
    const keyword = effectiveSearch.trim().toLowerCase()
    return sites.filter((site) => {
      const matchSearch =
        !keyword ||
        site.name.toLowerCase().includes(keyword) ||
        site.slug.toLowerCase().includes(keyword) ||
        site.base_url.toLowerCase().includes(keyword) ||
        siteAccountEmail(site).toLowerCase().includes(keyword)
      const matchType =
        effectiveTypeTab === 'all' ||
        normalizedSiteTypeFilter(site.site_type) === effectiveTypeTab
      return matchSearch && matchType
    })
  }, [effectiveSearch, effectiveTypeTab, sites])
  const normalSites = useMemo(
    () =>
      filteredSites.filter(
        (site) => !isSiteAbnormal(site, validationSnapshots[site.id]),
      ),
    [filteredSites, validationSnapshots],
  )
  const abnormalSites = useMemo(
    () =>
      filteredSites.filter((site) =>
        isSiteAbnormal(site, validationSnapshots[site.id]),
      ),
    [filteredSites, validationSnapshots],
  )

  const createMutation = useMutation({
    mutationFn: (input: SiteCreateSubmitInput) => createSite(input),
    onSuccess: async (result, input) => {
      queryClient.setQueryData(
        sitesQueryKeys.list(),
        (current: { items: Site[]; meta: { count: number } } | undefined) =>
          mergeListSite(current, result.site),
      )
      if (result.validation) {
        setValidationSnapshots((previous) => ({
          ...previous,
          [result.site.id]: {
            ...result.validation!,
            executedAt: new Date().toISOString(),
          },
        }))
      }
      if (result.model_sync?.items) {
        queryClient.setQueryData(sitesQueryKeys.models(result.site.id), {
          items: result.model_sync.items,
          meta: { count: result.model_sync.items.length },
        })
      }
      notifySiteSetupIncomplete(result)
      setEditingSite(null)
      setCreateOpen(false)
      toast.success(t('page.toast.syncQueued'))
      try {
        await updateSiteGroupMembershipForSite(
          siteGroups,
          result.site.id,
          input.siteGroupIds ?? [],
        )
      } catch (error) {
        toast.error(t('page.toast.saveFailed'), {
          description: error instanceof Error ? error.message : String(error),
        })
      }
      await invalidateSiteLists()
    },
    onError: (error) =>
      toast.error(t('page.toast.createFailed'), { description: error.message }),
  })

  const updateMutation = useMutation({
    mutationFn: async ({
      siteId,
      input,
    }: {
      siteId: string
      input: SiteCreateSubmitInput
    }) => {
      const result = await updateSite(siteId, input)
      await updateSiteGroupMembershipForSite(
        siteGroups,
        result.site.id,
        input.siteGroupIds ?? [],
      )
      return result
    },
    onSuccess: async (result) => {
      queryClient.setQueryData(
        sitesQueryKeys.list(),
        (current: { items: Site[]; meta: { count: number } } | undefined) =>
          mergeListSite(current, result.site),
      )
      if (result.validation) {
        setValidationSnapshots((previous) => ({
          ...previous,
          [result.site.id]: {
            ...result.validation!,
            executedAt: new Date().toISOString(),
          },
        }))
      }
      if (result.model_sync?.items) {
        queryClient.setQueryData(sitesQueryKeys.models(result.site.id), {
          items: result.model_sync.items,
          meta: { count: result.model_sync.items.length },
        })
      }
      notifySiteSetupIncomplete(result)
      setEditingSite(null)
      setCreateOpen(false)
      await invalidateSiteLists()
    },
    onError: (error) =>
      toast.error(t('page.toast.saveFailed'), { description: error.message }),
  })

  const deleteMutation = useMutation({
    mutationFn: deleteSite,
    onSuccess: (_result, siteId) => {
      for (const oauth of ['exclude', 'all', 'only'] as const) {
        queryClient.setQueryData(
          sitesQueryKeys.list(oauth),
          (current: { items: Site[]; meta: { count: number } } | undefined) =>
            removeListSite(current, siteId),
        )
      }
      queryClient.removeQueries({
        queryKey: sitesQueryKeys.detail(siteId),
      })
      queryClient.removeQueries({
        queryKey: sitesQueryKeys.models(siteId),
        exact: true,
      })
      setDeleteTargetSite((current) =>
        current?.id === siteId ? null : current,
      )
      setEditingSite(null)
      setCreateOpen(false)
      void invalidateSiteLists()
    },
    onError: (error) =>
      toast.error(t('page.toast.deleteFailed'), { description: error.message }),
  })

  const refreshSiteMutation = useMutation({
    mutationFn: (siteId: string) => refreshSite(siteId),
    onMutate: (siteId) =>
      setRefreshingSiteIds((current) =>
        current.includes(siteId) ? current : [...current, siteId],
      ),
    onSuccess: async (result) => {
      queryClient.setQueryData(
        sitesQueryKeys.list(),
        (current: { items: Site[]; meta: { count: number } } | undefined) =>
          mergeListSite(current, result.site),
      )
      if (result.site.validation) {
        setValidationSnapshots((previous) => ({
          ...previous,
          [result.site.id]: {
            ...result.site.validation!,
            executedAt: new Date().toISOString(),
          },
        }))
      }
      if (result.model_sync?.items) {
        queryClient.setQueryData(sitesQueryKeys.models(result.site.id), {
          items: result.model_sync.items,
          meta: { count: result.model_sync.items.length },
        })
      }
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.detail(result.site.id), 'api-keys'],
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.detail(result.site.id), 'grok-accounts'],
      })
      await queryClient.invalidateQueries({
        queryKey: sitesQueryKeys.models(result.site.id),
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'all-pricings'],
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'models-marketplace-api-keys'],
      })
      await queryClient.invalidateQueries({
        queryKey: ['settings', 'model-prices'],
      })
      if (!result.ok)
        toast.error(t('page.toast.refreshIncomplete'), {
          description: result.message,
        })
    },
    onError: (error) =>
      toast.error(t('page.toast.refreshFailed'), {
        description: error.message,
      }),
    onSettled: (_result, _error, siteId) =>
      setRefreshingSiteIds((current) => current.filter((id) => id !== siteId)),
  })

  const refreshAllMutation = useMutation({
    mutationFn: refreshAllSites,
    onMutate: () => {
      setRefreshingSiteIds(siteIds)
    },
    onSuccess: async (result) => {
      const latest = sanitizeSiteList(
        result.items.map((item) => item.site),
      ).filter((site) => !isOAuthSite(site))
      queryClient.setQueryData(sitesQueryKeys.list(), {
        items: sortSitesForDisplay(latest),
        meta: { count: latest.length },
      })
      for (const item of result.items) {
        if (item.site.validation) {
          setValidationSnapshots((previous) => ({
            ...previous,
            [item.site.id]: {
              ...item.site.validation!,
              executedAt: new Date().toISOString(),
            },
          }))
        }
      }
      await queryClient.invalidateQueries({ queryKey: sitesQueryKeys.list() })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'models'],
      })
      await Promise.all(
        sitesWithAPIKeys.map((site) =>
          queryClient.invalidateQueries({
            queryKey: [...sitesQueryKeys.detail(site.id), 'api-keys'],
            exact: true,
          }),
        ),
      )
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'all-pricings'],
      })
      await queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'models-marketplace-api-keys'],
      })
      await queryClient.invalidateQueries({
        queryKey: ['settings', 'model-prices'],
      })
      if (!result.ok) {
        toast.error(t('page.toast.refreshPartialFailed'), {
          description: result.meta?.failure_count
            ? t('page.toast.refreshPartialDesc', {
                count: result.meta.failure_count,
              })
            : t('page.toast.refreshPartialDescFallback'),
        })
      }
    },
    onError: (error) => {
      setRefreshingSiteIds([])
      toast.error(t('page.toast.refreshFailed'), {
        description: error.message,
      })
    },
    onSettled: () => setRefreshingSiteIds([]),
  })

  const toggleEnabledMutation = useMutation({
    mutationFn: ({ site, enabled }: { site: Site; enabled: boolean }) =>
      updateSiteEnabled(site.id, { enabled }),
    onMutate: async ({ site, enabled }) => {
      await queryClient.cancelQueries({ queryKey: sitesQueryKeys.list() })
      const previous = queryClient.getQueryData<{
        items: Site[]
        meta: { count: number }
      }>(sitesQueryKeys.list())
      queryClient.setQueryData(
        sitesQueryKeys.list(),
        (current: { items: Site[]; meta: { count: number } } | undefined) =>
          mergeListSite(current, { ...site, enabled }),
      )
      return { previous }
    },
    onSuccess: async (result) => {
      queryClient.setQueryData(
        sitesQueryKeys.list(),
        (current: { items: Site[]; meta: { count: number } } | undefined) =>
          mergeListSite(current, result.site),
      )
      await invalidateSiteLists()
    },
    onError: (error, _variables, context) => {
      if (context?.previous)
        queryClient.setQueryData(sitesQueryKeys.list(), context.previous)
      toast.error(t('page.toast.statusFailed'), { description: error.message })
    },
  })

  function invalidateSiteLists() {
    return Promise.all([
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'list'],
      }),
      queryClient.invalidateQueries({
        queryKey: siteGroupQueryKeys.list(),
        exact: true,
      }),
      queryClient.invalidateQueries({
        queryKey: oauthQueryKeys.connections(),
        exact: true,
      }),
      queryClient.invalidateQueries({
        queryKey: downstreamAPIKeyQueryKeys.list(),
        exact: true,
      }),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'canonical-models'],
      }),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'routes-matrix'],
      }),
      queryClient.invalidateQueries({
        queryKey: [...sitesQueryKeys.all, 'all-pricings'],
        exact: true,
      }),
      queryClient.invalidateQueries({
        queryKey: ['settings', 'model-prices'],
        exact: true,
      }),
    ])
  }

  function notifySiteSetupIncomplete(result: {
    ok?: boolean
    message?: string
    validation?: { ok: boolean; message?: string }
    model_sync?: { ok: boolean; message?: string }
  }) {
    const message =
      result.message || result.validation?.message || result.model_sync?.message
    if (
      result.ok === false ||
      result.validation?.ok === false ||
      result.model_sync?.ok === false
    ) {
      toast.error(t('page.toast.refreshIncomplete'), { description: message })
    }
  }

  function openCreateSheet() {
    setEditingSite(null)
    setCreateOpen(true)
  }

  function openEditSheet(site: Site) {
    setEditingSite(site)
    setCreateOpen(true)
  }

  function toggleSiteEnabled(site: Site) {
    const enabled = !site.enabled
    if (
      enabled &&
      isSiteEnableBlockedByAbnormalState(site, validationSnapshots[site.id])
    )
      return
    toggleEnabledMutation.mutate({ site, enabled })
  }

  const siteAPIKeysLoading = siteAPIKeyQueries.some((query) => query.isLoading)
  const siteAPIKeysError = siteAPIKeyQueries.find((query) => query.isError)
  const isDataLoading = sitesQuery.isLoading || siteAPIKeysLoading
  const isDataError = sitesQuery.isError || Boolean(siteAPIKeysError)
  const isDeleteTargetPending =
    deleteMutation.isPending &&
    deleteMutation.variables === deleteTargetSite?.id

  const tableProps = {
    items: normalSites,
    abnormalItems: abnormalSites,
    abnormalCollapsed: effectiveAbnormalCollapsed,
    onAbnormalCollapsedChange: setAbnormalCollapsed,
    modelsMap: siteModelsMap,
    apiKeysMap: siteAPIKeysMap,
    validationSnapshots,
    refreshingSiteIds,
    togglingSiteId: toggleEnabledMutation.isPending
      ? toggleEnabledMutation.variables?.site.id
      : null,
    deletingSiteId: deleteMutation.isPending ? deleteMutation.variables : null,
    updatedAtMode,
    now,
    onUpdatedAtModeChange: setUpdatedAtMode,
    onRefresh: (id: string) => refreshSiteMutation.mutate(id),
    onToggleEnabled: toggleSiteEnabled,
    onEdit: openEditSheet,
    onDelete: (site: Site) => setDeleteTargetSite(site),
    onOpenModels: setModelsSite,
    onOpenAPIKeys: setAPIKeysSite,
    onOpenGrokAccounts: setGrokAccountsSite,
    onOpenTest: setTestSite,
    onOpenUsageSplit: (site: Site) =>
      setUsageSplitTarget({ type: 'site', id: site.id }),
    resolvedMode,
    siteTypes,
  }

  const searchControl = (
    <div className="relative min-w-0 flex-1 md:w-52 md:flex-none">
      <Input
        value={effectiveSearch}
        onChange={(event) => {
          setURLSiteFilterActive(false)
          setSearch(event.target.value)
        }}
        placeholder={t('page.search')}
        className="pl-10"
      />
      <Search className="text-foreground/40 pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
    </div>
  )

  const tabControls = (
    <div className="min-w-0 overflow-x-auto">
      <Tabs value={effectiveTypeTab} onValueChange={setTypeTab}>
        <TabsList className="inline-flex w-max">
          <TabsTrigger value="all">{t('page.all')}</TabsTrigger>
          {filterTabs.map((tab) => (
            <TabsTrigger key={tab.value} value={tab.value}>
              <span className="flex items-center gap-1.5">
                {tab.icon ? (
                  <img
                    src={tab.icon}
                    alt=""
                    className={cn(
                      'h-4 w-4 rounded object-contain',
                      siteTypeIconClassName(tab.value),
                    )}
                  />
                ) : null}
                {tab.label}
              </span>
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
    </div>
  )

  const content = isDataLoading ? (
    <div className="space-y-3">
      <Skeleton className="h-28 w-full md:h-14" />
      <Skeleton className="h-28 w-full md:h-14" />
      <Skeleton className="h-28 w-full md:h-14" />
    </div>
  ) : isDataError ? (
    <ErrorState
      title={t('page.loadFailed')}
      description={
        sitesQuery.error?.message ??
        siteAPIKeysError?.error?.message ??
        t('page.unknownError')
      }
      action={
        <Button
          variant="outline"
          onClick={() => {
            sitesQuery.refetch()
            void Promise.all(siteAPIKeyQueries.map((query) => query.refetch()))
          }}
        >
          {t('page.retry')}
        </Button>
      }
    />
  ) : filteredSites.length ? (
    isMobile ? (
      <MobileSitesList {...tableProps} />
    ) : (
      <SitesTable
        {...tableProps}
        className="min-h-0 flex-1 [&>div]:scrollbar-hidden [&>div]:h-full [&>div]:overflow-auto [&_table]:min-w-[1120px] [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10"
      />
    )
  ) : (
    <EmptyState
      title={sites.length ? t('page.emptyFiltered') : t('page.emptyTitle')}
      description={
        sites.length ? t('page.emptyFilteredDesc') : t('page.emptyDesc')
      }
    />
  )

  const layout = isMobile ? (
    <div className="relative space-y-4 pb-2">
      <div>
        <PageHeader
          eyebrow={t('page.eyebrow')}
          title={t('page.title')}
          description={t('page.description')}
        />
      </div>

      <div className="sticky top-0 z-20 -mx-1">
        <div className="rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-2 shadow-[0_18px_38px_rgba(0,0,0,0.10)] backdrop-blur-[32px] backdrop-saturate-150">
          <div className="flex items-center gap-2">
            {searchControl}
            <Button
              variant="secondary"
              size="icon"
              className="h-10 w-10 shrink-0 bg-[hsl(var(--card)/0.34)] backdrop-blur-[24px] hover:bg-[hsl(var(--card)/0.46)]"
              onClick={() => refreshAllMutation.mutate()}
              disabled={refreshAllMutation.isPending}
              aria-label={t('page.refreshAll')}
            >
              {refreshAllMutation.isPending ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : (
                <RotateCcw className="h-4 w-4" />
              )}
            </Button>
          </div>
          <div className="mt-2">{tabControls}</div>
        </div>
      </div>

      {content}

      <Button
        className="fixed bottom-[calc(env(safe-area-inset-bottom,0px)+5.5rem)] right-5 z-20 h-12 w-12 rounded-full p-0 shadow-[0_18px_34px_rgba(15,23,42,0.22)]"
        onClick={openCreateSheet}
        aria-label={t('page.addSite')}
      >
        <Plus className="h-5 w-5" />
      </Button>
    </div>
  ) : (
    <div className="flex h-full min-h-0 flex-col gap-5 overflow-hidden">
      <PageHeader
        eyebrow={t('page.eyebrow')}
        title={t('page.title')}
        description={t('page.description')}
      />

      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="flex-1 flex items-center gap-3 min-w-0">
          {searchControl}
          {tabControls}
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <Button
            variant="secondary"
            onClick={() => refreshAllMutation.mutate()}
            disabled={refreshAllMutation.isPending}
          >
            {refreshAllMutation.isPending ? (
              <LoaderCircle className="h-4 w-4 animate-spin" />
            ) : (
              <RotateCcw className="h-4 w-4" />
            )}
            {t('page.refreshAll')}
          </Button>
          <Button onClick={openCreateSheet}>
            <Plus className="h-4 w-4" />
            {t('page.addSite')}
          </Button>
        </div>
      </div>

      <div
        className={cn(
          'min-h-0 flex-1',
          filteredSites.length && !isDataLoading && !isDataError
            ? 'contents'
            : 'scrollbar-hidden overflow-auto',
        )}
      >
        {content}
      </div>
    </div>
  )

  return (
    <div className={isMobile ? 'min-h-full' : 'h-full min-h-0 overflow-hidden'}>
      {layout}
      {createOpen ? (
        <SiteCreateSheet
          open={createOpen}
          onOpenChange={(next) => {
            setCreateOpen(next)
            if (!next) setEditingSite(null)
          }}
          mode={editingSite ? 'edit' : 'create'}
          initialSite={editingInitialSite}
          isPending={
            createMutation.isPending ||
            updateMutation.isPending ||
            editingSiteDetailQuery.isFetching
          }
          siteTypes={siteTypes}
          siteGroups={siteGroups}
          proxies={proxies}
          siteGroupsLoading={siteGroupsQuery.isLoading}
          onSubmit={async (input) => {
            if (editingSite) {
              await updateMutation.mutateAsync({
                siteId: editingSite.id,
                input,
              })
              return
            }
            await createMutation.mutateAsync(input)
          }}
        />
      ) : null}

      <SiteModelsDraw
        site={modelsSite}
        models={
          modelsSite
            ? (siteModelsMap[modelsSite.id] ?? EMPTY_MODELS)
            : EMPTY_MODELS
        }
        apiKeys={
          modelsSite
            ? (siteAPIKeysMap[modelsSite.id] ?? EMPTY_API_KEYS)
            : EMPTY_API_KEYS
        }
        onOpenChange={(open) => {
          if (!open) setModelsSite(null)
        }}
      />

      <SiteModelTestSheet
        site={testSite}
        models={
          testSite ? (siteModelsMap[testSite.id] ?? EMPTY_MODELS) : EMPTY_MODELS
        }
        onOpenChange={(open) => {
          if (!open) setTestSite(null)
        }}
      />

      <SiteAPIKeysDraw
        site={
          apiKeysSite ??
          (urlAPIKeysOpen &&
          initialURLTarget.panel === 'api_keys' &&
          urlTargetSite?.site_type !== 'grok'
            ? urlTargetSite
            : null)
        }
        onOpenChange={(open) => {
          if (!open) {
            setAPIKeysSite(null)
            setURLAPIKeysOpen(false)
          }
        }}
      />

      <GrokAccountsDraw
        site={
          grokAccountsSite ??
          (urlAPIKeysOpen &&
          urlTargetSite?.site_type === 'grok' &&
          ['api_keys', 'grok_accounts'].includes(initialURLTarget.panel)
            ? urlTargetSite
            : null)
        }
        onOpenChange={(open) => {
          if (!open) {
            setGrokAccountsSite(null)
            setURLAPIKeysOpen(false)
          }
        }}
      />

      {usageSplitTarget ? (
        <ChannelSplitDialog
          key={`${usageSplitTarget.type}:${usageSplitTarget.id}`}
          open
          initialTarget={usageSplitTarget}
          sites={sortSitesForDisplay(
            sanitizeSiteList(splitSitesQuery.data?.items ?? sites),
          )}
          oauthConnections={oauthConnections}
          onOpenChange={(open) => {
            if (!open) setUsageSplitTarget(null)
          }}
        />
      ) : null}

      <Dialog
        open={Boolean(deleteTargetSite)}
        onOpenChange={(next) => {
          if (!next && !isDeleteTargetPending) setDeleteTargetSite(null)
        }}
      >
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('page.deleteDialog.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="pt-0 text-sm text-muted-soft">
            <DialogDescription className="mt-0">
              {deleteTargetSite
                ? t('page.deleteDialog.confirm', {
                    name: deleteTargetSite.name,
                  })
                : t('page.deleteDialog.confirmDefault')}
            </DialogDescription>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button
              variant="outline"
              onClick={() => setDeleteTargetSite(null)}
              disabled={isDeleteTargetPending}
            >
              {t('page.deleteDialog.cancel')}
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                if (!deleteTargetSite) return
                deleteMutation.mutate(deleteTargetSite.id)
              }}
              disabled={isDeleteTargetPending}
            >
              {isDeleteTargetPending ? (
                <LoaderCircle className="h-4 w-4 animate-spin" />
              ) : null}
              {t('page.deleteDialog.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
