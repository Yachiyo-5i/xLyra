import { useMemo, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ErrorState } from '@/components/common/error-state'
import { Button } from '@/components/ui/button'
import { DatePicker } from '@/components/ui/date-picker'
import { Input } from '@/components/ui/input'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Progress } from '@/components/ui/progress'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { OAuthConnectionListItem } from '@/features/oauth/api/oauth'
import type { Site } from '@/features/sites/api/sites'
import { isOAuthSite, siteTypeIcon } from '@/features/sites/lib/site-utils'
import {
  channelSplitQueryKeys,
  getChannelSplit,
  type ChannelSplitInput,
  type ChannelSplitItem,
  type ChannelSplitSummary,
  type ChannelSplitTarget,
} from '@/features/usage/api/channel-split'
import { formatCompactNumber, formatDashboardCurrency, formatPercent } from '@/features/dashboard/lib/dashboard-utils'
import { useMobileLayout } from '@/hooks/use-media-query'
import { cn } from '@/lib/utils'

type ChannelSplitMetric = 'cost' | 'tokens'

type ChannelSplitDialogProps = {
  open: boolean
  initialTarget?: ChannelSplitTarget | null
  sites: Site[]
  oauthConnections: OAuthConnectionListItem[]
  onOpenChange: (open: boolean) => void
}

const QUICK_RANGES = [7, 15, 30] as const

