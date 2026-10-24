import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Card, CardMetric, CardMetrics } from '@/components/ui/card'
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
    <Card className="overflow-hidden p-0">
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
                className="break-words text-headline leading-5 text-label transition-opacity hover:opacity-80"
                onClick={(event) => {
                  event.stopPropagation()
                  copyToClipboard(model.model_key, t('card.copied'))
                }}
              >
                {model.model_key}
              </div>
              <div className="text-label-2 text-footnote">{model.brand}</div>
            </div>
            <Chevron className="mt-0.5 h-4 w-4 shrink-0 text-label-2" />
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
        <div className="border-t border-separator bg-fill-4 px-3 py-3">
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
    <section className="border-b border-separator pb-4 last:border-b-0 last:pb-0">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div
            className="truncate text-headline text-label"
            title={site.siteName}
          >
            {site.siteName}
          </div>
          <div className="mt-1 break-words text-footnote text-label-2">
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
  const endpointTypes = (pricing.supportedEndpointTypes ?? site.supportedEndpointTypes)
    .map((endpointType) => ({ endpointType, enabled: true }))

  return (
    <div className="rounded-inner bg-fill-4 px-3 py-3">
      <CardMetrics>
        <CardMetric
          label={t('card.headers.upstreamModel')}
          value={pricing.modelName || site.upstreamName || '--'}
        />
        <CardMetric
          label={t('card.headers.group')}
          value={isNewAPISite(site.siteType) ? pricing.groupName || '--' : '-'}
        />
        <MobileAPIKeyField pricing={pricing} />
        <CardMetric
          label={t('card.headers.groupRatio')}
          value={formatGroupRatio(pricing.groupRatio)}
        />
        <CardMetric
          label={t('card.headers.inputPrice')}
          value={formatPricingValue(
            pricing.inputValue ?? pricing.perRequestValue,
            pricing.billingType,
            pricing.currency,
            t,
          )}
        />
        <CardMetric
          label={t('card.headers.outputPrice')}
          value={formatPricingValue(
            pricing.outputValue ?? pricing.audioOutputValue,
            pricing.billingType,
            pricing.currency,
            t,
          )}
        />
        <CardMetric
          label={t('card.headers.cachePrice')}
          value={formatPricingValue(
            pricing.cacheInputValue,
            pricing.billingType,
            pricing.currency,
            t,
          )}
        />
        <CardMetric
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
      </CardMetrics>

      <div className="mt-3">
        <div className="text-[11px] text-label-2">
          {t('card.headers.protocol')}
        </div>
        <div className="mt-1 flex flex-wrap gap-1.5">
          {endpointTypes.length ? (
            endpointTypes.map(({ endpointType, enabled }, index) => (
              <span key={`${site.modelId}-${endpointType}`} className={enabled ? undefined : 'line-through opacity-60'}>
                {index > 0 ? ' / ' : ''}{formatEndpointTypeLabel(endpointType)}
              </span>
            ))
          ) : (
            <span className="text-footnote text-label-2">--</span>
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
    <CardMetric
      label={t('card.headers.apiKeys')}
      value={pricing.apiKeyName}
    />
  )
}

function MobileKeyGroupField({
  status,
}: {
  status?: MarketplaceKeyGroupStatus
}) {
  const { t } = useTranslation('models')

  if (!status) {
    return <CardMetric label={t('card.headers.apiKeys')} value="--" />
  }

  const variant =
    status.enabled > 0 ? 'success' : status.total > 0 ? 'warning' : 'neutral'
  const tooltipKey =
    status.source === 'codex_accounts'
      ? 'card.keyGroup.codexTooltip'
      : 'card.keyGroup.keyTooltip'

  return (
    <div className="min-w-0">
      <div className="text-[11px] text-label-2">
        {t('card.headers.apiKeys')}
      </div>
      <Badge
        variant={variant}
        className="mt-1 justify-center border-transparent px-2 py-0.5 text-footnote tabular-nums tracking-normal"
        title={t(tooltipKey, { enabled: status.enabled, total: status.total })}
      >
        {status.enabled}/{status.total}
      </Badge>
    </div>
  )
}
