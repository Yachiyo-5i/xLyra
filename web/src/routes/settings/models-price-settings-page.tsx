import {
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router-dom'
import { PageHeader } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/lib/toast'
import {
  bulkUpdateModelPrices,
  listModelPrices,
  modelPriceQueryKeys,
  resetSiteModelPricing,
  resetSitePricing,
  updateSiteModelPricing,
  type ModelPriceBillingType,
  type ModelPriceInput,
  type ModelPriceItem,
} from '@/features/settings/api/model-prices'
import {
  ModelsPriceFilterBar,
  type ModelsPriceBillingFilter,
  type ModelsPriceStatusFilter,
} from '@/features/settings/components/models-price/models-price-filter-bar'
import {
  MobileModelsPriceList,
  type MobileModelsPriceListHandle,
  type MobileModelsPriceSaveState,
} from '@/features/settings/components/models-price/mobile-models-price-list'
import { ModelsPriceTable } from '@/features/settings/components/models-price/models-price-table'
import {
  buildModelPriceGroups,
  buildSiteModelGroup,
  extractSiteOptions,
  type ModelPriceGroup,
} from '@/features/settings/lib/model-price-utils'
import { LoaderCircle, Save, Search } from 'lucide-react'
import { useMobileLayout } from '@/hooks/use-media-query'

type ModelsPriceTab = 'all' | 'missing'

function modelPriceURLFilters(params: URLSearchParams) {
  const pricingStatus = modelPriceStatusFromParam(params.get('pricing_status'))
  return {
    search: params.get('q')?.trim() ?? '',
    billingType: modelPriceBillingFromParam(params.get('billing_type')),
    pricingStatus,
    siteId: params.get('site_id')?.trim() ?? '',
    modelKey: params.get('model_key')?.trim() ?? '',
  }
}

function modelPriceBillingFromParam(
  value: string | null,
): ModelsPriceBillingFilter {
  if (value === 'tokens' || value === 'per_request') return value
  return 'all'
}

function modelPriceStatusFromParam(
  value: string | null,
): ModelsPriceStatusFilter {
  if (value === 'manual' || value === 'synced' || value === 'missing')
    return value
  return 'all'
}

export function ModelsPriceSettingsPage() {
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => {
    if (!location.search) return
    navigate(
      { pathname: location.pathname, hash: location.hash },
      { replace: true },
    )
  }, [location.hash, location.pathname, location.search, navigate])

  return <ModelsPriceSettingsWorkspace initialSearch={location.search} />
}

