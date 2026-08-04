import * as React from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { addYears, format, startOfDay, startOfMonth } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { CalendarDays, ChevronDown, ChevronLeft, ChevronRight, X } from 'lucide-react'
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

type DatePickerProps = Omit<DayPickerProps, 'mode' | 'selected' | 'onSelect' | 'disabled'> & {
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
  className?: string
  triggerClassName?: string
}

function formatDate(date: Date | undefined, formatString: string) {
  if (!date) return ''

  try {
    return format(date, formatString)
  } catch {
    return date.toLocaleDateString()
  }
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

export function DatePicker(props: DatePickerProps) {
  const {
    value,
    defaultValue,
    onValueChange,
    placeholder = 'Select date',
    formatString = 'yyyy-MM-dd',
    disabled = false,
    disabledDates,
    disablePastDates = false,
    disableFutureDates = false,
    clearable = true,
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
  const displayValue = formatDate(selectedDate, formatString)
  const calendarStartMonth = startMonth ?? (disablePastDates ? startOfMonth(today) : new Date(today.getFullYear() - 100, 0, 1))
  const calendarEndMonth = endMonth ?? (disableFutureDates ? today : addYears(today, 20))
  const disabledDateMatchers = [
    disabledDates,
    disablePastDates ? { before: today } : undefined,
    disableFutureDates ? { after: today } : undefined,
  ].flat().filter(Boolean) as Matcher[]
  const mergedDisabledDates = disabledDateMatchers.length ? disabledDateMatchers : undefined

  function setSelectedDate(nextDate: Date | undefined) {
    if (!isControlled) {
      setInternalDate(nextDate)
    }

    onValueChange?.(nextDate)
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
            <span className={cn('flex min-w-0 flex-1 items-center gap-3', clearable && selectedDate && 'pr-5')}>
              <CalendarDays className="text-muted-soft h-4 w-4 shrink-0" />
              <span className="truncate">{displayValue || placeholder}</span>
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
          className="glass-panel-strong z-[120] w-[344px] rounded-xl p-3"
        >
          <DayPicker
            mode="single"
            selected={selectedDate}
            onSelect={(nextDate) => {
              setSelectedDate(nextDate)
              setOpen(false)
            }}
            disabled={mergedDisabledDates}
            captionLayout={captionLayout}
            locale={locale}
            startMonth={calendarStartMonth}
            endMonth={calendarEndMonth}
            showOutsideDays={showOutsideDays}
            className="text-foreground"
            classNames={{
              root: 'w-full',
              months: 'flex flex-col gap-4',
              month: 'space-y-3',
              month_caption: 'flex h-10 items-center justify-center px-11',
              dropdowns: 'flex items-center justify-center gap-2',
              caption_label: 'text-sm font-semibold text-foreground',
              nav: 'absolute inset-x-3 top-3 flex items-center justify-between',
              button_previous:
                'flex h-9 w-9 items-center justify-center rounded-md text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground disabled:pointer-events-none disabled:opacity-35',
              button_next:
                'flex h-9 w-9 items-center justify-center rounded-md text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground disabled:pointer-events-none disabled:opacity-35',
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
          <div className="mt-3 flex items-center justify-between border-t border-[hsl(var(--divider))] pt-3">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setSelectedDate(today)
                setOpen(false)
              }}
            >
              Today
            </Button>
            <Button variant="outline" size="sm" onClick={() => setOpen(false)}>
              Close
            </Button>
          </div>
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
