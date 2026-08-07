import { useCallback, useEffect, useId, useMemo, useState, type CSSProperties } from 'react'
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
import { KeyRound, LoaderCircle, LogOut, RefreshCw } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { PaginationControls } from '@/components/ui/pagination'
import {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { LanguageSwitcher } from '@/components/common/language-switcher'
import { useMobileLayout } from '@/hooks/use-media-query'
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
    <div className="h-full overflow-y-auto bg-background text-foreground">
      <div className="mx-auto flex min-h-full w-full max-w-5xl flex-col gap-6 px-4 py-8 sm:px-6">
        <header className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <img src={APP_LOGO_SRC} alt={APP_NAME} className="h-7 w-7 rounded-md" />
            <div className="leading-tight">
              <p className="text-sm font-semibold">{APP_NAME}</p>
              <p className="text-muted-soft text-xs">{t('title')}</p>
            </div>
          </div>
          <LanguageSwitcher />
        </header>

        {settingsQuery.isLoading ? (
          <CenterState>
            <LoaderCircle className="h-5 w-5 animate-spin text-muted-soft" />
          </CenterState>
        ) : disabled ? (
          <CenterState>
            <p className="text-muted-soft text-sm">{t('disabled')}</p>
          </CenterState>
        ) : (
          <>
            <KeyBar activeKey={activeKey} onAuthenticated={handleAuthenticated} onClear={handleClear} />
            {activeKey && settings ? (
              <PortalDashboard portalKey={activeKey} settings={settings} onInvalidKey={handleClear} />
            ) : (
              <p className="text-muted-soft px-1 text-sm">{t('gate.hint')}</p>
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
      <CardContent className="space-y-3 p-4">
        <div className="flex items-center gap-2">
          <KeyRound className="h-4 w-4 text-primary" />
          <h2 className="text-sm font-semibold">{activeKey ? t('gate.switchTitle') : t('gate.title')}</h2>
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
          <label className="flex shrink-0 items-center gap-2 text-sm text-muted-soft">
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
          <LoaderCircle className="h-5 w-5 animate-spin text-muted-soft" />
        </CenterState>
      ) : null}

      {settings.show_summary && summary ? (
        <Card>
          <CardContent className="space-y-4 p-5">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold">{t('summary.title', { days: summary.range.days })}</h3>
              <span className="text-muted-soft text-xs">{t('summary.totalRequests', { count: summary.totals.success })}</span>
            </div>
            <TrendChart summary={summary} dims={dims} />
          </CardContent>
        </Card>
      ) : null}

      {settings.show_requests ? (
        <Card>
          <CardContent className="space-y-4 p-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <h3 className="text-sm font-semibold">{t('requests.title')}</h3>
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
                size="sm"
                className="ml-auto"
                onClick={() => requestsQuery.refetch()}
                disabled={requestsQuery.isFetching}
              >
                <RefreshCw className={requestsQuery.isFetching ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} />
              </Button>
            </div>

            <RequestsTable items={requestsQuery.data?.items ?? []} dims={dims} loading={requestsQuery.isLoading} />

            {requestsQuery.data && requestsQuery.data.total > 0 ? (
              <PaginationControls
                page={page}
                pageSize={requestsQuery.data.page_size}
                totalItems={requestsQuery.data.total}
                onPageChange={setPage}
                showPageSize={false}
                disabled={requestsQuery.isFetching}
                className="justify-end pt-1"
              />
            ) : null}
          </CardContent>
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
        <Badge variant="accent" className="h-8 w-fit rounded-md px-2.5 text-xs tabular-nums">
          {t('stats.cost')} {formatUSD(stats.cost)}
        </Badge>
      ) : null}
      {dims.tokens && stats.total_tokens != null ? (
        <Badge variant="neutral" className="h-8 w-fit rounded-md px-2.5 text-xs tabular-nums">
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
      <CardContent className="flex h-full flex-col justify-between gap-4 p-5">
        <div className="space-y-1">
          <p className="text-muted-soft text-xs">{t('overview.keyTitle')}</p>
          <p className="truncate text-base font-semibold">{key.name}</p>
          <p className="text-muted-soft truncate font-mono text-xs">{key.masked_key}</p>
        </div>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
          <span className={['text-xs font-medium', key.is_active ? 'text-emerald-500' : 'text-red-500'].join(' ')}>
            {key.is_active ? t('overview.active') : key.status}
          </span>
          {key.expires_at ? (
            <span className="text-muted-soft text-xs">
              {t('overview.expires')} {new Date(key.expires_at).toLocaleDateString()}
            </span>
          ) : (
            <span className="text-muted-soft text-xs">{t('overview.noExpiry')}</span>
          )}
          {key.last_used_at ? (
            <span className="text-muted-soft text-xs">
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
      <CardContent className="flex h-full flex-col gap-1.5 p-5">
        <div className="space-y-1">
          <p className="text-muted-soft text-xs">{t('overview.accumulatedUsage')}</p>
          <p className="text-2xl font-semibold tabular-nums">{formatUSD(quota.accumulated_used)}</p>
        </div>
      </CardContent>
    </Card>
  )
}

function QuotaLimitsCard({ overview }: { overview: PortalOverview }) {
  const { t } = useTranslation('portal')
  const isMobile = useMobileLayout()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [mousePos, setMousePos] = useState<{ x: number; y: number } | null>(null)
  const [tooltipHeight, setTooltipHeight] = useState<number | null>(null)
  const tooltipId = useId()
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
  const measureTooltip = useCallback((node: HTMLDivElement | null) => {
    const nextHeight = node?.offsetHeight ?? null
    setTooltipHeight((current) => current === nextHeight ? current : nextHeight)
  }, [])
  if (rows.length === 0) return null

  const trigger = (
    <button
      type="button"
      className="inline-block max-w-full min-w-0 rounded-sm text-left align-top focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-strong))]"
      onMouseEnter={!isMobile ? (event) => setMousePos({ x: event.clientX, y: event.clientY }) : undefined}
      onMouseMove={!isMobile ? (event) => setMousePos({ x: event.clientX, y: event.clientY }) : undefined}
      onMouseLeave={!isMobile ? () => {
        setMousePos(null)
        setTooltipHeight(null)
      } : undefined}
      onFocus={!isMobile ? (event) => {
        const rect = event.currentTarget.getBoundingClientRect()
        setMousePos({ x: rect.left + rect.width / 2, y: rect.bottom })
      } : undefined}
      onBlur={!isMobile ? () => setMousePos(null) : undefined}
      aria-describedby={!isMobile && mousePos ? tooltipId : undefined}
    >
      <span className="sr-only">{t('overview.quotaDetails')}, </span>
      <span className="block space-y-1.5 text-xs tabular-nums">
        {rows.map((row) => <PortalQuotaScopeRow key={row.id} row={row} />)}
      </span>
    </button>
  )

  const tooltip = mousePos && !isMobile ? (
    <div
      id={tooltipId}
      ref={measureTooltip}
      role="tooltip"
      className="glass-panel-strong pointer-events-none fixed z-[160] w-[min(420px,calc(100vw-24px))] rounded-lg px-4 py-2 text-xs text-foreground shadow-lg"
      style={getPortalQuotaTooltipLayout(mousePos, detailRows.length, tooltipHeight)}
    >
      <PortalQuotaDetails rows={detailRows} t={t} />
    </div>
  ) : null

  return <Card className="h-full"><CardContent className="flex h-full flex-col gap-2 p-5">
    <p className="text-muted-soft text-xs">{t('overview.limitsTitle')}</p>
    {isMobile ? (
      <Sheet open={mobileOpen} onOpenChange={setMobileOpen}>
        <SheetTrigger asChild>{trigger}</SheetTrigger>
        <SheetContent side="bottom" className="rounded-t-2xl">
          <SheetHeader>
            <SheetTitle>{t('overview.quotaDetails')}</SheetTitle>
            <SheetDescription>{overview.key.name}</SheetDescription>
          </SheetHeader>
          <SheetBody><PortalQuotaDetails rows={detailRows} t={t} /></SheetBody>
        </SheetContent>
      </Sheet>
    ) : <>{trigger}{tooltip}</>}
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
    <span className="w-6 shrink-0 text-muted-soft">{row.shortLabel}</span>
    <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${portalQuotaDot(row.quota)}`} />
    <span className="truncate font-medium text-foreground">{formatUSD(available)} / {formatUSD(row.quota.limit!)}</span>
  </span>
}

function PortalQuotaDetails({ rows, t }: { rows: PortalQuotaDetailRow[]; t: (key: string, options?: Record<string, unknown>) => string }) {
  return <div className="divide-y divide-[hsl(var(--glass-divider))] text-xs tabular-nums">
    {rows.map((row) => {
      const value = row.id === 'accumulated'
        ? formatUSD(row.used)
        : `${formatUSD(row.remaining ?? Math.max((row.limit ?? 0) - row.used, 0))} / ${formatUSD(row.limit!)} · ${t('overview.used')} ${formatUSD(row.used)}`
      const resetIn = formatResetIn(row.resetAt, t)
      return <div key={row.id} className="flex min-w-0 items-start justify-between gap-4 py-2.5 first:pt-1 last:pb-1">
        <div className="flex shrink-0 items-center gap-1.5 text-muted-soft">
          {row.id !== 'accumulated' ? <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${portalQuotaDot(row)}`} /> : null}
          <span>{row.label}</span>
          {resetIn ? <span className="text-[10px]">{resetIn}</span> : null}
        </div>
        <span className="min-w-0 text-right font-medium text-foreground">{value}</span>
      </div>
    })}
  </div>
}

