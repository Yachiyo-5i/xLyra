import { useState, type KeyboardEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Filter, LoaderCircle, RefreshCw, Search, X } from 'lucide-react'
import { Badge, type BadgeProps } from '@/components/ui/badge'
import { TokenUsageHoverCard, type TokenUsageBreakdown } from '@/components/common/token-usage-hover-card'
import { Button } from '@/components/ui/button'
import { DateTimePicker } from '@/components/ui/date-time-picker'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import type { Site } from '@/features/sites/api/sites'
import {
  hasRequestFilters,
  getEmptyRequestFilters,
  type RequestFilterState,
  type SuccessFilter,
} from '@/features/requests/lib/types'
import { formatCompactNumber } from '@/features/dashboard/lib/dashboard-utils'
import { requestTpmBadgeClassName } from '@/features/requests/lib/request-badge-styles'
import { cn } from '@/lib/utils'

type RequestsMobileFilterBarProps = {
  filters: RequestFilterState
  sites: Site[]
  apiKeys: DownstreamAPIKey[]
  autoRefresh: boolean
  isFetching: boolean
  totalCost?: number | null
  tokenUsage?: TokenUsageBreakdown | null
  totalCostSupported?: boolean
  totalCostLoading?: boolean
  currency?: string | null
  rateLimitUsage?: {
    rpm: number
    tpm: number
  }
  onFiltersChange: (filters: RequestFilterState) => void
  onAutoRefreshChange: (enabled: boolean) => void
  onRefresh: () => void
}

