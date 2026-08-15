import { useQuery } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { DatePicker } from '@/components/ui/date-picker'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { listDownstreamAPIKeys, downstreamAPIKeyQueryKeys } from '@/features/api-keys/api/api-keys'
import { sortAPIKeysForDisplay } from '@/features/api-keys/lib/api-key-utils'
import { sitesQueryKeys, listSites } from '@/features/sites/api/sites'
import { sortSitesForDisplay } from '@/features/sites/lib/site-utils'
import { modelNameIconInfo } from '@/features/sites/lib/model-icon'
import {
  presetRange,
  formatDateInput,
  parseDateInputToDate,
  type AnalyticsRangePreset,
} from '@/features/analytics/lib/analytics-utils'
import { formatDashboardRefreshTime } from '@/features/dashboard/lib/dashboard-utils'

export type AnalyticsFiltersState = {
  preset: AnalyticsRangePreset
  from: string
  to: string
  siteIds: string[]
  modelKeys: string[]
  apiKeyIds: string[]
  currency: string
}

type AnalyticsFilterBarProps = {
  filters: AnalyticsFiltersState
  availableCurrencies: string[]
  availableModelKeys: string[]
  onChange: (next: AnalyticsFiltersState) => void
  dataFrom?: string | null
  updatedAt?: string | null
  language?: string
  isFetching?: boolean
  onRefresh?: () => void
}

