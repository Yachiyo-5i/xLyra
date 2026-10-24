import { useState, type KeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { CalendarDays, LoaderCircle, RotateCcw, SlidersHorizontal, X } from 'lucide-react'
import { MobileFilterBar } from '@/components/common/mobile-filter-bar'
import { TokenUsageHoverCard, type TokenUsageBreakdown } from '@/components/common/token-usage-hover-card'
import { Button } from '@/components/ui/button'
import { DateTimePicker } from '@/components/ui/date-time-picker'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { filterTriggerActiveClass, filterTriggerClass } from '@/components/ui/field-styles'
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
import { requestTpmTextClassName } from '@/features/requests/lib/request-badge-styles'
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

  const hasDateRange = Boolean(filters.createdFromDate || filters.createdToDate)
  // 时间范围、隐藏无站点、自动刷新都放在抽屉里；有筛选生效时按钮带圆点提示
  const moreActive = hasDateRange || filters.hideWithoutSite

  return (
    <>
      <MobileFilterBar
        search={{
          value: searchInput,
          onChange: (event) => setSearchInput(event.target.value),
          onBlur: applySearchInputs,
          onKeyDown: handleSearchKeyDown,
          placeholder: t('filters.searchUnified'),
        }}
        actions={
          <>
            <Button
              type="button"
              variant="secondary"
              size="icon"
              className="relative rounded-full"
              aria-label={t('mobile.filters')}
              onClick={() => setSheetOpen(true)}
            >
              <SlidersHorizontal />
              {moreActive ? <span className="absolute right-2 top-2 size-1.5 rounded-full bg-tint" /> : null}
            </Button>
            <Button
              type="button"
              variant="secondary"
              size="icon"
              className="rounded-full"
              aria-label={t('filters.refresh')}
              disabled={isFetching}
              onClick={onRefresh}
            >
              {isFetching ? <LoaderCircle className="animate-spin" /> : <RotateCcw />}
            </Button>
          </>
        }
        filters={
          <>
            <button
              type="button"
              className={cn(filterTriggerClass, hasDateRange && filterTriggerActiveClass)}
              onClick={() => setSheetOpen(true)}
            >
              <CalendarDays className="size-3.5" />
              {hasDateRange
                ? `${formatShortDateTime(mergeDateAndTime(filters.createdFromDate, filters.createdFromTime))} → ${formatShortDateTime(mergeDateAndTime(filters.createdToDate, filters.createdToTime))}`
                : t('mobile.timeRange')}
            </button>
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
          </>
        }
        summary={
          <>
            {/* 当前筛选结果的汇总：一行小字，窄屏也能完整显示（Tokens 可点开明细） */}
            <div className="flex min-w-0 flex-1 flex-wrap items-center gap-x-1.5 gap-y-0.5 tabular-nums">
              <span className="text-tint" title={totalCostSupported ? undefined : t('filters.totalCostUnsupported')}>
                {totalCostLabel}
              </span>
              <span aria-hidden="true">·</span>
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
                <span>{totalTokensLabel} tokens</span>
              </TokenUsageHoverCard>
              <span aria-hidden="true">·</span>
              <span className="text-info" title={t('filters.rpmTooltip')}>RPM {formatInteger(rateLimitUsage?.rpm ?? 0)}</span>
              <span aria-hidden="true">·</span>
              <span className={requestTpmTextClassName} title={tpmTitle}>TPM {tpmLabel}</span>
            </div>
            {hasFilters ? (
              <button type="button" className="shrink-0 text-label-2 transition-colors active:text-label" onClick={clearFilters}>
                {t('filters.clearFilters')}
              </button>
            ) : null}
          </>
        }
      />

      <Draw open={sheetOpen} onOpenChange={setSheetOpen}>
        <DrawContent>
          <DrawHeader>
            <DrawTitle>{t('mobile.filters')}</DrawTitle>
          </DrawHeader>
          <DrawBody className="space-y-4">
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
              <label className="flex h-10 min-w-0 items-center gap-2 px-1 text-body text-label">
                <Switch
                  checked={filters.hideWithoutSite}
                  onCheckedChange={(checked) => patchFilters({ hideWithoutSite: checked })}
                  aria-label={t('filters.hideWithoutSiteLabel')}
                />
                <span className="min-w-0 truncate">{t('filters.hideWithoutSite')}</span>
              </label>
              <label className="flex h-10 min-w-0 items-center gap-2 px-1 text-body text-label">
                <Switch
                  checked={autoRefresh}
                  onCheckedChange={onAutoRefreshChange}
                  aria-label={t('filters.autoRefreshLabel')}
                />
                <span className="min-w-0 truncate">{t('filters.autoRefresh')}</span>
              </label>
            </div>
          </DrawBody>
          <DrawFooter
            cancel={(
              <Button type="button" variant="secondary" onClick={clearFilters} disabled={!hasFilters}>
                <X className="h-4 w-4" />
                {t('filters.clearFilters')}
              </Button>
            )}
            confirm={(
              <Button type="button" onClick={() => {
                applySearchInputs()
                setSheetOpen(false)
              }}>
                {t('mobile.done')}
              </Button>
            )}
          />
        </DrawContent>
      </Draw>
    </>
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
