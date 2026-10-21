import { chartCursorFill, chartGridStroke, chartSliceStroke, chartTick } from '@/components/charts/chart-theme'
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  RotateCcw,
  X,
} from 'lucide-react'
import {
  Area,
  AreaChart,
  CartesianGrid,
  Cell,
  Legend,
  Line,
  LineChart,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  type TooltipContentProps,
  XAxis,
  YAxis,
} from 'recharts'
import { useTranslation } from 'react-i18next'
import { ErrorState } from '@/components/common/error-state'
import { PageHeader } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { DatePicker } from '@/components/ui/date-picker'
import { MultiSelect, type MultiSelectOption } from '@/components/ui/multi-select'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import type { AnalyticsUsage } from '@/features/analytics/api/analytics'
import type { AnalyticsFiltersState } from '@/features/analytics/components/analytics-filter-bar'
import { AnalyticsKpiCards } from '@/features/analytics/components/analytics-kpi-cards'
import { AnalyticsSlashTabs } from '@/features/analytics/components/analytics-panel'
import { AnalyticsUserRanking } from '@/features/analytics/components/analytics-user-ranking'
import {
  buildTrendChart,
  formatCompactTick,
  formatDateInput,
  formatHourLabel,
  formatLatencyMs,
  formatShortDate,
  formatTrendMetricValue,
  parseDateInputToDate,
  presetRange,
  type AnalyticsBreakdownDimension,
  type AnalyticsRangePreset,
  type AnalyticsTrendDimension,
  type AnalyticsTrendMetric,
  type AnalyticsTrendSeries,
} from '@/features/analytics/lib/analytics-utils'
import { ApiKeyContributionsPanel } from '@/features/dashboard/components'
import {
  dashboardChartColors,
  dashboardTooltipStyle,
  formatDollarTick,
} from '@/features/dashboard/components/chart-style'
import type { ApiKeyContributions } from '@/features/dashboard/lib/dashboard-utils'
import {
  formatDashboardCurrency,
  formatDashboardRefreshTime,
  formatDashboardTokens,
  formatPercent,
} from '@/features/dashboard/lib/dashboard-utils'
import { cn } from '@/lib/utils'

type MobileAnalyticsProps = {
  usage: AnalyticsUsage
  filters: AnalyticsFiltersState
  onFiltersChange: (next: AnalyticsFiltersState) => void
  availableCurrencies: string[]
  selectedCurrency: string
  siteOptions: MultiSelectOption[]
  modelOptions: MultiSelectOption[]
  apiKeyOptions: MultiSelectOption[]
  optionsError?: string
  onRetryOptions?: () => void
  trendMetric: AnalyticsTrendMetric
  trendDimension: AnalyticsTrendDimension
  onTrendMetricChange: (metric: AnalyticsTrendMetric) => void
  onTrendDimensionChange: (dimension: AnalyticsTrendDimension) => void
  apiKeyContributions: ApiKeyContributions | undefined
  contributionsError?: string
  onRetryContributions?: () => void
  contributionKeyId: string
  onContributionKeyChange: (keyId: string) => void
  onDrillDown: (dimension: AnalyticsBreakdownDimension, item: { key: string; id?: string | null }) => void
  onClearDrillDown: (dimension: AnalyticsBreakdownDimension) => void
  isFetching: boolean
  onRefresh: () => void
  language: string
}