function portalQuotaDot(quota: Pick<PortalPeriodicQuota, 'limit' | 'used' | 'remaining' | 'unlimited'>) {
  if (quota.unlimited || quota.limit == null || quota.limit <= 0) return 'bg-emerald-500'
  const remain = Math.min(Math.max((quota.remaining ?? Math.max(quota.limit - quota.used, 0)) / quota.limit * 100, 0), 100)
  return remain <= 10 ? 'bg-red-500' : remain <= 30 ? 'bg-amber-500' : 'bg-emerald-500'
}

function getPortalQuotaTooltipLayout(mousePos: { x: number; y: number }, rowCount: number, measuredHeight: number | null): CSSProperties {
  if (typeof window === 'undefined') return { left: mousePos.x + 14, top: mousePos.y + 14, width: 420 }
  const width = Math.min(420, Math.max(240, window.innerWidth - 24))
  const height = measuredHeight ?? 44 + 36 * Math.max(1, rowCount)
  const left = Math.max(12, Math.min(mousePos.x + 14, window.innerWidth - width - 12))
  const top = mousePos.y + 14 + height > window.innerHeight - 12
    ? Math.max(12, mousePos.y - height - 14)
    : mousePos.y + 14
  return { left, top, width }
}

const trendColors = { tokens: 'hsl(var(--primary))', cost: '#f59e0b', requests: '#10b981' }

