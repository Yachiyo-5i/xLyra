import { useMemo, useState, type ReactNode } from 'react'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Activity, CalendarDays, Gauge, Hash, RefreshCw, Sigma, Timer, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { EmptyState } from '@/components/common/empty-state'
import { ErrorState } from '@/components/common/error-state'
import { PageHeader } from '@/components/common/page-header'
import type { TokenUsageColumn, TokenUsageLabels } from '@/components/common/token-usage-hover-card'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  ApiKeyContributionsPanel,
  DashboardChartPanel,
  DashboardCooldownList,
  DashboardMetricCard,
  DashboardRiskList,
  ModelCostChart,
  ModelRequestsChart,
  SiteCostChart,
  SiteUptimeStrip,
  SystemResourcePanel,
} from '@/features/dashboard/components'
import { MobileDashboard } from '@/features/dashboard/components/mobile-dashboard'
import {
  dashboardQueryKeys,
  getDashboardCooldowns,
  getDashboardHealth,
  getDashboardInsights,
  getDashboardUsage,
  type DashboardDays,
  type DashboardOverview,
} from '@/features/dashboard/api/dashboard'
import {
  buildCooldownItems,
  buildAttentionRiskItems,
  buildApiKeyContributions,
  buildRangedDailyModelCost,
  buildRangedDailyModelRequests,
  buildRangedDailySiteCost,
  buildRangedDailySiteRequests,
  buildSiteCosts,
  buildUptimeRows,
  dashboardDaysToRange,
  dashboardRangeToDays,
  formatCompactNumber,
  formatDashboardCurrency,
  formatDashboardRefreshTime,
  formatLimitValue,
  formatPercent,
  selectedSiteCosts,
} from '@/features/dashboard/lib/dashboard-utils'
import { clearRouteCooldown, routeQueryKeys } from '@/features/routes/api/routes'
import { useMobileLayout } from '@/hooks/use-media-query'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'

const CONTRIBUTION_DAYS = 365

