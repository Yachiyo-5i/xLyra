import { useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Card } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  type ApiKeyContributions,
  type ApiKeyContributionDay,
  formatDashboardCurrency,
  formatDashboardTokens,
} from '@/features/dashboard/lib/dashboard-utils'
import { cn } from '@/lib/utils'

type ApiKeyContributionsPanelProps = {
  className?: string
  contributions: ApiKeyContributions
  selectedKeyId: string
  onSelectedKeyChange: (keyId: string) => void
}

type ActiveDay = {
  day: ApiKeyContributionDay
  x: number
  y: number
}

type ContributionGridDay = ApiKeyContributionDay & {
  empty?: boolean
}

type ContributionMonthLabel = {
  label: string
  column: number
}

const contributionLevelClasses: Record<ApiKeyContributionDay['level'], string> = {
  0: 'bg-[hsl(var(--surface-subtle)/0.42)] border-[hsl(var(--glass-border))]',
  1: 'bg-[hsl(var(--primary)/0.18)] border-[hsl(var(--primary)/0.16)]',
  2: 'bg-[hsl(var(--primary)/0.34)] border-[hsl(var(--primary)/0.24)]',
  3: 'bg-[hsl(var(--primary)/0.58)] border-[hsl(var(--primary)/0.32)]',
  4: 'bg-[hsl(var(--primary)/0.86)] border-[hsl(var(--primary)/0.42)]',
}

const contributionLegendLevels: ApiKeyContributionDay['level'][] = [0, 1, 2, 3, 4]

