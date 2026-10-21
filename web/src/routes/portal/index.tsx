import { useEffect, useMemo, useState } from 'react'
import { HoverDetails } from '@/components/common/hover-details'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  Area,
  CartesianGrid,
  ComposedChart,
  Legend,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { KeyRound, LoaderCircle, LogOut, RotateCcw } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import type { ColumnDef } from '@tanstack/react-table'
import { DataTable } from '@/components/common/data-table'
import { StatusBadge } from '@/components/common/status-badge'
import {
  chartColors,
  chartGridStroke,
  chartTick,
  chartTooltipItemStyle,
  chartTooltipLabelStyle,
  chartTooltipStyle,
} from '@/components/charts/chart-theme'
import { useMobileLayout } from '@/hooks/use-media-query'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeading, CardMetric, CardMetrics, ItemCard } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { PaginationControls } from '@/components/ui/pagination'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { LanguageSwitcher } from '@/components/common/language-switcher'
import { APP_LOGO_SRC, APP_NAME } from '@/lib/brand'
import { APIError } from '@/lib/http'
import { toast } from '@/lib/toast'
import {
  fetchPortalModels,
  fetchPortalOverview,
  fetchPortalRequests,
  fetchPortalSettings,
  fetchPortalSummary,
  type PortalDimensions,
  type PortalOverview,
  type PortalPeriodicQuota,
  type PortalRequestFilters,
  type PortalRequestItem,
  type PortalRequestStats,
  type PortalSettings,
  type PortalSummary,
} from '@/features/portal/api'
import { clearStoredPortalKey, readStoredPortalKey, writeStoredPortalKey } from '@/features/portal/storage'

export function PortalPage() {
  const { t } = useTranslation('portal')
  const [activeKey, setActiveKey] = useState<string | null>(() => readStoredPortalKey())

  const settingsQuery = useQuery({
    queryKey: ['portal', 'settings'],
    queryFn: fetchPortalSettings,
    retry: false,
  })

  function handleAuthenticated(key: string, remember: boolean) {
    if (remember) {
      writeStoredPortalKey(key)
    } else {
      clearStoredPortalKey()
    }
    setActiveKey(key)
  }

  function handleClear() {
    clearStoredPortalKey()
    setActiveKey(null)
  }

  const settings = settingsQuery.data
  const disabled = (settings && !settings.enabled) || settingsQuery.isError

  return (
    <div className="h-full overflow-y-auto bg-canvas text-label">
      <div className="mx-auto flex min-h-full w-full max-w-5xl flex-col gap-6 px-4 pb-[calc(2rem+env(safe-area-inset-bottom,0px))] pt-[calc(1.5rem+env(safe-area-inset-top,0px))] sm:px-6 sm:pt-[calc(2rem+env(safe-area-inset-top,0px))]">
        <header className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <img src={APP_LOGO_SRC} alt={APP_NAME} className="size-8 rounded-control" />
            <div className="leading-tight">
              <p className="text-headline text-label">{APP_NAME}</p>
              <p className="text-caption-1 text-label-3">{t('title')}</p>
            </div>
          </div>
          <LanguageSwitcher />
        </header>

        {settingsQuery.isLoading ? (
          <CenterState>
            <LoaderCircle className="h-5 w-5 animate-spin text-label-3" />
          </CenterState>
        ) : disabled ? (
          <CenterState>
            <p className="text-body text-label-2">{t('disabled')}</p>
          </CenterState>
        ) : (
          <>
            <KeyBar activeKey={activeKey} onAuthenticated={handleAuthenticated} onClear={handleClear} />
            {activeKey && settings ? (
              <PortalDashboard portalKey={activeKey} settings={settings} onInvalidKey={handleClear} />
            ) : (
              <p className="px-1 text-footnote text-label-3">{t('gate.hint')}</p>
            )}
          </>
        )}
      </div>
    </div>
  )
}

function CenterState({ children }: { children: React.ReactNode }) {
  return <div className="flex flex-1 items-center justify-center py-20">{children}</div>
}

