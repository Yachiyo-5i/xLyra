import { useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { LoaderCircle, RotateCcw, Search, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { TokenUsageHoverCard, type TokenUsageBreakdown } from '@/components/common/token-usage-hover-card'
import { Button } from '@/components/ui/button'
import { DateTimePicker } from '@/components/ui/date-time-picker'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { formatCompactNumber } from '@/features/dashboard/lib/dashboard-utils'
import type { DownstreamAPIKey } from '@/features/api-keys/api/api-keys'
import type { Site } from '@/features/sites/api/sites'
import {
  hasRequestFilters,
  getEmptyRequestFilters,
  type RequestFilterState,
  type SuccessFilter,
} from '@/features/requests/lib/types'
import { requestTpmBadgeClassName } from '@/features/requests/lib/request-badge-styles'

type RequestsFilterBarProps = {
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

export function RequestsFilterBar({
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
}: RequestsFilterBarProps) {
  const { t } = useTranslation('requests')
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

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex min-w-0 flex-wrap items-center gap-3">
          <DateTimePicker
            value={mergeDateAndTime(filters.createdFromDate, filters.createdFromTime)}
            onValueChange={(date) => patchDateTimeFilter('createdFrom', date)}
            placeholder={`${t('filters.dateFrom')} / ${t('filters.timeFrom')}`}
            minuteStep={1}
            disableFutureDates
            className="w-64"
            triggerClassName="h-10"
          />
          <DateTimePicker
            value={mergeDateAndTime(filters.createdToDate, filters.createdToTime)}
            onValueChange={(date) => patchDateTimeFilter('createdTo', date)}
            placeholder={`${t('filters.dateTo')} / ${t('filters.timeTo')}`}
            minuteStep={1}
            disableFutureDates
            className="w-64"
            triggerClassName="h-10"
          />
        </div>

        <div className="ml-auto flex flex-nowrap items-center justify-end gap-2">
          <Badge
            variant="accent"
            className="h-8 w-fit rounded-md px-2.5 text-xs tabular-nums"
            title={totalCostSupported ? undefined : t('filters.totalCostUnsupported')}
          >
            {t('filters.totalCost', { cost: totalCostLabel })}
          </Badge>
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
            <Badge
              variant="neutral"
              className="h-8 w-fit rounded-md px-2.5 text-xs tabular-nums"
            >
              {t('filters.totalTokens', { tokens: totalTokensLabel })}
            </Badge>
          </TokenUsageHoverCard>
          <Badge variant="info" className="h-8 w-fit rounded-md px-2.5 text-xs tabular-nums" title={t('filters.rpmTooltip')}>
            RPM {formatInteger(rateLimitUsage?.rpm ?? 0)}
          </Badge>
          <Badge
            variant="neutral"
            className={`h-8 w-fit rounded-md px-2.5 text-xs tabular-nums ${requestTpmBadgeClassName}`}
            title={tpmTitle}
          >
            TPM {tpmLabel}
          </Badge>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="flex min-w-0 flex-wrap items-center gap-3">
          <label className="relative w-52">
            <Search className="text-muted-soft pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
            <Input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              onBlur={applySearchInputs}
              onKeyDown={handleSearchKeyDown}
              placeholder={t('filters.searchUnified')}
              className="pl-10"
            />
          </label>
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

        <div className="ml-auto flex flex-nowrap items-center justify-end gap-3">
          {hasFilters ? (
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setSearchInput('')
                onFiltersChange({
                  ...getEmptyRequestFilters(),
                  hideWithoutSite: filters.hideWithoutSite,
                })
              }}
            >
              <X className="h-4 w-4" />
              {t('filters.clearFilters')}
            </Button>
          ) : null}
          <label className="flex h-10 items-center gap-2 px-1 text-sm text-foreground">
            <Switch
              checked={filters.hideWithoutSite}
              onCheckedChange={(checked) => patchFilters({ hideWithoutSite: checked })}
              aria-label={t('filters.hideWithoutSiteLabel')}
            />
            <span>{t('filters.hideWithoutSite')}</span>
          </label>
          <label className="flex h-10 items-center gap-2 px-1 text-sm text-foreground">
            <Switch
              checked={autoRefresh}
              onCheckedChange={onAutoRefreshChange}
              aria-label={t('filters.autoRefreshLabel')}
            />
            <span>{t('filters.autoRefresh')}</span>
          </label>
          <Button variant="outline" onClick={onRefresh} disabled={isFetching}>
            {isFetching ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
            {t('filters.refresh')}
          </Button>
        </div>
      </div>
    </div>
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
