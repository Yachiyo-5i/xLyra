import * as React from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { ChevronDown, Clock, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type TimePickerProps = {
  value?: string
  defaultValue?: string
  onValueChange?: (time: string | undefined) => void
  placeholder?: string
  disabled?: boolean
  clearable?: boolean
  minuteStep?: number
  className?: string
  triggerClassName?: string
}

type TimeParts = {
  hour: string
  minute: string
}

function normalizeStep(step: number) {
  if (!Number.isFinite(step)) return 5
  return Math.min(Math.max(Math.trunc(step), 1), 30)
}

function padTimePart(value: number) {
  return String(value).padStart(2, '0')
}

function parseTime(time: string | undefined): TimeParts | undefined {
  if (!time) return undefined

  const match = /^(\d{1,2}):(\d{1,2})$/.exec(time.trim())
  if (!match) return undefined

  const hour = Number(match[1])
  const minute = Number(match[2])
  if (hour < 0 || hour > 23 || minute < 0 || minute > 59) return undefined

  return {
    hour: padTimePart(hour),
    minute: padTimePart(minute),
  }
}

function formatTime(parts: TimeParts) {
  return `${parts.hour}:${parts.minute}`
}

function getCurrentTime() {
  const now = new Date()
  return `${padTimePart(now.getHours())}:${padTimePart(now.getMinutes())}`
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

export function TimePicker({
  value,
  defaultValue,
  onValueChange,
  placeholder = 'Select time',
  disabled = false,
  clearable = true,
  minuteStep = 5,
  className,
  triggerClassName,
}: TimePickerProps) {
  const { t } = useTranslation('components')
  const [open, setOpen] = React.useState(false)
  const [internalTime, setInternalTime] = React.useState<string | undefined>(() => {
    const parsed = parseTime(defaultValue)
    return parsed ? formatTime(parsed) : undefined
  })
  const normalizedStep = normalizeStep(minuteStep)
  const selectedParts = parseTime(value ?? internalTime)
  const displayValue = selectedParts ? formatTime(selectedParts) : ''
  const hourOptions = React.useMemo(
    () => Array.from({ length: 24 }, (_, index) => padTimePart(index)),
    [],
  )
  const minuteOptions = React.useMemo(
    () => getMinuteOptions(normalizedStep, selectedParts?.minute),
    [normalizedStep, selectedParts?.minute],
  )

  function setSelectedTime(nextTime: string | undefined) {
    if (value === undefined) {
      setInternalTime(nextTime)
    }

    onValueChange?.(nextTime)
  }

  function setTimePart(part: keyof TimeParts, nextValue: string) {
    const nextParts = selectedParts ?? { hour: '00', minute: '00' }
    setSelectedTime(formatTime({ ...nextParts, [part]: nextValue }))
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
              !selectedParts && 'text-[hsl(var(--input-placeholder))]',
              triggerClassName,
            )}
          >
            <span className="flex min-w-0 flex-1 items-center gap-3">
              <Clock className="text-muted-soft h-4 w-4 shrink-0" />
              <span className="truncate tabular-nums">{displayValue || placeholder}</span>
            </span>
            <ChevronDown className={cn('text-muted-soft h-4 w-4 shrink-0 transition-transform duration-150', open && 'rotate-180')} />
          </button>
        </PopoverPrimitive.Trigger>

        {clearable && selectedParts && !disabled ? (
          <button
            type="button"
            aria-label={t('timePicker.clear')}
            className="text-muted-soft absolute right-10 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded-md transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
            onClick={(event) => {
              event.preventDefault()
              event.stopPropagation()
              setSelectedTime(undefined)
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
          className="glass-panel-strong z-[120] w-[280px] rounded-xl p-3"
        >
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-2">
              <p className="text-faint px-1 text-xs font-medium">{t('timePicker.hour')}</p>
              <div className="max-h-64 space-y-1 overflow-y-auto pr-1">
                {hourOptions.map((hour) => (
                  <TimeOptionButton
                    key={hour}
                    selected={selectedParts?.hour === hour}
                    onClick={() => setTimePart('hour', hour)}
                  >
                    {hour}
                  </TimeOptionButton>
                ))}
              </div>
            </div>

            <div className="space-y-2">
              <p className="text-faint px-1 text-xs font-medium">{t('timePicker.minute')}</p>
              <div className="max-h-64 space-y-1 overflow-y-auto pr-1">
                {minuteOptions.map((minute) => (
                  <TimeOptionButton
                    key={minute}
                    selected={selectedParts?.minute === minute}
                    onClick={() => setTimePart('minute', minute)}
                  >
                    {minute}
                  </TimeOptionButton>
                ))}
              </div>
            </div>
          </div>

          <div className="mt-3 flex items-center justify-between border-t border-[hsl(var(--divider))] pt-3">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setSelectedTime(getCurrentTime())
              }}
            >
              Now
            </Button>
            <div className="flex items-center gap-2">
              {clearable ? (
                <Button variant="ghost" size="sm" onClick={() => setSelectedTime(undefined)}>
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
