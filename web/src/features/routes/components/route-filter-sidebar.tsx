import { useTranslation } from 'react-i18next'
import { BrandMark } from '@/components/common/brand-mark'
import type { FilterItem, SortMode } from '@/features/routes/lib/types'

export function FilterColumn({
  title,
  items,
  selectedKey,
  onSelect,
}: {
  title: string
  items: FilterItem[]
  selectedKey: string | null
  onSelect: (key: string) => void
}) {
  return (
    <div className="space-y-2">
      <h2 className="px-1 text-sm font-medium text-foreground">{title}</h2>
      <div className="space-y-1">
        {items.map((item) => {
          const active = selectedKey === item.key

          return (
            <button
              key={item.key}
              type="button"
              onClick={() => onSelect(item.key)}
              className={`flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm transition-colors ${
                active
                  ? 'bg-[hsl(var(--surface-subtle))] text-foreground'
                  : 'bg-transparent text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground'
              }`}
            >
              <BrandMark
                iconPath={item.iconPath}
                label={item.label}
                fallback={item.label}
                fallbackText={item.fallbackText}
                size="sm"
              />
              <span className="min-w-0 flex-1 truncate">{item.label}</span>
              <span
                className={`inline-flex min-w-5 items-center justify-center rounded-full px-1.5 py-0.5 text-[11px] font-medium ${
                  active
                    ? 'bg-[hsl(var(--surface-subtle))] text-foreground'
                    : 'bg-[hsl(var(--surface-subtle))] text-muted-soft'
                }`}
              >
                {item.count}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

export function SortColumn({
  value,
  onChange,
}: {
  value: SortMode
  onChange: (value: SortMode) => void
}) {
  const { t } = useTranslation('routes')
  const items: Array<{ key: SortMode; label: string }> = [
    { key: 'default', label: t('sort.default') },
    { key: 'success_desc', label: t('sort.successDesc') },
    { key: 'success_asc', label: t('sort.successAsc') },
    { key: 'candidates_desc', label: t('sort.candidatesDesc') },
    { key: 'candidates_asc', label: t('sort.candidatesAsc') },
  ]

  return (
    <div className="space-y-2">
      <h2 className="px-1 text-sm font-medium text-foreground">{t('sort.title')}</h2>
      <div className="space-y-1">
        {items.map((item) => {
          const active = value === item.key

          return (
            <button
              key={item.key}
              type="button"
              onClick={() => onChange(item.key)}
              className={`flex w-full items-center rounded-md px-3 py-2 text-left text-sm transition-colors ${
                active
                  ? 'bg-[hsl(var(--surface-subtle))] text-foreground'
                  : 'bg-transparent text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground'
              }`}
            >
              {item.label}
            </button>
          )
        })}
      </div>
    </div>
  )
}
