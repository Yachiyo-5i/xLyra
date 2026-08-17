import { useMemo, type ReactNode } from 'react'
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
import {
  DashboardCooldownList,
  DashboardMetricCard,
  DashboardRiskList,
  SiteUptimeStrip,
  SystemResourcePanel,
  TabletRequestsTokensCard,
} from '@/features/dashboard/components'
import { MobileDashboard, MobileDashboardSkeleton } from '@/features/dashboard/components/mobile-dashboard'
import {
  dashboardQueryKeys,
  getDashboardCooldowns,
  getDashboardHealth,
  getDashboardInsights,
  getDashboardUsage,
  type DashboardOverview,
} from '@/features/dashboard/api/dashboard'
import {
  buildCooldownItems,
  buildAttentionRiskItems,
  buildUptimeRows,
  formatCompactNumber,
  formatDashboardCurrency,
  formatDashboardRefreshTime,
  formatLimitValue,
  formatPercent,
} from '@/features/dashboard/lib/dashboard-utils'
import { clearRouteCooldown, routeQueryKeys } from '@/features/routes/api/routes'
import { useMobileLayout, useTabletDashboardLayout } from '@/hooks/use-media-query'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'

export function DashboardPage() {
  const { t, i18n } = useTranslation('dashboard')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const isMobile = useMobileLayout()
  const isTabletDashboard = useTabletDashboardLayout()

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
    return isMobile ? <MobileDashboardSkeleton /> : <DashboardSkeleton tablet={isTabletDashboard} />
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
        riskItems={riskItems}
        cooldownItems={cooldownItems}
        uptimeRows={uptimeRows}
        cooldownsFetching={cooldownsQuery.isFetching}
        clearAllCooldownsPending={clearAllCooldownsMutation.isPending}
        onRefresh={refetchDashboard}
        onRefreshCooldowns={() => cooldownsQuery.refetch()}
        onRiskClick={handleRiskClick}
        onClearCooldown={(item) => clearCooldownMutation.mutate(item)}
        onClearAllCooldowns={() => clearAllCooldownsMutation.mutate()}
        formatRefreshTime={formatDashboardRefreshTime}
      />
    )
  }

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
        <div className={cn('grid gap-4 lg:grid-cols-3', isTabletDashboard && 'lg:grid-cols-2')}>
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
          {isTabletDashboard ? (
            <TabletRequestsTokensCard
              title={t('kpis.requestsTokens')}
              todayLabel={t('kpis.today')}
              totalLabel={t('kpis.total')}
              requestsLabel={t('kpis.requests')}
              tokensLabel={t('kpis.tokens')}
              requestsToday={requestsToday}
              requestsTotal={requestsTotal}
              tokensToday={tokensToday}
              tokensTotal={tokensTotal}
              successRateLabel={t('kpis.successRate')}
              successRate={successRate}
              yesterdayRequestsLabel={t('kpis.yesterdayRequests')}
              requestsYesterday={requestsYesterday}
              yesterdayTokensLabel={t('kpis.yesterdayTokens')}
              tokensYesterday={tokensYesterday}
              tokenUsage={tokenUsage}
              tokenUsageLabels={tokenUsageLabels}
            />
          ) : (
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
          )}
          <DashboardMetricCard
            className={cn(isTabletDashboard && 'lg:col-span-2')}
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

        <div className="grid gap-4 xl:grid-cols-2">
          <DashboardRiskList
            className="h-[360px]"
            items={riskItems}
            onItemClick={handleRiskClick}
          />
          <DashboardCooldownList
            className="h-[360px]"
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

function DashboardSkeleton({ tablet = false }: { tablet?: boolean }) {
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
        <div className={cn('grid gap-4 lg:grid-cols-3', tablet && 'lg:grid-cols-2')}>
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
          {tablet ? (
            <TabletRequestsTokensSkeleton />
          ) : (
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
          )}
          <DashboardMetricSkeleton className={cn(tablet && 'lg:col-span-2')}>
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

        <div className="grid gap-4 xl:grid-cols-2">
          <DashboardPanelSkeleton className="h-[360px]" variant="list" />
          <DashboardPanelSkeleton className="h-[360px]" variant="list" />
        </div>
        <div className="grid gap-4 xl:grid-cols-2">
          <DashboardPanelSkeleton className="h-[320px]" variant="uptime" />
          <DashboardPanelSkeleton className="h-[320px]" variant="resource" />
        </div>
      </div>
    </div>
  )
}

function TabletRequestsTokensSkeleton() {
  return (
    <DashboardMetricSkeleton>
      <div className="mb-3 flex items-center gap-2">
        <Skeleton className="size-4 rounded" />
        <Skeleton className="h-4 w-24" />
      </div>
      <div className="grid grid-cols-[minmax(0,1fr)_minmax(4.5rem,0.8fr)_minmax(4.5rem,0.8fr)] items-center gap-x-3 gap-y-2">
        <span />
        <Skeleton className="h-3 w-8 justify-self-end" />
        <Skeleton className="h-3 w-8 justify-self-end" />
        <Skeleton className="h-3 w-14" />
        <Skeleton className="h-5 w-10 justify-self-end" />
        <Skeleton className="h-5 w-14 justify-self-end" />
        <Skeleton className="h-3 w-14" />
        <Skeleton className="h-5 w-14 justify-self-end" />
        <Skeleton className="h-5 w-12 justify-self-end" />
      </div>
      <div className="mt-3 flex flex-wrap gap-2">
        <Skeleton className="h-3 w-20" />
        <Skeleton className="h-3 w-24" />
        <Skeleton className="h-3 w-24" />
      </div>
    </DashboardMetricSkeleton>
  )
}

function DashboardMetricSkeleton({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <Card className={cn('min-h-[132px] rounded-lg p-4', className)}>
      {children}
    </Card>
  )
}

type DashboardPanelSkeletonVariant = 'list' | 'resource' | 'uptime'

function DashboardPanelSkeleton({
  className,
  variant = 'list',
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
