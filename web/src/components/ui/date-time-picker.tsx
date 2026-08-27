import * as React from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { addYears, format, startOfDay, startOfMonth } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { CalendarDays, ChevronDown, ChevronLeft, ChevronRight, Clock, X } from 'lucide-react'
import { DayPicker, type DayPickerProps, type DropdownProps, type Matcher } from 'react-day-picker'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'

type DateTimePickerProps = Omit<DayPickerProps, 'mode' | 'selected' | 'onSelect' | 'disabled'> & {
  value?: Date
  defaultValue?: Date
  onValueChange?: (date: Date | undefined) => void
  placeholder?: string
  formatString?: string
  disabled?: boolean
  disabledDates?: Matcher | Matcher[]
  disablePastDates?: boolean
  disableFutureDates?: boolean
  clearable?: boolean
  minuteStep?: number
  className?: string
  triggerClassName?: string
}

type TimeParts = {
  hour: string
  minute: string
}

function formatDateTime(date: Date | undefined, formatString: string) {
  if (!date) return ''

  try {
    return format(date, formatString)
  } catch {
    return date.toLocaleString()
  }
}

function normalizeStep(step: number) {
  if (!Number.isFinite(step)) return 5
  return Math.min(Math.max(Math.trunc(step), 1), 30)
}

function padTimePart(value: number) {
  return String(value).padStart(2, '0')
}

function getTimeParts(date: Date | undefined): TimeParts | undefined {
  if (!date) return undefined

  return {
    hour: padTimePart(date.getHours()),
    minute: padTimePart(date.getMinutes()),
  }
}

function getMinuteOptions(step: number, selectedMinute?: string) {
  const minutes = new Set<string>()
  for (let minute = 0; minute < 60; minute += step) {
    minutes.add(padTimePart(minute))
  }

  if (selectedMinute) {
    minutes.add(selectedMinute)
  }

  return [...minutes].sort((a, b) => Number(a) - Number(b))
}

function mergeDateWithTime(date: Date, timeSource?: Date) {
  const nextDate = new Date(date)
  nextDate.setHours(timeSource?.getHours() ?? 0, timeSource?.getMinutes() ?? 0, 0, 0)
  return nextDate
}

function setDateTimePart(date: Date | undefined, part: keyof TimeParts, nextValue: string) {
  const nextDate = new Date(date ?? new Date())
  if (part === 'hour') {
    nextDate.setHours(Number(nextValue))
  } else {
    nextDate.setMinutes(Number(nextValue))
  }
  nextDate.setSeconds(0, 0)
  return nextDate
}

function CalendarChevron({
  orientation,
  className,
}: {
  orientation?: 'up' | 'down' | 'left' | 'right'
  className?: string
}) {
  const Icon = orientation === 'left' ? ChevronLeft : orientation === 'right' ? ChevronRight : ChevronDown
  return <Icon className={cn('h-4 w-4', className)} />
}