export function ChannelSplitDialog({
  open,
  initialTarget,
  sites,
  oauthConnections,
  onOpenChange,
}: ChannelSplitDialogProps) {
  const { t } = useTranslation('oauth')
  const isMobile = useMobileLayout()
  const [metric, setMetric] = useState<ChannelSplitMetric>('cost')
  const initialSelection = useMemo(() => initialChannelSplitSelection(initialTarget, oauthConnections), [initialTarget, oauthConnections])
  const [selectedSiteIds, setSelectedSiteIds] = useState(() => initialSelection.siteIds)
  const [oauthConnectionId] = useState(() => initialSelection.oauthConnectionId)
  const [dateRange, setDateRange] = useState(() => naturalDayRange(30))
  const [allTime, setAllTime] = useState(false)
  const [totalPriceDraft, setTotalPriceDraft] = useState<{ key: string; value: string } | null>(null)
  const siteOptions = useMemo(() => buildSiteOptions(sites, {
    deleted: t('split.filters.deletedSite'),
    oauth: t('split.filters.oauthGroup'),
    sites: t('split.filters.sitesGroup'),
  }), [sites, t])
  const dateFrom = formatDateParam(dateRange.from)
  const dateTo = formatDateParam(dateRange.to)
  const queryInput = useMemo<ChannelSplitInput | null>(() => {
    if (!selectedSiteIds.length) return null
    if (allTime) {
      return { siteIds: selectedSiteIds, oauthConnectionId, range: 'all' }
    }
    if (!dateFrom || !dateTo) return null
    return { siteIds: selectedSiteIds, oauthConnectionId, dateFrom, dateTo }
  }, [allTime, dateFrom, dateTo, oauthConnectionId, selectedSiteIds])

  const splitQuery = useQuery({
    queryKey: queryInput ? channelSplitQueryKeys.detail(queryInput) : channelSplitQueryKeys.all,
    queryFn: () => {
      if (!queryInput) throw new Error(t('split.targetRequired'))
      return getChannelSplit(queryInput)
    },
    enabled: open && Boolean(queryInput),
    placeholderData: keepPreviousData,
  })

  const visibleData = queryInput ? splitQuery.data : undefined
  const actualSummary = visibleData?.summary
  const emptySummary = useMemo(() => emptyChannelSplitSummary(allTime, dateFrom, dateTo), [allTime, dateFrom, dateTo])
  const summary = actualSummary ?? emptySummary
  const items = visibleData?.items ?? []
  const currency = summary?.currency ?? 'USD'
  const activeQuickRange = allTime ? null : QUICK_RANGES.find((days) => sameDayRange(dateRange, naturalDayRange(days)))
  const chartMetric = metric === 'cost' && (summary?.estimated_cost ?? 0) <= 0 ? 'tokens' : metric
  const totalPriceKey = summary ? channelSplitSummaryKey(summary) : ''
  const totalPriceInput = totalPriceDraft?.key === totalPriceKey
    ? totalPriceDraft.value
    : summary
      ? formatPriceInput(summary.estimated_cost)
      : ''
  const totalPrice = parsePriceInput(totalPriceInput)

  function applyQuickRange(days: number) {
    setAllTime(false)
    setDateRange(naturalDayRange(days))
  }

  function applyAllTime() {
    setAllTime(true)
  }

  function setFromDate(date: Date | undefined) {
    if (!date) return
    setAllTime(false)
    const nextFrom = startLocalDay(date)
    setDateRange((current) => ({
      from: nextFrom,
      to: current.to < nextFrom ? nextFrom : current.to,
    }))
  }

  function setToDate(date: Date | undefined) {
    if (!date) return
    setAllTime(false)
    const nextTo = startLocalDay(date)
    setDateRange((current) => ({
      from: current.from > nextTo ? nextTo : current.from,
      to: nextTo,
    }))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className={cn(
          'max-w-none grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-2xl',
          isMobile
            ? 'h-[84dvh] w-[min(94vw,1180px)]'
            : 'aspect-[16/10] w-[min(96vw,140.8vh,1180px)]',
        )}
      >
        <DialogHeader className={isMobile ? 'px-4 py-4' : 'px-6 py-5'}>
          <DialogTitle>{t('split.title')}</DialogTitle>
          <DialogDescription>
            {actualSummary
              ? t('split.descriptionWithTarget', {
                  target: splitTargetTitle(summary),
                  range: summary.range_all ? t('split.ranges.all') : t('split.ranges.between', { from: summary.date_from, to: summary.date_to }),
                })
              : t('split.description')}
          </DialogDescription>
        </DialogHeader>
        <DialogBody
          className={cn(
            'min-h-0',
            isMobile ? 'overflow-y-auto px-4 py-4' : 'overflow-hidden px-6 py-5',
          )}
        >
          <div className={cn('flex flex-col', isMobile ? 'min-h-full gap-3' : 'h-full min-h-0 gap-5')}>
            <div className={cn(
              'grid shrink-0',
              isMobile ? 'grid-cols-2 gap-2' : 'grid-cols-4 items-center gap-3',
            )}>
              <MultiSelect
                value={selectedSiteIds}
                options={siteOptions}
                placeholder={t('split.filters.target')}
                searchPlaceholder={t('split.filters.searchSites')}
                emptyText={t('split.filters.emptySites')}
                selectedText={t('split.filters.selectedSites')}
                maxVisibleTags={1}
                className={isMobile ? 'col-span-2' : undefined}
                onChange={setSelectedSiteIds}
              />

              <DatePicker
                value={dateRange.from}
                onValueChange={setFromDate}
                placeholder={t('split.filters.dateFrom')}
                disabledDates={{ after: startLocalDay(new Date()) }}
                disabled={allTime}
                clearable={false}
                triggerClassName={cn('h-10', isMobile && 'px-3')}
              />
              <DatePicker
                value={dateRange.to}
                onValueChange={setToDate}
                placeholder={t('split.filters.dateTo')}
                disabledDates={{ after: startLocalDay(new Date()) }}
                disabled={allTime}
                clearable={false}
                triggerClassName={cn('h-10', isMobile && 'px-3')}
              />

              <div className={cn('grid min-w-0 grid-cols-4 gap-2', isMobile && 'col-span-2')}>
                {QUICK_RANGES.map((days) => (
                  <Button
                    key={days}
                    type="button"
                    size="sm"
                    variant={activeQuickRange === days ? 'default' : 'outline'}
                    onClick={() => applyQuickRange(days)}
                    className="w-full min-w-0 px-2"
                  >
                    {t('split.ranges.days', { count: days })}
                  </Button>
                ))}
                <Button
                  type="button"
                  size="sm"
                  variant={allTime ? 'default' : 'outline'}
                  onClick={applyAllTime}
                  className="w-full min-w-0 px-2"
                >
                  {t('split.ranges.allShort')}
                </Button>
              </div>
            </div>

            {queryInput && splitQuery.isError ? (
              <ErrorState title={t('split.loadFailed')} description={splitQuery.error.message} />
            ) : queryInput && splitQuery.isLoading ? (
              <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-muted-soft">
                <LoaderCircle className="mr-2 h-4 w-4 animate-spin" />
                {t('split.loading')}
              </div>
            ) : (
              <>
                <div className={cn('grid shrink-0', isMobile ? 'grid-cols-2 gap-2' : 'grid-cols-4 gap-3')}>
                  <SplitMetricCard compact={isMobile} label={t('split.metrics.totalCost')} value={formatDashboardCurrency(summary.estimated_cost, currency, 2)} />
                  <SplitMetricCard compact={isMobile} label={t('split.metrics.totalTokens')} value={formatCompactNumber(summary.total_tokens)} />
                  <SplitMetricCard compact={isMobile} label={t('split.metrics.apiKeys')} value={String(summary.api_key_count)} />
                  <SplitMetricCard compact={isMobile} label={t('split.metrics.requests')} value={formatCompactNumber(summary.request_count)} />
                </div>

                <div className={cn(
                  'grid',
                  isMobile ? 'gap-3' : 'min-h-0 flex-1 grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-5',
                )}>
                  <div className={cn(
                    'flex flex-col rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))]',
                    isMobile ? 'p-3' : 'min-h-0 p-4',
                  )}>
                    <div className={cn('flex shrink-0 flex-wrap items-center justify-between gap-3', isMobile ? 'mb-3' : 'mb-4')}>
                      <div className="min-w-0">
                        <h3 className="text-sm font-semibold text-foreground">{t('split.chart.title')}</h3>
                      </div>
                      <Tabs value={metric} onValueChange={(value) => setMetric(value as ChannelSplitMetric)}>
                        <TabsList className={isMobile ? 'h-8 w-[148px]' : 'h-9 w-[164px]'}>
                          <TabsTrigger value="cost" className="text-xs">{t('split.metrics.cost')}</TabsTrigger>
                          <TabsTrigger value="tokens" className="text-xs">{t('split.metrics.tokens')}</TabsTrigger>
                        </TabsList>
                      </Tabs>
                    </div>
                    <div className={cn('pr-1', !isMobile && 'min-h-0 flex-1 overflow-y-auto')}>
                      <SplitContributionBars items={items} metric={chartMetric} currency={currency} />
                    </div>
                  </div>

                  <div className={cn(
                    'flex min-w-0 flex-col rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))]',
                    isMobile ? 'p-3' : 'min-h-0 p-4',
                  )}>
                    <div className={cn('flex shrink-0 flex-wrap items-center justify-between gap-2', isMobile ? 'mb-3' : 'mb-4')}>
                      <div>
                        <h3 className="text-sm font-semibold text-foreground">{t('split.table.title')}</h3>
                      </div>
                      <label className="flex items-center gap-2 text-xs text-muted-soft">
                        <span className="shrink-0">{t('split.table.totalPrice')}</span>
                        <Input
                          type="number"
                          inputMode="decimal"
                          min="0"
                          step="0.01"
                          value={totalPriceInput}
                          onChange={(event) => setTotalPriceDraft({ key: totalPriceKey, value: event.target.value })}
                          placeholder={t('split.table.totalPricePlaceholder')}
                          className={cn(
                            'h-9 px-3 text-right !text-base font-semibold tabular-nums [appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none',
                            isMobile ? 'w-32' : 'w-36',
                          )}
                        />
                      </label>
                    </div>
                    <div className={!isMobile ? 'min-h-0 flex-1' : undefined}>
                      <SplitTable isMobile={isMobile} items={items} currency={currency} totalPrice={totalPrice} />
                    </div>
                  </div>
                </div>
              </>
            )}
          </div>
        </DialogBody>
      </DialogContent>
    </Dialog>
  )
}

