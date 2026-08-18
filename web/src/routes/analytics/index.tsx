import { useMemo, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { PageHeader } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import type { MultiSelectOption } from '@/components/ui/multi-select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  analyticsQueryKeys,
  getAnalyticsAPIKeyContributions,
  getAnalyticsDataset,
  getAnalyticsOptions,
  getAnalyticsUsage,
  type AnalyticsGroupBy,
} from '@/features/analytics/api/analytics'
import { AnalyticsCostDonut } from '@/features/analytics/components/analytics-cost-donut'
import { AnalyticsFilterBar, type AnalyticsFiltersState } from '@/features/analytics/components/analytics-filter-bar'
import { AnalyticsKpiCards } from '@/features/analytics/components/analytics-kpi-cards'
import { AnalyticsTrendPanel } from '@/features/analytics/components/analytics-trend-panel'
import { AnalyticsUserRanking } from '@/features/analytics/components/analytics-user-ranking'
import { MobileAnalytics } from '@/features/analytics/components/mobile-analytics'
import {
  analyticsModelKeys,
  buildAnalyticsUsage,
  filterAnalyticsContributionPoints,
} from '@/features/analytics/lib/analytics-dataset'
import {
  defaultAnalyticsRange,
  type AnalyticsBreakdownDimension,
  type AnalyticsTrendDimension,
  type AnalyticsTrendMetric,
} from '@/features/analytics/lib/analytics-utils'
import { ApiKeyContributionsPanel } from '@/features/dashboard/components'
import type { DashboardOverview } from '@/features/dashboard/api/dashboard'
import {
  buildApiKeyContributions,
} from '@/features/dashboard/lib/dashboard-utils'
import { useMobileLayout } from '@/hooks/use-media-query'
import { modelNameIconInfo } from '@/features/sites/lib/model-icon'
import { APIError } from '@/lib/http'