function CalendarDropdown({
  options,
  value,
  onChange,
  disabled,
  'aria-label': ariaLabel,
}: DropdownProps) {
  const selectedOption = options?.find((option) => option.value === value)

  return (
    <Select
      value={value === undefined ? undefined : String(value)}
      onValueChange={(nextValue) => {
        onChange?.({
          target: { value: nextValue },
        } as React.ChangeEvent<HTMLSelectElement>)
      }}
    >
      <SelectTrigger
        aria-label={ariaLabel}
        disabled={disabled}
        className="h-9 min-w-[118px] rounded-md border-0 bg-[hsl(var(--surface-subtle))] px-3 shadow-none [&>span]:overflow-visible [&>span]:whitespace-nowrap [&>span]:text-clip"
      >
        <SelectValue placeholder={selectedOption?.label ?? 'Select'} />
      </SelectTrigger>
      <SelectContent className="z-[140] max-h-72" style={{ width: 'max-content', minWidth: '132px' }}>
        {options?.map((option) => (
          <SelectItem
            key={option.value}
            value={String(option.value)}
            disabled={option.disabled}
            hideIndicator
            className="justify-center whitespace-nowrap px-4 text-center"
          >
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function TimeOptionButton({
  selected,
  children,
  onClick,
}: {
  selected: boolean
  children: React.ReactNode
  onClick: () => void
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      className={cn(
        'flex h-8 w-full items-center justify-center rounded-md text-sm tabular-nums transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))]',
        selected
          ? 'bg-primary text-primary-foreground hover:bg-primary'
          : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
      )}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

export function DateTimePicker(props: DateTimePickerProps) {
  const {
    value,
    defaultValue,
    onValueChange,
    placeholder = 'Select date and time',
    formatString = 'yyyy-MM-dd HH:mm',
    disabled = false,
    disabledDates,
    disablePastDates = false,
    disableFutureDates = false,
    clearable = true,
    minuteStep = 5,
    className,
    triggerClassName,
    showOutsideDays = true,
    captionLayout = 'dropdown',
    locale = zhCN,
    startMonth,
    endMonth,
    ...calendarProps
  } = props
  const { t } = useTranslation('components')
  const [open, setOpen] = React.useState(false)
  const [internalDate, setInternalDate] = React.useState<Date | undefined>(defaultValue)
  const today = startOfDay(new Date())
  const isControlled = Object.prototype.hasOwnProperty.call(props, 'value')
  const selectedDate = isControlled ? value : internalDate
  const selectedParts = getTimeParts(selectedDate)
  const displayValue = formatDateTime(selectedDate, formatString)
  const normalizedStep = normalizeStep(minuteStep)
  const calendarStartMonth = startMonth ?? (disablePastDates ? startOfMonth(today) : new Date(today.getFullYear() - 100, 0, 1))
  const calendarEndMonth = endMonth ?? (disableFutureDates ? today : addYears(today, 20))
  const disabledDateMatchers = [
    disabledDates,
    disablePastDates ? { before: today } : undefined,
    disableFutureDates ? { after: today } : undefined,
  ].flat().filter(Boolean) as Matcher[]
  const mergedDisabledDates = disabledDateMatchers.length ? disabledDateMatchers : undefined
  const hourOptions = React.useMemo(
    () => Array.from({ length: 24 }, (_, index) => padTimePart(index)),
    [],
  )
  const minuteOptions = React.useMemo(
    () => getMinuteOptions(normalizedStep, selectedParts?.minute),
    [normalizedStep, selectedParts?.minute],
  )

  function setSelectedDate(nextDate: Date | undefined) {
    if (!isControlled) {
      setInternalDate(nextDate)
    }

    onValueChange?.(nextDate)
  }

  function handleDaySelect(nextDay: Date | undefined) {
    setSelectedDate(nextDay ? mergeDateWithTime(nextDay, selectedDate) : undefined)
  }

  function handleTimePartSelect(part: keyof TimeParts, nextValue: string) {
    setSelectedDate(setDateTimePart(selectedDate, part, nextValue))
  }

  function setCurrentDateTime() {
    setSelectedDate(new Date())
  }

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <div className={cn('relative', className)}>
        <PopoverPrimitive.Trigger asChild>
          <button
            type="button"
            disabled={disabled}
            className={cn(
              'glass-field flex h-11 w-full items-center justify-between gap-3 rounded-md px-4 text-left text-sm text-foreground outline-none transition-[border-color,background-color,box-shadow] duration-150 focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] disabled:cursor-not-allowed disabled:opacity-50',
              !selectedDate && 'text-[hsl(var(--input-placeholder))]',
              triggerClassName,
            )}
          >
            <span className="flex min-w-0 flex-1 items-center gap-3">
              <CalendarDays className="text-muted-soft h-4 w-4 shrink-0" />
              <span className="truncate tabular-nums">{displayValue || placeholder}</span>
            </span>
            <ChevronDown className={cn('text-muted-soft h-4 w-4 shrink-0 transition-transform duration-150', open && 'rotate-180')} />
          </button>
        </PopoverPrimitive.Trigger>

        {clearable && selectedDate && !disabled ? (
          <button
            type="button"
            aria-label={t('datePicker.clear')}
            className="text-muted-soft absolute right-10 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded-md transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
            onClick={(event) => {
              event.preventDefault()
              event.stopPropagation()
              setSelectedDate(undefined)
            }}
          >
            <X className="h-3.5 w-3.5" />
          </button>
        ) : null}
      </div>

      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="start"
          sideOffset={8}
          // 阻止滚轮/触摸事件冒泡到 document，否则弹层在 Dialog/Sheet 内打开时滚动会被其滚动锁拦截
          onWheel={(event) => event.stopPropagation()}
          onTouchMove={(event) => event.stopPropagation()}
          className="glass-panel-strong z-[120] w-[min(644px,calc(100vw-2rem))] rounded-xl p-3"
        >
          <div className="grid gap-4 min-[620px]:grid-cols-[320px_1fr]">
            <div className="relative min-w-0">
              <DayPicker
                mode="single"
                selected={selectedDate}
                onSelect={handleDaySelect}
                disabled={mergedDisabledDates}
                captionLayout={captionLayout}
                locale={locale}
                startMonth={calendarStartMonth}
                endMonth={calendarEndMonth}
                showOutsideDays={showOutsideDays}
                className="text-foreground"
                classNames={{
                  root: 'relative w-full',
                  months: 'flex flex-col gap-4',
                  month: 'space-y-3',
                  month_caption: 'relative flex h-10 items-center justify-center px-10',
                  dropdowns: 'flex items-center justify-center gap-2',
                  caption_label: 'text-sm font-semibold text-foreground',
                  nav: 'pointer-events-none absolute inset-x-0 top-0 flex h-10 items-center justify-between',
                  button_previous:
                    'pointer-events-auto flex h-9 w-9 items-center justify-center rounded-md text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground disabled:pointer-events-none disabled:opacity-35',
                  button_next:
                    'pointer-events-auto flex h-9 w-9 items-center justify-center rounded-md text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground disabled:pointer-events-none disabled:opacity-35',
                  chevron: 'text-current',
                  month_grid: 'w-full border-collapse',
                  weekdays: 'grid grid-cols-7',
                  weekday: 'text-faint flex h-8 items-center justify-center text-[11px] font-medium uppercase',
                  weeks: 'grid gap-1',
                  week: 'grid grid-cols-7 gap-1',
                  day: 'relative h-9 w-9 p-0 text-center text-sm',
                  day_button:
                    'flex h-9 w-9 items-center justify-center rounded-md text-sm text-foreground transition-colors hover:bg-[hsl(var(--surface-subtle))] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))]',
                  selected:
                    '[&>button]:bg-primary [&>button]:text-primary-foreground [&>button]:hover:bg-primary',
                  today:
                    '[&>button]:border [&>button]:border-[hsl(var(--glass-border-strong))] [&>button]:bg-[hsl(var(--surface-subtle))]',
                  outside: '[&>button]:text-[hsl(var(--text-faint))] [&>button]:opacity-45',
                  disabled: '[&>button]:pointer-events-none [&>button]:opacity-35',
                  hidden: 'invisible',
                }}
                components={{
                  Dropdown: CalendarDropdown,
                  Chevron: CalendarChevron,
                }}
                {...calendarProps}
              />
            </div>

            <div className="grid min-w-0 grid-cols-2 gap-3 border-t border-[hsl(var(--divider))] pt-4 min-[620px]:border-l min-[620px]:border-t-0 min-[620px]:pl-4 min-[620px]:pt-0">
              <div className="space-y-2">
                <p className="text-faint flex items-center gap-1.5 px-1 text-xs font-medium">
                  <Clock className="h-3.5 w-3.5" />
                  {t('timePicker.hour')}
                </p>
                <div className="max-h-[300px] space-y-1 overflow-y-auto pr-1">
                  {hourOptions.map((hour) => (
                    <TimeOptionButton
                      key={hour}
                      selected={selectedParts?.hour === hour}
                      onClick={() => handleTimePartSelect('hour', hour)}
                    >
                      {hour}
                    </TimeOptionButton>
                  ))}
                </div>
              </div>

              <div className="space-y-2">
                <p className="text-faint px-1 text-xs font-medium">{t('timePicker.minute')}</p>
                <div className="max-h-[300px] space-y-1 overflow-y-auto pr-1">
                  {minuteOptions.map((minute) => (
                    <TimeOptionButton
                      key={minute}
                      selected={selectedParts?.minute === minute}
                      onClick={() => handleTimePartSelect('minute', minute)}
                    >
                      {minute}
                    </TimeOptionButton>
                  ))}
                </div>
              </div>
            </div>
          </div>

          <div className="mt-3 flex items-center justify-between border-t border-[hsl(var(--divider))] pt-3">
            <Button variant="ghost" size="sm" onClick={setCurrentDateTime}>
              Now
            </Button>
            <div className="flex items-center gap-2">
              {clearable ? (
                <Button variant="ghost" size="sm" onClick={() => setSelectedDate(undefined)}>
                  Clear
                </Button>
              ) : null}
              <Button variant="outline" size="sm" onClick={() => setOpen(false)}>
                Close
              </Button>
            </div>
          </div>
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
