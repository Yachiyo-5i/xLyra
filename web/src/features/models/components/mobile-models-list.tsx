import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { BrandMark } from '@/components/common/brand-mark'
import { buildModelGlyph } from '@/components/common/brand-utils'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { OTHER_BRAND_KEY } from '@/lib/brands'
import { cn } from '@/lib/utils'
import { formatEndpointTypeLabel, isNewAPISite } from '../lib/model-helpers'
import {
  formatGroupRatio,
  formatPricingValue,
  type MarketplaceKeyGroupStatus,
  type MarketplaceModel,
} from '../api/models'

type MobileModelsListProps = {
  items: MarketplaceModel[]
  expandedId: string | null
  onExpandedIdChange: (id: string | null) => void
  className?: string
}

type ModelSite = MarketplaceModel['supportedSites'][number]
type PricingRow = ModelSite['pricingRows'][number]

export function MobileModelsList({
  items,
  expandedId,
  onExpandedIdChange,
  className,
}: MobileModelsListProps) {
  return (
    <div className={cn('space-y-3', className)}>
      {items.map((model) => (
        <MobileModelCard
          key={model.id}
          model={model}
          expanded={expandedId === model.id}
          onToggle={() =>
            onExpandedIdChange(expandedId === model.id ? null : model.id)
          }
        />
      ))}
    </div>
  )
}

