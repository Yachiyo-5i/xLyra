import { LoaderCircle, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { MobileFilterBar } from '@/components/common/mobile-filter-bar'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import type { FilterItem, SortMode } from '@/features/routes/lib/types'

type MobileRoutesFilterBarProps = {
  search: string
  brandItems: FilterItem[]
  siteItems: FilterItem[]
  selectedBrand: string
  selectedSite: string | null
  sortMode: SortMode
  isFetching: boolean
  onSearchChange: (value: string) => void
  onBrandChange: (value: string) => void
  onSiteChange: (value: string | null) => void
  onSortModeChange: (value: SortMode) => void
  onRefresh: () => void
}

const sortItems: Array<{ key: SortMode; labelKey: string }> = [
  { key: 'default', labelKey: 'sort.default' },
  { key: 'success_desc', labelKey: 'sort.successDesc' },
  { key: 'success_asc', labelKey: 'sort.successAsc' },
  { key: 'candidates_desc', labelKey: 'sort.candidatesDesc' },
  { key: 'candidates_asc', labelKey: 'sort.candidatesAsc' },
]

export function MobileRoutesFilterBar({
  search,
  brandItems,
  siteItems,
  selectedBrand,
  selectedSite,
  sortMode,
  isFetching,
  onSearchChange,
  onBrandChange,
  onSiteChange,
  onSortModeChange,
  onRefresh,
}: MobileRoutesFilterBarProps) {
  const { t } = useTranslation('routes')
  const hasFilters = Boolean(search.trim() || selectedBrand !== 'all' || selectedSite)
  const selectedSiteItem = siteItems.find((item) => item.key === selectedSite)
  const allModelsCount = brandItems.find((item) => item.key === 'all')?.count ?? 0
  const siteSelectItems = [
    { key: 'all', label: t('filters.all'), count: allModelsCount, fallbackText: '*' },
    ...siteItems,
  ]

  function clearFilters() {
    onSearchChange('')
    onBrandChange('all')
    onSiteChange(null)
  }

  // 移动端筛选区：搜索 + 刷新，下面一行是排序 / 品牌 / 站点三个 filter 胶囊
  return (
    <MobileFilterBar
      search={{ value: search, onChange: (event) => onSearchChange(event.target.value), placeholder: t('workspace.search') }}
      actions={
        <Button
          type="button"
          variant="secondary"
          size="icon"
          className="rounded-full"
          aria-label={t('workspace.refresh')}
          disabled={isFetching}
          onClick={onRefresh}
        >
          {isFetching ? <LoaderCircle className="animate-spin" /> : <RotateCcw />}
        </Button>
      }
      filters={
        <>
          <Select value={sortMode} onValueChange={(value) => onSortModeChange(value as SortMode)}>
            <SelectTrigger variant="filter" filterLabel={t('sort.title')} active={sortMode !== 'default'}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent searchable={false} widthMode="content">
              {sortItems.map((item) => (
                <SelectItem key={item.key} value={item.key}>
                  {t(item.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select value={selectedBrand} onValueChange={onBrandChange}>
            <SelectTrigger variant="filter" filterLabel={t('workspace.brand')} active={selectedBrand !== 'all'}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent searchable={false} widthMode="content">
              {brandItems.map((item) => (
                <SelectItem key={item.key} value={item.key}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>

          <Select value={selectedSite ?? 'all'} onValueChange={(key) => onSiteChange(key === 'all' ? null : key)}>
            <SelectTrigger variant="filter" filterLabel={t('workspace.site')} active={Boolean(selectedSiteItem)}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent searchable={false} widthMode="content">
              {siteSelectItems.map((item) => (
                <SelectItem key={item.key} value={item.key}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </>
      }
      summary={
        hasFilters ? (
          <button type="button" className="ml-auto text-label-2 transition-colors active:text-label" onClick={clearFilters}>
            {t('common:list.clearFilters')}
          </button>
        ) : undefined
      }
    />
  )
}
