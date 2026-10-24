import { useMemo, useState, type ReactNode } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { ChevronsUpDown, CirclePlus, Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useLiquidGlassRef } from '@/components/glass/use-liquid-glass'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  fieldIconClass,
  fieldSurfaceClass,
  filterTriggerActiveClass,
  filterTriggerClass,
  menuGroupLabelClass,
  panelSearchClass,
  popoverPanelClass,
} from '@/components/ui/field-styles'
import { cn } from '@/lib/utils'

export type MultiSelectOption = {
  value: string
  label: string
  description?: string
  icon?: string
  /** Shown at the end of the option row in the dropdown, e.g. a status badge. */
  badge?: ReactNode
  group?: string
  disabled?: boolean
}

export function MultiSelect({
  value,
  options,
  placeholder,
  searchPlaceholder = 'Search...',
  emptyText = 'No options',
  selectedText,
  clearText,
  maxVisibleTags = 4,
  triggerVariant = 'default',
  triggerLabel,
  dropdownWidthMode = 'trigger',
  disabled,
  className,
  onChange,
}: {
  value: string[]
  options: MultiSelectOption[]
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  selectedText?: string
  clearText?: string
  maxVisibleTags?: number
  triggerVariant?: 'default' | 'filter'
  triggerLabel?: ReactNode
  dropdownWidthMode?: 'trigger' | 'content'
  disabled?: boolean
  className?: string
  onChange: (value: string[]) => void
}) {
  const { t } = useTranslation('common')
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const glassRef = useLiquidGlassRef<HTMLDivElement>('regular')
  const selected = useMemo(() => new Set(value), [value])
  const selectedOptions = useMemo(
    () => options.filter((option) => selected.has(option.value)),
    [options, selected],
  )
  const filteredOptions = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    if (!keyword) return options
    return options.filter((option) => (
      `${option.label} ${option.description ?? ''}`.toLowerCase().includes(keyword)
    ))
  }, [options, search])

  function toggle(optionValue: string, checked: boolean) {
    if (checked) {
      onChange(value.includes(optionValue) ? value : [...value, optionValue])
      return
    }
    onChange(value.filter((item) => item !== optionValue))
  }

  const isFilterTrigger = triggerVariant === 'filter'
  const useContentWidth = dropdownWidthMode === 'content' || isFilterTrigger
  const isActive = selectedOptions.length > 0

  return (
    <PopoverPrimitive.Root
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (!nextOpen) setSearch('')
      }}
    >
      <PopoverPrimitive.Trigger
        type="button"
        className={cn(
          isFilterTrigger
            ? filterTriggerClass
            : cn(
                fieldSurfaceClass,
                'flex min-h-[var(--control-h)] w-full items-center justify-between gap-3 py-1.5 pl-1.5 pr-3.5 text-left text-body focus-visible:ring-2 focus-visible:ring-tint/50 data-[state=open]:ring-2 data-[state=open]:ring-tint/50',
                !selectedOptions.length && 'pl-3.5',
              ),
          isFilterTrigger && isActive && filterTriggerActiveClass,
          disabled && 'cursor-not-allowed opacity-40',
          className,
        )}
        disabled={disabled}
      >
        {isFilterTrigger ? (
          <>
            <CirclePlus className="size-3.5 shrink-0" strokeWidth={2} />
            <span className="min-w-0 truncate">
              {triggerLabel ?? placeholder}
              {selectedOptions.length ? ` | ${selectedOptions.length}` : ''}
            </span>
          </>
        ) : (
          <>
            <span className="flex min-w-0 flex-1 flex-wrap gap-1.5">
              {selectedOptions.length ? (
                selectedOptions.slice(0, maxVisibleTags).map((option) => (
                  <span
                    key={option.value}
                    className="inline-flex h-6 max-w-[180px] items-center gap-1 rounded-full bg-fill-2 pl-2 pr-1 text-caption-1 text-label"
                  >
                    {option.icon ? <img src={option.icon} alt="" className="h-3.5 w-3.5 shrink-0 rounded-sm object-contain" /> : null}
                    <span className="truncate">{option.label}</span>
                    <span
                      role="button"
                      tabIndex={-1}
                      aria-label={`${t('actions.clear')} ${option.label}`}
                      className="flex size-4 items-center justify-center rounded-full text-label-3 hover:bg-fill-1 hover:text-label"
                      onClick={(event) => {
                        event.stopPropagation()
                        toggle(option.value, false)
                      }}
                    >
                      <X className="h-3 w-3" />
                    </span>
                  </span>
                ))
              ) : (
                <span className="text-label-3">{placeholder}</span>
              )}
              {selectedOptions.length > maxVisibleTags ? (
                <span className="inline-flex h-6 items-center rounded-full bg-fill-3 px-2 text-caption-1 text-label-2 tabular-nums">
                  +{selectedOptions.length - maxVisibleTags}
                </span>
              ) : null}
            </span>
            <ChevronsUpDown className={fieldIconClass} />
          </>
        )}
      </PopoverPrimitive.Trigger>

      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          ref={glassRef}
          align="start"
          sideOffset={8}
          collisionPadding={16}
          onOpenAutoFocus={(event) => event.preventDefault()}
          // 阻止滚轮/触摸事件冒泡到 document，否则弹层在 Dialog/Sheet 内打开时滚动会被其滚动锁拦截
          onWheel={(event) => event.stopPropagation()}
          onTouchMove={(event) => event.stopPropagation()}
          className={cn(
            popoverPanelClass,
            'z-[120] flex max-h-[min(var(--radix-popover-content-available-height),32rem)] origin-(--radix-popover-content-transform-origin) flex-col overflow-hidden p-0',
            useContentWidth
              ? 'w-max min-w-[var(--radix-popover-trigger-width)] max-w-[min(32rem,calc(100vw-2rem))]'
              : 'w-[var(--radix-popover-trigger-width)]',
          )}
        >
          <div className="p-1.5 pb-0">
            <div className={panelSearchClass}>
              <Search className={fieldIconClass} />
              <input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={searchPlaceholder}
                className="w-full bg-transparent text-callout text-label outline-none placeholder:text-label-3"
              />
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
            {filteredOptions.length ? (
              filteredOptions.map((option, index) => {
                const checked = selected.has(option.value)
                const showGroup = option.group && option.group !== filteredOptions[index - 1]?.group
                return (
                  <div key={option.value}>
                    {showGroup ? (
                      <div className={menuGroupLabelClass}>
                        {option.group}
                      </div>
                    ) : null}
                    <label
                      className={cn(
                        'flex min-h-[var(--control-h-sm)] cursor-pointer items-center gap-2.5 rounded-[14px] px-2.5 py-1.5 transition-colors duration-100 hover:bg-fill-3',
                        option.disabled && 'cursor-not-allowed opacity-40',
                      )}
                    >
                      <Checkbox
                        checked={checked}
                        disabled={option.disabled}
                        onCheckedChange={(nextChecked) => toggle(option.value, Boolean(nextChecked))}
                        ariaLabel={option.label}
                      />
                      {option.icon ? <img src={option.icon} alt="" className="size-5 shrink-0 rounded-[5px] object-contain" /> : null}
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-body text-label">{option.label}</span>
                        {option.description ? (
                          <span className="block truncate text-footnote text-label-2">{option.description}</span>
                        ) : null}
                      </span>
                      {option.badge ? <span className="shrink-0">{option.badge}</span> : null}
                    </label>
                  </div>
                )
              })
            ) : (
              <div className="py-8 text-center text-callout text-label-3">{emptyText}</div>
            )}
          </div>
          <div className="flex items-center justify-between border-t border-separator p-1.5 pl-3">
            <span className="text-footnote text-label-2 tabular-nums">{selectedOptions.length} {selectedText ?? t('status.selected')}</span>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              onClick={() => onChange([])}
              disabled={!selectedOptions.length}
            >
              {clearText ?? t('actions.clear')}
            </Button>
          </div>
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
