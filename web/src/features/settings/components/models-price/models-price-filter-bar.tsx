import { useTranslation } from 'react-i18next'
import { Search } from 'lucide-react'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { ModelPriceBillingType } from '@/features/settings/api/model-prices'

export type ModelsPriceBillingFilter = ModelPriceBillingType | 'all'
export type ModelsPriceStatusFilter = 'all' | 'manual' | 'synced' | 'missing'

export type SiteOption = {
  id: string
  name: string
}

type ModelsPriceFilterBarProps = {
  search: string
  billingType: ModelsPriceBillingFilter
  pricingStatus: ModelsPriceStatusFilter
  siteId: string
  sites: SiteOption[]
  onSearchChange: (value: string) => void
  onBillingTypeChange: (value: ModelsPriceBillingFilter) => void
  onPricingStatusChange: (value: ModelsPriceStatusFilter) => void
  onSiteIdChange: (value: string) => void
}

export function ModelsPriceFilterBar({
  search,
  billingType,
  pricingStatus,
  siteId,
  sites,
  onSearchChange,
  onBillingTypeChange,
  onPricingStatusChange,
  onSiteIdChange,
}: ModelsPriceFilterBarProps) {
  const { t } = useTranslation('settings')

  return (
    <div className="flex flex-wrap items-center justify-end gap-3">
      <label className="relative w-56">
        <Search className="text-muted-soft pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2" />
        <Input
          value={search}
          onChange={(event) => onSearchChange(event.target.value)}
          placeholder={t('modelsPrice.searchPlaceholder')}
          className="pl-10"
        />
      </label>

      <Select value={billingType} onValueChange={(value) => onBillingTypeChange(value as ModelsPriceBillingFilter)}>
        <SelectTrigger variant="filter" filterLabel={t('modelsPrice.headers.billing')} active={billingType !== 'all'}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent searchable={false} widthMode="content">
          <SelectItem value="all">{t('modelsPrice.billingLabels.all')}</SelectItem>
          <SelectItem value="tokens">{t('modelsPrice.billingLabels.tokens')}</SelectItem>
          <SelectItem value="per_request">{t('modelsPrice.billingLabels.perRequest')}</SelectItem>
        </SelectContent>
      </Select>

      <Select
        value={pricingStatus}
        onValueChange={(value) => onPricingStatusChange(value as ModelsPriceStatusFilter)}
      >
        <SelectTrigger variant="filter" filterLabel={t('modelsPrice.headers.status')} active={pricingStatus !== 'all'}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent searchable={false} widthMode="content">
          <SelectItem value="all">{t('modelsPrice.statusFilter.all')}</SelectItem>
          <SelectItem value="manual">{t('modelsPrice.statusFilter.manual')}</SelectItem>
          <SelectItem value="synced">{t('modelsPrice.statusFilter.synced')}</SelectItem>
          <SelectItem value="missing">{t('modelsPrice.statusFilter.missing')}</SelectItem>
        </SelectContent>
      </Select>

      {sites.length > 0 && (
        <Select value={siteId} onValueChange={onSiteIdChange}>
          <SelectTrigger variant="filter" filterLabel={t('modelsPrice.siteFilter.label')} active={siteId !== 'all'}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent searchable={false} widthMode="content">
            <SelectItem value="all">{t('modelsPrice.siteFilter.all')}</SelectItem>
            {sites.map((s) => (
              <SelectItem key={s.id} value={s.id}>{s.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </div>
  )
}
