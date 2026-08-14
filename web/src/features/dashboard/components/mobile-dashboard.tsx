import { Activity, CalendarDays, Gauge, Hash, RefreshCw, Sigma, Timer, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { TokenUsageHoverCard, type TokenUsageColumn, type TokenUsageLabels } from '@/components/common/token-usage-hover-card'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { DashboardCooldownList } from './dashboard-cooldown-list'
import { DashboardRiskList } from './dashboard-risk-list'
import { SiteUptimeStrip } from './site-uptime-strip'
import { SystemResourcePanel } from './system-resource-panel'
import type { CooldownItem, DashboardRiskItem, SiteUptimeItem } from './dashboard-types'

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
  riskItems: DashboardRiskItem[]
  cooldownItems: CooldownItem[]
  uptimeRows: SiteUptimeItem[]
  cooldownsFetching?: boolean
  clearAllCooldownsPending?: boolean
  onRefresh: () => void
  onRefreshCooldowns: () => void
  onRiskClick: (item: DashboardRiskItem) => void
  onClearCooldown: (item: CooldownItem) => void
  onClearAllCooldowns: () => void
  formatRefreshTime: (date: string, locale: string) => string
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
  riskItems,
  cooldownItems,
  uptimeRows,
  cooldownsFetching,
  clearAllCooldownsPending,
  onRefresh,
  onRefreshCooldowns,
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

      <SiteUptimeStrip className="h-[380px] rounded-2xl" sites={uptimeRows} />
      <SystemResourcePanel className="h-[420px] rounded-2xl" />
    </div>
  )
}

export function MobileDashboardSkeleton() {
  return (
    <div className="space-y-4 pb-2">
      <section className="space-y-4">
        <div className="space-y-1.5">
          <Skeleton className="h-3 w-24" />
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
            <Skeleton className="h-9 w-32" />
            <Skeleton className="h-8 w-[4.5rem]" />
          </div>
          <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-1.5">
            <Skeleton className="h-5 w-56 max-w-full" />
            <Skeleton className="h-4 w-24" />
          </div>
        </div>

        <div className="grid gap-3">
          <MobileKpiCardSkeleton />
          <MobileKpiCardSkeleton />
          <MobileKpiCardSkeleton />
        </div>
      </section>

      <MobilePanelSkeleton className="h-[360px]" variant="list" />
      <MobilePanelSkeleton className="h-[360px]" variant="list" />
      <MobilePanelSkeleton className="h-[380px]" variant="uptime" />
      <MobilePanelSkeleton className="h-[420px]" variant="resource" />
    </div>
  )
}

function MobileKpiCardSkeleton() {
  return (
    <Card className="rounded-2xl p-4">
      <div className="mb-4 flex items-center gap-2">
        <Skeleton className="size-4 rounded" />
        <Skeleton className="h-4 w-20" />
      </div>
      <div className="grid grid-cols-[minmax(0,1fr)_1px_minmax(0,1fr)] gap-3">
        <Skeleton className="h-12 rounded-lg" />
        <div className="bg-[hsl(var(--glass-divider))]" />
        <Skeleton className="h-12 rounded-lg" />
      </div>
      <Skeleton className="mt-3 h-4 w-3/4" />
    </Card>
  )
}

type MobilePanelSkeletonVariant = 'list' | 'uptime' | 'resource'

function MobilePanelSkeleton({
  className,
  variant,
}: {
  className?: string
  variant: MobilePanelSkeletonVariant
}) {
  return (
    <Card className={cn('flex min-h-0 flex-col rounded-2xl p-4', className)}>
      <div className="mb-4 shrink-0 space-y-2">
        <Skeleton className="h-4 w-28" />
        <Skeleton className="h-3 w-20" />
      </div>
      <MobilePanelSkeletonContent variant={variant} />
    </Card>
  )
}

function MobilePanelSkeletonContent({ variant }: { variant: MobilePanelSkeletonVariant }) {
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
          <div key={index} className="space-y-2">
            <Skeleton className="h-4 w-24" />
            <div className="grid grid-cols-[repeat(24,minmax(0,1fr))] gap-0.5">
              {Array.from({ length: 24 }).map((_, bucketIndex) => (
                <Skeleton key={bucketIndex} className="h-4 rounded-[4px]" />
              ))}
            </div>
          </div>
        ))}
      </div>
    )
  }

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