export function DashboardPage() {
  const { t, i18n } = useTranslation('dashboard')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const isMobile = useMobileLayout()
  const [days, setDays] = useState<DashboardDays>(30)
  const [trendMetric, setTrendMetric] = useState<DashboardTrendMetric>('cost')
  const [trendScope, setTrendScope] = useState<DashboardTrendScope>('model')
  const [selectedContributionKeyId, setSelectedContributionKeyId] = useState('')

  const usageQuery = useQuery({
    queryKey: dashboardQueryKeys.usage(),
    queryFn: getDashboardUsage,
    placeholderData: keepPreviousData,
  })
  const cooldownsQuery = useQuery({
    queryKey: dashboardQueryKeys.cooldowns(),
    queryFn: getDashboardCooldowns,
    placeholderData: keepPreviousData,
  })
  const healthQuery = useQuery({
    queryKey: dashboardQueryKeys.health(),
    queryFn: getDashboardHealth,
    placeholderData: keepPreviousData,
  })
  const insightsQuery = useQuery({
    queryKey: dashboardQueryKeys.insights(),
    queryFn: getDashboardInsights,
    placeholderData: keepPreviousData,
  })

  const overview = useMemo<DashboardOverview | undefined>(() => {
    const usage = usageQuery.data
    if (!usage) return undefined
    return {
      ...usage,
      cooldowns: cooldownsQuery.data ?? { items: [] },
      health: healthQuery.data ?? { uptime_rows: [] },
      insights: insightsQuery.data?.insights ?? {
        failure_reasons: [],
        insufficient_candidates: [],
        high_latency: [],
      },
      attention: insightsQuery.data?.attention ?? { items: [] },
    }
  }, [cooldownsQuery.data, healthQuery.data, insightsQuery.data, usageQuery.data])
  const dashboardFetching = usageQuery.isFetching || cooldownsQuery.isFetching || healthQuery.isFetching || insightsQuery.isFetching
  const refetchDashboard = () => {
    void Promise.all([
      usageQuery.refetch(),
      cooldownsQuery.refetch(),
      healthQuery.refetch(),
      insightsQuery.refetch(),
    ])
  }
  const modelCost = useMemo(
    () => overview ? buildRangedDailyModelCost(overview.charts.daily_model_cost, overview, days) : { data: [], series: [] },
    [days, overview],
  )
  const modelRequests = useMemo(
    () => overview ? buildRangedDailyModelRequests(overview.charts.daily_model_requests, overview, days) : { data: [], series: [] },
    [days, overview],
  )
  const siteCostTrend = useMemo(
    () => overview ? buildRangedDailySiteCost(overview.charts.daily_site_cost, overview, days) : { data: [], series: [] },
    [days, overview],
  )
  const siteRequestsTrend = useMemo(
    () => overview ? buildRangedDailySiteRequests(overview.charts.daily_site_requests, overview, days) : { data: [], series: [] },
    [days, overview],
  )
  const siteCosts = useMemo(
    () => overview ? buildSiteCosts(selectedSiteCosts(overview, days)) : [],
    [days, overview],
  )
  const apiKeyContributions = useMemo(
    () => overview ? buildApiKeyContributions(overview.charts.api_key_contributions ?? [], overview, CONTRIBUTION_DAYS, selectedContributionKeyId) : undefined,
    [overview, selectedContributionKeyId],
  )
  const uptimeRows = useMemo(
    () => overview ? buildUptimeRows(overview.health.uptime_rows) : [],
    [overview],
  )
  const cooldownItems = useMemo(
    () => overview ? buildCooldownItems(overview.cooldowns.items, t) : [],
    [overview, t],
  )
  const riskItems = useMemo(
    () => overview ? buildAttentionRiskItems(overview, t) : [],
    [overview, t],
  )
  const effectiveContributionKeyId = apiKeyContributions?.selectedKey?.id ?? apiKeyContributions?.defaultKeyId ?? ''

  const clearCooldownMutation = useMutation({
    mutationFn: async (item: typeof cooldownItems[number]) => {
      if (!item.siteId) throw new Error(t('page.clearMissingSite'))
      return clearRouteCooldown({
        siteId: item.siteId,
        siteModelId: item.siteModelId,
        siteCredentialId: item.siteCredentialId,
        source: item.source,
      })
    },
    onSuccess: async (_, item) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.cooldowns() }),
        queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.insights() }),
        queryClient.invalidateQueries({ queryKey: routeQueryKeys.cooldowns() }),
      ])
      toast.success(t('page.clearSuccess'), { description: item.name })
    },
    onError: (error) => {
      toast.error(t('page.clearFailed'), { description: error.message })
    },
  })

  const clearAllCooldownsMutation = useMutation({
    mutationFn: async () => {
      await Promise.all(cooldownItems.map((item) => {
        if (!item.siteId) return Promise.resolve()
        return clearRouteCooldown({
          siteId: item.siteId,
          siteModelId: item.siteModelId,
          siteCredentialId: item.siteCredentialId,
          source: item.source,
        })
      }))
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.cooldowns() }),
        queryClient.invalidateQueries({ queryKey: dashboardQueryKeys.insights() }),
        queryClient.invalidateQueries({ queryKey: routeQueryKeys.cooldowns() }),
      ])
      toast.success(t('page.clearAllSuccess'))
    },
    onError: (error) => {
      toast.error(t('page.clearFailed'), { description: error.message })
    },
  })

  if (usageQuery.isLoading) {
    return <DashboardSkeleton />
  }

  if (usageQuery.isError) {
    return (
      <ErrorState
        title={t('page.loadFailed')}
        description={usageQuery.error.message}
        action={<Button variant="outline" onClick={() => usageQuery.refetch()}>{t('page.retry')}</Button>}
      />
    )
  }

  if (!overview) {
    return <EmptyState title={t('page.noData')} description={t('page.noDataDesc')} />
  }

  const costCurrency = overview.kpis.cost.currency || 'USD'
  const costYesterday = formatDashboardCurrency(overview.kpis.cost.yesterday, costCurrency)
  const requestsToday = formatCompactNumber(overview.kpis.requests.today)
  const requestsTotal = formatCompactNumber(overview.kpis.requests.total)
  const requestsYesterday = formatCompactNumber(overview.kpis.requests.yesterday)
  const tokensToday = formatCompactNumber(overview.kpis.requests.today_tokens)
  const tokensTotal = formatCompactNumber(overview.kpis.requests.total_tokens)
  const tokensYesterday = formatCompactNumber(overview.kpis.requests.yesterday_tokens)
  const tokenUsageLabels: TokenUsageLabels = {
    total: t('tokens.total'),
    input: t('tokens.input'),
    output: t('tokens.output'),
    cached: t('tokens.cached'),
    hitRate: t('tokens.hitRate'),
  }
  const tokenUsage: TokenUsageColumn[] = [
    {
      label: t('kpis.today'),
      usage: {
        total: overview.kpis.requests.today_tokens,
        input: overview.kpis.requests.today_prompt_tokens,
        output: overview.kpis.requests.today_completion_tokens,
        cached: overview.kpis.requests.today_cached_tokens,
      },
    },
    {
      label: t('kpis.total'),
      usage: {
        total: overview.kpis.requests.total_tokens,
        input: overview.kpis.requests.total_prompt_tokens,
        output: overview.kpis.requests.total_completion_tokens,
        cached: overview.kpis.requests.total_cached_tokens,
      },
    },
  ]
  const successRate = formatPercent(overview.kpis.requests.success_rate)
  const requestsNote = `${t('kpis.successRate')} ${successRate} · ${t('kpis.yesterdayRequests')} ${requestsYesterday} · ${t('kpis.yesterdayTokens')} ${tokensYesterday}`

  function handleRiskClick(item: typeof riskItems[number]) {
    if (item.actionPath) {
      navigate(item.actionPath)
      return
    }
    if (item.id.startsWith('route:')) {
      navigate('/routes')
      return
    }
    navigate('/requests')
  }

  if (isMobile) {
    return (
      <MobileDashboard
        generatedAt={overview.meta.generated_at}
        locale={i18n.language}
        isFetching={dashboardFetching}
        costToday={formatDashboardCurrency(overview.kpis.cost.today, costCurrency)}
        costTotal={formatDashboardCurrency(overview.kpis.cost.total, costCurrency)}
        costYesterday={costYesterday}
        requestsToday={requestsToday}
        requestsTotal={requestsTotal}
        tokensToday={tokensToday}
        tokensTotal={tokensTotal}
        requestsYesterday={requestsYesterday}
        tokensYesterday={tokensYesterday}
        tokenUsage={tokenUsage}
        tokenUsageLabels={tokenUsageLabels}
        successRate={successRate}
        rpm={formatLimitValue(overview.kpis.rate_limit.rpm.used, overview.kpis.rate_limit.rpm.limit)}
        tpm={formatLimitValue(overview.kpis.rate_limit.tpm.used, overview.kpis.rate_limit.tpm.limit)}
        completedRpm={overview.kpis.rate_limit.completed_rpm ?? 0}
        trendMetric={trendMetric}
        trendScope={trendScope}
        days={days}
        modelCost={modelCost}
        modelRequests={modelRequests}
        siteCostTrend={siteCostTrend}
        siteRequestsTrend={siteRequestsTrend}
        siteCosts={siteCosts}
        apiKeyContributions={apiKeyContributions}
        selectedContributionKeyId={effectiveContributionKeyId}
        riskItems={riskItems}
        cooldownItems={cooldownItems}
        uptimeRows={uptimeRows}
        cooldownsFetching={cooldownsQuery.isFetching}
        clearAllCooldownsPending={clearAllCooldownsMutation.isPending}
        onRefresh={refetchDashboard}
        onRefreshCooldowns={() => cooldownsQuery.refetch()}
        onTrendMetricChange={setTrendMetric}
        onTrendScopeChange={setTrendScope}
        onDaysChange={setDays}
        onContributionKeyChange={setSelectedContributionKeyId}
        onRiskClick={handleRiskClick}
        onClearCooldown={(item) => clearCooldownMutation.mutate(item)}
        onClearAllCooldowns={() => clearAllCooldownsMutation.mutate()}
        formatRefreshTime={formatDashboardRefreshTime}
      />
    )
  }

  const costTrend = trendScope === 'model' ? modelCost : siteCostTrend
  const requestsTrend = trendScope === 'model' ? modelRequests : siteRequestsTrend

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        eyebrow={t('page.eyebrow')}
        title={t('page.title')}
        description={t('page.description')}
        actions={
          <div className="flex items-center gap-3">
            <span className="text-xs text-muted-soft">
              {t('page.update')} {formatDashboardRefreshTime(overview.meta.generated_at, i18n.language)}
            </span>
            <Button
              variant="secondary"
              onClick={refetchDashboard}
              disabled={dashboardFetching}
            >
              <RefreshCw className={dashboardFetching ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} />
              {t('page.refresh')}
            </Button>
          </div>
        }
      />

      <div className="space-y-4">
        <div className="grid gap-4 lg:grid-cols-3">
          <DashboardMetricCard
            title={t('kpis.cost')}
            primaryLabel={t('kpis.today')}
            primaryValue={formatDashboardCurrency(overview.kpis.cost.today, costCurrency)}
            secondaryLabel={t('kpis.total')}
            secondaryValue={formatDashboardCurrency(overview.kpis.cost.total, costCurrency)}
            accent="cost"
            icon={<WalletCards />}
            primaryIcon={<CalendarDays />}
            secondaryIcon={<Sigma />}
            note={`${t('kpis.yesterdayCost')} ${costYesterday}`}
          />
          <DashboardMetricCard
            title={t('kpis.requestsTokens')}
            primaryLabel={t('kpis.todayTotalRequests')}
            primaryValue={`${requestsToday} / ${requestsTotal}`}
            secondaryLabel={t('kpis.todayTotalTokens')}
            secondaryValue={`${tokensToday} / ${tokensTotal}`}
            accent="traffic"
            icon={<Activity />}
            primaryIcon={<Hash />}
            secondaryIcon={<Sigma />}
            secondaryTokenUsage={tokenUsage}
            tokenUsageLabels={tokenUsageLabels}
            note={requestsNote}
          />
          <DashboardMetricCard
            title={t('kpis.performance')}
            primaryLabel="RPM"
            primaryValue={formatLimitValue(overview.kpis.rate_limit.rpm.used, overview.kpis.rate_limit.rpm.limit)}
            secondaryLabel="TPM"
            secondaryValue={formatLimitValue(overview.kpis.rate_limit.tpm.used, overview.kpis.rate_limit.tpm.limit)}
            accent="performance"
            icon={<Gauge />}
            primaryIcon={<Activity />}
            secondaryIcon={<Timer />}
            note={`${t('kpis.completedRpm')} ${overview.kpis.rate_limit.completed_rpm ?? 0}`}
          />
        </div>

        <div className="grid auto-rows-[360px] gap-4 2xl:grid-cols-3">
          <DashboardChartPanel
            className="h-full 2xl:col-span-2"
            title={(
              <DashboardSlashTabs
                value={trendMetric}
                onValueChange={setTrendMetric}
                items={orderedTrendMetricItems(trendScope, trendMetric, t)}
              />
            )}
            description={t('charts.trend', { days })}
            action={
              <div className="flex items-center gap-5">
                <DashboardRangeTabs days={days} onDaysChange={setDays} t={t} />
                <div className="h-4 w-px bg-[hsl(var(--glass-divider))]" />
                <DashboardSlashTabs
                  value={trendScope}
                  onValueChange={setTrendScope}
                  items={[
                    { label: t('charts.model'), value: 'model' },
                    { label: t('charts.site'), value: 'site' },
                  ]}
                />
              </div>
            }
          >
            {trendMetric === 'cost' ? (
              <ModelCostChart data={costTrend.data} series={costTrend.series} height={250} />
            ) : (
              <ModelRequestsChart data={requestsTrend.data} series={requestsTrend.series} height={250} />
            )}
          </DashboardChartPanel>
          <DashboardRiskList
            className="h-full"
            items={riskItems}
            onItemClick={handleRiskClick}
          />
          <DashboardChartPanel className="h-full" title={t('charts.siteCost')} description={t('charts.summary', { days })}>
            <SiteCostChart data={siteCosts} height={250} />
          </DashboardChartPanel>
          {apiKeyContributions ? (
            <ApiKeyContributionsPanel
              className="h-full"
              contributions={apiKeyContributions}
              selectedKeyId={effectiveContributionKeyId}
              onSelectedKeyChange={setSelectedContributionKeyId}
            />
          ) : null}
          <DashboardCooldownList
            className="h-full"
            items={cooldownItems}
            action={
              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-soft hover:text-foreground"
                  disabled={cooldownsQuery.isFetching}
                  onClick={() => cooldownsQuery.refetch()}
                >
                  <RefreshCw className={cooldownsQuery.isFetching ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} />
                  {t('page.refreshCooldowns')}
                </Button>
                {cooldownItems.length ? (
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-8 px-2 text-muted-soft hover:text-foreground"
                  disabled={clearAllCooldownsMutation.isPending}
                  onClick={() => clearAllCooldownsMutation.mutate()}
                >
                  {t('page.clearAll')}
                </Button>
                ) : null}
              </div>
            }
            onClearItem={(item) => clearCooldownMutation.mutate(item)}
          />
        </div>

        <div className="grid gap-4 xl:grid-cols-2">
          <SiteUptimeStrip className="h-[320px]" sites={uptimeRows} />
          <SystemResourcePanel className="h-[320px]" />
        </div>
      </div>
    </div>
  )
}