export function AnalyticsPage() {
  const { t, i18n } = useTranslation('analytics')
  const isMobile = useMobileLayout()

  const [filters, setFilters] = useState<AnalyticsFiltersState>(() => ({
    preset: '30d',
    ...defaultAnalyticsRange(),
    siteIds: [],
    modelKeys: [],
    apiKeyIds: [],
    currency: '',
  }))
  const [trendMetric, setTrendMetric] = useState<AnalyticsTrendMetric>('cost')
  const [trendDimension, setTrendDimension] = useState<AnalyticsTrendDimension>('model')

  const datasetParams = useMemo(() => ({
    from: filters.from,
    to: filters.to,
    success: true as const,
  }), [filters.from, filters.to])

  const datasetQuery = useQuery({
    queryKey: analyticsQueryKeys.dataset(datasetParams),
    queryFn: () => getAnalyticsDataset(datasetParams),
    placeholderData: keepPreviousData,
    retry: (failureCount, error) => (
      !(error instanceof APIError && error.code === 'analytics_dataset_too_large')
      && failureCount < 2
    ),
  })

  const datasetTooLarge = datasetQuery.error instanceof APIError
    && datasetQuery.error.code === 'analytics_dataset_too_large'
  const legacyParams = useMemo(() => ({
    from: filters.from,
    to: filters.to,
    group_by: trendDimension as AnalyticsGroupBy,
    site_ids: filters.siteIds,
    model_keys: filters.modelKeys,
    api_key_ids: filters.apiKeyIds,
    success: true as const,
    currency: filters.currency || undefined,
    include_contributions: false,
  }), [filters, trendDimension])
  const legacyQuery = useQuery({
    queryKey: analyticsQueryKeys.usage(legacyParams),
    queryFn: () => getAnalyticsUsage(legacyParams),
    enabled: datasetTooLarge,
    placeholderData: keepPreviousData,
  })
  const optionsQuery = useQuery({
    queryKey: analyticsQueryKeys.options(),
    queryFn: getAnalyticsOptions,
    staleTime: 5 * 60_000,
  })
  const contributionsQuery = useQuery({
    queryKey: analyticsQueryKeys.contributions(),
    queryFn: getAnalyticsAPIKeyContributions,
    staleTime: 60_000,
  })

  const usage = useMemo(() => {
    if (datasetTooLarge) return legacyQuery.data
    if (datasetQuery.data) return buildAnalyticsUsage(datasetQuery.data, filters, trendDimension)
    return undefined
  }, [datasetQuery.data, datasetTooLarge, filters, legacyQuery.data, trendDimension])
  const availableCurrencies = usage?.meta.available_currencies ?? []

  const siteOptions = useMemo<MultiSelectOption[]>(() => (
    (optionsQuery.data?.sites ?? []).map((site) => ({ value: site.id, label: site.name }))
  ), [optionsQuery.data?.sites])
  const apiKeyOptions = useMemo<MultiSelectOption[]>(() => (
    (optionsQuery.data?.api_keys ?? []).map((key) => ({ value: key.id, label: key.name }))
  ), [optionsQuery.data?.api_keys])
  const datasetCurrent = datasetQuery.data?.current
  const displayCurrency = usage?.meta.currency ?? ''
  const datasetModelKeys = useMemo(() => (
    analyticsModelKeys(datasetCurrent ?? [], {
      siteIds: filters.siteIds,
      apiKeyIds: filters.apiKeyIds,
      currency: displayCurrency,
    })
  ), [datasetCurrent, displayCurrency, filters.apiKeyIds, filters.siteIds])
  const legacyModelKeys = useMemo(() => (
    (legacyQuery.data?.breakdowns.model ?? [])
      .map((item) => item.key)
      .filter((key) => key && key !== 'other' && key !== 'unknown')
      .sort((a, b) => a.localeCompare(b))
  ), [legacyQuery.data?.breakdowns.model])
  const [cachedLegacyModelKeys, setCachedLegacyModelKeys] = useState<string[]>([])
  const availableModelKeys = datasetTooLarge
    ? (filters.modelKeys.length > 0 && cachedLegacyModelKeys.length > 0 ? cachedLegacyModelKeys : legacyModelKeys)
    : datasetModelKeys
  const modelOptions = useMemo<MultiSelectOption[]>(() => (
    availableModelKeys.map((key) => {
      const info = modelNameIconInfo(key)
      return { value: key, label: key, icon: info.iconPath }
    })
  ), [availableModelKeys])

  const [selectedContributionKeyId, setSelectedContributionKeyId] = useState('')
  const contributionOverview = useMemo(() => {
    if (!contributionsQuery.data) return undefined
    return {
      meta: {
        today_start: `${contributionsQuery.data.to}T00:00:00`,
        range_start: `${contributionsQuery.data.from}T00:00:00`,
        range_end: `${contributionsQuery.data.to}T23:59:59`,
      },
      charts: {},
    } as unknown as DashboardOverview
  }, [contributionsQuery.data])
  const apiKeyContributions = useMemo(() => {
    if (!contributionsQuery.data || !contributionOverview) return undefined
    const points = filterAnalyticsContributionPoints(contributionsQuery.data.points, filters.apiKeyIds)
    return buildApiKeyContributions(
      points,
      contributionOverview,
      365,
      selectedContributionKeyId,
    )
  }, [contributionsQuery.data, contributionOverview, filters.apiKeyIds, selectedContributionKeyId])
  const effectiveContributionKeyId = apiKeyContributions?.selectedKey?.id ?? apiKeyContributions?.defaultKeyId ?? ''

  const isFetching = datasetQuery.isFetching || legacyQuery.isFetching || contributionsQuery.isFetching
  const handleRefresh = () => {
    const usageRefresh = datasetTooLarge ? legacyQuery.refetch() : datasetQuery.refetch()
    void Promise.all([usageRefresh, contributionsQuery.refetch()])
  }

  function handleFiltersChange(next: AnalyticsFiltersState) {
    if (datasetTooLarge && filters.modelKeys.length === 0 && next.modelKeys.length > 0 && legacyModelKeys.length > 0) {
      setCachedLegacyModelKeys(legacyModelKeys)
    }
    setFilters(next)
  }

  function handleDrillDown(dimension: AnalyticsBreakdownDimension, item: { key: string; id?: string | null }) {
    if (dimension === 'model' && datasetTooLarge && filters.modelKeys.length === 0 && legacyModelKeys.length > 0) {
      setCachedLegacyModelKeys(legacyModelKeys)
    }
    setFilters((current) => {
      if (dimension === 'model') {
        // unknown 模型条目下钻无意义，跳过
        if (item.key === 'unknown') return current
        const already = current.modelKeys.includes(item.key)
        return { ...current, modelKeys: already ? current.modelKeys : [...current.modelKeys, item.key] }
      }
      if (!item.id) return current
      if (dimension === 'site') {
        const already = current.siteIds.includes(item.id)
        return { ...current, siteIds: already ? current.siteIds : [...current.siteIds, item.id] }
      }
      const already = current.apiKeyIds.includes(item.id)
      return { ...current, apiKeyIds: already ? current.apiKeyIds : [...current.apiKeyIds, item.id] }
    })
  }

  function handleClearDrillDown(dimension: AnalyticsBreakdownDimension) {
    setFilters((current) => {
      if (dimension === 'model') return { ...current, modelKeys: [] }
      if (dimension === 'site') return { ...current, siteIds: [] }
      return { ...current, apiKeyIds: [] }
    })
  }

  if (datasetQuery.isLoading || (datasetTooLarge && legacyQuery.isLoading)) {
    return <AnalyticsSkeleton isMobile={isMobile} />
  }

  const usageError = datasetTooLarge ? legacyQuery.error : datasetQuery.error
  if (usageError) {
    return (
      <div className="flex flex-col gap-5">
        <PageHeader
          eyebrow={t('page.eyebrow')}
          title={t('page.title')}
          description={t('page.description')}
        />
        <ErrorState
          title={t('page.loadFailed')}
          description={usageError.message}
          action={<Button variant="outline" onClick={handleRefresh}>{t('page.retry')}</Button>}
        />
      </div>
    )
  }

  if (!usage) {
    return <EmptyState title={t('page.noData')} description={t('page.noDataDesc')} />
  }

  if (isMobile) {
    return (
      <MobileAnalytics
        usage={usage}
        filters={filters}
        onFiltersChange={handleFiltersChange}
        availableCurrencies={availableCurrencies}
        selectedCurrency={usage.meta.currency}
        siteOptions={siteOptions}
        modelOptions={modelOptions}
        apiKeyOptions={apiKeyOptions}
        optionsError={optionsQuery.error?.message}
        onRetryOptions={() => optionsQuery.refetch()}
        trendMetric={trendMetric}
        trendDimension={trendDimension}
        onTrendMetricChange={setTrendMetric}
        onTrendDimensionChange={setTrendDimension}
        apiKeyContributions={apiKeyContributions}
        contributionsError={contributionsQuery.error?.message}
        onRetryContributions={() => contributionsQuery.refetch()}
        contributionKeyId={effectiveContributionKeyId}
        onContributionKeyChange={setSelectedContributionKeyId}
        onDrillDown={handleDrillDown}
        onClearDrillDown={handleClearDrillDown}
        isFetching={isFetching}
        onRefresh={handleRefresh}
        language={i18n.language}
      />
    )
  }

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        eyebrow={t('page.eyebrow')}
        title={t('page.title')}
        description={t('page.description')}
      />

      <AnalyticsFilterBar
        filters={filters}
        availableCurrencies={availableCurrencies}
        selectedCurrency={usage.meta.currency}
        siteOptions={siteOptions}
        modelOptions={modelOptions}
        apiKeyOptions={apiKeyOptions}
        optionsError={optionsQuery.error?.message}
        onRetryOptions={() => optionsQuery.refetch()}
        onChange={handleFiltersChange}
        dataFrom={usage.meta.data_from}
        updatedAt={usage.meta.generated_at}
        language={i18n.language}
        isFetching={isFetching}
        onRefresh={handleRefresh}
      />

      <AnalyticsKpiCards usage={usage} />

      <div className="grid gap-4 xl:grid-cols-4">
        <AnalyticsTrendPanel
          className="h-[420px] xl:col-span-3"
          usage={usage}
          metric={trendMetric}
          dimension={trendDimension}
          onMetricChange={setTrendMetric}
          onDimensionChange={setTrendDimension}
        />
        <AnalyticsUserRanking className="h-[420px]" usage={usage} metric={trendMetric} />
      </div>

      <div className="grid gap-4 xl:grid-cols-4">
        <AnalyticsCostDonut
          className="max-h-[520px] xl:col-span-2 xl:h-[360px] xl:max-h-none"
          usage={usage}
          onDrillDown={handleDrillDown}
          onClearDrillDown={handleClearDrillDown}
          activeSiteIds={filters.siteIds}
          activeModelKeys={filters.modelKeys}
          activeApiKeyIds={filters.apiKeyIds}
        />
        {contributionsQuery.isError ? <div className="xl:col-span-2"><ErrorState
            title={t('page.contributionsLoadFailed')}
            description={contributionsQuery.error.message}
            action={<Button variant="outline" onClick={() => contributionsQuery.refetch()}>{t('page.retry')}</Button>}
          /></div> : apiKeyContributions ? <ApiKeyContributionsPanel
            className="h-[360px] xl:col-span-2"
            contributions={apiKeyContributions}
            selectedKeyId={effectiveContributionKeyId}
            onSelectedKeyChange={setSelectedContributionKeyId}
          /> : <Card className="h-[360px] p-5 xl:col-span-2"><Skeleton className="h-full w-full" /></Card>}
      </div>
    </div>
  )
}