export function AnalyticsFilterBar({
  filters,
  availableCurrencies,
  availableModelKeys,
  onChange,
  dataFrom,
  updatedAt,
  language,
  isFetching,
  onRefresh,
}: AnalyticsFilterBarProps) {
  const { t } = useTranslation('analytics')

  const sitesQuery = useQuery({
    queryKey: sitesQueryKeys.list(),
    queryFn: () => listSites(),
    staleTime: 60_000,
  })
  const apiKeysQuery = useQuery({
    queryKey: downstreamAPIKeyQueryKeys.list(),
    queryFn: listDownstreamAPIKeys,
    staleTime: 60_000,
  })

  const siteOptions: MultiSelectOption[] = sortSitesForDisplay(sitesQuery.data?.items ?? [])
    .map((site) => ({ value: site.id, label: site.name }))

  const modelOptions: MultiSelectOption[] = [...availableModelKeys]
    .sort((a, b) => a.localeCompare(b))
    .map((key) => {
      const info = modelNameIconInfo(key)
      return { value: key, label: key, icon: info.iconPath }
    })

  const apiKeyOptions: MultiSelectOption[] = sortAPIKeysForDisplay(apiKeysQuery.data?.items ?? [])
    .map((key) => ({ value: key.id, label: key.name }))

  // 预设选项（不含 custom，custom 只在已自定义时作为展示态）
  const presetSelectItems: Array<{ label: string; value: AnalyticsRangePreset }> = [
    { label: t('filters.ranges.today'), value: 'today' },
    { label: t('filters.ranges.7d'), value: '7d' },
    { label: t('filters.ranges.30d'), value: '30d' },
    { label: t('filters.ranges.90d'), value: '90d' },
    { label: t('filters.ranges.all'), value: 'all' },
  ]
  if (filters.preset === 'custom') {
    presetSelectItems.push({ label: t('filters.ranges.custom'), value: 'custom' })
  }

  function handlePresetChange(preset: AnalyticsRangePreset) {
    if (preset === 'custom') return
    if (preset === 'all') {
      const from = dataFrom || '1970-01-01'
      const to = formatDateInput(new Date())
      onChange({ ...filters, preset, from, to })
      return
    }
    onChange({ ...filters, preset, ...presetRange(preset) })
  }

  const fromDateObj = parseDateInputToDate(filters.from)
  const toDateObj = parseDateInputToDate(filters.to)

  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* DatePicker 在最左侧 */}
      <div className="flex items-center gap-1.5">
        <DatePicker
          value={fromDateObj}
          onValueChange={(date) => {
            if (!date) return
            onChange({ ...filters, preset: 'custom', from: formatDateInput(date) })
          }}
          disableFutureDates
          disabledDates={toDateObj ? { after: toDateObj } : undefined}
          placeholder={t('filters.from')}
          clearable={false}
          triggerClassName="h-10"
        />
        <span className="text-xs text-faint">→</span>
        <DatePicker
          value={toDateObj}
          onValueChange={(date) => {
            if (!date) return
            onChange({ ...filters, preset: 'custom', to: formatDateInput(date) })
          }}
          disableFutureDates
          disabledDates={fromDateObj ? { before: fromDateObj } : undefined}
          placeholder={t('filters.to')}
          clearable={false}
          triggerClassName="h-10"
        />
      </div>

      {/* 时间范围预设：filter 风格 Select 下拉 */}
      <Select
        value={filters.preset}
        onValueChange={(value) => handlePresetChange(value as AnalyticsRangePreset)}
      >
        <SelectTrigger
          variant="filter"
          filterLabel={t('filters.ranges.label')}
          active
          className="h-10"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent searchable={false} widthMode="content">
          {presetSelectItems.map((item) => (
            <SelectItem key={item.value} value={item.value} textValue={item.label}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <MultiSelect
        triggerVariant="filter"
        dropdownWidthMode="content"
        value={filters.siteIds}
        options={siteOptions}
        triggerLabel={filters.siteIds.length ? t('filters.site') : t('filters.siteAll')}
        placeholder={t('filters.site')}
        searchPlaceholder={t('filters.search')}
        emptyText={t('filters.noOptions')}
        onChange={(siteIds) => onChange({ ...filters, siteIds })}
      />
      <MultiSelect
        triggerVariant="filter"
        dropdownWidthMode="content"
        value={filters.modelKeys}
        options={modelOptions}
        triggerLabel={filters.modelKeys.length ? t('filters.model') : t('filters.modelAll')}
        placeholder={t('filters.model')}
        searchPlaceholder={t('filters.search')}
        emptyText={t('filters.noOptions')}
        onChange={(modelKeys) => onChange({ ...filters, modelKeys })}
      />
      <MultiSelect
        triggerVariant="filter"
        dropdownWidthMode="content"
        value={filters.apiKeyIds}
        options={apiKeyOptions}
        triggerLabel={filters.apiKeyIds.length ? t('filters.apiKey') : t('filters.apiKeyAll')}
        placeholder={t('filters.apiKey')}
        searchPlaceholder={t('filters.search')}
        emptyText={t('filters.noOptions')}
        onChange={(apiKeyIds) => onChange({ ...filters, apiKeyIds })}
      />

      {availableCurrencies.length > 1 ? (
        <Select
          value={filters.currency || availableCurrencies[0]}
          onValueChange={(currency) => onChange({ ...filters, currency })}
        >
          <SelectTrigger variant="filter" filterLabel={t('filters.currency')} active className="h-10">
            <SelectValue placeholder={t('filters.currency')} />
          </SelectTrigger>
          <SelectContent searchable={false} widthMode="content">
            {availableCurrencies.map((currency) => (
              <SelectItem key={currency} value={currency} textValue={currency}>
                {currency}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : null}

      {/* 时间更新 + 刷新按钮：推到右端 */}
      {onRefresh ? (
        <div className="ml-auto flex items-center gap-2">
          {updatedAt ? (
            <span className="text-xs text-muted-soft">
              {t('page.update')} {formatDashboardRefreshTime(updatedAt, language)}
            </span>
          ) : null}
          <Button variant="secondary" size="sm" onClick={onRefresh} disabled={isFetching}>
            <RefreshCw className={isFetching ? 'h-4 w-4 animate-spin' : 'h-4 w-4'} />
            {t('page.refresh')}
          </Button>
        </div>
      ) : null}
    </div>
  )
}