function MobileModelCard({
  model,
  expanded,
  onToggle,
}: {
  model: MarketplaceModel
  expanded: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation('models')
  const Chevron = expanded ? ChevronDown : ChevronRight

  return (
    <Card className="overflow-hidden rounded-lg p-0">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-start gap-3 px-4 py-3.5 text-left"
      >
        <BrandMark
          iconPath={model.iconPath}
          label={model.brand}
          fallback={model.name}
          fallbackText={
            model.providerId === OTHER_BRAND_KEY
              ? buildModelGlyph(model.name)
              : undefined
          }
          highlightedFallback={model.providerId === OTHER_BRAND_KEY}
          size="sm"
        />

        <div className="min-w-0 flex-1 space-y-2.5">
          <div className="flex min-w-0 items-start justify-between gap-3">
            <div className="min-w-0 space-y-1">
              <div
                className="break-words text-sm font-semibold leading-5 text-foreground transition-opacity hover:opacity-80"
                onClick={(event) => {
                  event.stopPropagation()
                  copyToClipboard(model.model_key, t('card.copied'))
                }}
              >
                {model.model_key}
              </div>
              <div className="text-muted-soft text-xs">{model.brand}</div>
            </div>
            <Chevron className="mt-0.5 h-4 w-4 shrink-0 text-muted-soft" />
          </div>

          <div className="flex min-w-0 flex-wrap gap-1.5">
            {model.supportedSites.map((site) => (
              <Badge
                key={`${model.id}-${site.siteId}`}
                variant={site.enabled ? 'neutral' : 'warning'}
                className="max-w-full border-transparent px-2 py-0.5 text-[11px] tracking-normal"
              >
                <span className="truncate">{site.siteName}</span>
              </Badge>
            ))}
          </div>
        </div>
      </button>

      {expanded ? (
        <div className="border-t border-[hsl(var(--glass-divider))] bg-[hsl(var(--surface-subtle)/0.24)] px-3 py-3">
          <div className="max-h-[68vh] space-y-4 overflow-y-auto">
            {model.supportedSites.map((site) => (
              <MobileModelSiteDetails
                key={`${model.id}-${site.siteId}`}
                modelId={model.id}
                site={site}
              />
            ))}
          </div>
        </div>
      ) : null}
    </Card>
  )
}

function MobileModelSiteDetails({
  modelId,
  site,
}: {
  modelId: string
  site: ModelSite
}) {
  const { t } = useTranslation('models')
  const rows = site.pricingRows.length
    ? site.pricingRows
    : [
        {
          id: `${site.siteId}-empty`,
          modelName: site.upstreamName,
          groupName: '',
          keyGroupStatus: undefined,
          billingType: '',
          currency: '',
          available: false,
        },
      ]

  return (
    <section className="border-b border-[hsl(var(--glass-divider))] pb-4 last:border-b-0 last:pb-0">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div
            className="truncate text-sm font-semibold text-foreground"
            title={site.siteName}
          >
            {site.siteName}
          </div>
          <div className="mt-1 break-words text-xs text-muted-soft">
            {site.upstreamName || '--'}
          </div>
        </div>
        <Badge
          variant={site.enabled ? 'success' : 'warning'}
          className="shrink-0 border-transparent px-2 py-0.5 text-[11px] tracking-normal"
        >
          {site.enabled
            ? t('common:status.enabled')
            : t('common:status.disabled')}
        </Badge>
      </div>

      <div className="mt-3 space-y-3">
        {rows.map((pricing) => (
          <MobilePricingBlock
            key={`${modelId}-${site.siteId}-${pricing.id}`}
            site={site}
            pricing={pricing}
          />
        ))}
      </div>
    </section>
  )
}

function MobilePricingBlock({
  site,
  pricing,
}: {
  site: ModelSite
  pricing: PricingRow
}) {
  const { t } = useTranslation('models')
  const endpointTypes = site.supportedEndpointTypes.length
    ? site.supportedEndpointTypes
    : []

  return (
    <div className="rounded-lg bg-[hsl(var(--surface-subtle)/0.58)] px-3 py-3">
      <div className="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
        <MobileField
          label={t('card.headers.upstreamModel')}
          value={pricing.modelName || site.upstreamName || '--'}
        />
        <MobileField
          label={t('card.headers.group')}
          value={isNewAPISite(site.siteType) ? pricing.groupName || '--' : '-'}
        />
        <MobileAPIKeyField pricing={pricing} />
        <MobileField
          label={t('card.headers.groupRatio')}
          value={formatGroupRatio(pricing.groupRatio)}
        />
        <MobileField
          label={t('card.headers.inputPrice')}
          value={formatPricingValue(
            pricing.inputValue ?? pricing.perRequestValue,
            pricing.billingType,
            pricing.currency,
            t,
          )}
        />
        <MobileField
          label={t('card.headers.outputPrice')}
          value={formatPricingValue(
            pricing.outputValue ?? pricing.audioOutputValue,
            pricing.billingType,
            pricing.currency,
            t,
          )}
        />
        <MobileField
          label={t('card.headers.cachePrice')}
          value={formatPricingValue(
            pricing.cacheInputValue,
            pricing.billingType,
            pricing.currency,
            t,
          )}
        />
        <MobileField
          label={t('card.headers.cacheWritePrice')}
          value={formatPricingValue(
            pricing.createCacheInputValue,
            pricing.billingType,
            pricing.currency,
            t,
          )}
          title={
            pricing.createCache1hInputValue != null
              ? `5min: ${formatPricingValue(pricing.createCacheInputValue, pricing.billingType, pricing.currency, t)}\n1h: ${formatPricingValue(pricing.createCache1hInputValue, pricing.billingType, pricing.currency, t)}`
              : undefined
          }
        />
      </div>

      <div className="mt-3">
        <div className="text-[11px] text-muted-soft">
          {t('card.headers.protocol')}
        </div>
        <div className="mt-1 flex flex-wrap gap-1.5">
          {endpointTypes.length ? (
            endpointTypes.map((endpointType) => (
              <Badge
                key={`${site.modelId}-${endpointType}`}
                variant="neutral"
                className="max-w-full border-transparent px-2 py-0.5 text-[11px] tracking-normal"
              >
                <span className="break-all">
                  {formatEndpointTypeLabel(endpointType)}
                </span>
              </Badge>
            ))
          ) : (
            <span className="text-xs text-muted-soft">--</span>
          )}
        </div>
      </div>
    </div>
  )
}

function MobileAPIKeyField({ pricing }: { pricing: PricingRow }) {
  const { t } = useTranslation('models')
  if (!pricing.apiKeyName) {
    return <MobileKeyGroupField status={pricing.keyGroupStatus} />
  }
  return (
    <MobileField
      label={t('card.headers.apiKeys')}
      value={pricing.apiKeyName}
    />
  )
}

function MobileField({
  label,
  value,
  title,
}: {
  label: string
  value: string
  title?: string
}) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] text-muted-soft">{label}</div>
      <div
        className="mt-1 break-words font-medium leading-4 text-foreground"
        title={title ?? value}
      >
        {value}
      </div>
    </div>
  )
}

function MobileKeyGroupField({
  status,
}: {
  status?: MarketplaceKeyGroupStatus
}) {
  const { t } = useTranslation('models')

  if (!status) {
    return <MobileField label={t('card.headers.apiKeys')} value="--" />
  }

  const variant =
    status.enabled > 0 ? 'success' : status.total > 0 ? 'warning' : 'neutral'
  const tooltipKey =
    status.source === 'codex_accounts'
      ? 'card.keyGroup.codexTooltip'
      : 'card.keyGroup.keyTooltip'

  return (
    <div className="min-w-0">
      <div className="text-[11px] text-muted-soft">
        {t('card.headers.apiKeys')}
      </div>
      <Badge
        variant={variant}
        className="mt-1 justify-center border-transparent px-2 py-0.5 text-xs tabular-nums tracking-normal"
        title={t(tooltipKey, { enabled: status.enabled, total: status.total })}
      >
        {status.enabled}/{status.total}
      </Badge>
    </div>
  )
}