function AnalyticsSkeleton({ isMobile }: { isMobile: boolean }) {
  if (isMobile) return <MobileAnalyticsSkeleton />

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Skeleton className="h-3 w-10" />
          <div className="space-y-1.5">
            <Skeleton className="h-9 w-32" />
            <Skeleton className="h-4 w-72 max-w-full" />
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Skeleton className="h-10 w-72 rounded-lg" />
        <Skeleton className="h-10 w-36 rounded-lg" />
        <Skeleton className="h-10 w-28 rounded-lg" />
        <Skeleton className="h-10 w-28 rounded-lg" />
        <Skeleton className="h-10 w-28 rounded-lg" />
        <div className="ml-auto flex items-center gap-2">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-9 w-20 rounded-lg" />
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }).map((_, index) => (
          <Card key={index} className="flex min-h-[120px] flex-col justify-between gap-3 rounded-lg p-4">
            <div className="flex items-center gap-2">
              <Skeleton className="size-4 rounded" />
              <Skeleton className="h-4 w-20" />
            </div>
            <Skeleton className="h-8 w-24" />
            <Skeleton className="h-4 w-32" />
          </Card>
        ))}
      </div>

      <div className="grid gap-4 xl:grid-cols-4">
        <Card className="h-[420px] rounded-lg p-5 xl:col-span-3">
          <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
            <div className="space-y-2">
              <Skeleton className="h-5 w-64 max-w-full" />
              <Skeleton className="h-4 w-52 max-w-full" />
            </div>
            <Skeleton className="h-5 w-72 max-w-full" />
          </div>
          <Skeleton className="h-[310px] rounded-lg" />
        </Card>

        <Card className="h-[420px] rounded-lg p-5">
          <div className="mb-4 space-y-2">
            <Skeleton className="h-5 w-32" />
            <Skeleton className="h-4 w-40" />
          </div>
          <div className="space-y-4">
            {Array.from({ length: 6 }).map((_, index) => (
              <div key={index} className="space-y-1.5">
                <div className="flex items-center justify-between gap-3">
                  <Skeleton className="h-4 w-28" />
                  <Skeleton className="h-3 w-20" />
                </div>
                <Skeleton className="h-1.5 w-full rounded-full" />
              </div>
            ))}
          </div>
        </Card>
      </div>

      <div className="grid gap-4 xl:grid-cols-4">
        <Card className="h-[260px] max-h-[520px] rounded-lg p-5 xl:col-span-2 xl:h-[360px] xl:max-h-none">
          <div className="mb-4 flex items-start justify-between gap-3">
            <div className="space-y-2">
              <Skeleton className="h-5 w-24" />
              <Skeleton className="h-4 w-32" />
            </div>
            <Skeleton className="h-5 w-32" />
          </div>
          <div className="flex min-h-0 items-center gap-6">
            <Skeleton className="aspect-square w-[clamp(160px,30%,240px)] shrink-0 rounded-full" />
            <div className="min-w-0 flex-1 divide-y divide-[hsl(var(--glass-divider))]">
              {Array.from({ length: 5 }).map((_, index) => (
                <div key={index} className="flex items-center gap-2 py-2 first:pt-0 last:pb-0">
                  <Skeleton className="size-2 shrink-0 rounded-sm" />
                  <Skeleton className="h-4 min-w-0 flex-1" />
                  <Skeleton className="h-4 w-24 shrink-0" />
                </div>
              ))}
            </div>
          </div>
        </Card>

        <Card className="h-[360px] rounded-lg p-5 xl:col-span-2">
          <div className="mb-4 flex items-start justify-between gap-3">
            <div className="space-y-2">
              <Skeleton className="h-5 w-36" />
              <Skeleton className="h-4 w-48" />
            </div>
            <Skeleton className="h-9 w-28 rounded-lg" />
          </div>
          <Skeleton className="h-[260px] rounded-lg" />
        </Card>
      </div>
    </div>
  )
}