type DashboardTrendMetric = 'cost' | 'requests'
type DashboardTrendScope = 'model' | 'site'

function orderedTrendMetricItems(
  scope: DashboardTrendScope,
  value: DashboardTrendMetric,
  t: (key: string) => string,
) {
  const items: Array<{ label: string; value: DashboardTrendMetric }> = scope === 'model'
    ? [
        { label: t('charts.modelCost'), value: 'cost' },
        { label: t('charts.modelRequests'), value: 'requests' },
      ]
    : [
        { label: t('charts.siteCost'), value: 'cost' },
        { label: t('charts.siteRequests'), value: 'requests' },
      ]

  return [
    ...items.filter((item) => item.value === value),
    ...items.filter((item) => item.value !== value),
  ]
}

function DashboardSlashTabs<T extends string>({
  value,
  items,
  onValueChange,
}: {
  value: T
  items: Array<{ label: string; value: T }>
  onValueChange: (value: T) => void
}) {
  return (
    <Tabs variant="slash" value={value} onValueChange={(next) => onValueChange(next as T)}>
      <TabsList>
        {items.map((item) => (
          <TabsTrigger key={item.value} value={item.value} className="text-sm">
            {item.label}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}

function DashboardRangeTabs({
  days,
  onDaysChange,
  t,
}: {
  days: DashboardDays
  onDaysChange: (days: DashboardDays) => void
  t: (key: string) => string
}) {
  return (
    <Tabs
      variant="slash"
      value={dashboardDaysToRange(days)}
      onValueChange={(value) => onDaysChange(dashboardRangeToDays(value))}
    >
      <TabsList>
        <TabsTrigger value="7d" className="text-sm">{t('ranges.7d')}</TabsTrigger>
        <TabsTrigger value="30d" className="text-sm">{t('ranges.30d')}</TabsTrigger>
        <TabsTrigger value="90d" className="text-sm">{t('ranges.90d')}</TabsTrigger>
      </TabsList>
    </Tabs>
  )
}

function DashboardSkeleton() {
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div className="space-y-2">
          <Skeleton className="h-3 w-10" />
          <div className="space-y-1.5">
            <Skeleton className="h-9 w-28" />
            <Skeleton className="h-4 w-72 max-w-full" />
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-9 w-20" />
        </div>
      </div>
      <div className="space-y-4">
        <div className="grid gap-4 lg:grid-cols-3">
          <DashboardMetricSkeleton>
            <div className="mb-4 flex items-center gap-2">
              <Skeleton className="size-4 rounded" />
              <Skeleton className="h-4 w-20" />
            </div>
            <div className="grid grid-cols-[minmax(0,1fr)_1px_minmax(0,1fr)] gap-3">
              <Skeleton className="h-12 rounded-lg" />
              <div className="w-px bg-[hsl(var(--glass-divider))]" />
              <Skeleton className="h-12 rounded-lg" />
            </div>
          </DashboardMetricSkeleton>
          <DashboardMetricSkeleton>
            <div className="mb-4 flex items-center gap-2">
              <Skeleton className="size-4 rounded" />
              <Skeleton className="h-4 w-24" />
            </div>
            <div className="grid grid-cols-[minmax(0,1fr)_1px_minmax(0,1fr)] gap-3">
              <Skeleton className="h-12 rounded-lg" />
              <div className="w-px bg-[hsl(var(--glass-divider))]" />
              <Skeleton className="h-12 rounded-lg" />
            </div>
          </DashboardMetricSkeleton>
          <DashboardMetricSkeleton>
            <div className="mb-4 flex items-center gap-2">
              <Skeleton className="size-4 rounded" />
              <Skeleton className="h-4 w-20" />
            </div>
            <div className="grid grid-cols-[minmax(0,1fr)_1px_minmax(0,1fr)] gap-3">
              <Skeleton className="h-12 rounded-lg" />
              <div className="w-px bg-[hsl(var(--glass-divider))]" />
              <Skeleton className="h-12 rounded-lg" />
            </div>
          </DashboardMetricSkeleton>
        </div>

        <div className="grid auto-rows-[360px] gap-4 2xl:grid-cols-3">
          <DashboardPanelSkeleton className="2xl:col-span-2" variant="chart" />
          <DashboardPanelSkeleton variant="list" />
          <DashboardPanelSkeleton variant="bar" />
          <DashboardPanelSkeleton variant="heatmap" />
          <DashboardPanelSkeleton variant="list" />
        </div>
        <div className="grid gap-4 xl:grid-cols-2">
          <DashboardPanelSkeleton className="h-[320px]" variant="uptime" />
          <DashboardPanelSkeleton className="h-[320px]" variant="resource" />
        </div>
      </div>
    </div>
  )
}

function DashboardMetricSkeleton({ children }: { children: ReactNode }) {
  return (
    <Card className="min-h-[132px] rounded-lg p-4">
      {children}
    </Card>
  )
}

type DashboardPanelSkeletonVariant = 'chart' | 'list' | 'bar' | 'heatmap' | 'resource' | 'uptime'

function DashboardPanelSkeleton({
  className,
  variant = 'chart',
}: {
  className?: string
  variant?: DashboardPanelSkeletonVariant
}) {
  return (
    <Card
      className={cn(
        'flex min-h-0 flex-col rounded-lg p-5',
        className,
      )}
    >
      <div className="mb-5 flex shrink-0 flex-wrap items-start justify-between gap-3">
        <div className="space-y-2">
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-3 w-20" />
        </div>
        {variant === 'chart' ? <Skeleton className="h-5 w-56" /> : null}
      </div>
      <DashboardPanelSkeletonContent variant={variant} />
    </Card>
  )
}

function DashboardPanelSkeletonContent({ variant }: { variant: DashboardPanelSkeletonVariant }) {
  if (variant === 'list') {
    return (
      <div className="min-h-0 flex-1 divide-y divide-[hsl(var(--glass-divider))] overflow-hidden">
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-3 py-3">
            <Skeleton className="mt-0.5 size-4 rounded-full" />
            <div className="space-y-2">
              <Skeleton className="h-4 w-4/5" />
              <Skeleton className="h-3 w-11/12" />
            </div>
            <Skeleton className="h-4 w-12" />
          </div>
        ))}
      </div>
    )
  }

  if (variant === 'bar') {
    return (
      <div className="flex min-h-0 flex-1 flex-col justify-center gap-4">
        {Array.from({ length: 6 }).map((_, index) => (
          <div key={index} className="grid grid-cols-[92px_minmax(0,1fr)] items-center gap-3">
            <Skeleton className="h-3 w-full" />
            <Skeleton className={cn('h-6 rounded-md', index === 0 ? 'w-full' : index === 1 ? 'w-2/3' : 'w-1/3')} />
          </div>
        ))}
      </div>
    )
  }

  if (variant === 'heatmap') {
    return (
      <div className="min-h-0 flex-1">
        <div className="mb-6 flex items-center justify-between gap-4">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-8 w-24" />
        </div>
        <div className="grid grid-cols-[repeat(26,minmax(0,1fr))] gap-1.5">
          {Array.from({ length: 156 }).map((_, index) => (
            <Skeleton key={index} className="aspect-square rounded-[4px]" />
          ))}
        </div>
      </div>
    )
  }

  if (variant === 'uptime') {
    return (
      <div className="min-h-0 flex-1 space-y-3 overflow-hidden">
        {Array.from({ length: 6 }).map((_, index) => (
          <div key={index} className="grid grid-cols-[120px_minmax(0,1fr)_44px] items-center gap-3">
            <Skeleton className="h-4 w-full" />
            <div className="grid grid-cols-[repeat(24,minmax(0,1fr))] gap-1">
              {Array.from({ length: 24 }).map((_, bucketIndex) => (
                <Skeleton key={bucketIndex} className="h-4 rounded-[4px]" />
              ))}
            </div>
            <Skeleton className="h-4 w-full" />
          </div>
        ))}
      </div>
    )
  }

  if (variant === 'resource') {
    return (
      <div className="grid min-h-0 flex-1 grid-cols-2 gap-x-8 gap-y-6">
        {Array.from({ length: 4 }).map((_, index) => (
          <div key={index} className="space-y-3">
            <div className="flex items-center justify-between gap-4">
              <Skeleton className="h-4 w-20" />
              <Skeleton className="h-4 w-12" />
            </div>
            <Skeleton className="h-2 w-full rounded-full" />
            <Skeleton className="h-3 w-28" />
          </div>
        ))}
      </div>
    )
  }

  return <Skeleton className="h-[250px] rounded-lg" />
}
