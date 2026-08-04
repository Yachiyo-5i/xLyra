import { Activity, CalendarDays, Gauge, Hash, RefreshCw, Sigma, Timer, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { TokenUsageHoverCard, type TokenUsageColumn, type TokenUsageLabels } from '@/components/common/token-usage-hover-card'
import { Card } from '@/components/ui/card'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { DashboardDays } from '@/features/dashboard/api/dashboard'
import { dashboardDaysToRange, dashboardRangeToDays, type ApiKeyContributions } from '@/features/dashboard/lib/dashboard-utils'
import { cn } from '@/lib/utils'
import { ApiKeyContributionsPanel } from './api-key-contributions-panel'
import { DashboardCooldownList } from './dashboard-cooldown-list'
import { DashboardRiskList } from './dashboard-risk-list'
import { ModelCostChart } from './model-cost-chart'
import { ModelRequestsChart } from './model-requests-chart'
import { SiteCostChart } from './site-cost-chart'
import { SiteUptimeStrip } from './site-uptime-strip'
import { SystemResourcePanel } from './system-resource-panel'
import type { CooldownItem, DashboardRiskItem, DashboardSeries, DashboardTrendDatum, SiteCostDatum, SiteUptimeItem } from './dashboard-types'

type MobileDashboardProps = {
  generatedAt: string
  locale: string
  isFetching?: boolean
  costToday: string
  costTotal: string
  costYesterday: string
  requestsToday: string
  requestsTotal: string
  tokensToday: string
  tokensTotal: string
  requestsYesterday: string
  tokensYesterday: string
  tokenUsage: TokenUsageColumn[]
  tokenUsageLabels: TokenUsageLabels
  successRate: string
  rpm: string
  tpm: string
  completedRpm: number
  trendMetric: DashboardTrendMetric
  trendScope: DashboardTrendScope
  days: DashboardDays
  modelCost: {
    data: DashboardTrendDatum[]
    series: DashboardSeries[]
  }
  modelRequests: {
    data: DashboardTrendDatum[]
    series: DashboardSeries[]
  }
  siteCostTrend: {
    data: DashboardTrendDatum[]
    series: DashboardSeries[]
  }
  siteRequestsTrend: {
    data: DashboardTrendDatum[]
    series: DashboardSeries[]
  }
  siteCosts: SiteCostDatum[]
  apiKeyContributions?: ApiKeyContributions
  selectedContributionKeyId: string
  riskItems: DashboardRiskItem[]
  cooldownItems: CooldownItem[]
  uptimeRows: SiteUptimeItem[]
  cooldownsFetching?: boolean
  clearAllCooldownsPending?: boolean
  onRefresh: () => void
  onRefreshCooldowns: () => void
  onTrendMetricChange: (value: DashboardTrendMetric) => void
  onTrendScopeChange: (value: DashboardTrendScope) => void
  onDaysChange: (days: DashboardDays) => void
  onContributionKeyChange: (keyId: string) => void
  onRiskClick: (item: DashboardRiskItem) => void
  onClearCooldown: (item: CooldownItem) => void
  onClearAllCooldowns: () => void
  formatRefreshTime: (date: string, locale: string) => string
}

type DashboardTrendMetric = 'cost' | 'requests'
type DashboardTrendScope = 'model' | 'site'

export function MobileDashboard({
  generatedAt,
  locale,
  isFetching,
  costToday,
  costTotal,
  costYesterday,
  requestsToday,
  requestsTotal,
  tokensToday,
  tokensTotal,
  requestsYesterday,
  tokensYesterday,
  tokenUsage,
  tokenUsageLabels,
  successRate,
  rpm,
  tpm,
  completedRpm,
  trendMetric,
  trendScope,
  days,
  modelCost,
  modelRequests,
  siteCostTrend,
  siteRequestsTrend,
  siteCosts,
  apiKeyContributions,
  selectedContributionKeyId,
  riskItems,
  cooldownItems,
  uptimeRows,
  cooldownsFetching,
  clearAllCooldownsPending,
  onRefresh,
  onRefreshCooldowns,
  onTrendMetricChange,
  onTrendScopeChange,
  onDaysChange,
  onContributionKeyChange,
  onRiskClick,
  onClearCooldown,
  onClearAllCooldowns,
  formatRefreshTime,
}: MobileDashboardProps) {
  const { t } = useTranslation('dashboard')

  return (
    <div className="space-y-4 pb-2">
      <section className="space-y-4">
        <div className="space-y-1.5">
          <p className="text-xs font-medium uppercase tracking-[0.24em] text-faint">{t('page.eyebrow')}</p>
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
            <h2 className="text-3xl font-semibold tracking-tight text-foreground">{t('page.title')}</h2>
            <Button
              variant="secondary"
              size="sm"
              onClick={onRefresh}
              disabled={isFetching}
            >
              <RefreshCw className={cn('size-4', isFetching && 'animate-spin')} />
              {t('page.refresh')}
            </Button>
          </div>
          <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1.5">
            <p className="max-w-[18rem] text-sm leading-6 text-muted-soft">{t('page.description')}</p>
            <span className="whitespace-nowrap pt-1 text-xs text-muted-soft">
              {t('page.update')} {formatRefreshTime(generatedAt, locale)}
            </span>
          </div>
        </div>

        <div className="grid gap-3">
          <MobileKpiCard
            icon={WalletCards}
            title={t('kpis.cost')}
            primaryLabel={t('kpis.today')}
            primaryValue={costToday}
            primaryIcon={CalendarDays}
            secondaryLabel={t('kpis.total')}
            secondaryValue={costTotal}
            secondaryIcon={Sigma}
            note={`${t('kpis.yesterdayCost')} ${costYesterday}`}
            accent="cost"
          />
          <MobileKpiCard
            icon={Activity}
            title={t('kpis.requestsTokens')}
            primaryLabel={t('kpis.todayTotalRequests')}
            primaryValue={`${requestsToday} / ${requestsTotal}`}
            primaryIcon={Hash}
            secondaryLabel={t('kpis.todayTotalTokens')}
            secondaryValue={`${tokensToday} / ${tokensTotal}`}
            secondaryIcon={Sigma}
            secondaryTokenUsage={tokenUsage}
            tokenUsageLabels={tokenUsageLabels}
            note={`${t('kpis.successRate')} ${successRate} · ${t('kpis.yesterdayRequests')} ${requestsYesterday} · ${t('kpis.yesterdayTokens')} ${tokensYesterday}`}
            accent="traffic"
          />
          <MobileKpiCard
            icon={Gauge}
            title={t('kpis.performance')}
            primaryLabel="RPM"
            primaryValue={rpm}
            primaryIcon={Activity}
            secondaryLabel="TPM"
            secondaryValue={tpm}
            secondaryIcon={Timer}
            note={`${t('kpis.completedRpm')} ${completedRpm}`}
            accent="warning"
          />
        </div>
      </section>

      <DashboardRiskList
        className="h-[360px] rounded-2xl"
        items={riskItems}
        onItemClick={onRiskClick}
      />

      <DashboardCooldownList
        className="h-[360px] rounded-2xl"
        compact
        items={cooldownItems}
        action={
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              className="h-8 px-2 text-muted-soft hover:text-foreground"
              disabled={cooldownsFetching}
              onClick={onRefreshCooldowns}
            >
              <RefreshCw className={cn('size-4', cooldownsFetching && 'animate-spin')} />
              {t('page.refreshCooldowns')}
            </Button>
            {cooldownItems.length ? (
              <Button
                variant="ghost"
                size="sm"
                className="h-8 px-2 text-muted-soft hover:text-foreground"
                disabled={clearAllCooldownsPending}
                onClick={onClearAllCooldowns}
              >
                {t('page.clearAll')}
              </Button>
            ) : null}
          </div>
        }
        onClearItem={onClearCooldown}
      />

      <MobileModelTrendCard
        trendMetric={trendMetric}
        trendScope={trendScope}
        days={days}
        modelCost={modelCost}
        modelRequests={modelRequests}
        siteCostTrend={siteCostTrend}
        siteRequestsTrend={siteRequestsTrend}
        onTrendMetricChange={onTrendMetricChange}
        onTrendScopeChange={onTrendScopeChange}
        onDaysChange={onDaysChange}
      />

      <MobileSiteCostCard days={days} siteCosts={siteCosts} />

      {apiKeyContributions ? (
        <ApiKeyContributionsPanel
          className="h-[360px] rounded-2xl"
          contributions={apiKeyContributions}
          selectedKeyId={selectedContributionKeyId || apiKeyContributions.defaultKeyId}
          onSelectedKeyChange={onContributionKeyChange}
        />
      ) : null}

      <SiteUptimeStrip className="h-[380px] rounded-2xl" sites={uptimeRows} />
      <SystemResourcePanel className="h-[420px] rounded-2xl" />
    </div>
  )
}

type MobileKpiCardProps = {
  icon: typeof WalletCards
  title: string
  primaryLabel: string
  primaryValue: string
  primaryIcon: typeof WalletCards
  secondaryLabel: string
  secondaryValue: string
  secondaryIcon: typeof WalletCards
  secondaryTokenUsage?: TokenUsageColumn[]
  tokenUsageLabels?: TokenUsageLabels
  note?: string
  accent: 'cost' | 'traffic' | 'warning'
}

const accentClasses: Record<MobileKpiCardProps['accent'], string> = {
  cost: 'text-[hsl(190_88%_42%)]',
  traffic: 'text-[hsl(154_70%_38%)]',
  warning: 'text-[hsl(42_88%_48%)]',
}

function MobileKpiCard({
  icon: Icon,
  title,
  primaryLabel,
  primaryValue,
  primaryIcon: PrimaryIcon,
  secondaryLabel,
  secondaryValue,
  secondaryIcon: SecondaryIcon,
  secondaryTokenUsage,
  tokenUsageLabels,
  note,
  accent,
}: MobileKpiCardProps) {
  return (
    <Card className="rounded-2xl p-4">
      <div className="mb-4 flex items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <Icon className={cn('size-4 shrink-0', accentClasses[accent])} />
          <p className="min-w-0 text-sm font-semibold text-foreground">{title}</p>
        </div>
      </div>
      <div className="grid grid-cols-[minmax(0,1fr)_1px_minmax(0,1fr)] gap-3">
        <MobileKpiValue icon={PrimaryIcon} label={primaryLabel} value={primaryValue} />
        <div className="bg-[hsl(var(--glass-divider))]" />
        <MobileKpiValue icon={SecondaryIcon} label={secondaryLabel} value={secondaryValue} tokenUsage={secondaryTokenUsage} tokenUsageLabels={tokenUsageLabels} />
      </div>
      {note ? <p className="mt-3 break-words text-xs leading-5 text-muted-soft">{note}</p> : null}
    </Card>
  )
}

function MobileKpiValue({ icon: Icon, label, value, tokenUsage, tokenUsageLabels }: { icon: typeof WalletCards; label: string; value: string; tokenUsage?: TokenUsageColumn[]; tokenUsageLabels?: TokenUsageLabels }) {
  const content = (
    <div className="flex min-w-0 flex-col">
      <div className="mb-2 flex min-w-0 items-center gap-1.5 text-xs text-muted-soft">
        <Icon className="size-3.5 shrink-0" />
        <span className="min-w-0 break-words">{label}</span>
      </div>
      <p className="mt-auto break-words text-xl font-semibold tracking-normal text-foreground">{value}</p>
    </div>
  )
  if (!tokenUsage || !tokenUsageLabels) return content
  return (
    <TokenUsageHoverCard columns={tokenUsage} labels={tokenUsageLabels} className="w-full">
      {content}
    </TokenUsageHoverCard>
  )
}

function MobileModelTrendCard({
  trendMetric,
  trendScope,
  days,
  modelCost,
  modelRequests,
  siteCostTrend,
  siteRequestsTrend,
  onTrendMetricChange,
  onTrendScopeChange,
  onDaysChange,
}: {
  trendMetric: DashboardTrendMetric
  trendScope: DashboardTrendScope
  days: DashboardDays
  modelCost: {
    data: DashboardTrendDatum[]
    series: DashboardSeries[]
  }
  modelRequests: {
    data: DashboardTrendDatum[]
    series: DashboardSeries[]
  }
  siteCostTrend: {
    data: DashboardTrendDatum[]
    series: DashboardSeries[]
  }
  siteRequestsTrend: {
    data: DashboardTrendDatum[]
    series: DashboardSeries[]
  }
  onTrendMetricChange: (value: DashboardTrendMetric) => void
  onTrendScopeChange: (value: DashboardTrendScope) => void
  onDaysChange: (days: DashboardDays) => void
}) {
  const { t } = useTranslation('dashboard')
  const costTrend = trendScope === 'model' ? modelCost : siteCostTrend
  const requestsTrend = trendScope === 'model' ? modelRequests : siteRequestsTrend

  return (
    <Card className="rounded-2xl p-4">
      <div className="mb-4 space-y-1.5">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5">
          <h3 className="text-base font-semibold text-foreground">
            <MobileSlashTabs<DashboardTrendMetric>
              value={trendMetric}
              onValueChange={onTrendMetricChange}
              items={orderedTrendMetricItems(trendScope, trendMetric, t)}
            />
          </h3>
          <div className="min-w-0 max-w-full overflow-x-auto">
            <MobileSlashTabs<DashboardTrendScope>
              value={trendScope}
              onValueChange={onTrendScopeChange}
              items={[
                { label: t('charts.model'), value: 'model' },
                { label: t('charts.site'), value: 'site' },
              ]}
            />
          </div>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5">
          <p className="text-sm text-muted-soft">{t('charts.trend', { days })}</p>
          <div className="min-w-0 max-w-full overflow-x-auto">
            <MobileRangeTabs days={days} onDaysChange={onDaysChange} />
          </div>
        </div>
      </div>
      {trendMetric === 'cost' ? (
        <ModelCostChart data={costTrend.data} series={costTrend.series} height={230} />
      ) : (
        <ModelRequestsChart data={requestsTrend.data} series={requestsTrend.series} height={230} />
      )}
    </Card>
  )
}

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

function MobileSiteCostCard({ days, siteCosts }: { days: DashboardDays; siteCosts: SiteCostDatum[] }) {
  const { t } = useTranslation('dashboard')
  const chartHeight = Math.max(230, siteCosts.length * 34 + 72)

  return (
    <Card className="rounded-2xl p-4">
      <div className="mb-4 space-y-1">
        <h3 className="text-base font-semibold text-foreground">{t('charts.siteCost')}</h3>
        <p className="text-sm text-muted-soft">{t('charts.summary', { days })}</p>
      </div>
      <div className="scrollbar-hidden max-h-[340px] overflow-auto pr-1">
        <SiteCostChart data={siteCosts} height={chartHeight} compactYAxis />
      </div>
    </Card>
  )
}

function MobileSlashTabs<T extends string>({
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

function MobileRangeTabs({ days, onDaysChange }: { days: DashboardDays; onDaysChange: (days: DashboardDays) => void }) {
  const { t } = useTranslation('dashboard')

  return (
    <Tabs variant="slash" value={dashboardDaysToRange(days)} onValueChange={(value) => onDaysChange(dashboardRangeToDays(value))}>
      <TabsList>
        <TabsTrigger value="7d" className="text-sm">{t('ranges.7d')}</TabsTrigger>
        <TabsTrigger value="30d" className="text-sm">{t('ranges.30d')}</TabsTrigger>
        <TabsTrigger value="90d" className="text-sm">{t('ranges.90d')}</TabsTrigger>
      </TabsList>
    </Tabs>
  )
}