function SplitMetricCard({ compact, label, value }: { compact: boolean; label: string; value: string }) {
  return (
    <div className={cn(
      'rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))]',
      compact ? 'px-3 py-2.5' : 'px-4 py-3',
    )}>
      <div className="text-xs text-muted-soft">{label}</div>
      <div className={cn('mt-1 truncate font-semibold tabular-nums text-foreground', compact ? 'text-lg' : 'text-xl')} title={value}>
        {value}
      </div>
    </div>
  )
}

function SplitContributionBars({
  currency,
  items,
  metric,
}: {
  currency: string
  items: ChannelSplitItem[]
  metric: ChannelSplitMetric
}) {
  if (!items.length) {
    return <div className="h-full min-h-20" />
  }

  return (
    <div className="space-y-3">
      {items.map((item, index) => {
        const share = metric === 'cost' ? item.cost_share : item.token_share
        const value = metric === 'cost'
          ? formatDashboardCurrency(item.estimated_cost, currency, 2)
          : formatCompactNumber(item.total_tokens)

        return (
          <div key={item.api_key_id} className="space-y-1.5">
            <div className="flex items-center justify-between gap-3 text-sm">
              <div className="flex min-w-0 items-center gap-2">
                <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-md bg-[hsl(var(--surface-subtle))] text-[11px] text-muted-soft">
                  {index + 1}
                </span>
                <span className="truncate font-medium text-foreground" title={item.api_key_name || item.masked_key || item.api_key_id}>
                  {item.api_key_name || item.masked_key || item.api_key_id}
                </span>
              </div>
              <div className="shrink-0 text-right tabular-nums">
                <span className="text-foreground">{value}</span>
                <span className="ml-2 text-muted-soft">{formatPercent(share, 1)}</span>
              </div>
            </div>
            <Progress value={share * 100} max={100} variant={index === 0 ? 'accent' : 'info'} className="h-2" />
          </div>
        )
      })}
    </div>
  )
}