export function RequestsMobileFilterBar({
  filters,
  sites,
  apiKeys,
  autoRefresh,
  isFetching,
  totalCost,
  tokenUsage,
  totalCostSupported = true,
  totalCostLoading = false,
  currency,
  rateLimitUsage,
  onFiltersChange,
  onAutoRefreshChange,
  onRefresh,
}: RequestsMobileFilterBarProps) {
  const { t } = useTranslation('requests')
  const [sheetOpen, setSheetOpen] = useState(false)
  const [searchDraft, setSearchDraft] = useState({ base: filters.search, value: filters.search })
  const searchInput = searchDraft.base === filters.search ? searchDraft.value : filters.search
  const hasFilters = hasRequestFilters(filters)
  const totalCostLabel = totalCostLoading
    ? '...'
    : totalCostSupported
      ? formatTotalCost(totalCost ?? 0, currency ?? 'USD')
      : '--'
  const totalTokensLabel = totalCostLoading
    ? '...'
    : totalCostSupported
      ? formatCompactNumber(tokenUsage?.total ?? 0)
      : '--'
  const tpmValue = rateLimitUsage?.tpm ?? 0
  const tpmLabel = formatCompactNumber(tpmValue)
  const tpmTitle = `${t('filters.tpmTooltip')}: ${formatInteger(tpmValue)}`

  function setSearchInput(value: string) {
    setSearchDraft({ base: filters.search, value })
  }

  function patchFilters(patch: Partial<RequestFilterState>) {
    onFiltersChange({ ...filters, ...patch })
  }

  function patchDateTimeFilter(
    prefix: 'createdFrom' | 'createdTo',
    date: Date | undefined,
  ) {
    patchFilters(prefix === 'createdFrom'
      ? {
          createdFromDate: date,
          createdFromTime: date ? formatTimeFilter(date) : undefined,
        }
      : {
          createdToDate: date,
          createdToTime: date ? formatTimeFilter(date) : undefined,
        })
  }

  function applySearchInputs() {
    const nextSearch = searchInput.trim()

    if (nextSearch === filters.search) {
      return
    }

    patchFilters({
      search: nextSearch,
      requestId: '',
      modelKey: '',
    })
  }

  function handleSearchKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'Enter') {
      applySearchInputs()
    }
  }

  function clearFilters() {
    setSearchInput('')
    onFiltersChange({
      ...getEmptyRequestFilters(),
      hideWithoutSite: filters.hideWithoutSite,
    })
  }

  return (
    <section className="space-y-2.5">
      <div className="flex gap-2 overflow-x-auto pb-0.5">
        <MetricBadge variant="accent" title={totalCostSupported ? undefined : t('filters.totalCostUnsupported')}>
          {t('filters.totalCost', { cost: totalCostLabel })}
        </MetricBadge>
        <TokenUsageHoverCard
          columns={tokenUsage ? [{ usage: tokenUsage }] : []}
          labels={{
            total: t('tokens.total'),
            input: t('tokens.input'),
            output: t('tokens.output'),
            cached: t('tokens.cached'),
            hitRate: t('tokens.hitRate'),
          }}
          disabled={!totalCostSupported || totalCostLoading || !tokenUsage}
        >
          <MetricBadge>{t('filters.totalTokens', { tokens: totalTokensLabel })}</MetricBadge>
        </TokenUsageHoverCard>
        <MetricBadge variant="info" title={t('filters.rpmTooltip')}>RPM {formatInteger(rateLimitUsage?.rpm ?? 0)}</MetricBadge>
        <MetricBadge variant="neutral" className={requestTpmBadgeClassName} title={tpmTitle}>TPM {tpmLabel}</MetricBadge>
      </div>

      <div className="grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2">
        <label className="relative min-w-0">
          <Search className="text-muted-soft pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
          <Input
            value={searchInput}
            onChange={(event) => setSearchInput(event.target.value)}
            onBlur={applySearchInputs}
            onKeyDown={handleSearchKeyDown}
            placeholder={t('filters.searchUnified')}
            className="h-10 pl-10"
          />
        </label>
        <Button
          type="button"
          variant={hasFilters ? 'secondary' : 'outline'}
          size="icon"
          aria-label={t('mobile.filters')}
          onClick={() => setSheetOpen(true)}
        >
          <Filter className="h-4 w-4" />
        </Button>
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={t('filters.refresh')}
          disabled={isFetching}
          onClick={onRefresh}
        >
          {isFetching ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
        </Button>
      </div>

      <div className={cn('grid items-center gap-2', hasFilters ? 'grid-cols-[minmax(0,1fr)_auto]' : 'grid-cols-1')}>
        <div className="flex min-w-0 items-center gap-2 overflow-x-auto pb-0.5">
          <Select value={filters.success} onValueChange={(value) => patchFilters({ success: value as SuccessFilter })}>
            <SelectTrigger variant="filter" filterLabel={t('table.headers.status')} active={filters.success !== 'all'}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent searchable={false} widthMode="content">
              <SelectItem value="all">{t('filters.allStatuses')}</SelectItem>
              <SelectItem value="success">{t('filters.success')}</SelectItem>
              <SelectItem value="failed">{t('filters.failure')}</SelectItem>
            </SelectContent>
          </Select>
          <Select value={filters.siteId} onValueChange={(value) => patchFilters({ siteId: value })}>
            <SelectTrigger variant="filter" filterLabel={t('table.headers.site')} active={filters.siteId !== 'all'}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent searchable={false} widthMode="content">
              <SelectItem value="all">{t('filters.allSites')}</SelectItem>
              {sites.map((site) => (
                <SelectItem key={site.id} value={site.id}>
                  {site.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={filters.apiKeyId} onValueChange={(value) => patchFilters({ apiKeyId: value })}>
            <SelectTrigger variant="filter" filterLabel={t('table.headers.apiKey')} active={filters.apiKeyId !== 'all'}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent searchable={false} widthMode="content">
              <SelectItem value="all">{t('filters.allApiKeys')}</SelectItem>
              {apiKeys.map((apiKey) => (
                <SelectItem key={apiKey.id} value={apiKey.id}>
                  {apiKey.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {hasFilters ? (
          <Button type="button" variant="outline" size="sm" className="h-10 shrink-0 px-2.5" onClick={clearFilters}>
            <X className="h-4 w-4" />
            {t('filters.clearFilters')}
          </Button>
        ) : null}
      </div>

      <Draw open={sheetOpen} onOpenChange={setSheetOpen}>
        <DrawContent className="max-h-[82vh]">
          <DrawHeader className="py-4">
            <DrawTitle>{t('mobile.filters')}</DrawTitle>
          </DrawHeader>
          <DrawBody className="space-y-4 px-4 py-4">
            <div className="grid grid-cols-2 gap-2">
              <DateTimePicker
                value={mergeDateAndTime(filters.createdFromDate, filters.createdFromTime)}
                onValueChange={(date) => patchDateTimeFilter('createdFrom', date)}
                placeholder={`${t('filters.dateFrom')} / ${t('filters.timeFrom')}`}
                minuteStep={1}
                disableFutureDates
                className="min-w-0"
                triggerClassName="h-10"
              />
              <DateTimePicker
                value={mergeDateAndTime(filters.createdToDate, filters.createdToTime)}
                onValueChange={(date) => patchDateTimeFilter('createdTo', date)}
                placeholder={`${t('filters.dateTo')} / ${t('filters.timeTo')}`}
                minuteStep={1}
                disableFutureDates
                className="min-w-0"
                triggerClassName="h-10"
              />
            </div>

            <div className="grid grid-cols-2 gap-2">
              <label className="flex h-10 min-w-0 items-center gap-2 px-1 text-sm text-foreground">
                <Switch
                  checked={filters.hideWithoutSite}
                  onCheckedChange={(checked) => patchFilters({ hideWithoutSite: checked })}
                  aria-label={t('filters.hideWithoutSiteLabel')}
                />
                <span className="min-w-0 truncate">{t('filters.hideWithoutSite')}</span>
              </label>
              <label className="flex h-10 min-w-0 items-center gap-2 px-1 text-sm text-foreground">
                <Switch
                  checked={autoRefresh}
                  onCheckedChange={onAutoRefreshChange}
                  aria-label={t('filters.autoRefreshLabel')}
                />
                <span className="min-w-0 truncate">{t('filters.autoRefresh')}</span>
              </label>
            </div>
          </DrawBody>
          <DrawFooter className="grid grid-cols-2 gap-2 px-4">
            <Button type="button" variant="outline" onClick={clearFilters} disabled={!hasFilters}>
              <X className="h-4 w-4" />
              {t('filters.clearFilters')}
            </Button>
            <Button type="button" onClick={() => {
              applySearchInputs()
              setSheetOpen(false)
            }}>
              {t('mobile.done')}
            </Button>
          </DrawFooter>
        </DrawContent>
      </Draw>
    </section>
  )
}

function MetricBadge({
  children,
  className,
  title,
  variant = 'neutral',
}: {
  children: ReactNode
  className?: string
  title?: string
  variant?: BadgeProps['variant']
}) {
  return (
    <Badge
      variant={variant}
      className={cn('h-8 shrink-0 rounded-md px-2.5 text-xs tabular-nums', className)}
      title={title}
    >
      {children}
    </Badge>
  )
}

function formatInteger(value: number) {
  return new Intl.NumberFormat('en-US', {
    maximumFractionDigits: 0,
  }).format(value)
}

function formatTotalCost(value: number, currency?: string | null) {
  const amount = new Intl.NumberFormat('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(value)
  return currency && currency.toUpperCase() !== 'USD' ? `${currency} ${amount}` : `$${amount}`
}

function mergeDateAndTime(date: Date | undefined, time: string | undefined) {
  if (!date) return undefined

  const [hour, minute] = (time ?? '00:00').split(':').map(Number)
  const nextDate = new Date(date)
  nextDate.setHours(Number.isFinite(hour) ? hour : 0)
  nextDate.setMinutes(Number.isFinite(minute) ? minute : 0)
  nextDate.setSeconds(0, 0)
  return nextDate
}

function formatTimeFilter(date: Date) {
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}
