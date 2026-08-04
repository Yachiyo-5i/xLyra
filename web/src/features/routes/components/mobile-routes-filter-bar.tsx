import { useState } from 'react'
import { Filter, LoaderCircle, RotateCcw, Search, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle, DrawTrigger } from '@/components/ui/draw'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { FilterColumn, SortColumn } from '@/features/routes/components/route-filter-sidebar'
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
  const [drawerOpen, setDrawerOpen] = useState(false)
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

  return (
    <section className="space-y-2">
      <div className="grid grid-cols-[minmax(0,1fr)_auto_auto] gap-2">
        <label className="relative min-w-0">
          <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-foreground/40" />
          <Input
            value={search}
            onChange={(event) => onSearchChange(event.target.value)}
            placeholder={t('workspace.search')}
            className="h-10 pl-10"
          />
        </label>

        <Draw open={drawerOpen} onOpenChange={setDrawerOpen}>
          <DrawTrigger asChild>
            <Button type="button" variant={hasFilters ? 'secondary' : 'outline'} size="icon" aria-label={t('mobile.filters')}>
              <Filter className="h-4 w-4" />
            </Button>
          </DrawTrigger>
          <DrawContent className="max-h-[82vh]">
            <DrawHeader className="py-4">
              <DrawTitle>{t('mobile.filters')}</DrawTitle>
            </DrawHeader>
            <DrawBody className="space-y-5 px-4 py-4">
              <FilterColumn
                title={t('workspace.brand')}
                items={brandItems}
                selectedKey={selectedBrand}
                onSelect={onBrandChange}
              />
              <FilterColumn
                title={t('workspace.site')}
                items={siteSelectItems}
                selectedKey={selectedSite ?? 'all'}
                onSelect={(key) => onSiteChange(key === 'all' ? null : key)}
              />
              <SortColumn value={sortMode} onChange={onSortModeChange} />
            </DrawBody>
            <DrawFooter className="grid grid-cols-2 gap-2 px-4">
              <Button type="button" variant="outline" onClick={clearFilters} disabled={!hasFilters}>
                <X className="h-4 w-4" />
                {t('mobile.clear')}
              </Button>
              <Button type="button" onClick={() => setDrawerOpen(false)}>{t('mobile.done')}</Button>
            </DrawFooter>
          </DrawContent>
        </Draw>

        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={t('workspace.refresh')}
          disabled={isFetching}
          onClick={onRefresh}
        >
          {isFetching ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <RotateCcw className="h-4 w-4" />}
        </Button>
      </div>

      <div className={cn('grid items-center gap-2', hasFilters ? 'grid-cols-[minmax(0,1fr)_auto]' : 'grid-cols-1')}>
        <div className="flex min-w-0 items-center gap-2 overflow-x-auto pb-0.5">
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
        </div>
        {hasFilters ? (
          <Button type="button" variant="outline" size="sm" className="h-10 shrink-0 px-2.5" onClick={clearFilters}>
            <X className="h-4 w-4" />
            {t('mobile.clear')}
          </Button>
        ) : null}
      </div>
    </section>
  )
}
