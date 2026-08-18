import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { DatePicker } from '@/components/ui/date-picker'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
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
  selectedCurrency: string
  siteOptions: MultiSelectOption[]
  modelOptions: MultiSelectOption[]
  apiKeyOptions: MultiSelectOption[]
  optionsError?: string
  onRetryOptions?: () => void
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
  selectedCurrency,
  siteOptions,
  modelOptions,
  apiKeyOptions,
  optionsError,
  onRetryOptions,
  onChange,
  dataFrom,
  updatedAt,
  language,
  isFetching,
  onRefresh,
}: AnalyticsFilterBarProps) {
  const { t } = useTranslation('analytics')

  // 预设选项（不含 custom，custom 只在已自定义时作为展示态）
  const presetSelectItems: Array<{ label: string; value: AnalyticsRangePreset }> = [
    { label: t('filters.ranges.today'), value: 'today' },
    { label: t('filters.ranges.yesterday'), value: 'yesterday' },
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
          value={selectedCurrency}
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

      {optionsError ? (
        <div className="flex basis-full items-center justify-between gap-3 rounded-lg border border-red-400/20 bg-red-400/10 px-3 py-2 text-sm text-red-100">
          <span>{t('page.optionsLoadFailed')}: {optionsError}</span>
          {onRetryOptions ? <Button variant="outline" size="sm" onClick={onRetryOptions}>{t('page.retry')}</Button> : null}
        </div>
      ) : null}
    </div>
  )
}