function MobileAnalyticsSkeleton() {
  return (
    <div className="space-y-4 pb-2">
      <section className="space-y-1.5">
        <Skeleton className="h-3 w-10" />
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
          <Skeleton className="h-9 w-32" />
          <Skeleton className="h-9 w-20 rounded-lg" />
        </div>
        <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1.5">
          <div className="space-y-1.5">
            <Skeleton className="h-4 w-64 max-w-full" />
            <Skeleton className="h-4 w-48 max-w-full" />
          </div>
          <Skeleton className="mt-1 h-3 w-24 shrink-0" />
        </div>
      </section>

      <div className="flex items-center gap-2 overflow-hidden pb-1">
        <Skeleton className="h-10 w-28 shrink-0 rounded-lg" />
        <Skeleton className="h-10 w-24 shrink-0 rounded-lg" />
        <Skeleton className="h-10 w-24 shrink-0 rounded-lg" />
        <Skeleton className="h-10 w-24 shrink-0 rounded-lg" />
      </div>

      <div className="grid gap-3">
        {Array.from({ length: 4 }).map((_, index) => (
          <Card key={index} className="h-[117px] rounded-2xl p-4">
            <div className="mb-3 flex items-center gap-2">
              <Skeleton className="size-4 rounded" />
              <Skeleton className="h-4 w-20" />
            </div>
            <Skeleton className="h-8 w-28" />
            <Skeleton className="mt-2 h-4 w-36" />
          </Card>
        ))}
      </div>

      <Card className="rounded-2xl p-4">
        <div className="mb-3 space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
            <Skeleton className="h-5 w-48" />
            <Skeleton className="h-5 w-24" />
          </div>
          <Skeleton className="h-3 w-56 max-w-full" />
        </div>
        <Skeleton className="h-[240px] rounded-lg" />
      </Card>

      <Card className="h-[320px] rounded-2xl p-5">
        <div className="mb-4 space-y-2">
          <Skeleton className="h-5 w-36" />
          <Skeleton className="h-4 w-40" />
        </div>
        <div className="space-y-4">
          {Array.from({ length: 5 }).map((_, index) => (
            <div key={index} className="space-y-1.5">
              <div className="flex items-center justify-between gap-3">
                <Skeleton className="h-4 w-32" />
                <Skeleton className="h-3 w-20" />
              </div>
              <Skeleton className="h-1.5 w-full rounded-full" />
            </div>
          ))}
        </div>
      </Card>

      <Card className="rounded-2xl p-4">
        <div className="mb-3 flex items-start justify-between gap-3">
          <div className="space-y-2">
            <Skeleton className="h-5 w-24" />
            <Skeleton className="h-3 w-32" />
          </div>
          <Skeleton className="h-5 w-28" />
        </div>
        <Skeleton className="mx-auto size-[150px] rounded-full" />
        <div className="mt-3 divide-y divide-[hsl(var(--glass-divider))]">
          {Array.from({ length: 4 }).map((_, index) => (
            <div key={index} className="flex items-center gap-1.5 py-2 first:pt-0 last:pb-0">
              <Skeleton className="size-2 shrink-0 rounded-sm" />
              <Skeleton className="h-4 min-w-0 flex-1" />
              <Skeleton className="h-3 w-24 shrink-0" />
            </div>
          ))}
        </div>
      </Card>

      <Card className="h-[400px] rounded-2xl p-5">
        <div className="mb-4 flex items-start justify-between gap-3">
          <div className="space-y-2">
            <Skeleton className="h-5 w-36" />
            <Skeleton className="h-4 w-48 max-w-full" />
          </div>
          <Skeleton className="h-9 w-24 rounded-lg" />
        </div>
        <Skeleton className="h-[300px] rounded-lg" />
      </Card>
    </div>
  )
}
