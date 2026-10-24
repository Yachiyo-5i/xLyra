import { useTranslation } from 'react-i18next'
import { FilterGroup } from '@/components/common/filter-column'
import { filterOptionClass } from '@/components/common/filter-styles'
import type { SortMode } from '@/features/routes/lib/types'

export { FilterColumn } from '@/components/common/filter-column'

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
    <FilterGroup title={t('sort.title')}>
      {items.map((item) => (
        <button key={item.key} type="button" onClick={() => onChange(item.key)} className={filterOptionClass(value === item.key)}>
          {item.label}
        </button>
      ))}
    </FilterGroup>
  )
}
