import { useMemo, useRef, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { PageHeader } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import {
  analyticsQueryKeys,
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
  defaultAnalyticsRange,
  formatDateInput,
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

  const params = useMemo(() => ({
    from: filters.from,
    to: filters.to,
    group_by: trendDimension as AnalyticsGroupBy,
    site_ids: filters.siteIds,
    model_keys: filters.modelKeys,
    api_key_ids: filters.apiKeyIds,
    // 始终只看成功请求
    success: true as const,
    currency: filters.currency || undefined,
  }), [filters, trendDimension])

  const usageQuery = useQuery({
    queryKey: analyticsQueryKeys.usage(params),
    queryFn: () => getAnalyticsUsage(params),
    placeholderData: keepPreviousData,
  })

  const usage = usageQuery.data
  const availableCurrencies = usage?.meta.available_currencies ?? []

  // 热力图数据：使用 analytics 接口自带的 api_key_contributions
  const [selectedContributionKeyId, setSelectedContributionKeyId] = useState('')
  const contributionOverview = useMemo(() => {
    if (!usage) return undefined
    const today = new Date()
    const todayStr = formatDateInput(today)
    return {
      meta: {
        today_start: `${todayStr}T00:00:00`,
        range_start: `${usage.meta.from}T00:00:00`,
        range_end: `${usage.meta.to}T23:59:59`,
      },
      charts: {},
    } as unknown as DashboardOverview
  }, [usage])
  const apiKeyContributions = useMemo(() => {
    if (!usage || !contributionOverview) return undefined
    return buildApiKeyContributions(
      usage.api_key_contributions ?? [],
      contributionOverview,
      365,
      selectedContributionKeyId,
    )
  }, [usage, contributionOverview, selectedContributionKeyId])
  const effectiveContributionKeyId = apiKeyContributions?.selectedKey?.id ?? apiKeyContributions?.defaultKeyId ?? ''

  // 从 breakdowns.model 提取有数据的模型 key，给 filter bar 用
  // 用 useRef 缓存：只有在无模型筛选时才更新选项列表，防止选了某模型后其他选项消失
  const cachedModelKeysRef = useRef<string[]>([])
  const availableModelKeys = useMemo(() => {
    // 有模型筛选时不更新缓存，直接返回缓存（确保已选项和其他选项都还在）
    // eslint-disable-next-line react-hooks/refs -- intentional render-time read: ref acts as a stale-while-filtered cache
    if (filters.modelKeys.length > 0) return cachedModelKeysRef.current
    const keys = new Set<string>()
    for (const item of usage?.breakdowns.model ?? []) {
      if (item.key && item.key !== 'other' && item.key !== 'unknown') keys.add(item.key)
    }
    const sorted = [...keys].sort((a, b) => a.localeCompare(b))
    // eslint-disable-next-line react-hooks/refs -- intentional render-time write: ref acts as a stale-while-filtered cache
    if (sorted.length > 0) cachedModelKeysRef.current = sorted
    return cachedModelKeysRef.current
  }, [usage, filters.modelKeys])

  function handleDrillDown(dimension: AnalyticsBreakdownDimension, item: { key: string; id?: string | null }) {
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

  if (usageQuery.isLoading) {
    return <AnalyticsSkeleton isMobile={isMobile} />
  }

  if (usageQuery.isError) {
    return (
      <div className="flex flex-col gap-5">
        <PageHeader
          eyebrow={t('page.eyebrow')}
          title={t('page.title')}
          description={t('page.description')}
        />
        <ErrorState
          title={t('page.loadFailed')}
          description={usageQuery.error.message}
          action={<Button variant="outline" onClick={() => usageQuery.refetch()}>{t('page.retry')}</Button>}
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
        onFiltersChange={setFilters}
        availableCurrencies={availableCurrencies}
        availableModelKeys={availableModelKeys}
        trendMetric={trendMetric}
        trendDimension={trendDimension}
        onTrendMetricChange={setTrendMetric}
        onTrendDimensionChange={setTrendDimension}
        apiKeyContributions={apiKeyContributions}
        contributionKeyId={effectiveContributionKeyId}
        onContributionKeyChange={setSelectedContributionKeyId}
        onDrillDown={handleDrillDown}
        onClearDrillDown={handleClearDrillDown}
        isFetching={usageQuery.isFetching}
        onRefresh={() => usageQuery.refetch()}
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
        availableModelKeys={availableModelKeys}
        onChange={setFilters}
        dataFrom={usage.meta.data_from}
        updatedAt={usage.meta.generated_at}
        language={i18n.language}
        isFetching={usageQuery.isFetching}
        onRefresh={() => usageQuery.refetch()}
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
        <ApiKeyContributionsPanel
            className="h-[360px] xl:col-span-2"
            contributions={apiKeyContributions!}
            selectedKeyId={effectiveContributionKeyId}
            onSelectedKeyChange={setSelectedContributionKeyId}
          />
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