export function MobileAnalytics({
  usage,
  filters,
  onFiltersChange,
  availableCurrencies,
  selectedCurrency,
  siteOptions,
  modelOptions,
  apiKeyOptions,
  optionsError,
  onRetryOptions,
  trendMetric,
  trendDimension,
  onTrendMetricChange,
  onTrendDimensionChange,
  apiKeyContributions,
  contributionsError,
  onRetryContributions,
  contributionKeyId,
  onContributionKeyChange,
  onDrillDown,
  onClearDrillDown,
  isFetching,
  onRefresh,
  language,
}: MobileAnalyticsProps) {
  const { t } = useTranslation('analytics')

  return (
    <div className="space-y-4 pb-2">
      <PageHeader
        eyebrow={t('page.eyebrow')}
        title={t('page.title')}
        description={t('page.description')}
        actions={
          <>
            <span className="text-footnote text-label-3">
              {t('page.update')} {formatDashboardRefreshTime(usage.meta.generated_at, language)}
            </span>
            <Button variant="secondary" size="sm" onClick={onRefresh} disabled={isFetching}>
              <RotateCcw className={cn('size-4', isFetching && 'animate-spin')} />
              {t('page.refresh')}
            </Button>
          </>
        }
      />

      <MobileFilters
        filters={filters}
        onFiltersChange={onFiltersChange}
        availableCurrencies={availableCurrencies}
        selectedCurrency={selectedCurrency}
        siteOptions={siteOptions}
        modelOptions={modelOptions}
        apiKeyOptions={apiKeyOptions}
        optionsError={optionsError}
        onRetryOptions={onRetryOptions}
        dataFrom={usage.meta.data_from}
      />

      <AnalyticsKpiCards usage={usage} compact />

      <MobileTrendPanel
        usage={usage}
        metric={trendMetric}
        dimension={trendDimension}
        onMetricChange={onTrendMetricChange}
        onDimensionChange={onTrendDimensionChange}
      />

      <AnalyticsUserRanking className="h-[320px] rounded-2xl" usage={usage} metric={trendMetric} />

      <MobileCostBreakdown
        usage={usage}
        onDrillDown={onDrillDown}
        onClearDrillDown={onClearDrillDown}
        activeSiteIds={filters.siteIds}
        activeModelKeys={filters.modelKeys}
        activeApiKeyIds={filters.apiKeyIds}
      />

      {contributionsError ? (
        <ErrorState
          title={t('page.contributionsLoadFailed')}
          description={contributionsError}
          action={onRetryContributions ? <Button variant="outline" onClick={onRetryContributions}>{t('page.retry')}</Button> : undefined}
        />
      ) : apiKeyContributions ? (
        <ApiKeyContributionsPanel
          className="h-[400px] rounded-2xl"
          contributions={apiKeyContributions}
          selectedKeyId={contributionKeyId}
          onSelectedKeyChange={onContributionKeyChange}
        />
      ) : (
        <Card className="h-[400px] p-5"><Skeleton className="h-full w-full" /></Card>
      )}
    </div>
  )
}

/* ---------------------------------- 筛选区 ---------------------------------- */

type MobileFiltersProps = {
  filters: AnalyticsFiltersState
  onFiltersChange: (next: AnalyticsFiltersState) => void
  availableCurrencies: string[]
  selectedCurrency: string
  siteOptions: MultiSelectOption[]
  modelOptions: MultiSelectOption[]
  apiKeyOptions: MultiSelectOption[]
  optionsError?: string
  onRetryOptions?: () => void
  dataFrom?: string | null
}