function ModelsPriceSettingsWorkspace({
  initialSearch = '',
}: {
  initialSearch?: string
}) {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const isMobile = useMobileLayout()
  const searchParams = useMemo(
    () => new URLSearchParams(initialSearch),
    [initialSearch],
  )
  const initialURLFilters = modelPriceURLFilters(searchParams)
  const [activeTab, setActiveTab] = useState<ModelsPriceTab>(
    initialURLFilters.pricingStatus === 'missing' ? 'missing' : 'all',
  )
  const [search, setSearch] = useState(initialURLFilters.search)
  const [billingType, setBillingType] = useState<ModelsPriceBillingFilter>(
    initialURLFilters.billingType,
  )
  const [pricingStatus, setPricingStatus] = useState<ModelsPriceStatusFilter>(
    initialURLFilters.pricingStatus,
  )
  const [urlModelKey] = useState(initialURLFilters.modelKey)
  const deferredSearch = useDeferredValue(search.trim())
  const [pendingBulkGroupId, setPendingBulkGroupId] = useState<string | null>(
    null,
  )
  const [pendingSiteModelId, setPendingSiteModelId] = useState<string | null>(
    null,
  )
  const [siteId, setSiteId] = useState(initialURLFilters.siteId || 'all')
  const mobileListRef = useRef<MobileModelsPriceListHandle>(null)
  const [mobileSaveState, setMobileSaveState] =
    useState<MobileModelsPriceSaveState>({
      dirtyCount: 0,
      saving: false,
    })
  const handleMobileSaveStateChange = useCallback(
    (nextState: MobileModelsPriceSaveState) => {
      setMobileSaveState((previous) =>
        previous.dirtyCount === nextState.dirtyCount &&
        previous.saving === nextState.saving
          ? previous
          : nextState,
      )
    },
    [],
  )

  const filters = useMemo(
    () => ({
      q: deferredSearch || undefined,
      modelKey: urlModelKey || undefined,
      billingType,
      pricingStatus: pricingStatus === 'all' ? undefined : pricingStatus,
    }),
    [billingType, deferredSearch, pricingStatus, urlModelKey],
  )

  const modelPricesQuery = useQuery({
    queryKey: modelPriceQueryKeys.list(filters),
    queryFn: () => listModelPrices(filters),
    placeholderData: keepPreviousData,
  })

  const groups = useMemo(
    () => buildModelPriceGroups(modelPricesQuery.data?.items ?? []),
    [modelPricesQuery.data?.items],
  )

  const sites = useMemo(
    () => extractSiteOptions(modelPricesQuery.data?.items ?? []),
    [modelPricesQuery.data?.items],
  )

  const siteGroup = useMemo(() => {
    if (siteId === 'all') return null
    const group = buildSiteModelGroup(
      modelPricesQuery.data?.items ?? [],
      siteId,
    )
    if (group) return group
    return {
      siteId,
      siteName: sites.find((site) => site.id === siteId)?.name ?? siteId,
      siteType: '',
      rows: [],
      manualCount: 0,
      totalCount: 0,
    }
  }, [modelPricesQuery.data?.items, siteId, sites])

  const bulkMutation = useMutation({
    mutationFn: ({
      group,
      input,
    }: {
      group: ModelPriceGroup
      input: ModelPriceInput
    }) => {
      if (!group.canonicalModelId) {
        throw new Error(t('modelsPrice.notBoundToModel'))
      }
      return bulkUpdateModelPrices({
        canonical_model_id: group.canonicalModelId,
        site_model_ids: group.editableSiteModelIds,
        ...input,
      })
    },
    onMutate: ({ group }) => {
      setPendingBulkGroupId(group.id)
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: modelPriceQueryKeys.all,
      })
      await queryClient.invalidateQueries({
        queryKey: ['sites', 'all-pricings'],
      })
    },
    onError: (error) => {
      toast.error(t('modelsPrice.saveFailed'), { description: error.message })
    },
    onSettled: () => {
      setPendingBulkGroupId(null)
    },
  })

  const rowMutation = useMutation({
    mutationFn: ({
      row,
      input,
    }: {
      row: ModelPriceItem
      input: ModelPriceInput
    }) => updateSiteModelPricing(row.site_model_id, input),
    onMutate: ({ row }) => {
      setPendingSiteModelId(row.site_model_id)
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: modelPriceQueryKeys.all,
      })
      await queryClient.invalidateQueries({
        queryKey: ['sites', 'all-pricings'],
      })
    },
    onError: (error) => {
      toast.error(t('modelsPrice.saveFailed'), { description: error.message })
    },
    onSettled: () => {
      setPendingSiteModelId(null)
    },
  })

  const resetMutation = useMutation({
    mutationFn: (siteModelId: string) => resetSiteModelPricing(siteModelId),
    onMutate: (siteModelId) => {
      setPendingSiteModelId(siteModelId)
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: modelPriceQueryKeys.all,
      })
      await queryClient.invalidateQueries({
        queryKey: ['sites', 'all-pricings'],
      })
    },
    onError: (error) => {
      toast.error(t('modelsPrice.resetFailed'), { description: error.message })
    },
    onSettled: () => {
      setPendingSiteModelId(null)
    },
  })

  const resetSiteMutation = useMutation({
    mutationFn: (targetSiteId: string) => resetSitePricing(targetSiteId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: modelPriceQueryKeys.all,
      })
      await queryClient.invalidateQueries({
        queryKey: ['sites', 'all-pricings'],
      })
      toast.success(t('modelsPrice.resetAllSuccess'))
    },
    onError: (error) => {
      toast.error(t('modelsPrice.resetFailed'), { description: error.message })
    },
  })

  const renderFilterBar = () => (
    <ModelsPriceFilterBar
      search={search}
      billingType={billingType}
      pricingStatus={pricingStatus}
      siteId={siteId}
      sites={sites}
      onSearchChange={setSearch}
      onBillingTypeChange={(value) =>
        setBillingType(value as ModelPriceBillingType | 'all')
      }
      onPricingStatusChange={(value) => {
        setPricingStatus(value)
        setActiveTab(value === 'missing' ? 'missing' : 'all')
      }}
      onSiteIdChange={setSiteId}
    />
  )

  const renderTabsList = () => (
    <TabsList className="w-64">
      <TabsTrigger value="all">{t('modelsPrice.allPrices')}</TabsTrigger>
      <TabsTrigger value="missing">
        {t('modelsPrice.missingPrices')}
      </TabsTrigger>
    </TabsList>
  )

  if (isMobile) {
    return (
      <div className="relative space-y-4 pb-2">
        <PageHeader
          eyebrow={t('modelsPrice.eyebrow')}
          title={t('modelsPrice.title')}
          description={t('modelsPrice.description')}
        />

        <div className="sticky top-0 z-20 -mx-1">
          <div className="rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-2 shadow-[0_18px_38px_rgba(0,0,0,0.10)] backdrop-blur-[32px] backdrop-saturate-150">
            <label className="relative block">
              <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-muted-soft" />
              <Input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={t('modelsPrice.searchPlaceholder')}
                className="h-10 pl-10"
              />
            </label>

            <Tabs
              value={activeTab}
              onValueChange={(value) => {
                const nextTab = value as ModelsPriceTab
                setActiveTab(nextTab)
                setPricingStatus(nextTab === 'missing' ? 'missing' : 'all')
              }}
              variant="segmented"
              className="mt-2"
            >
              <TabsList className="w-full">
                <TabsTrigger value="all">
                  {t('modelsPrice.allPrices')}
                </TabsTrigger>
                <TabsTrigger value="missing">
                  {t('modelsPrice.missingPrices')}
                </TabsTrigger>
              </TabsList>
            </Tabs>

            <div className="mt-2 grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
              <div className="flex min-w-0 items-center gap-2 overflow-x-auto pb-0.5">
                <Select
                  value={billingType}
                  onValueChange={(value) =>
                    setBillingType(value as ModelsPriceBillingFilter)
                  }
                >
                  <SelectTrigger
                    variant="filter"
                    filterLabel={t('modelsPrice.headers.billing')}
                    active={billingType !== 'all'}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent searchable={false} widthMode="content">
                    <SelectItem value="all">
                      {t('modelsPrice.billingLabels.all')}
                    </SelectItem>
                    <SelectItem value="tokens">
                      {t('modelsPrice.billingLabels.tokens')}
                    </SelectItem>
                    <SelectItem value="per_request">
                      {t('modelsPrice.billingLabels.perRequest')}
                    </SelectItem>
                  </SelectContent>
                </Select>

                <Select
                  value={pricingStatus}
                  onValueChange={(value) => {
                    const nextStatus = value as ModelsPriceStatusFilter
                    setPricingStatus(nextStatus)
                    setActiveTab(nextStatus === 'missing' ? 'missing' : 'all')
                  }}
                >
                  <SelectTrigger
                    variant="filter"
                    filterLabel={t('modelsPrice.headers.status')}
                    active={pricingStatus !== 'all'}
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent searchable={false} widthMode="content">
                    <SelectItem value="all">
                      {t('modelsPrice.statusFilter.all')}
                    </SelectItem>
                    <SelectItem value="manual">
                      {t('modelsPrice.statusFilter.manual')}
                    </SelectItem>
                    <SelectItem value="synced">
                      {t('modelsPrice.statusFilter.synced')}
                    </SelectItem>
                    <SelectItem value="missing">
                      {t('modelsPrice.statusFilter.missing')}
                    </SelectItem>
                  </SelectContent>
                </Select>

                {sites.length > 0 ? (
                  <Select value={siteId} onValueChange={setSiteId}>
                    <SelectTrigger
                      variant="filter"
                      filterLabel={t('modelsPrice.siteFilter.label')}
                      active={siteId !== 'all'}
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent searchable={false} widthMode="content">
                      <SelectItem value="all">
                        {t('modelsPrice.siteFilter.all')}
                      </SelectItem>
                      {sites.map((site) => (
                        <SelectItem key={site.id} value={site.id}>
                          {site.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : null}
              </div>

              <Button
                className="h-10 max-w-[8.5rem] shrink-0 px-2 text-sm"
                onClick={() => mobileListRef.current?.saveAll()}
                disabled={
                  mobileSaveState.dirtyCount === 0 || mobileSaveState.saving
                }
              >
                {mobileSaveState.saving ? (
                  <LoaderCircle className="h-4 w-4 animate-spin" />
                ) : (
                  <Save className="h-4 w-4" />
                )}
                <span className="min-w-0 truncate">
                  {t('modelsPrice.saveChanges')}
                  {mobileSaveState.dirtyCount
                    ? ` ${mobileSaveState.dirtyCount}`
                    : ''}
                </span>
              </Button>
            </div>
          </div>
        </div>

        <MobileModelsPriceList
          ref={mobileListRef}
          groups={groups}
          siteGroup={siteGroup}
          loading={modelPricesQuery.isFetching}
          pendingBulkGroupId={pendingBulkGroupId}
          pendingSiteModelId={pendingSiteModelId}
          onSaveStateChange={handleMobileSaveStateChange}
          onBulkSave={async (group, input) => {
            await bulkMutation.mutateAsync({ group, input })
          }}
          onRowSave={async (row, input) => {
            await rowMutation.mutateAsync({ row, input })
          }}
          onReset={async (row) => {
            await resetMutation.mutateAsync(row.site_model_id)
          }}
          onResetSite={async (siteId) => {
            await resetSiteMutation.mutateAsync(siteId)
          }}
        />
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-7 overflow-hidden">
      <PageHeader
        eyebrow={t('modelsPrice.eyebrow')}
        title={t('modelsPrice.title')}
        description={t('modelsPrice.description')}
      />

      <Tabs
        value={activeTab}
        onValueChange={(value) => {
          const nextTab = value as ModelsPriceTab
          setActiveTab(nextTab)
          setPricingStatus(nextTab === 'missing' ? 'missing' : 'all')
        }}
        variant="segmented"
        className="flex min-h-0 flex-1 flex-col"
      >
        <TabsContent value="all" className="min-h-0 flex-1 overflow-hidden">
          <ModelsPriceTable
            groups={groups}
            loading={modelPricesQuery.isFetching}
            pendingBulkGroupId={pendingBulkGroupId}
            pendingSiteModelId={pendingSiteModelId}
            toolbarStart={renderTabsList()}
            toolbarControls={renderFilterBar()}
            onBulkSave={async (group, input) => {
              await bulkMutation.mutateAsync({ group, input })
            }}
            onRowSave={async (row, input) => {
              await rowMutation.mutateAsync({ row, input })
            }}
            onReset={async (row) => {
              await resetMutation.mutateAsync(row.site_model_id)
            }}
            siteGroup={siteGroup}
            onResetSite={async (targetSiteId) => {
              await resetSiteMutation.mutateAsync(targetSiteId)
            }}
          />
        </TabsContent>

        <TabsContent value="missing" className="min-h-0 flex-1 overflow-hidden">
          <ModelsPriceTable
            groups={groups}
            loading={modelPricesQuery.isFetching}
            pendingBulkGroupId={pendingBulkGroupId}
            pendingSiteModelId={pendingSiteModelId}
            toolbarStart={renderTabsList()}
            toolbarControls={renderFilterBar()}
            onBulkSave={async (group, input) => {
              await bulkMutation.mutateAsync({ group, input })
            }}
            onRowSave={async (row, input) => {
              await rowMutation.mutateAsync({ row, input })
            }}
            onReset={async (row) => {
              await resetMutation.mutateAsync(row.site_model_id)
            }}
            siteGroup={siteGroup}
            onResetSite={async (targetSiteId) => {
              await resetSiteMutation.mutateAsync(targetSiteId)
            }}
          />
        </TabsContent>
      </Tabs>
    </div>
  )
}
