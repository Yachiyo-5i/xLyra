import { useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { CalendarDays, LoaderCircle, RotateCcw, Search } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { TokenUsageHoverCard, type TokenUsageBreakdown } from '@/components/common/token-usage-hover-card'
import { Button } from '@/components/ui/button'
import { DateTimePicker } from '@/components/ui/date-time-picker'
import { filterTriggerActiveClass, filterTriggerClass, popoverPanelClass } from '@/components/ui/field-styles'
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
import { cn } from '@/lib/utils'

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

  const hasDateRange = Boolean(filters.createdFromDate || filters.createdToDate)
  const fromDate = mergeDateAndTime(filters.createdFromDate, filters.createdFromTime)
  const toDate = mergeDateAndTime(filters.createdToDate, filters.createdToTime)

  return (
    <div className="space-y-3">
      {/* 第一行：搜索 + 筛选胶囊（时间范围 / 状态 / 站点 / 下游密钥），右侧刷新 */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-3">
          <label className="relative w-52 shrink-0">
            <Search className="pointer-events-none absolute left-3 top-1/2 z-10 size-4 -translate-y-1/2 text-label-3" />
            <Input
              value={searchInput}
              onChange={(event) => setSearchInput(event.target.value)}
              onBlur={applySearchInputs}
              onKeyDown={handleSearchKeyDown}
              placeholder={t('filters.searchUnified')}
              className="pl-10"
            />
          </label>

          {/* 时间范围：一枚 filter 胶囊，点开后在面板里分别选择开始与结束时间 */}
          <PopoverPrimitive.Root>
            <PopoverPrimitive.Trigger asChild>
              <button type="button" className={cn(filterTriggerClass, hasDateRange && filterTriggerActiveClass)}>
                <CalendarDays className="size-3.5" />
                {hasDateRange
                  ? `${formatShortDateTime(fromDate)} → ${formatShortDateTime(toDate)}`
                  : t('mobile.timeRange')}
              </button>
            </PopoverPrimitive.Trigger>
            <PopoverPrimitive.Portal>
              <PopoverPrimitive.Content
                align="start"
                sideOffset={8}
                className={cn(popoverPanelClass, 'z-[110] w-[18rem] space-y-2 p-3')}
              >
                <div className="text-caption-1 text-label-3">{t('filters.dateFrom')}</div>
                <DateTimePicker
                  value={fromDate}
                  onValueChange={(date) => patchDateTimeFilter('createdFrom', date)}
                  placeholder={`${t('filters.dateFrom')} / ${t('filters.timeFrom')}`}
                  minuteStep={1}
                  disableFutureDates
                  className="w-full"
                />
                <div className="pt-1 text-caption-1 text-label-3">{t('filters.dateTo')}</div>
                <DateTimePicker
                  value={toDate}
                  onValueChange={(date) => patchDateTimeFilter('createdTo', date)}
                  placeholder={`${t('filters.dateTo')} / ${t('filters.timeTo')}`}
                  minuteStep={1}
                  disableFutureDates
                  className="w-full"
                />
              </PopoverPrimitive.Content>
            </PopoverPrimitive.Portal>
          </PopoverPrimitive.Root>

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

        <div className="flex shrink-0 items-center gap-4">
          <Button variant="secondary" onClick={onRefresh} disabled={isFetching}>
            {isFetching ? <LoaderCircle className="animate-spin" /> : <RotateCcw />}
            {t('filters.refresh')}
          </Button>
        </div>
      </div>

      {/* 第二行：当前筛选结果的汇总；右侧是清空筛选与两个开关 */}
      <div className="flex min-h-6 items-center justify-between gap-3">
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          <Badge
            variant="accent"
            className="tabular-nums"
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
            <Badge variant="neutral" className="tabular-nums">
              {t('filters.totalTokens', { tokens: totalTokensLabel })}
            </Badge>
          </TokenUsageHoverCard>
          <Badge variant="info" className="tabular-nums" title={t('filters.rpmTooltip')}>
            RPM {formatInteger(rateLimitUsage?.rpm ?? 0)}
          </Badge>
          <Badge variant="neutral" className={cn('tabular-nums', requestTpmBadgeClassName)} title={tpmTitle}>
            TPM {tpmLabel}
          </Badge>
        </div>
        <div className="flex shrink-0 items-center gap-4">
          {hasFilters ? (
            <button
              type="button"
              className="shrink-0 text-footnote text-label-2 transition-colors hover:text-label"
              onClick={() => {
                setSearchInput('')
                onFiltersChange({
                  ...getEmptyRequestFilters(),
                  hideWithoutSite: filters.hideWithoutSite,
                })
              }}
            >
              {t('filters.clearFilters')}
            </button>
          ) : null}
          <label className="flex items-center gap-2 text-footnote text-label-2">
            <Switch
              checked={filters.hideWithoutSite}
              onCheckedChange={(checked) => patchFilters({ hideWithoutSite: checked })}
              aria-label={t('filters.hideWithoutSiteLabel')}
            />
            <span>{t('filters.hideWithoutSite')}</span>
          </label>
          <label className="flex items-center gap-2 text-footnote text-label-2">
            <Switch
              checked={autoRefresh}
              onCheckedChange={onAutoRefreshChange}
              aria-label={t('filters.autoRefreshLabel')}
            />
            <span>{t('filters.autoRefresh')}</span>
          </label>
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

/** 时间胶囊上显示的短时间：MM-DD HH:mm，未设置的一端显示省略号 */
function formatShortDateTime(date: Date | undefined) {
  if (!date) return '…'
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}