function SplitTable({
  currency,
  isMobile,
  items,
  totalPrice,
}: {
  currency: string
  isMobile: boolean
  items: ChannelSplitItem[]
  totalPrice: number | null
}) {
  const { t } = useTranslation('oauth')

  if (!items.length) {
    return <div className="h-full min-h-20" />
  }

  if (isMobile) {
    return (
      <div className="divide-y divide-[hsl(var(--glass-divider))]">
        {items.map((item) => {
          const name = item.api_key_name || item.api_key_id
          const actualPrice = totalPrice == null ? null : totalPrice * item.cost_share

          return (
            <div key={item.api_key_id} className="py-3 first:pt-0 last:pb-0">
              <div className="flex min-w-0 items-center justify-between gap-3">
                <div className="truncate text-sm font-semibold text-foreground" title={name}>
                  {name}
                </div>
                <div className="shrink-0 text-right">
                  <div className="text-[11px] text-muted-soft">{t('split.table.actualPrice')}</div>
                  <div className="mt-0.5 text-sm font-semibold tabular-nums text-primary">
                    {formatPriceValue(actualPrice)}
                  </div>
                </div>
              </div>
              <div className="mt-2 grid grid-cols-2 gap-x-4 gap-y-2">
                <SplitMobileValue label={t('split.table.cost')} value={formatDashboardCurrency(item.estimated_cost, currency, 2)} />
                <SplitMobileValue label={t('split.table.share')} value={formatPercent(item.cost_share, 1)} />
                <SplitMobileValue label={t('split.table.tokens')} value={formatCompactNumber(item.total_tokens)} />
                <SplitMobileValue label={t('split.table.requests')} value={formatCompactNumber(item.request_count)} />
              </div>
            </div>
          )
        })}
      </div>
    )
  }

  return (
    <div className="h-full min-h-0 overflow-y-auto overflow-x-hidden rounded-lg">
      <table className="w-full table-fixed border-collapse text-left">
        <thead className="sticky top-0 z-10 bg-[hsl(var(--surface-subtle))] shadow-[0_1px_0_hsl(var(--glass-divider))]">
          <tr className="text-faint text-xs uppercase tracking-[0.16em]">
            <th className="w-[30%] overflow-hidden text-ellipsis whitespace-nowrap px-3 py-3 font-medium">{t('split.table.apiKey')}</th>
            <th className="w-[16%] overflow-hidden text-ellipsis whitespace-nowrap px-3 py-3 text-right font-medium">{t('split.table.cost')}</th>
            <th className="w-[12%] overflow-hidden text-ellipsis whitespace-nowrap px-3 py-3 text-right font-medium">{t('split.table.share')}</th>
            <th className="w-[16%] overflow-hidden text-ellipsis whitespace-nowrap px-3 py-3 text-right font-medium">{t('split.table.actualPrice')}</th>
            <th className="w-[16%] overflow-hidden text-ellipsis whitespace-nowrap px-3 py-3 text-right font-medium">{t('split.table.tokens')}</th>
            <th className="w-[10%] overflow-hidden text-ellipsis whitespace-nowrap px-3 py-3 text-right font-medium">{t('split.table.requests')}</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => {
            const name = item.api_key_name || item.api_key_id
            const actualPrice = totalPrice == null ? null : totalPrice * item.cost_share

            return (
              <tr
                key={item.api_key_id}
                className="border-t border-[hsl(var(--glass-divider))] [&>td]:transition-colors hover:[&>td]:bg-[hsl(var(--surface-subtle))] hover:[&>td:first-child]:rounded-l-md hover:[&>td:last-child]:rounded-r-md"
              >
                <td className="overflow-hidden px-3 py-2.5 align-middle">
                  <div className="truncate text-sm font-medium text-foreground" title={name}>
                    {name}
                  </div>
                </td>
                <td className="overflow-hidden text-ellipsis whitespace-nowrap px-3 py-2.5 text-right align-middle tabular-nums">
                  {formatDashboardCurrency(item.estimated_cost, currency, 2)}
                </td>
                <td className="overflow-hidden text-ellipsis whitespace-nowrap px-3 py-2.5 text-right align-middle tabular-nums">
                  {formatPercent(item.cost_share, 1)}
                </td>
                <td className="overflow-hidden text-ellipsis whitespace-nowrap px-3 py-2.5 text-right align-middle font-semibold tabular-nums text-primary">
                  {actualPrice == null ? '-' : formatPriceValue(actualPrice)}
                </td>
                <td className="overflow-hidden text-ellipsis whitespace-nowrap px-3 py-2.5 text-right align-middle tabular-nums">
                  {formatCompactNumber(item.total_tokens)}
                </td>
                <td className="overflow-hidden text-ellipsis whitespace-nowrap px-3 py-2.5 text-right align-middle tabular-nums">
                  {formatCompactNumber(item.request_count)}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function SplitMobileValue({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] text-muted-soft">{label}</div>
      <div className="mt-0.5 truncate text-sm font-medium tabular-nums text-foreground" title={value}>
        {value}
      </div>
    </div>
  )
}

function emptyChannelSplitSummary(allTime: boolean, dateFrom: string, dateTo: string): ChannelSplitSummary {
  return {
    site_id: '',
    site_ids: [],
    site_name: '',
    site_names: [],
    site_slug: '',
    site_type: '',
    target_label: '',
    oauth_connection_id: null,
    oauth_provider: null,
    oauth_account: null,
    range_all: allTime,
    date_from: allTime ? '' : dateFrom,
    date_to: allTime ? '' : dateTo,
    range_start: '',
    range_end: '',
    timezone: '',
    generated_at: '',
    request_count: 0,
    success_count: 0,
    failure_count: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    total_tokens: 0,
    estimated_cost: 0,
    currency: 'USD',
    api_key_count: 0,
  }
}

function buildSiteOptions(sites: Site[], labels: { deleted: string; oauth: string; sites: string }): MultiSelectOption[] {
  const activeOAuth = sortChannelSplitSites(sites.filter((site) => site.status !== 'deleted' && isOAuthSite(site)))
  const activeSites = sortChannelSplitSites(sites.filter((site) => site.status !== 'deleted' && !isOAuthSite(site)))
  const deletedOAuth = sortChannelSplitSites(sites.filter((site) => site.status === 'deleted' && isOAuthSite(site)))
  const deletedSites = sortChannelSplitSites(sites.filter((site) => site.status === 'deleted' && !isOAuthSite(site)))

  return [
    ...activeOAuth.map((site) => channelSplitSiteOption(site, labels, labels.oauth)),
    ...activeSites.map((site) => channelSplitSiteOption(site, labels, labels.sites)),
    ...deletedOAuth.map((site) => channelSplitSiteOption(site, labels, labels.deleted)),
    ...deletedSites.map((site) => channelSplitSiteOption(site, labels, labels.deleted)),
  ]
}

function sortChannelSplitSites(sites: Site[]) {
  return [...sites].sort((left, right) => channelSplitSiteSortLabel(left).localeCompare(channelSplitSiteSortLabel(right), 'zh-CN', { numeric: true, sensitivity: 'base' }))
}

function channelSplitSiteOption(site: Site, labels: { deleted: string }, group?: string): MultiSelectOption {
  const deleted = site.status === 'deleted'
  const name = channelSplitSiteDisplayName(site)
  return {
    value: site.id,
    label: deleted ? `${name} (${labels.deleted})` : name,
    description: [site.site_type, channelSplitSiteAccount(site), site.slug].filter(Boolean).join(' · '),
    icon: channelSplitSiteIcon(site),
    group,
  }
}

function channelSplitSiteIcon(site: Site) {
  return siteTypeIcon(site.site_type) || site.icon_url
}

function channelSplitSiteSortLabel(site: Site) {
  return `${channelSplitSiteDisplayName(site)} ${site.slug}`.trim()
}

function channelSplitSiteDisplayName(site: Site) {
  return site.name || channelSplitSiteAccount(site) || site.slug || site.id
}

function channelSplitSiteAccount(site: Site) {
  return site.oauth_account?.email || site.oauth_account?.account_id || ''
}

function initialChannelSplitSelection(target: ChannelSplitTarget | null | undefined, oauthConnections: OAuthConnectionListItem[]) {
  if (!target) return { siteIds: [] as string[], oauthConnectionId: undefined as string | undefined }
  if (target.type === 'site') {
    return { siteIds: [target.id], oauthConnectionId: undefined }
  }
  const connection = oauthConnections.find((item) => item.id === target.id)
  const siteId = target.siteIds?.[0] ?? connection?.site?.id ?? connection?.site_id ?? ''
  return {
    siteIds: siteId ? [siteId] : [],
    oauthConnectionId: target.id,
  }
}

function splitTargetTitle(summary: ChannelSplitSummary) {
  const target = summary.target_label || summary.site_name || summary.site_names?.[0] || '-'
  if (summary.oauth_account) return `${target} · ${summary.oauth_account}`
  return target
}

function channelSplitSummaryKey(summary: ChannelSplitSummary) {
  return [
    summary.site_ids.join(','),
    summary.oauth_connection_id ?? '',
    summary.range_all ? 'all' : 'range',
    summary.date_from,
    summary.date_to,
    summary.estimated_cost,
  ].join(':')
}

function formatPriceInput(value: number) {
  if (!Number.isFinite(value)) return ''
  return value.toFixed(2)
}

function parsePriceInput(value: string) {
  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed < 0) return null
  return parsed
}

function formatPriceValue(value: number | null) {
  if (value == null) return '-'
  if (!Number.isFinite(value)) return '-'
  return value.toFixed(2)
}

function naturalDayRange(days: number) {
  const to = startLocalDay(new Date())
  const from = addLocalDays(to, -days + 1)
  return { from, to }
}

function startLocalDay(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate())
}

function addLocalDays(date: Date, days: number) {
  const next = new Date(date)
  next.setDate(next.getDate() + days)
  return startLocalDay(next)
}

function formatDateParam(date: Date | undefined) {
  if (!date) return ''
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function sameDayRange(left: { from: Date; to: Date }, right: { from: Date; to: Date }) {
  return sameDay(left.from, right.from) && sameDay(left.to, right.to)
}

function sameDay(left: Date, right: Date) {
  return left.getFullYear() === right.getFullYear() &&
    left.getMonth() === right.getMonth() &&
    left.getDate() === right.getDate()
}