export function ApiKeyContributionsPanel({
  className,
  contributions,
  selectedKeyId,
  onSelectedKeyChange,
}: ApiKeyContributionsPanelProps) {
  const { i18n, t } = useTranslation('dashboard')
  const [activeDay, setActiveDay] = useState<ActiveDay | null>(null)
  const scrollAreaRef = useRef<HTMLDivElement>(null)
  const hasKeys = contributions.keys.length > 0
  const gridRows = useMemo(() => contributionRows(contributions.days), [contributions.days])
  const monthLabels = useMemo(() => contributionMonthLabels(gridRows, i18n.language), [gridRows, i18n.language])
  const gridColumns = Math.max(1, Math.ceil(gridRows.length / 7))

  useLayoutEffect(() => {
    const scrollArea = scrollAreaRef.current
    if (!scrollArea || !hasKeys) return
    scrollArea.scrollLeft = scrollArea.scrollWidth
  }, [hasKeys, selectedKeyId, contributions.days])

  const updateActiveDay = (target: HTMLButtonElement, day: ApiKeyContributionDay) => {
    const parent = target.closest('[data-contributions-grid]')
    const parentRect = parent?.getBoundingClientRect()
    const rect = target.getBoundingClientRect()
    setActiveDay({
      day,
      x: parentRect ? rect.left - parentRect.left + rect.width / 2 : rect.width / 2,
      y: parentRect ? rect.top - parentRect.top : 0,
    })
  }

  return (
    <Card
      className={cn(
        'flex h-full min-h-0 flex-col rounded-lg p-5 [--contribution-gap:5px] [--contribution-size:16px] sm:[--contribution-size:18px]',
        className,
      )}
    >
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h3 className="text-base font-semibold text-foreground">{t('contributions.title')}</h3>
          <p className="text-sm text-muted-soft">{t('contributions.description')}</p>
        </div>
        <Select value={selectedKeyId} onValueChange={onSelectedKeyChange} disabled={!hasKeys}>
          <SelectTrigger
            variant="filter"
            filterLabel={t('contributions.apiKeyFilter')}
            active={hasKeys}
            className="h-8 max-w-[9.5rem] gap-1 rounded-md px-2.5 text-xs [&>svg]:h-3.5 [&>svg]:w-3.5"
          >
            <SelectValue placeholder={t('contributions.apiKeyFilter')} />
          </SelectTrigger>
          <SelectContent searchable={false} widthMode="content">
            {contributions.keys.map((item) => (
              <SelectItem key={item.id} value={item.id} textValue={item.name}>
                {item.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="mb-3 grid grid-cols-3 items-baseline gap-x-5 gap-y-1">
        <ContributionStat label={t('contributions.currentStreakLabel')} value={t('contributions.daysValue', { count: contributions.currentStreak })} />
        <ContributionStat label={t('contributions.longestStreakLabel')} value={t('contributions.daysValue', { count: contributions.longestStreak })} />
        <ContributionStat label={t('contributions.monthTokensLabel')} value={formatDashboardTokens(contributions.monthTokens)} />
      </div>

      <div
        data-contributions-grid
        className="relative min-h-0 flex-1 overflow-visible"
        onMouseLeave={() => setActiveDay(null)}
      >
        {hasKeys ? (
          <div className="h-full pb-1">
            <div className="grid min-h-0 grid-cols-[1.5rem_minmax(0,1fr)] items-start gap-x-2">
              <div className="pt-6">
                <div className="grid grid-rows-7 text-xs text-faint" style={{ rowGap: 'var(--contribution-gap)' }}>
                  <span />
                  <span>{t('contributions.weekdays.mon')}</span>
                  <span />
                  <span>{t('contributions.weekdays.wed')}</span>
                  <span />
                  <span>{t('contributions.weekdays.fri')}</span>
                  <span />
                </div>
              </div>
              <div ref={scrollAreaRef} className="scrollbar-hidden min-w-0 overflow-x-auto overflow-y-visible">
                <div className="w-max">
                  <div
                    className="mb-1 grid text-xs leading-5 text-faint"
                    style={{ gridTemplateColumns: `repeat(${gridColumns}, var(--contribution-size))`, columnGap: 'var(--contribution-gap)' }}
                  >
                    {monthLabels.map((item) => (
                      <span
                        key={`${item.label}-${item.column}`}
                        className="whitespace-nowrap"
                        style={{ gridColumnStart: item.column + 1 }}
                      >
                        {item.label}
                      </span>
                    ))}
                  </div>
                <div
                  className="grid grid-flow-col grid-rows-7"
                  style={{
                    gridTemplateColumns: `repeat(${gridColumns}, var(--contribution-size))`,
                    gap: 'var(--contribution-gap)',
                  }}
                >
                  {gridRows.map((day, index) => day.empty ? (
                    <span
                      key={`empty-${index}`}
                      className="rounded-[4px]"
                      style={{ width: 'var(--contribution-size)', height: 'var(--contribution-size)' }}
                    />
                  ) : (
                    <button
                      key={day.date}
                      type="button"
                      aria-label={t('contributions.dayLabel', {
                        date: day.date,
                        tokens: formatDashboardTokens(day.tokens),
                        cost: formatDashboardCurrency(day.cost, day.currency, 4),
                      })}
                      className={cn(
                        'rounded-[4px] border transition-[border-color,filter,transform] duration-150 hover:brightness-110 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] active:scale-95',
                        contributionLevelClasses[day.level],
                      )}
                      style={{ width: 'var(--contribution-size)', height: 'var(--contribution-size)' }}
                      onFocus={(event) => updateActiveDay(event.currentTarget, day)}
                      onMouseMove={(event) => updateActiveDay(event.currentTarget, day)}
                      onClick={(event) => updateActiveDay(event.currentTarget, day)}
                    />
                  ))}
                </div>
                </div>
              </div>
            </div>
            <div className="mt-3 flex items-center justify-end gap-1.5 text-[11px] leading-none text-faint">
              <span>{t('contributions.legendLess')}</span>
              {contributionLegendLevels.map((level) => (
                <span
                  key={level}
                  className={cn('h-3 w-3 rounded-[3px] border', contributionLevelClasses[level])}
                  aria-hidden="true"
                />
              ))}
              <span>{t('contributions.legendMore')}</span>
            </div>
          </div>
        ) : (
          <div className="flex h-full items-center justify-center px-4 text-center text-sm text-muted-soft">
            {t('contributions.empty')}
          </div>
        )}

        {activeDay ? (
          <div
            className="pointer-events-none absolute z-10 min-w-[10rem] rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel)/0.88)] px-3 py-2 text-xs text-foreground shadow-[0_16px_40px_rgb(0_0_0/0.16)] backdrop-blur-[14px]"
            style={{
              left: `min(max(${activeDay.x}px, 5.5rem), calc(100% - 5.5rem))`,
              top: Math.max(8, activeDay.y - 76),
              transform: 'translateX(-50%)',
            }}
          >
            <div className="mb-1 flex items-center justify-between gap-3">
              <p className="font-medium">{activeDay.day.date}</p>
              {contributions.selectedKey ? (
                <span className="max-w-[8rem] truncate text-[11px] font-medium text-muted-soft">{contributions.selectedKey.name}</span>
              ) : null}
            </div>
            <p className="text-muted-soft">{t('contributions.tooltipCost', { value: formatDashboardCurrency(activeDay.day.cost, activeDay.day.currency, 4) })}</p>
            <p className="text-muted-soft">{t('contributions.tooltipTokens', { value: formatDashboardTokens(activeDay.day.tokens) })}</p>
          </div>
        ) : null}
      </div>
    </Card>
  )
}

function ContributionStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <p className="flex min-w-0 items-baseline gap-2 text-[11px] font-medium leading-5 text-muted-soft">
        <span className="truncate">{label}</span>
        <span className="shrink-0 text-sm font-semibold text-foreground">{value}</span>
      </p>
    </div>
  )
}

function contributionRows(days: ApiKeyContributionDay[]): ContributionGridDay[] {
  const rows: ContributionGridDay[] = []
  const leadingEmptyCount = firstDayOffset(days[0]?.date)
  const emptyDay: ContributionGridDay = {
    date: '',
    tokens: 0,
    cost: 0,
    currency: 'USD',
    level: 0,
    empty: true,
  }
  for (let index = 0; index < leadingEmptyCount; index++) rows.push(emptyDay)
  rows.push(...days)
  const trailingEmptyCount = rows.length % 7 === 0 ? 0 : 7 - (rows.length % 7)
  for (let index = 0; index < trailingEmptyCount; index++) rows.push(emptyDay)
  return rows
}

function contributionMonthLabels(days: ContributionGridDay[], language?: string): ContributionMonthLabel[] {
  const labels: ContributionMonthLabel[] = []
  let previousMonth = ''
  for (let index = 0; index < days.length; index += 7) {
    const week = days.slice(index, index + 7)
    const firstRealDay = week.find((day) => !day.empty && day.date)
    if (!firstRealDay) continue
    const month = firstRealDay.date.slice(5, 7)
    if (month === previousMonth) continue
    labels.push({ label: monthLabel(firstRealDay.date, language), column: index / 7 })
    previousMonth = month
  }
  return labels
}

function firstDayOffset(date?: string) {
  if (!date) return 0
  const [year, month, day] = date.split('-').map(Number)
  const parsed = new Date(Date.UTC(year, month - 1, day))
  if (Number.isNaN(parsed.getTime())) return 0
  return parsed.getUTCDay()
}

function monthLabel(date: string, language?: string) {
  const month = Number(date.slice(5, 7))
  if (!Number.isFinite(month) || month < 1 || month > 12) return date.slice(5, 7)
  if (language?.startsWith('zh')) return `${month}月`
  if (language?.startsWith('jp') || language?.startsWith('ja')) return `${month}月`
  return ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'][month - 1]
}
