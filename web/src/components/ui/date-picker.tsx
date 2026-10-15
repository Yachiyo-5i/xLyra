import * as React from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { addYears, format, startOfDay, startOfMonth } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { CalendarDays, ChevronDown, ChevronsUpDown, ChevronLeft, ChevronRight, X } from 'lucide-react'
import { DayPicker, type DayPickerProps, type DropdownProps, type Matcher } from 'react-day-picker'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { useLiquidGlassRef } from '@/components/glass/use-liquid-glass'
import { calendarClassNames, calendarDropdownTriggerClass } from '@/components/ui/calendar-styles'
import { fieldClearButtonClass, fieldIconClass, fieldTriggerClass, popoverPanelClass } from '@/components/ui/field-styles'
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
        className={calendarDropdownTriggerClass}
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
            className="justify-center whitespace-nowrap px-3 text-center"
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
  const glassRef = useLiquidGlassRef<HTMLDivElement>('regular')
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
              fieldTriggerClass,
              !selectedDate && 'text-label-3',
              triggerClassName,
            )}
          >
            <span className={cn('flex min-w-0 flex-1 items-center gap-3', clearable && selectedDate && 'pr-5')}>
              <CalendarDays className={fieldIconClass} />
              <span className="truncate">{displayValue || placeholder}</span>
            </span>
            <ChevronsUpDown className={fieldIconClass} />
          </button>
        </PopoverPrimitive.Trigger>

        {clearable && selectedDate && !disabled ? (
          <button
            type="button"
            aria-label={t('datePicker.clear')}
            className={fieldClearButtonClass}
            onClick={(event) => {
              event.preventDefault()
              event.stopPropagation()
              setSelectedDate(undefined)
            }}
          >
            <X className="size-3.5" />
          </button>
        ) : null}
      </div>

      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          ref={glassRef}
          align="start"
          sideOffset={8}
          className={cn(popoverPanelClass, 'z-[120] w-[344px] origin-(--radix-popover-content-transform-origin) p-3')}
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
            className="text-label"
            classNames={calendarClassNames}
            components={{
              Dropdown: CalendarDropdown,
              Chevron: CalendarChevron,
            }}
            {...calendarProps}
          />
          <div className="mt-3 flex items-center justify-between border-t border-separator pt-3">
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