function MobileFilters({
  filters,
  onFiltersChange,
  availableCurrencies,
  selectedCurrency,
  siteOptions,
  modelOptions,
  apiKeyOptions,
  optionsError,
  onRetryOptions,
  dataFrom,
}: MobileFiltersProps) {
  const { t } = useTranslation('analytics')

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
      onFiltersChange({ ...filters, preset, from, to })
      return
    }
    onFiltersChange({ ...filters, preset, ...presetRange(preset) })
  }

  const fromDateObj = parseDateInputToDate(filters.from)
  const toDateObj = parseDateInputToDate(filters.to)

  return (
    <section className="space-y-2">
      {/* 全部筛选横向铺开，限制在视口宽度内，超出部分横向滚动 */}
      {/* 筛选胶囊横向滚动：右侧淡出提示还有更多，末尾留白让最后一个胶囊能完整滑出 */}
      <div className="scrollbar-hidden -mr-4 flex items-center gap-2 overflow-x-auto pb-1 pr-8 [mask-image:linear-gradient(to_right,#000_calc(100%-40px),transparent)]">
        <Select
          value={filters.preset}
          onValueChange={(value) => handlePresetChange(value as AnalyticsRangePreset)}
        >
          <SelectTrigger
            variant="filter"
            filterLabel={t('filters.ranges.label')}
            active
            className="h-10 shrink-0"
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
          onChange={(siteIds) => onFiltersChange({ ...filters, siteIds })}
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
          onChange={(modelKeys) => onFiltersChange({ ...filters, modelKeys })}
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
          onChange={(apiKeyIds) => onFiltersChange({ ...filters, apiKeyIds })}
        />

        {availableCurrencies.length > 1 ? (
          <Select
            value={selectedCurrency}
            onValueChange={(currency) => onFiltersChange({ ...filters, currency })}
          >
            <SelectTrigger variant="filter" filterLabel={t('filters.currency')} active className="h-10 shrink-0">
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
      </div>

      {filters.preset === 'custom' ? (
        <div className="flex items-center gap-1.5">
          <DatePicker
            value={fromDateObj}
            onValueChange={(date) => {
              if (!date) return
              onFiltersChange({ ...filters, preset: 'custom', from: formatDateInput(date) })
            }}
            disableFutureDates
            disabledDates={toDateObj ? { after: toDateObj } : undefined}
            placeholder={t('filters.from')}
            clearable={false}
            triggerClassName="h-10"
          />
          <span className="text-footnote text-label-3">→</span>
          <DatePicker
            value={toDateObj}
            onValueChange={(date) => {
              if (!date) return
              onFiltersChange({ ...filters, preset: 'custom', to: formatDateInput(date) })
            }}
            disableFutureDates
            disabledDates={fromDateObj ? { before: fromDateObj } : undefined}
            placeholder={t('filters.to')}
            clearable={false}
            triggerClassName="h-10"
          />
        </div>
      ) : null}

      {optionsError ? (
        <div className="flex items-center justify-between gap-3 rounded-inner bg-danger/12 px-3 py-2 text-footnote text-danger">
          <span>{t('page.optionsLoadFailed')}: {optionsError}</span>
          {onRetryOptions ? <Button variant="outline" size="sm" onClick={onRetryOptions}>{t('page.retry')}</Button> : null}
        </div>
      ) : null}
    </section>
  )
}

/* ---------------------------------- KPI 卡 ---------------------------------- */





/* ---------------------------------- 趋势图 ---------------------------------- */

type MobileTrendPanelProps = {
  usage: AnalyticsUsage
  metric: AnalyticsTrendMetric
  dimension: AnalyticsTrendDimension
  onMetricChange: (metric: AnalyticsTrendMetric) => void
  onDimensionChange: (dimension: AnalyticsTrendDimension) => void
}

function MobileTrendPanel({
  usage,
  metric,
  dimension,
  onMetricChange,
  onDimensionChange,
}: MobileTrendPanelProps) {
  const { t } = useTranslation('analytics')
  const lineOnly = metric === 'latency'

  const isHour = usage.meta.granularity === 'hour'
  const xTickFormatter = isHour ? formatHourLabel : formatShortDate

  const { data, series } = useMemo(() => buildTrendChart(usage, metric), [usage, metric])
  const seriesWithColor = series.map((item, index) => ({
    ...item,
    color: dashboardChartColors[index % dashboardChartColors.length],
  }))

  const formatValue = (value: number) => formatTrendMetricValue(value, metric, usage.meta.currency)
  const tickFormatter = (value: number) => {
    if (metric === 'cost') return formatDollarTick(value)
    if (metric === 'latency') return formatLatencyMs(value)
    return formatCompactTick(value)
  }

  const hasData = series.length > 0 && data.length > 0

  return (
    <Card className="p-4">
      <div className="mb-3 space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
          <AnalyticsSlashTabs
            value={metric}
            onValueChange={onMetricChange}
            items={[
              { label: t('trend.metrics.cost'), value: 'cost' as const },
              { label: t('trend.metrics.tokens'), value: 'tokens' as const },
              { label: t('trend.metrics.requests'), value: 'requests' as const },
              { label: t('trend.metrics.latency'), value: 'latency' as const },
            ]}
          />
          <AnalyticsSlashTabs
            value={dimension}
            onValueChange={onDimensionChange}
            items={[
              { label: t('trend.dimensions.site'), value: 'site' as const },
              { label: t('trend.dimensions.model'), value: 'model' as const },
            ]}
          />
        </div>
        <p className="text-footnote text-label-2">
          {isHour
            ? t('trend.hourDescription', { date: usage.meta.from })
            : t('trend.description', { from: usage.meta.from, to: usage.meta.to })}
        </p>
      </div>

      {/* 图例在图内占一部分高度，整体加高到 300，绘图区约 220 */}
      {hasData ? (
        <div className="h-[300px]">
          <MobileTrendChart
            data={data}
            series={seriesWithColor}
            lineOnly={lineOnly}
            tickFormatter={tickFormatter}
            formatValue={formatValue}
            xTickFormatter={xTickFormatter}
            isHour={isHour}
          />
        </div>
      ) : (
        <div className="flex h-[300px] items-center justify-center text-body text-label-2">
          {t('trend.empty')}
        </div>
      )}
    </Card>
  )
}

type ColoredSeries = AnalyticsTrendSeries & { color: string }

function MobileTrendChart({
  data,
  series,
  lineOnly,
  tickFormatter,
  formatValue,
  xTickFormatter,
  isHour,
}: {
  data: ReturnType<typeof buildTrendChart>['data']
  series: ColoredSeries[]
  lineOnly: boolean
  tickFormatter: (value: number) => string
  formatValue: (value: number) => string
  xTickFormatter: (value: string) => string
  isHour: boolean
}) {
  const xAxisInterval = isHour
    ? Math.max(0, Math.floor(data.length / 8) - 1)
    : data.length > 20 ? Math.floor(data.length / 6) : 0
  const margin = { top: 8, right: 4, bottom: 0, left: 0 }

  const xAxis = (
    <XAxis
      dataKey="date"
      interval={xAxisInterval}
      tickFormatter={xTickFormatter}
      tickLine={false}
      axisLine={false}
      tick={chartTick}
    />
  )
  const yAxis = (
    <YAxis
      tickFormatter={tickFormatter}
      tickLine={false}
      axisLine={false}
      width={44}
      tick={chartTick}
    />
  )
  const grid = <CartesianGrid stroke={chartGridStroke} vertical={false} />
  const tooltip = (
    <Tooltip
      cursor={{ fill: chartCursorFill }}
      content={(props) => (
        <MobileTrendTooltip {...props} series={series} formatValue={formatValue} />
      )}
    />
  )
  const legend = (
    <Legend
      iconType="square"
      iconSize={8}
      wrapperStyle={{ fontSize: 11, paddingTop: 4, color: 'var(--label-tertiary)' }}
      formatter={(value) => (
        <span style={{ color: 'var(--label-primary)' }}>{value}</span>
      )}
    />
  )

  if (lineOnly) {
    return (
      <ResponsiveContainer width="100%" height="100%">
        <LineChart accessibilityLayer={false} data={data} margin={margin}>
          {grid}
          {xAxis}
          {yAxis}
          {tooltip}
          {legend}
          {series.map((item) => (
            <Line
              key={item.key}
              type="monotone"
              dataKey={item.key}
              name={item.name}
              stroke={item.color}
              strokeWidth={2}
              dot={false}
              connectNulls
              activeDot={{ r: 4 }}
            />
          ))}
        </LineChart>
      </ResponsiveContainer>
    )
  }

  return (
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart accessibilityLayer={false} data={data} margin={margin}>
        {grid}
        {xAxis}
        {yAxis}
        {tooltip}
        {legend}
        {series.map((item) => (
          <Area
            key={item.key}
            type="monotone"
            dataKey={item.key}
            name={item.name}
            stackId="analytics"
            stroke={item.color}
            fill={item.color}
            fillOpacity={0.32}
          />
        ))}
      </AreaChart>
    </ResponsiveContainer>
  )
}

type MobileTrendTooltipProps = TooltipContentProps & {
  series: ColoredSeries[]
  formatValue: (value: number) => string
}

function MobileTrendTooltip({ active, label, payload, series, formatValue }: MobileTrendTooltipProps) {
  if (!active || !payload?.length) return null

  const seriesByKey = new Map(series.map((item) => [item.key, item]))
  const items = payload
    .map((item) => {
      const raw = item.value
      let value: number | null = null
      if (typeof raw === 'number') {
        value = raw
      } else if (Array.isArray(raw) && raw.length === 2 && typeof raw[0] === 'number' && typeof raw[1] === 'number') {
        value = raw[1] - raw[0]
      }
      const dataKey = String(item.dataKey ?? '')
      const seriesItem = seriesByKey.get(dataKey)
      return {
        key: dataKey,
        name: seriesItem?.name ?? String(item.name ?? dataKey),
        color: seriesItem?.color ?? item.color ?? 'var(--label-tertiary)',
        value,
      }
    })
    .filter((item): item is typeof item & { value: number } => item.value !== null && item.value > 0)
    .sort((a, b) => b.value - a.value)

  if (!items.length) return null
  const total = items.reduce((sum, item) => sum + item.value, 0)

  return (
    <div style={dashboardTooltipStyle} className="min-w-[148px] max-w-[240px]">
      <div className="mb-2 flex items-center justify-between gap-4 text-footnote">
        <span className="text-label-2">{label}</span>
        <span className="font-medium tabular-nums text-label">{formatValue(total)}</span>
      </div>
      <div className="space-y-1">
        {items.map((item) => (
          <div key={item.key} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 text-footnote text-label">
            <span className="flex min-w-0 items-center gap-1.5">
              <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: item.color }} />
              <span className="truncate">{item.name}</span>
            </span>
            <span className="tabular-nums">
              {formatValue(item.value)}
              {total > 0 ? ` · ${((item.value / total) * 100).toFixed(1)}%` : ''}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

/* --------------------------------- 费用构成 --------------------------------- */

const MOBILE_ROSE_SIZE = 150
const MOBILE_ROSE_INNER_R = 30
const MOBILE_ROSE_OUTER_MAX = 66
const MOBILE_ROSE_MIN_RATIO = 0.42

function mobileRoseOuterRadius(value: number, minVal: number, maxVal: number): number {
  if (maxVal <= 0) return MOBILE_ROSE_OUTER_MAX
  const outerMin = MOBILE_ROSE_OUTER_MAX * MOBILE_ROSE_MIN_RATIO
  if (minVal >= maxVal) return MOBILE_ROSE_OUTER_MAX
  const scale = Math.log(1 + value / minVal) / Math.log(1 + maxVal / minVal)
  return Math.round(outerMin + (MOBILE_ROSE_OUTER_MAX - outerMin) * scale)
}

type MobileRoseSlice = {
  key: string
  id?: string | null
  name: string
  value: number
  tokens: number
  color: string
  outerRadius: number
}

type MobileCostBreakdownProps = {
  usage: AnalyticsUsage
  onDrillDown: (dimension: AnalyticsBreakdownDimension, item: { key: string; id?: string | null }) => void
  onClearDrillDown: (dimension: AnalyticsBreakdownDimension) => void
  activeSiteIds: string[]
  activeModelKeys: string[]
  activeApiKeyIds: string[]
}

function MobileCostBreakdown({
  usage,
  onDrillDown,
  onClearDrillDown,
  activeSiteIds,
  activeModelKeys,
  activeApiKeyIds,
}: MobileCostBreakdownProps) {
  const { t } = useTranslation('analytics')
  const [dimension, setDimension] = useState<AnalyticsBreakdownDimension>('model')
  const [roseTooltipActive, setRoseTooltipActive] = useState<boolean | undefined>(undefined)
  const roseChartRef = useRef<HTMLDivElement>(null)
  const currency = usage.meta.currency || 'USD'

  useEffect(() => {
    function closeRoseTooltip(event: PointerEvent) {
      if (roseChartRef.current?.contains(event.target as Node)) return
      setRoseTooltipActive(false)
    }

    document.addEventListener('pointerdown', closeRoseTooltip)
    return () => document.removeEventListener('pointerdown', closeRoseTooltip)
  }, [])

  const rows = useMemo(() => {
    return [...usage.breakdowns[dimension]]
      .sort((a, b) => b.cost - a.cost)
      .filter((item) => item.cost > 0)
  }, [dimension, usage.breakdowns])

  const roseSlices = useMemo<MobileRoseSlice[]>(() => {
    if (!rows.length) return []
    const maxVal = rows[0].cost
    const minVal = rows[rows.length - 1].cost
    return rows.map((item, index) => ({
      key: item.key,
      id: item.id,
      name: item.label,
      value: item.cost,
      tokens: item.total_tokens,
      color: dashboardChartColors[index % dashboardChartColors.length],
      outerRadius: mobileRoseOuterRadius(item.cost, minVal, maxVal),
    }))
  }, [rows])

  const total = rows.reduce((sum, item) => sum + item.cost, 0)

  const activeKeys = dimension === 'site'
    ? activeSiteIds
    : dimension === 'model'
      ? activeModelKeys
      : activeApiKeyIds
  const hasActive = activeKeys.length > 0

  return (
    <Card className="p-4">
      <div className="mb-3 space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
          <div className="min-w-0">
            <h3 className="text-headline text-label">{t('donut.title')}</h3>
            <p className="text-footnote text-label-2">{t('donut.description')}</p>
          </div>
          <div className="flex items-center gap-2">
            {hasActive ? (
              <button
                type="button"
                onClick={() => onClearDrillDown(dimension)}
                className="inline-flex shrink-0 items-center gap-0.5 rounded-full bg-fill-3 px-1.5 py-0.5 text-footnote text-label-2 hover:text-label"
                title={t('donut.clearFilter')}
              >
                <X className="h-3 w-3" />
                {t('donut.clear')}
              </button>
            ) : null}
            <AnalyticsSlashTabs
              value={dimension}
              onValueChange={setDimension}
              items={[
                { label: t('dimensions.site'), value: 'site' as const },
                { label: t('dimensions.model'), value: 'model' as const },
                { label: t('dimensions.apiKey'), value: 'api_key' as const },
              ]}
            />
          </div>
        </div>
      </div>

      {rows.length ? (
        <div className="flex flex-col gap-3">
          {/* 玫瑰图：固定 150px，圆心居中显示总费用 */}
          <div
            ref={roseChartRef}
            className="relative mx-auto"
            style={{ width: MOBILE_ROSE_SIZE, height: MOBILE_ROSE_SIZE }}
            onPointerDownCapture={(event) => {
              const target = event.target
              setRoseTooltipActive(target instanceof Element && Boolean(target.closest('.recharts-sector')))
            }}
          >
            <ResponsiveContainer width="100%" height="100%">
              <PieChart accessibilityLayer={false}>
                <Pie
                  data={roseSlices}
                  dataKey="value"
                  nameKey="name"
                  cx="50%"
                  cy="50%"
                  innerRadius={MOBILE_ROSE_INNER_R}
                  outerRadius={(point: MobileRoseSlice) => point.outerRadius}
                  paddingAngle={roseSlices.length > 8 ? 1 : 2}
                  stroke={chartSliceStroke}
                  strokeWidth={1.5}
                  isAnimationActive
                  animationDuration={500}
                  animationEasing="ease-out"
                  onClick={(entry) => {
                    const slice = entry as unknown as MobileRoseSlice
                    onDrillDown(dimension, { key: slice.key, id: slice.id })
                  }}
                >
                  {roseSlices.map((slice) => (
                    <Cell
                      key={slice.key}
                      fill={slice.color}
                      className="cursor-pointer"
                    />
                  ))}
                </Pie>
                <Tooltip
                  active={roseTooltipActive}
                  isAnimationActive={false}
                  wrapperStyle={{ zIndex: 10 }}
                  content={(props) => (
                    <MobileRoseTooltip {...props} currency={currency} total={total} />
                  )}
                />
              </PieChart>
            </ResponsiveContainer>
            <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
              <span
                className="leading-tight text-label-2"
                style={{ fontSize: Math.round(MOBILE_ROSE_SIZE * 0.065) }}
              >
                {t('donut.total')}
              </span>
              <span
                className="font-semibold tabular-nums leading-tight text-label"
                style={{ fontSize: Math.round(MOBILE_ROSE_SIZE * 0.08) }}
              >
                {formatDashboardCurrency(total, currency)}
              </span>
            </div>
          </div>

          {/* 占比列表：限高内部滚动 */}
          <div className="scrollbar-hidden flex max-h-[180px] flex-col divide-y divide-separator overflow-y-auto">
            {rows.map((item, index) => {
            const pct = total > 0 ? item.cost / total : 0
            const color = dashboardChartColors[index % dashboardChartColors.length]
            return (
              <button
                key={item.key}
                type="button"
                onClick={() => onDrillDown(dimension, { key: item.key, id: item.id })}
                className="flex min-w-0 items-center gap-1.5 py-2 text-left first:pt-0 last:pb-0"
              >
                <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: color }} />
                <span className="min-w-0 flex-1 truncate text-body text-label" title={item.label}>
                  {item.label}
                </span>
                <span className="shrink-0 text-footnote tabular-nums text-label-2">
                  {`${formatDashboardCurrency(item.cost, currency, 2)} · ${formatDashboardTokens(item.total_tokens)} · ${formatPercent(pct, 1)}`}
                </span>
              </button>
            )
          })}
          </div>
        </div>
      ) : (
        <div className="flex h-24 items-center justify-center text-body text-label-2">
          {t('donut.empty')}
        </div>
      )}
    </Card>
  )
}

function MobileRoseTooltip({ active, payload, currency, total }: TooltipContentProps & { currency: string; total: number }) {
  if (!active || !payload?.length) return null
  const item = payload[0]
  const value = Number(item.value ?? 0)
  const tokens = Number((item.payload as MobileRoseSlice | undefined)?.tokens ?? 0)
  return (
    <div style={dashboardTooltipStyle} className="min-w-[150px] text-footnote">
      <div className="mb-1 flex items-center gap-1.5 text-label">
        <span className="size-2 shrink-0 rounded-[2px]" style={{ backgroundColor: item.payload?.fill ?? item.color }} />
        <span className="truncate font-medium">{item.name}</span>
      </div>
      <div className="tabular-nums text-label-2">
        {formatDashboardCurrency(value, currency, 4)}
        {` · ${formatDashboardTokens(tokens)}`}
        {total > 0 ? ` · ${((value / total) * 100).toFixed(1)}%` : ''}
      </div>
    </div>
  )
}