function TrendChart({ summary, dims }: { summary: PortalSummary; dims: PortalDimensions }) {
  const { t } = useTranslation('portal')
  const showTokens = dims.tokens && summary.trend.some((bucket) => bucket.total_tokens != null)
  const showCost = dims.cost && summary.trend.some((bucket) => bucket.cost != null)
  return (
    <div style={{ height: 260 }}>
      <ResponsiveContainer width="100%" height="100%">
        <ComposedChart data={summary.trend} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="portalTrendTokens" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={trendColors.tokens} stopOpacity={0.35} />
              <stop offset="100%" stopColor={trendColors.tokens} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid stroke="hsl(var(--glass-border))" vertical={false} />
          <XAxis dataKey="date" tickLine={false} axisLine={false} tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 11 }} />
          {showTokens ? (
            <YAxis yAxisId="tokens" tickLine={false} axisLine={false} width={46} tickFormatter={(value) => formatTokens(Number(value))} tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 11 }} />
          ) : null}
          {showCost ? (
            <YAxis yAxisId="cost" orientation="right" tickLine={false} axisLine={false} width={46} tickFormatter={(value) => formatUSD(Number(value))} tick={{ fill: 'hsl(var(--text-muted-soft))', fontSize: 11 }} />
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
            contentStyle={{
              background: 'hsl(var(--surface-panel))',
              border: '1px solid hsl(var(--glass-border))',
              borderRadius: 8,
              boxShadow: '0 16px 40px rgb(0 0 0 / 0.16)',
              fontSize: 12,
            }}
            labelStyle={{ color: 'hsl(var(--text-muted-soft))', marginBottom: 4 }}
            itemStyle={{ padding: '1px 0' }}
          />
          <Legend wrapperStyle={{ fontSize: 12 }} />
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

function RequestsTable({ items, dims, loading }: { items: PortalRequestItem[]; dims: PortalDimensions; loading: boolean }) {
  const { t } = useTranslation('portal')

  if (loading) {
    return (
      <div className="flex justify-center py-8">
        <LoaderCircle className="h-5 w-5 animate-spin text-muted-soft" />
      </div>
    )
  }
  if (!items.length) {
    return <p className="text-muted-soft py-8 text-center text-sm">{t('requests.empty')}</p>
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] text-sm">
        <thead>
          <tr className="text-muted-soft border-b border-[hsl(var(--divider))] text-left text-xs">
            <th className="py-2 pr-3 font-medium">{t('requests.time')}</th>
            {dims.model ? <th className="py-2 pr-3 font-medium">{t('requests.model')}</th> : null}
            {dims.site ? <th className="py-2 pr-3 font-medium">{t('requests.site')}</th> : null}
            {dims.endpoint ? <th className="py-2 pr-3 font-medium">{t('requests.endpoint')}</th> : null}
            <th className="py-2 pr-3 font-medium">{t('requests.status')}</th>
            {dims.tokens ? <th className="py-2 pr-3 text-right font-medium">{t('requests.input')}</th> : null}
            {dims.tokens ? <th className="py-2 pr-3 text-right font-medium">{t('requests.output')}</th> : null}
            {dims.cost ? <th className="py-2 pr-3 text-right font-medium">{t('requests.cost')}</th> : null}
            {dims.latency ? <th className="py-2 pr-3 text-right font-medium">{t('requests.latency')}</th> : null}
            {dims.upstream ? <th className="py-2 pr-3 font-medium">{t('requests.upstream')}</th> : null}
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.id} className="border-b border-[hsl(var(--divider))]/50">
              <td className="py-2 pr-3 whitespace-nowrap">{formatTime(item.created_at)}</td>
              {dims.model ? (
                <td className="py-2 pr-3">{item.model?.canonical_model ?? item.model?.upstream_model ?? '—'}</td>
              ) : null}
              {dims.site ? (
                <td className="py-2 pr-3">
                  <div>{item.site?.name ?? '—'}</div>
                  {item.site?.site_type ? (
                    <div className="text-muted-soft text-[11px]">{item.site.site_type}</div>
                  ) : null}
                </td>
              ) : null}
              {dims.endpoint ? (
                <td className="py-2 pr-3">
                  <div className="max-w-[200px] truncate font-mono text-xs" title={item.endpoint}>{item.endpoint ?? '—'}</div>
                </td>
              ) : null}
              <td className="py-2 pr-3">
                <span className={item.success ? 'text-emerald-500' : 'text-red-500'}>{item.status_code}</span>
              </td>
              {dims.tokens ? (
                <td className="py-2 pr-3 text-right tabular-nums">
                  <div>{item.usage?.prompt_tokens ?? '—'}</div>
                  {item.usage?.cache_tokens ? (
                    <div className="text-muted-soft text-[11px]">{t('requests.cache', { count: item.usage.cache_tokens })}</div>
                  ) : null}
                  {item.usage?.cache_write_tokens ? (
                    <div className="text-muted-soft text-[11px]">{t('requests.cacheWrite', { count: item.usage.cache_write_tokens })}</div>
                  ) : null}
                </td>
              ) : null}
              {dims.tokens ? (
                <td className="py-2 pr-3 text-right tabular-nums">{item.usage?.completion_tokens ?? '—'}</td>
              ) : null}
              {dims.cost ? (
                <td className="py-2 pr-3 text-right tabular-nums">
                  {item.cost?.estimated_cost != null ? formatUSD(item.cost.estimated_cost) : '—'}
                </td>
              ) : null}
              {dims.latency ? (
                <td className="py-2 pr-3 text-right tabular-nums">
                  {item.latency_ms != null ? `${item.latency_ms} ms` : '—'}
                </td>
              ) : null}
              {dims.upstream ? (
                <td className="py-2 pr-3">
                  <div className="max-w-[220px] truncate font-mono text-xs" title={upstreamText(item.upstream)}>
                    {upstreamText(item.upstream) || '—'}
                  </div>
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
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
