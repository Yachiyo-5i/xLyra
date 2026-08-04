import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ChevronDown, CirclePlus, Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { cn } from '@/lib/utils'

export type MultiSelectOption = {
  value: string
  label: string
  description?: string
  icon?: string
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
  const rootRef = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
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

  useEffect(() => {
    if (!open) return
    function handlePointerDown(event: PointerEvent) {
      if (!rootRef.current?.contains(event.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('pointerdown', handlePointerDown)
    return () => document.removeEventListener('pointerdown', handlePointerDown)
  }, [open])

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
    <div ref={rootRef} className={cn('relative', className)}>
      <button
        type="button"
        className={cn(
          isFilterTrigger
            ? 'inline-flex h-10 w-fit shrink-0 items-center justify-center gap-1.5 rounded-lg border border-dashed border-[hsl(var(--glass-border-strong))] bg-[hsl(var(--surface-subtle))] px-4 text-left text-sm font-medium text-foreground outline-none transition-[border-color,background-color,box-shadow,color] duration-150 hover:border-[hsl(var(--field-border-focus))] hover:bg-[hsl(var(--surface-soft-hover))] focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))]'
            : 'glass-field flex min-h-11 w-full items-center justify-between gap-3 rounded-md px-3 py-2 text-left text-sm text-foreground outline-none transition-[border-color,background-color,box-shadow] duration-150 focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))]',
          isFilterTrigger && isActive && 'border-[hsl(var(--primary)/0.54)] bg-[hsl(var(--primary)/0.08)] text-primary',
          disabled && 'cursor-not-allowed opacity-60',
        )}
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
      >
        {isFilterTrigger ? (
          <>
            <CirclePlus className="h-4 w-4 shrink-0" strokeWidth={2} />
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
                    className="inline-flex max-w-[180px] items-center gap-1 rounded-md bg-[hsl(var(--surface-selected))] px-2 py-1 text-xs text-foreground"
                  >
                    {option.icon ? <img src={option.icon} alt="" className="h-3.5 w-3.5 shrink-0 rounded-sm object-contain" /> : null}
                    <span className="truncate">{option.label}</span>
                    <span
                      role="button"
                      tabIndex={-1}
                      className="rounded-sm text-muted-soft hover:text-foreground"
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
                <span className="text-[hsl(var(--input-placeholder))]">{placeholder}</span>
              )}
              {selectedOptions.length > maxVisibleTags ? (
                <span className="rounded-md bg-[hsl(var(--surface-subtle))] px-2 py-1 text-xs text-muted-soft">
                  +{selectedOptions.length - maxVisibleTags}
                </span>
              ) : null}
            </span>
            <ChevronDown className={cn('h-4 w-4 shrink-0 text-muted-soft transition-transform', open && 'rotate-180')} />
          </>
        )}
      </button>

      {open ? (
        <div
          className={cn(
            'glass-panel-strong absolute z-[120] mt-2 flex max-h-[min(calc(100dvh-8rem),32rem)] flex-col overflow-hidden rounded-xl shadow-xl',
            useContentWidth ? 'w-max min-w-full max-w-[min(32rem,calc(100vw-2rem))]' : 'w-full',
          )}
        >
          <div className="p-2 pb-0">
            <div className="flex items-center gap-2 rounded-lg border border-[hsl(var(--glass-border-strong))] bg-[hsl(var(--surface-field))] px-3 py-2">
              <Search className="h-4 w-4 shrink-0 text-muted-soft" />
              <input
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                placeholder={searchPlaceholder}
                className="w-full bg-transparent text-base text-foreground outline-none placeholder:text-[hsl(var(--input-placeholder))] md:text-sm"
              />
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {filteredOptions.length ? (
              filteredOptions.map((option, index) => {
                const checked = selected.has(option.value)
                const showGroup = option.group && option.group !== filteredOptions[index - 1]?.group
                return (
                  <div key={option.value}>
                    {showGroup ? (
                      <div className="px-3 pb-1 pt-3 text-[11px] font-semibold uppercase tracking-[0.14em] text-faint first:pt-1">
                        {option.group}
                      </div>
                    ) : null}
                    <label
                      className={cn(
                        'flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-[hsl(var(--surface-subtle))]',
                        option.disabled && 'cursor-not-allowed opacity-50',
                      )}
                    >
                      <Checkbox
                        checked={checked}
                        disabled={option.disabled}
                        onCheckedChange={(nextChecked) => toggle(option.value, Boolean(nextChecked))}
                        ariaLabel={option.label}
                      />
                      {option.icon ? <img src={option.icon} alt="" className="h-5 w-5 shrink-0 rounded-md object-contain" /> : null}
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm text-foreground">{option.label}</span>
                        {option.description ? (
                          <span className="block truncate text-xs text-muted-soft">{option.description}</span>
                        ) : null}
                      </span>
                    </label>
                  </div>
                )
              })
            ) : (
              <div className="py-8 text-center text-sm text-muted-soft">{emptyText}</div>
            )}
          </div>
          <div className="flex items-center justify-between border-t border-[hsl(var(--glass-divider))] p-2">
            <span className="px-2 text-xs text-muted-soft">{selectedOptions.length} {selectedText ?? t('status.selected')}</span>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="h-8"
              onClick={() => onChange([])}
              disabled={!selectedOptions.length}
            >
              {clearText ?? t('actions.clear')}
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  )
}