type KeyBarProps = {
  activeKey: string | null
  onAuthenticated: (key: string, remember: boolean) => void
  onClear: () => void
}

function KeyBar({ activeKey, onAuthenticated, onClear }: KeyBarProps) {
  const { t } = useTranslation('portal')
  const [key, setKey] = useState('')
  const [remember, setRemember] = useState(true)
  const [masked, setMasked] = useState(false)

  const verifyMutation = useMutation({
    mutationFn: (candidate: string) => fetchPortalOverview(candidate),
    onSuccess: (_data, candidate) => {
      onAuthenticated(candidate.trim(), remember)
    },
    onError: (error) => {
      const message = error instanceof APIError && error.status === 401 ? t('invalidKey') : (error as Error).message
      toast.error(t('loadFailed'), { description: message })
    },
  })

  function handleSubmit(event: React.FormEvent) {
    event.preventDefault()
    const trimmed = key.trim()
    if (!trimmed) return
    setMasked(true)
    verifyMutation.mutate(trimmed)
  }

  function handleClear() {
    setKey('')
    setMasked(false)
    onClear()
  }

  function revealForEditing() {
    if (masked) {
      setKey('')
      setMasked(false)
    }
  }

  return (
    <Card>
      <CardContent className="space-y-3 p-4 sm:p-5">
        <div className="flex items-center gap-2">
          <KeyRound className="h-4 w-4 text-tint" />
          <h2 className="text-headline text-label">{activeKey ? t('gate.switchTitle') : t('gate.title')}</h2>
          {activeKey ? (
            <Button variant="ghost" size="sm" className="ml-auto" onClick={handleClear}>
              <LogOut className="h-4 w-4" />
              {t('signOut')}
            </Button>
          ) : null}
        </div>
        <form className="flex flex-col gap-3 sm:flex-row sm:items-center" onSubmit={handleSubmit}>
          <Input
            type="text"
            value={masked ? maskKey(key) : key}
            readOnly={masked}
            onChange={(event) => {
              setMasked(false)
              setKey(event.target.value)
            }}
            onFocus={revealForEditing}
            placeholder={t('gate.placeholder')}
            autoFocus={!activeKey}
            spellCheck={false}
            autoComplete="off"
            className="font-mono"
          />
          <label className="flex shrink-0 items-center gap-2 text-footnote text-label-2">
            <Switch checked={remember} onCheckedChange={setRemember} aria-label={t('gate.remember')} />
            {t('gate.remember')}
          </label>
          <Button type="submit" className="shrink-0" disabled={!key.trim() || verifyMutation.isPending}>
            {verifyMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
            {t('gate.submit')}
          </Button>
        </form>
      </CardContent>
    </Card>
  )
}

type RangePreset = 'all' | 'today' | '7d' | '30d'

function rangeToDays(range: RangePreset, summaryDays: number): number {
  if (range === 'today') return 1
  if (range === '7d') return 7
  if (range === '30d') return 30
  return summaryDays
}

function daysToFromISO(days: number): string {
  const start = new Date()
  start.setHours(0, 0, 0, 0)
  start.setDate(start.getDate() - (days - 1))
  return start.toISOString()
}

function buildRangeOptions(summaryDays: number): RangePreset[] {
  const options: RangePreset[] = ['all', 'today']
  if (summaryDays >= 7) options.push('7d')
  if (summaryDays >= 30) options.push('30d')
  return options
}

type PortalDashboardProps = {
  portalKey: string
  settings: PortalSettings
  onInvalidKey: () => void
}

function PortalDashboard({ portalKey, settings, onInvalidKey }: PortalDashboardProps) {
  const { t } = useTranslation('portal')
  const dims = settings.dimensions
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<'all' | 'success' | 'failed'>('all')
  const [model, setModel] = useState('all')
  const [range, setRange] = useState<RangePreset>('all')

  const rangeOptions = useMemo(() => buildRangeOptions(settings.summary_days), [settings.summary_days])
  const rangeLabels = useMemo<Record<RangePreset, string>>(
    () => ({
      today: t('filters.rangeToday'),
      '7d': t('filters.range7d'),
      '30d': t('filters.range30d'),
      all: t('filters.rangeAll'),
    }),
    [t],
  )
  const days = rangeToDays(range, settings.summary_days)

  const filters = useMemo<PortalRequestFilters>(() => {
    const value: PortalRequestFilters = { from: daysToFromISO(days) }
    if (status !== 'all') value.status = status
    if (model !== 'all') value.model = model
    return value
  }, [status, model, days])

  const overviewQuery = useQuery({
    queryKey: ['portal', 'overview', portalKey],
    queryFn: () => fetchPortalOverview(portalKey),
    retry: false,
  })

  useEffect(() => {
    if (overviewQuery.error instanceof APIError && overviewQuery.error.status === 401) {
      toast.error(t('invalidKey'))
      onInvalidKey()
    }
  }, [overviewQuery.error, onInvalidKey, t])

  const summaryQuery = useQuery({
    queryKey: ['portal', 'summary', portalKey, days],
    queryFn: () => fetchPortalSummary(portalKey, days),
    enabled: settings.show_summary,
    retry: false,
  })

  const modelsQuery = useQuery({
    queryKey: ['portal', 'models', portalKey],
    queryFn: () => fetchPortalModels(portalKey),
    enabled: settings.show_requests && dims.model,
    retry: false,
  })

  const requestsQuery = useQuery({
    queryKey: ['portal', 'requests', portalKey, page, filters],
    queryFn: () => fetchPortalRequests(portalKey, page, 20, filters),
    enabled: settings.show_requests,
    retry: false,
  })

  const overview = overviewQuery.data
  const summary = summaryQuery.data
  const models = modelsQuery.data?.models ?? []

  function updateFilter(next: () => void) {
    setPage(1)
    next()
  }

  return (
    <div className="space-y-6">
      {overview ? (
        <section className={[
          'grid gap-4',
          hasConfiguredLimits(overview) ? 'sm:grid-cols-2 lg:grid-cols-3' : 'sm:grid-cols-2',
        ].join(' ')}>
          <KeyInfoCard overview={overview} />
          <AccumulatedUsageCard overview={overview} />
          <QuotaLimitsCard overview={overview} />
        </section>
      ) : overviewQuery.isLoading ? (
        <CenterState>
          <LoaderCircle className="h-5 w-5 animate-spin text-label-3" />
        </CenterState>
      ) : null}

      {settings.show_summary && summary ? (
        <Card>
          <CardContent className="space-y-4 p-4 sm:p-5">
            <div className="flex items-center justify-between gap-3">
              <h3 className="text-headline text-label">{t('summary.title', { days: summary.range.days })}</h3>
              <span className="text-footnote text-label-3">{t('summary.totalRequests', { count: summary.totals.success })}</span>
            </div>
            <TrendChart summary={summary} dims={dims} />
          </CardContent>
        </Card>
      ) : null}

      {settings.show_requests ? (
        <Card material="thick" className="overflow-hidden">
          <div className="space-y-3 p-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="text-headline text-label">{t('requests.title')}</h3>
              <RequestStats stats={requestsQuery.data?.stats} dims={dims} />
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <Select value={status} onValueChange={(value) => updateFilter(() => setStatus(value as typeof status))}>
                <SelectTrigger variant="filter" filterLabel={t('filters.status')} active={status !== 'all'}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent searchable={false} widthMode="content">
                  <SelectItem value="all">{t('filters.statusAll')}</SelectItem>
                  <SelectItem value="success">{t('filters.statusSuccess')}</SelectItem>
                  <SelectItem value="failed">{t('filters.statusFailed')}</SelectItem>
                </SelectContent>
              </Select>

              {dims.model ? (
                <Select value={model} onValueChange={(value) => updateFilter(() => setModel(value))}>
                  <SelectTrigger variant="filter" filterLabel={t('filters.model')} active={model !== 'all'}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent widthMode="content">
                    <SelectItem value="all">{t('filters.modelAll')}</SelectItem>
                    {models.map((item) => (
                      <SelectItem key={item} value={item}>
                        {item}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : null}

              <Select value={range} onValueChange={(value) => updateFilter(() => setRange(value as RangePreset))}>
                <SelectTrigger variant="filter" filterLabel={t('filters.range')} active={range !== 'all'}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent searchable={false} widthMode="content">
                  {rangeOptions.map((option) => (
                    <SelectItem key={option} value={option}>
                      {rangeLabels[option]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <Button
                variant="ghost"
                size="icon-sm"
                className="ml-auto"
                aria-label={t('requests.refresh', { defaultValue: 'Refresh' })}
                onClick={() => requestsQuery.refetch()}
                disabled={requestsQuery.isFetching}
              >
                <RotateCcw className={requestsQuery.isFetching ? 'size-4 animate-spin' : 'size-4'} />
              </Button>
            </div>
          </div>

          <RequestsList items={requestsQuery.data?.items ?? []} dims={dims} loading={requestsQuery.isLoading} />

          {requestsQuery.data && requestsQuery.data.total > 0 ? (
            <PaginationControls
              page={page}
              pageSize={requestsQuery.data.page_size}
              totalItems={requestsQuery.data.total}
              onPageChange={setPage}
              showPageSize={false}
              disabled={requestsQuery.isFetching}
              className="justify-center border-t border-separator p-3 sm:justify-end sm:px-4"
            />
          ) : null}
        </Card>
      ) : null}
    </div>
  )
}

function RequestStats({ stats, dims }: { stats: PortalRequestStats | undefined; dims: PortalDimensions }) {
  const { t } = useTranslation('portal')
  if (!stats || (!dims.tokens && !dims.cost)) return null
  return (
    <div className="flex flex-nowrap items-center gap-2">
      {dims.cost && stats.cost != null ? (
        <Badge variant="accent" className="tabular-nums">
          {t('stats.cost')} {formatUSD(stats.cost)}
        </Badge>
      ) : null}
      {dims.tokens && stats.total_tokens != null ? (
        <Badge variant="neutral" className="tabular-nums">
          {t('stats.tokens')} {formatTokens(stats.total_tokens)}
        </Badge>
      ) : null}
    </div>
  )
}

function hasConfiguredLimits(overview: PortalOverview) {
  const { daily, weekly } = overview.quota
  return (!overview.quota.unlimited && overview.quota.limit != null)
    || (!weekly.unlimited && weekly.limit != null)
    || (!daily.unlimited && daily.limit != null)
}

function formatResetIn(resetAt: string | null, t: (k: string, opts?: Record<string, unknown>) => string): string | null {
  if (!resetAt) return null
  const diffMs = new Date(resetAt).getTime() - Date.now()
  if (!Number.isFinite(diffMs) || diffMs <= 0) return null
  const totalMinutes = Math.floor(diffMs / 60_000)
  const days = Math.floor(totalMinutes / 1440)
  const hours = Math.floor((totalMinutes % 1440) / 60)
  const minutes = totalMinutes % 60
  if (days > 0) return t('overview.resetInDays', { count: days })
  if (hours > 0) return t('overview.resetInHours', { count: hours })
  return t('overview.resetInMinutes', { count: minutes })
}

function KeyInfoCard({ overview }: { overview: PortalOverview }) {
  const { t } = useTranslation('portal')
  const { key } = overview
  return (
    <Card className="h-full">
      <CardContent className="flex h-full flex-col justify-between gap-4 p-4 sm:p-5">
        <div className="space-y-1">
          <p className="text-caption-1 text-label-3">{t('overview.keyTitle')}</p>
          <p className="truncate text-headline text-label">{key.name}</p>
          <p className="truncate font-mono text-footnote text-label-3">{key.masked_key}</p>
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
          <StatusBadge status={key.is_active ? 'healthy' : 'error'}>
            {key.is_active ? t('overview.active') : key.status}
          </StatusBadge>
          {key.expires_at ? (
            <span className="text-caption-1 text-label-3">
              {t('overview.expires')} {new Date(key.expires_at).toLocaleDateString()}
            </span>
          ) : (
            <span className="text-caption-1 text-label-3">{t('overview.noExpiry')}</span>
          )}
          {key.last_used_at ? (
            <span className="text-caption-1 text-label-3">
              {t('overview.lastUsed')} {new Date(key.last_used_at).toLocaleDateString()}
            </span>
          ) : null}
        </div>
      </CardContent>
    </Card>
  )
}

function AccumulatedUsageCard({ overview }: { overview: PortalOverview }) {
  const { t } = useTranslation('portal')
  const { quota } = overview
  return (
    <Card className="h-full">
      <CardContent className="flex h-full flex-col gap-1.5 p-4 sm:p-5">
        <p className="text-caption-1 text-label-3">{t('overview.accumulatedUsage')}</p>
        <p className="text-kpi tabular-nums text-label">{formatUSD(quota.accumulated_used)}</p>
      </CardContent>
    </Card>
  )
}

function QuotaLimitsCard({ overview }: { overview: PortalOverview }) {
  const { t } = useTranslation('portal')
  const { daily, weekly } = overview.quota
  const total: PortalPeriodicQuota = {
    limit: overview.quota.limit,
    used: overview.quota.used,
    remaining: overview.quota.remaining,
    unlimited: overview.quota.unlimited,
    reset_at: null,
  }
  const hasTotal = !total.unlimited && total.limit != null
  const hasDaily = !daily.unlimited && daily.limit != null
  const hasWeekly = !weekly.unlimited && weekly.limit != null
  const rows = [
    hasTotal ? { id: 'total', label: t('overview.totalLimit'), shortLabel: t('overview.totalShort'), quota: total } : null,
    hasWeekly ? { id: 'weekly', label: t('overview.weeklyLimit'), shortLabel: t('overview.weeklyShort'), quota: weekly } : null,
    hasDaily ? { id: 'daily', label: t('overview.dailyLimit'), shortLabel: t('overview.dailyShort'), quota: daily } : null,
  ].filter((row): row is { id: string; label: string; shortLabel: string; quota: PortalPeriodicQuota } => row != null)
  const detailRows = [
    { id: 'accumulated', label: t('overview.accumulatedUsage'), shortLabel: '', used: overview.quota.accumulated_used, limit: null, remaining: null, resetAt: null, unlimited: true },
    ...rows.map(({ id, label, shortLabel, quota }) => ({
      id,
      label,
      shortLabel,
      used: quota.used,
      limit: quota.limit,
      remaining: quota.remaining,
      resetAt: quota.reset_at,
      unlimited: quota.unlimited,
    })),
  ]
  if (rows.length === 0) return null

  const trigger = (
    <button
      type="button"
      className="inline-block max-w-full min-w-0 rounded-inner text-left align-top focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tint"
    >
      <span className="sr-only">{t('overview.quotaDetails')}, </span>
      <span className="block space-y-1.5 text-footnote tabular-nums">
        {rows.map((row) => <PortalQuotaScopeRow key={row.id} row={row} />)}
      </span>
    </button>
  )

  return <Card className="h-full"><CardContent className="flex h-full flex-col gap-2 p-4 sm:p-5">
    <p className="text-caption-1 text-label-3">{t('overview.limitsTitle')}</p>
      <HoverDetails
        asChild
        title={t('overview.quotaDetails')}
        description={overview.key.name}
        contentClassName="w-[420px]"
        content={<PortalQuotaDetails rows={detailRows} t={t} />}
      >
        {trigger}
      </HoverDetails>
  </CardContent></Card>
}

type PortalQuotaDetailRow = {
  id: string
  label: string
  shortLabel: string
  used: number
  limit: number | null
  remaining: number | null
  resetAt: string | null
  unlimited: boolean
}

function PortalQuotaScopeRow({ row }: { row: { shortLabel: string; quota: PortalPeriodicQuota } }) {
  const available = row.quota.remaining ?? Math.max((row.quota.limit ?? 0) - row.quota.used, 0)
  return <span className="flex min-w-0 items-center gap-1.5">
    <span className="w-6 shrink-0 text-label-3">{row.shortLabel}</span>
    <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${portalQuotaDot(row.quota)}`} />
    <span className="truncate font-medium text-label">{formatUSD(available)} / {formatUSD(row.quota.limit!)}</span>
  </span>
}

function PortalQuotaDetails({ rows, t }: { rows: PortalQuotaDetailRow[]; t: (key: string, options?: Record<string, unknown>) => string }) {
  return <div className="divide-y divide-separator text-footnote tabular-nums">
    {rows.map((row) => {
      const value = row.id === 'accumulated'
        ? formatUSD(row.used)
        : `${formatUSD(row.remaining ?? Math.max((row.limit ?? 0) - row.used, 0))} / ${formatUSD(row.limit!)} · ${t('overview.used')} ${formatUSD(row.used)}`
      const resetIn = formatResetIn(row.resetAt, t)
      return <div key={row.id} className="flex min-w-0 items-start justify-between gap-4 py-2.5 first:pt-1 last:pb-1">
        <div className="flex shrink-0 items-center gap-1.5 text-label-3">
          {row.id !== 'accumulated' ? <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${portalQuotaDot(row)}`} /> : null}
          <span>{row.label}</span>
          {resetIn ? <span className="text-caption-2">{resetIn}</span> : null}
        </div>
        <span className="min-w-0 text-right font-medium text-label">{value}</span>
      </div>
    })}
  </div>
}

function portalQuotaDot(quota: Pick<PortalPeriodicQuota, 'limit' | 'used' | 'remaining' | 'unlimited'>) {
  if (quota.unlimited || quota.limit == null || quota.limit <= 0) return 'bg-success'
  const remain = Math.min(Math.max((quota.remaining ?? Math.max(quota.limit - quota.used, 0)) / quota.limit * 100, 0), 100)
  return remain <= 10 ? 'bg-danger' : remain <= 30 ? 'bg-warning' : 'bg-success'
}

const trendColors = { tokens: chartColors[0], cost: chartColors[3], requests: chartColors[2] }

function TrendChart({ summary, dims }: { summary: PortalSummary; dims: PortalDimensions }) {
  const { t } = useTranslation('portal')
  const showTokens = dims.tokens && summary.trend.some((bucket) => bucket.total_tokens != null)
  const showCost = dims.cost && summary.trend.some((bucket) => bucket.cost != null)
  return (
    <div className="h-[240px] sm:h-[260px]">
      <ResponsiveContainer width="100%" height="100%">
        <ComposedChart data={summary.trend} margin={{ top: 8, right: 0, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="portalTrendTokens" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={trendColors.tokens} stopOpacity={0.3} />
              <stop offset="100%" stopColor={trendColors.tokens} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid stroke={chartGridStroke} vertical={false} />
          <XAxis dataKey="date" tickLine={false} axisLine={false} tick={chartTick} minTickGap={16} />
          {showTokens ? (
            <YAxis yAxisId="tokens" tickLine={false} axisLine={false} width={46} tickFormatter={(value) => formatTokens(Number(value))} tick={chartTick} />
          ) : null}
          {showCost ? (
            <YAxis yAxisId="cost" orientation="right" tickLine={false} axisLine={false} width={46} tickFormatter={(value) => formatUSD(Number(value))} tick={chartTick} />
          ) : null}
          <YAxis yAxisId="requests" hide />
          <Tooltip
            formatter={(value, name, item) => {
              const numeric = Number(value)
              const formatted =
                item?.dataKey === 'cost'
                  ? formatUSD(numeric)
                  : item?.dataKey === 'total_tokens'
                    ? formatTokens(numeric)
                    : numeric.toLocaleString()
              return [formatted, name] as [string, string]
            }}
            contentStyle={chartTooltipStyle}
            labelStyle={chartTooltipLabelStyle}
            itemStyle={chartTooltipItemStyle}
          />
          <Legend
            iconType="square"
            iconSize={8}
            wrapperStyle={{ fontSize: 12, paddingTop: 4, color: 'var(--label-tertiary)' }}
            formatter={(value) => <span style={{ color: 'var(--label-primary)' }}>{value}</span>}
          />
          {showTokens ? (
            <Area
              yAxisId="tokens"
              type="monotone"
              dataKey="total_tokens"
              name={t('summary.seriesTokens')}
              stroke={trendColors.tokens}
              fill="url(#portalTrendTokens)"
              strokeWidth={2}
            />
          ) : null}
          {showCost ? (
            <Line yAxisId="cost" type="monotone" dataKey="cost" name={t('summary.seriesCost')} stroke={trendColors.cost} strokeWidth={2} dot={false} />
          ) : null}
          <Line yAxisId="requests" type="monotone" dataKey="success" name={t('summary.seriesRequests')} stroke={trendColors.requests} strokeWidth={2} dot={false} />
        </ComposedChart>
      </ResponsiveContainer>
    </div>
  )
}

function RequestsList({ items, dims, loading }: { items: PortalRequestItem[]; dims: PortalDimensions; loading: boolean }) {
  const { t } = useTranslation('portal')
  const isMobile = useMobileLayout()

  const columns = useMemo<ColumnDef<PortalRequestItem>[]>(() => {
    const cols: ColumnDef<PortalRequestItem>[] = [
      {
        id: 'time',
        header: t('requests.time'),
        cell: ({ row }) => <span className="whitespace-nowrap text-footnote text-label-2">{formatTime(row.original.created_at)}</span>,
      },
    ]
    if (dims.model) {
      cols.push({
        id: 'model',
        header: t('requests.model'),
        cell: ({ row }) => <span className="font-medium">{modelLabel(row.original)}</span>,
      })
    }
    if (dims.site) {
      cols.push({
        id: 'site',
        header: t('requests.site'),
        cell: ({ row }) => (
          <div>
            <div>{row.original.site?.name ?? '—'}</div>
            {row.original.site?.site_type ? (
              <div className="text-caption-2 text-label-3">{row.original.site.site_type}</div>
            ) : null}
          </div>
        ),
      })
    }
    if (dims.endpoint) {
      cols.push({
        id: 'endpoint',
        header: t('requests.endpoint'),
        cell: ({ row }) => (
          <div className="max-w-[200px] truncate font-mono text-footnote" title={row.original.endpoint}>{row.original.endpoint ?? '—'}</div>
        ),
      })
    }
    cols.push({
      id: 'status',
      header: t('requests.status'),
      cell: ({ row }) => <RequestStatusBadge item={row.original} />,
    })
    if (dims.tokens) {
      cols.push(
        {
          id: 'input',
          header: t('requests.input'),
          meta: { align: 'right' },
          cell: ({ row }) => (
            <div className="tabular-nums">
              <div>{row.original.usage?.prompt_tokens ?? '—'}</div>
              {row.original.usage?.cache_tokens ? (
                <div className="text-caption-2 text-label-3">{t('requests.cache', { count: row.original.usage.cache_tokens })}</div>
              ) : null}
              {row.original.usage?.cache_write_tokens ? (
                <div className="text-caption-2 text-label-3">{t('requests.cacheWrite', { count: row.original.usage.cache_write_tokens })}</div>
              ) : null}
            </div>
          ),
        },
        {
          id: 'output',
          header: t('requests.output'),
          meta: { align: 'right' },
          cell: ({ row }) => <span className="tabular-nums">{row.original.usage?.completion_tokens ?? '—'}</span>,
        },
      )
    }
    if (dims.cost) {
      cols.push({
        id: 'cost',
        header: t('requests.cost'),
        meta: { align: 'right' },
        cell: ({ row }) => (
          <span className="tabular-nums">
            {row.original.cost?.estimated_cost != null ? formatUSD(row.original.cost.estimated_cost) : '—'}
          </span>
        ),
      })
    }
    if (dims.latency) {
      cols.push({
        id: 'latency',
        header: t('requests.latency'),
        meta: { align: 'right' },
        cell: ({ row }) => (
          <span className="tabular-nums">{row.original.latency_ms != null ? `${row.original.latency_ms} ms` : '—'}</span>
        ),
      })
    }
    if (dims.upstream) {
      cols.push({
        id: 'upstream',
        header: t('requests.upstream'),
        cell: ({ row }) => (
          <div className="max-w-[220px] truncate font-mono text-footnote" title={upstreamText(row.original.upstream)}>
            {upstreamText(row.original.upstream) || '—'}
          </div>
        ),
      })
    }
    return cols
  }, [dims, t])

  if (loading) {
    return (
      <div className="flex justify-center border-t border-separator py-10">
        <LoaderCircle className="size-5 animate-spin text-label-3" />
      </div>
    )
  }
  if (!items.length) {
    return (
      <p className="border-t border-separator px-4 py-10 text-center text-body text-label-3">{t('requests.empty')}</p>
    )
  }

  if (isMobile) {
    return (
      <div className="space-y-3 px-4 pb-4">
        {items.map((item) => (
          <PortalRequestCard key={item.id} item={item} dims={dims} />
        ))}
      </div>
    )
  }

  return (
    <div className="border-t border-separator">
      <DataTable columns={columns} data={items} getRowId={(item) => item.id} className="[&_table]:min-w-[720px]" />
    </div>
  )
}

function RequestStatusBadge({ item }: { item: PortalRequestItem }) {
  return <StatusBadge status={item.success ? 'success' : 'error'}>{String(item.status_code)}</StatusBadge>
}

function modelLabel(item: PortalRequestItem) {
  return item.model?.canonical_model ?? item.model?.upstream_model ?? '—'
}

function PortalRequestCard({ item, dims }: { item: PortalRequestItem; dims: PortalDimensions }) {
  const { t } = useTranslation('portal')
  const tokens = item.usage
  const metrics: { key: string; label: string; value: string }[] = []
  if (dims.site) metrics.push({ key: 'site', label: t('requests.site'), value: item.site?.name ?? '—' })
  if (dims.tokens) {
    metrics.push({ key: 'input', label: t('requests.input'), value: String(tokens?.prompt_tokens ?? '—') })
    metrics.push({ key: 'output', label: t('requests.output'), value: String(tokens?.completion_tokens ?? '—') })
  }
  if (dims.cost) {
    metrics.push({ key: 'cost', label: t('requests.cost'), value: item.cost?.estimated_cost != null ? formatUSD(item.cost.estimated_cost) : '—' })
  }
  if (dims.latency) {
    metrics.push({ key: 'latency', label: t('requests.latency'), value: item.latency_ms != null ? `${item.latency_ms} ms` : '—' })
  }
  if (dims.endpoint) metrics.push({ key: 'endpoint', label: t('requests.endpoint'), value: item.endpoint ?? '—' })
  if (dims.upstream) metrics.push({ key: 'upstream', label: t('requests.upstream'), value: upstreamText(item.upstream) || '—' })

  return (
    <ItemCard>
      <CardHeading
        title={dims.model ? modelLabel(item) : formatTime(item.created_at)}
        tags={<RequestStatusBadge item={item} />}
        meta={dims.model ? formatTime(item.created_at) : undefined}
      />
      {metrics.length ? (
        <CardMetrics>
          {metrics.map((metric) => (
            <CardMetric key={metric.key} label={metric.label} value={metric.value} />
          ))}
        </CardMetrics>
      ) : null}
    </ItemCard>
  )
}

function upstreamText(upstream: PortalRequestItem['upstream']): string {
  if (!upstream) return ''
  if (typeof upstream.url === 'string' && upstream.url) return upstream.url
  if (typeof upstream.path === 'string' && upstream.path) return upstream.path
  return ''
}

function maskKey(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return ''
  const dash = trimmed.indexOf('-')
  const headKeep = (dash >= 0 ? dash + 1 : 0) + 4
  const tailKeep = 4
  if (trimmed.length <= headKeep + tailKeep) return trimmed
  return `${trimmed.slice(0, headKeep)}****${trimmed.slice(-tailKeep)}`
}

function formatTokens(value: number) {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(2)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return String(value)
}

function formatUSD(value: number) {
  return `$${value.toFixed(value >= 1 ? 2 : 4)}`
}

function formatTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
